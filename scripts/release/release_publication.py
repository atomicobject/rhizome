#!/usr/bin/env python3
"""Shared exact-release boundary for supported direct publication commands."""
from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess
import sys

from release_gateway import GatewayError, ensure_gateway, verify_gateway_receipt
from release_git import ReleaseGit, ReleaseGitError


class PublicationError(RuntimeError):
    """The tagged client release is not safe to publish."""


def _run(command, cwd):
    return subprocess.run(command, cwd=cwd, capture_output=True, text=True)


def tagged_release(repo_root: Path, *, runner=_run) -> str:
    """Require an exact version tag and clean committed publication input."""
    def git(*args):
        result = runner(("git", *args), repo_root)
        if result.returncode:
            raise PublicationError("HEAD must be an exact release tag in a clean checkout")
        return result.stdout.strip()
    tag = git("describe", "--tags", "--exact-match")
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", tag):
        raise PublicationError("HEAD must be an exact version release tag")
    commit = git("rev-parse", "HEAD")
    if not re.fullmatch(r"[0-9a-f]{40}", commit) or git("rev-parse", f"{tag}^{{commit}}") != commit:
        raise PublicationError("Release tag does not resolve to HEAD")
    if git("status", "--porcelain"):
        raise PublicationError("Release checkout must be clean before publication")
    return commit


def validate_topology(repo_root: Path, release_commit: str, *, runner=_run) -> None:
    # Both ordinary releases and hotfixes must be the current release branch and
    # integrated into main. The gateway source check enforces its stronger public
    # source contract as well, including private/public agreement.
    try:
        complete = ReleaseGit(repo_root, runner).publication_complete(release_commit, "main")
    except ReleaseGitError as error:
        raise PublicationError(str(error)) from error
    if not complete:
        raise PublicationError("Canonical release branches no longer match the release commit")


def gate_direct_publication(repo_root: Path, *, internal=False, receipt_path=None,
                            runner=_run, gateway=None, verifier=None) -> dict:
    """Gate a top-level command, or validate a parent's explicit receipt handoff."""
    repo_root = Path(repo_root).resolve()
    release_commit = tagged_release(repo_root, runner=runner)
    validate_topology(repo_root, release_commit, runner=runner)
    if receipt_path is None:
        result = runner(("git", "rev-parse", "--git-path", f"rhizome-release-gateway/{release_commit}.json"), repo_root)
        if result.returncode or not result.stdout.strip():
            raise PublicationError("Cannot locate the release gateway receipt")
        receipt_path = Path(result.stdout.strip())
        if not receipt_path.is_absolute():
            receipt_path = repo_root / receipt_path
        receipt = (gateway or ensure_gateway)(repo_root, release_commit, receipt_path, internal=internal)
    else:
        receipt = (verifier or verify_gateway_receipt)(repo_root, release_commit, Path(receipt_path), internal=internal)
    # The remote branches or local checkout may have moved during the wait.
    if tagged_release(repo_root, runner=runner) != release_commit:
        raise PublicationError("Release checkout changed while waiting for the gateway")
    validate_topology(repo_root, release_commit, runner=runner)
    return receipt


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--gateway-receipt", type=Path)
    parser.add_argument("--internal", action="store_true")
    args = parser.parse_args(argv)
    try:
        gate_direct_publication(args.repo_root, internal=args.internal, receipt_path=args.gateway_receipt)
    except (PublicationError, GatewayError, OSError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
