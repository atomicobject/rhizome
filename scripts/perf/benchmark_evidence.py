"""Deterministic evidence helpers for subprocess performance harnesses."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class ScenarioExecutionContract:
    expected_exit_codes: tuple[int, ...] = (0,)
    output_format: str = "json"
    json_channel: str | None = "stdout"
    output_contract: str = "json-v1"

    def __post_init__(self) -> None:
        if not self.expected_exit_codes:
            raise ValueError("expected_exit_codes must not be empty")
        if self.output_format not in {"json", "text"}:
            raise ValueError("output_format must be json or text")
        if self.output_format == "json" and self.json_channel not in {"stdout", "stderr"}:
            raise ValueError("JSON output requires stdout or stderr json_channel")
        if self.output_format == "text" and self.json_channel is not None:
            raise ValueError("text output must not declare a json_channel")


def normalized_channel(
    text: str, *, parse_json: bool, normalize: Any
) -> tuple[str, Any | None]:
    if not parse_json:
        return text, None
    try:
        payload = json.loads(text)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"expected JSON output: {exc}") from exc
    return json.dumps(normalize(payload), sort_keys=True, separators=(",", ":")), payload


def sha256_text(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def file_fingerprint(path: Path) -> dict[str, Any]:
    return {
        "path": str(path),
        "sizeBytes": path.stat().st_size,
        "sha256": sha256_file(path),
    }


def corpus_manifest(source: Path) -> dict[str, Any]:
    rhizome_dir = source / ".rhizome"
    config_path = rhizome_dir / "config.yml"
    index_path = rhizome_dir / "db.sqlite"
    markdown_paths = sorted(
        path for path in source.rglob("*.md") if ".git" not in path.parts
    )
    corpus_digest = hashlib.sha256()
    for path in markdown_paths:
        relative = path.relative_to(source).as_posix().encode()
        corpus_digest.update(len(relative).to_bytes(8, "big"))
        corpus_digest.update(relative)
        content = path.read_bytes()
        corpus_digest.update(len(content).to_bytes(8, "big"))
        corpus_digest.update(content)
    manifest: dict[str, Any] = {
        "markdownFiles": len(markdown_paths),
        "markdownSha256": corpus_digest.hexdigest(),
    }
    if config_path.is_file():
        manifest["config"] = file_fingerprint(config_path)
        manifest["configSha256"] = manifest["config"]["sha256"]
    if index_path.is_file():
        manifest["index"] = file_fingerprint(index_path)
    sidecars: dict[str, Any] = {}
    for label, suffix in (("wal", "-wal"), ("shm", "-shm")):
        path = Path(str(index_path) + suffix)
        if path.is_file():
            sidecars[label] = file_fingerprint(path)
    if sidecars:
        manifest["indexSidecars"] = sidecars
    return manifest


def index_fingerprints(source: Path) -> dict[str, dict[str, Any]]:
    """Return stable fingerprints for the unified index and its sidecars.

    Missing files are evidence too. This lets one-shot cells prove they did
    not create an index while preserving the same report shape for indexed
    and unavailable fixtures.
    """
    index_path = source / ".rhizome" / "db.sqlite"
    result: dict[str, dict[str, Any]] = {}
    for label, path in (
        ("db", index_path),
        ("wal", Path(str(index_path) + "-wal")),
        ("shm", Path(str(index_path) + "-shm")),
    ):
        if path.is_file():
            result[label] = {"present": True, **file_fingerprint(path)}
        else:
            result[label] = {"present": False, "path": str(path)}
    return result


def evidence_manifest(binary: str | Path, source: Path) -> dict[str, Any]:
    return {
        "binary": file_fingerprint(Path(binary)),
        "corpus": corpus_manifest(source),
    }


def operation_envelope(payload: Any) -> list[dict[str, Any]]:
    if not isinstance(payload, dict):
        return []
    diagnostics = payload.get("diagnostics")
    operations = diagnostics.get("operations") if isinstance(diagnostics, dict) else None
    if not isinstance(operations, list):
        return []
    envelope: list[dict[str, Any]] = []
    for operation in operations:
        if not isinstance(operation, dict):
            continue
        label = operation.get("label")
        count = operation.get("count")
        available = operation.get("available")
        if isinstance(label, str) and isinstance(count, int) and isinstance(available, bool):
            envelope.append({"label": label, "count": count, "available": available})
    return envelope


def has_operation_diagnostics(payload: Any) -> bool:
    if not isinstance(payload, dict):
        return False
    diagnostics = payload.get("diagnostics")
    return isinstance(diagnostics, dict) and isinstance(diagnostics.get("operations"), list)
