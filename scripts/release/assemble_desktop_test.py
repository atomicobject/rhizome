from __future__ import annotations

from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from assemble_desktop import assemble, signing_environment
from desktop_candidates import CandidateError, REQUIRED_TARGETS
from desktop_candidates_test import COMMIT, VERSION, make_candidate
from verify_desktop_signing import DesktopSigningError


class AssemblerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        config = self.root / "desktop/src-tauri/tauri.conf.json"
        config.parent.mkdir(parents=True)
        config.write_text('{"plugins":{"updater":{"pubkey":"c3ludGhldGljLXB1YmxpYy1rZXk="}}}')
        self.candidates, self.hashes = [], {}
        for target in REQUIRED_TARGETS:
            directory, digest = make_candidate(self.root, target)
            self.candidates.append(directory)
            self.hashes[target] = digest
        self.output = self.root / "assembled"
        self.sign_calls, self.verify_calls = [], []
        self.source = {"PATH": "/usr/bin", "TAURI_SIGNING_PRIVATE_KEY": "synthetic-key",
                       "VOYAGE_API_KEY": "synthetic-provider", "GITHUB_TOKEN": "synthetic-publish"}

    def tearDown(self):
        self.temporary.cleanup()

    def signer(self, command, **kwargs):
        self.sign_calls.append((command, kwargs))
        Path(command[-1] + ".sig").write_text("c3ludGhldGljLXNpZ25hdHVyZQ==\n")
        return subprocess.CompletedProcess(command, 0, "", "")

    def verifier(self, root, artifact, signature, **kwargs):
        self.verify_calls.append((root, artifact, signature, kwargs))

    def build(self, **kwargs):
        return assemble(self.candidates, VERSION, COMMIT, self.hashes, self.output,
                        root=self.root, source=self.source, runner=self.signer, verifier=self.verifier,
                        now=lambda: datetime(2026, 11, 5, tzinfo=timezone.utc), **kwargs)

    def test_complete_feed_is_signed_once_and_resume_verifies_without_resigning(self):
        feed_path = self.build()
        feed = json.loads(feed_path.read_text())
        self.assertEqual(REQUIRED_TARGETS, set(feed["platforms"]))
        self.assertEqual(VERSION, feed["version"])
        self.assertEqual(len(REQUIRED_TARGETS), len(self.sign_calls))
        for command, kwargs in self.sign_calls:
            self.assertIn("--app-version", command)
            self.assertEqual("synthetic-key", kwargs["env"]["TAURI_SIGNING_PRIVATE_KEY"])
            self.assertNotIn("VOYAGE_API_KEY", kwargs["env"])
            self.assertNotIn("GITHUB_TOKEN", kwargs["env"])
        for _, _, _, kwargs in self.verify_calls:
            self.assertNotIn("TAURI_SIGNING_PRIVATE_KEY", kwargs["environ"])
        self.assertEqual(feed_path, self.build())
        self.assertEqual(len(REQUIRED_TARGETS), len(self.sign_calls))
        # Each package is verified after signing and again on resume.
        self.assertEqual(2 * len(REQUIRED_TARGETS), len(self.verify_calls))
        self.assertTrue(all((directory / "desktop-candidate.json").exists() for directory in self.candidates))
        self.assertTrue(all(not list(directory.glob("*.sig")) for directory in self.candidates))

    def test_missing_target_or_wrong_hash_is_rejected_before_signing(self):
        self.candidates.pop()
        with self.assertRaisesRegex(CandidateError, "missing"):
            self.build()
        self.assertEqual([], self.sign_calls)
        self.assertFalse(self.output.exists())
        self.candidates.append(self.root / next(target for target in REQUIRED_TARGETS if target not in {d.name for d in self.candidates}))
        self.hashes["windows-x86_64"] = "0" * 64
        with self.assertRaisesRegex(CandidateError, "hash"):
            self.build()
        self.assertEqual([], self.sign_calls)

    def test_signing_or_verification_failure_leaves_no_advertisable_output(self):
        def refuse(*_args, **_kwargs):
            raise DesktopSigningError("synthetic bad signature")
        with self.assertRaises(DesktopSigningError):
            assemble(self.candidates, VERSION, COMMIT, self.hashes, self.output, root=self.root,
                     source=self.source, runner=self.signer, verifier=refuse)
        self.assertFalse(self.output.exists())
        self.assertTrue(all(not list(directory.glob("*.sig")) for directory in self.candidates))

    def test_production_refuses_test_key_or_partial_target_override(self):
        for options in ({"test_public_key": "synthetic"}, {"required_targets": {"windows-x86_64"}}):
            with self.subTest(options=options), self.assertRaisesRegex(CandidateError, "production"):
                self.build(**options)
        self.assertEqual([], self.sign_calls)

    def test_resume_refuses_changed_feed_urls(self):
        path = self.build()
        feed = json.loads(path.read_text())
        feed["platforms"]["windows-x86_64"]["url"] = "https://unrelated.invalid/package"
        path.write_text(json.dumps(feed))
        with self.assertRaisesRegex(CandidateError, "feed"):
            self.build()

    def test_signer_requires_one_key_and_filters_unrelated_credentials(self):
        with self.assertRaises(CandidateError):
            signing_environment({})
        with self.assertRaises(CandidateError):
            signing_environment({"TAURI_SIGNING_PRIVATE_KEY": "synthetic", "TAURI_SIGNING_PRIVATE_KEY_PATH": "synthetic-file"})
        env = signing_environment({"TAURI_SIGNING_PRIVATE_KEY_PATH": "synthetic-file", "AWS_SECRET_ACCESS_KEY": "synthetic"})
        self.assertEqual("synthetic-file", env["TAURI_SIGNING_PRIVATE_KEY_PATH"])
        self.assertNotIn("AWS_SECRET_ACCESS_KEY", env)


if __name__ == "__main__":
    unittest.main()
