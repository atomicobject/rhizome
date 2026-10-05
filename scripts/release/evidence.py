#!/usr/bin/env python3
"""Bounded, read-only evidence collection for Rhizome releases.

Git first-parent history defines what shipped. GitHub and effort notes enrich
those delivery units, but neither can add a delivery without range-local Git
proof. Raw diffs are deliberately absent from this module.
"""

from __future__ import annotations

import json
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Mapping, Protocol, Sequence

from release_types import Diagnostic, GitCommit


GitRunner = Callable[[list[str]], str]


@dataclass(frozen=True)
class PullRequestRecord:
    number: int
    title: str = ""
    body: str = ""
    url: str = ""
    base_ref: str = ""
    head_ref: str = ""
    merge_sha: str = ""
    merged_at: str = ""
    confidence: str = "github"


@dataclass(frozen=True)
class PrimarySelection:
    primary: tuple[PullRequestRecord, ...]
    covered_shas: tuple[str, ...]
    direct_shas: tuple[str, ...]
    diagnostics: tuple[Diagnostic, ...]


class GitHubClient(Protocol):
    def associated_prs(
        self, commit_shas: Sequence[str]
    ) -> Mapping[str, Sequence[PullRequestRecord]]: ...

    def pr_commits(self, number: int) -> Sequence[str]: ...


@dataclass
class StaticGitHubClient:
    associations: Mapping[str, Sequence[PullRequestRecord]]
    inventories: Mapping[int, Sequence[str]]

    def associated_prs(
        self, commit_shas: Sequence[str]
    ) -> Mapping[str, Sequence[PullRequestRecord]]:
        return {sha: self.associations.get(sha, ()) for sha in commit_shas}

    def pr_commits(self, number: int) -> Sequence[str]:
        return self.inventories.get(number, ())


