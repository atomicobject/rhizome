from pathlib import Path
import subprocess
import unittest
from unittest.mock import Mock

from apple_signing import AppleSigningError, credentials, notarize_dmg, require_credentials, verify_app


IDENTITY = "Developer ID Application: Example LLC (ABCDEFGHIJ)"
ENV = {"APPLE_SIGNING_IDENTITY": IDENTITY, "APPLE_API_KEY": "synthetic-id",
       "APPLE_API_ISSUER": "synthetic-issuer", "APPLE_API_PRIVATE_KEY": "synthetic-private-key"}


class AppleSigningTest(unittest.TestCase):
    def test_dmg_rejection_or_timeout_prevents_stapling(self):
        for result in (subprocess.CompletedProcess([], 0, '{"status":"Invalid"}', ""),
                       subprocess.TimeoutExpired([], 1)):
            runner = Mock(side_effect=result) if isinstance(result, Exception) else Mock(return_value=result)
            with self.assertRaises(AppleSigningError):
                notarize_dmg(Path("candidate.dmg"), runner=runner,
                             env={**ENV, "APPLE_API_KEY_PATH": "/synthetic/key.p8"})
            self.assertEqual(1, runner.call_count)

    def test_credentials_required_and_identity_must_be_developer_id(self):
        for name in ENV:
            with self.subTest(name=name), self.assertRaises(AppleSigningError):
                require_credentials({key: value for key, value in ENV.items() if key != name})
        with self.assertRaises(AppleSigningError):
            require_credentials({**ENV, "APPLE_SIGNING_IDENTITY": "-"})

    def test_private_key_file_is_protected_and_removed_even_on_failure(self):
        for fail in (False, True):
            try:
                with credentials(ENV) as env:
                    path = Path(env["APPLE_API_KEY_PATH"])
                    self.assertEqual(0o600, path.stat().st_mode & 0o777)
                    self.assertEqual("synthetic-private-key", path.read_text())
                    self.assertNotIn("APPLE_API_PRIVATE_KEY", env)
                    if fail:
                        raise RuntimeError("synthetic failure")
            except RuntimeError:
                pass
            self.assertFalse(path.exists())
        self.assertIn("APPLE_API_PRIVATE_KEY", ENV)

    def test_wrong_team_or_missing_runtime_is_rejected(self):
        for details in ("TeamIdentifier=WRONGTEAMX\nflags=0x10000(runtime)\n", "TeamIdentifier=ABCDEFGHIJ\nflags=0x0(none)\n"):
            runner = Mock(return_value=subprocess.CompletedProcess([], 0, "", details))
            with self.assertRaises(AppleSigningError):
                verify_app(Path("Rhizome.app"), IDENTITY, runner=runner, env={})

    def test_rejected_gatekeeper_or_stapling_stops_distribution_verification(self):
        for tool in ("spctl", "xcrun"):
            def run(command, **kwargs):
                if command[0] == tool:
                    raise subprocess.CalledProcessError(1, command, stderr="synthetic secret")
                return subprocess.CompletedProcess(command, 0, "", "TeamIdentifier=ABCDEFGHIJ\nflags=0x10000(runtime)\n")
            with self.assertRaises(AppleSigningError) as caught:
                verify_app(Path("Rhizome.app"), IDENTITY, runner=run, env={})
            self.assertNotIn("synthetic secret", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
