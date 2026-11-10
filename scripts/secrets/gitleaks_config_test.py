import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


@unittest.skipUnless(shutil.which("gitleaks"), "gitleaks is required")
class GeneratedFingerprintTest(unittest.TestCase):
    def scan(self, path, content):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source"
            target = source / path
            target.parent.mkdir(parents=True)
            target.write_text(content)
            report = root / "report.json"
            subprocess.run(
                ["gitleaks", "dir", "--redact=100", "--no-banner",
                 "--ignore-gitleaks-allow", "--exit-code=0",
                 "--config", str(ROOT / ".gitleaks.toml"),
                 "--report-format=json", "--report-path", str(report), str(source)],
                check=True, capture_output=True, text=True,
            )
            return json.loads(report.read_text())

    def test_generated_skill_fingerprints_are_not_api_keys(self):
        digest = hashlib.sha256(b"synthetic kit API reference 5\n").hexdigest()[:16]
        rows = "written:\n" + "".join(
            f"  .{harness}/skills/custom-views/references/kit-api.md: {digest}\n"
            for harness in ("agents", "claude")
        )
        self.assertEqual([], self.scan(".rhizome/generated-files.yml", rows))
        self.assertTrue(self.scan("notes/example.yml", rows))

    def test_credentials_in_the_manifest_still_fail(self):
        digest = hashlib.sha256(b"synthetic kit API reference 5\n").hexdigest()
        for entry in (
            f"api_key: {digest[:16]}",
            f"api_key: {digest[:32]}",
            f".agents/skills/custom-views/references/kit-api.md: {digest[:32]}",
        ):
            with self.subTest(entry=entry.split(":", 1)[0]):
                findings = self.scan(".rhizome/generated-files.yml", f"written:\n  {entry}\n")
                self.assertTrue(any(row["RuleID"] == "generic-api-key" for row in findings))


if __name__ == "__main__":
    unittest.main()
