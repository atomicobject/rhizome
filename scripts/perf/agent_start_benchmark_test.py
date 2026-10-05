import json
import re
import runpy
import sqlite3
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "perf"))
HARNESS = ROOT / "scripts/perf/agent_start_benchmark.sh"
MODULE = runpy.run_path(str(ROOT / "scripts/perf/agent_start_benchmark.py"))
NORMALIZE = MODULE["normalize"]
ASSERT_OUTPUT_CONTRACT = MODULE["assert_output_contract"]
ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT = MODULE["assert_file_context_output_contract"]
SCENARIO_COMMANDS = MODULE["scenario_commands"]
SEMANTIC_QUERY_COMMANDS = MODULE["semantic_query_commands"]
ASSERT_SEMANTIC_QUERY_OUTPUT_CONTRACT = MODULE["assert_semantic_query_output_contract"]
RUN_SAMPLES = MODULE["run_samples"]
REPRESENTATIVE_SCENARIO_COMMANDS = MODULE["representative_scenario_commands"]
REPRESENTATIVE_EXECUTION_CONTRACTS = MODULE["representative_execution_contracts"]
RUN_EXACT_INDEX_EVIDENCE = MODULE["run_exact_index_evidence"]
VALIDATE_EXACT_INDEX_FIXTURES = MODULE["validate_exact_index_fixtures"]
RUN_EXACT_INDEX_BASELINE_COMPARISON = MODULE["run_exact_index_baseline_comparison"]
EXACT_INDEX_FIXTURE_FACTS = MODULE["_exact_index_fixture_facts"]
EXACT_INDEX_RESPONSE_EVIDENCE = MODULE["exact_index_response_evidence"]
EXACT_INDEX_BASELINE_CONTRACT = MODULE["exact_index_baseline_execution_contract"]


