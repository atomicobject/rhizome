import hashlib
import runpy
import stat
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "perf"))
MODULE = runpy.run_path(str(ROOT / "scripts/perf/agent_start_benchmark.py"))
RUN_SAMPLES = MODULE["run_samples"]
OPERATION_ENVELOPE = MODULE["operation_envelope"]
EVIDENCE_MANIFEST = MODULE["evidence_manifest"]
SCENARIO_EXECUTION_CONTRACT = MODULE["ScenarioExecutionContract"]


class BenchmarkEvidenceTest(unittest.TestCase):
    def test_text_contract_hashes_output_without_json_parsing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "rzm"
            fake.write_text("#!/bin/sh\nprintf '%s\\n' 'No matches.'\n")
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)

            report = RUN_SAMPLES(
                "rootSearch", [str(fake)], root, 2,
                contract=SCENARIO_EXECUTION_CONTRACT(
                    output_format="text",
                    json_channel=None,
                    output_contract="root-search-text-v1",
                ),
            )

            self.assertTrue(report["outputEquivalent"])
            self.assertEqual(report["uniqueNormalizedStdoutOutputs"], 1)
            self.assertFalse(report["operationDiagnosticsAvailable"])

    def test_operation_envelope_preserves_unavailable_zero(self):
        envelope = OPERATION_ENVELOPE({
            "diagnostics": {
                "operations": [
                    {"label": "available.zero", "count": 0, "available": True},
                    {"label": "unavailable.zero", "count": 0, "available": False},
                ],
            },
        })
        self.assertEqual(envelope, [
            {"label": "available.zero", "count": 0, "available": True},
            {"label": "unavailable.zero", "count": 0, "available": False},
        ])

    def test_run_samples_records_structured_failure_stdout_and_stderr_hashes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "rzm"
            fake.write_text(
                "#!/bin/sh\n"
                "printf '%s\\n' 'preflight unavailable'\n"
                "printf '%s\\n' '{\"ok\":false,\"error\":\"index missing\","
                "\"exitCode\":2,\"remediation\":\"rzm index\"}' >&2\n"
                "exit 2\n"
            )
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)
            report = RUN_SAMPLES(
                "unavailableIndex", [str(fake)], root, 2,
                contract=SCENARIO_EXECUTION_CONTRACT(
                    expected_exit_codes=(2,), json_channel="stderr",
                    output_contract="structured-unavailable-v1",
                ),
            )
            self.assertEqual(report["exitCodes"], [2, 2])
            self.assertEqual(report["stdoutBytes"], [22, 22])
            self.assertGreater(report["stderrBytes"][0], 0)
            self.assertEqual(len(report["normalizedStdoutSha256s"]), 2)
            self.assertEqual(len(report["normalizedStderrSha256s"]), 2)
            self.assertTrue(report["outputEquivalent"])
            self.assertFalse(report["operationDiagnosticsAvailable"])
            self.assertEqual(report["operationDiagnosticsAvailableBySample"], [False, False])
            self.assertEqual(report["outputContract"], "structured-unavailable-v1")

    def test_full_normalized_output_hash_detects_meaningful_stderr_change(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "rzm"
            counter = root / "counter"
            fake.write_text(
                "#!/bin/sh\n"
                f"if [ -f '{counter}' ]; then warning=stale; else warning=missing; fi\n"
                f"touch '{counter}'\n"
                "printf '{\"ok\":false,\"warning\":\"%s\",\"exitCode\":2}\\n' \"$warning\" >&2\n"
                "exit 2\n"
            )
            fake.chmod(fake.stat().st_mode | stat.S_IXUSR)
            report = RUN_SAMPLES(
                "unavailableIndex", [str(fake)], root, 2,
                contract=SCENARIO_EXECUTION_CONTRACT(
                    expected_exit_codes=(2,), json_channel="stderr",
                    output_contract="structured-unavailable-v1",
                ),
            )
            self.assertFalse(report["outputEquivalent"])
            self.assertEqual(report["uniqueNormalizedStderrOutputs"], 2)

    def test_evidence_manifest_fingerprints_binary_config_index_and_sidecars(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            rhizome = source / ".rhizome"
            rhizome.mkdir(parents=True)
            (source / "One.md").write_text("# One\n")
            (source / "Two.md").write_text("# Two\n")
            config = rhizome / "config.yml"
            index = rhizome / "db.sqlite"
            wal = rhizome / "db.sqlite-wal"
            shm = rhizome / "db.sqlite-shm"
            config.write_bytes(b"provider: test\n")
            index.write_bytes(b"index")
            wal.write_bytes(b"wal")
            shm.write_bytes(b"shm")
            binary = root / "rzm"
            binary.write_bytes(b"binary")
            manifest = EVIDENCE_MANIFEST(binary, source)
            self.assertEqual(
                manifest["binary"]["sha256"], hashlib.sha256(b"binary").hexdigest()
            )
            self.assertEqual(
                manifest["corpus"]["configSha256"],
                hashlib.sha256(b"provider: test\n").hexdigest(),
            )
            self.assertEqual(manifest["corpus"]["markdownFiles"], 2)
            self.assertRegex(manifest["corpus"]["markdownSha256"], r"^[0-9a-f]{64}$")
            self.assertEqual(
                manifest["corpus"]["index"]["sha256"], hashlib.sha256(b"index").hexdigest()
            )
            self.assertEqual(
                manifest["corpus"]["indexSidecars"]["wal"]["sha256"],
                hashlib.sha256(b"wal").hexdigest(),
            )
            self.assertEqual(
                manifest["corpus"]["indexSidecars"]["shm"]["sha256"],
                hashlib.sha256(b"shm").hexdigest(),
            )


if __name__ == "__main__":
    unittest.main()
