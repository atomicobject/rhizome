#!/usr/bin/env python3
"""Publication boundary behavior without publishing or dispatching workflows."""
import subprocess
from pathlib import Path
import unittest

from release_gateway import GatewayError
from release_publication import PublicationError, gate_direct_publication

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


if __name__ == "__main__":
    unittest.main()
