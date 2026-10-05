"""Shared snapshot and deterministic-check helpers for synthetic fixtures."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any, Iterable, Mapping


_REPO_ROOT = Path(__file__).resolve().parents[2]
_FIXTURE_ROOT = _REPO_ROOT / "testdata" / "agent-experience"


# Derived SQLite state is captured as evidence but is not an authored deliverable.
# Exact paths keep config, ontology, guidance, and similarly named files protected.
RUNTIME_PATHS = frozenset({
    ".rhizome/db.sqlite",
    ".rhizome/db.sqlite-wal",
    ".rhizome/db.sqlite-shm",
    ".rhizome/db.sqlite-journal",
})


_REGISTRY_PATH = _FIXTURE_ROOT / "registry.json"
_REGISTRY = json.loads(_REGISTRY_PATH.read_text())
if _REGISTRY.get("version") != 1:
    raise ValueError("unsupported fixture registry version")
CASES: dict[str, dict[str, Any]] = {case["case_id"]: case for case in _REGISTRY["cases"]}


def _source_directory(case: Mapping[str, Any]) -> Path:
    return _FIXTURE_ROOT / str(case["relative_path"])


def _safe_relative(path: Path, root: Path) -> str:
    try:
        return path.resolve().relative_to(root.resolve()).as_posix()
    except ValueError as exc:
        raise ValueError(f"path escapes fixture destination: {path}") from exc


def _iter_files(root: Path) -> Iterable[Path]:
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"symlinks are not allowed in synthetic fixtures: {path}")
        if path.is_file():
            yield path


def snapshot(destination: Path) -> dict[str, Any]:
    """Return a compact, content-bearing snapshot suitable for ``check``.

    The runner may use its own before/after format.  This helper is intentionally
    public so tests and adapters can produce a compatible snapshot without
    importing implementation details from the runner.
    """

    destination = Path(destination)
    files: dict[str, dict[str, Any]] = {}
    for path in _iter_files(destination):
        relative = _safe_relative(path, destination)
        data = path.read_bytes()
        files[relative] = {
            "sha256": hashlib.sha256(data).hexdigest(),
            "size": len(data),
            "content": data.decode("utf-8") if _is_utf8_text(data) else None,
        }
    return {"root": str(destination), "files": files}


def _is_utf8_text(data: bytes) -> bool:
    if b"\x00" in data:
        return False
    try:
        data.decode("utf-8")
    except UnicodeDecodeError:
        return False
    return True


def _before_file_map(before: Mapping[str, Any] | None) -> Mapping[str, Any]:
    if not isinstance(before, Mapping):
        return {}
    files = before.get("files")
    if isinstance(files, Mapping):
        return files
    # Accept a flat path -> record map from small adapters.
    if all(isinstance(key, str) for key in before):
        return before
    return {}


def _before_content(before: Mapping[str, Any] | None, relative: str) -> bytes | None:
    record = _before_file_map(before).get(relative)
    if isinstance(record, Mapping):
        value = record.get("content")
    else:
        value = record
    if isinstance(value, bytes):
        return value
    if isinstance(value, str):
        return value.encode("utf-8")
    return None


def _before_sha256(before: Mapping[str, Any] | None, relative: str) -> str | None:
    record = _before_file_map(before).get(relative)
    if isinstance(record, Mapping) and isinstance(record.get("sha256"), str):
        return record["sha256"]
    return None


def _before_paths(before: Mapping[str, Any] | None) -> set[str]:
    return {str(path) for path in _before_file_map(before)}


def _before_hashes(before: Mapping[str, Any] | None) -> dict[str, str]:
    hashes: dict[str, str] = {}
    for relative, record in _before_file_map(before).items():
        if isinstance(record, Mapping) and isinstance(record.get("sha256"), str):
            hashes[str(relative)] = record["sha256"]
            continue
        content = record.get("content") if isinstance(record, Mapping) else record
        if isinstance(content, bytes):
            hashes[str(relative)] = hashlib.sha256(content).hexdigest()
        elif isinstance(content, str):
            hashes[str(relative)] = hashlib.sha256(content.encode("utf-8")).hexdigest()
    return hashes


def _current_hashes(destination: Path) -> dict[str, str]:
    hashes: dict[str, str] = {}
    for path in _iter_files(destination):
        relative = _safe_relative(path, destination)
        if relative == ".git" or relative.startswith(".git/") or _is_transient(relative):
            continue
        hashes[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    return hashes


def _current_paths(destination: Path) -> set[str]:
    return set(_current_hashes(destination))


def _is_transient(relative: str) -> bool:
    return (
        relative.endswith(".pyc")
        or "/__pycache__/" in f"/{relative}"
        or any(
            relative == prefix or relative.startswith(f"{prefix}/")
            for prefix in (".rhizome/cache", ".rhizome/index", ".rhizome/tmp")
        )
    )


def _check(name: str, passed: bool, detail: str, **extra: Any) -> dict[str, Any]:
    result: dict[str, Any] = {"passed": bool(passed), "detail": detail}
    result.update(extra)
    return {name: result}


def _result(case_id: str, checks: Iterable[Mapping[str, Any]]) -> dict[str, Any]:
    merged: dict[str, dict[str, Any]] = {}
    for group in checks:
        for name, value in group.items():
            merged[name] = dict(value)
    failures = [name for name, value in merged.items() if not value.get("passed", False)]
    passed = not failures
    return {
        "case_id": case_id,
        "passed": passed,
        "ok": passed,
        "checks": merged,
        "failures": failures,
    }


def _required_paths(destination: Path, case: Mapping[str, Any]) -> dict[str, Any]:
    missing = [
        str(path)
        for path in case["expected_paths"]
        if not (destination / str(path)).is_file()
    ]
    return _check(
        "required_paths",
        not missing,
        "all seeded visible files are present" if not missing else f"missing files: {missing}",
        missing=missing,
    )


def _read(destination: Path, relative: str) -> str:
    return (destination / relative).read_text(encoding="utf-8")


def _run_visible_tests(destination: Path) -> dict[str, Any]:
    tests = destination / "tests"
    if not tests.is_dir():
        return _check("visible_tests", True, "case intentionally has no visible test suite")
    try:
        completed = subprocess.run(
            [sys.executable, "-m", "unittest", "discover", "-s", "tests", "-q"],
            cwd=destination,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=20,
            check=False,
            env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return _check("visible_tests", False, f"visible tests could not complete: {exc}")
    detail = "visible tests pass" if completed.returncode == 0 else "visible tests failed"
    return _check(
        "visible_tests",
        completed.returncode == 0,
        detail,
        exit_code=completed.returncode,
        stdout=completed.stdout[-4000:],
        stderr=completed.stderr[-4000:],
    )


def _run_behavior_probe(destination: Path, script: str, name: str, detail: str) -> dict[str, Any]:
    try:
        completed = subprocess.run(
            [sys.executable, "-c", script],
            cwd=destination,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=20,
            check=False,
            env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return _check(name, False, f"behavior probe could not complete: {exc}")
    return _check(
        name,
        completed.returncode == 0,
        detail if completed.returncode == 0 else f"{detail}; probe failed",
        exit_code=completed.returncode,
        stdout=completed.stdout[-2000:],
        stderr=completed.stderr[-2000:],
    )


def _unchanged_seeded_files(
    destination: Path,
    case: Mapping[str, Any],
    *,
    allowed_changes: set[str],
) -> dict[str, Any]:
    source = _source_directory(case)
    changed: list[str] = []
    for relative in case["expected_paths"]:
        relative = str(relative)
        if relative in allowed_changes:
            continue
        source_path = source / relative
        destination_path = destination / relative
        if not destination_path.is_file() or destination_path.read_bytes() != source_path.read_bytes():
            changed.append(relative)
    return _check(
        "preserved_seeded_files",
        not changed,
        "protected seeded files are unchanged" if not changed else f"unexpected seeded changes: {changed}",
        changed=changed,
    )


def _unexpected_added_paths(
    destination: Path,
    case: Mapping[str, Any],
    before: Mapping[str, Any] | None,
) -> dict[str, Any]:
    before_paths = _before_paths(before)
    if not before_paths:
        return _check("no_unexpected_added_paths", True, "no comparable before snapshot was supplied")
    current_hashes = _current_hashes(destination)
    current = set(current_hashes)
    allowed = set(str(path) for path in case["allowed_changes"]) | RUNTIME_PATHS
    before_hashes = _before_hashes(before)
    added = sorted(current - before_paths)
    removed = sorted(before_paths - current)
    modified = sorted(
        path
        for path in before_hashes.keys() & current_hashes.keys()
        if before_hashes[path] != current_hashes[path]
    )
    unexpected = [
        path for path in added
        if path not in allowed
    ]
    unexpected_removed = [
        path for path in removed
        if path not in allowed
    ]
    unexpected_modified = [path for path in modified if path not in allowed]
    return _check(
        "no_unexpected_added_paths",
        not unexpected and not unexpected_removed and not unexpected_modified,
        (
            "no unexpected files were added, removed, or modified"
            if not unexpected and not unexpected_removed and not unexpected_modified
            else (
                "unexpected tree changes: "
                f"added={unexpected}, removed={unexpected_removed}, modified={unexpected_modified}"
            )
        ),
        added=added,
        unexpected=unexpected,
        removed=removed,
        unexpected_removed=unexpected_removed,
        modified=modified,
        unexpected_modified=unexpected_modified,
        runtime_changes={
            "added": sorted(set(added) & RUNTIME_PATHS),
            "removed": sorted(set(removed) & RUNTIME_PATHS),
            "modified": sorted(set(modified) & RUNTIME_PATHS),
        },
    )
