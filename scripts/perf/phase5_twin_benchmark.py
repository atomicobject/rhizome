#!/usr/bin/env python3
"""Run isolated Phase 5 human/agent one-shot parity evidence.

Each of the 20 samples starts from its own clone of an indexed source template.
Sample 1 is the first invocation; samples 2 through 20 are repeated
fresh-process invocations. This records a first-process effect without calling
it OS cold, and makes filesystem mutations observable.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import tempfile
import time
import re
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from benchmark_evidence import index_fingerprints, operation_envelope, sha256_file, sha256_text
from phase5_twin_support import (
    APPROVED_VOLATILE_SQLITE_COLUMNS, canonical_sqlite_snapshot, copy_state,
    fingerprint_content_identity, fingerprint_delta,
    filesystem_mutation, mutation, normalize_text, normalized, sqlite_snapshot_diff_columns,
    summarize, tree_manifest,
)
import phase5_twin_support


WORKTREE_PROJECTION_EXCLUDED_ROOTS = phase5_twin_support.WORKTREE_PROJECTION_EXCLUDED_ROOTS


def create_rhizome_projection(source: Path, destination: Path) -> dict[str, Any]:
    return phase5_twin_support.create_rhizome_projection(
        source, destination, WORKTREE_PROJECTION_EXCLUDED_ROOTS,
    )


@dataclass(frozen=True)
class Cell:
    name: str
    command: tuple[str, ...]
    json_output: bool
    uses_vault_flag: bool = True
    expected_exit_codes: tuple[int, ...] = (0,)


def phase5_cells(source_kind: str = "fixture") -> tuple[Cell, ...]:
    if source_kind == "fixture":
        recipe_id, view_id = "fixture-action-items", "action-items"
    elif source_kind == "rhizome":
        recipe_id, view_id = "all-action-items", "action-items.open"
    else:
        raise ValueError(f"unknown Phase 5 source kind: {source_kind}")
    query = 'query { note(path: "CONTEXT.md") { path title } }'
    return (
        Cell("rootSearch", ("search", "runtime capability", "--fast", "--limit", "5"), False, False),
        Cell("ontologyQueryRoot", ("ontology", "query", "--query", query), True),
        Cell("ontologyQueryAgent", ("agent", "ontology-query", "--query", query), True),
        Cell("queryRecipeRoot", ("query-recipe", "run", "--id", recipe_id), True),
        Cell("queryRecipeAgent", ("agent", "query-recipe", "run", "--id", recipe_id), True),
        Cell("viewRoot", ("view", "run", "--id", view_id), True),
        Cell("viewAgent", ("agent", "view", "run", "--id", view_id), True),
        Cell("semanticSession", ("agent", "semantic-query", "--path", "notes", "--query", "runtime capability", "--limit", "5", "--session-id", "phase5-semantic-session", "--timings"), True),
        Cell("graphFileContextNever", ("graph", "file-context", "src/phase5_apply.py", "--profile", "code", "--ensure-link-targets", "never"), False),
        Cell("graphVaultContext", ("graph", "vault-context", "--profile", "vault"), False),
    )


def restore_state(template: Path, state: Path) -> None:
    """Restore a pristine template at the same absolute sample path."""
    if state.exists():
        # Preserve the just-run state until TemporaryDirectory cleanup. The
        # macOS trash command cannot move sandbox temporary paths reliably.
        state.rename(state.with_name(f"{state.name}.prior-{uuid.uuid4().hex}"))
    copy_state(template, state)


def source_kind(source: Path) -> str:
    recipe = source / ".rhizome" / "query-recipes" / "action-items.yaml"
    if recipe.is_file() and "id: fixture-action-items" in recipe.read_text():
        return "fixture"
    return "rhizome"


def apply_deterministic_provider_overlay(source: Path, kind: str) -> bool:
    """Use the fixture's offline embedding provider in a disposable worktree copy."""
    if kind != "rhizome":
        return False
    config = source / ".rhizome" / "config.yml"
    content = config.read_text()
    updated = content.replace("provider: voyage", "provider: test")
    updated = re.sub(r"(?m)^  model: .*\n", "  dimensions: 256\n", updated)
    updated = re.sub(r"(?m)^  endpoint: .*\n", "", updated)
    if updated == content or updated.count("provider: test") != 2 or "provider: voyage" in updated:
        raise RuntimeError("could not apply deterministic worktree embedding overlay")
    config.write_text(updated)
    return True


