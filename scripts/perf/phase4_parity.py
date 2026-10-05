#!/usr/bin/env python3
"""Record Phase 4 one-shot parity from disposable, state-specific fixtures.

This is evidence tooling, not a benchmark.  Each binary/state/operation/sample
receives a new copy of a prepared fixture.  The report keeps both normalized
and exact channels, JSON shape/order evidence, warning/remediation fields, and
the SQLite DB/WAL/SHM fingerprints before and after the command.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import sqlite3
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable

from benchmark_evidence import file_fingerprint, index_fingerprints, sha256_text


VOLATILE_KEYS = {
    "durationMs", "elapsedMs", "sessionId", "startedAt", "finishedAt",
    "timestamp", "generatedAt", "analyzedAt", "latestAgeDays",
}
REQUIRED_STATES = ("current", "missing", "stale", "incompatible", "provider-unavailable")
FIND_CONNECTION_STATES = REQUIRED_STATES
SESSION_RESERVATION_TABLES = frozenset({
    "mcp_sessions",
    "mcp_session_items",
    "mcp_session_item_reservations",
    "mcp_session_maintenance",
})


@dataclass(frozen=True)
class Phase4Cell:
    name: str
    command: tuple[str, ...]
    states: tuple[str, ...] = ("current", "missing", "stale", "incompatible")
    index_mutation_policy: str = "forbid"

    def argv(self, binary: str) -> list[str]:
        return [binary, *self.command]


def phase4_cells() -> tuple[Phase4Cell, ...]:
    """Return every Phase 4 operation and request shape that needs parity."""
    agent = ("agent",)
    cells = [
        Phase4Cell("files-basic", agent + ("files", "--input", "notes/product-brief.md", "--include-content", "false", "--limit", "5")),
        Phase4Cell("files-graph-max-depth", agent + ("files", "--input", "notes/product-brief.md", "--include-content", "false", "--include-backlinks", "--max-depth", "2", "--limit", "10"), index_mutation_policy="query-only"),
        Phase4Cell("tags", agent + ("list-tags", "--match", "project")),
        Phase4Cell("properties", agent + ("list-properties", "--source", "all", "--value-counts", "--verbose"), index_mutation_policy="query-only"),
        Phase4Cell("vault-health", agent + ("vault-health", "--include", "staleNotes", "--include", "deadEnds", "--limit", "10"), index_mutation_policy="query-only"),
        Phase4Cell("community-list", agent + ("community-list", "--max-communities", "5", "--max-top-notes", "3"), index_mutation_policy="query-only"),
        Phase4Cell("vault-context-vault-profile", agent + ("vault-context", "--profile", "vault", "--context-file", "notes/product-brief.md"), index_mutation_policy="session-dedupe"),
        Phase4Cell("vault-context-code-profile", agent + ("vault-context", "--profile", "code", "--file", "src/todoapp/main.py", "--submodule-depth", "1"), index_mutation_policy="session-dedupe"),
        Phase4Cell("vault-context-normalized-options", agent + ("vault-context", "--profile", "vault", "--include-tags=false", "--recency-cascade=false", "--graph-summary", "--context-file", "notes/product-brief.md"), index_mutation_policy="session-dedupe"),
        Phase4Cell("find-connections-note", agent + ("find-connections", "--note", "notes/product-brief.md", "--limit", "5"), FIND_CONNECTION_STATES, "query-only"),
        Phase4Cell("find-connections-text", agent + ("find-connections", "--text", "product roadmap delivery", "--limit", "5"), FIND_CONNECTION_STATES, "query-only"),
        Phase4Cell("node-link-plan", agent + ("node-link", "--ensure", "plan", "--target", "notes/product-brief.md#Product Brief")),
        Phase4Cell("node-link-apply-refusal", agent + ("node-link", "--ensure", "apply", "--target", "notes/product-brief.md#Product Brief")),
        Phase4Cell("rename-heading-plan", agent + ("note-rename-heading", "notes/product-brief.md", "Product Brief", "Product Direction")),
        Phase4Cell("rename-heading-apply-refusal", agent + ("note-rename-heading", "notes/product-brief.md", "Product Brief", "Product Direction", "--apply")),
        # The agent rows above invoke the shared MCP handlers in-process.  The
        # root rows are included only where a user-visible root twin exists.
        Phase4Cell("graph-file-context-plan", ("graph", "file-context", "notes/product-brief.md", "--ensure-link-targets", "plan"), ("current",), "preserve"),
        Phase4Cell("graph-file-context-apply", ("graph", "file-context", "notes/product-brief.md", "--ensure-link-targets", "apply"), ("current",), "preserve"),
        Phase4Cell("root-vault-context-vault-profile", ("graph", "vault-context", "--profile", "vault"), ("current",), "preserve"),
    ]
    for operation in ("doc_coverage", "complexity", "hotspots", "rationale_attention", "relatedness", "code_similarity"):
        cells.append(Phase4Cell(f"report-{operation}", agent + ("report", "--op", operation, "--path", "src", "--limit", "10"), index_mutation_policy="query-only"))
    return tuple(cells)


def _normalize(value: Any, fixture_root: Path | None = None) -> Any:
    if isinstance(value, dict):
        return {
            key: _normalize(item, fixture_root)
            for key, item in value.items()
            if key not in VOLATILE_KEYS
        }
    if isinstance(value, list):
        return [_normalize(item, fixture_root) for item in value]
    if isinstance(value, str) and fixture_root is not None:
        normalized = value.replace(str(fixture_root), "<fixture>")
        return re.sub(
            rf"(?m)^- vault: {re.escape(fixture_root.name)}$",
            "- vault: <fixture>",
            normalized,
        )
    return value


def _json_shape(value: Any) -> Any:
    if isinstance(value, dict):
        return {"object": list(value), "fields": {key: _json_shape(item) for key, item in value.items()}}
    if isinstance(value, list):
        return {"list": [_json_shape(item) for item in value]}
    return type(value).__name__


def _warning_remediation(value: Any) -> list[dict[str, Any]]:
    found: list[dict[str, Any]] = []
    if isinstance(value, dict):
        record = {key: value[key] for key in ("code", "warning", "remediation", "message", "nextAction") if key in value}
        if record:
            found.append(record)
        for item in value.values():
            found.extend(_warning_remediation(item))
    elif isinstance(value, list):
        for item in value:
            found.extend(_warning_remediation(item))
    return found


def _parse_channel(text: str, fixture_root: Path) -> tuple[Any | None, str, str]:
    if not text.strip():
        return None, "", sha256_text("")
    try:
        value = json.loads(text)
    except json.JSONDecodeError:
        normalized = text.replace(str(fixture_root), "<fixture>")
        normalized = re.sub(
            r"(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} ",
            "<go-log-time> ",
            normalized,
        )
        return None, normalized, sha256_text(normalized)
    normalized = json.dumps(_normalize(value, fixture_root), separators=(",", ":"))
    return value, normalized, sha256_text(normalized)


def _sqlite_table_facts(source: Path) -> dict[str, Any]:
    """Return deterministic row facts without creating SQLite sidecars.

    These facts make the two mutation exceptions narrow.  Session-dedupe and
    root-twin cells may change only their MCP session reservation tables; all
    migrations, metadata, index content, and projections remain protected.
    """
    database = source / ".rhizome" / "db.sqlite"
    if not database.is_file():
        return {"databasePresent": False, "schemaSha256": None, "tables": {}}
    try:
        # This inspection is itself part of the no-write proof.  Immutable
        # mode prevents SQLite from creating coordination sidecars while the
        # command's before/after fingerprints remain authoritative for them.
        connection = sqlite3.connect(f"file:{database}?mode=ro&immutable=1", uri=True)
        try:
            schema_objects = list(connection.execute(
                "SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master "
                "WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name, tbl_name"
            ))
            schema_sha256 = hashlib.sha256(
                json.dumps(schema_objects, separators=(",", ":"), ensure_ascii=False).encode()
            ).hexdigest()
            table_definitions = [
                (row[0], row[1] or "")
                for row in connection.execute(
                    "SELECT name, sql FROM sqlite_master "
                    "WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
                )
            ]
            tables: dict[str, dict[str, Any]] = {}
            for table, definition in table_definitions:
                if definition.lstrip().upper().startswith("CREATE VIRTUAL TABLE"):
                    # The fixture contains vec0 tables.  Python's standard
                    # SQLite cannot load that module, but the ordinary shadow
                    # tables remain queryable and carry its stored contents.
                    tables[table] = {
                        "virtual": True,
                        "definitionSha256": hashlib.sha256(definition.encode()).hexdigest(),
                    }
                    continue
                escaped = table.replace('"', '""')
                cursor = connection.execute(f'SELECT * FROM "{escaped}"')
                digest = hashlib.sha256()
                count = 0
                for row in cursor:
                    values = [
                        {"bytesSha256": hashlib.sha256(value).hexdigest()}
                        if isinstance(value, bytes)
                        else value
                        for value in row
                    ]
                    digest.update(json.dumps(values, separators=(",", ":"), ensure_ascii=False).encode())
                    digest.update(b"\n")
                    count += 1
                tables[table] = {"rowCount": count, "rowsSha256": digest.hexdigest()}
        finally:
            connection.close()
    except sqlite3.Error as exc:
        raise ValueError(f"cannot inspect SQLite mutation state: {database}: {exc}") from exc
    return {"databasePresent": True, "schemaSha256": schema_sha256, "tables": tables}


def _table_changes(before: dict[str, Any], after: dict[str, Any]) -> list[str]:
    names = set(before["tables"]) | set(after["tables"])
    return sorted(name for name in names if before["tables"].get(name) != after["tables"].get(name))


def _session_reservation_invariant(
    before: dict[str, Any], after: dict[str, Any]
) -> dict[str, Any]:
    """Allow only MCP session reservation state to change."""
    table_changes = _table_changes(before, after)
    protected_changes = [
        name for name in table_changes if name not in SESSION_RESERVATION_TABLES
    ]
    database_preserved = before["databasePresent"] == after["databasePresent"]
    schema_preserved = before["schemaSha256"] == after["schemaSha256"]
    return {
        "databasePreserved": database_preserved,
        "schemaPreserved": schema_preserved,
        "sessionTableChanges": [
            name for name in table_changes if name in SESSION_RESERVATION_TABLES
        ],
        "protectedTableChanges": protected_changes,
        "satisfied": database_preserved and schema_preserved and not protected_changes,
    }


def _cell_record(binary: str, source: Path, cell: Phase4Cell, sample: int) -> dict[str, Any]:
    before = index_fingerprints(source)
    table_facts_before = (
        _sqlite_table_facts(source)
        if cell.index_mutation_policy in {"session-dedupe", "preserve"}
        else None
    )
    corpus_before = (
        _non_runtime_corpus_sha256(source)
        if cell.index_mutation_policy == "preserve"
        else None
    )
    result = subprocess.run(
        cell.argv(binary), cwd=source, text=True, capture_output=True, check=False,
        env={**os.environ, "RZM_SKIP_REPO_DELEGATE": "1"},
    )
    after = index_fingerprints(source)
    table_facts_after = (
        _sqlite_table_facts(source)
        if table_facts_before is not None
        else None
    )
    session_invariant = (
        _session_reservation_invariant(table_facts_before, table_facts_after)
        if table_facts_before is not None and table_facts_after is not None
        else None
    )
    corpus_after = (
        _non_runtime_corpus_sha256(source)
        if corpus_before is not None
        else None
    )
    stdout_json, normalized_stdout_text, normalized_stdout = _parse_channel(result.stdout, source)
    stderr_json, normalized_stderr_text, normalized_stderr = _parse_channel(result.stderr, source)
    payloads = [value for value in (stdout_json, stderr_json) if value is not None]
    changed = [
        label for label in ("db", "wal", "shm")
        if before[label] != after[label]
    ]
    before_db = before["db"]
    after_db = after["db"]
    if before_db["present"]:
        query_only_invariant = (
            after_db["present"]
            and before_db["sha256"] == after_db["sha256"]
            and before_db["sizeBytes"] == after_db["sizeBytes"]
        )
    else:
        query_only_invariant = not any(after[label]["present"] for label in ("db", "wal", "shm"))
    return {
        "sample": sample,
        "argv": cell.argv(binary),
        "exitCode": result.returncode,
        "stdoutSha256": sha256_text(result.stdout),
        "stderrSha256": sha256_text(result.stderr),
        "normalizedStdout": normalized_stdout_text,
        "normalizedStderr": normalized_stderr_text,
        "normalizedStdoutSha256": normalized_stdout,
        "normalizedStderrSha256": normalized_stderr,
        "stdoutBytes": len(result.stdout.encode()),
        "stderrBytes": len(result.stderr.encode()),
        "jsonShapes": [_json_shape(value) for value in payloads],
        "warningRemediation": [record for value in payloads for record in _warning_remediation(value)],
        "indexBefore": before,
        "indexAfter": after,
        "indexUnchanged": before == after,
        "sidecarMutationLabels": changed,
        "queryOnlyInvariant": query_only_invariant,
        "sqliteTableFactsBefore": table_facts_before,
        "sqliteTableFactsAfter": table_facts_after,
        "sessionReservationInvariant": session_invariant,
        "rootTwinMutationInvariant": (
            {
                **session_invariant,
                "corpusPreserved": corpus_before == corpus_after,
            }
            if cell.index_mutation_policy == "preserve" and session_invariant is not None
            else None
        ),
    }


def _record_identity(record: dict[str, Any], index_mutation_policy: str) -> dict[str, Any]:
    identity = {
        key: record[key]
        for key in (
            "exitCode", "normalizedStdoutSha256", "normalizedStderrSha256",
            "jsonShapes", "warningRemediation",
        )
    }
    if index_mutation_policy == "query-only":
        # SQLite may coordinate a read-only shared-cache query through WAL/SHM.
        # The DB itself must remain byte-identical, and a missing state must
        # remain wholly missing; only the sidecars are intentionally excluded.
        identity["queryOnlyInvariant"] = record["queryOnlyInvariant"]
    elif index_mutation_policy == "session-dedupe":
        identity["sessionReservationInvariant"] = record["sessionReservationInvariant"]
    elif index_mutation_policy == "preserve":
        root = record["rootTwinMutationInvariant"]
        identity["rootTwinMutationTransition"] = {
            key: root[key]
            for key in (
                "databasePreserved", "sessionTableChanges", "protectedTableChanges", "corpusPreserved"
            )
        }
    else:
        identity["indexUnchanged"] = record["indexUnchanged"]
        identity["sidecarMutationLabels"] = record["sidecarMutationLabels"]
    return identity


def _is_provider_failure(record: dict[str, Any]) -> bool:
    """Distinguish an unavailable provider from a metadata/index mismatch."""
    text = (record["normalizedStdout"] + "\n" + record["normalizedStderr"]).lower()
    if not any(token in text for token in (
        "provider", "embedding provider", "provider_unavailable",
        "embed input", "api/embed", "dial tcp", "connection refused",
    )):
        return False
    return not any(token in text for token in (
        "metadata", "schema mismatch", "index incompatible", "dimension mismatch", "model mismatch",
    ))


def _fixture_content_sha256(source: Path) -> str:
    """Hash corpus inputs, excluding prepared runtime state and its config knob."""
    digest = hashlib.sha256()
    for path in sorted(source.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(source).as_posix()
        if relative == ".rhizome/config.yml" or relative.startswith(".rhizome/db.sqlite"):
            continue
        encoded = relative.encode()
        content = path.read_bytes()
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def _non_runtime_corpus_sha256(source: Path) -> str:
    """Hash user corpus inputs while excluding all runtime-owned state."""
    digest = hashlib.sha256()
    for path in sorted(source.rglob("*")):
        if not path.is_file() or ".rhizome" in path.parts:
            continue
        relative = path.relative_to(source).as_posix()
        encoded = relative.encode()
        content = path.read_bytes()
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def _embedding_providers(config: Path) -> dict[str, str]:
    """Read the configured note and code provider without a YAML dependency."""
    if not config.is_file():
        raise ValueError(f"fixture config missing: {config}")
    providers: dict[str, str] = {}
    domain = None
    domains = {"noteEmbeddings:": "note", "codeEmbeddings:": "code"}
    for line in config.read_text().splitlines():
        if line and not line[0].isspace():
            domain = domains.get(line.strip())
            continue
        stripped = line.strip()
        if domain and stripped.startswith("provider:"):
            providers[domain] = stripped.split(":", 1)[1].strip()
    if set(providers) != {"note", "code"} or not all(providers.values()):
        raise ValueError(f"fixture config must set note and code embedding providers: {config}")
    return providers


def _embedding_metadata(
    connection: sqlite3.Connection, tables: set[str], table: str
) -> dict[str, Any]:
    if table not in tables:
        return {"present": False}
    row = connection.execute(
        f"SELECT provider, model, dimensions, schema_version FROM {table} WHERE id=1"
    ).fetchone()
    if row is None:
        return {"present": False}
    return {
        "present": True,
        "provider": row[0],
        "model": row[1],
        "dimensions": row[2],
        "schemaVersion": row[3],
    }


def _is_legacy_unified_note_metadata(metadata: dict[str, Any]) -> bool:
    """Recognize absent or wholly blank historical note metadata, and nothing else."""
    return not metadata.get("present") or (
        metadata.get("provider") in (None, "")
        and metadata.get("model") in (None, "")
        and metadata.get("dimensions") in (None, 0)
    )


def _legacy_note_vector_proof(
    connection: sqlite3.Connection, tables: set[str], expected_dimensions: Any
) -> dict[str, Any]:
    """Prove blank note metadata has a usable expected-dimension stored vector.

    Callers hold the immutable fixture connection. The table name is built only
    after strict integer validation, then the row must join through both index
    tables. A provider match alone is not sufficient proof of stored evidence.
    """
    if not isinstance(expected_dimensions, int) or expected_dimensions <= 0:
        return {"expectedDimensions": expected_dimensions, "tablePresent": False, "matchingRow": False}
    table = f"intel_embeddings_vec_d{expected_dimensions}"
    if table not in tables or not {"intel_embeddings", "intel_chunks"} <= tables:
        return {"expectedDimensions": expected_dimensions, "tablePresent": table in tables, "matchingRow": False}
    statement = f"""
        SELECT EXISTS(
            SELECT 1
            FROM intel_embeddings AS embedding
            JOIN intel_chunks AS chunk ON chunk.id = embedding.chunk_row_id
            JOIN {{vector_table}} AS vector ON vector.chunk_id = embedding.chunk_row_id
            WHERE embedding.dimensions = ?
        )
    """
    proof_source = "vector-table"
    try:
        matching_row = bool(connection.execute(
            statement.format(vector_table=table), (expected_dimensions,)
        ).fetchone()[0])
    except sqlite3.OperationalError as exc:
        # Python's bundled SQLite cannot load Rhizome's vec0 module. SQLite-vec
        # persists each vec0 row identity in this deterministic shadow table, so
        # use it only for that missing-module case; any other read error is proof
        # failure rather than a fallback.
        if str(exc) != "no such module: vec0":
            raise ValueError(f"fixture note vector proof cannot be read: {exc}") from exc
        shadow_table = f"{table}_rowids"
        if shadow_table not in tables:
            return {"expectedDimensions": expected_dimensions, "tablePresent": True, "matchingRow": False}
        matching_row = bool(connection.execute(
            statement.format(vector_table=shadow_table), (expected_dimensions,)
        ).fetchone()[0])
        proof_source = "vec0-rowids-shadow"
    except sqlite3.Error as exc:
        raise ValueError(f"fixture note vector proof cannot be read: {exc}") from exc
    return {
        "expectedDimensions": expected_dimensions,
        "tablePresent": True,
        "matchingRow": matching_row,
        "proofSource": proof_source,
    }


def _validate_embedding_metadata(
    database: dict[str, Any], providers: dict[str, str]
) -> None:
    code = database.get("codeEmbeddingMetadata", {})
    if not code.get("present"):
        raise ValueError("find-connections current fixture lacks code embedding metadata")
    if code.get("provider") != providers["code"]:
        raise ValueError("code embedding metadata provider does not match code provider config")
    if not isinstance(code.get("dimensions"), int) or code["dimensions"] <= 0:
        raise ValueError("find-connections current fixture lacks valid code embedding dimensions")
    note = database.get("noteEmbeddingMetadata", {})
    if _is_legacy_unified_note_metadata(note):
        if providers["note"] != providers["code"]:
            raise ValueError("legacy unified note metadata requires matching note and code providers")
        if not database.get("legacyNoteVectorProof", {}).get("matchingRow"):
            raise ValueError("legacy unified note metadata requires compatible legacy note vectors")
        return
    if not note.get("present") or not note.get("provider") or not note.get("model") or not isinstance(note.get("dimensions"), int) or note["dimensions"] <= 0:
        raise ValueError("note embedding metadata is partially populated; use complete metadata or the legacy unified blank row")
    if note["provider"] != providers["note"]:
        raise ValueError("note embedding metadata provider does not match note provider config")


def _database_facts(source: Path) -> dict[str, Any]:
    database = source / ".rhizome" / "db.sqlite"
    facts: dict[str, Any] = {"databasePresent": database.is_file(), "index": index_fingerprints(source)}
    if not database.is_file():
        return facts
    try:
        # `immutable=1` prevents this validator from creating a WAL/SHM sidecar
        # merely by inspecting a fixture that is meant to prove no-write behavior.
        connection = sqlite3.connect(f"file:{database}?mode=ro&immutable=1", uri=True)
        try:
            tables = {row[0] for row in connection.execute("SELECT name FROM sqlite_master WHERE type='table'")}
            if "rzm_migration_state" not in tables or "index_metadata" not in tables:
                raise ValueError(f"fixture database lacks Rhizome state tables: {database}")
            row = connection.execute("SELECT version FROM rzm_migration_state WHERE domain='intel'").fetchone()
            metadata = dict(connection.execute("SELECT key, value FROM index_metadata WHERE key IN ('indexer_version', 'scope_config_hash')"))
            semantic_chunks = 0
            note_sections = 0
            if {"intel_chunks", "intel_embeddings"} <= tables:
                semantic_chunks = connection.execute(
                    "SELECT COUNT(*) FROM intel_embeddings"
                ).fetchone()[0]
            if "intel_doc_sections" in tables:
                note_sections = connection.execute(
                    "SELECT COUNT(*) FROM intel_doc_sections"
                ).fetchone()[0]
            code_embedding_metadata = _embedding_metadata(connection, tables, "code_index_meta")
            note_embedding_metadata = _embedding_metadata(connection, tables, "emb_index_meta")
            legacy_note_vector_proof = _legacy_note_vector_proof(
                connection, tables, code_embedding_metadata.get("dimensions")
            )
        finally:
            connection.close()
    except sqlite3.Error as exc:
        raise ValueError(f"fixture SQLite state cannot be read: {database}: {exc}") from exc
    if row is None or not isinstance(row[0], int):
        raise ValueError(f"fixture has no Intel schema state: {database}")
    facts.update({
        "intelSchemaVersion": row[0],
        "metadata": metadata,
        "semanticChunkCount": semantic_chunks,
        "noteSectionCount": note_sections,
        "codeEmbeddingMetadata": code_embedding_metadata,
        "noteEmbeddingMetadata": note_embedding_metadata,
        "legacyNoteVectorProof": legacy_note_vector_proof,
    })
    return facts


def validate_fixture_states(
    fixtures: dict[str, Path], *, require_semantic_chunks: bool = False
) -> dict[str, dict[str, Any]]:
    """Prove state labels from the actual config and SQLite data before a run."""
    supplied = set(fixtures)
    missing = set(REQUIRED_STATES) - supplied
    if missing:
        raise ValueError(f"missing fixture states: {', '.join(sorted(missing))}")
    manifests: dict[str, dict[str, Any]] = {}
    for state, source in fixtures.items():
        if not source.is_dir():
            raise ValueError(f"fixture for {state} is not a directory: {source}")
        manifests[state] = {
            "corpusSha256": _fixture_content_sha256(source),
            "embeddingProviders": _embedding_providers(source / ".rhizome" / "config.yml"),
            "database": _database_facts(source),
        }
    if len({manifest["corpusSha256"] for manifest in manifests.values()}) != 1:
        raise ValueError("fixture states do not have equivalent non-runtime corpus content")
    current = manifests["current"]
    # Missing, stale, and incompatible model index state only. They must not
    # also change embedding providers, or the comparison has two variables.
    for state in ("missing", "stale", "incompatible"):
        if manifests[state]["embeddingProviders"] != current["embeddingProviders"]:
            raise ValueError(f"{state} fixture must retain the current provider configuration")
    current_database = current["database"]
    if not current_database["databasePresent"]:
        raise ValueError("current fixture must contain a real SQLite index")
    current_schema = current_database["intelSchemaVersion"]
    current_metadata = current_database["metadata"]
    if not current_metadata.get("indexer_version") or not current_metadata.get("scope_config_hash"):
        raise ValueError("current fixture must contain index freshness metadata")
    if require_semantic_chunks:
        if current_database.get("semanticChunkCount", 0) <= 0 or current_database.get("noteSectionCount", 0) <= 0:
            raise ValueError("find-connections evidence requires stored note semantic chunks and sections")
        _validate_embedding_metadata(current_database, current["embeddingProviders"])
    missing_database = manifests["missing"]["database"]
    if missing_database["databasePresent"] or any(missing_database["index"][name]["present"] for name in ("wal", "shm")):
        raise ValueError("missing fixture must not contain db.sqlite, WAL, or SHM")
    stale = manifests["stale"]["database"]
    if not stale["databasePresent"] or stale.get("intelSchemaVersion") != current_schema:
        raise ValueError("stale fixture must retain the current Intel schema")
    if stale.get("metadata") == current_metadata:
        raise ValueError("stale fixture must change index freshness metadata")
    incompatible = manifests["incompatible"]["database"]
    if not incompatible["databasePresent"] or incompatible.get("intelSchemaVersion", 0) <= current_schema:
        raise ValueError("incompatible fixture must use a future Intel schema")
    if manifests["provider-unavailable"]["embeddingProviders"] == current["embeddingProviders"]:
        raise ValueError("provider-unavailable fixture must use a different provider configuration")
    unavailable_database = manifests["provider-unavailable"]["database"]
    current_index = current_database["index"]["db"]
    unavailable_index = unavailable_database["index"]["db"]
    if (
        not unavailable_index["present"]
        or unavailable_index["sha256"] != current_index["sha256"]
        or unavailable_index["sizeBytes"] != current_index["sizeBytes"]
    ):
        raise ValueError("provider-unavailable fixture must retain a byte-identical current database")
    return manifests


def run_phase4_matrix(
    baseline_binary: str,
    current_binary: str,
    fixtures: dict[str, Path],
    *,
    samples: int = 1,
    cells: Iterable[Phase4Cell] | None = None,
    authorized_differences: Iterable[str] = (),
) -> dict[str, Any]:
    """Run a baseline/current matrix with a new fixture copy for every cell.

    Fixture state directories must be prepared by the caller.  This keeps
    missing, stale, incompatible, and provider-unavailable construction out
    of the measured command path and prevents personal vault input.
    """
    if samples < 1:
        raise ValueError("samples must be at least 1")
    selected = tuple(cells or phase4_cells())
    authorized = set(authorized_differences)
    fixture_facts = validate_fixture_states(
        fixtures,
        require_semantic_chunks=any(cell.name.startswith("find-connections-") for cell in selected),
    )
    results: dict[str, Any] = {
        "contract": "phase4-parity-v1",
        "cellIsolation": "fresh-copy-per-binary-state-command-sample",
        "equivalence": {
            "compares": "exit code plus canonical exact stdout/stderr channels, JSON key/list order, warning/remediation records, and SQLite mutation policy",
            "canonicalization": "only fixture-root paths and documented volatile keys are removed; raw channel hashes remain forensic evidence",
            "queryOnlySQLite": "allows WAL/SHM coordination only when db.sqlite stays byte-identical; missing state remains DB/WAL/SHM absent",
            "sessionDedupeSQLite": "may change only mcp_session reservation tables; database presence and all migrations, metadata, index content, and projections stay unchanged",
            "rootTwinSQLite": "must preserve exact baseline table-transition classes and non-runtime corpus behavior; it is not a blanket mutation exception",
        },
        "binaries": {"baseline": file_fingerprint(Path(baseline_binary)), "current": file_fingerprint(Path(current_binary))},
        "fixtureStates": fixture_facts,
        "authorizedDifferenceMappings": sorted(authorized),
        "cells": {},
        "differences": [],
        "authorizedDifferences": [],
        "unexplainedDifferences": [],
        "unexpectedCurrentIndexMutations": [],
        "providerUnavailableObservations": [],
        "evidenceGaps": [],
    }
    with tempfile.TemporaryDirectory(prefix="rzm-phase4-parity-") as temp_dir:
        root = Path(temp_dir)
        for cell in selected:
            per_cell: dict[str, Any] = {}
            for state in cell.states:
                if state not in fixtures:
                    raise ValueError(f"{cell.name} needs unavailable fixture state {state}")
                per_state: dict[str, list[dict[str, Any]]] = {}
                for label, binary in (("baseline", baseline_binary), ("current", current_binary)):
                    records: list[dict[str, Any]] = []
                    for sample in range(samples):
                        copy = root / cell.name / state / label / str(sample)
                        shutil.copytree(fixtures[state], copy)
                        records.append(_cell_record(binary, copy, cell, sample))
                    per_state[label] = records
                baseline_identity = [
                    _record_identity(record, cell.index_mutation_policy)
                    for record in per_state["baseline"]
                ]
                current_identity = [
                    _record_identity(record, cell.index_mutation_policy)
                    for record in per_state["current"]
                ]
                equivalent = baseline_identity == current_identity
                root_twin_contract = None
                if cell.index_mutation_policy == "preserve":
                    baseline_transitions = [identity["rootTwinMutationTransition"] for identity in baseline_identity]
                    current_transitions = [identity["rootTwinMutationTransition"] for identity in current_identity]
                    root_twin_contract = {
                        "contract": "exact-baseline-table-transition-and-corpus-parity",
                        "baseline": baseline_transitions,
                        "current": current_transitions,
                        "satisfied": baseline_transitions == current_transitions,
                    }
                    if not root_twin_contract["satisfied"]:
                        results["unexpectedCurrentIndexMutations"].append({
                            "cell": cell.name,
                            "state": state,
                            "policy": cell.index_mutation_policy,
                            "baseline": baseline_transitions,
                            "current": current_transitions,
                        })
                if not equivalent:
                    difference = {"cell": cell.name, "state": state, "baseline": baseline_identity, "current": current_identity}
                    results["differences"].append(difference)
                    if f"{cell.name}:{state}" in authorized:
                        results["authorizedDifferences"].append(difference)
                    else:
                        results["unexplainedDifferences"].append(difference)
                for record in per_state["current"]:
                    if cell.index_mutation_policy == "forbid" and not record["indexUnchanged"]:
                        results["unexpectedCurrentIndexMutations"].append({
                            "cell": cell.name,
                            "state": state,
                            "sample": record["sample"],
                            "policy": cell.index_mutation_policy,
                            "sidecarMutationLabels": record["sidecarMutationLabels"],
                        })
                    if cell.index_mutation_policy == "query-only" and not record["queryOnlyInvariant"]:
                        results["unexpectedCurrentIndexMutations"].append({
                            "cell": cell.name,
                            "state": state,
                            "sample": record["sample"],
                            "policy": cell.index_mutation_policy,
                            "sidecarMutationLabels": record["sidecarMutationLabels"],
                        })
                    if cell.index_mutation_policy == "session-dedupe" and not record["sessionReservationInvariant"]["satisfied"]:
                        results["unexpectedCurrentIndexMutations"].append({
                            "cell": cell.name,
                            "state": state,
                            "sample": record["sample"],
                            "policy": cell.index_mutation_policy,
                            "protectedTableChanges": record["sessionReservationInvariant"]["protectedTableChanges"],
                            "databasePreserved": record["sessionReservationInvariant"]["databasePreserved"],
                            "schemaPreserved": record["sessionReservationInvariant"]["schemaPreserved"],
                        })
                if state == "provider-unavailable":
                    for label, records in per_state.items():
                        for record in records:
                            provider_failure = _is_provider_failure(record)
                            results["providerUnavailableObservations"].append({
                                "cell": cell.name,
                                "binary": label,
                                "sample": record["sample"],
                                "exitCode": record["exitCode"],
                                "warningRemediation": record["warningRemediation"],
                                "observedFailure": record["exitCode"] != 0 or bool(record["warningRemediation"]),
                                "providerFailure": provider_failure,
                            })
                per_cell[state] = {
                    **per_state,
                    "equivalent": equivalent,
                    "indexMutationPolicy": cell.index_mutation_policy,
                    "rootTwinMutationContract": root_twin_contract,
                }
            results["cells"][cell.name] = per_cell
    if results["providerUnavailableObservations"] and not any(
        observation["providerFailure"] for observation in results["providerUnavailableObservations"]
    ):
        results["evidenceGaps"].append(
            "provider-unavailable configuration differed but no selected command proved a provider failure"
        )
    results["equivalent"] = not (
        results["unexplainedDifferences"]
        or results["unexpectedCurrentIndexMutations"]
        or results["evidenceGaps"]
    )
    return results


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-rzm", required=True)
    parser.add_argument("--current-rzm", required=True)
    parser.add_argument("--fixture", action="append", required=True, metavar="STATE=DIR")
    parser.add_argument("--cell", action="append", help="run only this declared Phase 4 cell (repeatable)")
    parser.add_argument(
        "--authorize-difference", action="append", default=[], metavar="CELL:STATE",
        help="existing-plan-approved parity difference; unexplained differences still fail",
    )
    parser.add_argument("--samples", type=int, default=1)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    fixtures: dict[str, Path] = {}
    for raw in args.fixture:
        state, separator, path = raw.partition("=")
        if not separator or not state or not path:
            parser.error("--fixture must be STATE=DIR")
        fixtures[state] = Path(path).resolve()
    declared = {cell.name: cell for cell in phase4_cells()}
    selected = None
    if args.cell:
        unknown = sorted(set(args.cell) - set(declared))
        if unknown:
            parser.error(f"unknown Phase 4 cell: {', '.join(unknown)}")
        selected = tuple(declared[name] for name in args.cell)
    invalid_authorizations = [item for item in args.authorize_difference if item.count(":") != 1]
    if invalid_authorizations:
        parser.error("--authorize-difference must be CELL:STATE")
    try:
        report = run_phase4_matrix(
            args.baseline_rzm,
            args.current_rzm,
            fixtures,
            samples=args.samples,
            cells=selected,
            authorized_differences=args.authorize_difference,
        )
    except (ValueError, OSError) as exc:
        parser.error(str(exc))
    rendered = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.write_text(rendered)
    else:
        print(rendered, end="")
    return 0 if report["equivalent"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
