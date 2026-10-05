#!/usr/bin/env python3
"""Parse and select range-local EffortNote delivery evidence.

The selector is deliberately independent of Git. Its caller supplies the
EffortNote snapshots at a delivery-unit boundary and the paths changed in that
same unit. This keeps lifecycle filtering deterministic and prevents unrelated
completed efforts from becoming release claims.

Requirements: [[../../docs/specs/technical/evidence-grounded-release-orchestration#^acceptancecriterion-a257b28b|SPEC-0078.US1.AC3]]
"""

from __future__ import annotations

import ast
import atexit
import json
from functools import lru_cache
from pathlib import Path
import subprocess
import threading
from html.parser import HTMLParser
import re
from dataclasses import dataclass
from datetime import datetime
from typing import Callable, Iterable


EFFORT_PATH_PREFIX = "docs/efforts/"
PLACEHOLDERS = frozenset(
    {
        "",
        "-",
        "n/a",
        "na",
        "none",
        "none yet",
        "not applicable",
        "not delivered",
        "not started",
        "nothing yet",
        "pending",
        "tbd",
        "todo",
    }
)


@dataclass(frozen=True)
class EffortSnapshot:
    effort_id: str
    path: str
    name: str
    status: str
    actual_delivered: str
    deviations: tuple[str, ...]
    components: frozenset[str] = frozenset()


@dataclass(frozen=True)
class EffortCandidate:
    effort_id: str
    path: str
    name: str
    actual_delivered: str
    deviations: tuple[str, ...]
    inclusion_reason: str


@dataclass(frozen=True)
class EffortDiagnostic:
    code: str
    message: str
    effort_id: str
    path: str


@dataclass(frozen=True)
class EffortSelection:
    candidate: EffortCandidate | None
    diagnostics: tuple[EffortDiagnostic, ...] = ()


class _MetadataParser(HTMLParser):
    # The canonical provider parses with scripting enabled, so noscript is raw text.
    CDATA_CONTENT_ELEMENTS = (*HTMLParser.CDATA_CONTENT_ELEMENTS, "noscript")
    _HEAD_ELEMENTS = frozenset({"base", "link", "meta", "script", "style", "title", "template", "noscript"})
    _VOID_ELEMENTS = frozenset({
        "area", "base", "br", "col", "embed", "hr", "img", "input", "link",
        "meta", "param", "source", "track", "wbr",
    })

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parents: list[str] = []
        self.blocks: list[str] = []
        self.current: list[str] | None = None
        self.head_placement_valid = True

    def handle_starttag(self, tag, attrs):
        # HTML5 inserts an omitted head when metadata precedes body content.
        if self.head_placement_valid and tag in self._HEAD_ELEMENTS and self.parents in ([], ["html"]):
            self.parents.append("head")
        # HTMLParser does not perform HTML5 tree recovery. Fail closed once
        # content could move the provider's insertion point out of the head.
        if tag == "html":
            self.head_placement_valid &= not self.parents
        elif tag == "head":
            self.head_placement_valid &= self.parents in ([], ["html"])
        elif "template" not in self.parents and (
            tag not in self._HEAD_ELEMENTS or self.parents not in (["head"], ["html", "head"])
        ):
            self.head_placement_valid = False
        # HTML attributes use the first occurrence, as in canonicalMarker.
        attributes = dict(reversed(attrs))
        if (tag == "script" and attributes.get("id") == "rhizome-metadata"
                and (attributes.get("type") or "").strip().lower() == "application/json"):
            for marker in ("id", "type"):
                if sum(name == marker for name, _ in attrs) > 1:
                    raise ValueError(f"rhizome-metadata has duplicate {marker!r} attribute")
            if not self.head_placement_valid or self.parents not in (["head"], ["html", "head"]):
                raise ValueError("rhizome-metadata must be a direct child of head")
            self.current = []
        if tag not in self._VOID_ELEMENTS:
            self.parents.append(tag)

    def handle_startendtag(self, tag, attrs):
        self.handle_starttag(tag, attrs)
        if tag == "script" and self.current is not None:
            raise ValueError("workspace requires a complete rhizome-metadata script")

    def handle_data(self, data):
        if self.current is not None:
            self.current.append(data)
        elif data.strip(" \t\n\r\f") and "template" not in self.parents and (not self.parents or self.parents[-1] not in {"title", "style", "script", "noscript"}):
            self.head_placement_valid = False

    def handle_endtag(self, tag):
        # Before/in head, HTML5 ignores stray end tags except those that
        # close the head or trigger body insertion. Template content has its
        # own insertion modes and must retain the stricter checks below.
        if (self.head_placement_valid and "template" not in self.parents
                and self.parents in ([], ["html"]) and tag == "head"):
            return
        if (self.head_placement_valid and "template" not in self.parents
                and self.parents in ([], ["html"], ["head"], ["html", "head"])
                and tag not in {"head", "body", "html", "br"}):
            return
        if "template" in self.parents:
            # Accept balanced template content without allowing a mismatched
            # closing tag to manufacture a direct-child metadata position.
            if not self.parents or self.parents[-1] != tag:
                self.head_placement_valid = False
        elif tag not in {"title", "style", "script", "noscript"} or not self.parents or self.parents[-1] != tag:
            self.head_placement_valid = False
        if tag == "script" and self.current is not None:
            self.blocks.append("".join(self.current))
            self.current = None
        if tag in self.parents:
            del self.parents[len(self.parents) - 1 - self.parents[::-1].index(tag):]


