import base64
import importlib.util
import json
import os
from pathlib import Path
import random
import string
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("teamkeys_release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


PRIVATE = lambda: "PRIVATE"


class ReleaseCredentialsTest(unittest.TestCase):
    def environment(self, internal=True):
        env = {name: os.environ[name] for name in ("PATH", "HOME", "USERPROFILE", "SystemRoot", "TMPDIR", "TEMP", "TMP") if name in os.environ}
        env.update(ATOMIC_RHIZOME_KEY=base64.b64encode(bytes(32)).decode(), VOYAGE_API_KEY="synthetic-voyage", TYPESAFE_API_KEY="synthetic-typesafe", GITHUB_TOKEN="synthetic-github")
        if internal:
            env["RZM_INTERNAL_S3_BUCKET"] = "example-bucket"
        env.pop("GOFLAGS", None)
        return env

    def test_public_release_never_generates_or_passes_team_keys(self):
        env = self.environment(internal=False)
        runner = Mock(return_value=0)
        with patch.object(release.subprocess, "run") as generate:
            self.assertEqual(0, release.run_release(["build"], env, runner=runner, visibility=lambda: "PUBLIC"))
            generate.assert_not_called()
        child = runner.call_args.kwargs["env"]
        self.assertFalse(set(release.BUNDLE_INPUTS) & child.keys())
        self.assertNotIn("GOFLAGS", child)
        self.assertEqual("off", child["GOENV"])
        self.assertEqual("synthetic-github", child["GITHUB_TOKEN"])

    def test_public_release_rejects_overlays_and_nonempty_source(self):
        runner = Mock(return_value=0)
        with self.assertRaisesRegex(ValueError, "compiler overlay"):
            release.run_release(["build"], {"GOFLAGS": "-overlay=/synthetic/overlay.json"}, runner=runner, visibility=lambda: "PUBLIC")
        with patch.object(Path, "read_text", return_value='package teamkeys\nvar encryptedKeys = "synthetic-bundle"\n'):
            with self.assertRaisesRegex(ValueError, "must be empty"):
                release.run_release(["build"], {}, runner=runner, visibility=lambda: "PUBLIC")
        runner.assert_not_called()

    def test_public_release_removes_stale_mirror_settings(self):
        runner = Mock(return_value=0)
        release.run_release(["build"], {"RZM_INTERNAL_S3_LEGACY_SUFFIX": "abcde12345", "GOENV": "/synthetic/goenv"}, runner=runner, visibility=lambda: "PUBLIC")
        self.assertEqual({"GOENV": "off"}, runner.call_args.kwargs["env"])

    def test_direct_public_shaped_release_refuses_a_private_repository(self):
        runner = Mock(return_value=0)
        with self.assertRaisesRegex(ValueError, "must be internal"):
            release.run_release(["goreleaser", "release"], self.environment(internal=False), runner=runner, visibility=PRIVATE)
        runner.assert_not_called()

    def test_internal_release_refuses_a_public_repository(self):
        runner = Mock(return_value=0)
        for visibility in ("PUBLIC", "INTERNAL", ""):
            with self.subTest(visibility=visibility), self.assertRaisesRegex(ValueError, "must be private"):
                release.run_release(["build"], self.environment(), runner=runner, visibility=lambda: visibility)
        runner.assert_not_called()

    def test_overlay_build_decrypts_synthetic_keys_and_leaves_source_unchanged(self):
        source = release.ROOT / "pkg/teamkeys/keys_encrypted.go"
        before = source.read_bytes()
        env = self.environment()
        with tempfile.TemporaryDirectory() as directory:
            probe = Path(directory) / "main.go"
            probe.write_text('package main\nimport ("os"; "github.com/atomicobject/rhizome/pkg/teamkeys")\nfunc main() { if os.Getenv("VOYAGE_API_KEY") != "" || os.Getenv("ATOMIC_RHIZOME_KEY") != "" { os.Exit(2) }; os.Setenv("ATOMIC_RHIZOME_KEY", ' + json.dumps(env["ATOMIC_RHIZOME_KEY"]) + '); if teamkeys.VoyageKey() != "synthetic-voyage" || teamkeys.TypeSafeKey() != "synthetic-typesafe" { os.Exit(3) } }')
            self.assertEqual(0, release.run_release(["go", "run", "-mod=vendor", str(probe)], env, visibility=PRIVATE))
            binary = Path(directory) / ("probe.exe" if sys.platform == "win32" else "probe")
            build = ["go", "build", "-mod=vendor", "-ldflags=-s -w", "-o", str(binary), str(probe)]
            self.assertEqual(0, release.run_release(build, env, visibility=PRIVATE))
            with self.assertRaisesRegex(ValueError, "internal bundle"):
                release.verify_artifact(binary, {})
            subprocess.run(build, env=release.public_environment(env), check=True)
            release.verify_artifact(binary, {})
        self.assertEqual(before, source.read_bytes())

    def test_artifact_check_rejects_mirror_urls_and_scanner_failures(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory) / "installer.sh"
            artifact.write_bytes(b"https://example-bucket.s3.amazonaws.com/releases")
            with self.assertRaisesRegex(ValueError, "mirror URL"):
                release.verify_artifact(artifact, {})
            artifact.write_bytes(b"clean artifact")
            with patch.object(release.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, b"synthetic-secret", b"synthetic-secret")):
                with self.assertRaisesRegex(ValueError, "credential scanning") as error:
                    release.verify_artifact(artifact, {})
                self.assertNotIn("synthetic-secret", str(error.exception))
            with patch.object(release, "repo_visibility", return_value="PUBLIC"):
                with self.assertRaisesRegex(ValueError, "private GitHub repository"):
                    release.verify_artifact(artifact, {"RZM_INTERNAL_S3_BUCKET": "example-bucket"})

    def test_scanner_rejects_plaintext_credentials_inside_binary_content(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory) / "probe"
            token = ''.join(random.Random(42).choices(string.ascii_letters + string.digits, k=36))
            artifact.write_bytes(b"\x7fELF\x00binary\x00" + b"ghp_" + token.encode() + b"\x00")
            with self.assertRaisesRegex(ValueError, "credential scanning") as error:
                release.verify_artifact(artifact, {})
            self.assertNotIn(token, str(error.exception))

    def test_failed_build_cleans_overlay(self):
        paths = []
        def fail(command, *, env):
            overlay = Path(env["GOFLAGS"].split("=", 1)[1].strip("'"))
            paths.append(overlay)
            self.assertTrue(overlay.exists())
            return 7
        self.assertEqual(7, release.run_release(["build"], self.environment(), runner=fail, visibility=PRIVATE))
        self.assertFalse(paths[0].parent.exists())

    def test_missing_key_cannot_start_build(self):
        env = self.environment()
        del env["TYPESAFE_API_KEY"]
        runner = Mock(return_value=0)
        with self.assertRaisesRegex(ValueError, "credential generation failed"):
            release.run_release(["build"], env, runner=runner, visibility=PRIVATE)
        runner.assert_not_called()

    def test_check_validates_keys_without_building(self):
        env = self.environment()
        del env["ATOMIC_RHIZOME_KEY"]
        with patch.object(release, "repo_visibility", return_value="PRIVATE"):
            with self.assertRaisesRegex(ValueError, "credential generation failed"):
                release.check(env)
            release.check(self.environment())

    def test_check_requires_mode_to_match_repository_visibility(self):
        with self.assertRaisesRegex(ValueError, "must be internal"):
            release.check(self.environment(internal=False), visibility=PRIVATE)
        with self.assertRaisesRegex(ValueError, "must be internal"):
            release.check(self.environment(internal=False), visibility=lambda: "")
        with self.assertRaisesRegex(ValueError, "must be private"):
            release.check(self.environment(), visibility=lambda: "PUBLIC")
        release.check(self.environment(internal=False), visibility=lambda: "PUBLIC")
        release.check(self.environment(), visibility=PRIVATE)


if __name__ == "__main__":
    unittest.main()
