#!/usr/bin/env python3
"""Deploy the pinned public gateway before supported client publication."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import uuid

from release_gateway_runs import (
    DISCOVERY_SECONDS, POLL_SECONDS, REPOSITORY, WAIT_SECONDS, WORKFLOW,
    GatewayError, command, fetch_run, find_run, successful, workflow_id,
)
from release_gateway_source import SourceError, resolve_source


_PROCESS_INSTANCE = uuid.uuid4().hex


def _process_start(pid):
    try:
        result = subprocess.run(("ps", "-o", "lstart=", "-p", str(pid)),
                                capture_output=True, text=True, timeout=5)
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
    except (OSError, subprocess.SubprocessError):
        pass
    return _PROCESS_INSTANCE if pid == os.getpid() else None


def _live_handoff(attempt):
    """Only the publisher process or its descendants may borrow its receipt."""
    owner = attempt.get("publisher", {})
    if not isinstance(owner, dict) or type(owner.get("pid")) is not int or not owner.get("started"):
        return False
    pid = os.getpid()
    for _ in range(64):
        if pid == owner["pid"]:
            return _process_start(pid) == owner["started"]
        if pid <= 1:
            return False
        try:
            result = subprocess.run(("ps", "-o", "ppid=", "-p", str(pid)),
                                    capture_output=True, text=True, timeout=5)
            if result.returncode:
                return False
            pid = int(result.stdout.strip())
        except (ValueError, OSError, subprocess.SubprocessError):
            return False
    return False


def _load(path: Path):
    if not path.exists():
        return None
    try:
        receipt = json.loads(path.read_text(encoding="utf-8"))
        source = receipt["source"]
        attempt = receipt["attempt"]
        if (receipt.get("version") != 1 or not isinstance(source, dict)
                or source.get("repository") != REPOSITORY
                or not isinstance(attempt, dict)
                or not re.fullmatch(r"[0-9a-f]{32}", attempt.get("request_id", ""))
                or attempt.get("state") not in {"prepared", "dispatch_uncertain", "pending", "success", "failed"}
                or (attempt.get("run_id") is not None and
                    (type(attempt["run_id"]) is not int or attempt["run_id"] <= 0))):
            raise ValueError("receipt identity")
        return receipt
    except (OSError, ValueError, TypeError, KeyError) as error:
        raise GatewayError("Invalid gateway receipt; publication is blocked") from error


def _save(path: Path, receipt: dict):
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        json.dump(receipt, handle, indent=2, sort_keys=True)
        handle.write("\n")
        handle.flush()
        os.fsync(handle.fileno())
    os.replace(temporary, path)


def _source(repo_root, release_commit, receipt, internal, runner):
    try:
        source = resolve_source(Path(repo_root), release_commit,
                                saved_source=receipt["source"].get("source_commit") if receipt else None,
                                internal=internal, runner=runner)
    except SourceError as error:
        raise GatewayError(str(error)) from error
    if source.get("repository") != REPOSITORY or (receipt and receipt["source"] != source):
        raise GatewayError("Gateway receipt source identity does not match this release")
    return source


def ensure_gateway(repo_root: Path, release_commit: str, receipt_path: Path, *,
                   internal=False, runner=None, clock=None, sleep=None, output=print) -> dict:
    """Resume an uncertain/pending attempt, or verify a fresh deployment attempt."""
    runner = runner or command
    clock = clock or time.monotonic
    sleep = sleep or time.sleep
    receipt_path = Path(receipt_path)
    receipt = _load(receipt_path)
    source = _source(repo_root, release_commit, receipt, internal, runner)
    expected_workflow = workflow_id(runner)
    if receipt is None or receipt["attempt"]["state"] in {"success", "failed"}:
        receipt = {"version": 1, "source": source,
                   "attempt": {"request_id": uuid.uuid4().hex, "state": "prepared"}}
        _save(receipt_path, receipt)
        attempt = receipt["attempt"]
        # Persist before sending: a crash or uncertain dispatch response must never
        # cause a second dispatch with a new request ID on resume.
        attempt["state"] = "dispatch_uncertain"
        _save(receipt_path, receipt)
        try:
            runner(("gh", "workflow", "run", WORKFLOW, "--repo", REPOSITORY,
                    "--ref", "main", "-f", f"source_commit={source['source_commit']}",
                    "-f", f"request_id={attempt['request_id']}"))
        except Exception as error:
            output("Gateway dispatch response was uncertain; reconciling its saved request ID.")
            # The server may have accepted the request. Only correlation can tell.
            dispatch_error = error
        else:
            dispatch_error = None
    else:
        attempt = receipt["attempt"]
        dispatch_error = None
        if attempt["state"] == "prepared":
            # Even a pre-dispatch crash is deliberately not blindly retried.
            attempt["state"] = "dispatch_uncertain"
            _save(receipt_path, receipt)
    start = clock()
    announced = False
    while clock() - start <= WAIT_SECONDS:
        if attempt.get("run_id") is None:
            run = find_run(runner, attempt, source, expected_workflow)
            if run is None:
                if clock() - start >= DISCOVERY_SECONDS:
                    raise GatewayError("No correlated gateway run found; saved dispatch remains uncertain") from dispatch_error
                sleep(POLL_SECONDS)
                continue
            attempt.update(run_id=run["id"], run_url=run["html_url"], state="pending")
            _save(receipt_path, receipt)
        if not announced:
            output(f"Gateway deployment: {attempt['run_url']}")
            announced = True
        run = fetch_run(runner, attempt, source, expected_workflow)
        try:
            complete = successful(run)
        except GatewayError:
            attempt["state"] = "failed"
            _save(receipt_path, receipt)
            raise
        if complete:
            # Recheck uniqueness at completion, including resumed known run IDs.
            matched = find_run(runner, attempt, source, expected_workflow)
            if matched is None:
                raise GatewayError("Successful gateway run is missing from the correlated listing")
            attempt["state"] = "success"
            attempt["publisher"] = {"pid": os.getpid(), "started": _process_start(os.getpid())}
            _save(receipt_path, receipt)
            return receipt
        sleep(POLL_SECONDS)
    raise GatewayError("Timed out waiting for gateway deployment; resume with the retained receipt")


def verify_gateway_receipt(repo_root: Path, release_commit: str, receipt_path: Path, *,
                           internal=False, runner=None) -> dict:
    """Recheck a live publisher handoff, renewing receipts from old invocations."""
    runner = runner or command
    receipt = _load(Path(receipt_path))
    if receipt is None or receipt["attempt"]["state"] != "success":
        raise GatewayError("A successful gateway receipt is required before publication")
    source = _source(repo_root, release_commit, receipt, internal, runner)
    if not _live_handoff(receipt["attempt"]):
        return ensure_gateway(repo_root, release_commit, receipt_path,
                              internal=internal, runner=runner)
    attempt = receipt["attempt"]
    if attempt.get("run_id") is None:
        raise GatewayError("Gateway receipt has no deployment run")
    expected_workflow = workflow_id(runner)
    if find_run(runner, attempt, source, expected_workflow) is None:
        raise GatewayError("Gateway receipt run is missing")
    if not successful(fetch_run(runner, attempt, source, expected_workflow)):
        raise GatewayError("Gateway receipt run has not completed")
    return receipt


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--release-commit", required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    parser.add_argument("--internal", action="store_true")
    parser.add_argument("--verify-receipt", action="store_true",
                        help="internal handoff within one uninterrupted gated publish invocation")
    args = parser.parse_args(argv)
    operation = verify_gateway_receipt if args.verify_receipt else ensure_gateway
    try:
        operation(args.repo_root, args.release_commit, args.receipt, internal=args.internal)
    except GatewayError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