def _run_git(repo: Path, args: list[str]) -> str:
    result = subprocess.run(
        ["git", *args], cwd=repo, capture_output=True, text=True
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(detail or f"git {' '.join(args)} failed")
    # Preserve Git's record/unit separators. Callers normalize line-oriented
    # output themselves; stripping here corrupts an empty final log field.
    return result.stdout


def collect_mainline(repo: Path, base: str, head: str = "HEAD") -> tuple[GitCommit, ...]:
    return collect_mainline_with_runner(lambda args: _run_git(repo, args), base, head)


def collect_mainline_with_runner(
    runner: GitRunner, base: str, head: str = "HEAD"
) -> tuple[GitCommit, ...]:
    base_sha = runner(["rev-parse", base]).strip()
    head_sha = runner(["rev-parse", head]).strip()
    chain = tuple(
        line.strip()
        for line in runner(["rev-list", "--first-parent", head_sha]).splitlines()
        if line.strip()
    )
    if base_sha not in chain:
        raise ValueError(f"base {base} is not on {head}'s first-parent chain")

    output = runner(
        [
            "log",
            "--reverse",
            "--first-parent",
            "--format=%H%x1f%P%x1f%s%x1f%b%x1e",
            f"{base_sha}..{head_sha}",
        ]
    )
    commits = []
    for raw_record in output.split("\x1e"):
        record = raw_record.strip("\n")
        if not record:
            continue
        fields = record.split("\x1f", 3)
        if len(fields) != 4:
            raise ValueError("unexpected git log record while collecting release evidence")
        sha, parents, subject, body = fields
        commits.append(
            GitCommit(
                sha=sha.strip(),
                parents=tuple(parents.split()),
                subject=subject.strip(),
                body=body.strip(),
                mainline_index=len(commits),
            )
        )
    return tuple(commits)


def changed_paths(repo: Path, before: str, after: str) -> tuple[str, ...]:
    output = _run_git(
        repo,
        ["diff-tree", "--no-commit-id", "--name-only", "-r", "-M", before, after],
    )
    return tuple(sorted(line for line in output.splitlines() if line))


def extract_unreleased(changelog: str) -> tuple[str, ...]:
    in_unreleased = False
    bullets = []
    for line in changelog.splitlines():
        if re.match(r"^##\s+\[?Unreleased\]?\s*$", line, re.IGNORECASE):
            in_unreleased = True
            continue
        if in_unreleased and line.startswith("## "):
            break
        if in_unreleased and re.match(r"^\s*[-*]\s+\S", line):
            bullets.append(re.sub(r"^\s*[-*]\s+", "", line).strip())
    return tuple(bullets)


_KEPT_PR_SECTIONS = {
    "summary",
    "problem",
    "root cause",
    "product behavior",
    "user impact",
    "breaking changes",
    "deployment notes",
    "known gotchas",
}


def project_pr_body(body: str) -> str:
    sections: list[tuple[str, list[str]]] = []
    current_heading = ""
    current_lines: list[str] = []
    for line in body.splitlines():
        match = re.match(r"^#{1,3}\s+(.+?)\s*$", line)
        if match:
            if current_heading:
                sections.append((current_heading, current_lines))
            current_heading = match.group(1).strip()
            current_lines = []
        elif current_heading:
            current_lines.append(line)
    if current_heading:
        sections.append((current_heading, current_lines))

    kept = []
    for heading, lines in sections:
        normalized = re.sub(r"[^a-z0-9 ]", "", heading.lower()).strip()
        if normalized not in _KEPT_PR_SECTIONS:
            continue
        content = "\n".join(lines).strip()
        if content:
            kept.append(f"## {heading}\n{content}")
    if kept:
        return "\n\n".join(kept)
    return body.strip()[:4000]


def select_primary_prs(
    mainline: Sequence[GitCommit],
    associations: Mapping[str, Sequence[PullRequestRecord]],
    release_branch: str,
) -> PrimarySelection:
    mainline_shas = {commit.sha for commit in mainline}
    selected_by_number: dict[int, PullRequestRecord] = {}
    covered: set[str] = set()
    ambiguous: set[str] = set()
    diagnostics: list[Diagnostic] = []

    for commit in mainline:
        exact = {
            pr.number: pr
            for pr in associations.get(commit.sha, ())
            if pr.merged_at
            and pr.base_ref == release_branch
            and pr.merge_sha in mainline_shas
        }
        if len(exact) > 1:
            ambiguous.add(commit.sha)
            diagnostics.append(
                Diagnostic(
                    code="ambiguous_primary_pr",
                    message=(
                        f"commit {commit.sha} has multiple exact primary PRs: "
                        + ", ".join(f"#{number}" for number in sorted(exact))
                    ),
                )
            )
            continue
        if len(exact) == 1:
            pr = next(iter(exact.values()))
            selected_by_number[pr.number] = pr
            covered.add(commit.sha)

    selected = tuple(
        sorted(
            selected_by_number.values(),
            key=lambda pr: next(
                commit.mainline_index
                for commit in mainline
                if commit.sha == pr.merge_sha
            ),
        )
    )
    direct = tuple(
        commit.sha
        for commit in mainline
        if commit.sha not in covered or commit.sha in ambiguous
    )
    return PrimarySelection(
        primary=selected,
        covered_shas=tuple(commit.sha for commit in mainline if commit.sha in covered),
        direct_shas=direct,
        diagnostics=tuple(diagnostics),
    )


def expand_supporting_prs(
    primaries: Sequence[PullRequestRecord],
    client: GitHubClient,
    *,
    primary_numbers: set[int] | None = None,
) -> tuple[dict[int, tuple[PullRequestRecord, ...]], tuple[Diagnostic, ...]]:
    primary_ids = primary_numbers or {pr.number for pr in primaries}
    result: dict[int, tuple[PullRequestRecord, ...]] = {}
    diagnostics: list[Diagnostic] = []

    def children(parent: PullRequestRecord, seen: set[int]) -> tuple[PullRequestRecord, ...]:
        try:
            inventory = tuple(client.pr_commits(parent.number))
            associations = client.associated_prs(inventory)
        except Exception as exc:  # GitHub is optional evidence.
            diagnostics.append(
                Diagnostic(
                    code="github_stack_partial",
                    message=f"could not expand PR #{parent.number}: {_safe_error(exc)}",
                )
            )
            return ()
        candidates: dict[int, PullRequestRecord] = {}
        for sha in inventory:
            for candidate in associations.get(sha, ()):
                if (
                    candidate.number not in primary_ids
                    and candidate.number not in seen
                    and candidate.base_ref == parent.head_ref
                    and candidate.merge_sha == sha
                ):
                    candidates[candidate.number] = candidate
        ordered = tuple(
            sorted(candidates.values(), key=lambda pr: inventory.index(pr.merge_sha))
        )
        for candidate in ordered:
            children(candidate, seen | {candidate.number})
        return ordered

    for primary in primaries:
        result[primary.number] = children(primary, {primary.number})
    return result, tuple(diagnostics)


_LOCAL_PR_PATTERNS = (
    re.compile(r"^Merge pull request #(\d+)\b", re.IGNORECASE),
    re.compile(r"^Merge PR #(\d+)\b", re.IGNORECASE),
    re.compile(r"\(#(\d+)\)\s*$"),
)


def local_pr_stubs(commits: Sequence[GitCommit]) -> tuple[PullRequestRecord, ...]:
    stubs = []
    seen = set()
    for commit in commits:
        number = None
        for pattern in _LOCAL_PR_PATTERNS:
            match = pattern.search(commit.subject)
            if match:
                number = int(match.group(1))
                break
        if number is None or number in seen:
            continue
        seen.add(number)
        stubs.append(
            PullRequestRecord(
                number=number,
                title=commit.subject,
                merge_sha=commit.sha,
                confidence="local_marker",
            )
        )
    return tuple(stubs)


def _safe_error(exc: Exception) -> str:
    text = re.sub(r"(?i)(token|authorization|bearer)[:= ]+\S+", r"\1=[redacted]", str(exc))
    return text.replace("\n", " ")[:500]


class GhGitHubClient:
    """Optional GitHub enrichment implemented through the authenticated gh CLI."""

    def __init__(self, repo_slug: str, gh_bin: str = "gh") -> None:
        self.repo_slug = repo_slug
        self.gh_bin = gh_bin

    def _api(self, endpoint: str) -> object:
        result = subprocess.run(
            [self.gh_bin, "api", "--paginate", endpoint],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            raise RuntimeError(_safe_error(RuntimeError(result.stderr.strip())))
        chunks = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
        if len(chunks) == 1:
            return chunks[0]
        flattened = []
        for chunk in chunks:
            flattened.extend(chunk if isinstance(chunk, list) else [chunk])
        return flattened

    def associated_prs(
        self, commit_shas: Sequence[str]
    ) -> Mapping[str, Sequence[PullRequestRecord]]:
        result = {}
        for sha in commit_shas:
            payload = self._api(
                f"repos/{self.repo_slug}/commits/{sha}/pulls"
            )
            result[sha] = tuple(_pr_from_rest(item) for item in payload)
        return result

    def pr_commits(self, number: int) -> Sequence[str]:
        payload = self._api(f"repos/{self.repo_slug}/pulls/{number}/commits")
        return tuple(item["sha"] for item in payload)


def _pr_from_rest(payload: Mapping[str, object]) -> PullRequestRecord:
    base = payload.get("base") or {}
    head = payload.get("head") or {}
    return PullRequestRecord(
        number=int(payload["number"]),
        title=str(payload.get("title") or ""),
        body=str(payload.get("body") or ""),
        url=str(payload.get("html_url") or ""),
        base_ref=str(base.get("ref") or ""),
        head_ref=str(head.get("ref") or ""),
        merge_sha=str(payload.get("merge_commit_sha") or ""),
        merged_at=str(payload.get("merged_at") or ""),
    )
