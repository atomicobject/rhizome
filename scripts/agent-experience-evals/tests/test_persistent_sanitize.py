from pathlib import Path
import json
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import persistent_sanitize


class PersistentSanitizeTests(unittest.TestCase):
    def inputs(self, root: Path):
        results = root / "results"
        results.mkdir()
        manifest = root / "manifest.json"
        environment = root / "environment.json"
        manifest.write_text(json.dumps({"runs": [{
            "id": "p01-direct", "repo": str(root / "prepared"), "support": str(root / "support"),
            "binary": str(root / "rzm"), "build_receipt": str(root / "build.json"),
        }]}))
        environment.write_text(json.dumps({"runs": {"p01-direct": {
            "clean_home": str(root / "home"), "codex_home": str(root / "home/.codex"),
            "execution_repo": str(root / "execution"),
        }}}))
        return results, manifest, environment

    def test_replaces_local_paths_and_refuses_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            results, manifest, environment = self.inputs(root)
            (results / "event.jsonl").write_text(str(root / "execution/file.py"))
            output = root / "sanitized"
            persistent_sanitize.sanitize(results, output, manifest, environment)
            self.assertEqual((output / "event.jsonl").read_text(), "<EXECUTION_P01-DIRECT>/file.py")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            results, manifest, environment = self.inputs(root)
            (results / "event.jsonl").write_text('{"access_token":"secret"}')
            with self.assertRaises(ValueError):
                persistent_sanitize.sanitize(results, root / "sanitized", manifest, environment)


if __name__ == "__main__":
    unittest.main()
