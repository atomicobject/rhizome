"""State, normalization, and SQLite evidence helpers for the Phase 5 matrix."""

from __future__ import annotations

import json
import math
import platform
import re
import shutil
import sqlite3
import statistics
import subprocess
import tempfile
from pathlib import Path
from typing import Any

from benchmark_evidence import sha256_file, sha256_text


VOLATILE_KEYS = {
    "duration", "durationMs", "elapsed", "elapsedMs", "finishedAt", "remainingMs",
    "sessionId", "startedAt", "timings", "timestamp", "updatedAt",
}
LOG_TIMESTAMP_RE = re.compile(r"\b\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\b")
APPROVED_VOLATILE_SQLITE_COLUMNS = {
    ("notes", "indexed_at"), ("property_keys", "updated_at"),
    ("note_metadata_state", "loaded_at"), ("ontology_schema_state", "loaded_at"),
    ("ontology_edges", "updated_at"), ("ontology_node_field_value_dependencies", "updated_at"),
    ("ontology_node_field_values", "updated_at"), ("ontology_nodes", "updated_at"),
    ("ontology_note_assessments", "updated_at"), ("ontology_note_state", "updated_at"),
    ("ontology_note_types", "updated_at"),
}
REQUIRED_RHIZOME_PROJECTION_INPUTS = (
    ".rhizome/config.yml", ".rhizome/ignore", ".rhizome/ontology",
    ".rhizome/query-recipes", ".rhizome/views",
)
WORKTREE_PROJECTION_EXCLUDED_ROOTS = ("web/node_modules", ".gocache")


def percentile(values: list[float], fraction: float) -> float:
    ordered = sorted(values)
    return ordered[max(0, math.ceil(len(ordered) * fraction) - 1)]


def normalized(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: normalized(item) for key, item in sorted(value.items()) if key not in VOLATILE_KEYS}
    if isinstance(value, list):
        return [normalized(item) for item in value]
    return value


def normalize_text(value: str, roots: tuple[Path, ...], *, parse_json: bool) -> str:
    if parse_json and value.strip():
        value = json.dumps(normalized(json.loads(value)), sort_keys=True, separators=(",", ":"))
    for root in sorted((str(root) for root in roots), key=len, reverse=True):
        value = value.replace(root, "$VAULT")
    return LOG_TIMESTAMP_RE.sub("$TIMESTAMP", value)


def tree_manifest(root: Path) -> dict[str, str]:
    return {
        path.relative_to(root).as_posix(): sha256_file(path)
        for path in sorted(root.rglob("*")) if path.is_file()
    }


def mutation(before: dict[str, str], after: dict[str, str]) -> dict[str, list[str]]:
    return {
        "created": sorted(set(after) - set(before)),
        "deleted": sorted(set(before) - set(after)),
        "changed": sorted(path for path in before.keys() & after.keys() if before[path] != after[path]),
    }


def filesystem_mutation(before: dict[str, str], after: dict[str, str], sqlite_delta: dict[str, list[str]]) -> dict[str, list[str]]:
    result = mutation(before, after)
    sqlite_paths = {
        ".rhizome/db.sqlite": "db", ".rhizome/db.sqlite-wal": "wal", ".rhizome/db.sqlite-shm": "shm",
    }
    for action in ("created", "deleted", "changed"):
        result[action] = [path for path in result[action] if path not in sqlite_paths]
        result[action].extend(path for path, label in sqlite_paths.items() if label in sqlite_delta[action])
        result[action].sort()
    return result


def copy_state(source: Path, destination: Path) -> None:
    if destination.exists():
        raise RuntimeError(f"destination already exists: {destination}")
    if platform.system() == "Darwin":
        result = subprocess.run(["cp", "-cR", str(source), str(destination)], text=True, capture_output=True, check=False)
        if result.returncode == 0:
            return
    shutil.copytree(source, destination)


def relative_git_paths(source: Path, arguments: list[str]) -> list[Path]:
    result = subprocess.run(["/usr/bin/git", *arguments, "-z"], cwd=source, text=False, capture_output=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"could not list projection files: {result.stderr.decode().strip()}")
    paths = [Path(value.decode()) for value in result.stdout.split(b"\0") if value]
    if any(path.is_absolute() or ".." in path.parts for path in paths):
        raise RuntimeError("Git returned a path outside the benchmark source")
    return paths


def create_rhizome_projection(
    source: Path, destination: Path,
    excluded_roots: tuple[str, ...] = WORKTREE_PROJECTION_EXCLUDED_ROOTS,
) -> dict[str, Any]:
    if destination.exists():
        raise RuntimeError(f"projection destination already exists: {destination}")
    tracked = set(relative_git_paths(source, ["ls-files", "--cached"]))
    excluded_counts: dict[str, int] = {}
    for root in excluded_roots:
        root_path = Path(root)
        tracked_under_root = sorted(path for path in tracked if path == root_path or root_path in path.parents)
        if tracked_under_root:
            raise RuntimeError(f"projection cannot exclude tracked root {root}: {tracked_under_root[0]}")
        excluded_counts[root] = sum(1 for path in (source / root).rglob("*") if path.is_file()) if (source / root).exists() else 0
    listed = relative_git_paths(source, ["ls-files", "--cached", "--others", "--exclude-standard"])
    excluded_paths = tuple(Path(root) for root in excluded_roots)
    included = [
        path for path in listed
        if not any(path == root or root in path.parents for root in excluded_paths)
    ]
    destination.mkdir(parents=True)
    for relative in included:
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source / relative, target, follow_symlinks=False)
    missing = [path for path in REQUIRED_RHIZOME_PROJECTION_INPUTS if not (destination / path).exists()]
    if missing:
        raise RuntimeError(f"projection is missing required Rhizome inputs: {', '.join(missing)}")
    manifest = tree_manifest(destination)
    return {
        "kind": "git-tracked-and-unignored", "includedFileCount": len(included),
        "excludedRoots": excluded_counts,
        "retainedTrackedRoots": {".tmp": sum(1 for path in tracked if Path(".tmp") in path.parents)},
        "manifestSha256": sha256_text(json.dumps(manifest, sort_keys=True)),
    }


