#!/usr/bin/env python3
"""Behavioral contracts for correlation, persistence, and publisher handoff."""

import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import release_gateway as gate
from release_gateway_runs import GatewayError, REPOSITORY, WORKFLOW_PATH


SOURCE = {"repository": REPOSITORY, "release_commit": "a" * 40,
          "source_commit": "b" * 40, "fingerprint": "c" * 64}


class Clock:
    def __init__(self):
        self.value = 0

    def __call__(self):
        return self.value

    def sleep(self, seconds):
        self.value += seconds


class GitHub:
    def __init__(self):
        self.calls = []
        self.runs = []
        self.dispatch_uncertain = False
        self.create_run = True
        self.status = "completed"
        self.conclusion = "success"
        self.list_error = False
        self.truncated = False

    def run(self, request_id="f" * 32, **changes):
        run_id = 100 + len(self.runs)
        result = {"id": run_id, "workflow_id": 7, "path": WORKFLOW_PATH,
                  "repository": {"full_name": REPOSITORY}, "event": "workflow_dispatch",
                  "head_branch": "main", "display_title": f"Deploy team API {request_id} {SOURCE['source_commit']}",
                  "html_url": f"https://github.com/{REPOSITORY}/actions/runs/{run_id}",
                  "status": self.status, "conclusion": self.conclusion}
        result.update(changes)
        return result

    def __call__(self, argv):
        argv = tuple(argv)
        self.calls.append(argv)
        if argv[:3] == ("gh", "workflow", "run"):
            request_id = next(value.split("=", 1)[1] for value in argv if value.startswith("request_id="))
            if self.create_run:
                self.runs.append(self.run(request_id))
            if self.dispatch_uncertain:
                raise RuntimeError("connection ended after server accepted")
            return ""
        if argv[:2] != ("gh", "api"):
            raise AssertionError(argv)
        endpoint = argv[2]
        if "/workflows/" in endpoint and "/runs?" not in endpoint:
            return json.dumps({"id": 7, "path": WORKFLOW_PATH})
        if "/runs?" in endpoint:
            if self.list_error:
                raise RuntimeError("API unavailable")
            return json.dumps([{"total_count": len(self.runs) + int(self.truncated),
                                "workflow_runs": self.runs}])
        if "/runs/" in endpoint:
            run_id = int(endpoint.rsplit("/", 1)[1])
            return json.dumps(next(run for run in self.runs if run["id"] == run_id))
        raise AssertionError(argv)

    @property
    def dispatches(self):
        return [argv for argv in self.calls if argv[:3] == ("gh", "workflow", "run")]


class GatewayTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.receipt = self.root / "run/gateway.json"
        self.github = GitHub()
        self.clock = Clock()
        self.output = []
        self.source_calls = []

        def resolve(root, release, *, saved_source, internal, runner):
            self.source_calls.append(saved_source)
            if release != SOURCE["release_commit"] or (saved_source and saved_source != SOURCE["source_commit"]):
                raise gate.SourceError("source identity mismatch")
            return copy.deepcopy(SOURCE)

        self.mock_source = patch.object(gate, "resolve_source", side_effect=resolve)
        self.mock_source.start()
        self.addCleanup(self.mock_source.stop)

    def ensure(self, **kwargs):
        return gate.ensure_gateway(self.root, SOURCE["release_commit"], self.receipt,
                                   runner=self.github, clock=self.clock,
                                   sleep=self.clock.sleep, output=self.output.append, **kwargs)

    def verify(self, release=None):
        return gate.verify_gateway_receipt(self.root, release or SOURCE["release_commit"],
                                           self.receipt, runner=self.github)

    def seed(self, state="pending", **attempt):
        request_id = "f" * 32
        receipt = {"version": 1, "source": copy.deepcopy(SOURCE),
                   "attempt": {"request_id": request_id, "state": state, **attempt}}
        gate._save(self.receipt, receipt)
        return receipt

    def test_dispatch_only_sends_public_source_and_correlation(self):
        result = self.ensure(internal=True)
        self.assertEqual(result["attempt"]["state"], "success")
        dispatch = self.github.dispatches[0]
        self.assertEqual(dispatch[:8], ("gh", "workflow", "run", "team-api-deploy.yml",
                                       "--repo", REPOSITORY, "--ref", "main"))
        self.assertEqual(dispatch[8:11], ("-f", f"source_commit={SOURCE['source_commit']}", "-f"))
        self.assertRegex(dispatch[11], r"^request_id=[0-9a-f]{32}$")
        self.assertNotIn(SOURCE["release_commit"], " ".join(dispatch))
        self.assertEqual(self.output, [f"Gateway deployment: {result['attempt']['run_url']}"])
        self.assertEqual(self.receipt.stat().st_mode & 0o777, 0o600)

    def test_selection_is_persisted_before_dispatch(self):
        original = self.github

        def runner(argv):
            if tuple(argv)[:3] == ("gh", "workflow", "run"):
                saved = json.loads(self.receipt.read_text())
                self.assertEqual(saved["source"], SOURCE)
                self.assertEqual(saved["attempt"]["state"], "dispatch_uncertain")
            return original(argv)

        gate.ensure_gateway(self.root, SOURCE["release_commit"], self.receipt,
                            runner=runner, clock=self.clock, sleep=self.clock.sleep, output=lambda _: None)

    def test_unrelated_newest_run_is_ignored(self):
        self.github.runs.append(self.github.run(display_title="Deploy team API unrelated"))
        result = self.ensure()
        self.assertEqual(result["attempt"]["run_id"], 101)

    def test_duplicate_matches_block_publication(self):
        self.seed()
        self.github.runs = [self.github.run(), self.github.run(id=102)]
        with self.assertRaisesRegex(GatewayError, "Multiple"):
            self.ensure()
        self.assertFalse(self.github.dispatches)

    def test_wrong_event_branch_workflow_repository_or_path_block(self):
        for changes in ({"event": "push"}, {"head_branch": "release"}, {"workflow_id": 99},
                        {"repository": {"full_name": "other/repo"}}, {"path": "other.yml"}):
            with self.subTest(changes=changes):
                self.seed()
                self.github.runs = [self.github.run(**changes)]
                with self.assertRaisesRegex(GatewayError, "identity"):
                    self.ensure()

    def test_uncertain_dispatch_reconciles_one_run(self):
        self.github.dispatch_uncertain = True
        result = self.ensure()
        self.assertEqual(result["attempt"]["state"], "success")
        self.assertEqual(len(self.github.dispatches), 1)
        self.assertIn("uncertain", self.output[0])

    def test_missing_uncertain_dispatch_never_redispatches_on_resume(self):
        self.github.dispatch_uncertain = True
        self.github.create_run = False
        for _ in range(2):
            with self.assertRaisesRegex(GatewayError, "No correlated"):
                self.ensure()
        self.assertEqual(len(self.github.dispatches), 1)
        self.assertEqual(json.loads(self.receipt.read_text())["attempt"]["state"], "dispatch_uncertain")

    def test_prepared_receipt_reconciles_without_blind_dispatch(self):
        self.seed(state="prepared")
        self.github.runs = [self.github.run()]
        self.assertEqual(self.ensure()["attempt"]["state"], "success")
        self.assertFalse(self.github.dispatches)

    def test_wait_allows_current_workflow_timeout_plus_approval_margin(self):
        self.assertGreater(gate.WAIT_SECONDS, 150 * 60)

    def test_pending_run_resumes_without_dispatch(self):
        self.seed(run_id=100, run_url=f"https://github.com/{REPOSITORY}/actions/runs/100")
        self.github.runs = [self.github.run()]
        self.assertEqual(self.ensure()["attempt"]["state"], "success")
        self.assertFalse(self.github.dispatches)
        self.assertEqual(self.source_calls, [SOURCE["source_commit"]])

    def test_known_pending_run_prints_url_before_wait(self):
        self.seed(run_id=100, run_url=f"https://github.com/{REPOSITORY}/actions/runs/100")
        self.github.runs = [self.github.run(status="waiting", conclusion=None)]
        with patch.object(gate, "WAIT_SECONDS", 30):
            with self.assertRaisesRegex(GatewayError, "Timed out"):
                self.ensure()
        self.assertEqual(self.output[0], f"Gateway deployment: https://github.com/{REPOSITORY}/actions/runs/100")
        self.assertEqual(json.loads(self.receipt.read_text())["attempt"]["state"], "pending")

    def test_failure_and_cancellation_retained_then_fresh_attempt(self):
        for conclusion in ("failure", "cancelled", "timed_out", None):
            with self.subTest(conclusion=conclusion):
                self.github.conclusion = conclusion
                with self.assertRaisesRegex(GatewayError, "did not succeed"):
                    self.ensure()
                self.assertEqual(json.loads(self.receipt.read_text())["attempt"]["state"], "failed")
        self.assertEqual(len(self.github.dispatches), 4)

    def test_prior_success_gets_fresh_attempt_with_same_source(self):
        first = self.ensure()
        second = self.ensure()
        self.assertNotEqual(first["attempt"]["request_id"], second["attempt"]["request_id"])
        self.assertEqual(first["source"], second["source"])
        self.assertEqual(len(self.github.dispatches), 2)
        self.assertEqual(self.source_calls[-1], SOURCE["source_commit"])

    def test_api_failure_or_truncation_retains_receipt_without_retry(self):
        for setting in ("list_error", "truncated"):
            with self.subTest(setting=setting):
                self.seed()
                setattr(self.github, setting, True)
                with self.assertRaises(GatewayError):
                    self.ensure()
                setattr(self.github, setting, False)
                self.assertTrue(self.receipt.exists())
        self.assertFalse(self.github.dispatches)

    def test_handoff_rechecks_success_and_source_identity(self):
        result = self.ensure()
        self.assertEqual(self.verify(), result)
        self.assertEqual(len(self.github.dispatches), 1)
        self.github.runs[-1]["conclusion"] = "cancelled"
        with self.assertRaises(GatewayError):
            self.verify()
        with self.assertRaises(GatewayError):
            self.verify(release="d" * 40)

    def test_receipt_from_ended_publisher_requires_fresh_deployment(self):
        self.ensure()
        receipt = json.loads(self.receipt.read_text())
        receipt["attempt"]["publisher"] = {"pid": 1, "started": "old invocation"}
        gate._save(self.receipt, receipt)
        renewed = self.verify()
        self.assertEqual(len(self.github.dispatches), 2)
        self.assertNotEqual(renewed["attempt"]["request_id"], receipt["attempt"]["request_id"])
        self.assertEqual(renewed["source"], receipt["source"])

    def test_reused_pid_with_different_start_is_not_a_live_handoff(self):
        receipt = self.ensure()
        receipt["attempt"]["publisher"]["started"] = "previous process with same pid"
        self.assertFalse(gate._live_handoff(receipt["attempt"]))

    @unittest.skipUnless(os.name == "posix" and shutil.which("ps"), "process ancestry requires ps")
    def test_child_publisher_can_borrow_its_live_parents_receipt(self):
        receipt = self.ensure()
        result = subprocess.check_output((sys.executable, "-c",
            "import sys,json; sys.path.insert(0,sys.argv[1]); "
            "import release_gateway; print(release_gateway._live_handoff(json.loads(sys.argv[2])))",
            str(Path(gate.__file__).parent), json.dumps(receipt["attempt"])), text=True)
        self.assertEqual(result.strip(), "True")

    def test_handoff_refuses_missing_or_pending_receipt(self):
        with self.assertRaises(GatewayError):
            self.verify()
        self.seed()
        with self.assertRaises(GatewayError):
            self.verify()

    def test_handoff_rejects_tampered_fingerprint_or_run_id(self):
        self.ensure()
        original = json.loads(self.receipt.read_text())
        for target in ("fingerprint", "run_id"):
            receipt = copy.deepcopy(original)
            if target == "fingerprint":
                receipt["source"][target] = "0" * 64
            else:
                receipt["attempt"][target] = 999
            gate._save(self.receipt, receipt)
            with self.assertRaises(GatewayError):
                self.verify()


if __name__ == "__main__":
    unittest.main()
