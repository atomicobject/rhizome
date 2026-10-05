#!/usr/bin/env python3
"""Prepare six clean, paired treatments without launching an evaluated model."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess

import adapter_support
import persistent_campaign
import prepare
import runner


def atomic_json(path: Path, value: object) -> None:
    runner.atomic_json(path, value)


def prepare_campaign(profile_path: Path, binary: Path, source_sha: str, root: Path,
                     auth_source: Path) -> tuple[Path, Path]:
    profile, profile_hash = persistent_campaign.load_profile(profile_path.resolve())
    binary = binary.resolve()
    root = root.resolve()
    auth_source = auth_source.resolve()
    if not root.is_absolute() or root.is_relative_to(Path("/private/tmp")) or root.is_relative_to(Path("/tmp")):
        raise ValueError("campaign root must be absolute and outside shared temporary directories")
    if root.exists():
        raise FileExistsError("campaign root already exists; preparation never overwrites or resumes")
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ValueError("candidate binary is missing or not executable")
    receipt = binary.parent / "build.json"
    if not receipt.is_file():
        raise ValueError("candidate binary requires adjacent build.json")
    receipt_value = json.loads(receipt.read_text())
    if receipt_value.get("source_sha") != source_sha or receipt_value.get("binary_sha256") != adapter_support.sha256(binary):
        raise ValueError("candidate build receipt does not match source revision and binary")
    if not auth_source.is_file() or auth_source.name != "auth.json":
        raise ValueError("--auth-source must name an existing Codex ChatGPT auth.json")

    originals = root / "prepared"
    executions = root / "execution"
    supports = root / "preparation-support"
    homes = root / "homes"
    output = root / "results"
    runtime_support = root / "runtime-support"
    root.mkdir(parents=True)
    runs = []
    cases = {item["case_id"]: item for item in profile["cases"]}
    for run_id, case_id, arm in persistent_campaign.RUN_ORDER:
        original = originals / run_id
        support = supports / run_id
        run = prepare.prepare(case_id, arm, original, binary, source_sha, support, output)
        case = cases[case_id]
        instruction = profile["method_instructions"][arm]
        run.update(
            id=run_id,
            pair_id=case["pair_id"],
            method=arm,
            task_prompt=case["task_prompt"],
            method_instruction=instruction,
            prompt=case["task_prompt"].rstrip() + "\n\nEvaluation method constraint: " + instruction.strip(),
        )
        atomic_json(support / "run.json", run)
        execution = executions / run_id
        shutil.copytree(original, execution, symlinks=True)
        if subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=all"],
                                   cwd=execution, text=True).strip():
            raise ValueError(f"execution fixture is dirty after copy: {run_id}")
        codex_home = homes / run_id / ".codex"
        codex_home.mkdir(parents=True)
        shutil.copy2(auth_source, codex_home / "auth.json")
        (codex_home / "auth.json").chmod(0o600)
        config = codex_home / "config.toml"
        config.write_text('forced_login_method = "chatgpt"\ncli_auth_credentials_store = "file"\n')
        config.chmod(0o600)
        runs.append(run)

    manifest = {
        "schema_version": 2,
        "kind": "persistent-code-mode-subscription-evaluation",
        "campaign_id": profile["campaign_id"],
        "profile_sha256": profile_hash,
        "model": profile["model"],
        "reasoning": profile["reasoning"],
        "max_launches": profile["max_launches"],
        "per_run_seconds": profile["per_run_seconds"],
        "total_seconds": profile["total_seconds"],
        "automatic_retries": 0,
        "api_fallback": False,
        "comparison_design": "forced-arm",
        "runs": runs,
    }
    persistent_campaign.validate_manifest(manifest)
    manifest_path = root / "manifest.json"
    atomic_json(manifest_path, manifest)
    manifest_hash = persistent_campaign.canonical_hash(manifest)

    output.mkdir()
    ledger = runner.load_ledger(output, manifest)
    if ledger["launches_reserved"] != 0 or ledger["runs"]:
        raise ValueError("fresh persistent campaign ledger is not empty")
    execution_paths = {run_id: executions / run_id for run_id, _, _ in persistent_campaign.RUN_ORDER}
    overlay_runs = {}
    for run_id, _, _ in persistent_campaign.RUN_ORDER:
        clean_home = homes / run_id
        config = clean_home / ".codex/config.toml"
        overlay_runs[run_id] = {
            "clean_home": str(clean_home),
            "codex_home": str(clean_home / ".codex"),
            "config_sha256": adapter_support.sha256(config),
            "execution_repo": str(execution_paths[run_id]),
            "protected_paths": [str(path) for other_id, path in execution_paths.items() if other_id != run_id],
        }
    overlay = {
        "schema_version": 1,
        "kind": "clean-codex-environment-persistent-campaign",
        "campaign_id": profile["campaign_id"],
        "campaign_hash": runner.campaign_hash(manifest),
        "manifest_sha256": manifest_hash,
        "expected_run_ids": [item[0] for item in persistent_campaign.RUN_ORDER],
        "runs": overlay_runs,
        "support_root": str(runtime_support),
        "output_root": str(output),
        "protected_paths": [str(originals), str(supports), str(profile_path.resolve()),
                            str(manifest_path), str(auth_source)],
    }
    environment_path = root / "environment.json"
    atomic_json(environment_path, overlay)
    return manifest_path, environment_path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--profile", type=Path,
        default=Path(__file__).resolve().parents[2] / "docs/reference/analysis/persistent-code-mode-evaluation.json",
    )
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--auth-source", type=Path, required=True)
    args = parser.parse_args()
    manifest, environment = prepare_campaign(
        args.profile, args.binary, args.source_sha, args.root, args.auth_source,
    )
    print(json.dumps({"manifest": str(manifest), "environment": str(environment),
                      "model_calls": 0, "launches_reserved": 0}, indent=2))


if __name__ == "__main__":
    main()
