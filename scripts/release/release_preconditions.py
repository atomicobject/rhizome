#!/usr/bin/env python3
"""Safety checks for the mutation boundary of a reviewed release."""

from __future__ import annotations

from pathlib import Path
from typing import Any, Callable, Mapping, Sequence

from release_actions import (
    CHANGELOG_PATH,
    EMBEDDED_CHANGELOG_PATH,
    RELEASE_FILE_PATHS,
    VERSION_PATH,
    render_changelog,
    render_version_go,
)


class ReleasePreconditionError(ValueError):
    """Repository state cannot safely converge to the reviewed release."""


def verify_base_tag(base_tag: str, base_commit: str, resolve: Callable[[str], str]) -> None:
    if resolve(base_tag) != base_commit:
        raise ReleasePreconditionError(
            f"base tag {base_tag} moved since evidence collection"
        )


def _dirty_paths(status: str) -> set[str]:
    paths: set[str] = set()
    for line in status.splitlines():
        if len(line) < 4:
            raise ReleasePreconditionError("worktree status could not be parsed safely")
        path = line[3:]
        if " -> " in path:
            paths.update(path.split(" -> ", 1))
        else:
            paths.add(path)
    return paths


def validate_apply_retry(
    repo_root: Path,
    *,
    status: str,
    phase: str,
    head_commit: str,
    version: str,
    release_date: str,
    changelog_entries: Sequence[str],
    read_planned: Callable[[str, str], str],
) -> None:
    """Require a clean first apply or a provably convergent partial retry."""

    if phase != "applying":
        if status:
            raise ReleasePreconditionError("worktree must be clean before first release apply")
        return

    dirty = _dirty_paths(status)
    owned = {path.as_posix() for path in RELEASE_FILE_PATHS}
    if dirty.difference(owned):
        raise ReleasePreconditionError("worktree has changes outside release-owned files")

    originals = {
        path: read_planned(head_commit, path.as_posix()) for path in RELEASE_FILE_PATHS
    }
    desired_changelog = render_changelog(
        originals[CHANGELOG_PATH],
        version=version,
        release_date=release_date,
        changelog_entries=changelog_entries,
    )
    desired: Mapping[Path, str] = {
        CHANGELOG_PATH: desired_changelog,
        VERSION_PATH: render_version_go(version),
        EMBEDDED_CHANGELOG_PATH: desired_changelog,
    }
    root = Path(repo_root)
    for relative_path in RELEASE_FILE_PATHS:
        current = (root / relative_path).read_text(encoding="utf-8")
        if current not in {originals[relative_path], desired[relative_path]}:
            raise ReleasePreconditionError(
                f"release-owned file has arbitrary retry content: {relative_path}"
            )


def validate_apply_boundary(
    repo_root: Path,
    *,
    base_tag: str,
    base_commit: str,
    planned_head: str,
    current_head: str,
    evidence_fingerprint: str,
    planned_fingerprint: str,
    status: str,
    phase: str,
    version: str,
    release_date: str,
    changelog_entries: Sequence[str],
    resolve: Callable[[str], str],
    read_planned: Callable[[str, str], str],
) -> None:
    verify_base_tag(base_tag, base_commit, resolve)
    if current_head != planned_head:
        raise ReleasePreconditionError("HEAD changed since the release plan was reviewed")
    if evidence_fingerprint != planned_fingerprint:
        raise ReleasePreconditionError("release evidence fingerprint no longer matches the plan")
    validate_apply_retry(
        repo_root,
        status=status,
        phase=phase,
        head_commit=planned_head,
        version=version,
        release_date=release_date,
        changelog_entries=changelog_entries,
        read_planned=read_planned,
    )


def validate_stored_apply_boundary(
    repo_root: Path,
    *,
    run: Any,
    version: str,
    release_date: str,
    current_head: str,
    status: str,
    resolve: Callable[[str], str],
    read_planned: Callable[[str, str], str],
) -> None:
    validate_apply_boundary(
        repo_root,
        base_tag=run.plan.base_tag,
        base_commit=run.plan.base_commit,
        planned_head=run.plan.head_commit,
        current_head=current_head,
        evidence_fingerprint=run.evidence.fingerprint,
        planned_fingerprint=run.plan.evidence_fingerprint,
        status=status,
        phase=run.state.phase,
        version=version,
        release_date=release_date,
        changelog_entries=run.plan.changelog_entries,
        resolve=resolve,
        read_planned=read_planned,
    )
