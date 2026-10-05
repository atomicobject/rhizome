from __future__ import annotations

import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import subprocess

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import clean_codex_adapter


class CleanAdapterTests(unittest.TestCase):
    def adapter(self, root: Path) -> clean_codex_adapter.Adapter:
        home = root / "home"
        entry = {"clean_home": str(home), "codex_home": str(home / ".codex"),
                 "config_sha256": "a" * 64, "execution_repo": str(root / "execution"),
                 "protected_paths": [str(root / "oracle")]}
        return clean_codex_adapter.Adapter(
            {"manifest_sha256": "b" * 64, "support_root": str(root / "support"),
             "runs": {"a02-baseline": entry}}, "c" * 64)

    def sample_run(self, root: Path) -> dict:
        return {"id": "a02-baseline", "repo": str(root / "original"),
                "binary": str(root / "rzm"), "fixture": {"README.md": "d" * 64},
                "isolation": {"codex": "/opt/homebrew/bin/codex", "denied_roots": [],
                              "denied_files": []}}

    def test_effective_run_preserves_original_identity_and_environment_is_allowlisted(self):
        with tempfile.TemporaryDirectory() as temporary, patch.dict(
                os.environ, {"OPENAI_API_KEY": "secret", "CODEX_THREAD_ID": "parent"}):
            root = Path(temporary)
            adapter = self.adapter(root)
            run = self.sample_run(root)
            effective = adapter.effective_run(run)
            self.assertEqual(run["repo"], str(root / "original"))
            self.assertEqual(effective["original_repo"], run["repo"])
            self.assertEqual(effective["repo"], str(root / "execution"))
            env = adapter.environment({}, effective)
            self.assertEqual(env["HOME"], str(root / "home"))
            self.assertEqual(env["PATH"].split(os.pathsep)[:3], [
                str(root / "execution/scripts"), "/opt/homebrew/bin",
                "/Library/Developer/CommandLineTools/usr/bin"])
            self.assertNotIn("OPENAI_API_KEY", env)
            self.assertNotIn("CODEX_THREAD_ID", env)

    def test_native_prefix_uses_evaluation_profile_and_glob_denies(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            adapter = self.adapter(root)
            run = adapter.effective_run(self.sample_run(root))
            (root / "home/.codex/skills/.system/example").mkdir(parents=True)
            (root / "home/.codex/skills/.system/example/SKILL.md").write_text("disabled")
            prefix = adapter._prefix({"runs": [self.sample_run(root)]}, run, root / "results")
            rendered = "\n".join(prefix)
            self.assertIn('default_permissions="evaluation"', rendered)
            self.assertIn('permissions.evaluation.network.enabled=false', rendered)
            self.assertIn(str((root / "oracle").resolve()) + '{,/**}"="deny"', rendered)
            self.assertIn(str((root / "results").resolve()) + '{,/**}"="deny"', rendered)
            self.assertIn("SKILL.md", rendered)
            self.assertNotIn("sandbox_mode", rendered)
            self.assertNotIn("workspace-write", rendered)
            self.assertNotIn("-s", prefix)

    def test_probe_classifies_before_sandbox_and_rejects_wrong_path_kind(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            file = root / "file"
            file.write_text("x")
            directory = root / "directory"
            directory.mkdir()
            self.assertEqual(clean_codex_adapter.probe_targets([file, directory]),
                             [f"f:{file}", f"d:{directory}"])
            fixture = root / "fixture"
            fixture.write_text("x")
            binary = root / "binary"
            binary.write_text("x")
            sentinel = root / "sentinel"
            sentinel.write_text("x")
            metadata = root / "metadata"
            metadata.write_text("x")
            process = subprocess.run(
                [sys.executable, "-c", clean_codex_adapter.ACCESS_PROBE_SCRIPT,
                 str(fixture), str(binary), str(sentinel), str(metadata), str(metadata), f"f:{directory}"],
                capture_output=True, text=True)
            self.assertNotEqual(process.returncode, 0)
            self.assertIn("IsADirectoryError", process.stdout)


if __name__ == "__main__":
    unittest.main()
