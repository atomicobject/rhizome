#!/usr/bin/env python3
"""Real-Git contract tests for canonical release ref publication."""

from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from release_git import ReleaseGit, ReleaseGitError


class GitRepository:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.origin = root / "origin.git"
        self.work = root / "work"
        self.other = root / "other"
        self.run("git", "init", "--bare", str(self.origin), cwd=root)
        self.run("git", "clone", str(self.origin), str(self.work), cwd=root)
        self.configure(self.work)
        self.git("checkout", "-b", "main")
        self.commit("base.txt", "base", "base")
        self.git("push", "-u", "origin", "main")

    @staticmethod
    def run(*args: str, cwd: Path) -> str:
        return subprocess.run(
            args, cwd=cwd, check=True, text=True, capture_output=True
        ).stdout.strip()

    @classmethod
    def configure(cls, repo: Path) -> None:
        cls.run("git", "config", "user.name", "Release Test", cwd=repo)
        cls.run("git", "config", "user.email", "release@example.com", cwd=repo)

    def git(self, *args: str, cwd: Path | None = None) -> str:
        return self.run("git", *args, cwd=cwd or self.work)

    def commit(self, name: str, content: str, message: str, cwd: Path | None = None) -> str:
        repo = cwd or self.work
        (repo / name).write_text(content, encoding="utf-8")
        self.git("add", name, cwd=repo)
        self.git("commit", "-m", message, cwd=repo)
        return self.git("rev-parse", "HEAD", cwd=repo)

    def clone_other(self) -> None:
        self.run("git", "clone", str(self.origin), str(self.other), cwd=self.root)
        self.configure(self.other)
        self.git("checkout", "main", cwd=self.other)

    def remote_ref(self, name: str) -> str:
        return self.git("--git-dir", str(self.origin), "rev-parse", name)


class ReleaseGitContractTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = GitRepository(Path(self.temp.name))

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_preflight_accepts_fresh_main_and_rejects_stale_or_other_branch(self) -> None:
        git = ReleaseGit(self.repo.work)
        self.assertEqual(git.preflight_source(), ("main", self.repo.git("rev-parse", "HEAD")))

        self.repo.git("branch", "release")
        self.repo.git("push", "origin", "release")
        self.repo.git("checkout", "release")
        self.assertEqual(
            git.preflight_source(), ("release", self.repo.git("rev-parse", "HEAD"))
        )

        self.repo.git("checkout", "-b", "feature")
        with self.assertRaisesRegex(ReleaseGitError, "main or release"):
            git.preflight_source()

        self.repo.git("checkout", "main")
        self.repo.clone_other()
        self.repo.commit("remote.txt", "remote", "advance", cwd=self.repo.other)
        self.repo.git("push", "origin", "main", cwd=self.repo.other)
        with self.assertRaisesRegex(ReleaseGitError, "latest origin/main"):
            git.preflight_source()

    def test_main_release_atomically_advances_both_branches_without_pushing_tag(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        release_commit = self.repo.commit("release.txt", "release", "Release v1.2.3")
        self.repo.git("tag", "v1.2.3")

        git = ReleaseGit(self.repo.work)
        targets = git.branch_targets("main", release_commit, "v1.2.3")
        git.publish_canonical_branches(targets)

        self.assertEqual(self.repo.remote_ref("refs/heads/main"), release_commit)
        self.assertEqual(self.repo.remote_ref("refs/heads/release"), release_commit)
        with self.assertRaises(subprocess.CalledProcessError):
            self.repo.remote_ref("refs/tags/v1.2.3")

    def test_hotfix_merge_has_main_first_parent_and_release_second_parent(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        main_tip = self.repo.commit("main.txt", "unreleased", "unreleased main")
        self.repo.git("push", "origin", "main")
        self.repo.git("checkout", "release")
        release_commit = self.repo.commit("hotfix.txt", "fix", "Release v1.2.4")

        git = ReleaseGit(self.repo.work)
        targets = git.branch_targets("release", release_commit, "v1.2.4")
        parents = self.repo.git("show", "-s", "--format=%P", targets.main_commit).split()
        self.assertEqual(parents, [main_tip, release_commit])

        git.publish_canonical_branches(targets)
        self.assertEqual(self.repo.remote_ref("refs/heads/release"), release_commit)
        self.assertEqual(self.repo.remote_ref("refs/heads/main"), targets.main_commit)
        self.assertTrue(git.publication_complete(release_commit, "release"))

    def test_atomic_rejection_leaves_release_unchanged_when_main_moves(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        self.repo.git("checkout", "release")
        release_commit = self.repo.commit("hotfix.txt", "fix", "Release v1.2.5")
        git = ReleaseGit(self.repo.work)
        targets = git.branch_targets("release", release_commit, "v1.2.5")

        self.repo.clone_other()
        self.repo.commit("race.txt", "race", "main raced", cwd=self.repo.other)
        self.repo.git("push", "origin", "main", cwd=self.repo.other)

        with self.assertRaisesRegex(ReleaseGitError, "atomically publish"):
            git.publish_canonical_branches(targets)
        self.assertEqual(self.repo.remote_ref("refs/heads/release"), base)

    def test_hotfix_recovery_rejects_merge_with_release_as_first_parent(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        main_tip = self.repo.commit("main.txt", "unreleased", "unreleased main")
        self.repo.git("push", "origin", "main")
        self.repo.git("checkout", "release")
        release_commit = self.repo.commit("hotfix.txt", "fix", "Release v1.2.8")
        tree = self.repo.git("merge-tree", "--write-tree", main_tip, release_commit)
        reversed_merge = self.repo.git(
            "commit-tree", tree, "-p", release_commit, "-p", main_tip, "-m", "wrong parents"
        )
        self.repo.git("push", "origin", f"{reversed_merge}:refs/heads/main")
        self.repo.git("push", "origin", f"{release_commit}:refs/heads/release")

        self.assertFalse(
            ReleaseGit(self.repo.work).publication_complete(release_commit, "release")
        )

    def test_hotfix_corrects_existing_reversed_parent_merge(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        main_tip = self.repo.commit("main.txt", "unreleased", "unreleased main")
        self.repo.git("push", "origin", "main")
        self.repo.git("checkout", "release")
        release_commit = self.repo.commit("hotfix.txt", "fix", "Release v1.2.10")
        tree = self.repo.git("merge-tree", "--write-tree", main_tip, release_commit)
        reversed_merge = self.repo.git(
            "commit-tree", tree, "-p", release_commit, "-p", main_tip, "-m", "wrong parents"
        )
        self.repo.git("push", "origin", f"{reversed_merge}:refs/heads/main")

        targets = ReleaseGit(self.repo.work).branch_targets(
            "release", release_commit, "v1.2.10"
        )

        parents = self.repo.git("show", "-s", "--format=%P", targets.main_commit).split()
        self.assertEqual(parents, [reversed_merge, release_commit])

    def test_hotfix_recovery_rejects_main_pointing_directly_to_release(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        self.repo.git("checkout", "release")
        release_commit = self.repo.commit("hotfix.txt", "fix", "Release v1.2.12")
        self.repo.git("push", "origin", f"{release_commit}:refs/heads/main")

        git = ReleaseGit(self.repo.work)
        with self.assertRaisesRegex(ReleaseGitError, "main-first merge"):
            git.branch_targets("release", release_commit, "v1.2.12")
        self.assertFalse(git.publication_complete(release_commit, "release"))

    def test_conflicting_hotfix_stops_before_remote_mutation(self) -> None:
        base = self.repo.git("rev-parse", "HEAD")
        self.repo.git("branch", "release", base)
        self.repo.git("push", "origin", "release")
        self.repo.commit("shared.txt", "main", "main change")
        self.repo.git("push", "origin", "main")
        self.repo.git("checkout", "release")
        release_commit = self.repo.commit("shared.txt", "release", "Release v1.2.6")

        with self.assertRaisesRegex(ReleaseGitError, "conflicts"):
            ReleaseGit(self.repo.work).branch_targets("release", release_commit, "v1.2.6")
        self.assertEqual(self.repo.remote_ref("refs/heads/release"), base)

    def test_remote_tag_must_resolve_to_release_commit(self) -> None:
        release_commit = self.repo.git("rev-parse", "HEAD")
        self.repo.git("tag", "v1.2.7")
        self.repo.git("push", "origin", "refs/tags/v1.2.7")
        git = ReleaseGit(self.repo.work)
        git.verify_remote_tag("v1.2.7", release_commit)

        with self.assertRaisesRegex(ReleaseGitError, "does not resolve"):
            git.verify_remote_tag("v1.2.7", "0" * 40)

    def test_remote_tag_query_failure_is_not_treated_as_absence(self) -> None:
        def failing_runner(command, cwd):
            return subprocess.CompletedProcess(command, 1, "", "network unavailable")

        with self.assertRaisesRegex(ReleaseGitError, "network unavailable"):
            ReleaseGit(self.repo.work, failing_runner).remote_tag_target("v1.2.9")

    def test_remote_tag_queries_direct_and_peeled_refs_together(self) -> None:
        calls: list[tuple[str, ...]] = []

        def recording_runner(command, cwd):
            calls.append(tuple(command))
            return subprocess.CompletedProcess(command, 0, "", "")

        self.assertIsNone(
            ReleaseGit(self.repo.work, recording_runner).remote_tag_target("v1.2.11")
        )
        self.assertEqual(
            calls,
            [
                (
                    "git",
                    "ls-remote",
                    "origin",
                    "refs/tags/v1.2.11",
                    "refs/tags/v1.2.11^{}",
                )
            ],
        )

    def test_publication_recovery_propagates_fetch_failure(self) -> None:
        release_commit = "a" * 40

        def failing_fetch(command, cwd):
            if command[:3] == ("git", "ls-remote", "origin"):
                ref = command[-1]
                return subprocess.CompletedProcess(
                    command, 0, f"{release_commit}\t{ref}\n", ""
                )
            return subprocess.CompletedProcess(command, 1, "", "fetch unavailable")

        with self.assertRaisesRegex(ReleaseGitError, "fetch unavailable"):
            ReleaseGit(self.repo.work, failing_fetch).publication_complete(
                release_commit, "main"
            )


if __name__ == "__main__":
    unittest.main()
