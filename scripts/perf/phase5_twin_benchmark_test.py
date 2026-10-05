import runpy
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "perf"))
MODULE = runpy.run_path(str(ROOT / "scripts/perf/phase5_twin_benchmark.py"))


class Phase5TwinBenchmarkTest(unittest.TestCase):
    def test_canonical_snapshot_does_not_create_wal_sidecars(self):
        with tempfile.TemporaryDirectory() as directory:
            database = Path(directory) / "db.sqlite"
            connection = sqlite3.connect(database)
            connection.execute("PRAGMA journal_mode=WAL")
            connection.execute("CREATE TABLE facts(value TEXT)")
            connection.execute("INSERT INTO facts VALUES ('stable')")
            connection.commit()
            connection.execute("PRAGMA wal_checkpoint(TRUNCATE)")
            connection.close()

            for suffix in ("-wal", "-shm"):
                sidecar = Path(f"{database}{suffix}")
                if sidecar.exists():
                    sidecar.rename(Path(f"{sidecar}.saved"))

            self.assertFalse(Path(f"{database}-wal").exists())
            self.assertFalse(Path(f"{database}-shm").exists())
            snapshot = MODULE["canonical_sqlite_snapshot"](database, set())
            self.assertEqual(snapshot["tables"]["facts"]["rows"], [["stable"]])
            self.assertFalse(Path(f"{database}-wal").exists())
            self.assertFalse(Path(f"{database}-shm").exists())

    def test_canonical_snapshot_reads_committed_wal_pages(self):
        with tempfile.TemporaryDirectory() as directory:
            database = Path(directory) / "db.sqlite"
            connection = sqlite3.connect(database)
            connection.execute("PRAGMA journal_mode=WAL")
            connection.execute("PRAGMA wal_autocheckpoint=0")
            connection.execute("CREATE TABLE facts(value TEXT)")
            connection.execute("INSERT INTO facts VALUES ('from-wal')")
            connection.commit()

            snapshot = MODULE["canonical_sqlite_snapshot"](database, set())
            self.assertEqual(snapshot["tables"]["facts"]["rows"], [["from-wal"]])
            connection.close()

    def test_fingerprint_delta_ignores_absolute_source_paths(self):
        delta = MODULE["fingerprint_delta"](
            {"db": {"present": True, "path": "/templates/db.sqlite", "sha256": "same"}},
            {"db": {"present": True, "path": "/samples/db.sqlite", "sha256": "same"}},
        )
        self.assertEqual(delta, {"created": [], "deleted": [], "changed": []})

    def test_phase5_cells_cover_human_agent_twins_and_write_capable_graph_path(self):
        cells = {cell.name: cell for cell in MODULE["phase5_cells"]("fixture")}
        self.assertEqual(
            set(cells),
            {
                "rootSearch", "ontologyQueryRoot", "ontologyQueryAgent",
                "queryRecipeRoot", "queryRecipeAgent", "viewRoot", "viewAgent",
                "semanticSession", "graphFileContextNever", "graphVaultContext",
            },
        )
        self.assertIn("never", cells["graphFileContextNever"].command)
        self.assertTrue(cells["ontologyQueryRoot"].json_output)
        self.assertFalse(cells["rootSearch"].json_output)
        self.assertFalse(cells["rootSearch"].uses_vault_flag)
        self.assertNotIn("--json", cells["viewRoot"].command)

    def test_phase5_cells_use_real_rhizome_ids_without_mutating_a_source(self):
        cells = {cell.name: cell for cell in MODULE["phase5_cells"]("rhizome")}
        self.assertIn("all-action-items", cells["queryRecipeRoot"].command)
        self.assertIn("action-items.open", cells["viewAgent"].command)

    def test_graph_apply_fixture_uses_an_unaddressed_embedded_node(self):
        prepare = MODULE["prepare_graph_apply_fixture"]
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            schema = source / ".rhizome/ontology/schema.graphql"
            schema.parent.mkdir(parents=True)
            schema.write_text("type Existing { id: ID }\n")
            target = prepare(source, "rhizome")
            self.assertEqual(target["notePath"], "docs/phase5-spec.md")
            self.assertEqual((source / "src/phase5_apply.py").read_text(), "# WHY: implements [[phase5-spec#Story A]]\ndef phase5_apply():\n    return None\n")
            note = (source / target["notePath"]).read_text()
            self.assertIn("### Story A", note)
            self.assertNotIn("^phase5applystory-story-a-", note)

    def test_source_kind_distinguishes_rhizome_and_fixture_recipe_registries(self):
        source_kind = MODULE["source_kind"]
        self.assertEqual(source_kind(ROOT), "rhizome")
        self.assertEqual(source_kind(ROOT / "testdata/integration/python-app/vault"), "fixture")

    def test_deterministic_provider_overlay_changes_only_a_disposable_rhizome_config(self):
        overlay = MODULE["apply_deterministic_provider_overlay"]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / ".rhizome" / "config.yml"
            config.parent.mkdir()
            config.write_text(
                "noteEmbeddings:\n  provider: voyage\n  model: voyage-4-lite\n"
                "  endpoint: https://example.test\ncodeEmbeddings:\n  provider: voyage\n"
                "  model: voyage-4-lite\n  endpoint: https://example.test\n"
            )
            self.assertTrue(overlay(root, "rhizome"))
            self.assertEqual(
                config.read_text(),
                "noteEmbeddings:\n  provider: test\n  dimensions: 256\n"
                "codeEmbeddings:\n  provider: test\n  dimensions: 256\n",
            )
            self.assertFalse(overlay(root, "fixture"))

    def test_normalize_text_removes_only_sample_state_path_and_volatile_json(self):
        normal = MODULE["normalize_text"]
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary) / "vault"
            state.mkdir()
            template = Path(temporary) / "template"
            rendered = normal(
                '{"path":"' + str(template) + '/A.md","durationMs":5,"updatedAt":9,"definitionFingerprint":"x"}',
                (state, template), parse_json=True,
            )
        self.assertEqual(rendered, '{"definitionFingerprint":"x","path":"$VAULT/A.md"}')

    def test_normalize_text_removes_log_timestamp_only(self):
        normal = MODULE["normalize_text"]
        self.assertEqual(
            normal("2026/08/06 16:10:11 compression: failed (Forbidden)\n", (), parse_json=False),
            "$TIMESTAMP compression: failed (Forbidden)\n",
        )

    def test_mutation_reports_created_deleted_and_changed_paths(self):
        mutation = MODULE["mutation"]
        self.assertEqual(
            mutation({"same": "a", "changed": "a", "deleted": "a"}, {"same": "a", "changed": "b", "created": "b"}),
            {"created": ["created"], "deleted": ["deleted"], "changed": ["changed"]},
        )

    def test_filesystem_mutation_uses_sqlite_fingerprint_delta_for_short_lived_sidecars(self):
        filesystem_mutation = MODULE["filesystem_mutation"]
        self.assertEqual(
            filesystem_mutation(
                {"unchanged": "a", ".rhizome/db.sqlite": "before"},
                {"unchanged": "a", ".rhizome/db.sqlite": "after", "note.md": "new"},
                {"created": ["wal", "shm"], "deleted": [], "changed": ["db"]},
            ),
            {
                "created": [".rhizome/db.sqlite-shm", ".rhizome/db.sqlite-wal", "note.md"],
                "deleted": [],
                "changed": [".rhizome/db.sqlite"],
            },
        )

    def test_fingerprint_identity_ignores_only_state_path(self):
        identity = MODULE["fingerprint_content_identity"]
        self.assertEqual(
            identity({"db": {"present": True, "path": "/a/db", "sha256": "x"}}),
            {"db": {"present": True, "sha256": "x"}},
        )

    def test_restore_state_replaces_a_disposable_sample_with_template_bytes(self):
        restore = MODULE["restore_state"]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            template = root / "template"
            state = root / "state"
            template.mkdir()
            (template / "value.txt").write_text("pristine")
            state.mkdir()
            (state / "value.txt").write_text("changed")
            restore(template, state)
            self.assertEqual((state / "value.txt").read_text(), "pristine")

    def test_rhizome_projection_rejects_a_tracked_excluded_root_and_preserves_required_inputs(self):
        create_projection = MODULE["create_rhizome_projection"]
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "source"
            source.mkdir()
            subprocess.run(["/usr/bin/git", "init", "-q"], cwd=source, check=True)
            for path in (".rhizome/config.yml", ".rhizome/ignore", ".rhizome/ontology/schema.graphql", ".rhizome/query-recipes/a.yaml", ".rhizome/views/a.yaml", ".tmp/keep.txt"):
                file = source / path
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("test\n")
            subprocess.run(["/usr/bin/git", "add", "."], cwd=source, check=True)
            projection = Path(temporary) / "projection"
            result = create_projection(source, projection)
            self.assertTrue((projection / ".rhizome/config.yml").is_file())
            self.assertTrue((projection / ".tmp/keep.txt").is_file())
            self.assertEqual(result["retainedTrackedRoots"][".tmp"], 1)

    def test_rhizome_projection_refuses_to_exclude_tracked_content(self):
        create_projection = MODULE["create_rhizome_projection"]
        excluded_roots = MODULE["WORKTREE_PROJECTION_EXCLUDED_ROOTS"]
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "source"
            source.mkdir()
            subprocess.run(["/usr/bin/git", "init", "-q"], cwd=source, check=True)
            for path in (".rhizome/config.yml", ".rhizome/ignore", ".rhizome/ontology/schema.graphql", ".rhizome/query-recipes/a.yaml", ".rhizome/views/a.yaml", "web/node_modules/tracked.js"):
                file = source / path
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("test\n")
            subprocess.run(["/usr/bin/git", "add", "."], cwd=source, check=True)
            MODULE["WORKTREE_PROJECTION_EXCLUDED_ROOTS"] = ("web/node_modules",)
            try:
                with self.assertRaisesRegex(RuntimeError, "cannot exclude tracked root"):
                    create_projection(source, Path(temporary) / "projection")
            finally:
                MODULE["WORKTREE_PROJECTION_EXCLUDED_ROOTS"] = excluded_roots

    def test_rhizome_projection_does_not_copy_untracked_excluded_content(self):
        create_projection = MODULE["create_rhizome_projection"]
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "source"
            source.mkdir()
            subprocess.run(["/usr/bin/git", "init", "-q"], cwd=source, check=True)
            for path in (".rhizome/config.yml", ".rhizome/ignore", ".rhizome/ontology/schema.graphql", ".rhizome/query-recipes/a.yaml", ".rhizome/views/a.yaml"):
                file = source / path
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("test\n")
            subprocess.run(["/usr/bin/git", "add", "."], cwd=source, check=True)
            excluded = source / "cache/untracked.bin"
            excluded.parent.mkdir()
            excluded.write_bytes(b"large dependency content")

            projection = Path(temporary) / "projection"
            result = MODULE["phase5_twin_support"].create_rhizome_projection(
                source, projection, ("cache",)
            )
            self.assertFalse((projection / "cache").exists())
            self.assertEqual(result["excludedRoots"]["cache"], 1)

    def test_summary_reports_variance_and_full_channel_stability(self):
        sample = {
            "elapsedMs": 10.0,
            "exitCode": 0,
            "normalizedStdoutSha256": "stdout",
            "normalizedStderrSha256": "stderr",
            "fullStdoutSha256": "full-out",
            "fullStderrSha256": "full-err",
            "operationEnvelope": [],
            "operationDiagnosticsAvailable": False,
            "indexFingerprintsBefore": {},
            "indexFingerprintsAfter": {},
            "filesystemMutation": {"created": [], "deleted": [], "changed": []},
            "sqliteSidecarDelta": {"created": [], "deleted": [], "changed": []},
            "canonicalLogicalSha256": "canonical",
        }
        summary = MODULE["summarize"]([sample, {**sample, "elapsedMs": 20.0}])
        self.assertEqual(summary["medianMs"], 15.0)
        self.assertEqual(summary["p95Ms"], 20.0)
        self.assertEqual(summary["varianceMs2"], 25.0)
        self.assertTrue(summary["fullOutputEquivalent"])
        self.assertTrue(summary["filesystemMutationEquivalent"])

    def test_sqlite_snapshot_diff_rejects_non_timestamp_value_changes(self):
        diff = MODULE["sqlite_snapshot_diff_columns"]
        base = {"schema": [], "tables": {"notes": {"columns": ["updated_at", "title"], "rows": [[1, "same"]]}}}
        timestamp = {"schema": [], "tables": {"notes": {"columns": ["updated_at", "title"], "rows": [[2, "same"]]}}}
        changed = {"schema": [], "tables": {"notes": {"columns": ["updated_at", "title"], "rows": [[1, "different"]]}}}
        self.assertEqual(diff(base, timestamp), {("notes", "updated_at")})
        self.assertEqual(diff(base, changed), {("notes", "title")})

    def test_parity_uses_canonical_content_and_sidecar_delta_not_raw_after_bytes(self):
        identity = MODULE["_parity_identity"]
        base = {
            "exitCodes": [0], "normalizedStdoutSha256s": ["out"], "normalizedStderrSha256s": ["err"],
            "operationEnvelopes": [[]], "filesystemMutations": [{"created": [], "deleted": [], "changed": []}],
            "indexFingerprintsBefore": [{"db": {"present": True, "path": "/same", "sha256": "pre"}}],
            "indexFingerprintsAfter": [{"db": {"present": True, "path": "/same", "sha256": "raw-a"}}],
            "sqliteSidecarDeltas": [{"created": [], "deleted": [], "changed": ["db"]}],
            "canonicalLogicalSha256s": ["canonical"],
        }
        raw_changed = {**base, "indexFingerprintsAfter": [{"db": {"present": True, "path": "/same", "sha256": "raw-b"}}]}
        self.assertEqual(identity(base), identity(raw_changed))
        self.assertNotEqual(identity(base), identity({**raw_changed, "sqliteSidecarDeltas": [{"created": ["wal"], "deleted": [], "changed": ["db"]}]}))
        self.assertNotEqual(identity(base), identity({**raw_changed, "canonicalLogicalSha256s": ["different"]}))


if __name__ == "__main__":
    unittest.main()
