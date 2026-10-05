"""Frozen six-launch persistent code-mode subscription evaluation profile."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from campaign import RunnerError, canonical_hash


MODEL = "gpt-5.6-luna"
REASONING = "xhigh"
CAMPAIGN_ID = "persistent-code-mode-subscription-evaluation-2026-09-07"
MAX_LAUNCHES = 6
MAX_PER_RUN_SECONDS = 600
MAX_TOTAL_SECONDS = 3600
APPROVED_PROFILE_SHA256 = "b4e098121dd9c4a2db81f5620b0da673a56b34082fd4743970946fe50fba6a13"
CASES = {
    "A01": ("P01", "Fix the expiration boundary bug. Inspect the governing repository context, implement the repair, and run the focused checks."),
    "A03": ("P02", "Resume the approved retry-limit effort from its durable worktree state. Complete the implementation and handoff, updating the owning durable effort record with actual verification evidence."),
    "A05": ("P03", "Assess the revised retention source and record its implications for the current feature. Preserve source provenance, distinguish the unresolved conflict, and update the durable coverage record without promoting candidate evidence."),
}
METHOD_INSTRUCTIONS = {
    "direct": "Use direct rzm commands for Rhizome operations, batching independent CLI calls in one shell invocation when useful. Do not use rzm agent code.",
    "persistent": "Use one generated JavaScript client connected to one persistent rzm agent code serve process for Rhizome operations. Direct rzm commands are allowed only for code-mode discovery, generation, lifecycle setup, or verification without a generated operation.",
}
RUN_ORDER = (
    ("p01-direct", "A01", "direct"),
    ("p01-persistent", "A01", "persistent"),
    ("p02-direct", "A03", "direct"),
    ("p02-persistent", "A03", "persistent"),
    ("p03-direct", "A05", "direct"),
    ("p03-persistent", "A05", "persistent"),
)


def load_profile(path: Path) -> tuple[dict[str, Any], str]:
    profile = json.loads(path.read_text())
    validate_profile(profile)
    digest = canonical_hash(profile)
    if digest != APPROVED_PROFILE_SHA256:
        raise RunnerError("persistent evaluation profile bytes differ from the approved profile")
    return profile, digest


def validate_profile(profile: dict[str, Any]) -> None:
    if profile.get("schema_version") != 1 or profile.get("kind") != "persistent-code-mode-evaluation-profile":
        raise RunnerError("unsupported persistent evaluation profile")
    if profile.get("campaign_id") != CAMPAIGN_ID:
        raise RunnerError("persistent evaluation campaign_id differs from the approved campaign")
    if profile.get("model") != MODEL or profile.get("reasoning") != REASONING:
        raise RunnerError("persistent evaluation must use Luna xhigh")
    if profile.get("max_launches") != MAX_LAUNCHES:
        raise RunnerError("persistent evaluation must reserve exactly six launches")
    if profile.get("per_run_seconds") != MAX_PER_RUN_SECONDS or profile.get("total_seconds") != MAX_TOTAL_SECONDS:
        raise RunnerError("persistent evaluation time bounds differ from the approved profile")
    if (profile.get("comparison_design") != "forced-arm"
            or profile.get("natural_adoption_claim_authorized") is not False
            or profile.get("automatic_retries") != 0
            or profile.get("api_fallback") is not False
            or profile.get("historical_campaign_reuse") is not False):
        raise RunnerError("persistent evaluation method or authority limits changed")
    cases = profile.get("cases")
    if not isinstance(cases, list) or [(item.get("case_id"), item.get("pair_id")) for item in cases] != [
        ("A01", "P01"), ("A03", "P02"), ("A05", "P03")
    ]:
        raise RunnerError("persistent evaluation cases differ from the approved three pairs")
    methods = profile.get("method_instructions")
    if methods != METHOD_INSTRUCTIONS:
        raise RunnerError("both forced-arm method instructions are required")
    if [(item["case_id"], item["pair_id"], item["task_prompt"]) for item in cases] != [
        (case_id, *CASES[case_id]) for case_id in ("A01", "A03", "A05")
    ]:
        raise RunnerError("persistent task prompts differ from the approved profile")


def load_manifest(path: Path) -> tuple[dict[str, Any], str]:
    manifest = json.loads(path.read_text())
    validate_manifest(manifest)
    return manifest, canonical_hash(manifest)


def validate_manifest(manifest: dict[str, Any]) -> None:
    if manifest.get("schema_version") != 2 or manifest.get("kind") != "persistent-code-mode-subscription-evaluation":
        raise RunnerError("unsupported persistent campaign manifest")
    if manifest.get("campaign_id") != CAMPAIGN_ID:
        raise RunnerError("manifest is not the approved persistent campaign")
    if manifest.get("model") != MODEL or manifest.get("reasoning") != REASONING:
        raise RunnerError("manifest must use Luna xhigh")
    if manifest.get("max_launches") != MAX_LAUNCHES:
        raise RunnerError("manifest launch limit must equal six")
    if (manifest.get("profile_sha256") != APPROVED_PROFILE_SHA256
            or manifest.get("comparison_design") != "forced-arm"
            or manifest.get("automatic_retries") != 0
            or manifest.get("api_fallback") is not False):
        raise RunnerError("manifest method or authority limits differ from the frozen profile")
    if manifest.get("per_run_seconds") != MAX_PER_RUN_SECONDS or manifest.get("total_seconds") != MAX_TOTAL_SECONDS:
        raise RunnerError("manifest time bounds differ from the frozen profile")
    runs = manifest.get("runs")
    if not isinstance(runs, list) or len(runs) != MAX_LAUNCHES:
        raise RunnerError("manifest must contain exactly six runs")
    required = {
        "id", "case_id", "pair_id", "arm", "method", "repo", "binary", "binary_sha256",
        "source_sha", "task_prompt", "method_instruction", "prompt", "fixture", "fixture_commit",
        "fixture_tree", "guidance", "support", "setup_sha256", "isolation", "build_receipt",
        "build_receipt_sha256",
    }
    tuples = []
    for run in runs:
        if not isinstance(run, dict) or not required.issubset(run):
            raise RunnerError("persistent run is missing required identity or provenance fields")
        if not all(isinstance(run[key], str) and run[key] for key in required - {"fixture", "guidance", "isolation"}):
            raise RunnerError("persistent scalar run fields must be non-empty strings")
        if not all(isinstance(run[key], dict) and run[key] for key in ("fixture", "guidance", "isolation")):
            raise RunnerError("fixture, guidance and isolation must be non-empty objects")
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", run["id"]):
            raise RunnerError("run ids must be safe path segments")
        for field in ("source_sha", "fixture_commit", "fixture_tree"):
            if not re.fullmatch(r"[0-9a-f]{40}", run[field]):
                raise RunnerError(f"{field} must be a full lowercase revision")
        for field in ("binary_sha256", "setup_sha256", "build_receipt_sha256"):
            if not re.fullmatch(r"[0-9a-f]{64}", run[field]):
                raise RunnerError(f"{field} must be lowercase SHA-256")
        if not all(Path(run[field]).is_absolute() for field in ("repo", "binary", "support", "build_receipt")):
            raise RunnerError("repo, binary, support and build_receipt must be absolute")
        expected_prompt = run["task_prompt"].rstrip() + "\n\nEvaluation method constraint: " + run["method_instruction"].strip()
        if run["prompt"] != expected_prompt:
            raise RunnerError("run prompt does not preserve the task and explicit method constraint")
        if run["method"] != run["arm"] or run["arm"] not in {"direct", "persistent"}:
            raise RunnerError("run method and arm must identify direct or persistent")
        if (run["pair_id"], run["task_prompt"]) != CASES[run["case_id"]] or run["method_instruction"] != METHOD_INSTRUCTIONS[run["arm"]]:
            raise RunnerError("run task or method instruction differs from the frozen profile")
        tuples.append((run["id"], run["case_id"], run["arm"]))
    if tuples != list(RUN_ORDER):
        raise RunnerError("runs must follow the frozen paired order")
    if len({run["repo"] for run in runs}) != MAX_LAUNCHES or len({run["support"] for run in runs}) != MAX_LAUNCHES:
        raise RunnerError("every launch requires a distinct fixture and support directory")
    if len({(run["binary"], run["binary_sha256"], run["source_sha"]) for run in runs}) != 1:
        raise RunnerError("all six runs must use the same candidate binary and source revision")
    for case_id in ("A01", "A03", "A05"):
        pair = [run for run in runs if run["case_id"] == case_id]
        if len(pair) != 2 or pair[0]["fixture_tree"] != pair[1]["fixture_tree"]:
            raise RunnerError(f"{case_id} arms must use byte-equivalent Git trees")
        if pair[0]["fixture"] != pair[1]["fixture"] or pair[0]["guidance"] != pair[1]["guidance"]:
            raise RunnerError(f"{case_id} authored sources and installed guidance must match across arms")


def selected_runs(manifest: dict[str, Any], ids: list[str] | None) -> list[dict[str, Any]]:
    if not ids:
        return list(manifest["runs"])
    if len(ids) != 1:
        raise RunnerError("select exactly one persistent run id per invocation")
    index = {run["id"]: run for run in manifest["runs"]}
    try:
        return [index[run_id] for run_id in ids]
    except KeyError as error:
        raise RunnerError(f"unknown persistent run id: {error.args[0]}") from error
