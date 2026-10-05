#!/usr/bin/env python3
"""Benchmark local user-visible agent-start, file-context, and semantic-query scenarios."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import platform
import shutil
import sqlite3
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

from benchmark_evidence import (
    ScenarioExecutionContract,
    evidence_manifest,
    file_fingerprint,
    has_operation_diagnostics,
    index_fingerprints,
    normalized_channel,
    operation_envelope,
    sha256_text,
)
from benchmark_scenarios import (
    FILE_CONTEXT_SCENARIOS,
    default_execution_contract,
    representative_execution_contracts,
    representative_scenario_commands,
    scenario_commands,
)


VOLATILE_KEYS = {
    "duration", "durationMs", "elapsed", "elapsedMs", "finishedAt",
    "remainingMs", "sessionId", "startedAt", "timings", "timestamp",
}
MINIMAL_ZERO_OPERATIONS = (
    "agent_start.note.passes",
    "agent_start.note.reads",
    "agent_start.code.reads",
    "agent_start.repo.walks",
    "agent_start.index.writes",
    "agent_start.sqlite.schema_statements",
    "agent_start.sqlite.integrity_checks",
    "agent_start.sqlite.transactions",
    "agent_start.session.reserves",
    "agent_start.session.commits",
    "agent_start.session.releases",
    "agent_start.session.commit_failures",
    "agent_start.session.release_failures",
    "agent_start.session.cleanups",
)
LIVE_DISCOVERY_ZERO_OPERATIONS = (
    "agent_start.note.passes",
    "agent_start.note.reads",
    "agent_start.code.reads",
    "agent_start.repo.walks",
)
LIVE_DISCOVERY_ZERO_PHASES = (
    "agent_start.note.crawl",
    "agent_start.coderef.discovery_read",
)
READ_ONLY_ZERO_OPERATIONS = (
    "agent_start.index.writes",
    "agent_start.sqlite.schema_statements",
    "agent_start.sqlite.integrity_checks",
)
SEMANTIC_QUERY_SCENARIOS = (
    "semanticFirstNoSession",
    "semanticRepeatedNoSession",
    "semanticRepeatedSession",
    "semanticOverview",
    "semanticPrecision",
)
EXACT_INDEX_SCENARIOS = (
    "codeSymbol",
    "codeReferences",
    "codeRationale",
    "graphPath",
)


def _exact_index_fixture_facts(source: Path) -> dict[str, Any]:
    """Read the persisted facts that define one exact-index fixture state."""
    rhizome = source / ".rhizome"
    database = rhizome / "db.sqlite"
    if (rhizome / "state").exists():
        raise RuntimeError(f"exact-index fixture must not use a state label: {source}")
    if not database.exists():
        return {"databasePresent": False}
    try:
        connection = sqlite3.connect(f"file:{database}?mode=ro&immutable=1", uri=True)
        try:
            integrity = connection.execute("PRAGMA integrity_check").fetchone()
            tables = {
                row[0] for row in connection.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table'"
                )
            }
            required = {"schema_version", "rzm_migration_state", "index_metadata", "files"}
            if not required <= tables or not ({"intel_code_anchors", "intel_symbol_refs"} & tables):
                raise RuntimeError(
                    f"exact-index fixture is not a supported unified index: {database}"
                )
            schema_row = connection.execute(
                "SELECT version FROM rzm_migration_state WHERE domain = 'intel'"
            ).fetchone()
            schema_version = schema_row[0] if schema_row else None
            metadata = dict(connection.execute(
                "SELECT key, value FROM index_metadata "
                "WHERE key IN ('indexer_version', 'scope_config_hash')"
            ))
            code_rows = 0
            for table in ("intel_code_anchors", "intel_symbol_refs"):
                if table in tables:
                    code_rows += connection.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
        finally:
            connection.close()
    except sqlite3.Error as exc:
        raise RuntimeError(f"exact-index fixture SQLite validation failed for {database}: {exc}") from exc
    if integrity != ("ok",):
        raise RuntimeError(f"exact-index fixture integrity check failed for {database}: {integrity!r}")
    if not isinstance(schema_version, int):
        raise RuntimeError(f"exact-index fixture has no Intel migration state: {database}")
    return {
        "databasePresent": True,
        "schemaVersion": schema_version,
        "metadata": metadata,
        "codeRows": code_rows,
    }


def _fixture_content_fingerprint(source: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(source.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(source)
        if relative.as_posix().startswith(".rhizome/db.sqlite"):
            continue
        encoded = relative.as_posix().encode()
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
        content = path.read_bytes()
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def validate_exact_index_fixtures(sources: dict[str, Path]) -> dict[str, dict[str, Any]]:
    """Validate real SQLite state before any exact-index command runs."""
    expected_states = {"indexed", "missing", "stale", "incompatible"}
    if set(sources) != expected_states:
        raise RuntimeError(
            "exact-index evidence requires indexed, missing, stale, and incompatible fixtures"
        )
    for source in sources.values():
        if (source / ".rhizome" / "state").exists():
            raise RuntimeError(f"exact-index fixture must not use a state label: {source}")
    resolved: dict[str, Path] = {}
    for state, source in sources.items():
        path = source.resolve()
        if not path.is_dir():
            raise RuntimeError(f"exact-index {state} fixture is not a directory: {path}")
        resolved[state] = path
    fingerprints = {_fixture_content_fingerprint(path) for path in resolved.values()}
    if len(fingerprints) != 1:
        raise RuntimeError("exact-index fixtures must have equivalent non-index corpus content")
    facts = {state: _exact_index_fixture_facts(path) for state, path in resolved.items()}
    indexed = facts["indexed"]
    if not indexed["databasePresent"] or indexed["codeRows"] <= 0:
        raise RuntimeError("indexed fixture must contain a real indexed code row")
    indexed_metadata = indexed["metadata"]
    if not indexed_metadata.get("indexer_version") or not indexed_metadata.get("scope_config_hash"):
        raise RuntimeError("indexed fixture must contain indexer and scope freshness metadata")
    if facts["missing"]["databasePresent"]:
        raise RuntimeError("missing fixture must not contain db.sqlite")
    for suffix in ("-wal", "-shm"):
        if Path(str(resolved["missing"] / ".rhizome" / "db.sqlite") + suffix).exists():
            raise RuntimeError("missing fixture must not contain SQLite sidecars")
    stale = facts["stale"]
    if not stale["databasePresent"] or stale["codeRows"] <= 0:
        raise RuntimeError("stale fixture must contain a real indexed code row")
    if stale["schemaVersion"] != indexed["schemaVersion"]:
        raise RuntimeError("stale fixture must retain the indexed schema version")
    stale_metadata = stale["metadata"]
    if not stale_metadata.get("indexer_version") or not stale_metadata.get("scope_config_hash"):
        raise RuntimeError("stale fixture must retain explicit freshness metadata")
    if stale_metadata == indexed_metadata:
        raise RuntimeError("stale fixture must mutate index freshness metadata")
    incompatible = facts["incompatible"]
    if not incompatible["databasePresent"]:
        raise RuntimeError("incompatible fixture must contain a real SQLite database")
    if incompatible["schemaVersion"] <= indexed["schemaVersion"]:
        raise RuntimeError("incompatible fixture must use a future schema version")
    return facts
def normalize(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: normalize(item) for key, item in sorted(value.items()) if key not in VOLATILE_KEYS}
    if isinstance(value, list):
        return [normalize(item) for item in value]
    return value


def percentile(values: list[float], fraction: float) -> float:
    ordered = sorted(values)
    return ordered[max(0, math.ceil(len(ordered) * fraction) - 1)]


def resolve_binary(binary: str) -> str:
    if os.sep in binary:
        resolved = Path(binary).resolve()
        if not resolved.is_file():
            raise RuntimeError(f"rzm binary is not a file: {resolved}")
        return str(resolved)
    resolved = shutil.which(binary)
    if not resolved:
        raise RuntimeError(f"rzm binary was not found on PATH: {binary}")
    return resolved


def environment_manifest() -> dict[str, Any]:
    return {
        "hostname": platform.node(),
        "os": platform.platform(),
        "architecture": platform.machine(),
        "cpu": platform.processor() or None,
        "python": platform.python_version(),
    }


def semantic_query_commands(
    binary: str,
    path: str,
    query: str,
    session_id: str = "semantic-query-benchmark",
    timings: bool = False,
) -> dict[str, list[str]]:
    base = [binary, "agent", "semantic-query", "--path", path, "--query", query, "--limit", "20"]
    if timings:
        base.append("--timings")
    return {
        "semanticFirstNoSession": list(base),
        "semanticRepeatedNoSession": list(base),
        "semanticRepeatedSession": [*base, "--session-id", session_id],
        "semanticOverview": [*base, "--mode", "subsystem_overview"],
        "semanticPrecision": [*base, "--mode", "docs_for_code"],
    }


def exact_index_commands(
    binary: str, *, symbol: str, graph_from: str, graph_to: str
) -> dict[str, list[str]]:
    agent = [binary, "agent"]
    return {
        "codeSymbol": [*agent, "code-symbol", "--symbol", symbol],
        "codeReferences": [*agent, "code-references", "--symbol", symbol],
        "codeRationale": [*agent, "code-rationale"],
        "graphPath": [*agent, "graph-path", "--from", graph_from, "--to", graph_to],
    }


def exact_index_execution_contract(state: str) -> ScenarioExecutionContract:
    if state == "indexed":
        return ScenarioExecutionContract(output_contract="exact-index-v1")
    if state in {"missing", "stale", "incompatible"}:
        # Current exact-index handlers emit typed remediation envelopes for
        # missing, stale, and schema-incompatible indexes.
        return ScenarioExecutionContract(
            expected_exit_codes=(1,),
            json_channel="stderr",
            output_contract="exact-index-v1",
        )
    raise ValueError(f"unknown exact-index state: {state}")


def exact_index_baseline_execution_contract(state: str) -> ScenarioExecutionContract:
    """Accept the baseline envelope while recording its exact observed channel."""
    if state == "indexed":
        return exact_index_execution_contract(state)
    if state in {"missing", "stale", "incompatible"}:
        return ScenarioExecutionContract(
            expected_exit_codes=(0, 1), json_channel="stdout", output_contract="exact-index-v1"
        )
    raise ValueError(f"unknown exact-index state: {state}")


def exact_index_response_evidence(name: str, payload: Any, *, available: bool) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name} output is not a JSON object")
    if not available:
        error = payload.get("error")
        if isinstance(error, str) and error:
            return {"shape": ["error"], "sourceOrder": []}
        if all(isinstance(payload.get(key), str) and payload[key] for key in ("code", "message", "remediation")):
            return {"shape": sorted(payload), "sourceOrder": []}
        raise RuntimeError(f"{name} unavailable output omitted error or typed remediation")

    if name == "codeSymbol":
        if not isinstance(payload.get("symbol"), str) or not isinstance(payload.get("status"), str):
            raise RuntimeError("codeSymbol omitted symbol or status")
        definition = payload.get("definition")
        if payload["status"] == "resolved":
            if not isinstance(definition, dict):
                raise RuntimeError("codeSymbol resolved output omitted definition")
            for key in ("path", "fqn", "startLine", "endLine", "content"):
                if key not in definition:
                    raise RuntimeError(f"codeSymbol definition omitted {key}")
        candidates = payload.get("candidates", [])
        if not isinstance(candidates, list):
            raise RuntimeError("codeSymbol candidates is not a list")
        return {
            "shape": sorted(payload),
            "sourceOrder": [
                [item.get("path"), item.get("startLine"), item.get("anchorId")]
                for item in candidates if isinstance(item, dict)
            ],
        }
    if name == "codeReferences":
        if not isinstance(payload.get("symbol"), str) or not isinstance(payload.get("status"), str):
            raise RuntimeError("codeReferences omitted symbol or status")
        order: list[list[Any]] = []
        for field in ("callers", "callees", "definitions"):
            values = payload.get(field, [])
            if not isinstance(values, list):
                raise RuntimeError(f"codeReferences {field} is not a list")
            order.extend(
                [[field, item.get("path"), item.get("startLine"), item.get("anchorId")]
                 for item in values if isinstance(item, dict)]
            )
        return {"shape": sorted(payload), "sourceOrder": order}
    if name == "codeRationale":
        rows = payload.get("rationale")
        if not isinstance(payload.get("count"), int) or not isinstance(rows, list):
            raise RuntimeError("codeRationale omitted count or rationale")
        if payload["count"] != len(rows):
            raise RuntimeError("codeRationale count does not match rows")
        order = []
        for row in rows:
            if not isinstance(row, dict):
                raise RuntimeError("codeRationale row is not an object")
            for key in ("id", "path", "kind", "startLine", "endLine", "content"):
                if key not in row:
                    raise RuntimeError(f"codeRationale row omitted {key}")
            order.append([row["path"], row["startLine"], row["id"]])
        if order != sorted(order):
            raise RuntimeError("codeRationale source ordering is not stable")
        return {"shape": sorted(payload), "sourceOrder": order}
    if name == "graphPath":
        for key in ("From", "To", "Hops", "Path"):
            if key not in payload:
                raise RuntimeError(f"graphPath omitted {key}")
        if not isinstance(payload["Hops"], int) or not isinstance(payload["Path"], list):
            raise RuntimeError("graphPath emitted invalid hop/path shape")
        order = []
        for edge in payload["Path"]:
            if not isinstance(edge, dict):
                raise RuntimeError("graphPath edge is not an object")
            order.append([edge.get("FromPath"), edge.get("ToPath"), edge.get("EdgeKind")])
        return {"shape": sorted(payload), "sourceOrder": order}
    raise RuntimeError(f"unknown exact-index scenario: {name}")


def semantic_result_identity(payload: dict[str, Any]) -> dict[str, Any]:
    return {
        "modeApplied": payload.get("modeApplied"),
        "targetStatus": payload.get("targetStatus"),
        "lanes": normalize(payload.get("lanes", [])),
        "warnings": normalize(payload.get("warnings", [])),
        "matches": [
            {
                "type": match.get("type"),
                "path": match.get("path"),
                "symbol": match.get("symbol"),
                "startLine": match.get("startLine"),
                "role": match.get("role"),
            }
            for match in payload.get("matches", [])
            if isinstance(match, dict)
        ],
    }


def assert_semantic_query_output_contract(name: str, payload: Any, require_timings: bool) -> None:
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name} output is not a JSON object")
    if not isinstance(payload.get("query"), str) or not payload["query"]:
        raise RuntimeError(f"{name} omitted query")
    if not isinstance(payload.get("matches"), list):
        raise RuntimeError(f"{name} omitted matches")
    if not isinstance(payload.get("lanes"), list):
        raise RuntimeError(f"{name} omitted lanes")
    diagnostics = payload.get("diagnostics")
    if require_timings:
        if not isinstance(diagnostics, dict):
            raise RuntimeError(f"{name} omitted diagnostics")
        for key in ("phases", "operations", "search", "deadline"):
            if key not in diagnostics:
                raise RuntimeError(f"{name} diagnostics omitted {key}")
def assert_output_contract(name: str, payload: Any) -> dict[str, int]:
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name} output is not a JSON object")
    if not isinstance(payload.get("sessionId"), str) or not payload["sessionId"]:
        raise RuntimeError(f"{name} omitted a non-empty sessionId")
    if payload.get("surfaceCommand") != "rzm agent surface":
        raise RuntimeError(f"{name} omitted the surface command pointer")
    if not isinstance(payload.get("notes"), list):
        raise RuntimeError(f"{name} omitted notes")
    context = payload.get("vaultContext")
    if not isinstance(context, dict) or not isinstance(context.get("text"), str) or not context["text"]:
        raise RuntimeError(f"{name} omitted vaultContext.text")
    diagnostics = payload.get("diagnostics")
    operations = diagnostics.get("operations") if isinstance(diagnostics, dict) else None
    if not isinstance(operations, list):
        raise RuntimeError(f"{name} omitted timing operation diagnostics")
    counts = {
        operation["label"]: operation["count"]
        for operation in operations
        if isinstance(operation, dict)
        and isinstance(operation.get("label"), str)
        and isinstance(operation.get("count"), int)
    }
    phases = diagnostics.get("phases")
    phase_counts = {
        phase["label"]: phase["count"]
        for phase in phases
        if isinstance(phase, dict)
        and isinstance(phase.get("label"), str)
        and isinstance(phase.get("count"), int)
    } if isinstance(phases, list) else {}
    if name in {"firstBare", "repeatedBare", "directoryTargeted"}:
        for label in MINIMAL_ZERO_OPERATIONS:
            if counts.get(label) != 0:
                raise RuntimeError(f"{name} expected {label}=0, got {counts.get(label)!r}")
        if "Graph statistics" in context["text"]:
            raise RuntimeError(f"{name} unexpectedly emitted removed graph statistics")
    if name == "explicitRich":
        enrichment = context.get("indexedEnrichment")
        if not isinstance(enrichment, dict) or enrichment.get("status") != "available":
            status = enrichment.get("status") if isinstance(enrichment, dict) else None
            raise RuntimeError(
                f"{name} expected indexed-enrichment status available, got {status!r}"
            )
        ontology = payload.get("ontology")
        if (
            not isinstance(ontology, dict)
            or ontology.get("available") is not True
            or ontology.get("ready") is not True
            or not isinstance(ontology.get("schemaHash"), str)
            or not ontology["schemaHash"]
            or not isinstance(ontology.get("totalNotes"), int)
            or not isinstance(ontology.get("typeCounts"), list)
            or not ontology["typeCounts"]
        ):
            raise RuntimeError(f"{name} omitted a meaningful ready ontology summary")
        for label in LIVE_DISCOVERY_ZERO_OPERATIONS:
            if counts.get(label) != 0:
                raise RuntimeError(f"{name} expected {label}=0, got {counts.get(label)!r}")
        for label in READ_ONLY_ZERO_OPERATIONS:
            if counts.get(label) != 0:
                raise RuntimeError(f"{name} expected {label}=0, got {counts.get(label)!r}")
        session_reserves = counts.get("agent_start.session.reserves")
        session_commits = counts.get("agent_start.session.commits")
        if session_reserves != session_commits:
            raise RuntimeError(
                f"{name} expected balanced session reserve/commit counts, "
                f"got {session_reserves!r}/{session_commits!r}"
            )
        for label in (
            "agent_start.session.releases",
            "agent_start.session.commit_failures",
            "agent_start.session.release_failures",
            "agent_start.session.cleanups",
        ):
            if counts.get(label) != 0:
                raise RuntimeError(f"{name} expected {label}=0, got {counts.get(label)!r}")
        expected_transactions = (session_reserves or 0) + (session_commits or 0)
        if counts.get("agent_start.sqlite.transactions") != expected_transactions:
            raise RuntimeError(
                f"{name} expected session-only sqlite transactions="
                f"{expected_transactions}, got "
                f"{counts.get('agent_start.sqlite.transactions')!r}"
            )
        for label in LIVE_DISCOVERY_ZERO_PHASES:
            if phase_counts.get(label) != 0:
                raise RuntimeError(f"{name} expected {label}=0, got {phase_counts.get(label)!r}")
    if counts.get("agent_start.sqlite.integrity_checks") != 0:
        raise RuntimeError(f"{name} ran an integrity check")
    return counts


def assert_file_context_output_contract(name: str, payload: Any) -> None:
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name} output is not a JSON object")
    if not isinstance(payload.get("sessionId"), str) or not payload["sessionId"]:
        raise RuntimeError(f"{name} omitted a non-empty sessionId")
    text = payload.get("text")
    if not isinstance(text, str) or not text:
        raise RuntimeError(f"{name} omitted non-empty file-context text")
    if "tool: file_context" not in text:
        raise RuntimeError(f"{name} output was not file-context text")
    lines = text.splitlines()
    for index in range(len(lines) - 2):
        heading = lines[index]
        if (
            heading.startswith("### ")
            and heading.endswith((" (code)", " (note)"))
            and not lines[index + 1]
            and lines[index + 2].startswith("Error:")
        ):
            raise RuntimeError(f"{name} emitted a rendered target error")
    enrichment = payload.get("indexedEnrichment")
    status = enrichment.get("status") if isinstance(enrichment, dict) else None
    if status != "available":
        raise RuntimeError(
            f"{name} expected indexedEnrichment.status=available, got {status!r}"
        )
    dedupe_hits = payload.get("dedupeHits")
    if dedupe_hits is not None and (
        not isinstance(dedupe_hits, int) or dedupe_hits < 0
    ):
        raise RuntimeError(f"{name} emitted invalid dedupeHits")


def run_samples(
    name: str,
    command: list[str],
    source: Path,
    samples: int,
    *,
    contract: ScenarioExecutionContract | None = None,
    require_index_unchanged: bool = True,
) -> dict[str, Any]:
    semantic_query_scenario = name in SEMANTIC_QUERY_SCENARIOS
    if contract is None:
        contract = (
            ScenarioExecutionContract(output_contract="semantic-query-v1")
            if semantic_query_scenario
            else default_execution_contract(name)
        )
    durations: list[float] = []
    stdout_hashes: list[str] = []
    stderr_hashes: list[str] = []
    exit_codes: list[int] = []
    diagnostics: list[Any] = []
    operation_counts: list[dict[str, int]] = []
    operation_envelopes: list[list[dict[str, Any]]] = []
    operation_diagnostics_available: list[bool] = []
    indexed_enrichment_statuses: list[str] = []
    stdout_bytes: list[int] = []
    stderr_bytes: list[int] = []
    file_context_scenario = name in FILE_CONTEXT_SCENARIOS
    semantic_timings = "--timings" in command
    result_identity_hashes: list[str] = []
    full_stdout_hashes: list[str] = []
    full_stderr_hashes: list[str] = []
    index_fingerprints_before: list[dict[str, Any]] = []
    index_fingerprints_after: list[dict[str, Any]] = []
    exact_index_evidence: list[dict[str, Any]] = []
    exact_index_scenario = contract.output_contract == "exact-index-v1"
    command_env = os.environ.copy()
    command_env["RZM_SKIP_REPO_DELEGATE"] = "1"
    for iteration in range(samples):
        if exact_index_scenario:
            index_before = index_fingerprints(source)
        started = time.perf_counter()
        result = subprocess.run(
            command,
            cwd=source,
            env=command_env,
            text=True,
            capture_output=True,
            check=False,
        )
        elapsed_ms = (time.perf_counter() - started) * 1000
        if result.returncode not in contract.expected_exit_codes:
            raise RuntimeError(
                f"{name} iteration {iteration + 1} returned unexpected exit code "
                f"{result.returncode} (expected {contract.expected_exit_codes}): "
                f"{result.stderr.strip()}"
            )
        try:
            if exact_index_scenario:
                normalized_stdout, stdout_payload = normalized_channel(
                    result.stdout, parse_json=bool(result.stdout.strip()), normalize=normalize,
                )
                normalized_stderr, stderr_payload = normalized_channel(
                    result.stderr, parse_json=bool(result.stderr.strip()), normalize=normalize,
                )
            else:
                normalized_stdout, stdout_payload = normalized_channel(
                    result.stdout,
                    parse_json=contract.output_format == "json" and contract.json_channel == "stdout",
                    normalize=normalize,
                )
                normalized_stderr, stderr_payload = normalized_channel(
                    result.stderr,
                    parse_json=contract.output_format == "json" and contract.json_channel == "stderr",
                    normalize=normalize,
                )
        except RuntimeError as exc:
            raise RuntimeError(f"{name} iteration {iteration + 1} {exc}") from exc
        payload = stdout_payload if contract.json_channel == "stdout" else stderr_payload
        if result.returncode == 0:
            if semantic_query_scenario:
                assert_semantic_query_output_contract(name, payload, semantic_timings)
            elif file_context_scenario:
                assert_file_context_output_contract(name, payload)
            elif contract.output_contract == "agent-start-v2":
                operation_counts.append(assert_output_contract(name, payload))
        if exact_index_scenario:
            exact_index_evidence.append(exact_index_response_evidence(
                name, payload, available=result.returncode == 0
            ))
            index_after = index_fingerprints(source)
            index_fingerprints_before.append(index_before)
            index_fingerprints_after.append(index_after)
            if require_index_unchanged and index_before != index_after:
                raise RuntimeError(f"{name} iteration {iteration + 1} changed the index or sidecars")
        if name == "explicitRich":
            indexed_enrichment_statuses.append(
                payload["vaultContext"]["indexedEnrichment"]["status"]
            )
        if semantic_query_scenario:
            identity = semantic_result_identity(payload)
            stable = json.dumps(identity, sort_keys=True, separators=(",", ":"))
            result_identity_hashes.append(sha256_text(stable))
        durations.append(elapsed_ms)
        exit_codes.append(result.returncode)
        stdout_hashes.append(sha256_text(normalized_stdout))
        stderr_hashes.append(sha256_text(normalized_stderr))
        full_stdout_hashes.append(sha256_text(result.stdout))
        full_stderr_hashes.append(sha256_text(result.stderr))
        stdout_bytes.append(len(result.stdout.encode()))
        stderr_bytes.append(len(result.stderr.encode()))
        envelope = operation_envelope(payload)
        operations_available = has_operation_diagnostics(payload)
        operation_diagnostics_available.append(operations_available)
        if operations_available:
            operation_envelopes.append(envelope)
        if isinstance(payload, dict) and isinstance(payload.get("diagnostics"), dict):
            diagnostics.append(payload["diagnostics"])
        if iteration == 0 and isinstance(payload, dict) and isinstance(payload.get("packMeta"), dict):
            report_pack_meta = payload["packMeta"]
    equivalent = (
        len(set(exit_codes)) == 1
        and len(set(stdout_hashes)) == 1
        and len(set(stderr_hashes)) == 1
    )
    payload_hashes = stdout_hashes if contract.json_channel != "stderr" else stderr_hashes
    operation_envelope_hashes = [
        sha256_text(json.dumps(envelope, sort_keys=True, separators=(",", ":")))
        for envelope in operation_envelopes
    ]
    if semantic_query_scenario:
        equivalent = len(set(result_identity_hashes)) == 1
    report = {
        "command": command,
        "samples": samples,
        "medianMs": round(statistics.median(durations), 3),
        "p95Ms": round(percentile(durations, 0.95), 3),
        "durationsMs": [round(value, 3) for value in durations],
        "exitCodes": exit_codes,
        "normalizedStdoutSha256s": stdout_hashes,
        "normalizedStderrSha256s": stderr_hashes,
        "uniqueNormalizedStdoutOutputs": len(set(stdout_hashes)),
        "uniqueNormalizedStderrOutputs": len(set(stderr_hashes)),
        "normalizedOutputSha256": payload_hashes[0],
        "outputEquivalent": equivalent,
        "stdoutBytes": stdout_bytes,
        "stderrBytes": stderr_bytes,
        "outputBytes": stdout_bytes,
        "outputContract": contract.output_contract,
        "outputFormat": contract.output_format,
        "operationDiagnosticsAvailable": all(operation_diagnostics_available),
        "operationDiagnosticsAvailableBySample": operation_diagnostics_available,
    }
    if file_context_scenario:
        report["runMode"] = "repeatedOneShot"
        report["indexStatePrecondition"] = "explicitRich.available"
    if semantic_query_scenario:
        report["runMode"] = "firstOneShot" if name == "semanticFirstNoSession" else "repeatedOneShot"
        report["resultIdentity"] = "ordered type/path/symbol/startLine/role plus lanes and warnings"
        report["resultIdentitySha256s"] = result_identity_hashes
        report["uniqueResultIdentities"] = len(set(result_identity_hashes))
        report["normalizedOutputSha256"] = result_identity_hashes[0]
        report["normalizedOutputSha256s"] = result_identity_hashes
        report["uniqueNormalizedOutputs"] = len(set(result_identity_hashes))
    if operation_counts:
        report["operationCounts"] = operation_counts
    if operation_envelopes:
        report["operationEnvelopes"] = operation_envelopes
        report["operationEnvelopeSha256s"] = operation_envelope_hashes
        report["operationEnvelopesEquivalent"] = len(set(operation_envelope_hashes)) == 1
    if diagnostics:
        report["diagnostics"] = diagnostics
    if indexed_enrichment_statuses:
        report["indexedEnrichmentStatuses"] = indexed_enrichment_statuses
    if exact_index_scenario:
        report["fullStdoutSha256s"] = full_stdout_hashes
        report["fullStderrSha256s"] = full_stderr_hashes
        report["fullOutputEquivalent"] = (
            len(set(exit_codes)) == 1
            and len(set(full_stdout_hashes)) == 1
            and len(set(full_stderr_hashes)) == 1
        )
        report["indexFingerprintsBefore"] = index_fingerprints_before
        report["indexFingerprintsAfter"] = index_fingerprints_after
        report["indexFingerprintsUnchanged"] = all(
            before == after
            for before, after in zip(index_fingerprints_before, index_fingerprints_after)
        )
        report["responseEvidence"] = exact_index_evidence
    if "report_pack_meta" in locals():
        report["packMeta"] = report_pack_meta
    return report


def run_exact_index_evidence(
    binary: str,
    sources: dict[str, Path],
    *,
    samples: int,
    symbol: str,
    graph_from: str,
    graph_to: str,
    require_index_unchanged: bool = True,
    contract_for_state: Any = exact_index_execution_contract,
    cell_copy_root: Path | None = None,
) -> dict[str, Any]:
    """Capture per-operation exact-index evidence from disposable fixtures.

    The caller supplies one indexed fixture and three separately prepared
    fixtures. This avoids mutating a benchmark corpus merely to manufacture
    missing, stale, or incompatible states. Each unavailable state must retain
    its typed, state-specific remediation.
    """
    fixture_facts = validate_exact_index_fixtures(sources)
    commands = exact_index_commands(
        binary, symbol=symbol, graph_from=graph_from, graph_to=graph_to
    )
    states: dict[str, dict[str, Any]] = {}
    for state in ("indexed", "missing", "stale", "incompatible"):
        source = sources[state].resolve()
        contract = contract_for_state(state)
        cells: dict[str, dict[str, Any]] = {}
        for name, command in commands.items():
            cell_source = source
            cell_facts = fixture_facts[state]
            if cell_copy_root is not None:
                # A command must never observe mutations made by another
                # command. Revalidate the copied fixture before every cell.
                cell_source = cell_copy_root / state / name
                shutil.copytree(source, cell_source)
                copied_sources = dict(sources)
                copied_sources[state] = cell_source
                cell_facts = validate_exact_index_fixtures(copied_sources)[state]
            cell = run_samples(
                name, command, cell_source, samples, contract=contract,
                require_index_unchanged=require_index_unchanged,
            )
            if cell_copy_root is not None:
                cell["fixtureStateBeforeRun"] = cell_facts
            cells[name] = cell
        states[state] = cells

    return {
        "contract": "exact-index-evidence-v1",
        "states": states,
        "fixtureFacts": fixture_facts,
        "indexMutationForbidden": require_index_unchanged,
        "cellIsolation": "fresh-copy-per-command" if cell_copy_root is not None else "caller-owned",
    }


def _exact_index_cell_identity(cell: dict[str, Any]) -> dict[str, Any]:
    return {
        "exitCodes": cell["exitCodes"],
        "normalizedStdoutSha256s": cell["normalizedStdoutSha256s"],
        "normalizedStderrSha256s": cell["normalizedStderrSha256s"],
        "fullStdoutSha256s": cell["fullStdoutSha256s"],
        "fullStderrSha256s": cell["fullStderrSha256s"],
        "responseEvidence": cell["responseEvidence"],
    }


def run_exact_index_baseline_comparison(
    baseline_binary: str,
    current_binary: str,
    sources: dict[str, Path],
    *,
    samples: int,
    symbol: str,
    graph_from: str,
    graph_to: str,
) -> dict[str, Any]:
    """Run every exact-index cell on a freshly copied, revalidated fixture."""
    if samples < 2:
        raise RuntimeError("exact-index evidence requires at least two samples")
    fixture_facts = validate_exact_index_fixtures(sources)
    with tempfile.TemporaryDirectory(prefix="rzm-exact-index-baseline-") as temp_dir:
        root = Path(temp_dir)
        baseline = run_exact_index_evidence(
            baseline_binary, sources, samples=samples, symbol=symbol,
            graph_from=graph_from, graph_to=graph_to, require_index_unchanged=False,
            contract_for_state=exact_index_baseline_execution_contract,
            cell_copy_root=root / "baseline",
        )
        current = run_exact_index_evidence(
            current_binary, sources, samples=samples, symbol=symbol,
            graph_from=graph_from, graph_to=graph_to,
            cell_copy_root=root / "current",
        )
    differences: list[dict[str, str]] = []
    for state in ("indexed", "missing", "stale", "incompatible"):
        for name in EXACT_INDEX_SCENARIOS:
            baseline_cell = baseline["states"][state][name]
            current_cell = current["states"][state][name]
            if _exact_index_cell_identity(baseline_cell) != _exact_index_cell_identity(current_cell):
                differences.append({"state": state, "scenario": name})
            if not current_cell["indexFingerprintsUnchanged"]:
                raise RuntimeError(f"current exact-index command mutated the index for {state}/{name}")
    return {
        "contract": "exact-index-baseline-current-v1",
        "samples": samples,
        "fixtureFacts": fixture_facts,
        "baseline": {
            "binary": file_fingerprint(Path(baseline_binary)),
            "evidence": baseline,
        },
        "current": {
            "binary": file_fingerprint(Path(current_binary)),
            "evidence": current,
        },
        "baselineCurrentEquivalent": not differences,
        "baselineCurrentDifferences": differences,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rzm", default=os.environ.get("RZM", "rzm"), help="rzm binary to execute")
    parser.add_argument("--source", type=Path, default=Path.cwd(), help="repository root to benchmark")
    parser.add_argument("--directory-target", default="docs/specs", help="directory used by targeted and rich scenarios")
    parser.add_argument(
        "--file-context-code-target",
        default="pkg/app/cli/file_context.go",
        help="exact code file used by file-context scenarios",
    )
    parser.add_argument(
        "--file-context-directory-target",
        default="pkg/app/cli",
        help="directory used by file-context scenarios",
    )
    parser.add_argument(
        "--file-context-note-target",
        default="docs/reference/subsystems/agent-surface.md",
        help="Markdown note used by file-context scenarios",
    )
    parser.add_argument("--samples", type=int, default=7, help="samples for every repeated scenario (minimum 7)")
    parser.add_argument("--include-semantic-query", action="store_true", help="include semantic-query benchmark cells")
    parser.add_argument("--semantic-query-only", action="store_true", help="run only semantic-query benchmark cells")
    parser.add_argument(
        "--semantic-query-scenario",
        action="append",
        choices=SEMANTIC_QUERY_SCENARIOS,
        help="semantic-query cell to run (repeatable; defaults to all cells)",
    )
    parser.add_argument("--semantic-query-path", default="pkg/app/mcp", help="path seed for semantic-query scenarios")
    parser.add_argument(
        "--semantic-query-text",
        default="semantic query transaction invariants identifier mappings",
        help="query text for semantic-query scenarios",
    )
    parser.add_argument("--semantic-query-timings", action="store_true", help="request semantic-query diagnostics")
    parser.add_argument(
        "--extended",
        action="store_true",
        help="include representative schema/index/live/ontology/unavailable/root-search cells",
    )
    parser.add_argument(
        "--include-exact-index-evidence",
        action="store_true", help="compare exact-index parity from four disposable fixtures",
    )
    parser.add_argument("--exact-index-only", action="store_true", help="run only exact-index comparison evidence")
    parser.add_argument("--baseline-rzm", help="baseline rzm binary for exact-index comparison")
    parser.add_argument("--exact-index-symbol", default="RuntimeRequirements")
    parser.add_argument("--exact-index-graph-from", default="README.md")
    parser.add_argument("--exact-index-graph-to", default="CONTEXT.md")
    parser.add_argument("--exact-index-missing-source", type=Path)
    parser.add_argument("--exact-index-stale-source", type=Path)
    parser.add_argument("--exact-index-incompatible-source", type=Path)
    parser.add_argument("--output", type=Path, help="write JSON report to this path (stdout by default)")
    args = parser.parse_args()
    if args.samples < 7:
        parser.error("--samples must be at least 7")
    if args.exact_index_only and not args.include_exact_index_evidence:
        parser.error("--exact-index-only requires --include-exact-index-evidence")
    source = args.source.resolve()
    if not source.is_dir():
        parser.error(f"--source is not a directory: {source}")
    try:
        binary = resolve_binary(args.rzm)
        commands = scenario_commands(
            binary,
            args.directory_target,
            args.file_context_code_target,
            args.file_context_directory_target,
            args.file_context_note_target,
        )
        scenarios: dict[str, dict[str, Any]] = {}
        if not args.semantic_query_only and not args.exact_index_only:
            scenarios = {
                "firstBare": run_samples("firstBare", commands["firstBare"], source, 1),
                "repeatedBare": run_samples("repeatedBare", commands["repeatedBare"], source, args.samples),
                "directoryTargeted": run_samples("directoryTargeted", commands["directoryTargeted"], source, args.samples),
                "explicitRich": run_samples("explicitRich", commands["explicitRich"], source, args.samples),
                "repeatedIndexedFileContextCode": run_samples(
                    "repeatedIndexedFileContextCode", commands["repeatedIndexedFileContextCode"], source, args.samples,
                ),
                "repeatedIndexedFileContextDirectory": run_samples(
                    "repeatedIndexedFileContextDirectory", commands["repeatedIndexedFileContextDirectory"], source, args.samples,
                ),
                "repeatedIndexedFileContextMarkdown": run_samples(
                    "repeatedIndexedFileContextMarkdown", commands["repeatedIndexedFileContextMarkdown"], source, args.samples,
                ),
                "repeatedIndexedFileContextMixed": run_samples(
                    "repeatedIndexedFileContextMixed", commands["repeatedIndexedFileContextMixed"], source, args.samples,
                ),
            }
            if args.extended:
                representative_commands = representative_scenario_commands(
                    binary, code_target=args.file_context_code_target
                )
                representative_contracts = representative_execution_contracts()
                for name, command in representative_commands.items():
                    scenarios[name] = run_samples(
                        name,
                        command,
                        source,
                        args.samples,
                        contract=representative_contracts[name],
                    )
        if (args.include_semantic_query or args.semantic_query_only) and not args.exact_index_only:
            semantic_commands = semantic_query_commands(
                binary,
                args.semantic_query_path,
                args.semantic_query_text,
                timings=args.semantic_query_timings,
            )
            selected = args.semantic_query_scenario or list(SEMANTIC_QUERY_SCENARIOS)
            for name in selected:
                sample_count = 1 if name == "semanticFirstNoSession" else args.samples
                scenarios[name] = run_samples(name, semantic_commands[name], source, sample_count)
        exact_index_evidence = None
        if args.include_exact_index_evidence:
            if not args.baseline_rzm:
                raise RuntimeError("exact-index evidence requires --baseline-rzm")
            baseline_binary = resolve_binary(args.baseline_rzm)
            unavailable_sources = {
                "missing": args.exact_index_missing_source,
                "stale": args.exact_index_stale_source,
                "incompatible": args.exact_index_incompatible_source,
            }
            if any(path is None for path in unavailable_sources.values()):
                raise RuntimeError(
                    "exact-index evidence requires --exact-index-missing-source, "
                    "--exact-index-stale-source, and --exact-index-incompatible-source"
                )
            exact_index_evidence = run_exact_index_baseline_comparison(
                baseline_binary,
                binary,
                {"indexed": source, **unavailable_sources},
                samples=args.samples,
                symbol=args.exact_index_symbol,
                graph_from=args.exact_index_graph_from,
                graph_to=args.exact_index_graph_to,
            )
    except RuntimeError as exc:
        parser.error(str(exc))
    report = {
        "schemaVersion": 3,
        "environment": environment_manifest(),
        **evidence_manifest(binary, source),
        "sourceRoot": str(source),
        "scenarios": scenarios,
    }
    if exact_index_evidence is not None:
        report["exactIndexEvidence"] = exact_index_evidence
    rendered = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.write_text(rendered)
    else:
        sys.stdout.write(rendered)
    scenario_equivalent = all(value["outputEquivalent"] for value in scenarios.values())
    exact_equivalent = exact_index_evidence is None or (
        exact_index_evidence["baselineCurrentEquivalent"]
        and all(
            cell["outputEquivalent"] and cell["fullOutputEquivalent"]
            and cell["indexFingerprintsUnchanged"]
            for cells in exact_index_evidence["current"]["evidence"]["states"].values()
            for cell in cells.values()
        )
    )
    return 0 if scenario_equivalent and exact_equivalent else 2


if __name__ == "__main__":
    raise SystemExit(main())