def prepare_graph_apply_fixture(source: Path, kind: str) -> dict[str, str]:
    """Add a disposable embedded-node target that `apply` must repair."""
    code_path = source / "src" / "phase5_apply.py"
    note_path = source / "docs" / "phase5-spec.md"
    schema_path = source / ".rhizome/ontology/schema.graphql"
    code_path.parent.mkdir(parents=True, exist_ok=True)
    note_path.parent.mkdir(parents=True, exist_ok=True)
    code_path.write_text("# WHY: implements [[phase5-spec#Story A]]\ndef phase5_apply():\n    return None\n")
    note_path.write_text("# Phase 5 Spec\n\n## Stories\n\n### Story A\nsummary:: Repair exact node context\n")
    with schema_path.open("a") as schema:
        schema.write("""

type Phase5ApplySpec @node(paths: [\"docs/phase5-spec.md\"]) {
  stories: Phase5ApplyStories @contains(level: H2, heading: \"Stories\")
}

type Phase5ApplyStories implements Section {
  stories: [Phase5ApplyStory!] @contains(level: H3)
}

type Phase5ApplyStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
""")
    return {
        "codePath": code_path.relative_to(source).as_posix(),
        "notePath": note_path.relative_to(source).as_posix(),
        "mintedLocatorPrefix": "^phase5applystory-story-a-",
    }


