#!/usr/bin/env python3
"""Bounded, manual runner for observed agent behavior evaluations."""

from __future__ import annotations

import argparse
import importlib
import json
import os
import queue
import signal
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import evidence
import score
import campaign
import environment_overlay

MAX_STDERR_BYTES = 10 * 1024 * 1024
RunnerError = campaign.RunnerError
PILOT_PROMPTS = campaign.PILOT_PROMPTS
canonical_hash = campaign.canonical_hash
load_manifest = campaign.load_manifest
validate_manifest = campaign.validate_manifest
selected_runs = campaign.selected_runs


def adapter_module(overlay: dict[str, Any] | None = None, overlay_hash: str | None = None):
    if overlay is None:
        return importlib.import_module("codex_adapter")
    module = importlib.import_module("clean_codex_adapter")
    return module.Adapter(overlay, overlay_hash)


def preflight_one(adapter, manifest: dict[str, Any], run: dict[str, Any]) -> dict[str, Any]:
    result = adapter.preflight(manifest, run)
    if not isinstance(result, dict) or not isinstance(result.get("ready"), bool):
        raise RunnerError("adapter preflight must return an object with boolean ready")
    return result


def effective_run(adapter, run: dict[str, Any]) -> dict[str, Any]:
    return adapter.effective_run(run) if hasattr(adapter, "effective_run") else run


def atomic_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + f".tmp-{os.getpid()}")
    temporary.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    os.replace(temporary, path)