class AgentStartBenchmarkTest(unittest.TestCase):
    def test_exact_index_unavailable_accepts_typed_remediation(self):
        evidence = EXACT_INDEX_RESPONSE_EVIDENCE(
            "codeSymbol",
            {"code": "indexed-context-stale", "message": "stale", "remediation": "rzm index"},
            available=False,
        )
        self.assertEqual(evidence["shape"], ["code", "message", "remediation"])

    def test_exact_index_baseline_failure_reads_declared_stdout_channel(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "rzm"
            fake.write_text(
                "#!/usr/bin/env python3\n"
                "import json\n"
                "print(json.dumps({'error':'legacy unavailable'}))\n"
                "raise SystemExit(1)\n"
            )
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)
            report = RUN_SAMPLES(
                "codeSymbol", [str(fake)], root, 1,
                contract=EXACT_INDEX_BASELINE_CONTRACT("missing"),
            )
            self.assertEqual(report["responseEvidence"][0]["shape"], ["error"])

    def test_exact_index_fixture_validation_does_not_create_wal_sidecars(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory)
            database = source / ".rhizome" / "db.sqlite"
            database.parent.mkdir()
            connection = sqlite3.connect(database)
            connection.execute("PRAGMA journal_mode=WAL")
            connection.executescript(
                "CREATE TABLE schema_version(version INTEGER);"
                "CREATE TABLE rzm_migration_state(domain TEXT PRIMARY KEY, version INTEGER);"
                "CREATE TABLE index_metadata(key TEXT PRIMARY KEY, value TEXT);"
                "CREATE TABLE files(path TEXT);"
                "CREATE TABLE intel_code_anchors(id TEXT);"
            )
            connection.execute("INSERT INTO rzm_migration_state VALUES ('intel', 59)")
            connection.executemany(
                "INSERT INTO index_metadata VALUES (?, ?)",
                [("indexer_version", "v1.9.0"), ("scope_config_hash", "scope-v1")],
            )
            connection.execute("INSERT INTO intel_code_anchors VALUES ('anchor-a')")
            connection.commit()
            connection.execute("PRAGMA wal_checkpoint(TRUNCATE)")
            connection.close()

            for suffix in ("-wal", "-shm"):
                sidecar = Path(f"{database}{suffix}")
                if sidecar.exists():
                    sidecar.rename(Path(f"{sidecar}.saved"))

            self.assertFalse(Path(f"{database}-wal").exists())
            self.assertFalse(Path(f"{database}-shm").exists())
            self.assertTrue(EXACT_INDEX_FIXTURE_FACTS(source)["databasePresent"])
            self.assertFalse(Path(f"{database}-wal").exists())
            self.assertFalse(Path(f"{database}-shm").exists())

    def test_exact_index_evidence_freezes_each_state_without_index_writes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sources: dict[str, Path] = {}
            for state in ("indexed", "missing", "stale", "incompatible"):
                source = root / state
                rhizome = source / ".rhizome"
                rhizome.mkdir(parents=True)
                (source / "README.md").write_text("# Exact fixture\n")
                if state != "missing":
                    connection = sqlite3.connect(rhizome / "db.sqlite")
                    connection.executescript(
                        "CREATE TABLE schema_version(version INTEGER);"
                        "CREATE TABLE rzm_migration_state(domain TEXT PRIMARY KEY, version INTEGER, dirty INTEGER);"
                        "CREATE TABLE index_metadata(key TEXT PRIMARY KEY, value TEXT);"
                        "CREATE TABLE files(path TEXT);"
                        "CREATE TABLE intel_code_anchors(id TEXT);"
                    )
                    version = 60 if state == "incompatible" else 59
                    indexer = "v0.0.0-stale" if state == "stale" else "v1.9.0"
                    connection.execute("INSERT INTO schema_version VALUES (?)", (version,))
                    connection.execute("INSERT INTO rzm_migration_state VALUES ('intel', ?, 0)", (version,))
                    connection.executemany(
                        "INSERT INTO index_metadata VALUES (?, ?)",
                        [("indexer_version", indexer), ("scope_config_hash", "scope-v1")],
                    )
                    connection.execute("INSERT INTO files VALUES ('pkg/a.go')")
                    connection.execute("INSERT INTO intel_code_anchors VALUES ('anchor-a')")
                    connection.commit()
                    connection.close()
                sources[state] = source

            fake = root / "rzm"
            fake.write_text(
                "#!/usr/bin/env python3\n"
                "import json, sqlite3, sys\n"
                "from pathlib import Path\n"
                "args = ' '.join(sys.argv[1:])\n"
                "ops = {'code-symbol':'codeSymbol', 'code-references':'codeReferences', 'code-rationale':'codeRationale', 'graph-path':'graphPath'}\n"
                "op = next((value for key, value in ops.items() if key in args), None)\n"
                "if not op: raise SystemExit(9)\n"
                "db = Path('.rhizome/db.sqlite')\n"
                "available = False\n"
                "if db.exists():\n"
                "  con = sqlite3.connect(db)\n"
                "  version = con.execute(\"SELECT version FROM rzm_migration_state WHERE domain = 'intel'\").fetchone()[0]\n"
                "  metadata = dict(con.execute('SELECT key, value FROM index_metadata'))\n"
                "  available = version == 59 and metadata.get('indexer_version') == 'v1.9.0'\n"
                "if not available:\n"
                "  print(json.dumps({'error':'code index unavailable; run `rzm index`'}), file=sys.stderr)\n"
                "  raise SystemExit(1)\n"
                "payloads = {\n"
                " 'codeSymbol': {'symbol':'pkg.Symbol','status':'resolved','definition':{'path':'pkg/a.go','fqn':'pkg.Symbol','startLine':4,'endLine':8,'content':'func Symbol() {}'}},\n"
                " 'codeReferences': {'symbol':'pkg.Symbol','status':'resolved','callers':[{'path':'pkg/a_test.go','fqn':'pkg.TestSymbol','startLine':3}],'callees':[]},\n"
                " 'codeRationale': {'count':2,'rationale':[{'id':'a','path':'pkg/a.go','kind':'note','startLine':2,'endLine':2,'content':'NOTE: one'},{'id':'b','path':'pkg/b.go','kind':'todo','startLine':4,'endLine':4,'content':'TODO: two'}]},\n"
                " 'graphPath': {'From':'note:a','To':'note:b','Hops':1,'Path':[{'FromPath':'note:a','ToPath':'note:b','EdgeKind':'link'}]},\n"
                "}\n"
                "print(json.dumps(payloads[op]))\n"
            )
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)

            report = RUN_EXACT_INDEX_EVIDENCE(
                str(fake), sources, samples=2, symbol="pkg.Symbol", graph_from="a", graph_to="b"
            )

            self.assertEqual(set(report["states"]), {"indexed", "missing", "stale", "incompatible"})
            for state, cells in report["states"].items():
                self.assertEqual(set(cells), {"codeSymbol", "codeReferences", "codeRationale", "graphPath"})
                for cell in cells.values():
                    self.assertTrue(cell["indexFingerprintsUnchanged"])
                    self.assertEqual(len(cell["indexFingerprintsBefore"]), 2)
                    self.assertEqual(len(cell["fullStdoutSha256s"]), 2)
                    self.assertEqual(len(cell["fullStderrSha256s"]), 2)
            for name in ("codeSymbol", "codeReferences", "codeRationale", "graphPath"):
                self.assertEqual(report["states"]["stale"][name]["exitCodes"], [1, 1])
            self.assertEqual(
                report["states"]["indexed"]["codeRationale"]["responseEvidence"][0]["sourceOrder"],
                [["pkg/a.go", 2, "a"], ["pkg/b.go", 4, "b"]],
            )
            self.assertEqual(
                report["states"]["indexed"]["graphPath"]["responseEvidence"][0]["sourceOrder"],
                [["note:a", "note:b", "link"]],
            )
            baseline_fake = root / "rzm-baseline"
            baseline_fake.write_text(
                "#!/usr/bin/env python3\n"
                "import subprocess, sys\n"
                f"result = subprocess.run([{str(fake)!r}, *sys.argv[1:]], text=True, capture_output=True)\n"
                "sys.stdout.write(result.stdout if result.returncode == 0 else result.stderr)\n"
                "raise SystemExit(result.returncode)\n"
            )
            baseline_fake.chmod(baseline_fake.stat().st_mode | stat.S_IXUSR)
            comparison = RUN_EXACT_INDEX_BASELINE_COMPARISON(
                str(baseline_fake), str(fake), sources, samples=2, symbol="pkg.Symbol", graph_from="a", graph_to="b"
            )
            self.assertFalse(comparison["baselineCurrentEquivalent"])
            self.assertTrue(all(
                difference["state"] in {"missing", "stale", "incompatible"}
                for difference in comparison["baselineCurrentDifferences"]
            ))
            self.assertEqual(comparison["samples"], 2)

            mutating_baseline = root / "rzm-mutating-baseline"
            mutating_baseline.write_text(
                "#!/usr/bin/env python3\n"
                "import sqlite3, subprocess, sys\n"
                "from pathlib import Path\n"
                f"child = {str(fake)!r}\n"
                "result = subprocess.run([child, *sys.argv[1:]], check=False, text=True, capture_output=True)\n"
                "sys.stdout.write(result.stdout if result.returncode == 0 else result.stderr)\n"
                "if 'agent code-symbol' in ' '.join(sys.argv[1:]) and not Path('.rhizome/db.sqlite').exists():\n"
                "    db = sqlite3.connect('.rhizome/db.sqlite')\n"
                "    db.executescript(\"CREATE TABLE rzm_migration_state(domain TEXT, version INTEGER); CREATE TABLE index_metadata(key TEXT, value TEXT); INSERT INTO rzm_migration_state VALUES ('intel', 59); INSERT INTO index_metadata VALUES ('indexer_version', 'v1.9.0');\")\n"
                "    db.commit()\n"
                "    db.close()\n"
                "raise SystemExit(result.returncode)\n"
            )
            mutating_baseline.chmod(mutating_baseline.stat().st_mode | stat.S_IXUSR)
            isolated = RUN_EXACT_INDEX_BASELINE_COMPARISON(
                str(mutating_baseline), str(fake), sources,
                samples=2, symbol="pkg.Symbol", graph_from="a", graph_to="b",
            )
            baseline_missing = isolated["baseline"]["evidence"]["states"]["missing"]
            self.assertTrue(baseline_missing["codeSymbol"]["indexFingerprintsAfter"][0]["db"]["present"])
            self.assertFalse(baseline_missing["codeReferences"]["indexFingerprintsBefore"][0]["db"]["present"])
            self.assertEqual(
                isolated["baseline"]["evidence"]["cellIsolation"],
                "fresh-copy-per-command",
            )

    def test_exact_index_fixture_validation_rejects_state_labels(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sources = {state: root / state for state in ("indexed", "missing", "stale", "incompatible")}
            for source in sources.values():
                (source / ".rhizome").mkdir(parents=True)
                (source / "README.md").write_text("# fixture\n")
            (sources["indexed"] / ".rhizome" / "state").write_text("indexed")
            with self.assertRaisesRegex(RuntimeError, "must not use a state label"):
                VALIDATE_EXACT_INDEX_FIXTURES(sources)
    def test_run_samples_disables_repository_delegation(self):
        completed = subprocess.CompletedProcess(
            args=[],
            returncode=0,
            stdout=json.dumps({"query": "q", "matches": [], "lanes": []}),
            stderr="",
        )
        with mock.patch.object(subprocess, "run", return_value=completed) as run:
            RUN_SAMPLES(
                "semanticRepeatedNoSession",
                ["/tmp/rzm", "agent", "semantic-query", "--query", "q"],
                Path("/tmp"),
                1,
            )

        self.assertEqual(run.call_args.kwargs["env"]["RZM_SKIP_REPO_DELEGATE"], "1")

    def test_semantic_query_commands_cover_session_and_modes(self):
        commands = SEMANTIC_QUERY_COMMANDS(
            "/tmp/rzm", "pkg/app/mcp", "semantic query invariants", timings=True
        )
        self.assertEqual(
            commands["semanticRepeatedNoSession"],
            [
                "/tmp/rzm", "agent", "semantic-query", "--path", "pkg/app/mcp",
                "--query", "semantic query invariants", "--limit", "20", "--timings",
            ],
        )
        self.assertIn("--session-id", commands["semanticRepeatedSession"])
        self.assertEqual(commands["semanticOverview"][-2:], ["--mode", "subsystem_overview"])
        self.assertEqual(commands["semanticPrecision"][-2:], ["--mode", "docs_for_code"])

    def test_semantic_query_contract_requires_opt_in_diagnostics(self):
        payload = {
            "query": "semantic query invariants",
            "matches": [],
            "lanes": [],
            "diagnostics": {"phases": [], "operations": [], "search": [], "deadline": {"set": False}},
        }
        ASSERT_SEMANTIC_QUERY_OUTPUT_CONTRACT("semanticRepeatedNoSession", payload, True)
        with self.assertRaisesRegex(RuntimeError, "omitted diagnostics"):
            ASSERT_SEMANTIC_QUERY_OUTPUT_CONTRACT(
                "semanticRepeatedNoSession", {"query": "q", "matches": [], "lanes": []}, True
            )

    def test_representative_matrix_builds_required_one_shot_cells(self):
        commands = REPRESENTATIVE_SCENARIO_COMMANDS(
            "/tmp/rzm", code_target="pkg/app/bootstrap/live.go"
        )
        self.assertEqual(
            set(commands),
            {
                "schemaOnly",
                "exactIndex",
                "liveNote",
                "ontologyQuery",
                "unavailableIndex",
                "rootSearch",
            },
        )
        self.assertEqual(commands["schemaOnly"][:3], ["/tmp/rzm", "agent", "ontology-query-schema"])
        self.assertEqual(commands["rootSearch"][:2], ["/tmp/rzm", "search"])
        contracts = REPRESENTATIVE_EXECUTION_CONTRACTS()
        self.assertEqual(contracts["rootSearch"].output_format, "text")
        self.assertIsNone(contracts["rootSearch"].json_channel)
        self.assertEqual(contracts["ontologyQuery"].expected_exit_codes, (0,))

    def test_normalization_retains_diagnostic_availability(self):
        normalized = NORMALIZE({"durationMs": 2, "diagnostics": {"operations": [
            {"label": "agent_start.sqlite.schema_statements", "count": 0, "available": False}
        ]}})
        self.assertNotIn("durationMs", normalized)
        self.assertFalse(normalized["diagnostics"]["operations"][0]["available"])

    def test_explicit_rich_command_matches_the_representative_activity(self):
        command = SCENARIO_COMMANDS("/tmp/rzm", "docs/specs")["explicitRich"]
        self.assertEqual(
            command,
            [
                "/tmp/rzm", "agent", "start", "--profile", "code", "--ontology",
                "--file", "docs/specs", "--submodule-depth", "1", "--intent",
                "Test out some rhizome stuff", "--timings",
            ],
        )

    def test_file_context_commands_cover_indexed_target_shapes(self):
        commands = SCENARIO_COMMANDS(
            "/tmp/rzm",
            "docs/specs",
            "src/service.go",
            "src",
            "docs/service.md",
        )
        self.assertEqual(
            commands["repeatedIndexedFileContextCode"],
            [
                "/tmp/rzm", "agent", "file-context", "--profile", "code",
                "--file", "src/service.go",
            ],
        )
        self.assertEqual(
            commands["repeatedIndexedFileContextDirectory"],
            [
                "/tmp/rzm", "agent", "file-context", "--profile", "code",
                "--file", "src", "--submodule-depth", "1",
            ],
        )
        self.assertEqual(
            commands["repeatedIndexedFileContextMarkdown"],
            [
                "/tmp/rzm", "agent", "file-context", "--profile", "vault",
                "--file", "docs/service.md",
            ],
        )
        self.assertEqual(
            commands["repeatedIndexedFileContextMixed"],
            [
                "/tmp/rzm", "agent", "file-context", "--profile", "code",
                "--file", "src/service.go", "--file", "src",
                "--file", "docs/service.md", "--submodule-depth", "1",
            ],
        )

    def test_file_context_contract_requires_session_and_text(self):
        ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
            "repeatedIndexedFileContextCode",
            {
                "sessionId": "session",
                "dedupeHits": 0,
                "indexedEnrichment": {"status": "available"},
                "text": "# Rhizome context\n\n- tool: file_context",
            },
        )
        with self.assertRaisesRegex(RuntimeError, "non-empty sessionId"):
            ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
                "repeatedIndexedFileContextCode",
                {"text": "# Rhizome context\n\n- tool: file_context"},
            )
        with self.assertRaisesRegex(RuntimeError, "file-context text"):
            ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
                "repeatedIndexedFileContextCode",
                {"sessionId": "session", "text": ""},
            )
        with self.assertRaisesRegex(RuntimeError, "not file-context text"):
            ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
                "repeatedIndexedFileContextCode",
                {"sessionId": "session", "text": "# Rhizome context"},
            )
        with self.assertRaisesRegex(RuntimeError, "invalid dedupeHits"):
            ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
                "repeatedIndexedFileContextCode",
                {
                    "sessionId": "session",
                    "dedupeHits": -1,
                    "indexedEnrichment": {"status": "available"},
                    "text": "# Rhizome context\n\n- tool: file_context",
                },
            )

    def test_file_context_contract_requires_available_index(self):
        for enrichment in (None, {"status": "missing"}, {"status": "stale"}):
            with self.subTest(enrichment=enrichment):
                payload = {
                    "sessionId": "session",
                    "text": "# Rhizome context\n\n- tool: file_context",
                }
                if enrichment is not None:
                    payload["indexedEnrichment"] = enrichment
                with self.assertRaisesRegex(
                    RuntimeError, "indexedEnrichment.status=available"
                ):
                    ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
                        "repeatedIndexedFileContextCode", payload
                    )

    def test_file_context_contract_rejects_rendered_target_error_blocks(self):
        for target_kind in ("code", "note"):
            with self.subTest(target_kind=target_kind):
                with self.assertRaisesRegex(RuntimeError, "rendered target error"):
                    ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
                        "repeatedIndexedFileContextCode",
                        {
                            "sessionId": "session",
                            "text": (
                                "# Rhizome context\n\n"
                                "- tool: file_context\n\n"
                                f"### missing.go ({target_kind})\n\n"
                                "Error: stat missing.go: no such file or directory"
                            ),
                        },
                    )

        ASSERT_FILE_CONTEXT_OUTPUT_CONTRACT(
            "repeatedIndexedFileContextCode",
            {
                "sessionId": "session",
                "indexedEnrichment": {"status": "available"},
                "text": (
                    "# Rhizome context\n\n"
                    "- tool: file_context\n\n"
                    "### existing.go (code)\n\n"
                    "## Troubleshooting\n\n"
                    "Error: messages in source documentation remain valid context."
                ),
            },
        )

    def test_reports_first_and_repeated_local_scenarios(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            source.mkdir()
            fake = root / "rzm"
            calls = root / "calls"
            operations = [
                {"label": label, "count": 0, "available": True}
                for label in (
                    "agent_start.note.passes", "agent_start.note.reads", "agent_start.code.reads",
                    "agent_start.repo.walks", "agent_start.index.writes",
                    "agent_start.sqlite.schema_statements",
                    "agent_start.sqlite.integrity_checks", "agent_start.sqlite.transactions",
                    "agent_start.session.reserves", "agent_start.session.commits",
                    "agent_start.session.releases", "agent_start.session.commit_failures",
                    "agent_start.session.release_failures", "agent_start.session.cleanups",
                )
            ]
            payload = json.dumps({
                "sessionId": "volatile", "surfaceCommand": "rzm agent surface",
                "notes": ["next"], "vaultContext": {"text": "minimal guidance"},
                "diagnostics": {"operations": operations},
            })
            rich_operations = [dict(operation) for operation in operations]
            rich_payload = json.dumps({
                "sessionId": "volatile", "surfaceCommand": "rzm agent surface",
                "notes": ["next"], "vaultContext": {
                    "text": "rich guidance",
                    "indexedEnrichment": {"status": "available"},
                },
                "ontology": {
                    "available": True, "ready": True, "schemaHash": "abc123",
                    "totalNotes": 470, "typedNotes": 400, "untypedNotes": 70,
                    "typeCounts": [{"typeName": "Spec", "count": 20}],
                },
                "diagnostics": {
                    "operations": rich_operations,
                    "phases": [
                        {"label": "agent_start.note.crawl", "count": 0, "durationMs": 0},
                        {"label": "agent_start.coderef.discovery_read", "count": 0, "durationMs": 0},
                    ],
                },
            })
            file_context_payload = json.dumps({
                "sessionId": "volatile",
                "indexedEnrichment": {"status": "available"},
                "text": "# Rhizome context\n\n- tool: file_context\n\n### Target",
            })
            fake.write_text(
                "#!/bin/sh\n"
                + f"printf '%s\\n' \"$*\" >> '{calls}'\n"
                + f"if echo \"$*\" | grep -q 'agent file-context'; then printf '%s\\n' '{file_context_payload}'; "
                + f"elif echo \"$*\" | grep -q -- --ontology; then printf '%s\\n' '{rich_payload}'; "
                + f"else printf '%s\\n' '{payload}'; fi\n"
            )
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)
            result = subprocess.run(
                [str(HARNESS), "--rzm", str(fake), "--source", str(source)],
                text=True, capture_output=True, check=True,
            )
            report = json.loads(result.stdout)
            self.assertEqual(report["schemaVersion"], 3)
            self.assertNotIn("lima", report["environment"])
            scenarios = report["scenarios"]
            self.assertEqual(set(scenarios), {
                "firstBare",
                "repeatedBare",
                "directoryTargeted",
                "explicitRich",
                "repeatedIndexedFileContextCode",
                "repeatedIndexedFileContextDirectory",
                "repeatedIndexedFileContextMarkdown",
                "repeatedIndexedFileContextMixed",
            })
            self.assertEqual(scenarios["firstBare"]["samples"], 1)
            self.assertEqual(len(scenarios["firstBare"]["durationsMs"]), 1)
            for name in ("repeatedBare", "directoryTargeted", "explicitRich"):
                self.assertEqual(scenarios[name]["samples"], 7)
                self.assertEqual(len(scenarios[name]["operationCounts"]), 7)
                self.assertEqual(len(scenarios[name]["operationEnvelopes"]), 7)
                self.assertTrue(scenarios[name]["operationEnvelopesEquivalent"])
                self.assertTrue(scenarios[name]["operationDiagnosticsAvailable"])
                self.assertTrue(scenarios[name]["outputEquivalent"])
                self.assertEqual(scenarios[name]["outputContract"], "agent-start-v2")
            self.assertEqual(
                scenarios["explicitRich"]["indexedEnrichmentStatuses"],
                ["available"] * 7,
            )
            for name in (
                "repeatedIndexedFileContextCode",
                "repeatedIndexedFileContextDirectory",
                "repeatedIndexedFileContextMarkdown",
                "repeatedIndexedFileContextMixed",
            ):
                self.assertEqual(scenarios[name]["samples"], 7)
                self.assertEqual(scenarios[name]["outputContract"], "file-context-v1")
                self.assertEqual(scenarios[name]["runMode"], "repeatedOneShot")
                self.assertEqual(
                    scenarios[name]["indexStatePrecondition"],
                    "explicitRich.available",
                )
                self.assertTrue(scenarios[name]["outputEquivalent"])
                self.assertEqual(len(scenarios[name]["outputBytes"]), 7)
                self.assertNotIn("diagnostics", scenarios[name])
                self.assertNotIn("operationCounts", scenarios[name])
            invocations = calls.read_text().splitlines()
            self.assertEqual(len(invocations), 50)  # first bare plus seven seven-sample scenarios
            self.assertEqual(invocations[0], "agent start --timings")
            self.assertIn("--file docs/specs", invocations[8])
            self.assertIn("--ontology", invocations[15])
            self.assertEqual(
                invocations[22],
                "agent file-context --profile code --file pkg/app/cli/file_context.go",
            )
            self.assertEqual(
                invocations[29],
                "agent file-context --profile code --file pkg/app/cli --submodule-depth 1",
            )
            self.assertEqual(
                invocations[36],
                "agent file-context --profile vault --file docs/reference/subsystems/agent-surface.md",
            )
            self.assertEqual(
                invocations[43],
                "agent file-context --profile code --file pkg/app/cli/file_context.go "
                "--file pkg/app/cli --file docs/reference/subsystems/agent-surface.md "
                "--submodule-depth 1",
            )

    def test_contract_failure_stops_the_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "rzm"
            fake.write_text("#!/bin/sh\nprintf '%s\\n' '{\"sessionId\":\"x\"}'\n")
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)
            result = subprocess.run(
                [str(HARNESS), "--rzm", str(fake), "--source", str(root)],
                text=True, capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("surface command pointer", result.stderr)

    def test_minimal_contract_rejects_index_writes_and_session_work(self):
        for nonzero_label in ("agent_start.index.writes", "agent_start.session.cleanups"):
            with self.subTest(label=nonzero_label):
                payload = {
                    "sessionId": "session",
                    "surfaceCommand": "rzm agent surface",
                    "notes": [],
                    "vaultContext": {"text": "minimal guidance"},
                    "diagnostics": {"operations": [
                        {
                            "label": label,
                            "count": int(label == nonzero_label),
                            "available": True,
                        }
                        for label in MODULE["MINIMAL_ZERO_OPERATIONS"]
                    ]},
                }
                with self.assertRaisesRegex(RuntimeError, f"{re.escape(nonzero_label)}=0"):
                    ASSERT_OUTPUT_CONTRACT("repeatedBare", payload)

    def test_explicit_rich_contract_rejects_minimal_fallback(self):
        operations = [
            {"label": label, "count": 0, "available": True}
            for label in MODULE["MINIMAL_ZERO_OPERATIONS"]
        ]
        payload = {
            "sessionId": "session",
            "surfaceCommand": "rzm agent surface",
            "notes": [],
            "vaultContext": {
                "text": "minimal guidance",
                "indexedEnrichment": {"status": "available"},
            },
            "diagnostics": {"operations": operations},
        }
        with self.assertRaisesRegex(RuntimeError, "meaningful ready ontology summary"):
            ASSERT_OUTPUT_CONTRACT("explicitRich", payload)

    def test_explicit_rich_contract_rejects_live_discovery_work(self):
        operations = [
            {"label": label, "count": 0, "available": True}
            for label in MODULE["MINIMAL_ZERO_OPERATIONS"]
        ]
        operations[0]["count"] = 1
        payload = {
            "sessionId": "session",
            "surfaceCommand": "rzm agent surface",
            "notes": [],
            "vaultContext": {
                "text": "indexed guidance",
                "indexedEnrichment": {"status": "available"},
            },
            "ontology": {
                "available": True,
                "ready": True,
                "schemaHash": "abc123",
                "totalNotes": 470,
                "typeCounts": [{"typeName": "Spec", "count": 20}],
            },
            "diagnostics": {
                "operations": operations,
                "phases": [
                    {"label": "agent_start.note.crawl", "count": 0},
                    {"label": "agent_start.coderef.discovery_read", "count": 0},
                ],
            },
        }
        with self.assertRaisesRegex(RuntimeError, "agent_start.note.passes=0"):
            ASSERT_OUTPUT_CONTRACT("explicitRich", payload)

    def test_explicit_rich_contract_rejects_each_read_only_violation(self):
        for nonzero_label in (
            "agent_start.index.writes",
            "agent_start.sqlite.schema_statements",
            "agent_start.sqlite.transactions",
            "agent_start.sqlite.integrity_checks",
        ):
            with self.subTest(label=nonzero_label):
                operations = [
                    {"label": label, "count": int(label == nonzero_label), "available": True}
                    for label in MODULE["MINIMAL_ZERO_OPERATIONS"]
                ]
                payload = {
                    "sessionId": "session",
                    "surfaceCommand": "rzm agent surface",
                    "notes": [],
                    "vaultContext": {
                        "text": "indexed guidance",
                        "indexedEnrichment": {"status": "available"},
                    },
                    "ontology": {
                        "available": True,
                        "ready": True,
                        "schemaHash": "abc123",
                        "totalNotes": 470,
                        "typeCounts": [{"typeName": "Spec", "count": 20}],
                    },
                    "diagnostics": {
                        "operations": operations,
                        "phases": [
                            {"label": "agent_start.note.crawl", "count": 0},
                            {"label": "agent_start.coderef.discovery_read", "count": 0},
                        ],
                    },
                }
                with self.assertRaisesRegex(
                    RuntimeError,
                    (
                        "session-only sqlite transactions"
                        if nonzero_label == "agent_start.sqlite.transactions"
                        else f"{re.escape(nonzero_label)}=0"
                    ),
                ):
                    ASSERT_OUTPUT_CONTRACT("explicitRich", payload)

    def test_explicit_rich_contract_rejects_missing_indexed_status(self):
        operations = [
            {"label": label, "count": 0, "available": True}
            for label in MODULE["MINIMAL_ZERO_OPERATIONS"]
        ]
        payload = {
            "sessionId": "session",
            "surfaceCommand": "rzm agent surface",
            "notes": [],
            "vaultContext": {"text": "indexed guidance"},
            "ontology": {
                "available": True,
                "ready": True,
                "schemaHash": "abc123",
                "totalNotes": 470,
                "typeCounts": [{"typeName": "Spec", "count": 20}],
            },
            "diagnostics": {
                "operations": operations,
                "phases": [
                    {"label": "agent_start.note.crawl", "count": 0},
                    {"label": "agent_start.coderef.discovery_read", "count": 0},
                ],
            },
        }
        with self.assertRaisesRegex(RuntimeError, "indexed-enrichment status"):
            ASSERT_OUTPUT_CONTRACT("explicitRich", payload)

    def test_rejects_missing_source_and_too_few_samples(self):
        missing = subprocess.run(
            [str(HARNESS), "--source", "/definitely/not/here"], text=True, capture_output=True
        )
        self.assertNotEqual(missing.returncode, 0)
        self.assertIn("not a directory", missing.stderr)
        samples = subprocess.run([str(HARNESS), "--samples", "6"], text=True, capture_output=True)
        self.assertNotEqual(samples.returncode, 0)
        self.assertIn("at least 7", samples.stderr)


if __name__ == "__main__":
    unittest.main()
