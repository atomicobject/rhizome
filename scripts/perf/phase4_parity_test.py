import json
import runpy
import sqlite3
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "perf"))
MODULE = runpy.run_path(str(ROOT / "scripts/perf/phase4_parity.py"))
PHASE4_CELLS = MODULE["phase4_cells"]
PHASE4_CELL = MODULE["Phase4Cell"]
RUN_MATRIX = MODULE["run_phase4_matrix"]
VALIDATE_FIXTURE_STATES = MODULE["validate_fixture_states"]
DATABASE_FACTS = MODULE["_database_facts"]
LEGACY_NOTE_VECTOR_PROOF = MODULE["_legacy_note_vector_proof"]
NORMALIZE = MODULE["_normalize"]
PARSE_CHANNEL = MODULE["_parse_channel"]
HARNESS = ROOT / "scripts" / "perf" / "phase4_parity.py"


def fixture_states(root: Path) -> dict[str, Path]:
    fixtures = {}
    for state in ("current", "missing", "stale", "incompatible", "provider-unavailable"):
        fixture = root / state
        rhizome = fixture / ".rhizome"
        rhizome.mkdir(parents=True)
        provider = "ollama" if state == "provider-unavailable" else "test"
        (rhizome / "config.yml").write_text(
            f"noteEmbeddings:\n  provider: {provider}\ncodeEmbeddings:\n  provider: {provider}\n"
        )
        (fixture / "fixture.txt").write_text("shared corpus\n")
        if state != "missing":
            database = sqlite3.connect(rhizome / "db.sqlite")
            schema = 60 if state == "incompatible" else 59
            version = "stale-v0" if state == "stale" else "v1.9.0"
            database.executescript(
                "CREATE TABLE rzm_migration_state(domain TEXT, version INTEGER);"
                "CREATE TABLE index_metadata(key TEXT, value TEXT);"
                "CREATE TABLE intel_chunks(id INTEGER PRIMARY KEY);"
                "CREATE TABLE intel_embeddings(chunk_row_id INTEGER PRIMARY KEY, dimensions INTEGER NOT NULL);"
                "CREATE TABLE intel_embeddings_vec_d256(chunk_id INTEGER PRIMARY KEY);"
                "CREATE TABLE intel_doc_sections(id INTEGER PRIMARY KEY);"
                "CREATE TABLE code_index_meta(id INTEGER PRIMARY KEY, provider TEXT, model TEXT, dimensions INTEGER, schema_version INTEGER);"
                "CREATE TABLE emb_index_meta(id INTEGER PRIMARY KEY, provider TEXT, model TEXT, dimensions INTEGER, schema_version INTEGER);"
                "CREATE TABLE mcp_sessions(session_id TEXT PRIMARY KEY, last_seen_at TEXT);"
                "CREATE TABLE mcp_session_items(session_id TEXT, item_key TEXT, PRIMARY KEY(session_id, item_key));"
                "CREATE TABLE mcp_session_item_reservations(session_id TEXT, item_key TEXT, reservation_id TEXT, PRIMARY KEY(session_id, item_key));"
                "CREATE TABLE mcp_session_maintenance(id INTEGER PRIMARY KEY, maintained_at TEXT);"
            )
            database.execute("INSERT INTO rzm_migration_state VALUES ('intel', ?)", (schema,))
            database.executemany(
                "INSERT INTO index_metadata VALUES (?, ?)",
                (("indexer_version", version), ("scope_config_hash", "scope-v1")),
            )
            database.execute("INSERT INTO intel_chunks VALUES (1)")
            database.execute("INSERT INTO intel_embeddings VALUES (1, 256)")
            database.execute("INSERT INTO intel_embeddings_vec_d256 VALUES (1)")
            database.execute("INSERT INTO intel_doc_sections VALUES (1)")
            database.execute("INSERT INTO code_index_meta VALUES (1, 'test', 'deterministic', 256, 12)")
            database.execute("INSERT INTO emb_index_meta VALUES (1, '', '', 0, 7)")
            database.commit()
            database.close()
        fixtures[state] = fixture
    return fixtures


