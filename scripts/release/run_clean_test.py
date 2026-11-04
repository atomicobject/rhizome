#!/usr/bin/env python3

import unittest
from unittest.mock import patch

from run_clean import run, scoped_environment


class CleanReleaseEnvironmentTest(unittest.TestCase):
    def setUp(self) -> None:
        self.source = {
            "PATH": "/bin",
            "GOCACHE": "/cache",
            "VOYAGE_API_KEY": "synthetic-voyage",
            "TYPESAFE_API_KEY": "synthetic-typesafe",
            "AWS_ACCESS_KEY_ID": "synthetic-aws-id",
            "AWS_SECRET_ACCESS_KEY": "synthetic-aws-secret",
            "AWS_SESSION_TOKEN": "synthetic-aws-session",
            "OP_SERVICE_ACCOUNT_TOKEN": "synthetic-op-token",
            "OP_ACCOUNT": "synthetic-op-account",
            "RZM_INTERNAL_S3_LEGACY_SUFFIX": "abcde12345",
            "ATOMIC_RHIZOME_KEY": "synthetic-unlock",
            "GITHUB_TOKEN": "synthetic-github-conflict",
            "GH_TOKEN": "synthetic-gh",
            "RZM_RELEASE_GITHUB_TOKEN": "synthetic-release",
            "BREW_GITHUB_TOKEN": "synthetic-brew",
        }

    def test_build_receives_no_credentials(self) -> None:
        self.assertEqual(
            {"PATH": "/bin", "GOCACHE": "/cache", "GOENV": "off"},
            scoped_environment(self.source, "build"),
        )

    def test_publish_receives_only_github_and_homebrew_tokens(self) -> None:
        self.assertEqual(
            {
                "PATH": "/bin",
                "GOCACHE": "/cache",
                "GOENV": "off",
                "GITHUB_TOKEN": "synthetic-release",
                "BREW_GITHUB_TOKEN": "synthetic-brew",
            },
            scoped_environment(self.source, "publish"),
        )

    def test_desktop_signing_key_reaches_only_goreleaser(self) -> None:
        self.source["TAURI_SIGNING_PRIVATE_KEY"] = "synthetic-signing-key"
        self.source["TAURI_SIGNING_PRIVATE_KEY_PASSWORD"] = "synthetic-signing-password"
        for mode in ("build", "publish"):
            for command, expected in ((["goreleaser", "release"], True), (["go", "build"], False)):
                with self.subTest(mode=mode, command=command[0]):
                    with patch("run_clean.subprocess.run") as runner:
                        runner.return_value.returncode = 0
                        run(mode, command, self.source)
                    env = runner.call_args.kwargs["env"]
                    self.assertEqual(expected, env.get("TAURI_SIGNING_PRIVATE_KEY") == "synthetic-signing-key")
                    self.assertEqual(expected, "TAURI_SIGNING_PRIVATE_KEY_PASSWORD" in env)

    def test_direct_wrapper_rejects_public_overlay_and_preserves_internal_overlay(self):
        self.source["GOFLAGS"] = "-overlay=/synthetic/overlay.json"
        with self.assertRaisesRegex(ValueError, "compiler overlay"):
            scoped_environment(self.source, "publish")
        self.source["RZM_INTERNAL_S3_BUCKET"] = "example-bucket"
        with patch("scripts.teamkeys.release.repo_visibility", return_value="PRIVATE"):
            internal = scoped_environment(self.source, "publish")
        self.assertEqual(self.source["GOFLAGS"], internal["GOFLAGS"])
        self.assertEqual("example-bucket", internal["RZM_INTERNAL_S3_BUCKET"])
        for visible in ("PUBLIC", ""):
            with patch("scripts.teamkeys.release.repo_visibility", return_value=visible):
                with self.assertRaisesRegex(ValueError, "private GitHub repository"):
                    scoped_environment(self.source, "publish")


if __name__ == "__main__":
    unittest.main()
