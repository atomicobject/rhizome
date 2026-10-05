#!/usr/bin/env python3

import re
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("cut_release.sh")


class CutReleasePortabilityTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.script = SCRIPT_PATH.read_text(encoding="utf-8")

    def test_uses_python3_on_macos_without_python_alias(self):
        bare_python = re.compile(r"(?m)^\s*python(?:\s|$)")
        self.assertNotRegex(self.script, bare_python)
        self.assertIn("python3", self.script)

    def test_delegates_to_the_python_release_orchestrator(self):
        self.assertIn('release_cli.py" cut-release', self.script)
        self.assertIn('"$@"', self.script)

    def test_wrapper_contains_no_release_mutation_logic(self):
        for command in ("git commit", "git tag", "goreleaser release"):
            with self.subTest(command=command):
                self.assertNotIn(command, self.script)

    def test_cleanup_does_not_delete_worktree_state(self):
        self.assertNotIn("rm -rf", self.script)
        self.assertNotIn("reset --hard", self.script)


if __name__ == "__main__":
    unittest.main()