def fingerprint_delta(before: dict[str, dict[str, Any]], after: dict[str, dict[str, Any]]) -> dict[str, list[str]]:
    changed, created, deleted = [], [], []
    for name in sorted(set(before) | set(after)):
        prior, current = before.get(name, {"present": False}), after.get(name, {"present": False})
        if not prior.get("present") and current.get("present"):
            created.append(name)
        elif prior.get("present") and not current.get("present"):
            deleted.append(name)
        elif ({key: value for key, value in prior.items() if key != "path"} !=
              {key: value for key, value in current.items() if key != "path"}):
            changed.append(name)
    return {"created": created, "deleted": deleted, "changed": changed}


def fingerprint_content_identity(fingerprints: dict[str, dict[str, Any]]) -> dict[str, dict[str, Any]]:
    return {name: {key: value for key, value in fact.items() if key != "path"} for name, fact in fingerprints.items()}


def canonical_sqlite_snapshot(path: Path, volatile_columns: set[tuple[str, str]]) -> dict[str, Any]:
    with tempfile.TemporaryDirectory(prefix="rzm-sqlite-snapshot-") as temporary:
        snapshot_path = Path(temporary) / path.name
        shutil.copy2(path, snapshot_path)
        for suffix in ("-wal", "-shm"):
            sidecar = Path(f"{path}{suffix}")
            if sidecar.exists():
                shutil.copy2(sidecar, Path(f"{snapshot_path}{suffix}"))
        connection = sqlite3.connect(f"file:{snapshot_path}?mode=ro", uri=True)
        try:
            schema = list(connection.execute("SELECT type, name, tbl_name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type, name"))
            tables = [row[0] for row in connection.execute(
                "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' "
                "AND sql NOT LIKE 'CREATE VIRTUAL TABLE%' ORDER BY name"
            )]
            contents: dict[str, Any] = {}
            for table in tables:
                columns = [row[1] for row in connection.execute(f'PRAGMA table_info("{table}")')]
                order = ", ".join(f'"{column}"' for column in columns)
                rows = [["<volatile>" if (table, columns[index]) in volatile_columns else value
                         for index, value in enumerate(row)]
                        for row in connection.execute(f'SELECT * FROM "{table}" ORDER BY {order}')]
                contents[table] = {"columns": columns, "rows": rows}
            return {"schema": schema, "tables": contents}
        finally:
            connection.close()


def sqlite_snapshot_diff_columns(left: dict[str, Any], right: dict[str, Any]) -> set[tuple[str, str]]:
    changed: set[tuple[str, str]] = set()
    for table in set(left["tables"]) | set(right["tables"]):
        a, b = left["tables"].get(table), right["tables"].get(table)
        if a is None or b is None or a["columns"] != b["columns"]:
            changed.add((table, "<schema-or-table>"))
            continue
        for index, column in enumerate(a["columns"]):
            if [row[index] for row in a["rows"]] != [row[index] for row in b["rows"]]:
                changed.add((table, column))
    return changed


def summarize(samples: list[dict[str, Any]]) -> dict[str, Any]:
    durations = [sample["elapsedMs"] for sample in samples]
    normalized_stdout = [sample["normalizedStdoutSha256"] for sample in samples]
    normalized_stderr = [sample["normalizedStderrSha256"] for sample in samples]
    full_stdout = [sample["fullStdoutSha256"] for sample in samples]
    full_stderr = [sample["fullStderrSha256"] for sample in samples]
    mutations = [sample["filesystemMutation"] for sample in samples]
    return {
        "samples": len(samples), "durationsMs": durations,
        "medianMs": round(statistics.median(durations), 3), "p95Ms": round(percentile(durations, 0.95), 3),
        "varianceMs2": round(statistics.pvariance(durations), 3),
        "exitCodes": [sample["exitCode"] for sample in samples],
        "normalizedStdoutSha256s": normalized_stdout, "normalizedStderrSha256s": normalized_stderr,
        "fullStdoutSha256s": full_stdout, "fullStderrSha256s": full_stderr,
        "normalizedOutputEquivalent": len(set(normalized_stdout)) == 1 and len(set(normalized_stderr)) == 1,
        "fullOutputEquivalent": len(set(full_stdout)) == 1 and len(set(full_stderr)) == 1,
        "operationEnvelopes": [sample["operationEnvelope"] for sample in samples],
        "operationDiagnosticsAvailable": [sample["operationDiagnosticsAvailable"] for sample in samples],
        "indexFingerprintsBefore": [sample["indexFingerprintsBefore"] for sample in samples],
        "indexFingerprintsAfter": [sample["indexFingerprintsAfter"] for sample in samples],
        "filesystemMutations": mutations, "sqliteSidecarDeltas": [sample["sqliteSidecarDelta"] for sample in samples],
        "canonicalLogicalSha256s": [sample["canonicalLogicalSha256"] for sample in samples],
        "filesystemMutationEquivalent": len({json.dumps(item, sort_keys=True) for item in mutations}) == 1,
    }
