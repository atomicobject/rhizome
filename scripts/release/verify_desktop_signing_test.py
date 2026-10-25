#!/usr/bin/env python3
"""Real signature checks using newly generated, disposable synthetic keys only."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from run_clean import sensitive_name
from verify_desktop_signing import DesktopSigningError, ROOT, check_signing_key, verify_archive


class DesktopSigningTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="rzm-synthetic-signing-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.root = Path(cls.temp.name)
        cls.config = cls.root / "desktop/src-tauri/tauri.conf.json"
        cls.config.parent.mkdir(parents=True)
        cls.tauri = ROOT / "desktop/node_modules/.bin/tauri"
        cls.env = {"PATH": os.environ.get("PATH", ""), "CI": "true"}
        cls.keys = []
        for name in ("synthetic-a", "synthetic-b"):
            key = cls.root / name
            subprocess.run([str(cls.tauri), "signer", "generate", "--ci", "--password", "", "--write-keys", str(key)],
                           env=cls.env, capture_output=True, check=True, timeout=30)
            cls.keys.append(key)
        cls.archive = cls.root / "fixture.app.tar.gz"

    def setUp(self):
        self.archive.write_bytes(b"Synthetic updater archive\n")
        self.configure_public_key(self.keys[0])
        self.signing_env = {**self.env, "TAURI_SIGNING_PRIVATE_KEY": self.keys[0].read_text(),
                            "TAURI_SIGNING_PRIVATE_KEY_PASSWORD": "", "VOYAGE_API_KEY": "synthetic-provider"}
        self.run_signer([str(self.tauri), "signer", "sign", str(self.archive)], env=self.signing_env)

    def configure_public_key(self, key):
        self.config.write_text(json.dumps({"plugins": {"updater": {"pubkey": key.with_suffix(".pub").read_text().strip()}}}))

    @staticmethod
    def run_signer(command, **kwargs):
        return subprocess.run(command, capture_output=True, check=True, timeout=30, **kwargs)

    def test_matched_key_and_archive_verify_without_credentials_in_verifier(self):
        calls = []

        def runner(command, **kwargs):
            calls.append(command)
            self.assertFalse(any(sensitive_name(name) for name in kwargs["env"]))
            return subprocess.run(command, **kwargs)

        verify_archive(self.root, self.archive, self.archive.with_suffix(".gz.sig"),
                       environ=self.signing_env, runner=runner)
        self.assertEqual(len(calls), 2)

    def test_wrong_public_key_and_corrupt_archive_are_rejected(self):
        self.configure_public_key(self.keys[1])
        with self.assertRaisesRegex(DesktopSigningError, "signature verification"):
            verify_archive(self.root, self.archive, self.archive.with_suffix(".gz.sig"), environ=self.signing_env)
        self.configure_public_key(self.keys[0])
        self.archive.write_bytes(b"Corrupt archive\n")
        with self.assertRaisesRegex(DesktopSigningError, "signature verification"):
            verify_archive(self.root, self.archive, self.archive.with_suffix(".gz.sig"), environ=self.signing_env)

    def test_current_key_preflight_accepts_match_and_rejects_replaced_key(self):
        def runner(command, **kwargs):
            if command[0] == "npm":
                self.assertNotIn("VOYAGE_API_KEY", kwargs["env"])
                command = [str(self.tauri), *command[4:]]
            else:
                self.assertFalse(any(sensitive_name(name) for name in kwargs["env"]))
            return subprocess.run(command, **kwargs)

        check_signing_key(self.root, environ=self.signing_env, runner=runner)
        self.signing_env["TAURI_SIGNING_PRIVATE_KEY"] = self.keys[1].read_text()
        with self.assertRaisesRegex(DesktopSigningError, "signature verification"):
            check_signing_key(self.root, environ=self.signing_env, runner=runner)

    def test_subprocess_timeout_is_bounded_and_does_not_expose_output(self):
        def timeout(command, **kwargs):
            self.assertEqual(kwargs["timeout"], 120)
            raise subprocess.TimeoutExpired(command, 120, stderr="synthetic secret output")

        with self.assertRaises(DesktopSigningError) as raised:
            verify_archive(self.root, self.archive, self.archive.with_suffix(".gz.sig"),
                           environ=self.signing_env, runner=timeout)
        self.assertNotIn("synthetic secret output", str(raised.exception))


if __name__ == "__main__":
    unittest.main()
