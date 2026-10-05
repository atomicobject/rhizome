#!/usr/bin/env python3
"""Assemble persisted release evidence from bounded collectors."""

from __future__ import annotations

import json
import re
import subprocess
from dataclasses import replace
from pathlib import Path
from typing import Mapping, Sequence

from effort_evidence import (
    EffortDiagnostic, _canonical_contract, _MetadataParser, _parse_frontmatter, select_effort_evidence, workspace_components,
)
from evidence import (
    GitHubClient,
    PullRequestRecord,
    changed_paths,
    collect_mainline,
    expand_supporting_prs,
    extract_unreleased,
    local_pr_stubs,
    project_pr_body,
    select_primary_prs,
)
from release_types import (
    DeliveryUnit,
    Diagnostic,
    EffortEvidence,
    GitCommit,
    PullRequestEvidence,
    ReleaseEvidence,
)


def _git(repo: Path, *args: str, check: bool = True) -> str:
    result = subprocess.run(
        ["git", *args], cwd=repo, capture_output=True, text=True
    )
    if check and result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(detail or f"git {' '.join(args)} failed")
    return result.stdout.strip() if result.returncode == 0 else ""


def _branch(repo: Path) -> str:
    branch = _git(repo, "symbolic-ref", "--quiet", "--short", "HEAD", check=False)
    return branch or "main"


def _change_totals(repo: Path, before: str, after: str) -> tuple[int, int]:
    output = _git(repo, "diff-tree", "--no-commit-id", "--numstat", "-r", before, after)
    insertions = 0
    deletions = 0
    for line in output.splitlines():
        fields = line.split("\t", 2)
        if len(fields) >= 2:
            insertions += int(fields[0]) if fields[0].isdigit() else 0
            deletions += int(fields[1]) if fields[1].isdigit() else 0
    return insertions, deletions


def _pr_evidence(
    record: PullRequestRecord,
    *,
    role: str,
    commit_shas: Sequence[str] = (),
) -> PullRequestEvidence:
    return PullRequestEvidence(
        number=record.number,
        title=record.title,
        body=project_pr_body(record.body),
        url=record.url,
        base_ref=record.base_ref,
        head_ref=record.head_ref,
        merge_sha=record.merge_sha,
        merged_at=record.merged_at,
        role=role,
        commit_shas=tuple(commit_shas),
    )


def _read_blob(repo: Path, revision: str, path: str) -> str | None:
    result = subprocess.run(
        ["git", "show", f"{revision}:{path}"],
        cwd=repo,
        capture_output=True,
        text=True,
    )
    return result.stdout if result.returncode == 0 else None


def _discovery_metadata(text: str) -> dict:
    """Retain unambiguous links without accepting invalid release evidence."""
    parser = _MetadataParser()
    parser.feed(text.removeprefix("\ufeff"))
    parser.close()
    if len(parser.blocks) != 1 or parser.current is not None:
        raise ValueError("workspace requires exactly one complete rhizome-metadata script")

    def unambiguous_fields(pairs):
        values = {}
        duplicates = set()
        for key, value in pairs:
            if key in values:
                duplicates.add(key)
            values[key] = value
        return {key: value for key, value in values.items() if key not in duplicates}

    metadata = json.loads(parser.blocks[0], object_pairs_hook=unambiguous_fields)
    if not isinstance(metadata, dict) or metadata.get("type") != "EffortWorkspace":
        raise ValueError("HTML entry is not an EffortWorkspace")
    return metadata


