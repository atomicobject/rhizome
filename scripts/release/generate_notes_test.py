#!/usr/bin/env python3

import importlib.util
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from notes import GenerationResult
from release_theme import ReleaseTheme
from release_types import ReleaseEvidence


ROOT = Path(__file__).resolve().parents[2]
MODULE_PATH = ROOT / "scripts" / "release" / "generate_notes.py"
SPEC = importlib.util.spec_from_file_location("generate_notes_compat", MODULE_PATH)
generate_notes = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(generate_notes)


class GenerateNotesCompatibilityTest(unittest.TestCase):
    def test_legacy_outputs_are_written_from_bounded_evidence(self):
        evidence = ReleaseEvidence(
            base_tag="v1.0.0",
            base_commit="base",
            head_commit="head",
            release_branch="main",
        )
        result = GenerationResult(
            model="gpt-5.6-luna",
            reasoning_effort="high",
            recommended_bump="minor",
            rationale="User-facing capabilities were added.",
            themes=(ReleaseTheme("- Added bounded releases.", "Added bounded releases.", ("commit:head",)),),
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = [root / name for name in ("release", "changelog", "bump", "reason")]
            with (
                patch.object(generate_notes, "repo_root", return_value=ROOT),
                patch.object(generate_notes, "collect_release_evidence", return_value=evidence),
                patch.object(generate_notes, "generate_release_notes", return_value=result),
            ):
                code = generate_notes.main(
                    [
                        "--prev-tag", "v1.0.0",
                        "--release-out", str(paths[0]),
                        "--changelog-out", str(paths[1]),
                        "--bump-out", str(paths[2]),
                        "--bump-reason-out", str(paths[3]),
                    ]
                )

            self.assertEqual(code, 0)
            self.assertEqual(paths[0].read_text(), "- Added bounded releases.\n")
            self.assertEqual(paths[1].read_text(), "- Added bounded releases.\n")
            self.assertEqual(paths[2].read_text(), "minor\n")
            self.assertEqual(paths[3].read_text(), "User-facing capabilities were added.\n")

    def test_compatibility_entrypoint_has_no_raw_diff_collector(self):
        source = MODULE_PATH.read_text(encoding="utf-8")

        self.assertNotIn("collect_diff", source)
        self.assertNotIn('"diff"', source)

    def test_help_needs_no_openai_api_key(self):
        env = os.environ.copy()
        env.pop("OPENAI_API_KEY", None)
        completed = subprocess.run(
            ["python3", str(MODULE_PATH), "--help"],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
        )

        self.assertEqual(completed.returncode, 0)
        self.assertIn("bounded release evidence", completed.stdout)


if __name__ == "__main__":
    unittest.main()