def run_command(
    binary: Path, cell: Cell, state: Path, *, timeout_seconds: int,
    before_manifest: dict[str, str], before_index_fingerprints: dict[str, dict[str, Any]],
    normalization_roots: tuple[Path, ...], volatile_sqlite_columns: set[tuple[str, str]],
    capture_normalized_output: bool = False,
) -> dict[str, Any]:
    command = [str(binary), *cell.command]
    if cell.uses_vault_flag:
        command.extend(("--vault", str(state)))
    env = os.environ.copy()
    env["RZM_SKIP_REPO_DELEGATE"] = "1"
    started = time.perf_counter()
    try:
        result = subprocess.run(
            command, cwd=state, text=True, capture_output=True, check=False,
            env=env, timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError(f"{cell.name} exceeded {timeout_seconds}s") from exc
    elapsed_ms = (time.perf_counter() - started) * 1000
    if result.returncode not in cell.expected_exit_codes:
        raise RuntimeError(
            f"{cell.name} returned {result.returncode}, expected {cell.expected_exit_codes}: {result.stderr.strip()}"
        )
    try:
        stdout = normalize_text(result.stdout, normalization_roots, parse_json=cell.json_output)
        stderr = normalize_text(result.stderr, normalization_roots, parse_json=False)
        payload = json.loads(result.stdout) if cell.json_output and result.stdout.strip() else None
    except (json.JSONDecodeError, TypeError) as exc:
        raise RuntimeError(f"{cell.name} emitted invalid JSON: {exc}") from exc
    after = tree_manifest(state)
    database = state / ".rhizome" / "db.sqlite"
    canonical = canonical_sqlite_snapshot(database, volatile_sqlite_columns) if database.is_file() else None
    fingerprints_after = index_fingerprints(state)
    sqlite_delta = fingerprint_delta(before_index_fingerprints, fingerprints_after)
    record = {
        "command": command,
        "elapsedMs": round(elapsed_ms, 3),
        "exitCode": result.returncode,
        "normalizedStdoutSha256": sha256_text(stdout),
        "normalizedStderrSha256": sha256_text(stderr),
        "fullStdoutSha256": sha256_text(result.stdout),
        "fullStderrSha256": sha256_text(result.stderr),
        "stdoutBytes": len(result.stdout.encode()),
        "stderrBytes": len(result.stderr.encode()),
        "operationEnvelope": operation_envelope(payload),
        "operationDiagnosticsAvailable": bool(isinstance(payload, dict) and isinstance(payload.get("diagnostics"), dict)),
        "indexFingerprintsBefore": before_index_fingerprints,
        "indexFingerprintsAfter": fingerprints_after,
        "sqliteSidecarDelta": sqlite_delta,
        "canonicalLogicalSha256": sha256_text(json.dumps(canonical, sort_keys=True, default=str)) if canonical is not None else None,
        "filesystemMutation": filesystem_mutation(before_manifest, after, sqlite_delta),
    }
    if capture_normalized_output:
        record["normalizedStdout"] = stdout
        record["normalizedStderr"] = stderr
    return record


def graph_apply_contract_evidence(
    binaries: dict[str, Path], templates: dict[str, Path], template_manifests: dict[str, dict[str, str]],
    template_indexes: dict[str, dict[str, dict[str, Any]]], graph_apply_target: dict[str, str],
    work_root: Path, *, timeout_seconds: int, volatile_sqlite_columns: set[tuple[str, str]],
) -> dict[str, Any]:
    """Prove the approved root plan/apply repair separately from parity cells."""
    records: dict[str, dict[str, Any]] = {}
    note_path = graph_apply_target["notePath"]
    minted = graph_apply_target["mintedLocatorPrefix"]
    cases = (
        ("baselinePlan", "baseline", "plan"),
        ("baselineApply", "baseline", "apply"),
        ("candidatePlan", "candidate", "plan"),
        ("candidateApply", "candidate", "apply"),
    )
    for case_name, binary_name, ensure in cases:
        state = work_root / "graph-apply-contract" / "same-path"
        restore_state(templates[binary_name], state)
        cell = Cell(case_name, ("graph", "file-context", graph_apply_target["codePath"], "--profile", "code", "--ensure-link-targets", ensure), False)
        result = run_command(
            binaries[binary_name], cell, state, timeout_seconds=timeout_seconds,
            before_manifest=template_manifests[binary_name], before_index_fingerprints=template_indexes[binary_name],
            normalization_roots=(state, templates[binary_name]), volatile_sqlite_columns=volatile_sqlite_columns,
            capture_normalized_output=True,
        )
        source_changes = {
            action: [path for path in result["filesystemMutation"][action] if not path.startswith(".rhizome/db.sqlite")]
            for action in ("created", "deleted", "changed")
        }
        note_changed = note_path in source_changes["changed"]
        output = result.pop("normalizedStdout")
        result.pop("normalizedStderr")
        if case_name in {"baselinePlan", "baselineApply"}:
            if note_changed or any(source_changes.values()) or 'kind="ontology-node"' in output:
                raise RuntimeError(f"{case_name} no longer proves the frozen baseline no-op")
        elif case_name == "candidatePlan":
            if note_changed or any(source_changes.values()) or 'kind="ontology-node"' not in output:
                raise RuntimeError("candidate plan did not expose exactly a no-write ontology-node repair")
        else:
            if source_changes != {"created": [], "deleted": [], "changed": [note_path]}:
                raise RuntimeError(f"candidate apply changed unexpected source files: {source_changes}")
            note = (state / note_path).read_text()
            if minted not in note or f'wikilink="[[phase5-spec#{minted}' not in output:
                raise RuntimeError("candidate apply did not mint and render the durable locator")
        records[case_name] = {
            "exitCode": result["exitCode"],
            "normalizedStdoutSha256": result["normalizedStdoutSha256"],
            "normalizedStderrSha256": result["normalizedStderrSha256"],
            "filesystemMutation": result["filesystemMutation"],
            "sourceMutation": source_changes,
            "sqliteSidecarDelta": result["sqliteSidecarDelta"],
            "canonicalLogicalSha256": result["canonicalLogicalSha256"],
            "indexFingerprintsBefore": result["indexFingerprintsBefore"],
            "indexFingerprintsAfter": result["indexFingerprintsAfter"],
        }
    no_write_hashes = {records[name]["canonicalLogicalSha256"] for name in ("baselinePlan", "baselineApply", "candidatePlan")}
    if len(no_write_hashes) != 1:
        raise RuntimeError(f"graph plan/no-op cases changed canonical SQLite content: "
                           f"{ {name: records[name]['canonicalLogicalSha256'] for name in ('baselinePlan', 'baselineApply', 'candidatePlan')} }")
    if records["candidateApply"]["canonicalLogicalSha256"] in no_write_hashes:
        raise RuntimeError("candidate apply did not project its exact source repair into SQLite")
    sidecar_deltas = {json.dumps(record["sqliteSidecarDelta"], sort_keys=True) for record in records.values()}
    if len(sidecar_deltas) != 1:
        raise RuntimeError(f"graph apply repair changed SQLite sidecar mutation shape: "
                           f"{ {name: record['sqliteSidecarDelta'] for name, record in records.items()} }")
    return {
        "approvedDeviation": "baseline plan/apply no-op; candidate plan exposes repair and candidate apply mints exactly one source locator",
        "target": graph_apply_target,
        "noWriteCanonicalSQLiteEquivalent": True,
        "candidateApplyProjectionChanged": True,
        "sqliteSidecarMutationEquivalent": True,
        "cases": records,
    }


def _parity_identity(summary: dict[str, Any]) -> dict[str, Any]:
    return {
        "exitCodes": summary["exitCodes"],
        "normalizedStdoutSha256s": summary["normalizedStdoutSha256s"],
        "normalizedStderrSha256s": summary["normalizedStderrSha256s"],
        "operationEnvelopes": summary["operationEnvelopes"],
        "filesystemMutations": summary["filesystemMutations"],
        "indexFingerprintsBefore": [fingerprint_content_identity(item) for item in summary["indexFingerprintsBefore"]],
        "sqliteSidecarDeltas": summary["sqliteSidecarDeltas"],
        "canonicalLogicalSha256s": summary["canonicalLogicalSha256s"],
    }


def run_matrix(
    binaries: dict[str, Path], source: Path, samples: int, work_root: Path,
    *, timeout_seconds: int, progress: Any | None = None,
) -> dict[str, Any]:
    if samples != 20:
        raise RuntimeError("Phase 5 evidence requires exactly 20 samples")
    kind = source_kind(source)
    source_template = work_root / "source"
    original_manifest = tree_manifest(source)
    if kind == "rhizome":
        source_projection = create_rhizome_projection(source, source_template)
    else:
        copy_state(source, source_template)
        source_projection = {
            "kind": "full-copy",
            "includedFileCount": len(tree_manifest(source_template)),
            "excludedRoots": {},
            "retainedTrackedRoots": {},
            "manifestSha256": sha256_text(json.dumps(tree_manifest(source_template), sort_keys=True)),
        }
    deterministic_provider_overlay = apply_deterministic_provider_overlay(source_template, kind)
    graph_apply_target = prepare_graph_apply_fixture(source_template, kind)
    templates: dict[str, Path] = {}
    template_manifests: dict[str, dict[str, str]] = {}
    template_indexes: dict[str, dict[str, dict[str, Any]]] = {}
    prepared_template = work_root / "canonical-indexed-source"
    # Phase 5 changes no index schema/indexer contract. Build this index once,
    # then clone its exact bytes for both executable lanes.
    copy_state(source_template, prepared_template)
    indexed = subprocess.run(
        [str(binaries["baseline"]), "index", "--vault", str(prepared_template)],
        cwd=prepared_template, text=True, capture_output=True, check=False,
        env={**os.environ, "RZM_SKIP_REPO_DELEGATE": "1"},
    )
    if indexed.returncode != 0:
        raise RuntimeError(f"baseline could not prepare indexed state: {indexed.stderr.strip()}")
    prepared_index = index_fingerprints(prepared_template)
    for binary_name, binary in binaries.items():
        template = work_root / "templates" / binary_name / "indexed"
        template.parent.mkdir(parents=True, exist_ok=True)
        # Copy the one indexed template, not the source. Its bytes, absolute
        # source paths, mtimes, and view fingerprints are therefore identical.
        copy_state(prepared_template, template)
        templates[binary_name] = template
        template_manifests[binary_name] = tree_manifest(template)
        template_indexes[binary_name] = index_fingerprints(template)
        if fingerprint_content_identity(template_indexes[binary_name]) != fingerprint_content_identity(prepared_index):
            raise RuntimeError(f"{binary_name} template changed indexed bytes during copy")
    control_cell = next(cell for cell in phase5_cells(kind) if cell.name == "ontologyQueryRoot")
    control_state = work_root / "volatility-control" / "same-path"
    restore_state(templates["baseline"], control_state)
    control_before = canonical_sqlite_snapshot(control_state / ".rhizome" / "db.sqlite", set())
    run_command(binaries["baseline"], control_cell, control_state, timeout_seconds=timeout_seconds,
                before_manifest=template_manifests["baseline"], before_index_fingerprints=template_indexes["baseline"],
                normalization_roots=(control_state, source_template), volatile_sqlite_columns=set())
    first_control = canonical_sqlite_snapshot(control_state / ".rhizome" / "db.sqlite", set())
    restore_state(templates["baseline"], control_state)
    time.sleep(1.1)
    run_command(binaries["baseline"], control_cell, control_state, timeout_seconds=timeout_seconds,
                before_manifest=template_manifests["baseline"], before_index_fingerprints=template_indexes["baseline"],
                normalization_roots=(control_state, source_template), volatile_sqlite_columns=set())
    second_control = canonical_sqlite_snapshot(control_state / ".rhizome" / "db.sqlite", set())
    observed_volatile_columns = sqlite_snapshot_diff_columns(first_control, second_control)
    if observed_volatile_columns != APPROVED_VOLATILE_SQLITE_COLUMNS:
        raise RuntimeError(f"baseline volatility control found unapproved SQLite differences: {sorted(observed_volatile_columns)}")
    graph_apply_evidence = graph_apply_contract_evidence(
        binaries, templates, template_manifests, template_indexes, graph_apply_target,
        work_root, timeout_seconds=timeout_seconds, volatile_sqlite_columns=observed_volatile_columns,
    )
    cells: dict[str, Any] = {}
    for cell in phase5_cells(kind):
        by_binary: dict[str, Any] = {name: {"firstInvocation": [], "repeatedInvocation": []} for name in binaries}
        for sample in range(samples):
            # Both binaries run serially at this exact path. Restore the same
            # indexed bytes between them so path-derived SQLite rows are real
            # comparable state, not a lane-directory artifact.
            state = work_root / "samples" / "indexed" / cell.name / str(sample + 1)
            state.parent.mkdir(parents=True, exist_ok=True)
            for binary_name, binary in binaries.items():
                restore_state(templates[binary_name], state)
                result = run_command(
                    binary, cell, state, timeout_seconds=timeout_seconds,
                    before_manifest=template_manifests[binary_name],
                    before_index_fingerprints=template_indexes[binary_name],
                    normalization_roots=(state, templates[binary_name], source_template),
                    volatile_sqlite_columns=observed_volatile_columns,
                )
                if sample == 0:
                    by_binary[binary_name]["firstInvocation"].append(result)
                else:
                    by_binary[binary_name]["repeatedInvocation"].append(result)
        by_binary = {name: {mode: summarize(items) for mode, items in modes.items()} for name, modes in by_binary.items()}
        baseline = by_binary["baseline"]
        candidate = by_binary["candidate"]
        parity: dict[str, bool] = {}
        wins: dict[str, float] = {}
        for mode in ("firstInvocation", "repeatedInvocation"):
            parity[mode] = _parity_identity(baseline[mode]) == _parity_identity(candidate[mode])
            wins[mode] = round(baseline[mode]["medianMs"] / candidate[mode]["medianMs"], 3)
        raw_equivalent = {
            mode: [fingerprint_content_identity(item) for item in baseline[mode]["indexFingerprintsAfter"]]
            == [fingerprint_content_identity(item) for item in candidate[mode]["indexFingerprintsAfter"]]
            for mode in ("firstInvocation", "repeatedInvocation")
        }
        cells[cell.name] = {"baseline": baseline, "candidate": candidate, "parity": parity,
                            "rawFingerprintEquivalent": raw_equivalent,
                            "canonicalLogicalContentEquivalent": {mode: baseline[mode]["canonicalLogicalSha256s"] == candidate[mode]["canonicalLogicalSha256s"] for mode in ("firstInvocation", "repeatedInvocation")},
                            "medianWinRatioBaselineOverCandidate": wins}
        if progress is not None:
            progress(cell.name, cells)
        if not all(parity.values()):
            raise RuntimeError(f"Phase 5 parity drift in {cell.name}: {parity}")
    return {
        "source": str(source),
        "sourceKind": kind,
        "sourceProjection": source_projection,
        "deterministicProviderOverlay": deterministic_provider_overlay,
        "graphApplyTarget": graph_apply_target,
        "graphApplyContractEvidence": graph_apply_evidence,
        "volatilityControlColumns": sorted([list(column) for column in observed_volatile_columns]),
        "originalSourceUnchanged": tree_manifest(source) == original_manifest,
        "sourceSha256": sha256_text(json.dumps(tree_manifest(source), sort_keys=True)),
        "state": "indexed",
        "cellIsolation": "same-absolute-path-restored-pristine-template-per-binary-cell-sample",
        "cells": cells,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-rzm", type=Path, required=True)
    parser.add_argument("--candidate-rzm", type=Path, required=True)
    parser.add_argument("--source", type=Path, action="append", required=True)
    parser.add_argument("--samples", type=int, default=20)
    parser.add_argument("--command-timeout-seconds", type=int, default=90)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    binaries = {"baseline": args.baseline_rzm.resolve(), "candidate": args.candidate_rzm.resolve()}
    if any(not path.is_file() for path in binaries.values()):
        parser.error("both binary paths must be files")
    if args.samples != 20:
        parser.error("--samples must be exactly 20 for Phase 5 evidence")
    if args.command_timeout_seconds <= 0:
        parser.error("--command-timeout-seconds must be positive")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    report: dict[str, Any] = {
        "schemaVersion": 1,
        "status": "running",
        "samples": args.samples,
        "firstInvocationSamples": 1,
        "repeatedInvocationSamples": args.samples - 1,
        "measurement": "fresh process; not OS-cold",
        "commandTimeoutSeconds": args.command_timeout_seconds,
        "binaries": {name: {"path": str(path), "sha256": sha256_file(path)} for name, path in binaries.items()},
        "sources": [],
    }
    args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    reports: list[dict[str, Any]] = []
    with tempfile.TemporaryDirectory(prefix="rzm-phase5-twin-") as temporary:
        root = Path(temporary)
        for index, source in enumerate(args.source):
            if not source.is_dir():
                parser.error(f"--source is not a directory: {source}")
            def save_progress(cell_name: str, cells: dict[str, Any]) -> None:
                partial = {"source": str(source.resolve()), "cells": cells, "lastCompletedCell": cell_name}
                report["sources"] = [*reports, partial]
                args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")

            reports.append(run_matrix(
                binaries, source.resolve(), args.samples, root / str(index),
                timeout_seconds=args.command_timeout_seconds, progress=save_progress,
            ))
            report["sources"] = list(reports)
            args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    report["status"] = "complete"
    report["sources"] = reports
    args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
