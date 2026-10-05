#!/usr/bin/env python3
"""Git contracts for publishing canonical release branches and verifying tags.

Canonical branches are published together before GoReleaser creates the GitHub
tag. Hotfix merges are built without checking out main and preserve main as the
first parent so first-parent release evidence remains meaningful.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import subprocess
from typing import Callable, Sequence


CommandRunner = Callable[[Sequence[str], Path], subprocess.CompletedProcess[str]]


def _run_command(command: Sequence[str], cwd: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=cwd, text=True, capture_output=True)


class ReleaseGitError(RuntimeError):
    """Raised when release Git state cannot be advanced safely."""


@dataclass(frozen=True, slots=True)
class PublicationTargets:
    source_branch: str
    release_commit: str
    main_commit: str


class ReleaseGit:
    def __init__(self, repo_root: Path, command_runner: CommandRunner = _run_command) -> None:
        self.repo_root = repo_root
        self.command_runner = command_runner

    def _git(self, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        result = self.command_runner(("git", *args), self.repo_root)
        if check and result.returncode:
            raise subprocess.CalledProcessError(
                result.returncode, result.args, result.stdout, result.stderr
            )
        return result

    def _output(self, *args: str) -> str:
        try:
            return self._git(*args).stdout.strip()
        except subprocess.CalledProcessError as error:
            detail = error.stderr.strip() or error.stdout.strip()
            raise ReleaseGitError(f"git {' '.join(args)} failed: {detail}") from error

    def preflight_source(self) -> tuple[str, str]:
        branch_result = self._git("symbolic-ref", "--quiet", "--short", "HEAD", check=False)
        branch = branch_result.stdout.strip()
        if branch not in {"main", "release"}:
            raise ReleaseGitError("release source branch must be exactly main or release")

        self._output("fetch", "origin", branch)
        head = self._output("rev-parse", "HEAD")
        remote = self._output("rev-parse", f"refs/remotes/origin/{branch}")
        if head != remote:
            raise ReleaseGitError(
                f"release source must equal the latest origin/{branch}; "
                f"HEAD is {head} and origin/{branch} is {remote}"
            )
        return branch, head

    def branch_targets(
        self, source_branch: str, release_commit: str, version: str
    ) -> PublicationTargets:
        if source_branch not in {"main", "release"}:
            raise ReleaseGitError("release source branch must be exactly main or release")
        self._output("cat-file", "-e", f"{release_commit}^{{commit}}")
        self._output("fetch", "origin", "main", "release")
        if source_branch == "main":
            return PublicationTargets(source_branch, release_commit, release_commit)

        main_tip = self._output("rev-parse", "refs/remotes/origin/main")
        if main_tip == release_commit:
            raise ReleaseGitError(
                "origin/main points directly to the hotfix release commit; "
                "canonical hotfix publication requires a main-first merge"
            )
        if self._hotfix_topology_complete(release_commit, main_tip):
            return PublicationTargets(source_branch, release_commit, main_tip)

        merge = self._git("merge-tree", "--write-tree", main_tip, release_commit, check=False)
        if merge.returncode != 0:
            raise ReleaseGitError("release hotfix conflicts with the latest origin/main")
        tree = merge.stdout.splitlines()[0].strip() if merge.stdout else ""
        if not tree:
            raise ReleaseGitError("release hotfix merge did not produce a tree")
        main_commit = self._output(
            "commit-tree",
            tree,
            "-p",
            main_tip,
            "-p",
            release_commit,
            "-m",
            f"Merge release {version} into main",
        )
        return PublicationTargets(source_branch, release_commit, main_commit)

    def validate_local_release_commit(self, version: str, planned_head: str) -> str:
        head = self._output("rev-parse", "HEAD")
        tag = self._output("rev-parse", f"refs/tags/{version}")
        subject = self._output("log", "-1", "--pretty=%s")
        parent = self._output("rev-parse", "HEAD^")
        status = self._output("status", "--porcelain")
        if tag != head or subject != f"Release {version}" or parent != planned_head or status:
            raise ReleaseGitError(
                "local release commit or tag no longer matches the reviewed plan"
            )
        return head

    def publish_canonical_branches(self, targets: PublicationTargets) -> None:
        if self.publication_complete(targets.release_commit, targets.source_branch):
            return
        result = self._git(
            "push",
            "--atomic",
            "origin",
            f"{targets.main_commit}:refs/heads/main",
            f"{targets.release_commit}:refs/heads/release",
            check=False,
        )
        if result.returncode == 0 or self.publication_complete(
            targets.release_commit, targets.source_branch
        ):
            return
        detail = result.stderr.strip() or result.stdout.strip()
        raise ReleaseGitError(f"failed to atomically publish canonical branches: {detail}")

    def publication_complete(self, release_commit: str, source_branch: str) -> bool:
        release_ref = self._remote_ref("refs/heads/release")
        if release_ref != release_commit:
            return False
        self._output("fetch", "origin", "main")
        main_tip = self._output("rev-parse", "refs/remotes/origin/main")
        if source_branch == "main":
            return self._is_ancestor(release_commit, main_tip)
        if source_branch != "release":
            return False
        return self._hotfix_topology_complete(release_commit, main_tip)

    def _hotfix_topology_complete(self, release_commit: str, main_tip: str) -> bool:
        history = self._git(
            "rev-list",
            "--first-parent",
            "--parents",
            "--ancestry-path",
            f"{release_commit}..{main_tip}",
            check=False,
        )
        if history.returncode != 0:
            return False
        return any(
            len(fields := line.split()) >= 3 and fields[2] == release_commit
            for line in history.stdout.splitlines()
        )

    def verify_remote_tag(self, version: str, release_commit: str) -> None:
        resolved = self.remote_tag_target(version)
        if resolved != release_commit:
            raise ReleaseGitError(
                f"remote tag {version} does not resolve to release commit {release_commit}"
            )

    def remote_tag_target(self, version: str) -> str | None:
        direct_ref = f"refs/tags/{version}"
        peeled_ref = f"{direct_ref}^{{}}"
        result = self._git("ls-remote", "origin", direct_ref, peeled_ref, check=False)
        if result.returncode != 0:
            detail = result.stderr.strip() or result.stdout.strip() or "remote query failed"
            raise ReleaseGitError(f"git ls-remote failed for {direct_ref}: {detail}")
        refs = {
            fields[1]: fields[0]
            for line in result.stdout.splitlines()
            if len(fields := line.split()) == 2
        }
        return refs.get(peeled_ref) or refs.get(direct_ref)

    def _remote_ref(self, ref: str) -> str | None:
        result = self._git("ls-remote", "origin", ref, check=False)
        if result.returncode != 0:
            detail = result.stderr.strip() or result.stdout.strip() or "remote query failed"
            raise ReleaseGitError(f"git ls-remote failed for {ref}: {detail}")
        for line in result.stdout.splitlines():
            fields = line.split()
            if len(fields) == 2 and fields[1] == ref:
                return fields[0]
        return None

    def _is_ancestor(self, ancestor: str, descendant: str) -> bool:
        return self._git(
            "merge-base", "--is-ancestor", ancestor, descendant, check=False
        ).returncode == 0