class OutputLock:
    def __init__(self, output: Path):
        self.path = output / ".runner.lock"
        self.fd: int | None = None

    def __enter__(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        try:
            self.fd = os.open(self.path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError as error:
            raise RunnerError(f"output is locked: {self.path}") from error
        os.write(self.fd, f"pid={os.getpid()}\n".encode())
        return self

    def __exit__(self, *_):
        if self.fd is not None:
            os.close(self.fd)
        try:
            self.path.unlink()
        except FileNotFoundError:
            pass


def campaign_hash(manifest: dict[str, Any]) -> str:
    return canonical_hash({key: value for key, value in manifest.items() if key != "runs"})


def load_ledger(output: Path, manifest: dict[str, Any]) -> dict[str, Any]:
    path = output / "ledger.json"
    binding = campaign_hash(manifest)
    declared = {run["id"]: {"hash": canonical_hash(run), "run": run} for run in manifest["runs"]}
    order = [run["id"] for run in manifest["runs"]]
    if not path.exists():
        ledger = {"schema_version": 1, "campaign_id": manifest["campaign_id"],
                  "campaign_hash": binding, "declared_order": order, "declared_runs": declared,
                  "launches_reserved": 0, "reservations": [],
                  "total_elapsed_seconds": 0.0, "runs": {}}
        atomic_json(path, ledger)
        return ledger
    ledger = json.loads(path.read_text())
    if ledger.get("campaign_hash") != binding or ledger.get("campaign_id") != manifest["campaign_id"]:
        raise RunnerError("output ledger belongs to a different campaign or bound configuration")
    validate_reservations(ledger)
    prior_order = ledger.get("declared_order", [])
    if order[:len(prior_order)] != prior_order:
        raise RunnerError("manifest runs may only be extended by appending")
    for run_id in prior_order:
        if declared.get(run_id) != ledger.get("declared_runs", {}).get(run_id):
            raise RunnerError(f"previously declared run changed: {run_id}")
    if len(order) > len(prior_order):
        ledger["declared_order"] = order
        ledger["declared_runs"] = declared
        atomic_json(path, ledger)
    return ledger


def validate_reservations(ledger: dict[str, Any]) -> None:
    reservations = ledger.get("reservations")
    if not isinstance(reservations, list) or ledger.get("launches_reserved") != len(reservations):
        raise RunnerError("ledger reservation count is inconsistent")
    previous = None
    seen: set[str] = set()
    for record in reservations:
        if not isinstance(record, dict) or record.get("previous_hash") != previous:
            raise RunnerError("ledger reservation chain is invalid")
        payload = {key: value for key, value in record.items() if key != "hash"}
        if record.get("hash") != canonical_hash(payload):
            raise RunnerError("ledger reservation hash is invalid")
        run_id = record.get("run_id")
        if run_id in seen or run_id not in ledger.get("runs", {}):
            raise RunnerError("ledger reservation run mapping is invalid")
        seen.add(run_id)
        previous = record["hash"]


def validate_output(output: Path, runs: list[dict[str, Any]]) -> Path:
    if output.exists() and output.is_symlink():
        raise RunnerError("output directory cannot be a symlink")
    resolved = output.resolve()
    for run in runs:
        repo = Path(run["repo"]).resolve()
        if resolved.is_relative_to(repo) or repo.is_relative_to(resolved):
            raise RunnerError("output directory must be outside every fixture repository")
    return resolved


def reserve(ledger: dict[str, Any], manifest: dict[str, Any], run: dict[str, Any], output: Path) -> None:
    validate_reservations(ledger)
    if run["id"] in ledger["runs"] or (output / run["id"]).exists():
        raise RunnerError(f"run id already used: {run['id']}")
    if ledger["launches_reserved"] >= manifest["max_launches"]:
        raise RunnerError("manifest launch limit exhausted")
    if ledger["total_elapsed_seconds"] >= manifest["total_seconds"]:
        raise RunnerError("manifest aggregate time limit exhausted")
    ledger["launches_reserved"] += 1
    ledger["runs"][run["id"]] = {
        "id": run["id"], "case_id": run["case_id"], "arm": run["arm"],
        "status": "reserved", "reserved_at": now(), "run_hash": canonical_hash(run),
    }
    reservation = {"run_id": run["id"], "run_hash": canonical_hash(run),
                   "reserved_at": ledger["runs"][run["id"]]["reserved_at"],
                   "previous_hash": ledger["reservations"][-1]["hash"] if ledger["reservations"] else None}
    reservation["hash"] = canonical_hash(reservation)
    ledger["reservations"].append(reservation)
    atomic_json(output / "ledger.json", ledger)


def now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _reader(stream, destination: Path, channel: str, messages: queue.Queue, limit: int | None = None):
    total = 0
    retained = 0
    with destination.open("wb") as target:
        while True:
            chunk = stream.read1(65536)
            if not chunk:
                break
            total += len(chunk)
            writable = chunk if limit is None else chunk[:max(0, limit - retained)]
            if writable:
                target.write(writable)
                target.flush()
                retained += len(writable)
            messages.put((channel, chunk))
    messages.put((channel, {"total_bytes": total, "retained_bytes": retained,
                            "truncated": retained < total}))


def signal_group(process: subprocess.Popen, sent_signal: int) -> None:
    try:
        os.killpg(process.pid, sent_signal)
    except (ProcessLookupError, PermissionError):
        pass


def execute(argv: list[str], env: dict[str, str], prompt: str, result_dir: Path, timeout: float) -> dict[str, Any]:
    if not argv or not all(isinstance(item, str) for item in argv):
        raise RunnerError("adapter command must be a non-empty string argument list")
    process = subprocess.Popen(
        argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env=env, start_new_session=True,
    )
    assert process.stdin and process.stdout and process.stderr
    process.stdin.write(prompt.encode())
    process.stdin.close()
    messages: queue.Queue = queue.Queue(maxsize=128)
    threads = [
        threading.Thread(target=_reader, args=(process.stdout, result_dir / "events.jsonl", "stdout", messages), daemon=True),
        threading.Thread(target=_reader, args=(process.stderr, result_dir / "stderr.txt", "stderr", messages, MAX_STDERR_BYTES), daemon=True),
    ]
    for thread in threads:
        thread.start()
    deadline = time.monotonic() + timeout
    grace = min(1.0, max(0.01, timeout * 0.1))
    terminate_at = deadline - grace
    open_channels = 2
    stream_metadata: dict[str, Any] = {}
    timed_out = False
    term_sent = False
    try:
        while open_channels:
            current = time.monotonic()
            if not term_sent and process.poll() is None and current >= terminate_at:
                timed_out = True
                term_sent = True
                signal_group(process, signal.SIGTERM)
            remaining = deadline - current
            if remaining <= 0:
                timed_out = True
                signal_group(process, signal.SIGKILL)
                break
            try:
                next_signal = deadline if term_sent else terminate_at
                channel, chunk = messages.get(timeout=min(0.05, remaining, max(0.001, next_signal - current)))
            except queue.Empty:
                continue
            if isinstance(chunk, dict):
                stream_metadata[channel] = chunk
                open_channels -= 1
            else:
                destination = sys.stdout.buffer if channel == "stdout" else sys.stderr.buffer
                destination.write(chunk)
                destination.flush()
        if process.poll() is None:
            remaining = max(0, deadline - time.monotonic())
            try:
                process.wait(timeout=remaining)
            except subprocess.TimeoutExpired:
                timed_out = True
                signal_group(process, signal.SIGKILL)
                process.wait()
    except KeyboardInterrupt:
        signal_group(process, signal.SIGTERM)
        signal_group(process, signal.SIGKILL)
        process.wait()
        return {"status": "cancelled", "exit_code": process.returncode, "timed_out": False}
    finally:
        for thread in threads:
            thread.join(timeout=max(0.0, deadline - time.monotonic()))
        while True:
            try:
                channel, item = messages.get_nowait()
            except queue.Empty:
                break
            if isinstance(item, dict):
                stream_metadata[channel] = item
        process.stdout.close()
        process.stderr.close()
        atomic_json(result_dir / "streams.json", stream_metadata)
    return {
        "status": "failed" if timed_out or process.returncode != 0 else "complete",
        "exit_code": process.returncode,
        "timed_out": timed_out,
    }


def run_one(adapter, manifest: dict[str, Any], manifest_hash: str, run: dict[str, Any], output: Path,
            ledger: dict[str, Any]) -> None:
    if run["id"] in ledger["runs"] or (output / run["id"]).exists():
        raise RunnerError(f"run id already used: {run['id']}")
    execution_run = effective_run(adapter, run)
    preflight = preflight_one(adapter, manifest, execution_run)
    if not preflight["ready"]:
        ledger["runs"][run["id"]] = {
            "id": run["id"], "case_id": run["case_id"], "arm": run["arm"],
            "status": "blocked", "run_hash": canonical_hash(run), "preflight": preflight,
            "finished_at": now(),
        }
        atomic_json(output / "ledger.json", ledger)
        raise RunnerError(f"preflight blocked {run['id']}: {preflight}")
    reserve(ledger, manifest, run, output)
    result_dir = output / run["id"]
    entry = ledger["runs"][run["id"]]
    before: dict[str, Any] | None = None
    process_elapsed = 0.0
    try:
        result_dir.mkdir()
        atomic_json(result_dir / "manifest.json", {"manifest_hash": manifest_hash, "manifest": manifest, "run": run})
        atomic_json(result_dir / "preflight.json", preflight)
        if hasattr(adapter, "provenance"):
            atomic_json(result_dir / "environment.json", adapter.provenance(execution_run))
        (result_dir / "prompt.txt").write_text(run["prompt"])
        repo = Path(execution_run["repo"])
        before = evidence.snapshot(repo)
        atomic_json(result_dir / "before.json", before)
        provenance = evidence.capture_provenance(
            repo, execution_run, result_dir,
            adapter_files=getattr(adapter, "source_files", ("codex_adapter.py",)))
        atomic_json(result_dir / "provenance.json", provenance)
        argv = adapter.command(manifest, execution_run, result_dir)
        adapter_env = adapter.environment(manifest, execution_run)
        if not isinstance(adapter_env, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in adapter_env.items()):
            raise RunnerError("adapter environment must be a string mapping")
        atomic_json(result_dir / "command.json", {"argv": argv})
        remaining_total = manifest["total_seconds"] - ledger["total_elapsed_seconds"]
        launched = time.monotonic()
        outcome = execute(argv, adapter_env, run["prompt"], result_dir,
                          min(manifest["per_run_seconds"], remaining_total))
        process_elapsed = time.monotonic() - launched
        entry.update(outcome, process_elapsed_seconds=process_elapsed,
                     process_finished_at=now())
        ledger["total_elapsed_seconds"] += process_elapsed
        atomic_json(output / "ledger.json", ledger)
    except BaseException as error:
        if entry.get("status") == "reserved":
            entry.update(status="cancelled" if isinstance(error, KeyboardInterrupt) else "failed",
                         launch_error=f"{type(error).__name__}: {error}")
            atomic_json(output / "ledger.json", ledger)
        raise

    artifact_errors: list[dict[str, str]] = []
    after: dict[str, Any] | None = None
    delta: dict[str, list[str]] | None = None

    def capture(name: str, operation):
        try:
            return operation()
        except Exception as error:
            artifact_errors.append({"artifact": name, "error": f"{type(error).__name__}: {error}"})
            return None

    capture("final.txt", lambda: (result_dir / "final.txt").touch(exist_ok=True))
    after = capture("after.json", lambda: evidence.snapshot(repo))
    if after is not None:
        capture("after.json.write", lambda: atomic_json(result_dir / "after.json", after))
        delta = evidence.changes(before, after)
        capture("changes.json", lambda: atomic_json(result_dir / "changes.json", delta))
        capture("added-files", lambda: evidence.copy_added_files(repo, after, delta, result_dir / "added-files"))
    capture("changes.patch", lambda: evidence.write_changes_patch(repo, result_dir / "changes.patch"))
    fixtures = capture("fixtures.import", lambda: importlib.import_module("fixtures"))
    checks = capture("checks.json", lambda: fixtures.check(run["case_id"], destination=repo, before=before)
                     if fixtures is not None else None)
    if checks is not None:
        capture("checks.json.write", lambda: atomic_json(result_dir / "checks.json", checks))
        entry["checks"] = checks
    parsed = capture("events-summary.json", lambda: evidence.parse_events(result_dir / "events.jsonl"))
    if parsed is not None:
        capture("events-summary.json.write", lambda: atomic_json(result_dir / "events-summary.json", parsed))
        entry["evidence"] = parsed
    entry.update(artifact_errors=artifact_errors, finished_at=now())
    atomic_json(output / "ledger.json", ledger)
    if artifact_errors:
        raise RunnerError(f"post-launch evidence capture failed for {run['id']}; see ledger")


def command_list(manifest: dict[str, Any]) -> None:
    for run in manifest["runs"]:
        print(f"{run['id']}\t{run['case_id']}\t{run['arm']}")


def command_preflight(adapter, manifest: dict[str, Any], runs: list[dict[str, Any]], dry: bool) -> int:
    results = []
    for run in runs:
        execution_run = effective_run(adapter, run)
        item = preflight_one(adapter, manifest, execution_run)
        record = {"run_id": run["id"], "preflight": item}
        if dry and item["ready"]:
            record["argv"] = adapter.command(manifest, execution_run, Path("<output>") / run["id"])
            record["environment_keys"] = sorted(adapter.environment(manifest, execution_run))
        results.append(record)
    print(json.dumps(results, indent=2, sort_keys=True))
    return 0 if all(item["preflight"]["ready"] for item in results) else 2


def load_scores(output: Path) -> list[dict[str, Any]]:
    ledger = json.loads((output / "ledger.json").read_text())
    results = []
    for run_id, entry in ledger.get("runs", {}).items():
        review_path = output / run_id / "review.json"
        review = json.loads(review_path.read_text()) if review_path.exists() else None
        results.append(score.score_run(entry, review))
    return results


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser()
    sub = result.add_subparsers(dest="command", required=True)
    for name in ("list", "preflight", "dry-run", "run", "compare"):
        child = sub.add_parser(name)
        child.add_argument("--manifest", required=True, type=Path)
        child.add_argument("--output", type=Path)
        child.add_argument("--run-id", action="append")
        if name == "compare":
            child.add_argument("--other-output", type=Path)
        child.add_argument("--environment", type=Path)
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        manifest, manifest_hash = load_manifest(args.manifest)
        runs = selected_runs(manifest, args.run_id)
        if args.command == "list":
            command_list(manifest)
            return 0
        if args.command == "compare":
            if args.output is None:
                raise RunnerError("compare requires --output")
            left = load_scores(args.output)
            if args.other_output:
                right = load_scores(args.other_output)
            else:
                arms = sorted({item.get("arm") for item in left})
                if len(arms) != 2:
                    raise RunnerError("single-output compare requires exactly two arms")
                first_arm, second_arm = arms
                right = [item for item in left if item.get("arm") == second_arm]
                left = [item for item in left if item.get("arm") == first_arm]
            print(json.dumps(score.compare(left, right), indent=2, sort_keys=True))
            return 0
        overlay = overlay_hash = None
        if args.environment is not None:
            overlay, overlay_hash = environment_overlay.load(args.environment.resolve(), manifest, manifest_hash)
            if any(run["id"] not in overlay["expected_run_ids"] for run in runs):
                raise RunnerError("selected run is outside the environment continuation")
            if args.output is None:
                raise RunnerError("environment continuation requires its bound --output")
            environment_overlay.validate_ledger(overlay, args.output.resolve())
            continuation_ledger = load_ledger(args.output.resolve(), manifest)
            if args.command == "run":
                remaining = [run_id for run_id in overlay["expected_run_ids"]
                             if run_id not in continuation_ledger["runs"]]
                selected = [run["id"] for run in runs]
                if selected != remaining[:len(selected)]:
                    raise RunnerError("continuation runs must launch in frozen declared order")
        adapter = adapter_module(overlay, overlay_hash)
        if args.command in {"preflight", "dry-run"}:
            return command_preflight(adapter, manifest, runs, args.command == "dry-run")
        if args.output is None:
            raise RunnerError("run requires an explicit --output")
        if not args.run_id:
            raise RunnerError("run requires at least one explicit --run-id")
        output = validate_output(args.output, manifest["runs"])
        validate_output(output, [effective_run(adapter, run) for run in manifest["runs"]
                                 if run["id"] in getattr(adapter, "overlay", {}).get("runs", {})])
        with OutputLock(output):
            ledger = load_ledger(output, manifest)
            for run in runs:
                run_one(adapter, manifest, manifest_hash, run, output, ledger)
        return 0
    except Exception as error:
        print(f"error: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
