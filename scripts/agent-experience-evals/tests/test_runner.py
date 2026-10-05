from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
import types
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

import runner
import score
import environment_overlay


def pilot_run(case_id: str, arm: str, repo: str):
    return {
        "id": f"{case_id.lower()}-{arm}", "case_id": case_id, "arm": arm,
        "repo": repo, "binary": "/tmp/rzm", "binary_sha256": "a" * 64,
        "source_sha": "b" * 40, "prompt": runner.PILOT_PROMPTS[case_id],
        "fixture": {"README.md": "c" * 64}, "fixture_commit": "d" * 40,
        "fixture_tree": "e" * 40, "guidance": {"AGENTS.md": "f" * 64},
        "support": repo + "-support", "setup_sha256": "1" * 64,
        "build_receipt": repo + "-build.json", "build_receipt_sha256": "2" * 64,
        "isolation": {"profile": "/tmp/profile"},
    }


def valid_manifest(repo: str = "/tmp/repo"):
    return {
        "schema_version": 1,
        "campaign_id": "pilot-1",
        "model": "gpt-5.6-luna",
        "reasoning": "xhigh",
        "max_launches": 4,
        "per_run_seconds": 600,
        "total_seconds": 2400,
        "runs": [pilot_run("A01", "baseline", repo + "-a01"),
                 pilot_run("A02", "baseline", repo + "-a02")],
    }


