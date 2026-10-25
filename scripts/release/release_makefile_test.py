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
        self.assertIn("release-plan:\n\tpython3 scripts/release/release_cli.py plan", self.makefile)
        self.assertIn("release-dry:\n\tpython3 scripts/release/release_cli.py dry-run", self.makefile)

    def test_direct_release_bundles_through_the_clean_publish_wrapper(self):
        release_target = self.makefile.split("\nrelease:\n", 1)[1].split("\nrelease-plan:\n", 1)[0]
        self.assertIn("python3 scripts/teamkeys/release.py python3 scripts/release/run_clean.py publish -- goreleaser release --clean", release_target)

    def test_resumable_phase_targets_are_exposed(self):
        for target, command in (
            ("release-build", "build"),
            ("release-apply", "apply"),
            ("release-publish", "publish"),
            ("release-resume", "resume"),
        ):
            with self.subTest(target=target):
                self.assertIn(f"{target}:", self.makefile)
                self.assertIn(f"release_cli.py {command}", self.makefile)

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
