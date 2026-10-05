#!/usr/bin/env python3
"""Run the deterministic progressive code-mode comparison; never launches a model."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import time
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "testdata" / "agent-experience" / "code-mode"
ENV = {**os.environ, "RZM_SKIP_REPO_DELEGATE": "1", "PYTHONDONTWRITEBYTECODE": "1", "NO_COLOR": "1"}


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_hashes(repo: Path) -> dict[str, str]:
    return {p.relative_to(repo).as_posix(): sha(p) for p in sorted(repo.rglob("*"))
            if p.is_file() and ".git" not in p.parts and ".rhizome" not in p.parts
            and "task-artifacts" not in p.parts and "__pycache__" not in p.parts and p.suffix != ".pyc"}


def invoke(argv: list[str], cwd: Path, timeout: int = 120) -> dict[str, Any]:
    started = time.monotonic_ns()
    proc = subprocess.run(argv, cwd=cwd, env=ENV, capture_output=True, timeout=timeout)
    return {"argv": argv, "elapsed_ns": time.monotonic_ns() - started, "exit_code": proc.returncode,
            "stdout_bytes": len(proc.stdout), "stderr_bytes": len(proc.stderr),
            "stdout": proc.stdout.decode(errors="replace"), "stderr": proc.stderr.decode(errors="replace")}


def checked(argv: list[str], cwd: Path) -> dict[str, Any]:
    result = invoke(argv, cwd)
    if result["exit_code"]:
        raise RuntimeError(f"command failed: {argv}: {result['stderr'][-500:]}")
    return result


def install(binary: Path, source: Path, repo: Path) -> tuple[list[dict[str, Any]], dict[str, str]]:
    shutil.copytree(source, repo, ignore=shutil.ignore_patterns("__pycache__", "*.pyc", ".git", ".rhizome"))
    before = source_hashes(repo)
    (repo / ".rhizome").mkdir()
    config = (
        "rhizome:\n  devBinaryDir: bin\nnoteEmbeddings:\n  enabled: false\n"
        "codeEmbeddings:\n  enabled: false\n"
    )
    config_path = repo / ".rhizome" / "config.yml"
    config_path.write_text(config)
    initialized = checked(
        [str(binary), "init", "--path", str(repo), "--workflow", "complex-domain", "--agents", "codex"], repo)
    config_path.write_text(config)
    indexed = checked([str(binary), "index", "--rebuild"], repo)
    config_path.write_text(config)
    return [initialized, indexed], before


def cli_task(binary: Path, repo: Path, task: str, case: dict[str, Any], batched: bool) -> list[list[str]]:
    prefix = [str(binary), "agent"]
    if task == "base-context":
        paths = case["expected_code_paths"]
        docs = case["expected_current_docs"] + case["expected_historical_docs"]
        if batched:
            return [prefix + ["file-context", *sum((["--file", path] for path in paths), []),
                              "--ensure-link-targets", "never"],
                    prefix + ["files", *sum((["--input", path] for path in docs), []),
                              "--include-content", "true", "--max-depth", "0", "--limit", str(len(docs))]]
        return ([prefix + ["file-context", "--file", path, "--ensure-link-targets", "never"] for path in paths]
                + [prefix + ["files", "--input", path, "--include-content", "true",
                             "--max-depth", "0", "--limit", "1"] for path in docs])
    if task == "coverage":
        return [prefix + ["query-recipe", "run", "--id", "coverage-gap-pack"]]
    return [prefix + ["files", "--input", case["expected_current_docs"][0],
                      "--include-content", "true", "--max-depth", "0", "--limit", "1"]]


def run_cli_task(binary: Path, repo: Path, task: str, case: dict[str, Any], batched: bool) -> list[dict[str, Any]]:
    repetitions = []
    for _ in range(2):
        started = time.monotonic_ns()
        calls = [invoke(argv, repo) for argv in cli_task(binary, repo, task, case, batched)]
        repetitions.append({"elapsed_ns": time.monotonic_ns() - started, "calls": calls,
                            "output_bytes": sum(call["stdout_bytes"] for call in calls)})
    return {"transport": "cli-mediated-batched" if batched else "cli-mediated-direct",
            "repetitions": repetitions}


NODE_SCRIPT = """import {readFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
const [manifestPath, operation, encoded] = process.argv.slice(2);
const manifest=JSON.parse(await readFile(manifestPath,'utf8'));
const {createClient}=await import(pathToFileURL(manifest.modulePath).href);
const client=createClient({executablePath:manifest.executablePath,vaultPath:manifest.vaultPath});
try { for(let i=0;i<2;i++){const start=process.hrtime.bigint();const result=await client[operation](JSON.parse(encoded));
console.log(JSON.stringify({elapsed_ns:Number(process.hrtime.bigint()-start),result}));}} finally {await client.close();}
"""


def typed_setup(binary: Path, repo: Path, artifacts: Path) -> tuple[dict[str, Any], Path, Path]:
    generated = artifacts / "generated"
    generation = checked([str(binary), "agent", "code", "generate", "--operation", "files",
                          "--operation", "file_context", "--output", str(generated)], repo)
    manifest = json.loads(generation["stdout"])
    runtime_manifest = artifacts / "runtime-manifest.json"
    runtime_manifest.write_text(json.dumps(manifest))
    script = artifacts / "run-client.mjs"
    script.parent.mkdir(parents=True, exist_ok=True)
    script.write_text(NODE_SCRIPT)
    return generation, script, runtime_manifest


def run_typed(binary: Path, repo: Path, task: str, case: dict[str, Any],
              script: Path, runtime_manifest: Path) -> dict[str, Any]:
    if task == "coverage":
        return {"transport": "hybrid-common-cli", "calls": run_cli_task(binary, repo, task, case, True)}
    def call(operation: str, inputs: dict[str, Any]) -> dict[str, Any]:
        outer = checked(["node", str(script), str(runtime_manifest), operation, json.dumps(inputs)], repo)
        inner = [json.loads(line) for line in outer["stdout"].splitlines() if line.strip()]
        if len(inner) != 2:
            raise RuntimeError("typed client did not return first and repeat measurements")
        return {"outer": outer, "inner_repetitions": inner}
    if task == "base-context":
        calls = []
        for operation, inputs in (
            ("fileContext", {"files": case["expected_code_paths"]}),
            ("files", {"inputs": case["expected_current_docs"] + case["expected_historical_docs"],
                       "includeContent": True, "limit": 4}),
        ):
            calls.append(call(operation, inputs))
        return {"transport": "typed-client", "calls": calls,
                "outer_node_elapsed_ns": sum(item["outer"]["elapsed_ns"] for item in calls)}
    else:
        operation = "files"
        inputs = {"inputs": [case["expected_current_docs"][0]], "includeContent": True, "limit": 1}
    result = call(operation, inputs)
    return {"transport": "typed-client", "outer_node_elapsed_ns": result["outer"]["elapsed_ns"],
            "outer_output_bytes": result["outer"]["stdout_bytes"], **result}


def observed_text(value: Any) -> str:
    found = []
    def visit(item: Any) -> None:
        if isinstance(item, str):
            found.append(item)
            try:
                visit(json.loads(item))
            except (json.JSONDecodeError, TypeError):
                for line in item.splitlines():
                    try:
                        visit(json.loads(line))
                    except (json.JSONDecodeError, TypeError):
                        pass
        elif isinstance(item, dict):
            for child in item.values():
                visit(child)
        elif isinstance(item, list):
            for child in item:
                visit(child)
    visit(value)
    return "\n".join(found)


def requirement_rows(value: Any) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    def visit(item: Any) -> None:
        if isinstance(item, str):
            try:
                visit(json.loads(item))
            except (json.JSONDecodeError, TypeError):
                pass
        elif isinstance(item, dict):
            if str(item.get("id", "")).startswith("REQ-"):
                rows.append(item)
            for child in item.values():
                visit(child)
        elif isinstance(item, list):
            for child in item:
                visit(child)
    visit(value)
    unique = {json.dumps(row, sort_keys=True): row for row in rows}
    return [unique[key] for key in sorted(unique)]


def checks(results: dict[str, Any], case: dict[str, Any], repos: dict[str, Path], before: dict[str, dict[str, str]]) -> dict[str, Any]:
    preservation = {arm: all(source_hashes(repos[arm]).get(path) == digest
                             for path, digest in before[arm].items()) for arm in repos}
    context = {}
    for arm in repos:
        text = observed_text(results[arm]["tasks"]["base-context"])
        context[arm] = [{"path": path, "sha256": sha(repos[arm] / path)}
                        for path in case["expected_current_docs"] + case["expected_historical_docs"]
                        if path in text and (repos[arm] / path).read_text() in text]
    coverage = {arm: observed_text(results[arm]["tasks"]["coverage"]) for arm in repos}
    raw_rows = {arm: requirement_rows(results[arm]["tasks"]["coverage"]) for arm in repos}
    rows = {arm: [row for row in values if row.get("status") == "accepted"]
            for arm, values in raw_rows.items()}
    required = case["requirements"]["accepted_untraced"]
    covered = case["requirements"]["accepted_covered"]
    candidate = case["requirements"]["candidate"]
    lookup_path = case["expected_current_docs"][0]
    lookup = {arm: observed_text(results[arm]["tasks"]["trivial-lookup"]) for arm in repos}
    lookup_items = {arm: {"path": lookup_path, "sha256": sha(repos[arm] / lookup_path)}
                    if lookup_path in text and (repos[arm] / lookup_path).read_text() in text else None
                    for arm, text in lookup.items()}
    def successful(value: Any) -> bool:
        if isinstance(value, dict):
            if "exit_code" in value and value["exit_code"] != 0:
                return False
            if "exitCode" in value and value["exitCode"] != 0 or value.get("ok") is False:
                return False
            return all(successful(child) for child in value.values())
        if isinstance(value, list):
            return all(successful(child) for child in value)
        return True
    result = {"source_preserved": preservation,
            "all_operations_succeeded": {arm: successful(results[arm]["tasks"]) for arm in repos},
            "context_complete": {arm: len(items) == 4 for arm, items in context.items()},
            "equivalent_context": len({json.dumps(v) for v in context.values()}) == 1,
            "context": context, "coverage_has_untraced": {arm: required in text for arm, text in coverage.items()},
            "coverage_rows_nonempty": {arm: bool(value) for arm, value in rows.items()},
            "coverage_classification": {arm: {
                "accepted_ids_exact": {row.get("id") for row in value} == {required, covered},
                "untraced": any(row.get("id") == required and not row.get("specs")
                                and not row.get("storyRefs") and not row.get("acceptanceCriterionRefs") for row in value),
                "covered": any(row.get("id") == covered and row.get("specs")
                               and row.get("storyRefs") and row.get("acceptanceCriterionRefs") for row in value),
                "candidate_preserved_raw": any(row.get("id") == candidate and row.get("status") == "candidate"
                                               for row in raw_rows[arm]),
            } for arm, value in rows.items()},
            "equivalent_coverage_rows": len({json.dumps(value, sort_keys=True) for value in rows.values()}) == 1,
            "accepted_coverage_rows": rows, "raw_coverage_rows": raw_rows,
            "trivial_lookup": lookup_items,
            "equivalent_trivial_lookup": None not in lookup_items.values()
            and len({json.dumps(value, sort_keys=True) for value in lookup_items.values()}) == 1}
    result["passed"] = (all(preservation.values()) and all(result["all_operations_succeeded"].values())
                        and all(result["context_complete"].values()) and result["equivalent_context"]
                        and all(result["coverage_rows_nonempty"].values())
                        and all(all(contract.values()) for contract in result["coverage_classification"].values())
                        and result["equivalent_coverage_rows"] and result["equivalent_trivial_lookup"])
    return result


def load_manifest(path: Path) -> dict[str, Any]:
    manifest = json.loads(path.read_text())
    if (manifest.get("schema_version") != 1 or manifest.get("kind") != "deterministic-code-mode-comparison"
            or manifest.get("tasks") != ["base-context", "coverage", "trivial-lookup"]
            or manifest.get("arms") != ["direct", "batched", "typed"]
            or manifest.get("repetitions") != ["first", "repeat"]
            or manifest.get("model_launches_authorized") != 0):
        raise ValueError("manifest does not match the bounded deterministic comparison")
    binary = Path(manifest["binary"])
    if not binary.is_absolute() or sha(binary) != manifest.get("binary_sha256"):
        raise ValueError("manifest binary identity does not match")
    if (not isinstance(manifest.get("source_sha"), str) or len(manifest["source_sha"]) != 40
            or any(character not in "0123456789abcdef" for character in manifest["source_sha"])):
        raise ValueError("manifest source_sha must be a full revision")
    receipt = Path(manifest["build_receipt"])
    if sha(receipt) != manifest.get("build_receipt_sha256"):
        raise ValueError("build receipt identity does not match")
    built = json.loads(receipt.read_text())
    if built.get("source_sha") != manifest["source_sha"] or built.get("binary_sha256") != manifest["binary_sha256"]:
        raise ValueError("build receipt does not bind source and binary")
    if manifest.get("fixture_registry") != "testdata/agent-experience/code-mode/registry.json":
        raise ValueError("manifest fixture registry is outside the bounded comparison")
    registry = ROOT / manifest["fixture_registry"]
    if not registry.is_file() or sha(registry) != manifest.get("fixture_registry_sha256"):
        raise ValueError("fixture registry identity does not match")
    manifest["resolved_fixture_registry"] = str(registry)
    return manifest


def run(manifest: dict[str, Any], output: Path) -> dict[str, Any]:
    binary = Path(manifest["binary"])
    registry_path = Path(manifest["resolved_fixture_registry"])
    registry = json.loads(registry_path.read_text())
    case = next(case for case in registry["cases"] if case["case_id"] == "E01")
    output.mkdir(parents=True, exist_ok=False)
    shutil.copyfile(manifest["build_receipt"], output / "build-receipt.json")
    results, repos, before = {}, {}, {}
    arm_order = ["direct", "batched", "typed"]
    for arm in arm_order:
        repo = output / "scratch" / arm
        setup, initial = install(binary, registry_path.parent / case["relative_path"], repo)
        repos[arm], before[arm] = repo, initial
        generation = script = runtime_manifest = None
        if arm == "typed":
            generation, script, runtime_manifest = typed_setup(
                binary, repo, output / "task-artifacts" / arm)
        tasks = {}
        for task in ("base-context", "coverage", "trivial-lookup"):
            tasks[task] = run_typed(binary, repo, task, case, script, runtime_manifest) \
                if arm == "typed" else run_cli_task(binary, repo, task, case, arm == "batched")
        results[arm] = {"setup": setup, "generation": generation, "tasks": tasks}
    invalid = next(case for case in registry["cases"] if case["case_id"] == "E02")
    invalid_repo = output / "scratch" / "invalid"
    invalid_setup, _ = install(binary, registry_path.parent / invalid["relative_path"], invalid_repo)
    invalid_validation = invoke([str(binary), "validate"], invalid_repo)
    invalid_text = invalid_validation["stdout"] + invalid_validation["stderr"]
    invalid_checks = {
        "nonzero": invalid_validation["exit_code"] != 0,
        "invalid_source_kind": all(value in invalid_text for value in (
            "field_type_mismatch", "docs/reference/requirements/sources/missing-kind.md")),
        "unresolvable_trace": all(value in invalid_text for value in (
            "link_target_missing", "docs/reference/requirements/requirements/unresolvable-trace.md")),
    }
    source_inventory = {}
    for arm, repo in repos.items():
        current = source_hashes(repo)
        source_inventory[arm] = {"before": before[arm],
                                 "after": {path: current.get(path) for path in before[arm]},
                                 "changed_or_missing": sorted(path for path, digest in before[arm].items()
                                                              if current.get(path) != digest)}
    evidence = {"schema_version": 1, "manifest": manifest, "binary": str(binary), "binary_sha256": sha(binary),
                "arm_execution_order": arm_order,
                "registry_sha256": sha(registry_path), "results": results,
                "checks": checks(results, case, repos, before),
                "source_inventory": source_inventory,
                "invalid_fixture": {"setup": invalid_setup, "validation": invalid_validation,
                                    "checks": invalid_checks}}
    evidence["checks"]["invalid_fixture"] = invalid_checks
    evidence["checks"]["passed"] = evidence["checks"]["passed"] and all(invalid_checks.values())
    (output / "evidence.json").write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n")
    return evidence


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("code-mode-manifest.json"))
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    result = run(load_manifest(args.manifest.resolve()), args.output.resolve())
    print(json.dumps(result["checks"], indent=2))
    if not result["checks"]["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
