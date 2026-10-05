"""Clean-home bindings for the fresh six-run persistent code-mode campaign."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from persistent_campaign import RUN_ORDER, RunnerError, canonical_hash


EXPECTED_RUN_IDS = tuple(item[0] for item in RUN_ORDER)


def load(path: Path, manifest: dict[str, Any], manifest_hash: str) -> tuple[dict[str, Any], str]:
    overlay = json.loads(path.read_text())
    if (overlay.get("schema_version") != 1
            or overlay.get("kind") != "clean-codex-environment-persistent-campaign"
            or overlay.get("manifest_sha256") != manifest_hash
            or overlay.get("expected_run_ids") != list(EXPECTED_RUN_IDS)):
        raise RunnerError("environment does not match the fresh persistent campaign")
    output = Path(overlay.get("output_root", ""))
    support = Path(overlay.get("support_root", ""))
    if not output.is_absolute() or not support.is_absolute() or output == support:
        raise RunnerError("output_root and support_root must be distinct absolute paths")
    runs = overlay.get("runs")
    if not isinstance(runs, dict) or list(runs) != list(EXPECTED_RUN_IDS):
        raise RunnerError("environment must define all six clean homes in launch order")
    homes: set[Path] = set()
    execution_repos: set[Path] = set()
    for run_id, entry in runs.items():
        if not isinstance(entry, dict) or set(entry) != {
                "clean_home", "codex_home", "config_sha256", "execution_repo", "protected_paths"}:
            raise RunnerError(f"environment fields differ for {run_id}")
        clean_home = Path(entry["clean_home"])
        codex_home = Path(entry["codex_home"])
        execution_repo = Path(entry["execution_repo"])
        protected = entry["protected_paths"]
        if (not clean_home.is_absolute() or codex_home != clean_home / ".codex"
                or not execution_repo.is_absolute()):
            raise RunnerError(f"environment paths differ for {run_id}")
        if not re.fullmatch(r"[0-9a-f]{64}", entry["config_sha256"]):
            raise RunnerError(f"config hash is invalid for {run_id}")
        if not isinstance(protected, list) or not all(isinstance(item, str) and Path(item).is_absolute() for item in protected):
            raise RunnerError(f"protected paths are invalid for {run_id}")
        resolved_home = clean_home.resolve()
        resolved_repo = execution_repo.resolve()
        if resolved_home in homes or resolved_repo in execution_repos:
            raise RunnerError("every run requires a distinct clean home and execution repository")
        if any(resolved_repo == Path(item).resolve() or resolved_repo.is_relative_to(Path(item).resolve())
               for item in protected):
            raise RunnerError(f"protected path overlaps execution repository for {run_id}")
        homes.add(resolved_home)
        execution_repos.add(resolved_repo)
    protected = overlay.get("protected_paths", [])
    if not isinstance(protected, list) or not all(isinstance(item, str) and Path(item).is_absolute() for item in protected):
        raise RunnerError("campaign protected_paths must be absolute")
    return overlay, canonical_hash(overlay)


def validate_ledger(overlay: dict[str, Any], output: Path) -> dict[str, Any]:
    if output.resolve() != Path(overlay["output_root"]).resolve():
        raise RunnerError("persistent campaign requires its bound output directory")
    ledger_path = output / "ledger.json"
    if not ledger_path.is_file():
        raise RunnerError("prepare the fresh persistent campaign ledger before preflight")
    ledger = json.loads(ledger_path.read_text())
    if (ledger.get("campaign_id") != overlay.get("campaign_id")
            or ledger.get("campaign_hash") != overlay.get("campaign_hash")):
        raise RunnerError("ledger belongs to another campaign")
    if ledger.get("launches_reserved", 0) > 6:
        raise RunnerError("persistent campaign ledger exceeds six reservations")
    return ledger
