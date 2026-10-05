"""Frozen pilot manifest validation."""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path
from typing import Any

MODEL = "gpt-5.6-luna"
REASONING = "xhigh"
MAX_LAUNCHES = 4
MAX_PER_RUN_SECONDS = 600
MAX_TOTAL_SECONDS = 2400
PILOT_ORDER = (
    ("a01-baseline", "A01", "baseline"),
    ("a02-baseline", "A02", "baseline"),
    ("a01-candidate", "A01", "candidate"),
    ("a02-candidate", "A02", "candidate"),
)
PILOT_PROMPTS = {
    "A01": "Fix the expiration boundary bug.",
    "A02": "Change ‘recieve’ to ‘receive’ in README.md; change nothing else.",
}


class RunnerError(RuntimeError):
    pass


def canonical_hash(value: Any) -> str:
    encoded = json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    return hashlib.sha256(encoded).hexdigest()


def load_manifest(path: Path) -> tuple[dict[str, Any], str]:
    manifest = json.loads(path.read_text())
    validate_manifest(manifest)
    return manifest, canonical_hash(manifest)


def validate_manifest(manifest: dict[str, Any]) -> None:
    if manifest.get("schema_version") != 1:
        raise RunnerError("manifest schema_version must be 1")
    if manifest.get("model") != MODEL or manifest.get("reasoning") != REASONING:
        raise RunnerError(f"manifest must use {MODEL} with {REASONING} reasoning")
    campaign_id = manifest.get("campaign_id")
    if not isinstance(campaign_id, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", campaign_id):
        raise RunnerError("campaign_id must be a safe path segment")
    for key, ceiling in {
        "max_launches": MAX_LAUNCHES,
        "per_run_seconds": MAX_PER_RUN_SECONDS,
        "total_seconds": MAX_TOTAL_SECONDS,
    }.items():
        value = manifest.get(key)
        if not isinstance(value, int) or isinstance(value, bool) or value < 1 or value > ceiling:
            raise RunnerError(f"{key} must be an integer from 1 through {ceiling}")
    runs = manifest.get("runs")
    if not isinstance(runs, list) or len(runs) not in {2, 4} or len(runs) > manifest["max_launches"]:
        raise RunnerError("pilot manifest must contain two baseline runs or all four pilot runs")
    required = {
        "id", "case_id", "arm", "repo", "binary", "binary_sha256", "source_sha", "prompt",
        "fixture", "fixture_commit", "fixture_tree", "guidance", "support", "setup_sha256", "isolation",
        "build_receipt", "build_receipt_sha256",
    }
    ids = []
    for run in runs:
        if not isinstance(run, dict) or not required.issubset(run):
            raise RunnerError("each run is missing required identity, fixture, or provenance fields")
        string_fields = required - {"fixture", "guidance", "isolation"}
        if not all(isinstance(run[key], str) and run[key] for key in string_fields):
            raise RunnerError("all required scalar run fields must be non-empty strings")
        if not all(isinstance(run[key], dict) and run[key] for key in ("fixture", "guidance", "isolation")):
            raise RunnerError("fixture, guidance, and isolation must be non-empty objects")
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", run["id"]):
            raise RunnerError("run ids must be safe path segments")
        for field in ("source_sha", "fixture_commit", "fixture_tree"):
            if not re.fullmatch(r"[0-9a-f]{40}", run[field]):
                raise RunnerError(f"{field} must be a full lowercase hexadecimal revision")
        for field in ("binary_sha256", "setup_sha256", "build_receipt_sha256"):
            if not re.fullmatch(r"[0-9a-f]{64}", run[field]):
                raise RunnerError(f"{field} must be lowercase hexadecimal SHA-256")
        if not all(Path(run[field]).is_absolute() for field in ("repo", "binary", "support", "build_receipt")):
            raise RunnerError("repo, binary, support, and build_receipt must be absolute paths")
        if run["prompt"] != PILOT_PROMPTS.get(run["case_id"]):
            raise RunnerError(f"prompt differs from the frozen {run['case_id']} pilot prompt")
        ids.append(run["id"])
    if len(ids) != len(set(ids)):
        raise RunnerError("run ids must be unique")
    tuples = [(run["id"], run["case_id"], run["arm"]) for run in runs]
    if tuples != list(PILOT_ORDER[:len(runs)]):
        raise RunnerError("runs must be the ordered A01/A02 baseline stage, optionally followed by candidates")


def selected_runs(manifest: dict[str, Any], ids: list[str] | None) -> list[dict[str, Any]]:
    if not ids:
        return list(manifest["runs"])
    index = {run["id"]: run for run in manifest["runs"]}
    missing = [run_id for run_id in ids if run_id not in index]
    if missing:
        raise RunnerError("unknown run id(s): " + ", ".join(missing))
    if len(ids) != len(set(ids)):
        raise RunnerError("selected run ids must be unique")
    return [index[run_id] for run_id in ids]
