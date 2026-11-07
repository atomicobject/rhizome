#!/usr/bin/env python3
"""The two owned recipes retry from synthetic, source-bound publication receipts."""
import base64
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from desktop_candidates import TARGETS
from release_homebrew import HomebrewError, RECIPES, recipe_contents, retain_recipes, publish_tap

COMMIT = "a" * 40
VERSION = "2.0.0"


def digest(content):
    return hashlib.sha256(content).hexdigest()


class TapFixture:
    def __init__(self, assets):
        self.files = {"rhizome.rb": recipe_contents("1.0.0", assets)["rhizome.rb"]}
        self.puts = []
        self.interrupt_after = self.fail_before = False
        self.fail_path = None

    def request(self, path, *, data=None):
        if data is None:
            if path not in self.files:
                return None
            content = self.files[path]
            return {"type": "file", "path": path, "encoding": "base64", "sha": digest(content)[:40],
                    "content": base64.b64encode(content).decode()}
        self.puts.append((path, data.copy()))
        if self.fail_before or path == self.fail_path:
            self.fail_before = False
            raise HomebrewError("synthetic failure before write")
        current_sha = digest(self.files[path])[:40] if path in self.files else None
        if data.get("sha") != current_sha:
            raise HomebrewError("synthetic conflicting write")
        self.files[path] = base64.b64decode(data["content"])
        if self.interrupt_after:
            self.interrupt_after = False
            raise HomebrewError("synthetic interrupted response after write")
        return {"commit": {"sha": "b" * 40}}


class HomebrewTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "advertisement.json"
        names = {"latest.json", "checksums.txt", "install-rzm.sh", "install-rzm.sh.sha256", "desktop-assembly.json",
                 "release-manifest.json", "Rhizome-macos-arm64.dmg"}
        names.update(f"rhizome-{target}.tar.gz" for target in
                     ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64"))
        names.update(target["asset"] + suffix for target in TARGETS.values() for suffix in ("", ".sig"))
        self.assets = {name: {"sha256": digest(name.encode()), "size": 1} for name in names}
        hashes = retain_recipes(self.path, VERSION, self.assets)
        self.advertisement = {"schemaVersion": 1, "phase": "complete", "releaseId": 2, "previousLatest": None,
                              "expected": {"repository": "atomicobject/rhizome", "version": VERSION, "sourceCommit": COMMIT,
                                           "publicKeySha256": "b" * 64, **hashes, "assets": self.assets}}
        self.save()
        self.tap = TapFixture(self.assets)

    def save(self):
        self.path.write_text(json.dumps(self.advertisement))

    def publish(self, **kwargs):
        return publish_tap(self.path, expected_commit=COMMIT, expected_version=VERSION, tap=self.tap, **kwargs)

    def test_formula_and_cask_publish_once_and_retry_does_not_recommit(self):
        receipt = self.publish()
        self.assertEqual(set(receipt["recipes"]), set(RECIPES))
        self.assertTrue(all(value["phase"] == "complete" for value in receipt["recipes"].values()))
        self.assertEqual(self.tap.files, recipe_contents(VERSION, self.assets))
        self.assertEqual([path for path, _ in self.tap.puts], ["rhizome.rb", "Casks/rhizome-desktop.rb"])
        self.assertTrue(all(data["branch"] == "main" for _, data in self.tap.puts))
        self.publish()
        self.assertEqual(len(self.tap.puts), 2)

    def test_cask_preserves_darwin_dmg_and_bundled_cli_contract(self):
        cask = recipe_contents(VERSION, self.assets)["Casks/rhizome-desktop.rb"].decode()
        for text in ('cask "rhizome-desktop" do', 'version "2.0.0"', 'auto_updates true',
                     'depends_on arch: :arm64', 'depends_on macos: :monterey', 'app "Rhizome.app"',
                     'binary "#{appdir}/Rhizome.app/Contents/MacOS/rzm"',
                     '/download/v#{version}/Rhizome-macos-arm64.dmg'):
            self.assertIn(text, cask)
        self.assertIn(self.assets["Rhizome-macos-arm64.dmg"]["sha256"], cask)

    def test_unfinished_advertisement_never_updates_either_recipe(self):
        self.advertisement["phase"] = "public"
        self.save()
        with self.assertRaisesRegex(HomebrewError, "completed release advertisement"):
            self.publish()
        self.assertEqual(self.tap.puts, [])

    def test_wrong_formula_or_cask_binding_blocks_every_write(self):
        for name, (suffix, field) in RECIPES.items():
            with self.subTest(path=name):
                retained = self.path.with_suffix(suffix)
                original = retained.read_bytes()
                wrong = original.replace(b"/download/", b"/different/")
                retained.write_bytes(wrong)
                self.advertisement["expected"][field] = digest(wrong)
                self.save()
                with self.assertRaisesRegex(HomebrewError, "source/version/assets"):
                    self.publish()
                self.assertEqual(self.tap.puts, [])
                retained.write_bytes(original)
                self.advertisement["expected"][field] = digest(original)
                self.save()

    def test_partial_formula_success_retries_only_cask(self):
        self.tap.fail_path = "Casks/rhizome-desktop.rb"
        with self.assertRaises(HomebrewError):
            self.publish()
        formula = self.tap.files["rhizome.rb"]
        self.tap.fail_path = None
        self.tap.puts.clear()
        checked = []
        self.publish(before_update=lambda: checked.append("gateway"))
        self.assertEqual(checked, ["gateway"])
        self.assertEqual([path for path, _ in self.tap.puts], ["Casks/rhizome-desktop.rb"])
        self.assertEqual(self.tap.files["rhizome.rb"], formula)

    def test_interrupted_write_reconciles_current_bytes_without_duplicate_formula_commit(self):
        self.tap.interrupt_after = True
        with self.assertRaises(HomebrewError):
            self.publish()
        self.publish()
        self.assertEqual([path for path, _ in self.tap.puts].count("rhizome.rb"), 1)
        self.assertEqual(self.tap.files, recipe_contents(VERSION, self.assets))

    def test_competing_tap_change_during_retry_is_not_overwritten(self):
        self.tap.fail_before = True
        with self.assertRaises(HomebrewError):
            self.publish()
        self.tap.files["rhizome.rb"] = recipe_contents("1.5.0", self.assets)["rhizome.rb"]
        with self.assertRaisesRegex(HomebrewError, "tap changed"):
            self.publish()
        self.assertEqual(len(self.tap.puts), 1)

    def test_later_tap_release_is_not_rolled_back(self):
        self.tap.files["rhizome.rb"] = recipe_contents("3.0.0", self.assets)["rhizome.rb"]
        with self.assertRaisesRegex(HomebrewError, "newer release"):
            self.publish()
        self.assertEqual(self.tap.puts, [])

    def test_missing_completed_cask_requires_fresh_gate_before_repair(self):
        self.publish()
        del self.tap.files["Casks/rhizome-desktop.rb"]
        self.tap.puts.clear()
        def reject():
            raise HomebrewError("fresh gateway failed")
        with self.assertRaisesRegex(HomebrewError, "fresh gateway failed"):
            self.publish(before_update=reject)
        self.assertNotIn("Casks/rhizome-desktop.rb", self.tap.files)
        self.assertEqual(self.tap.puts, [])


if __name__ == "__main__":
    unittest.main()
