"""Validation for the bounded clean-home continuation overlay."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from campaign import RunnerError, canonical_hash

REMAINING_RUN_IDS = ("a02-baseline", "a01-candidate", "a02-candidate")


def load(path: Path, manifest: dict[str, Any], manifest_hash: str) -> tuple[dict[str, Any], str]:
    overlay = json.loads(path.read_text())
    if (overlay.get("schema_version") != 1
            or overlay.get("kind") != "clean-codex-environment-continuation"
            or overlay.get("manifest_sha256") != manifest_hash
            or overlay.get("expected_run_ids") != list(REMAINING_RUN_IDS)):
        raise RunnerError("environment overlay does not match the frozen continuation")
    output = Path(overlay.get("output_root", ""))
    checkpoint = overlay.get("ledger_checkpoint")
    if (not output.is_absolute() or not isinstance(checkpoint, dict)
            or set(checkpoint) != {"ledger_sha256", "launches_reserved", "reservation_head",
                                   "consumed_run_ids", "consumed_run_hash", "total_elapsed_seconds"}
            or checkpoint["launches_reserved"] != 1
            or checkpoint["consumed_run_ids"] != ["a01-baseline"]
            or not re.fullmatch(r"[0-9a-f]{64}", checkpoint["ledger_sha256"])
            or not re.fullmatch(r"[0-9a-f]{64}", checkpoint["reservation_head"])
            or not re.fullmatch(r"[0-9a-f]{64}", checkpoint["consumed_run_hash"])
            or not isinstance(checkpoint["total_elapsed_seconds"], (int, float))):
        raise RunnerError("environment overlay ledger checkpoint is invalid")
    runs = overlay.get("runs")
    if not isinstance(runs, dict) or list(runs) != list(REMAINING_RUN_IDS):
        raise RunnerError("environment overlay must define the three remaining runs in order")
    manifest_ids = {run["id"] for run in manifest["runs"]}
    if not set(runs).issubset(manifest_ids):
        raise RunnerError("environment overlay names a run outside the frozen manifest")
    homes: set[Path] = set()
    for run_id, entry in runs.items():
        if not isinstance(entry, dict) or set(entry) != {
                "clean_home", "codex_home", "config_sha256", "execution_repo", "protected_paths"}:
            raise RunnerError(f"environment overlay fields differ for {run_id}")
        clean_home = Path(entry["clean_home"])
        codex_home = Path(entry["codex_home"])
        if (not clean_home.is_absolute() or not codex_home.is_absolute()
                or codex_home != clean_home / ".codex"):
            raise RunnerError(f"environment overlay home paths differ for {run_id}")
        if not re.fullmatch(r"[0-9a-f]{64}", entry["config_sha256"]):
            raise RunnerError(f"environment overlay config hash is invalid for {run_id}")
        execution_repo = Path(entry["execution_repo"])
        protected = entry["protected_paths"]
        if (not execution_repo.is_absolute() or not isinstance(protected, list)
                or not all(isinstance(item, str) and Path(item).is_absolute() for item in protected)):
            raise RunnerError(f"environment relocation fields are invalid for {run_id}")
        if any(execution_repo.resolve() == Path(item).resolve()
               or execution_repo.resolve().is_relative_to(Path(item).resolve()) for item in protected):
            raise RunnerError(f"protected path overlaps execution repository for {run_id}")
        resolved = clean_home.resolve()
        if resolved in homes:
            raise RunnerError("environment overlay must use a separate home for each run")
        homes.add(resolved)
    support = Path(overlay.get("support_root", ""))
    if (not support.is_absolute() or any(support.resolve().is_relative_to(home)
                                         or home.is_relative_to(support.resolve()) for home in homes)):
        raise RunnerError("environment overlay support_root must be absolute and outside clean homes")
    protected = overlay.get("protected_paths", [])
    if not isinstance(protected, list) or not all(isinstance(item, str) and Path(item).is_absolute()
                                                  for item in protected):
        raise RunnerError("environment overlay protected_paths must be absolute paths")
    return overlay, canonical_hash(overlay)


def validate_ledger(overlay: dict[str, Any], output: Path) -> dict[str, Any]:
    if output.resolve() != Path(overlay["output_root"]).resolve():
        raise RunnerError("environment continuation requires its bound output directory")
    ledger_path = output / "ledger.json"
    if not ledger_path.is_file():
        raise RunnerError("environment continuation requires the existing pilot ledger")
    raw = ledger_path.read_bytes()
    ledger = json.loads(raw)
    checkpoint = overlay["ledger_checkpoint"]
    count = checkpoint["launches_reserved"]
    reservations = ledger.get("reservations", [])
    if (ledger.get("launches_reserved", 0) < count or len(reservations) < count
            or reservations[count - 1].get("hash") != checkpoint["reservation_head"]
            or ledger.get("total_elapsed_seconds", 0) < checkpoint["total_elapsed_seconds"]
            or ledger.get("runs", {}).get("a01-baseline", {}).get("run_hash") != checkpoint["consumed_run_hash"]
            or any(run_id not in ledger.get("runs", {}) for run_id in checkpoint["consumed_run_ids"])):
        raise RunnerError("existing pilot ledger does not extend the bound checkpoint")
    if ledger["launches_reserved"] == count:
        import hashlib
        if hashlib.sha256(raw).hexdigest() != checkpoint["ledger_sha256"]:
            raise RunnerError("existing pilot ledger differs from the bound checkpoint")
    return ledger
