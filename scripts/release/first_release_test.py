#!/usr/bin/env python3
"""First and subsequent release contracts against disposable local Git remotes."""

from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import unittest

from collect_evidence import collect_release_evidence
from generate_notes import detect_prev_tag
from notes import GenerationResult
from release_cli import OrchestrationError, Orchestrator
from release_command import run_cli
from release_git import ReleaseGit, ReleaseGitError
from release_git_test import GitRepository
from release_theme import ReleaseTheme
from release_version import choose_release_version


class FirstReleaseTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.repo = GitRepository(Path(temp.name))
        self.initial = self.repo.git("rev-parse", "HEAD")
        for name, text in {
            ".gitignore": ".release/\n",
            "CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n\n- First usable release.\n",
            "pkg/vault/version/version.go": 'package version\n\nvar Version = "v0.50.5"\n',
            "pkg/vault/changelog/CHANGELOG.md": "# Changelog\n",
        }.items():
            target = self.repo.work / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(text)
        self.repo.git("add", ".")
        self.repo.git("commit", "-m", "Prepare release inputs")
        self.repo.git("push", "origin", "main")
        self.calls = []
        self.fail_push_after_success = False
        self.fail_apply_after_commit = False
        self.fail_signing_probe = False
        self.version = "v0.51.0"
        self.release = Orchestrator(
            self.repo.work,
            command_runner=self.run_command,
            note_generator=lambda *args, **kwargs: GenerationResult(
                recommended_bump="minor", rationale="First public release.",
                themes=(ReleaseTheme("First usable release.", "First usable release.", ("unreleased:1",)),),
            ),
            input_fn=lambda _: "", output_fn=lambda _: None,
            environ={"RZM_RELEASE_GITHUB_TOKEN": "synthetic", "TAURI_SIGNING_PRIVATE_KEY": "synthetic"},
        )

    def run_command(self, command, cwd):
        command = tuple(command)
        self.calls.append(command)
        if command == (sys.executable, "scripts/release/verify_desktop_signing.py") and self.fail_signing_probe:
            return subprocess.CompletedProcess(command, 1, "", "signing key does not match")
        if command[0] == "git":
            if command == ("git", "tag", self.version) and self.fail_apply_after_commit:
                self.fail_apply_after_commit = False
                return subprocess.CompletedProcess(command, 1, "", "interrupted before tag")
            result = subprocess.run(command, cwd=cwd, capture_output=True, text=True)
            if command[:3] == ("git", "push", "--atomic") and self.fail_push_after_success:
                self.fail_push_after_success = False
                return subprocess.CompletedProcess(command, 1, "", "connection lost after push")
            return result
        if len(command) > 1 and command[1].endswith("release_publish.py"):
            self.repo.git("push", "origin", f"refs/tags/{self.version}")
        elif command[0] == "trash":
            path = Path(command[1])
            path.rename(path.with_name("completed-" + path.name))
        else:
            self.assertEqual(command[0], sys.executable)
            self.assertIn(command[1], ("scripts/release/run_clean.py", "scripts/teamkeys/release.py",
                                       "scripts/release/verify_desktop_signing.py"))
        return subprocess.CompletedProcess(command, 0, "", "")

    def test_first_release_includes_initial_commit_and_publishes_then_second_release_uses_tag(self):
        self.assertEqual(detect_prev_tag(self.repo.work), "")
        run = self.release.plan()
        self.assertEqual((run.plan.base_tag, run.plan.base_commit), ("", ""))
        self.assertEqual(run.plan.selected_version, "v0.51.0")
        initial = run.evidence.delivery_units[0]
        self.assertEqual(initial.mainline_commit.sha, self.initial)
        self.assertEqual(initial.mainline_commit.parents, ())
        self.assertEqual(initial.changed_paths, ("base.txt",))
        self.assertEqual((initial.insertions, initial.deletions), (1, 0))
        self.assertEqual(self.repo.git("tag", "--list"), "")
        self.assertTrue(run.run_dir.name.startswith("first-release-"))

        self.release.build("", run.plan.head_commit)
        self.release.apply("", run.plan.head_commit, accept_degraded_evidence=True)
        self.fail_push_after_success = True
        self.release.publish("", run.plan.head_commit)
        first_release = self.repo.git("rev-parse", "HEAD")
        self.assertEqual(self.repo.remote_ref("refs/heads/main"), first_release)
        self.assertEqual(self.repo.remote_ref("refs/heads/release"), first_release)
        self.assertEqual(self.repo.remote_ref("refs/tags/v0.51.0"), first_release)
        self.assertEqual(sum(call[:3] == ("git", "push", "--atomic") for call in self.calls), 1)

        next_head = self.repo.commit("next.txt", "next\n", "Add next feature")
        self.repo.git("push", "origin", "main")
        self.version = "v0.52.0"
        second = self.release.plan()
        self.assertEqual((second.plan.base_tag, second.plan.base_commit), ("v0.51.0", first_release))
        self.assertEqual(second.plan.selected_version, "v0.52.0")
        self.assertEqual([unit.mainline_commit.sha for unit in second.evidence.delivery_units], [next_head])
        self.release.resume(second.plan.base_tag, next_head, accept_degraded_evidence=True)
        self.assertEqual(self.repo.remote_ref("refs/tags/v0.52.0"), self.repo.git("rev-parse", "HEAD"))

    def test_first_release_recovers_commit_created_before_tag_checkpoint(self):
        run = self.release.plan()
        self.release.build("", run.plan.head_commit)
        self.fail_apply_after_commit = True
        with self.assertRaisesRegex(OrchestrationError, "interrupted before tag") as raised:
            self.release.apply("", run.plan.head_commit, accept_degraded_evidence=True)
        retry = shlex.split(raised.exception.next_command)[2:]
        self.assertEqual(retry, ["resume", "--base", "", "--head", run.plan.head_commit])
        release_commit = self.repo.git("rev-parse", "HEAD")
        run_cli(self.release, retry + ["--accept-degraded-evidence"])
        self.assertEqual(self.repo.git("rev-parse", "HEAD"), release_commit)
        self.assertEqual(self.repo.remote_ref("refs/tags/v0.51.0"), release_commit)

    def test_new_tag_invalidates_first_release_before_build(self):
        run = self.release.plan()
        self.repo.git("tag", "v0.0.1")
        with self.assertRaisesRegex(OrchestrationError, "tags appeared"):
            self.release.build("", run.plan.head_commit)
        with self.assertRaisesRegex(OrchestrationError, "no existing version tags"):
            self.release.plan(base="", new_plan=True)

    def test_non_version_tags_do_not_fabricate_a_previous_release(self):
        self.repo.git("tag", "project-bootstrap")
        run = self.release.plan()
        self.assertEqual(run.plan.base_tag, "")
        self.assertEqual(run.plan.selected_version, "v0.51.0")

    def test_mismatched_current_signing_key_stops_apply_before_mutation(self):
        run = self.release.plan()
        self.release.build("", run.plan.head_commit)
        self.fail_signing_probe = True
        with self.assertRaisesRegex(OrchestrationError, "signing key does not match"):
            self.release.apply("", run.plan.head_commit, accept_degraded_evidence=True)
        self.assertEqual(self.repo.git("rev-parse", "HEAD"), run.plan.head_commit)
        self.assertEqual(self.repo.git("status", "--porcelain"), "")
        self.assertEqual(self.repo.git("tag", "--list"), "")

    def test_divergent_release_branch_rejects_atomic_first_publication(self):
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("checkout", "-b", "release")
        conflicting = self.repo.commit("other.txt", "other", "Other release")
        self.repo.git("push", "origin", "release")
        self.repo.git("checkout", "main")
        candidate = self.repo.commit("release.txt", "release", "Release v0.51.0")
        git = ReleaseGit(self.repo.work)
        with self.assertRaisesRegex(ReleaseGitError, "atomically publish"):
            git.publish_canonical_branches(git.branch_targets("main", candidate, "v0.51.0"))
        self.assertEqual(self.repo.remote_ref("refs/heads/main"), base)
        self.assertEqual(self.repo.remote_ref("refs/heads/release"), conflicting)

    def test_unreachable_tag_does_not_silently_become_first_release(self):
        self.repo.git("checkout", "--orphan", "other-history")
        self.repo.git("commit", "-m", "Other history")
        self.repo.git("tag", "v9.0.0")
        self.repo.git("checkout", "main")
        with self.assertRaises(OrchestrationError):
            self.release.plan()

    def test_single_initial_commit_collects_content_without_a_prior_commit(self):
        evidence = collect_release_evidence(self.repo.work, "", self.initial)
        self.assertEqual(len(evidence.delivery_units), 1)
        self.assertEqual(evidence.delivery_units[0].changed_paths, ("base.txt",))

    def test_first_version_can_be_custom_or_supplied(self):
        answers = iter(["4", "v1.0.0"])
        self.assertEqual(choose_release_version(
            "v0.50.5", "minor", supplied=None, environ={}, input_fn=lambda _: next(answers), output_fn=lambda _: None,
        ), "v1.0.0")
        self.assertEqual(choose_release_version(
            "v0.50.5", "minor", supplied="v2.0.0", environ={}, input_fn=lambda _: self.fail("unexpected prompt"),
        ), "v2.0.0")


if __name__ == "__main__":
    unittest.main()
