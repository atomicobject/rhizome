#!/usr/bin/env python3

import unittest
import os
from pathlib import Path
import subprocess
import tempfile


MAKEFILE = Path(__file__).resolve().parents[2] / "Makefile"


class ReleaseMakeTargetsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.makefile = MAKEFILE.read_text(encoding="utf-8")

    def test_plan_and_dry_run_use_release_orchestrator(self):
        with tempfile.TemporaryDirectory() as directory:
            stub = Path(directory) / "python3"
            stub.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\n', encoding="utf-8")
            stub.chmod(0o755)
            env = {**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"]}
            for target, phase in (("release-plan", "plan"), ("release-dry", "dry-run")):
                with self.subTest(target=target):
                    result = subprocess.run(
                        ["make", "--no-print-directory", "-s", "-f", str(MAKEFILE), target],
                        cwd=directory, env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout.splitlines(), ["scripts/release/release_cli.py", phase])

    def test_direct_make_release_executes_gated_publisher(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "git").write_text('#!/bin/sh\nprintf "v1.2.3\\n"\n', encoding="utf-8")
            (root / "python3").write_text('#!/bin/sh\nprintf "%s\\n" "$@"\n', encoding="utf-8")
            for name in ("git", "python3"):
                (root / name).chmod(0o755)
            env = {**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"]}
            result = subprocess.run(["make", "--no-print-directory", "-s", "-f", str(MAKEFILE), "release"],
                                    cwd=directory, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.splitlines(), ["scripts/release/release_publish.py"])

    def test_internal_mirror_location_never_appears_in_source(self):
        for target in ("release-s3:", "release-s3-check:", "release-s3-dry:"):
            self.assertIn(target, self.makefile)
        self.assertNotIn("RZM_INTERNAL_S3_BUCKET", self.makefile)
        self.assertNotIn("s3.amazonaws.com", self.makefile)

    def test_phase_targets_forward_an_empty_first_release_base_and_require_head(self):
        with tempfile.TemporaryDirectory() as directory:
            stub = Path(directory) / "python3"
            stub.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\n', encoding="utf-8")
            stub.chmod(0o755)
            env = {**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"]}
            for phase in ("build", "apply", "publish", "resume"):
                command = ["make", "--no-print-directory", "-s", "-f", str(MAKEFILE),
                           "release-" + phase, "RELEASE_BASE=", "ACCEPT_DEGRADED_EVIDENCE="]
                with self.subTest(phase=phase):
                    result = subprocess.run(command + ["RELEASE_HEAD=abc123"], cwd=directory,
                                            env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout.splitlines(), ["scripts/release/release_cli.py",
                                     phase, "--base", "", "--head", "abc123"])
                    missing = subprocess.run(command + ["RELEASE_HEAD="], cwd=directory,
                                             env=env, capture_output=True, text=True)
                    self.assertNotEqual(missing.returncode, 0)
                    self.assertIn("set RELEASE_HEAD", missing.stderr)


if __name__ == "__main__":
    unittest.main()