def _effort_pairs(repo: Path, before: str, after: str) -> tuple[tuple[str | None, str], ...]:
    output = _git(
        repo,
        "diff-tree",
        "--no-commit-id",
        "--name-status",
        "-r",
        "-M",
        before,
        after,
        "--",
        "docs/efforts",
    )
    pairs = []
    for line in output.splitlines():
        fields = line.split("\t")
        if len(fields) < 2:
            continue
        status = fields[0]
        if status.startswith("D"):
            continue
        if status.startswith("R") and len(fields) >= 3:
            pairs.append((fields[1], fields[2]))
        elif status.startswith("A"):
            pairs.append((None, fields[1]))
        else:
            pairs.append((fields[1], fields[1]))
    # Membership comes from explicit metadata, never folder proximity. Read both
    # snapshots so removed or replaced component links still identify their owner.
    entries: dict[str, set[str]] = {}
    for entry in _git(repo, "ls-tree", "-r", "--name-only", after, "--", "docs/efforts").splitlines():
        parts = entry.split("/")
        if len(parts) != 4 or not parts[-1].endswith("-effort.html"):
            continue
        components = {entry}
        old = next((old for old, final in pairs if final == entry), entry)
        for revision, path in ((before, old), (after, entry)):
            text = _read_blob(repo, revision, path) if path else None
            if text is None:
                continue
            try:
                metadata = _discovery_metadata(text)
            except ValueError:
                continue
            # Discovery retains valid associations even when another field is
            # invalid. Selection still validates the complete workspace.
            for key in ("implementation-plan", "work-log", "materials"):
                try:
                    components.update(workspace_components({key: metadata[key]}))
                except (KeyError, ValueError):
                    continue
        entries[entry] = components
    changed = {field for line in output.splitlines() for field in line.split("\t")[1:]}
    owners = {entry for entry, components in entries.items() if changed & components}
    folders = {entry.rsplit("/", 1)[0] + "/" for entry in entries}
    grouped = []
    for pair in pairs:
        if any(pair[1] in components for components in entries.values()):
            continue
        if any(pair[1].startswith(folder) for folder in folders):
            text = _read_blob(repo, after, pair[1]) or ""
            try:
                if not pair[1].endswith(".md") or _parse_frontmatter(text).get("type") != "EffortNote":
                    continue
            except ValueError:
                continue
        grouped.append(pair)
    for entry in sorted(owners):
        old = next((old for old, final in pairs if final == entry), entry)
        if old and _read_blob(repo, before, old) is None:
            old = None
        grouped.append((old, entry))
    return tuple(grouped)


def _diagnostic(value: EffortDiagnostic) -> Diagnostic:
    # Historical type/identifier normalization is an expected exclusion. A
    # post-closure delivery edit is the only lifecycle violation here that
    # should demand maintainer attention.
    severity = "warning" if value.code == "post_closure_mutation" else "info"
    return Diagnostic(
        code=value.code,
        message=value.message,
        severity=severity,
        source=value.path,
        details=(("effort_id", value.effort_id),) if value.effort_id else (),
    )


def _snapshot_note_discovered(repo: Path, revision: str, path: str) -> bool:
    # Only the target and its discovery inputs are needed. Read every byte from
    # this revision so working-tree configuration cannot change release claims.
    policy_paths = {".rhizome/config.yml", ".rhizome/ignore", ".obsidianignore"}
    policy_paths.update(str(parent / ".gitignore") for parent in Path(path).parents)
    files = {name: content for name in sorted(policy_paths)
             if (content := _read_blob(repo, revision, name)) is not None}
    return _canonical_contract("note-discovery", json.dumps({"path": path, "files": files}, sort_keys=True))


def _snapshot_target_contract(repo: Path, revision: str, path: str, content: str, *, kind: str = "target-type") -> str | bool:
    directory = ".rhizome/ontology/"
    names = [name.removeprefix(directory) for name in
             _git(repo, "ls-tree", "-r", "--name-only", revision, "--", directory).splitlines()]
    files = {name: _read_blob(repo, revision, directory + name)
             for name in names if "/" not in name and name.endswith(".graphql")}
    return _canonical_contract(kind, json.dumps({
        "path": path, "content": content, "schema_files": files,
    }, sort_keys=True))


