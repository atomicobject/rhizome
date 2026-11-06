#!/usr/bin/env python3
"""Tap retries use synthetic contents API responses and retained release receipts."""
import base64
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from desktop_candidates import TARGETS
from release_advertise import DOWNLOAD
from release_homebrew import (ARCHIVES, FORMULA, HomebrewError, homebrew_environment, publish_tap,
                              verify_live_handoff)

COMMIT = "a" * 40
VERSION = "2.0.0"


def digest(content):
    return hashlib.sha256(content).hexdigest()


def formula(version):
    return ('class Rhizome < Formula\n  version "' + version + '"\n' + "".join(
        f'  url "{DOWNLOAD}/download/v{version}/rhizome-{target}.tar.gz"\n  sha256 "{digest(target.encode())}"\n'
        for target in ARCHIVES) + 'end\n').encode()


class TapFixture:
    def __init__(self):
        self.content = formula("1.0.0")
        self.puts = []
        self.interrupt_after = False
        self.fail_before = False

    def request(self, *, data=None):
        if data is None:
            if self.content is None:
                return None
            return {"type": "file", "path": FORMULA, "encoding": "base64", "sha": digest(self.content)[:40],
                    "content": base64.b64encode(self.content).decode()}
        self.puts.append(data.copy())
        if self.fail_before:
            self.fail_before = False
            raise HomebrewError("synthetic failure before write")
        current_sha = digest(self.content)[:40] if self.content is not None else None
        if data.get("sha") != current_sha:
            raise HomebrewError("synthetic conflicting write")
        self.content = base64.b64decode(data["content"])
        if self.interrupt_after:
            self.interrupt_after = False
            raise HomebrewError("synthetic interrupted response after write")
        return {"commit": {"sha": "b" * 40}}


class HomebrewTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "advertisement.json"
        assets = {name: {"sha256": digest(name.encode()), "size": 1} for name in
                  ("latest.json", "checksums.txt", "install-rzm.sh", "install-rzm.sh.sha256", "desktop-assembly.json",
                   "Rhizome-macos-arm64.dmg", "rhizome-windows-amd64.tar.gz")}
        for target in ARCHIVES:
            assets[f"rhizome-{target}.tar.gz"] = {"sha256": digest(target.encode()), "size": 1}
        for target in TARGETS.values():
            for suffix in ("", ".sig"):
                assets[target["asset"] + suffix] = {"sha256": digest(target["asset"].encode()), "size": 1}
        self.advertisement = {"schemaVersion": 1, "phase": "complete", "releaseId": 2, "previousLatest": None,
                              "expected": {"repository": "atomicobject/rhizome", "version": VERSION, "sourceCommit": COMMIT,
                                           "publicKeySha256": "b" * 64, "homebrewFormulaSha256": digest(formula(VERSION)),
                                           "assets": assets}}
        self.path.with_suffix(".rhizome.rb").write_bytes(formula(VERSION))
        self.save()
        self.tap = TapFixture()

    def save(self):
        self.path.write_text(json.dumps(self.advertisement))

    def publish(self):
        return publish_tap(self.path, expected_commit=COMMIT, expected_version=VERSION, tap=self.tap)

    def test_completed_advertisement_updates_tap_once_and_retry_does_not_recommit(self):
        receipt = self.publish()
        self.assertEqual(receipt["phase"], "complete")
        self.assertEqual(self.tap.content, formula(VERSION))
        self.assertEqual(len(self.tap.puts), 1)
        self.assertEqual(self.tap.puts[0]["branch"], "main")
        self.publish()
        self.assertEqual(len(self.tap.puts), 1)

    def test_unfinished_advertisement_never_updates_tap(self):
        self.advertisement["phase"] = "public"
        self.save()
        with self.assertRaisesRegex(HomebrewError, "completed release advertisement"):
            self.publish()
        self.assertEqual(self.tap.puts, [])

    def test_retained_formula_archive_urls_hashes_and_version_are_all_bound(self):
        for replacement in (formula(VERSION).replace(b"/download/v2.0.0/", b"/download/v3.0.0/"),
                            formula(VERSION).replace(digest(b"darwin-amd64").encode(), b"0" * 64),
                            formula("3.0.0")):
            with self.subTest(formula=replacement[:40]):
                self.path.with_suffix(".rhizome.rb").write_bytes(replacement)
                self.advertisement["expected"]["homebrewFormulaSha256"] = digest(replacement)
                self.save()
                with self.assertRaises(HomebrewError):
                    self.publish()
                self.assertEqual(self.tap.puts, [])

    def test_mutated_retained_formula_is_rejected(self):
        self.path.with_suffix(".rhizome.rb").write_bytes(b"changed formula")
        with self.assertRaisesRegex(HomebrewError, "missing or changed"):
            self.publish()
        self.assertEqual(self.tap.puts, [])

    def test_interrupted_write_reconciles_exact_current_bytes(self):
        self.tap.interrupt_after = True
        with self.assertRaises(HomebrewError):
            self.publish()
        self.assertEqual(json.loads(self.path.with_suffix(".homebrew.json").read_text())["phase"], "publishing")
        self.publish()
        self.assertEqual(len(self.tap.puts), 1)
        self.assertEqual(json.loads(self.path.with_suffix(".homebrew.json").read_text())["phase"], "complete")

    def test_failed_write_retries_without_touching_release_publication(self):
        self.tap.fail_before = True
        with self.assertRaises(HomebrewError):
            self.publish()
        self.assertEqual(self.tap.content, formula("1.0.0"))
        self.publish()
        self.assertEqual(self.tap.content, formula(VERSION))
        self.assertEqual(len(self.tap.puts), 2)

    def test_competing_tap_change_during_retry_is_not_overwritten(self):
        self.tap.fail_before = True
        with self.assertRaises(HomebrewError):
            self.publish()
        self.tap.content = formula("1.5.0")
        with self.assertRaisesRegex(HomebrewError, "tap changed"):
            self.publish()
        self.assertEqual(self.tap.content, formula("1.5.0"))
        self.assertEqual(len(self.tap.puts), 1)

    def test_later_release_is_not_rolled_back(self):
        self.tap.content = formula("3.0.0")
        with self.assertRaisesRegex(HomebrewError, "newer release"):
            self.publish()
        self.assertEqual(self.tap.puts, [])

    def test_token_scope_excludes_release_and_signing_credentials(self):
        env = homebrew_environment({"PATH": "/bin", "BREW_GITHUB_TOKEN": "synthetic-brew",
            "GITHUB_TOKEN": "synthetic-release", "RZM_RELEASE_GITHUB_TOKEN": "synthetic-release",
            "TAURI_SIGNING_PRIVATE_KEY": "synthetic-updater", "APPLE_API_PRIVATE_KEY": "synthetic-apple"})
        self.assertEqual(env, {"PATH": "/bin", "BREW_GITHUB_TOKEN": "synthetic-brew"})

    def test_worker_requires_live_exact_gateway_parent_handoff(self):
        receipt = {"source": {"release_commit": COMMIT, "source_commit": COMMIT}, "attempt": {"state": "success"}}
        with patch("release_homebrew.tagged_release_identity", return_value=(VERSION, COMMIT)), \
             patch("release_homebrew.validate_topology"), patch("release_homebrew._load", return_value=receipt), \
             patch("release_homebrew._live_handoff", return_value=False):
            with self.assertRaisesRegex(HomebrewError, "live verified gateway"):
                verify_live_handoff(Path("/repo"), Path("/receipt"), COMMIT, VERSION)


if __name__ == "__main__":
    unittest.main()
