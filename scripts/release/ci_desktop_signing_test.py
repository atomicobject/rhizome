#!/usr/bin/env python3

import base64
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest

from ci_desktop_signing import SigningSetupError, cleanup, prepare, security


class Runner:
    def __init__(self, fail=None):
        self.commands = []
        self.calls = []
        self.fail = fail

    def __call__(self, argv, **kwargs):
        self.calls.append((argv, kwargs))
        command = shlex.split(kwargs["input"])
        self.commands.append(command)
        stdout = '"/synthetic/login.keychain-db"\n' if command == ["list-keychains", "-d", "user"] else ""
        if command[0] == "find-identity":
            stdout = "1 valid identities found\n"
        if command[0] == "create-keychain":
            Path(command[-1]).touch(mode=0o600)
        if command[0] == "delete-keychain":
            Path(command[-1]).unlink()
        return subprocess.CompletedProcess(argv, 1 if command[0] == self.fail else 0, stdout=stdout,
                                           stderr="synthetic-private-output" if command[0] == self.fail else "")


class CISigningTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.env = {
            "RUNNER_TEMP": str(self.root),
            "GITHUB_ENV": str(self.root / "github-env"),
            "APPLE_CERTIFICATE": base64.b64encode(b"synthetic-p12").decode(),
            "APPLE_CERTIFICATE_PASSWORD": 'synthetic password with "quotes" and \\slashes',
            "APPLE_API_PRIVATE_KEY": "synthetic-p8",
        }

    def test_private_assets_and_cleanup_restore_search_list(self):
        runner = Runner()
        prepare(self.env, runner=runner, download=lambda: b"synthetic-intermediate")
        directory = self.root / "rhizome-desktop-signing"
        self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        key = directory / "notarization.p8"
        self.assertEqual(key.stat().st_mode & 0o777, 0o600)
        self.assertEqual(key.read_text(), "synthetic-p8")
        self.assertFalse((directory / "certificate.p12").exists())
        self.assertFalse((directory / "intermediate.cer").exists())
        exported = (self.root / "github-env").read_text()
        self.assertEqual(exported, f"APPLE_API_KEY_PATH={key}\n")
        imports = [command for command in runner.commands if command[0] == "import"]
        self.assertEqual(imports[0][imports[0].index("-P") + 1], self.env["APPLE_CERTIFICATE_PASSWORD"])
        for argv, kwargs in runner.calls:
            self.assertEqual(argv, ["/usr/bin/security", "-i"])
            self.assertNotIn("APPLE_API_PRIVATE_KEY", kwargs["env"])
        cleanup(self.env, runner=runner)
        self.assertFalse(directory.exists())
        self.assertIn(["list-keychains", "-d", "user", "-s", "/synthetic/login.keychain-db"], runner.commands)
        self.assertEqual(runner.commands[-1][0], "delete-keychain")

    def test_import_failure_cleans_up_and_does_not_export_key(self):
        runner = Runner(fail="import")
        with self.assertRaises(SigningSetupError):
            prepare(self.env, runner=runner, download=lambda: b"synthetic-intermediate")
        self.assertFalse((self.root / "rhizome-desktop-signing").exists())
        self.assertFalse((self.root / "github-env").exists())

    def test_cleanup_is_safe_when_preparation_never_ran(self):
        runner = Runner()
        cleanup(self.env, runner=runner)
        self.assertEqual(runner.commands, [])

    def test_missing_or_malformed_credentials_do_not_touch_keychain(self):
        for updates in ({"APPLE_CERTIFICATE": "not base64!"}, {"APPLE_CERTIFICATE_PASSWORD": "bad\npassword"},
                        {"APPLE_API_PRIVATE_KEY": ""}, {"GITHUB_ENV": ""}):
            with self.subTest(updates=tuple(updates)):
                runner = Runner()
                with self.assertRaises(SigningSetupError):
                    prepare({**self.env, **updates}, runner=runner)
                self.assertEqual(runner.calls, [])

    def test_security_failure_does_not_include_secret_output(self):
        with self.assertRaises(SigningSetupError) as caught:
            security(["import", "synthetic"], runner=Runner(fail="import"))
        self.assertNotIn("synthetic-private-output", str(caught.exception))

    def test_command_error_is_rejected_even_if_interpreter_returns_zero(self):
        def run(argv, **kwargs):
            return subprocess.CompletedProcess(argv, 0, stdout="", stderr="security: synthetic import failure")
        with self.assertRaises(SigningSetupError):
            security(["import", "synthetic"], runner=run)

    def test_missing_valid_identity_stops_setup_and_cleans_up(self):
        original = Runner()
        def run(argv, **kwargs):
            result = original(argv, **kwargs)
            if shlex.split(kwargs["input"])[0] == "find-identity":
                result.stdout = "0 valid identities found\n"
            return result
        with self.assertRaises(SigningSetupError):
            prepare(self.env, runner=run, download=lambda: b"synthetic-intermediate")
        self.assertFalse((self.root / "github-env").exists())
        self.assertFalse((self.root / "rhizome-desktop-signing").exists())

    def test_security_arguments_reject_command_injection(self):
        with self.assertRaises(SigningSetupError):
            security(["import", "synthetic\ncommand"], runner=Runner())


if __name__ == "__main__":
    unittest.main()