class Phase4ParityTest(unittest.TestCase):
    def test_matrix_covers_phase4_operations_variants_and_twins(self):
        cells = {cell.name: cell for cell in PHASE4_CELLS()}
        required = {
            "files-basic", "files-graph-max-depth", "tags", "properties",
            "vault-health", "community-list", "vault-context-vault-profile",
            "vault-context-code-profile", "vault-context-normalized-options",
            "find-connections-note", "find-connections-text", "node-link-plan",
            "node-link-apply-refusal", "rename-heading-plan",
            "rename-heading-apply-refusal", "graph-file-context-plan",
            "graph-file-context-apply", "root-vault-context-vault-profile",
        }
        required.update(f"report-{op}" for op in (
            "doc_coverage", "complexity", "hotspots", "rationale_attention",
            "relatedness", "code_similarity",
        ))
        self.assertTrue(required <= set(cells))
        self.assertIn("--max-depth", cells["files-graph-max-depth"].command)
        self.assertIn("staleNotes", cells["vault-health"].command)
        self.assertIn("deadEnds", cells["vault-health"].command)
        self.assertEqual(cells["find-connections-note"].states, (
            "current", "missing", "stale", "incompatible", "provider-unavailable",
        ))
        self.assertIn("--ensure", cells["node-link-apply-refusal"].command)
        self.assertEqual(cells["files-basic"].index_mutation_policy, "forbid")
        self.assertEqual(cells["properties"].index_mutation_policy, "query-only")
        self.assertEqual(cells["vault-context-vault-profile"].index_mutation_policy, "session-dedupe")
        self.assertEqual(cells["find-connections-text"].index_mutation_policy, "query-only")

    def test_normalization_removes_documented_time_only_fields_without_reordering(self):
        normalized = NORMALIZE({
            "first": 1,
            "generatedAt": "2026-08-06T00:00:00Z",
            "items": [{"analyzedAt": "now", "stable": "kept"}],
            "latestAgeDays": 9,
            "last": 2,
        })
        self.assertEqual(list(normalized), ["first", "items", "last"])
        self.assertEqual(normalized["items"], [{"stable": "kept"}])

    def test_normalization_replaces_only_fixture_copy_vault_header(self):
        root = Path("/tmp/phase4-fixture/3")
        normalized = NORMALIZE({
            "text": "# Context\n- vault: 3\n- vault: named-vault\n- vault: 30\n",
        }, root)
        self.assertEqual(
            normalized["text"],
            "# Context\n- vault: <fixture>\n- vault: named-vault\n- vault: 30\n",
        )

    def test_plain_text_normalization_replaces_fixture_path(self):
        root = Path("/tmp/phase4-fixture")
        _, normalized, _ = PARSE_CHANNEL(f"vault: {root}\\n", root)
        self.assertEqual(normalized, "vault: <fixture>\\n")

    def test_plain_text_normalization_replaces_only_go_log_timestamp_prefix(self):
        root = Path("/tmp/phase4-fixture")
        text = (
            "2026/08/06 12:34:56 compression: failed for vault "+ str(root) + "\\n"
            "2026-08-06 12:34:56 remains exact\\n"
        )
        _, normalized, _ = PARSE_CHANNEL(text, root)
        self.assertEqual(
            normalized,
            "<go-log-time> compression: failed for vault <fixture>\\n"
            "2026-08-06 12:34:56 remains exact\\n",
        )

    def test_runs_each_binary_state_command_sample_in_a_fresh_copy(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "rzm"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "import json\n"
                "from pathlib import Path\n"
                "Path('ran').write_text('yes')\n"
                "print(json.dumps({'items':['first','second'], 'root':str(Path.cwd()), 'warnings':[{'code':'index_missing','remediation':'rzm index'}]}))\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures, samples=2,
                cells=(PHASE4_CELL("cell", ("agent", "files"), ("current",)),),
            )
            state = report["cells"]["cell"]["current"]
            self.assertTrue(report["equivalent"])
            self.assertEqual(report["cellIsolation"], "fresh-copy-per-binary-state-command-sample")
            self.assertEqual(report["fixtureStates"]["incompatible"]["database"]["intelSchemaVersion"], 60)
            self.assertEqual(len(state["baseline"]), 2)
            self.assertEqual(len(state["current"]), 2)
            for record in state["baseline"] + state["current"]:
                self.assertTrue(record["indexUnchanged"])
                self.assertEqual(record["warningRemediation"][0]["code"], "index_missing")
                self.assertEqual(record["jsonShapes"][0]["fields"]["items"]["list"], ["str", "str"])
                self.assertIn("<fixture>", record["normalizedStdout"])
                self.assertEqual(record["normalizedStderr"], "")
            self.assertFalse(any((fixture / "ran").exists() for fixture in fixtures.values()))

    def test_reports_exact_contract_difference_without_hiding_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            baseline = root / "baseline-bin"
            current = root / "current-bin"
            baseline.write_text("#!/bin/sh\nprintf '%s\\n' '{\"result\":\"baseline\"}'\n")
            current.write_text("#!/bin/sh\nprintf '%s\\n' '{\"result\":\"current\"}'\n")
            for binary in (baseline, current):
                binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(str(baseline), str(current), fixtures, cells=(PHASE4_CELL("cell", ("agent", "files"), ("current",)),))
            self.assertFalse(report["equivalent"])
            self.assertEqual(report["differences"][0]["cell"], "cell")
            self.assertEqual(report["differences"][0]["state"], "current")

    def test_requires_all_prepared_fixture_states(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaisesRegex(ValueError, "missing fixture states"):
                RUN_MATRIX("baseline", "current", {"current": root})

    def test_fixture_state_validation_rejects_unproven_provider_unavailability(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            (fixtures["provider-unavailable"] / ".rhizome" / "config.yml").write_text(
                "noteEmbeddings:\n  provider: test\ncodeEmbeddings:\n  provider: test\n"
            )
            with self.assertRaisesRegex(ValueError, "different provider configuration"):
                VALIDATE_FIXTURE_STATES(fixtures)

    def test_fixture_state_validation_rejects_provider_change_in_index_state_fixture(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            (fixtures["stale"] / ".rhizome" / "config.yml").write_text(
                "noteEmbeddings:\n  provider: other\ncodeEmbeddings:\n  provider: other\n"
            )
            with self.assertRaisesRegex(ValueError, "stale fixture must retain the current provider configuration"):
                VALIDATE_FIXTURE_STATES(fixtures)

    def test_fixture_facts_separate_code_metadata_from_legacy_unified_note_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            facts = DATABASE_FACTS(fixtures["current"])
            self.assertEqual(facts["codeEmbeddingMetadata"], {
                "present": True,
                "provider": "test",
                "model": "deterministic",
                "dimensions": 256,
                "schemaVersion": 12,
            })
            self.assertEqual(facts["noteEmbeddingMetadata"], {
                "present": True,
                "provider": "",
                "model": "",
                "dimensions": 0,
                "schemaVersion": 7,
            })
            VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_legacy_note_vector_proof_falls_back_only_for_exact_vec0_module_error(self):
        class MissingVecConnection:
            def __init__(self, connection: sqlite3.Connection, message: str):
                self.connection = connection
                self.message = message
                self.first = True

            def execute(self, statement: str, parameters=()):
                if self.first:
                    self.first = False
                    raise sqlite3.OperationalError(self.message)
                return self.connection.execute(statement, parameters)

        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute("CREATE TABLE intel_embeddings_vec_d256_rowids(chunk_id INTEGER PRIMARY KEY)")
            database.execute("INSERT INTO intel_embeddings_vec_d256_rowids VALUES (1)")
            database.commit()
            tables = {row[0] for row in database.execute("SELECT name FROM sqlite_master WHERE type = 'table'")}

            proof = LEGACY_NOTE_VECTOR_PROOF(
                MissingVecConnection(database, "no such module: vec0"), tables, 256
            )
            self.assertTrue(proof["matchingRow"])
            self.assertEqual(proof["proofSource"], "vec0-rowids-shadow")

            for message in ("no such module: vec01", "database is locked"):
                with self.subTest(message=message):
                    with self.assertRaisesRegex(ValueError, "cannot be read"):
                        LEGACY_NOTE_VECTOR_PROOF(MissingVecConnection(database, message), tables, 256)
            database.close()

    def test_fixture_validation_rejects_partial_note_embedding_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute("UPDATE emb_index_meta SET provider = 'test'")
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "partially populated"):
                VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_rejects_blank_note_metadata_without_expected_vector_table(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute("DROP TABLE intel_embeddings_vec_d256")
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "compatible legacy note vectors"):
                VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_rejects_blank_note_metadata_with_wrong_dimension_vector_table(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute("DROP TABLE intel_embeddings_vec_d256")
            database.execute("CREATE TABLE intel_embeddings_vec_d128(chunk_id INTEGER PRIMARY KEY)")
            database.execute("INSERT INTO intel_embeddings_vec_d128 VALUES (1)")
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "compatible legacy note vectors"):
                VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_rejects_blank_note_metadata_with_orphan_vector_row(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute("DELETE FROM intel_chunks")
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "compatible legacy note vectors"):
                VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_allows_absent_note_metadata_with_joined_expected_vector(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            for state in ("current", "provider-unavailable"):
                database = sqlite3.connect(fixtures[state] / ".rhizome" / "db.sqlite")
                database.execute("DELETE FROM emb_index_meta")
                database.commit()
                database.close()
            VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_rejects_mismatched_note_embedding_provider(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute(
                "UPDATE emb_index_meta SET provider = 'other', model = 'model', dimensions = 256"
            )
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "note embedding metadata provider"):
                VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_rejects_mismatched_code_embedding_provider(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["current"] / ".rhizome" / "db.sqlite")
            database.execute("UPDATE code_index_meta SET provider = 'other'")
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "code embedding metadata provider"):
                VALIDATE_FIXTURE_STATES(fixtures, require_semantic_chunks=True)

    def test_fixture_validation_requires_byte_identical_provider_unavailable_database(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            database = sqlite3.connect(fixtures["provider-unavailable"] / ".rhizome" / "db.sqlite")
            database.execute("UPDATE index_metadata SET value = 'changed' WHERE key = 'indexer_version'")
            database.commit()
            database.close()
            with self.assertRaisesRegex(ValueError, "byte-identical current database"):
                VALIDATE_FIXTURE_STATES(fixtures)

    def test_fixture_validation_does_not_create_wal_or_shm_sidecars(self):
        with tempfile.TemporaryDirectory() as directory:
            fixtures = fixture_states(Path(directory))
            VALIDATE_FIXTURE_STATES(fixtures)
            for state in ("current", "stale", "incompatible", "provider-unavailable"):
                database = fixtures[state] / ".rhizome" / "db.sqlite"
                self.assertFalse(Path(str(database) + "-wal").exists())
                self.assertFalse(Path(str(database) + "-shm").exists())

    def test_sidecar_mutation_is_part_of_parity_and_current_read_only_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            baseline = root / "baseline-bin"
            current = root / "current-bin"
            baseline.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "Path('.rhizome/db.sqlite-wal').write_text('baseline mutation')\n"
                "print('{\"ok\":true}')\n"
            )
            current.write_text("#!/bin/sh\nprintf '%s\\n' '{\"ok\":true}'\n")
            for binary in (baseline, current):
                binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(baseline), str(current), fixtures,
                cells=(PHASE4_CELL("cell", ("agent", "files"), ("current",)),),
            )
            state = report["cells"]["cell"]["current"]
            self.assertFalse(report["equivalent"])
            self.assertEqual(state["baseline"][0]["sidecarMutationLabels"], ["wal"])
            self.assertTrue(state["current"][0]["indexUnchanged"])
            self.assertEqual(report["unexpectedCurrentIndexMutations"], [])
            self.assertEqual(len(report["unexplainedDifferences"]), 1)

    def test_current_read_only_sidecar_mutation_fails_even_if_outputs_match(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "writer"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "Path('.rhizome/db.sqlite-wal').write_text('mutation')\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL("cell", ("agent", "files"), ("current",)),),
            )
            self.assertFalse(report["equivalent"])
            self.assertEqual(len(report["unexpectedCurrentIndexMutations"]), 1)
            self.assertEqual(report["unexpectedCurrentIndexMutations"][0]["sidecarMutationLabels"], ["wal"])

    def test_provider_unavailable_records_command_observed_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "rzm"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "import sys\n"
                "from pathlib import Path\n"
                "if 'ollama' in Path('.rhizome/config.yml').read_text():\n"
                "  print('{\"error\":\"embed input: Post http://localhost:11434/api/embed: dial tcp connection refused\"}', file=sys.stderr)\n"
                "  raise SystemExit(1)\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL("provider", ("agent", "find-connections"), ("provider-unavailable",)),),
            )
            self.assertTrue(report["providerUnavailableObservations"][0]["observedFailure"])
            self.assertTrue(report["providerUnavailableObservations"][0]["providerFailure"])
            self.assertEqual(report["evidenceGaps"], [])

    def test_query_only_allows_sidecar_coordination_but_not_database_change(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            baseline = root / "baseline-bin"
            current = root / "current-bin"
            baseline.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "Path('.rhizome/db.sqlite-wal').write_text('coordination')\n"
                "print('{\"ok\":true}')\n"
            )
            current.write_text("#!/bin/sh\nprintf '%s\\n' '{\"ok\":true}'\n")
            for binary in (baseline, current):
                binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(baseline), str(current), fixtures,
                cells=(PHASE4_CELL("query", ("agent", "report"), ("current",), "query-only"),),
            )
            self.assertTrue(report["equivalent"])
            self.assertEqual(report["unexpectedCurrentIndexMutations"], [])

    def test_query_only_missing_state_forbids_database_creation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "writer"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "db = Path('.rhizome/db.sqlite')\n"
                "if not db.exists(): db.write_text('created')\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL("query", ("agent", "report"), ("missing",), "query-only"),),
            )
            self.assertFalse(report["equivalent"])
            self.assertEqual(len(report["unexpectedCurrentIndexMutations"]), 1)

    def test_properties_missing_state_uses_the_query_only_no_creation_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "writer"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "Path('.rhizome/db.sqlite').write_text('created')\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            properties = next(cell for cell in PHASE4_CELLS() if cell.name == "properties")
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL(properties.name, properties.command, ("missing",), properties.index_mutation_policy),),
            )
            violation = report["unexpectedCurrentIndexMutations"][0]
            self.assertEqual(violation["cell"], "properties")
            self.assertEqual(violation["policy"], "query-only")

    def test_session_dedupe_allows_only_mcp_session_reservation_tables(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "session-writer"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "import sqlite3\n"
                "db = sqlite3.connect('.rhizome/db.sqlite')\n"
                "db.execute(\"INSERT INTO mcp_session_item_reservations VALUES ('s', 'item', 'r')\")\n"
                "db.commit()\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL("session", ("agent", "vault-context"), ("current",), "session-dedupe"),),
            )
            record = report["cells"]["session"]["current"]["current"][0]
            self.assertTrue(record["sessionReservationInvariant"]["satisfied"])
            self.assertEqual(record["sessionReservationInvariant"]["sessionTableChanges"], ["mcp_session_item_reservations"])
            self.assertEqual(report["unexpectedCurrentIndexMutations"], [])

    def test_session_dedupe_rejects_index_metadata_change(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "metadata-writer"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "import sqlite3\n"
                "db = sqlite3.connect('.rhizome/db.sqlite')\n"
                "db.execute(\"UPDATE index_metadata SET value = 'changed' WHERE key = 'indexer_version'\")\n"
                "db.commit()\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL("session", ("agent", "vault-context"), ("current",), "session-dedupe"),),
            )
            violation = report["unexpectedCurrentIndexMutations"][0]
            self.assertEqual(violation["policy"], "session-dedupe")
            self.assertEqual(violation["protectedTableChanges"], ["index_metadata"])

    def test_session_dedupe_rejects_schema_only_change(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            binary = root / "schema-writer"
            binary.write_text(
                "#!/usr/bin/env python3\n"
                "import sqlite3\n"
                "db = sqlite3.connect('.rhizome/db.sqlite')\n"
                "db.execute('CREATE INDEX evidence_gap ON index_metadata(key)')\n"
                "db.commit()\n"
                "print('{\"ok\":true}')\n"
            )
            binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(binary), str(binary), fixtures,
                cells=(PHASE4_CELL("session", ("agent", "vault-context"), ("current",), "session-dedupe"),),
            )
            violation = report["unexpectedCurrentIndexMutations"][0]
            self.assertFalse(violation["schemaPreserved"])

    def test_root_twin_preserve_rejects_corpus_change(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            baseline = root / "root-reader"
            current = root / "root-writer"
            baseline.write_text("#!/bin/sh\nprintf '%s\\n' '{\"ok\":true}'\n")
            current.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                "Path('fixture.txt').write_text('changed corpus\\n')\n"
                "print('{\"ok\":true}')\n"
            )
            for binary in (baseline, current):
                binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            report = RUN_MATRIX(
                str(baseline), str(current), fixtures,
                cells=(PHASE4_CELL("root", ("graph", "file-context"), ("current",), "preserve"),),
            )
            violation = report["unexpectedCurrentIndexMutations"][0]
            self.assertEqual(violation["policy"], "preserve")
            self.assertFalse(violation["current"][0]["corpusPreserved"])

    def test_cli_authorization_mapping_separates_planned_from_unexplained_differences(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = fixture_states(root)
            baseline = root / "baseline-bin"
            current = root / "current-bin"
            baseline.write_text("#!/bin/sh\nprintf '%s\\n' '{\"result\":\"baseline\"}'\n")
            current.write_text("#!/bin/sh\nprintf '%s\\n' '{\"result\":\"current\"}'\n")
            for binary in (baseline, current):
                binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
            command = [
                sys.executable, str(HARNESS), "--baseline-rzm", str(baseline),
                "--current-rzm", str(current), "--cell", "files-basic",
            ]
            for state, fixture in fixtures.items():
                command.extend(("--fixture", f"{state}={fixture}"))
            for state in ("current", "missing", "stale", "incompatible"):
                command.extend(("--authorize-difference", f"files-basic:{state}"))
            result = subprocess.run(command, text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads(result.stdout)
            self.assertEqual(len(report["authorizedDifferences"]), 4)
            self.assertEqual(report["unexplainedDifferences"], [])


if __name__ == "__main__":
    unittest.main()
