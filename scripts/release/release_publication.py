#!/usr/bin/env python3
"""Shared exact-release boundary for supported direct publication commands."""
from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess
import sys

from release_gateway import GatewayError, _load, _source, ensure_gateway, verify_gateway_receipt
from release_gateway_runs import command, fetch_run, find_run, successful, workflow_id
from release_git import ReleaseGit, ReleaseGitError


class PublicationError(RuntimeError):
    """The tagged client release is not safe to publish."""


def _run(command, cwd):
    return subprocess.run(command, cwd=cwd, capture_output=True, text=True)


def tagged_release_identity(repo_root: Path, *, runner=_run, allow_generated=False) -> tuple[str, str]:
    """Require an exact version tag and clean committed publication input."""
    def git(*args):
        result = runner(("git", *args), repo_root)
        if result.returncode:
            raise PublicationError("HEAD must be an exact release tag in a clean checkout")
        return result.stdout.rstrip("\r\n")
    tag = git("describe", "--tags", "--exact-match")
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", tag):
        raise PublicationError("HEAD must be an exact version release tag")
    commit = git("rev-parse", "HEAD")
    if not re.fullmatch(r"[0-9a-f]{40}", commit) or git("rev-parse", f"{tag}^{{commit}}") != commit:
        raise PublicationError("Release tag does not resolve to HEAD")
    dirty = git("status", "--porcelain")
    # make web-build deterministically updates this tracked entry during packaging.
    allowed = {" M pkg/app/web/assets/dist/index.html"} if allow_generated else set()
    if any(line not in allowed for line in dirty.splitlines()):
        raise PublicationError("Release checkout must be clean before publication")
    return tag.removeprefix("v"), commit


def tagged_release(repo_root: Path, *, runner=_run, allow_generated=False) -> str:
    return tagged_release_identity(repo_root, runner=runner, allow_generated=allow_generated)[1]


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


def verify_terminal_gateway_receipt(repo_root: Path, release_commit: str, receipt_path: Path, *,
                                    internal=False, runner=command) -> dict:
    """Read-only CI proof from the prerequisite job, never a deployment retry."""
    receipt = _load(Path(receipt_path))
    if receipt is None or receipt["attempt"]["state"] != "success":
        raise GatewayError("A successful terminal gateway receipt is required")
    source = _source(repo_root, release_commit, receipt, internal, runner)
    attempt = receipt["attempt"]
    if attempt.get("run_id") is None:
        raise GatewayError("Terminal gateway receipt has no deployment run")
    workflow = workflow_id(runner)
    if find_run(runner, attempt, source, workflow) is None:
        raise GatewayError("Terminal gateway receipt run is missing")
    if not successful(fetch_run(runner, attempt, source, workflow)):
        raise GatewayError("Terminal gateway receipt run has not completed")
    return receipt


def gate_direct_publication(repo_root: Path, *, internal=False, receipt_path=None,
                            runner=_run, gateway=None, verifier=None, allow_generated=False,
                            terminal_receipt=False) -> dict:
    """Gate a top-level command, or validate a parent's explicit receipt handoff."""
    repo_root = Path(repo_root).resolve()
    release_commit = tagged_release(repo_root, runner=runner, allow_generated=allow_generated)
    validate_topology(repo_root, release_commit, runner=runner)
    if terminal_receipt and receipt_path is None:
        raise PublicationError("terminal gateway verification requires an explicit receipt")
    if receipt_path is None:
        result = runner(("git", "rev-parse", "--git-path", f"rhizome-release-gateway/{release_commit}.json"), repo_root)
        if result.returncode or not result.stdout.strip():
            raise PublicationError("Cannot locate the release gateway receipt")
        receipt_path = Path(result.stdout.strip())
        if not receipt_path.is_absolute():
            receipt_path = repo_root / receipt_path
        receipt = (gateway or ensure_gateway)(repo_root, release_commit, receipt_path, internal=internal)
    else:
        operation = verify_terminal_gateway_receipt if terminal_receipt else verify_gateway_receipt
        receipt = (verifier or operation)(repo_root, release_commit, Path(receipt_path), internal=internal)
    # The remote branches or local checkout may have moved during the wait.
    if tagged_release(repo_root, runner=runner, allow_generated=allow_generated) != release_commit:
        raise PublicationError("Release checkout changed while waiting for the gateway")
    validate_topology(repo_root, release_commit, runner=runner)
    # The child advertiser borrows this live handoff instead of deploying twice.
    return {**receipt, "receiptPath": str(Path(receipt_path))}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, required=True)
    receipts = parser.add_mutually_exclusive_group()
    receipts.add_argument("--gateway-receipt", type=Path)
    receipts.add_argument("--terminal-gateway-receipt", type=Path)
    parser.add_argument("--internal", action="store_true")
    args = parser.parse_args(argv)
    try:
        gate_direct_publication(args.repo_root, internal=args.internal,
                                receipt_path=args.terminal_gateway_receipt or args.gateway_receipt,
                                terminal_receipt=args.terminal_gateway_receipt is not None)
    except (PublicationError, GatewayError, OSError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
