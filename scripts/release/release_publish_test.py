#!/usr/bin/env python3

import subprocess
import unittest
from unittest.mock import patch

from release_publish import PublishCredentialError, publish_github_release


class PublishGithubReleaseTest(unittest.TestCase):
    def test_release_token_is_scoped_to_goreleaser_environment(self):
        calls = []

        def runner(command, **kwargs):
            calls.append((tuple(command), kwargs))
            return subprocess.CompletedProcess(command, 0, "", "")

        publish_github_release(
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
                "PATH": "/bin",
            },
            runner=runner,
        )

        command, kwargs = calls[0]
        self.assertTrue(command[1].endswith("scripts/teamkeys/release.py"))
        self.assertTrue(command[3].endswith("scripts/release/run_clean.py"))
        self.assertEqual(
            command[4:],
            ("publish", "--", "goreleaser", "release", "--clean", "--release-notes", "/repo/notes.md"),
        )
        self.assertNotIn("release-secret", command)
        self.assertEqual(kwargs["env"]["GITHUB_TOKEN"], "release-secret")
        self.assertNotIn("GH_TOKEN", kwargs["env"])
        self.assertNotIn("RZM_RELEASE_GITHUB_TOKEN", kwargs["env"])
        self.assertNotIn("AWS_SESSION_TOKEN", kwargs["env"])
        self.assertNotIn("OP_SERVICE_ACCOUNT_TOKEN", kwargs["env"])
        # Only the bundler sees team-key inputs; it strips them before GoReleaser.
        self.assertEqual(kwargs["env"]["TYPESAFE_API_KEY"], "provider-secret")
        # run_clean.py hands the signing key to GoReleaser's desktop build hook.
        self.assertEqual(kwargs["env"]["TAURI_SIGNING_PRIVATE_KEY"], "signing-secret")
        self.assertEqual(kwargs["env"]["TAURI_SIGNING_PRIVATE_KEY_PASSWORD"], "signing-password")
        self.assertEqual(kwargs["env"]["RZM_RELEASE_DESKTOP"], "1")

    def test_internal_release_does_not_require_the_desktop_app(self):
        calls = []

        def runner(command, **kwargs):
            calls.append(kwargs)
            return subprocess.CompletedProcess(command, 0, "", "")

        with patch("scripts.teamkeys.release.repo_visibility", return_value="PRIVATE"):
            publish_github_release(
                "/repo/notes.md",
                environ={"RZM_RELEASE_GITHUB_TOKEN": "release-secret", "RZM_INTERNAL_S3_BUCKET": "example-bucket"},
                runner=runner,
            )

        self.assertNotIn("RZM_RELEASE_DESKTOP", calls[0]["env"])

    def test_missing_release_token_fails_before_goreleaser(self):
        calls = []

        with self.assertRaisesRegex(PublishCredentialError, "RZM_RELEASE_GITHUB_TOKEN"):
            publish_github_release(
                "/repo/notes.md",
                environ={},
                runner=lambda *args, **kwargs: calls.append((args, kwargs)),
            )

        self.assertEqual(calls, [])


if __name__ == "__main__":
    unittest.main()