def _collect_efforts(
    repo: Path,
    before: str,
    after: str,
    unit_paths: Sequence[str],
) -> tuple[tuple[EffortEvidence, ...], tuple[Diagnostic, ...]]:
    efforts = []
    diagnostics = []
    for old_path, final_path in _effort_pairs(repo, before, after):
        final_text = _read_blob(repo, after, final_path)
        if final_text is None:
            continue
        parent_text = _read_blob(repo, before, old_path) if old_path else None
        selection = select_effort_evidence(
            parent_text,
            final_text,
            path=final_path,
            parent_path=old_path,
            changed_paths=unit_paths,
            parent_read_link=lambda path: _read_blob(repo, before, path),
            final_read_link=lambda path: _read_blob(repo, after, path),
            parent_target_type=lambda path, content: _snapshot_target_contract(repo, before, path, content),
            final_target_type=lambda path, content: _snapshot_target_contract(repo, after, path, content),
            parent_is_spec_like=lambda path, content: _snapshot_target_contract(repo, before, path, content, kind="spec-like"),
            final_is_spec_like=lambda path, content: _snapshot_target_contract(repo, after, path, content, kind="spec-like"),
            parent_is_discovered=lambda path: _snapshot_note_discovered(repo, before, path),
            final_is_discovered=lambda path: _snapshot_note_discovered(repo, after, path),
        )
        diagnostics.extend(_diagnostic(item) for item in selection.diagnostics)
        if selection.candidate:
            candidate = selection.candidate
            efforts.append(
                EffortEvidence(
                    effort_id=candidate.effort_id,
                    path=candidate.path,
                    name=candidate.name,
                    actual_delivered=candidate.actual_delivered,
                    deviations=candidate.deviations,
                    inclusion_reason=candidate.inclusion_reason,
                )
            )
    return rank_efforts(tuple(efforts)), tuple(diagnostics)


def _mentioned_effort_ids(text: str, known_ids: set[str]) -> set[str]:
    mentioned = {
        effort_id
        for effort_id in known_ids
        if re.search(
            rf"(?<![A-Za-z0-9_-]){re.escape(effort_id)}(?![A-Za-z0-9_-])",
            text,
            re.IGNORECASE,
        )
    }
    numeric_ids = {
        effort_id: int(match.group(1))
        for effort_id in known_ids
        if (match := re.fullmatch(r"EFF-(\d{4})", effort_id, re.IGNORECASE))
    }
    for start, end in re.findall(
        r"EFF-(\d{4})\s+(?:through|to)\s+EFF-(\d{4})", text, re.IGNORECASE
    ):
        low, high = sorted((int(start), int(end)))
        mentioned.update(
            effort_id
            for effort_id, number in numeric_ids.items()
            if low <= number <= high
        )
    return mentioned


def rank_efforts(efforts: Sequence[EffortEvidence]) -> tuple[EffortEvidence, ...]:
    """Rank an integration account first without creating duplicate deliveries."""
    if len(efforts) < 2:
        return tuple(efforts)
    known_ids = {item.effort_id for item in efforts}
    scores = {
        item.effort_id: len(_mentioned_effort_ids(item.actual_delivered, known_ids) - {item.effort_id})
        for item in efforts
    }
    primary_id, primary_score = max(scores.items(), key=lambda item: (item[1], item[0]))
    if primary_score < 2:
        return tuple(efforts)
    primary = next(item for item in efforts if item.effort_id == primary_id)
    supporting = tuple(item for item in efforts if item.effort_id != primary_id)
    return (replace(primary, role="primary"),) + tuple(
        replace(item, role="supporting") for item in supporting
    )


def _coverage_by_pr(
    mainline: Sequence[GitCommit],
    associations: Mapping[str, Sequence[PullRequestRecord]],
    primary: Sequence[PullRequestRecord],
) -> dict[int, tuple[GitCommit, ...]]:
    primary_ids = {item.number for item in primary}
    coverage: dict[int, list[GitCommit]] = {number: [] for number in primary_ids}
    for commit in mainline:
        for record in associations.get(commit.sha, ()):
            if record.number in primary_ids and commit not in coverage[record.number]:
                coverage[record.number].append(commit)
    return {number: tuple(commits) for number, commits in coverage.items()}


