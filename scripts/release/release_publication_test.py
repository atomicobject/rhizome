#!/usr/bin/env python3
"""Publication boundary behavior without publishing or dispatching workflows."""
import subprocess
from pathlib import Path
import unittest
from unittest.mock import patch

from release_gateway import GatewayError
from release_publication import PublicationError, gate_direct_publication, verify_terminal_gateway_receipt

SHA = "a" * 40


class Commands:
    def __init__(self):
        self.head = SHA
        self.tag = "v1.2.3"
        self.release = SHA
        self.dirty = ""

    def __call__(self, command, cwd):
        command = tuple(command)
        result = ""
        if command[:2] == ("git", "describe"):
            if not self.tag:
                return subprocess.CompletedProcess(command, 1, "", "no tag")
            result = self.tag
        elif command[:3] == ("git", "rev-parse", "HEAD"):
            result = self.head
        elif command[:3] == ("git", "rev-parse", f"{self.tag}^{{commit}}"):
            result = self.head
        elif command[:2] == ("git", "status"):
            result = self.dirty
        elif command[:3] == ("git", "ls-remote", "origin"):
            result = f"{self.release}\trefs/heads/release"
        elif command[:3] == ("git", "rev-parse", "refs/remotes/origin/main"):
            result = SHA
        elif command[:3] == ("git", "rev-parse", "--git-path"):
            result = ".git/gateway.json"
        return subprocess.CompletedProcess(command, 0, result, "")


class PublicationTest(unittest.TestCase):
    def setUp(self):
        self.commands = Commands()
        self.calls = []

    def gate(self, *args, **kwargs):
        self.calls.append((args, kwargs))
        return {"attempt": {"state": "success"}}

    def invoke(self, **kwargs):
        return gate_direct_publication(Path("/repo"), runner=self.commands,
                                       gateway=self.gate, verifier=self.gate, **kwargs)

    def test_direct_invocation_runs_fresh_gate_and_keeps_receipt_in_git_directory(self):
        self.invoke()
        self.invoke()
        self.assertEqual(len(self.calls), 2)
        self.assertEqual(self.calls[0][0], (Path("/repo"), SHA, Path("/repo/.git/gateway.json")))

    def test_explicit_handoff_uses_actual_receipt_verifier(self):
        def forbidden(*args, **kwargs):
            self.fail("handoff must not deploy a second time")
        receipt = gate_direct_publication(Path("/repo"), runner=self.commands,
            gateway=forbidden, verifier=self.gate, receipt_path=Path("/tmp/receipt.json"), internal=True)
        self.assertEqual(receipt["attempt"]["state"], "success")
        self.assertTrue(self.calls[0][1]["internal"])

    def test_missing_tag_dirty_checkout_or_moved_branch_fail_before_dispatch(self):
        for attr, value in (("tag", ""), ("tag", "not-release"), ("dirty", " M source.go"),
                            ("release", "b" * 40)):
            with self.subTest(attr=attr, value=value):
                self.commands = Commands()
                setattr(self.commands, attr, value)
                with self.assertRaises(PublicationError):
                    self.invoke()
                self.assertEqual(self.calls, [])

    def test_moved_branch_after_wait_blocks_publication(self):
        def moved(*args, **kwargs):
            self.commands.release = "b" * 40
            return {}
        with self.assertRaisesRegex(PublicationError, "Canonical release branches"):
            gate_direct_publication(Path("/repo"), runner=self.commands, gateway=moved)

    def test_invalid_handoff_does_not_fall_back_to_dispatch(self):
        def invalid(*args, **kwargs):
            raise GatewayError("receipt mismatches")
        with self.assertRaisesRegex(GatewayError, "receipt mismatches"):
            gate_direct_publication(Path("/repo"), runner=self.commands,
                gateway=self.gate, verifier=invalid, receipt_path=Path("/tmp/receipt.json"))
        self.assertEqual(self.calls, [])

    def test_post_build_gate_allows_only_the_owned_unstaged_generated_entry(self):
        self.commands.dirty = " M pkg/app/web/assets/dist/index.html\n"
        with self.assertRaises(PublicationError):
            self.invoke()
        receipt = self.invoke(allow_generated=True)
        self.assertEqual(receipt["receiptPath"], "/repo/.git/gateway.json")
        for dirty in (" M source.go\n", "M  pkg/app/web/assets/dist/index.html\n",
                      " D pkg/app/web/assets/dist/index.html\n",
                      " M pkg/app/web/assets/dist/index.html\n M source.go\n"):
            self.commands.dirty = dirty
            with self.assertRaises(PublicationError):
                self.invoke(allow_generated=True)

    def test_terminal_receipt_mode_never_dispatches_or_borrows_live_handoff(self):
        receipt = {"source": {"repository": "atomicobject/rhizome", "release_commit": SHA},
                   "attempt": {"state": "success", "run_id": 42}}
        with patch("release_publication._load", return_value=receipt), \
             patch("release_publication._source", return_value=receipt["source"]) as source, \
             patch("release_publication.workflow_id", return_value=7), \
             patch("release_publication.find_run", return_value={"id": 42}), \
             patch("release_publication.fetch_run", return_value={"id": 42}), \
             patch("release_publication.successful", return_value=True), \
             patch("release_publication.ensure_gateway", side_effect=AssertionError("terminal mode dispatched")):
            result = gate_direct_publication(Path("/repo"), runner=self.commands,
                receipt_path=Path("/ci/terminal.json"), terminal_receipt=True)
            self.assertEqual(result["attempt"]["run_id"], 42)
            self.assertEqual(source.call_args.args[:3], (Path("/repo"), SHA, receipt))

    def test_invalid_terminal_receipt_fails_without_a_deployment_fallback(self):
        receipt = {"source": {}, "attempt": {"state": "success", "run_id": 42}}
        with patch("release_publication._load", return_value=receipt), \
             patch("release_publication._source", return_value={}), \
             patch("release_publication.workflow_id", return_value=7), \
             patch("release_publication.find_run", return_value=None), \
             patch("release_publication.ensure_gateway", side_effect=AssertionError("terminal mode dispatched")):
            with self.assertRaisesRegex(GatewayError, "run is missing"):
                verify_terminal_gateway_receipt(Path("/repo"), SHA, Path("/ci/terminal.json"))
        with self.assertRaisesRegex(PublicationError, "explicit receipt"):
            self.invoke(terminal_receipt=True)


if __name__ == "__main__":
    unittest.main()
