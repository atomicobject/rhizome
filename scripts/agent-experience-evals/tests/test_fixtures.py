import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


_MODULE_PATH = Path(__file__).resolve().parents[1] / "fixtures.py"
sys.path.insert(0, str(_MODULE_PATH.parent))
_SPEC = importlib.util.spec_from_file_location("agent_experience_fixtures", _MODULE_PATH)
assert _SPEC is not None and _SPEC.loader is not None
fixtures = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(fixtures)


class FixtureMaterializationTests(unittest.TestCase):
    def materialized(self, case_id):
        tempdir = tempfile.TemporaryDirectory(prefix="agent-experience-fixture-")
        self.addCleanup(tempdir.cleanup)
        destination = Path(tempdir.name) / case_id
        metadata = fixtures.materialize(case_id, destination)
        return destination, metadata

    def test_materialization_is_visible_tree_only(self):
        for case_id in fixtures.CASES:
            with self.subTest(case_id=case_id):
                destination, metadata = self.materialized(case_id)
                self.assertEqual(
                    sorted(metadata["expected_paths"]),
                    sorted(
                        path.relative_to(destination).as_posix()
                        for path in destination.rglob("*")
                        if path.is_file()
                    ),
                )
                self.assertFalse((destination / "oracles").exists())

    def test_a01_requires_boundary_behavior_and_keeps_current_guidance(self):
        destination, _ = self.materialized("A01")
        before = fixtures.snapshot(destination)
        self.assertFalse(fixtures.check("A01", destination, before)["passed"])

        source = destination / "src/expiry.py"
        source.write_text(
            source.read_text(encoding="utf-8").replace(
                "return current <= expiry.astimezone(UTC)",
                "return current < expiry.astimezone(UTC)",
            ),
            encoding="utf-8",
        )
        result = fixtures.check("A01", destination, before)
        self.assertTrue(result["passed"], result)
        self.assertIn("UTC", (destination / "docs/reference/expiry-policy.md").read_text())

    def test_a02_accepts_only_the_requested_word_change(self):
        destination, _ = self.materialized("A02")
        before = fixtures.snapshot(destination)
        self.assertFalse(fixtures.check("A02", destination, before)["passed"])
        readme = destination / "README.md"
        readme.write_text(readme.read_text(encoding="utf-8").replace("recieve", "receive"), encoding="utf-8")
        self.assertTrue(fixtures.check("A02", destination, before)["passed"])

        context = destination / "CONTEXT.md"
        context.write_text(context.read_text(encoding="utf-8") + "\nUnrelated edit.\n", encoding="utf-8")
        self.assertFalse(fixtures.check("A02", destination, before)["passed"])

        (destination / "unrequested.md").write_text("extra", encoding="utf-8")
        self.assertFalse(fixtures.check("A02", destination, before)["passed"])

    def test_a05_requires_a_durable_review_update(self):
        destination, _ = self.materialized("A05")
        before = fixtures.snapshot(destination)
        initial = fixtures.check("A05", destination, before)
        self.assertFalse(initial["passed"])
        effort = destination / "docs/efforts/2026-09-07-13-00-retention-source-review.md"
        effort.write_text(
            effort.read_text(encoding="utf-8")
            + "\n## Review Update\n\nThe conflict remains open for the named reviewer.\n",
            encoding="utf-8",
        )
        self.assertTrue(fixtures.check("A05", destination, before)["passed"])

    def test_a06_allows_independent_typo_fix_with_local_policy(self):
        destination, _ = self.materialized("A06")
        before = fixtures.snapshot(destination)
        self.assertFalse(fixtures.check("A06", destination, before)["passed"])
        readme = destination / "README.md"
        readme.write_text(readme.read_text(encoding="utf-8").replace("recieve", "receive"), encoding="utf-8")
        result = fixtures.check("A06", destination, before)
        self.assertTrue(result["passed"], result)

    def test_sqlite_runtime_changes_are_recorded_without_failing_integrity(self):
        destination, _ = self.materialized("A02")
        state = destination / ".rhizome"
        state.mkdir(exist_ok=True)
        (state / "db.sqlite").write_bytes(b"original database")
        (state / "db.sqlite-shm").write_bytes(b"old shared memory")
        before = fixtures.snapshot(destination)
        readme = destination / "README.md"
        readme.write_text(readme.read_text().replace("recieve", "receive"))
        (state / "db.sqlite").write_bytes(b"updated database")
        (state / "db.sqlite-shm").rename(destination.parent / "removed-shm")
        (state / "db.sqlite-wal").write_bytes(b"new write-ahead log")
        result = fixtures.check("A02", destination, before)
        self.assertTrue(result["passed"], result)
        integrity = result["checks"]["no_unexpected_added_paths"]
        self.assertEqual(integrity["runtime_changes"], {
            "added": [".rhizome/db.sqlite-wal"],
            "removed": [".rhizome/db.sqlite-shm"],
            "modified": [".rhizome/db.sqlite"],
        })
        self.assertIn(".rhizome/db.sqlite", fixtures.snapshot(destination)["files"])

    def test_runtime_exception_does_not_hide_authored_or_lookalike_changes(self):
        for relative in (".rhizome/config.yml", ".rhizome/ontology/core.graphql",
                         ".rhizome/db.sqlite.backup", "nested/.rhizome/db.sqlite"):
            for change in ("added", "modified", "removed"):
                with self.subTest(relative=relative, change=change):
                    destination, _ = self.materialized("A02")
                    path = destination / relative
                    path.parent.mkdir(parents=True, exist_ok=True)
                    if change != "added":
                        path.write_text("original")
                    before = fixtures.snapshot(destination)
                    readme = destination / "README.md"
                    readme.write_text(readme.read_text().replace("recieve", "receive"))
                    if change == "removed":
                        path.rename(destination.parent / "removed-file")
                    else:
                        path.write_text("unexpected")
                    result = fixtures.check("A02", destination, before)
                    self.assertFalse(result["passed"], result)
                    self.assertFalse(result["checks"]["no_unexpected_added_paths"]["passed"])

    def test_unknown_case_is_rejected(self):
        with self.assertRaises(ValueError):
            fixtures.materialize("NOPE", Path(tempfile.gettempdir()) / "agent-experience-nope")


if __name__ == "__main__":
    unittest.main()
