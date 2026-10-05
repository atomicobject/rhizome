#!/usr/bin/env python3
"""Compatibility entrypoint backed by bounded release evidence and edited notes."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import shutil
import subprocess
import sys
from typing import Sequence

from collect_evidence import collect_release_evidence
from evidence import GhGitHubClient
from notes import generate_release_notes


def run_git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ("git", *args), cwd=root, capture_output=True, text=True
    )
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(detail or f"git {' '.join(args)} failed")
    return result.stdout.strip()


def repo_root() -> Path:
    result = subprocess.run(
        ("git", "rev-parse", "--show-toplevel"), capture_output=True, text=True
    )
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or "not inside a Git repository")
    return Path(result.stdout.strip())


def detect_prev_tag(root: Path) -> str:
    return run_git(root, "describe", "--tags", "--abbrev=0")


def github_client(root: Path) -> GhGitHubClient | None:
    if shutil.which("gh") is None:
        return None
    remote = run_git(root, "remote", "get-url", "origin")
    match = re.search(r"(?:github\.com[:/])([^/]+/[^/]+?)(?:\.git)?$", remote)
    return GhGitHubClient(match.group(1)) if match else None


def write_file(path: str, content: str) -> None:
    if path:
        Path(path).write_text(content.rstrip() + "\n", encoding="utf-8")


def _changelog(entries: Sequence[str]) -> str:
    return "\n".join(
        entry if entry.lstrip().startswith("- ") else f"- {entry}"
        for entry in entries
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Generate notes from bounded release evidence with Codex exec."
    )
    parser.add_argument("--prev-tag", default="")
    parser.add_argument("--release-out", default="")
    parser.add_argument("--changelog-out", default="")
    parser.add_argument("--bump-out", default="")
    parser.add_argument("--bump-reason-out", default="")
    parser.add_argument("--extra", default="", help="Optional generation guidance.")
    args = parser.parse_args(argv)

    root = repo_root()
    base = args.prev_tag or detect_prev_tag(root)
    evidence = collect_release_evidence(
        root,
        base,
        "HEAD",
        github_client=github_client(root),
    )
    generated = generate_release_notes(
        evidence,
        repo_root=root,
        extra_guidance=args.extra,
    )
    changelog = _changelog(generated.changelog_entries)
    write_file(args.release_out, generated.release_notes)
    write_file(args.changelog_out, changelog)
    write_file(args.bump_out, generated.recommended_bump)
    write_file(args.bump_reason_out, generated.rationale)

    if not any(
        (args.release_out, args.changelog_out, args.bump_out, args.bump_reason_out)
    ):
        print(f"===BUMP===\n{generated.recommended_bump}")
        print(f"\n===BUMP_REASON===\n{generated.rationale}")
        print(f"\n===RELEASE_NOTES===\n{generated.release_notes}")
        print(f"\n===CHANGELOG_ENTRY===\n{changelog}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"error: {error}", file=sys.stderr)
        raise SystemExit(1)