def collect_release_evidence(
    repo: Path,
    base: str,
    head: str = "HEAD",
    *,
    release_branch: str = "",
    github_client: GitHubClient | None = None,
) -> ReleaseEvidence:
    """Collect a complete read-only evidence snapshot for one frozen range."""
    repo = repo.resolve()
    base_sha = _git(repo, "rev-parse", base)
    head_sha = _git(repo, "rev-parse", head)
    branch = release_branch or _branch(repo)
    mainline = collect_mainline(repo, base_sha, head_sha)
    diagnostics: list[Diagnostic] = []
    associations: Mapping[str, Sequence[PullRequestRecord]] = {}
    supporting: dict[int, tuple[PullRequestRecord, ...]] = {}
    github_status = "unavailable"

    if github_client:
        try:
            associations = github_client.associated_prs([item.sha for item in mainline])
            github_status = "available"
        except Exception as exc:
            diagnostics.append(
                Diagnostic("github_unavailable", f"GitHub enrichment unavailable: {exc}")
            )

    selection = select_primary_prs(mainline, associations, branch)
    diagnostics.extend(selection.diagnostics)
    if github_client and github_status == "available":
        supporting, stack_diagnostics = expand_supporting_prs(
            selection.primary,
            github_client,
            primary_numbers={item.number for item in selection.primary},
        )
        diagnostics.extend(stack_diagnostics)
        if stack_diagnostics:
            github_status = "partial"
    elif not github_client:
        diagnostics.append(
            Diagnostic(
                "github_unavailable",
                "GitHub enrichment was not requested; using local commit markers.",
                severity="info",
            )
        )

    coverage = _coverage_by_pr(mainline, associations, selection.primary)
    anchor_by_sha = {item.merge_sha: item for item in selection.primary}
    covered_nonanchors = {
        commit.sha
        for record in selection.primary
        for commit in coverage.get(record.number, ())
        if commit.sha != record.merge_sha
    }
    stubs = {item.merge_sha: item for item in local_pr_stubs(mainline)} if github_status == "unavailable" else {}
    units = []
    for commit in mainline:
        if commit.sha in covered_nonanchors:
            continue
        record = anchor_by_sha.get(commit.sha)
        grouped = coverage.get(record.number, (commit,)) if record else (commit,)
        grouped = tuple(sorted(grouped, key=lambda item: item.mainline_index))
        before = grouped[0].parents[0]
        after = grouped[-1].sha
        paths = changed_paths(repo, before, after)
        insertions, deletions = _change_totals(repo, before, after)
        efforts, effort_diagnostics = _collect_efforts(repo, before, after, paths)
        diagnostics.extend(effort_diagnostics)
        stub = stubs.get(commit.sha)
        primary_evidence = (
            _pr_evidence(record, role="primary", commit_shas=[item.sha for item in grouped])
            if record
            else _pr_evidence(stub, role="stub", commit_shas=(commit.sha,)) if stub else None
        )
        support_evidence = tuple(
            _pr_evidence(item, role="supporting")
            for item in supporting.get(record.number, ())
        ) if record else ()
        units.append(
            DeliveryUnit(
                mainline_commit=commit,
                changed_paths=paths,
                insertions=insertions,
                deletions=deletions,
                pull_request=primary_evidence,
                supporting_pull_requests=support_evidence,
                efforts=efforts,
                commits=grouped,
            )
        )

    changelog_path = repo / "CHANGELOG.md"
    unreleased = extract_unreleased(
        changelog_path.read_text(encoding="utf-8") if changelog_path.exists() else ""
    )
    return ReleaseEvidence(
        base_tag=base,
        base_commit=base_sha,
        head_commit=head_sha,
        release_branch=branch,
        unreleased_entries=unreleased,
        delivery_units=tuple(units),
        diagnostics=tuple(diagnostics),
        github_status=github_status,
    )
