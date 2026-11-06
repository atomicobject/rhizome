#!/usr/bin/env python3
"""Explicit resumable native-candidate stage before gateway/client publication."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time
import uuid
import zipfile

from desktop_candidates import CandidateError, MANIFEST, NATIVE_TARGETS, read_candidate, sha256
from desktop_source import validate_source
from release_gateway import _save
from release_gateway_runs import (API, DISCOVERY_SECONDS, POLL_SECONDS, REPOSITORY, WAIT_SECONDS, GatewayError,
                                  api, command, fetch_run, find_run, successful, workflow_id)
from release_gateway_source import SourceError


WORKFLOW = "desktop-candidates.yml"
ARTIFACT_NAMES = {target: f"native-desktop-{target}" for target in NATIVE_TARGETS}
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")


def title(attempt: dict, source: dict) -> str:
    return f"Desktop candidates {attempt['request_id']} {source['source_commit']} {source['version']}"


def load(path: Path, source: dict) -> dict | None:
    if not path.exists():
        return None
    try:
        receipt = json.loads(path.read_text())
        attempt = receipt["attempt"]
        if (not isinstance(attempt, dict) or receipt.get("schemaVersion") != 1 or receipt.get("source") != source
                or not re.fullmatch(r"[0-9a-f]{32}", attempt.get("request_id", ""))
                or attempt.get("state") not in {"dispatch_uncertain", "pending", "verified", "success", "failed"}
                or (attempt.get("run_id") is not None and (type(attempt["run_id"]) is not int or attempt["run_id"] <= 0))):
            raise ValueError("identity")
        return receipt
    except (OSError, ValueError, TypeError, KeyError) as error:
        raise CandidateError("native desktop receipt is invalid or belongs to another release") from error


def artifact_listing(runner, run_id: int) -> dict:
    pages = api(runner, f"{API}/runs/{run_id}/artifacts?per_page=100", "--paginate", "--slurp")
    if not isinstance(pages, list) or not pages:
        raise CandidateError("native candidate artifact listing is incomplete")
    all_artifacts, totals = [], []
    for page in pages:
        if (not isinstance(page, dict) or type(page.get("total_count")) is not int
                or not isinstance(page.get("artifacts"), list)):
            raise CandidateError("native candidate artifact listing is invalid")
        totals.append(page["total_count"])
        all_artifacts.extend(page["artifacts"])
    if len(all_artifacts) < max(totals) or any(not isinstance(item, dict) for item in all_artifacts):
        raise CandidateError("native candidate artifact listing was truncated")
    result = {}
    for target, name in ARTIFACT_NAMES.items():
        matches = [item for item in all_artifacts if item.get("name") == name]
        if len(matches) != 1:
            raise CandidateError("native build did not produce exactly one artifact for every target")
        item = matches[0]
        if (type(item.get("id")) is not int or item["id"] <= 0
                or not isinstance(item.get("digest"), str) or not DIGEST.fullmatch(item["digest"])
                or not isinstance(item.get("workflow_run"), dict) or item["workflow_run"].get("id") != run_id):
            raise CandidateError("native candidate artifact identity does not match its exact workflow run")
        result[target] = {"id": item["id"], "digest": item["digest"], "expired": item.get("expired") is True}
    return result


def download(artifact: dict, destination: Path) -> None:
    if artifact["expired"]:
        raise CandidateError("native candidate artifact expired before it was retained; rerun the failed stage")
    archive = destination.parent / f"{artifact['id']}.zip"
    try:
        with archive.open("wb") as output:
            subprocess.run(("gh", "api", f"{API}/artifacts/{artifact['id']}/zip"),
                           stdout=output, stderr=subprocess.PIPE, check=True, timeout=180)
        if "sha256:" + sha256(archive) != artifact["digest"]:
            raise CandidateError("downloaded native artifact digest differs from GitHub's exact-run digest")
        with zipfile.ZipFile(archive) as source:
            names = set()
            for entry in source.infolist():
                path = Path(entry.filename)
                if ("\\" in entry.filename or ":" in entry.filename or path.is_absolute() or ".." in path.parts
                        or (entry.external_attr >> 16) & 0o170000 == 0o120000 or str(path) in names):
                    raise CandidateError("native candidate archive contains an unsafe path or symlink")
                names.add(str(path))
            source.extractall(destination)
    except (OSError, subprocess.SubprocessError, zipfile.BadZipFile) as error:
        raise CandidateError("native candidate download/extraction failed; resume its retained run") from error


def verify_files(directory: Path, source: dict, *, expected: dict | None = None) -> dict:
    hashes = {}
    for target in sorted(NATIVE_TARGETS):
        candidate = directory / target
        raw = json.loads((candidate / MANIFEST).read_text())
        digest = raw.get("canonicalCliSha256")
        manifest, _ = read_candidate(candidate, version=source["version"], commit=source["source_commit"],
                                     canonical_hash=digest, production=True)
        if manifest["target"] != target:
            raise CandidateError("native candidate target differs from its producer artifact name")
        hashes[target] = {"manifest": sha256(candidate / MANIFEST), "cli": digest,
                          "package": manifest["artifact"]["sha256"]}
    if expected is not None and hashes != expected:
        raise CandidateError("retained native candidate files changed since their verified download")
    return hashes


def verify_prepared(root: Path, commit: str, version: str, receipt_path: Path, output: Path, *,
                    runner=command, validator=validate_source) -> dict:
    """Publication boundary: recheck exact source, producer run, artifact IDs and retained bytes."""
    source = validator(root, commit, version, runner=runner)
    receipt = load(receipt_path, source)
    if receipt is None or receipt["attempt"]["state"] != "success" or "files" not in receipt:
        raise CandidateError("native desktop candidates require a successful exact-run receipt")
    attempt = receipt["attempt"]
    workflow = workflow_id(runner, workflow=WORKFLOW)
    context = {"workflow": WORKFLOW, "title": title(attempt, source)}
    if not successful(fetch_run(runner, attempt, source, workflow, **context)):
        raise CandidateError("native desktop producer has not completed successfully")
    if find_run(runner, attempt, source, workflow, **context) is None:
        raise CandidateError("native desktop producer disappeared from the correlated listing")
    artifacts = artifact_listing(runner, attempt["run_id"])
    if {t: {k: v for k, v in a.items() if k != "expired"} for t, a in artifacts.items()} != receipt.get("artifacts"):
        raise CandidateError("native desktop artifact identities differ from the retained receipt")
    return verify_files(output, source, expected=receipt["files"])


def ensure_candidates(root: Path, commit: str, version: str, receipt_path: Path, output: Path, *, runner=command,
                      validator=validate_source, downloader=download, clock=time.monotonic, sleep=time.sleep,
                      output_fn=print) -> Path:
    source = validator(root, commit, version, runner=runner)
    receipt = load(receipt_path, source)
    expected_workflow = workflow_id(runner, workflow=WORKFLOW)
    if receipt is None or receipt["attempt"]["state"] == "failed":
        receipt = {"schemaVersion": 1, "source": source,
                   "attempt": {"request_id": uuid.uuid4().hex, "state": "dispatch_uncertain"}}
        _save(receipt_path, receipt)
        attempt = receipt["attempt"]
        try:
            runner(("gh", "workflow", "run", WORKFLOW, "--repo", REPOSITORY, "--ref", "main",
                    "-f", f"source_commit={source['source_commit']}", "-f", f"version={source['version']}",
                    "-f", f"request_id={attempt['request_id']}"))
        except Exception:
            output_fn("Native desktop dispatch response was uncertain; reconciling the saved request ID.")
    attempt = receipt["attempt"]
    context = {"workflow": WORKFLOW, "title": title(attempt, source)}
    started = clock()
    while clock() - started <= WAIT_SECONDS:
        if attempt.get("run_id") is None:
            run = find_run(runner, attempt, source, expected_workflow, **context)
            if run is None:
                if clock() - started >= DISCOVERY_SECONDS:
                    raise CandidateError("no correlated native candidate run found; retained dispatch remains uncertain")
                sleep(POLL_SECONDS)
                continue
            attempt.update(run_id=run["id"], run_url=run["html_url"], state="pending")
            _save(receipt_path, receipt)
            output_fn(f"Native desktop candidates: {attempt['run_url']}")
        try:
            complete = successful(fetch_run(runner, attempt, source, expected_workflow, **context))
        except GatewayError:
            attempt["state"] = "failed"
            _save(receipt_path, receipt)
            raise
        if complete:
            if find_run(runner, attempt, source, expected_workflow, **context) is None:
                raise CandidateError("successful native candidate run is missing from the exact listing")
            artifacts = artifact_listing(runner, attempt["run_id"])
            if receipt.get("artifacts") and {t: {k: v for k, v in a.items() if k != "expired"} for t, a in artifacts.items()} != receipt["artifacts"]:
                raise CandidateError("native candidate artifact identity changed after verification")
            if output.exists():
                if "files" not in receipt:
                    raise CandidateError("unexpected native output exists without a verified receipt")
                verify_files(output, source, expected=receipt["files"])
            else:
                output.parent.mkdir(parents=True, exist_ok=True)
                with tempfile.TemporaryDirectory(prefix=".native-desktop-", dir=output.parent) as temporary:
                    staging = Path(temporary) / "candidates"
                    staging.mkdir()
                    for target, artifact in artifacts.items():
                        downloader(artifact, staging / target)
                    files = verify_files(staging, source)
                    receipt.update(files=files, artifacts={t: {k: v for k, v in a.items() if k != "expired"}
                                                         for t, a in artifacts.items()})
                    attempt["state"] = "verified"
                    _save(receipt_path, receipt)
                    staging.rename(output)
            if validator(root, commit, version, runner=runner) != source:
                raise CandidateError("canonical desktop source changed while waiting for native builds")
            attempt["state"] = "success"
            _save(receipt_path, receipt)
            return output
        sleep(POLL_SECONDS)
    raise CandidateError("native candidate build timed out; resume with its retained receipt")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--verify-only", action="store_true")
    args = parser.parse_args()
    try:
        if args.verify_only:
            verify_prepared(args.repo_root, args.commit, args.version, args.receipt, args.output)
            print("Native desktop producer and retained candidates verified.")
        else:
            print(ensure_candidates(args.repo_root, args.commit, args.version, args.receipt, args.output))
    except (CandidateError, GatewayError, SourceError, OSError, ValueError) as error:
        print(f"native desktop preparation: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
