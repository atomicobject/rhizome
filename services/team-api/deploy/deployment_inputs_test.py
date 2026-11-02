import subprocess
from pathlib import Path
import tempfile
import unittest

from deployment_inputs import validate_inputs, validate_source


class DeploymentInputTest(unittest.TestCase):
    def select(self, source="a" * 40, request="b" * 32, image="", **kwargs):
        return validate_inputs(kwargs.get("repository", "atomicobject/rhizome"),
                               kwargs.get("ref", "refs/heads/main"), source, request, image)

    def test_source_mode_pins_the_image_to_the_source(self):
        selected = self.select()
        self.assertEqual(("source", "a" * 40, "a" * 40),
                         (selected.mode, selected.image_tag, selected.source_commit))

    def test_rollback_does_not_require_source_or_request(self):
        selected = self.select("", "", "c" * 40)
        self.assertEqual(("image", "c" * 40, ""),
                         (selected.mode, selected.image_tag, selected.source_commit))

    def test_untrusted_dispatch_context_is_rejected(self):
        for context in ({"repository": "example/rhizome"}, {"ref": "refs/heads/release"},
                        {"ref": "refs/tags/main"}):
            with self.subTest(context=context), self.assertRaises(ValueError):
                self.select(**context)

    def test_missing_malformed_and_mixed_inputs_are_rejected(self):
        cases = [("", "", ""), ("a" * 40, "", ""), ("", "b" * 32, ""),
                 ("a" * 39, "b" * 32, ""), ("A" * 40, "b" * 32, ""),
                 ("a" * 40, "x" * 32, ""), ("a" * 40, "b" * 32 + "\n", ""),
                 ("main", "b" * 32, ""), ("--upload-pack=x", "b" * 32, ""),
                 ("a" * 40, "b" * 32, "c" * 40), ("", "b" * 32, "c" * 40),
                 ("", "", "main"), ("", "", "A" * 40)]
        for source, request, image in cases:
            with self.subTest(source=source, request=request, image=image), self.assertRaises(ValueError):
                self.select(source, request, image)


class SourceTrustTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Synthetic Operator")
        self.git("config", "user.email", "operator@example.invalid")
        self.base = self.commit("base")
        self.tip = self.commit("next")
        self.git("update-ref", "refs/remotes/origin/main", self.tip)

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.root, check=True,
                              capture_output=True, text=True).stdout.strip()

    def commit(self, content):
        (self.root / "fixture").write_text(content)
        self.git("add", "fixture")
        self.git("commit", "-m", content)
        return self.git("rev-parse", "HEAD")

    def test_current_and_historical_main_work_without_a_release_branch(self):
        validate_source(self.root, self.tip)
        validate_source(self.root, self.base)
        self.assertEqual("", self.git("branch", "--list", "release"))

    def test_missing_commit_and_unintegrated_branch_are_rejected(self):
        self.git("checkout", "-b", "unintegrated", self.base)
        unrelated = self.commit("unreviewed")
        for source in (unrelated, "f" * 40, "main", "a" * 40 + "\n"):
            with self.subTest(source=source), self.assertRaises(ValueError):
                validate_source(self.root, source)

    def test_a_hotfix_becomes_trusted_after_integration(self):
        self.git("checkout", "-b", "release", self.base)
        (self.root / "hotfix").write_text("synthetic hotfix")
        self.git("add", "hotfix")
        self.git("commit", "-m", "hotfix")
        hotfix = self.git("rev-parse", "HEAD")
        self.git("checkout", "main")
        self.git("merge", "--no-ff", "release", "-m", "Merge hotfix")
        self.git("update-ref", "refs/remotes/origin/main", self.git("rev-parse", "HEAD"))
        validate_source(self.root, hotfix)