class RunnerTests(unittest.TestCase):
    def write_manifest(self, directory: str, manifest=None) -> Path:
        path = Path(directory) / "manifest.json"
        path.write_text(json.dumps(manifest or valid_manifest()))
        return path

    def test_rejects_wrong_model_and_limits(self):
        manifest = valid_manifest()
        manifest["model"] = "other"
        with self.assertRaises(runner.RunnerError):
            runner.validate_manifest(manifest)
        manifest = valid_manifest()
        manifest["runs"][0]["id"] = "../escape"
        with self.assertRaises(runner.RunnerError):
            runner.validate_manifest(manifest)
        manifest = valid_manifest()
        manifest["max_launches"] = 5
        with self.assertRaises(runner.RunnerError):
            runner.validate_manifest(manifest)

    def test_list_does_not_import_adapter(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = self.write_manifest(directory)
            with mock.patch.object(runner, "adapter_module", side_effect=AssertionError("adapter loaded")):
                self.assertEqual(runner.main(["list", "--manifest", str(manifest)]), 0)

    def test_environment_overlay_is_manifest_bound_and_does_not_change_ledger(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = valid_manifest()
            for baseline in list(manifest["runs"]):
                candidate = dict(baseline)
                candidate.update(id=baseline["id"].replace("baseline", "candidate"),
                                 arm="candidate", source_sha="c" * 40)
                manifest["runs"].append(candidate)
            output = root / "output"
            ledger = runner.load_ledger(output, manifest)
            runner.reserve(ledger, manifest, manifest["runs"][0], output)
            before = (output / "ledger.json").read_bytes()
            manifest_hash = runner.canonical_hash(manifest)
            entries = {}
            for run_id in environment_overlay.REMAINING_RUN_IDS:
                home = root / run_id / "home"
                entries[run_id] = {"clean_home": str(home), "codex_home": str(home / ".codex"),
                                   "config_sha256": "a" * 64,
                                   "execution_repo": str(root / run_id / "repo"),
                                   "protected_paths": [str(root / "protected" / run_id)]}
            overlay = {"schema_version": 1, "kind": "clean-codex-environment-continuation",
                       "manifest_sha256": manifest_hash,
                       "expected_run_ids": list(environment_overlay.REMAINING_RUN_IDS),
                       "runs": entries, "support_root": str(root / "support"),
                       "output_root": str(output),
                       "ledger_checkpoint": {
                           "ledger_sha256": __import__("hashlib").sha256(before).hexdigest(),
                           "launches_reserved": 1,
                           "reservation_head": ledger["reservations"][0]["hash"],
                           "consumed_run_ids": ["a01-baseline"],
                           "consumed_run_hash": ledger["runs"]["a01-baseline"]["run_hash"],
                           "total_elapsed_seconds": 0.0}}
            path = root / "environment.json"
            path.write_text(json.dumps(overlay))
            loaded, _ = environment_overlay.load(path, manifest, manifest_hash)
            self.assertEqual(list(loaded["runs"]), list(environment_overlay.REMAINING_RUN_IDS))
            environment_overlay.validate_ledger(loaded, output)
            self.assertEqual((output / "ledger.json").read_bytes(), before)
            with self.assertRaises(runner.RunnerError):
                environment_overlay.validate_ledger(loaded, root / "fresh-output")
            adapter = types.SimpleNamespace(overlay=loaded,
                effective_run=lambda run: {**run, "original_repo": run["repo"],
                                            "repo": loaded["runs"][run["id"]]["execution_repo"]})
            with mock.patch.object(runner, "adapter_module", return_value=adapter):
                self.assertEqual(runner.main([
                    "run", "--manifest", str(self.write_manifest(directory, manifest)),
                    "--environment", str(path), "--output", str(output),
                    "--run-id", "a01-candidate"]), 2)
            overlay["manifest_sha256"] = "0" * 64
            path.write_text(json.dumps(overlay))
            with self.assertRaises(runner.RunnerError):
                environment_overlay.load(path, manifest, manifest_hash)

    def test_preflight_does_not_construct_or_launch_command(self):
        adapter = types.SimpleNamespace(
            preflight=lambda manifest, run: {"ready": True},
            command=lambda *args: (_ for _ in ()).throw(AssertionError("command constructed")),
            environment=lambda *args: (_ for _ in ()).throw(AssertionError("environment read")),
        )
        with tempfile.TemporaryDirectory() as directory:
            manifest = self.write_manifest(directory)
            with mock.patch.object(runner, "adapter_module", return_value=adapter), \
                    mock.patch("subprocess.Popen", side_effect=AssertionError("process launched")):
                self.assertEqual(runner.main(["preflight", "--manifest", str(manifest)]), 0)

    def test_reservation_is_atomic_and_reuse_is_refused(self):
        manifest = valid_manifest()
        run = manifest["runs"][0]
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            ledger = runner.load_ledger(output, manifest)
            runner.reserve(ledger, manifest, run, output)
            persisted = json.loads((output / "ledger.json").read_text())
            self.assertEqual(persisted["launches_reserved"], 1)
            self.assertEqual(persisted["runs"][run["id"]]["status"], "reserved")
            self.assertEqual(len(persisted["reservations"]), 1)
            with self.assertRaises(runner.RunnerError):
                runner.reserve(ledger, manifest, run, output)

    def test_manifest_hash_mismatch_and_concurrent_lock_are_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            manifest = valid_manifest()
            runner.load_ledger(output, manifest)
            changed = valid_manifest()
            changed["per_run_seconds"] = 599
            with self.assertRaises(runner.RunnerError):
                runner.load_ledger(output, changed)
            with runner.OutputLock(output):
                with self.assertRaises(runner.RunnerError):
                    with runner.OutputLock(output):
                        pass

    def test_output_must_be_outside_fixture_repositories(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory) / "repo"
            repo.mkdir()
            with self.assertRaises(runner.RunnerError):
                manifest = valid_manifest(str(repo))
                manifest["runs"][0]["repo"] = str(repo)
                runner.validate_output(repo / "evidence", manifest["runs"])

    def test_campaign_allows_append_but_rejects_changed_prior_run(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            manifest = valid_manifest()
            ledger = runner.load_ledger(output, manifest)
            runner.reserve(ledger, manifest, manifest["runs"][0], output)
            extended = json.loads(json.dumps(manifest))
            for baseline in list(extended["runs"]):
                candidate = dict(baseline)
                candidate.update(id=baseline["id"].replace("baseline", "candidate"),
                                 arm="candidate", source_sha="c" * 40)
                extended["runs"].append(candidate)
            appended = runner.load_ledger(output, extended)
            self.assertEqual(appended["launches_reserved"], 1)
            self.assertEqual(appended["declared_order"], [
                "a01-baseline", "a02-baseline", "a01-candidate", "a02-candidate"])
            changed = json.loads(json.dumps(extended))
            changed["runs"][0]["prompt"] = "changed"
            with self.assertRaises(runner.RunnerError):
                runner.load_ledger(output, changed)

    def test_tampered_reservation_chain_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            manifest = valid_manifest()
            ledger = runner.load_ledger(output, manifest)
            runner.reserve(ledger, manifest, manifest["runs"][0], output)
            ledger["reservations"][0]["run_hash"] = "0" * 64
            runner.atomic_json(output / "ledger.json", ledger)
            with self.assertRaises(runner.RunnerError):
                runner.load_ledger(output, manifest)

    def test_fake_process_receives_exact_prompt_and_stream_is_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            result = Path(directory)
            executable = Path(directory) / "fake.py"
            executable.write_text(
                "import json, pathlib, sys\n"
                "prompt=sys.stdin.read()\n"
                "pathlib.Path(sys.argv[1]).write_text(prompt)\n"
                "print(json.dumps({'type':'tool_call','name':'rhizome.search'}), flush=True)\n"
                "print('{malformed', flush=True)\n"
                "print('warning', file=sys.stderr, flush=True)\n"
            )
            received = result / "received.txt"
            outcome = runner.execute(
                [sys.executable, str(executable), str(received)], dict(os.environ),
                "literal $() `ticks`\n", result, 5,
            )
            self.assertEqual(outcome["status"], "complete")
            self.assertEqual(received.read_text(), "literal $() `ticks`\n")
            self.assertIn(b"{malformed", (result / "events.jsonl").read_bytes())
            self.assertEqual((result / "stderr.txt").read_text(), "warning\n")

    def test_hung_fake_process_is_terminated(self):
        with tempfile.TemporaryDirectory() as directory:
            result = Path(directory)
            started = time.monotonic()
            outcome = runner.execute(
                [sys.executable, "-c", "import time; time.sleep(30)"],
                dict(os.environ), "", result, 0.1,
            )
            self.assertTrue(outcome["timed_out"])
            self.assertEqual(outcome["status"], "failed")
            self.assertLess(time.monotonic() - started, 0.6)

    def test_postlaunch_artifact_failure_preserves_process_result(self):
        adapter = types.SimpleNamespace(
            preflight=lambda manifest, run: {"ready": True},
            command=lambda manifest, run, result: ["fake"],
            environment=lambda manifest, run: {},
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / "repo"
            output = root / "evidence"
            repo.mkdir()
            subprocess.run(["git", "init", "-q", str(repo)], check=True)
            manifest = valid_manifest(str(root / "fixture"))
            run = manifest["runs"][0]
            run["repo"] = str(repo)
            ledger = runner.load_ledger(output, manifest)

            def fake_execute(argv, env, prompt, result_dir, timeout):
                (result_dir / "events.jsonl").write_text('{"type":"turn.completed"}\n')
                return {"status": "complete", "exit_code": 0, "timed_out": False}

            with mock.patch.object(runner, "execute", side_effect=fake_execute), \
                    mock.patch.object(runner.evidence, "capture_provenance", return_value={}), \
                    mock.patch.object(runner.evidence, "snapshot",
                                      side_effect=[{"files": {}, "transient": {}}, RuntimeError("after failed")]):
                with self.assertRaises(runner.RunnerError):
                    runner.run_one(adapter, manifest, runner.canonical_hash(manifest), run, output, ledger)
            persisted = json.loads((output / "ledger.json").read_text())["runs"][run["id"]]
            self.assertEqual(persisted["status"], "complete")
            self.assertEqual(persisted["exit_code"], 0)
            self.assertEqual(persisted["artifact_errors"][0]["artifact"], "after.json")
            self.assertIn("evidence", persisted)

    def test_incomplete_events_and_invalid_not_applicable_are_unassessable(self):
        entry = {
            "id": "a01-baseline", "case_id": "A01", "arm": "baseline", "status": "complete",
            "checks": {"passed": True}, "evidence": {"incomplete": True},
        }
        review = {"criteria": {key: "pass" for key in score.MANDATORY_CRITERIA}}
        result = score.score_run(entry, review)
        self.assertFalse(result["task_passed"])
        self.assertFalse(result["evidence_complete"])
        entry["evidence"] = {"incomplete": False}
        review["criteria"]["correctness"] = "not-applicable"
        result = score.score_run(entry, review)
        self.assertEqual(result["criteria"]["correctness"], "unassessable")
        self.assertFalse(result["task_passed"])
        entry["artifact_errors"] = [{"artifact": "after.json", "error": "failed"}]
        review["criteria"]["correctness"] = "pass"
        result = score.score_run(entry, review)
        self.assertFalse(result["evidence_complete"])


if __name__ == "__main__":
    unittest.main()
