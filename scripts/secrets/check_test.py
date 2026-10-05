from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check


class HistoryRangeTest(unittest.TestCase):
    def test_uses_documented_floor_when_it_is_an_ancestor(self):
        completed = subprocess.CompletedProcess([], 0)
        with patch.object(check.subprocess, "run", return_value=completed):
            self.assertEqual(f"{check.HISTORY_FLOOR}..HEAD", check.history_log_opts())

    def test_scans_all_reachable_history_when_floor_is_absent(self):
        completed = subprocess.CompletedProcess([], 128)
        with patch.object(check.subprocess, "run", return_value=completed):
            self.assertEqual("--all", check.history_log_opts())


class LocalValueTest(unittest.TestCase):
    def test_blocks_env_file_and_environment_values_without_printing_them(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            env_file = root / ".env"
            env_file.write_text("# comment\nexport VOYAGE_API_KEY='synthetic-voyage-value'\nRZM_REPO_DELEGATE=1\nRZM_INTERNAL_S3_LEGACY_SUFFIX=abcde12345\n")
            (root / "leak.go").write_text('key := "synthetic-voyage-value"')
            (root / "unlock.md").write_text("unlock: synthetic-unlock-value")
            (root / "clean.go").write_text("RZM_REPO_DELEGATE=1")
            (root / "mirror.sh").write_text("rhizome-arm64-darwin-abcde12345.tbz")
            environ = {"ATOMIC_RHIZOME_KEY": "synthetic-unlock-value", "RZM_INTERNAL_S3_BUCKET": "synthetic-bucket", "HOME": "/synthetic/home/directory"}
            (root / "bucket.md").write_text("mirror: synthetic-bucket")
            secrets = check.local_secret_values([env_file, root / "missing.env"], environ)
            findings = check.leaked_values(root, ["leak.go", "unlock.md", "clean.go", "mirror.sh", "bucket.md", "deleted.go"], secrets)
        self.assertEqual(["bucket.md (RZM_INTERNAL_S3_BUCKET)", "leak.go (VOYAGE_API_KEY)", "mirror.sh (RZM_INTERNAL_S3_LEGACY_SUFFIX)", "unlock.md (ATOMIC_RHIZOME_KEY)"], findings)
        self.assertFalse(any("synthetic" in finding for finding in findings))


if __name__ == "__main__":
    unittest.main()