def _unique_metadata_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    metadata: dict[str, object] = {}
    for key, value in pairs:
        if key in metadata:
            raise ValueError(f"canonical metadata has duplicate key {key!r}")
        metadata[key] = value
    return metadata


def _reject_metadata_constant(value: str) -> None:
    raise ValueError(f"canonical metadata has invalid JSON constant {value!r}")


def _workspace_metadata(text: str) -> dict:
    parser = _MetadataParser()
    parser.feed(text.removeprefix("\ufeff"))
    parser.close()
    if len(parser.blocks) != 1 or parser.current is not None:
        raise ValueError("workspace requires exactly one complete rhizome-metadata script")
    metadata = json.loads(
        parser.blocks[0], object_pairs_hook=_unique_metadata_object,
        parse_constant=_reject_metadata_constant,
    )
    if not isinstance(metadata, dict) or metadata.get("type") != "EffortWorkspace":
        raise ValueError("HTML entry is not an EffortWorkspace")
    for key in ("id", "name", "created-at", "status", "summary", "implementation-plan", "work-log"):
        if key not in metadata:
            raise ValueError(f"workspace requires {key}")
        if not isinstance(metadata[key], str):
            raise ValueError(f"workspace {key} must be a string")
        if not metadata[key].strip():
            raise ValueError(f"workspace requires nonblank {key}")
    # EffortWorkspace declares a DATETIME id, but no required aliases field.
    _canonical_contract("workspace-id", metadata["id"])
    if "governing-specs" not in metadata:
        raise ValueError("workspace requires governing-specs")
    specs = metadata["governing-specs"]
    if not isinstance(specs, list):
        raise ValueError("workspace governing-specs must be an array of canonical spec paths")
    # Discovery and provider projection preserve all Markdown extension casings.
    # Shipped SpecLike types select authored metadata, not lowercase path globs.
    for spec in specs:
        if not isinstance(spec, str) or not _canonical_path(spec) or not spec.lower().endswith(".md"):
            raise ValueError(f"workspace has invalid governing-specs path: {spec!r}")
    if metadata["status"] not in {"planned", "active", "complete", "archived"}:
        raise ValueError(f"workspace has invalid status: {metadata['status']!r}")
    created_at = metadata["created-at"]
    # fromisoformat also accepts dates and timezone-free times outside DateTime.
    if not re.fullmatch(
        r"[0-9]{4}-[0-9]{2}-[0-9]{2}T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]"
        r"(?:\.[0-9]+)?(?:Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])",
        created_at,
    ):
        raise ValueError("workspace created-at must be an RFC 3339 timestamp")
    try:
        datetime.fromisoformat(created_at.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError("workspace created-at must be an RFC 3339 timestamp") from error
    return metadata


def parse_effort_snapshot(
    text: str, path: str, *, read_link: Callable[[str], str | None] | None = None,
    is_discovered: Callable[[str], bool] | None = None,
    target_type: Callable[[str, str], str] | None = None,
    is_spec_like: Callable[[str, str], bool] | None = None,
) -> EffortSnapshot:
    """Parse the bounded EffortNote fields used as release evidence."""
    if path.endswith(".html"):
        if not _workspace_path(path):
            raise ValueError(f"{path} is outside the EffortWorkspace entry selector")
        frontmatter = _workspace_metadata(text)
        components = workspace_components(frontmatter)
        for link in frontmatter["governing-specs"]:
            content = read_link(link) if read_link is not None else None
            if content is None:
                raise ValueError(f"{path} linked governing-specs is missing at this snapshot: {link}")
            if is_discovered is not None and not is_discovered(link):
                raise ValueError(f"{path} linked governing-specs is not a discovered note at this snapshot: {link}")
            if not (is_spec_like(link, content) if is_spec_like is not None else
                    _canonical_contract("spec-like", json.dumps({"path": link, "content": content}))):
                raise ValueError(f"{path} linked governing-specs target is not SpecLike: {link}")
        for key in ("implementation-plan", "work-log", "materials"):
            links = frontmatter.get(key) or []
            if isinstance(links, str):
                links = [links]
            for link in links:
                if key == "work-log" and not link.endswith(".md"):
                    raise ValueError(f"{path} has invalid {key} EffortMaterial path: {link!r}")
                content = read_link(link) if read_link is not None else None
                if content is None:
                    raise ValueError(f"{path} linked {key} is missing at this snapshot: {link}")
                if is_discovered is not None and not is_discovered(link):
                    raise ValueError(f"{path} linked {key} is not a discovered note at this snapshot: {link}")
                if (target_type(link, content) if target_type is not None else
                        _canonical_contract("target-type", json.dumps({"path": link, "content": content}))) != "EffortMaterial":
                    raise ValueError(f"{path} linked {key} target is not EffortMaterial: {link}")
                if key == "work-log":
                    text = content
    else:
        components = frozenset()
        frontmatter = _parse_frontmatter(text)
        if frontmatter.get("type") != "EffortNote":
            raise ValueError(f"{path} is not an EffortNote")

    effort_id = frontmatter.get("id", "").strip()
    status = frontmatter.get("status", "").strip().lower()
    if not effort_id:
        raise ValueError(f"{path} has no effort id")
    if not status:
        raise ValueError(f"{path} has no effort status")

    sections = _canonical_contract("effort-sections", text)
    actual_delivered = sections["actual_delivered"]
    deviations = _section_items(sections["deviations"])
    name = frontmatter.get("name", "").strip() or effort_id
    return EffortSnapshot(
        effort_id=effort_id,
        path=path,
        name=name,
        status=status,
        actual_delivered=actual_delivered,
        deviations=deviations,
        components=components,
    )


def select_effort_evidence(
    parent_text: str | None,
    final_text: str,
    *,
    path: str,
    changed_paths: Iterable[str],
    parent_path: str | None = None,
    parent_read_link: Callable[[str], str | None] | None = None,
    final_read_link: Callable[[str], str | None] | None = None,
    parent_is_discovered: Callable[[str], bool] | None = None,
    final_is_discovered: Callable[[str], bool] | None = None,
    parent_target_type: Callable[[str, str], str] | None = None,
    final_target_type: Callable[[str, str], str] | None = None,
    parent_is_spec_like: Callable[[str, str], bool] | None = None,
    final_is_spec_like: Callable[[str, str], bool] | None = None,
) -> EffortSelection:
    """Select one effort only when closure and delivery both occur in-range.

    ``changed_paths`` must describe the same delivery unit as the two snapshots,
    not the whole release range. A non-effort path in that unit is the local
    proof that the effort closure accompanies shipped work.
    """
    try:
        final = parse_effort_snapshot(final_text, path, read_link=final_read_link, is_discovered=final_is_discovered, target_type=final_target_type, is_spec_like=final_is_spec_like)
        original_path = parent_path or path
        # A rename into the workspace selector introduces a new workspace.
        # Never project its previous bytes under the final path's type.
        if original_path.endswith(".html") and not _workspace_path(original_path):
            parent = None
        else:
            parent = parse_effort_snapshot(parent_text, original_path, read_link=parent_read_link, is_discovered=parent_is_discovered, target_type=parent_target_type, is_spec_like=parent_is_spec_like) if parent_text is not None else None
    except ValueError as error:
        return _excluded("invalid_effort", str(error), "", path)

    if parent is not None and parent.effort_id != final.effort_id:
        return _excluded(
            "effort_id_changed",
            f"Excluded {path}: effort id changed from {parent.effort_id} to {final.effort_id}.",
            final.effort_id,
            path,
        )

    if final.status != "complete":
        return _excluded(
            "active_effort",
            f"Excluded {final.effort_id}: final status is {final.status!r}, not complete.",
            final.effort_id,
            path,
        )

    if parent is not None and parent.status == "complete":
        relevant_change = (
            _material_text(parent.actual_delivered) != _material_text(final.actual_delivered)
            or parent.deviations != final.deviations
        )
        if relevant_change:
            return _excluded(
                "post_closure_mutation",
                f"Excluded {final.effort_id}: delivery evidence changed after the effort was complete.",
                final.effort_id,
                path,
            )
        return _excluded(
            "administrative_only",
            f"Excluded {final.effort_id}: only administrative or formatting state changed after closure.",
            final.effort_id,
            path,
        )

    if _is_placeholder(final.actual_delivered):
        return _excluded(
            "missing_actual_delivered",
            f"Excluded {final.effort_id}: Actual Delivered is empty or placeholder text.",
            final.effort_id,
            path,
        )

    if parent is not None and _material_text(parent.actual_delivered) == _material_text(
        final.actual_delivered
    ):
        return _excluded(
            "unchanged_actual_delivered",
            f"Excluded {final.effort_id}: completion did not add or materially update Actual Delivered.",
            final.effort_id,
            path,
        )

    components = final.components | (parent.components if parent else frozenset())
    if not _has_delivery_proof(changed_paths, components):
        return _excluded(
            "missing_delivery_proof",
            f"Excluded {final.effort_id}: its delivery unit has no changed path outside effort files and linked components.",
            final.effort_id,
            path,
        )

    reason = "new_complete" if parent is None else "completed_in_range"
    return EffortSelection(
        candidate=EffortCandidate(
            effort_id=final.effort_id,
            path=final.path,
            name=final.name,
            actual_delivered=final.actual_delivered,
            deviations=final.deviations,
            inclusion_reason=reason,
        )
    )


class CanonicalContractError(RuntimeError):
    """The canonical parser could not evaluate the supplied content."""


_canonical_process: subprocess.Popen[str] | None = None
_canonical_process_lock = threading.Lock()


def _stop_canonical_process() -> None:
    global _canonical_process
    with _canonical_process_lock:
        process, _canonical_process = _canonical_process, None
        if process is None:
            return
        try:
            if process.stdin is not None:
                process.stdin.close()
            process.wait(timeout=1)
        except (OSError, subprocess.TimeoutExpired):
            process.kill()
            process.wait()


atexit.register(_stop_canonical_process)


def _canonical_contract_response(kind: str, content: str) -> str:
    """Exchange one request with a process shared by the release collection."""
    global _canonical_process
    root = Path(__file__).resolve().parents[2]
    with _canonical_process_lock:
        if _canonical_process is None or _canonical_process.poll() is not None:
            _canonical_process = subprocess.Popen(
                ["go", "run", "./scripts/release/contract"], cwd=root,
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, bufsize=1,
            )
        process = _canonical_process
        assert process.stdin is not None and process.stdout is not None
        process.stdin.write(json.dumps({"kind": kind, "content": content}) + "\n")
        process.stdin.flush()
        response = process.stdout.readline()
        if response:
            return response
        detail = process.stderr.read().strip() if process.stderr is not None else ""
        _canonical_process = None
        raise CanonicalContractError(
            "canonical release contract parser failed: "
            + (detail or f"exit status {process.poll()}")
        )


@lru_cache(maxsize=256)
def _canonical_contract(kind: str, content: str):
    """Use shipped Go parsers on supplied snapshot bytes, independent of note files."""
    try:
        output = _canonical_contract_response(kind, content)
    except OSError as error:
        raise CanonicalContractError(f"canonical release contract parser unavailable: {error}") from error
    try:
        response = json.loads(output)
    except json.JSONDecodeError as error:
        raise CanonicalContractError(f"invalid canonical release contract parser response: {error}") from error
    if isinstance(response, dict):
        if (set(response) == {"error", "error_kind"} and response["error_kind"] == "snapshot_schema"
                and isinstance(response["error"], str) and response["error"]):
            raise CanonicalContractError(response["error"])
        if set(response) == {"error"} and isinstance(response["error"], str) and response["error"]:
            raise ValueError(response["error"])
        if set(response) == {"result"}:
            value = response["result"]
            if (kind == "workspace-id" and value is True) or (kind in {"note-discovery", "spec-like"} and isinstance(value, bool)) or (
                kind == "spec-metadata" and isinstance(value, dict)
            ) or (kind == "target-type" and isinstance(value, str)) or (
                kind == "effort-sections" and isinstance(value, dict)
                and set(value) == {"actual_delivered", "deviations"}
                and all(isinstance(item, str) for item in value.values())
            ):
                return value
    raise CanonicalContractError("invalid canonical release contract parser response")


def _parse_frontmatter(text: str) -> dict[str, str]:
    lines = text.replace("\r\n", "\n").splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValueError("note has no YAML frontmatter")

    values: dict[str, str] = {}
    closed = False
    for line in lines[1:]:
        if line.strip() == "---":
            closed = True
            break
        match = re.match(r"^([A-Za-z][A-Za-z0-9_-]*):\s*(.*?)\s*$", line)
        if match:
            values[match.group(1)] = _unquote(match.group(2))
    if not closed:
        raise ValueError("note has unterminated YAML frontmatter")
    return values


def _unquote(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        try:
            parsed = ast.literal_eval(value)
            return parsed if isinstance(parsed, str) else value
        except (SyntaxError, ValueError):
            return value[1:-1]
    return value


def _section_items(content: str) -> tuple[str, ...]:
    if _is_placeholder(content):
        return ()

    items: list[str] = []
    current: list[str] = []
    for line in content.splitlines():
        match = re.match(r"^\s*[-*+]\s+(?:\[[ xX]\]\s+)?(.*)$", line)
        if match:
            if current:
                items.append(" ".join(current).strip())
            current = [match.group(1).strip()]
        elif line.strip():
            current.append(line.strip())
    if current:
        items.append(" ".join(current).strip())
    return tuple(item for item in items if not _is_placeholder(item))


def _material_text(content: str) -> str:
    text = content.strip().lower()
    text = re.sub(r"\[\[([^\]|]+)\|([^\]]+)\]\]", r"\2", text)
    text = re.sub(r"\[\[([^\]]+)\]\]", r"\1", text)
    text = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", text)
    text = re.sub(r"\s+\^[a-z0-9_-]+\s*$", "", text, flags=re.MULTILINE)
    text = re.sub(r"^\s*[-*+]\s+(?:\[[ x]\]\s+)?", "", text, flags=re.MULTILINE)
    text = re.sub(r"[`*_~]", "", text)
    text = re.sub(r"[^a-z0-9]+", " ", text)
    return " ".join(text.split())


def _is_placeholder(content: str) -> bool:
    return _material_text(content) in PLACEHOLDERS


def workspace_components(metadata: dict) -> frozenset[str]:
    components = set()
    for key in ("implementation-plan", "work-log", "materials"):
        value = metadata.get(key, [])
        links = [value] if isinstance(value, str) else value
        if key == "materials" and links is None:
            links = []
        if not isinstance(links, list):
            raise ValueError(f"workspace {key} must contain EffortMaterial paths")
        for link in links:
            if not isinstance(link, str) or not _material_path(link):
                raise ValueError(f"workspace has invalid {key} EffortMaterial path: {link!r}")
            components.add(link)
    return frozenset(components)


def _canonical_path(path: str) -> bool:
    if (any(part in ("", ".", "..") for part in path.split("/"))
            or any(char in path for char in "\\:#?%")
            or any(ord(char) < 32 for char in path) or path != path.strip()):
        return False
    return True


def _workspace_path(path: str) -> bool:
    return _canonical_path(path) and re.fullmatch(
        r"(?:pkg/app/cli/init/templates/starters/agentic-engineering/repo/)?"
        r"docs/efforts/[^/]+/[^/]*-effort\.html", path,
    ) is not None


def _material_path(path: str) -> bool:
    # EffortMaterial membership comes from the shipped ontology path selectors.
    if not _canonical_path(path):
        return False
    return re.fullmatch(
        r"(?:pkg/app/cli/init/templates/starters/agentic-engineering/repo/)?"
        r"docs/efforts/[^/]+/(?:plan\.html|work-log\.md|materials/[^/]+\.(?:md|html))",
        path,
    ) is not None


def _has_delivery_proof(changed_paths: Iterable[str], components: frozenset[str]) -> bool:
    return any(
        normalized and not normalized.startswith(EFFORT_PATH_PREFIX) and normalized not in components
        for path in changed_paths
        if (normalized := path.replace("\\", "/").lstrip("./"))
    )


def _excluded(code: str, message: str, effort_id: str, path: str) -> EffortSelection:
    return EffortSelection(
        candidate=None,
        diagnostics=(
            EffortDiagnostic(
                code=code,
                message=message,
                effort_id=effort_id,
                path=path,
            ),
        ),
    )
