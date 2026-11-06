#!/usr/bin/env python3

import subprocess
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from release_publish import PublishCredentialError, publish_github_release


class PublishGithubReleaseTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def publish(self, *args, **kwargs):
        if kwargs.get("environ", {}).get("RZM_RELEASE_GITHUB_TOKEN"):
            kwargs["environ"] = {"BREW_GITHUB_TOKEN": "synthetic-tap", **kwargs["environ"]}
        kwargs.setdefault("repo_root", self.root)
        kwargs.setdefault("release_identity", lambda root, **kwargs: ("1.2.3", "a" * 40))
        kwargs.setdefault("prepare", lambda *args: None)
        kwargs.setdefault("verify", lambda *args: None)
        return publish_github_release(*args, **kwargs)

    def test_release_token_is_scoped_to_goreleaser_environment(self):
        calls = []

        def runner(command, **kwargs):
            calls.append((tuple(command), kwargs))
            return subprocess.CompletedProcess(command, 0, "", "")

        self.publish(
            "/repo/notes.md",
            environ={
                "RZM_RELEASE_GITHUB_TOKEN": "release-secret",
                "GITHUB_TOKEN": "conflicting-secret",
                "GH_TOKEN": "evidence-secret",
                "AWS_SESSION_TOKEN": "aws-secret",
                "OP_SERVICE_ACCOUNT_TOKEN": "op-secret",
                "TYPESAFE_API_KEY": "provider-secret",
                "TAURI_SIGNING_PRIVATE_KEY": "signing-secret",
                "TAURI_SIGNING_PRIVATE_KEY_PASSWORD": "signing-password",
                "APPLE_API_PRIVATE_KEY": "synthetic-apple-private-key",
                "PATH": "/bin",
            },
            runner=runner,
            gate=lambda *args, **kwargs: {},
        )

        command, kwargs = calls[0]
        self.assertTrue(command[1].endswith("scripts/teamkeys/release.py"))
        self.assertTrue(command[3].endswith("scripts/release/run_clean.py"))
        self.assertEqual(
            command[4:],
            ("publish", "--", "goreleaser", "release", "--clean", "--draft", "--release-notes", "/repo/notes.md"),
        )
        self.assertNotIn("release-secret", command)
        self.assertEqual(kwargs["env"]["GITHUB_TOKEN"], "release-secret")
        self.assertNotIn("GH_TOKEN", kwargs["env"])
        self.assertNotIn("RZM_RELEASE_GITHUB_TOKEN", kwargs["env"])
        self.assertNotIn("BREW_GITHUB_TOKEN", kwargs["env"])
        self.assertNotIn("AWS_SESSION_TOKEN", kwargs["env"])
        self.assertNotIn("OP_SERVICE_ACCOUNT_TOKEN", kwargs["env"])
        # Only the bundler sees team-key inputs; it strips them before GoReleaser.
        self.assertEqual(kwargs["env"]["TYPESAFE_API_KEY"], "provider-secret")
        # run_clean.py hands the signing key to GoReleaser's desktop build hook.
        self.assertEqual(kwargs["env"]["TAURI_SIGNING_PRIVATE_KEY"], "signing-secret")
        self.assertEqual(kwargs["env"]["TAURI_SIGNING_PRIVATE_KEY_PASSWORD"], "signing-password")
        self.assertEqual(kwargs["env"]["APPLE_API_PRIVATE_KEY"], "synthetic-apple-private-key")
        self.assertEqual(kwargs["env"]["RZM_RELEASE_DESKTOP"], "1")
        self.assertEqual(len(calls), 3)
        advertisement_command, advertisement = calls[1]
        self.assertTrue(advertisement_command[1].endswith("release_advertise.py"))
        self.assertEqual(advertisement["env"]["GITHUB_TOKEN"], "release-secret")
        for name in ("TAURI_SIGNING_PRIVATE_KEY", "APPLE_API_PRIVATE_KEY", "TYPESAFE_API_KEY", "BREW_GITHUB_TOKEN", "GH_TOKEN"):
            self.assertNotIn(name, advertisement["env"])
        tap_command, tap = calls[2]
        self.assertTrue(tap_command[1].endswith("release_homebrew.py"))
        self.assertEqual(tap["env"]["BREW_GITHUB_TOKEN"], "synthetic-tap")
        for name in ("GITHUB_TOKEN", "RZM_RELEASE_GITHUB_TOKEN", "TAURI_SIGNING_PRIVATE_KEY", "APPLE_API_PRIVATE_KEY", "TYPESAFE_API_KEY"):
            self.assertNotIn(name, tap["env"])

    def test_internal_release_does_not_require_the_desktop_app(self):
        calls = []

        def runner(command, **kwargs):
            calls.append(kwargs)
            return subprocess.CompletedProcess(command, 0, "", "")

        with patch("scripts.teamkeys.release.repo_visibility", return_value="PRIVATE"):
            self.publish(
                "/repo/notes.md",
                environ={"RZM_RELEASE_GITHUB_TOKEN": "release-secret", "RZM_INTERNAL_S3_BUCKET": "example-bucket"},
                runner=runner,
                gate=lambda *args, **kwargs: {},
            )

        self.assertNotIn("RZM_RELEASE_DESKTOP", calls[0]["env"])

    def test_gateway_failure_blocks_goreleaser(self):
        from release_gateway import GatewayError
        calls = []
        def gate(*args, **kwargs):
            raise GatewayError("gateway failed")
        with self.assertRaisesRegex(GatewayError, "gateway failed"):
            self.publish(environ={"RZM_RELEASE_GITHUB_TOKEN": "synthetic"},
                                   gate=gate, runner=lambda *a, **k: calls.append(a))
        self.assertEqual(calls, [])

    def test_direct_publish_gates_before_goreleaser_and_optional_notes(self):
        calls = []
        def gate(*args, **kwargs):
            calls.append(("gate", kwargs))
            return {}
        def runner(command, **kwargs):
            calls.append(("publish", command))
            return subprocess.CompletedProcess(command, 0, "", "")
        self.publish(environ={"RZM_RELEASE_GITHUB_TOKEN": "synthetic"},
                               gate=gate, runner=runner)
        self.assertEqual([c[0] for c in calls], ["gate", "publish", "publish", "gate", "publish"])
        self.assertIsNone(calls[0][1]["receipt_path"])
        self.assertNotIn("--release-notes", calls[1][1])

    def test_handoff_passes_receipt_for_verification(self):
        from pathlib import Path
        received = []
        self.publish(gateway_receipt=Path("/tmp/receipt.json"),
            environ={"RZM_RELEASE_GITHUB_TOKEN": "synthetic"},
            gate=lambda *a, **k: received.append(k),
            runner=lambda c, **k: subprocess.CompletedProcess(c, 0, "", ""))
        self.assertEqual(received[0]["receipt_path"], Path("/tmp/receipt.json"))

    def test_missing_release_token_fails_before_goreleaser(self):
        calls = []

        with self.assertRaisesRegex(PublishCredentialError, "RZM_RELEASE_GITHUB_TOKEN"):
            self.publish(
                "/repo/notes.md",
                environ={},
                runner=lambda *args, **kwargs: calls.append((args, kwargs)),
                gate=lambda *args, **kwargs: self.fail("gate dispatched without publish credentials"),
            )

        self.assertEqual(calls, [])

    def test_native_verification_precedes_gateway_and_blocks_every_upload(self):
        calls = []
        def verify(*args):
            calls.append("native")
            raise PublishCredentialError("native candidate changed")
        with self.assertRaisesRegex(PublishCredentialError, "native candidate changed"):
            self.publish(environ={"RZM_RELEASE_GITHUB_TOKEN": "synthetic"}, verify=verify,
                         gate=lambda *a, **k: calls.append("gateway"), runner=lambda *a, **k: calls.append("upload"))
        self.assertEqual(calls, ["native"])

    def test_failed_upload_refuses_automatic_reupload_even_before_a_remote_tag_exists(self):
        calls = []
        def fail(command, **kwargs):
            calls.append(command)
            return subprocess.CompletedProcess(command, 1, "", "interrupted upload")
        options = {"environ": {"RZM_RELEASE_GITHUB_TOKEN": "synthetic"}, "runner": fail,
                   "gate": lambda *a, **k: {}}
        with self.assertRaisesRegex(PublishCredentialError, "interrupted upload"):
            self.publish(**options)
        with self.assertRaisesRegex(PublishCredentialError, "inspect its draft"):
            self.publish(**options)
        self.assertEqual(len(calls), 1)

    def test_verified_advertisement_resume_skips_goreleaser(self):
        calls = []
        path = self.root / "advertisement.json"
        path.write_text("synthetic exact verified upload receipt")
        with patch("release_publish.advertisement_resume_available", return_value=True):
            self.publish(advertisement_receipt=path, environ={"RZM_RELEASE_GITHUB_TOKEN": "synthetic"},
                         gate=lambda *a, **k: {}, runner=lambda c, **k: calls.append(c) or subprocess.CompletedProcess(c, 0, "", ""))
        self.assertEqual(len(calls), 2)
        self.assertTrue(calls[0][1].endswith("release_advertise.py"))
        self.assertIn("--resume", calls[0])
        self.assertTrue(calls[1][1].endswith("release_homebrew.py"))


if __name__ == "__main__":
    unittest.main()
