#!/usr/bin/env python3
"""Budgeted, schema-validated release-note generation.

``ReleaseEvidence`` remains the complete audit artifact. This module projects it
into bounded generation packets and invokes Codex in a read-only sandbox. It
never mutates repository state and never sends a raw diff.
"""

from __future__ import annotations

from dataclasses import dataclass, replace
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from typing import Any, Mapping, Sequence

from release_prompt import MAX_THEMES, generation_prompt, synthesis_prompt
from release_theme import GenerationResult, ReleaseTheme
from release_types import Diagnostic, DeliveryUnit, ReleaseEvidence, canonical_json


DEFAULT_MODEL = "gpt-5.6-luna"
DEFAULT_REASONING_EFFORT = "medium"
DEFAULT_BUDGET_CHARS = 620_000
SUMMARY_LIMIT = 500
_RESULT_FIELDS = {
    "recommended_bump",
    "rationale",
    "themes",
}


class GenerationError(RuntimeError):
    """Release-note generation could not produce a trustworthy result."""


@dataclass(frozen=True, slots=True)
class GenerationPacket:
    payload: dict[str, Any]
    omitted_sources: tuple[str, ...] = ()
    truncated_sources: tuple[str, ...] = ()
    part_number: int = 1
    total_parts: int = 1

    def prompt_payload(self) -> str:
        return canonical_json(
            {
                "packet": self.payload,
                "budget_report": {
                    "omitted_sources": self.omitted_sources,
                    "truncated_sources": self.truncated_sources,
                },
                "part_number": self.part_number,
                "total_parts": self.total_parts,
            }
        )


@dataclass(frozen=True, slots=True)
class _Detail:
    priority: int
    source_id: str
    unit_id: str | None
    field: str
    text: str


def _summary(unit: DeliveryUnit) -> str:
    text = unit.pull_request.title if unit.pull_request else unit.mainline_commit.subject
    text = text.strip() or unit.mainline_commit.sha
    if len(text) > SUMMARY_LIMIT:
        return text[: SUMMARY_LIMIT - 1].rstrip() + "…"
    return text


def _summary_truncations(units: Sequence[DeliveryUnit]) -> list[str]:
    return [
        f"summary:{unit.unit_id}"
        for unit in units
        if len((unit.pull_request.title if unit.pull_request else unit.mainline_commit.subject).strip()) > SUMMARY_LIMIT
    ]


def _minimal_payload(evidence: ReleaseEvidence, units: Sequence[DeliveryUnit]) -> dict[str, Any]:
    return {
        "release_range": {
            "base_tag": evidence.base_tag,
            "base_commit": evidence.base_commit,
            "head_commit": evidence.head_commit,
            "github_status": evidence.github_status,
        },
        "unreleased_entries": [],
        "delivery_units": [
            {
                "unit_id": unit.unit_id,
                "commit": unit.mainline_commit.sha,
                "summary": _summary(unit),
            }
            for unit in units
        ],
    }


def _details(
    evidence: ReleaseEvidence,
    units: Sequence[DeliveryUnit],
    *,
    include_curated: bool,
) -> list[_Detail]:
    details: list[_Detail] = []
    if include_curated:
        details.extend(
            _Detail(0, f"unreleased:{index}", None, "unreleased_entries", entry)
            for index, entry in enumerate(evidence.unreleased_entries, 1)
        )
    for unit in units:
        ordered_efforts = sorted(
            unit.efforts,
            key=lambda effort: ({"primary": 0, "peer": 1, "supporting": 2}.get(effort.role, 3), effort.effort_id),
        )
        for effort in ordered_efforts:
            text = f"{effort.name}\n{effort.actual_delivered}".strip()
            if effort.deviations:
                text += "\nDeviations: " + "; ".join(effort.deviations)
            details.append(_Detail(1, f"effort:{effort.effort_id}", unit.unit_id, "effort_details", text))
        pull_requests = (() if unit.pull_request is None else (unit.pull_request,)) + tuple(unit.supporting_pull_requests)
        for pull_request in pull_requests:
            text = f"{pull_request.title}\n{pull_request.body}".strip()
            details.append(_Detail(2, f"pr:{pull_request.number}", unit.unit_id, "pr_details", text))
        for commit in unit.commits:
            text = f"{commit.subject}\n{commit.body}".strip()
            details.append(_Detail(3, f"commit:{commit.sha}", unit.unit_id, "commit_details", text))
    return sorted(details, key=lambda item: item.priority)


def _add_detail(payload: dict[str, Any], detail: _Detail, text: str) -> None:
    item = {"source_id": detail.source_id, "text": text}
    if detail.unit_id is None:
        payload[detail.field].append(item)
        return
    target = next(unit for unit in payload["delivery_units"] if unit["unit_id"] == detail.unit_id)
    target.setdefault(detail.field, []).append(item)


def _copy_payload(payload: Mapping[str, Any]) -> dict[str, Any]:
    return json.loads(json.dumps(payload))


def _packet(
    payload: dict[str, Any],
    omitted: Sequence[str],
    truncated: Sequence[str],
    part_number: int = 1,
    total_parts: int = 1,
) -> GenerationPacket:
    return GenerationPacket(
        payload=payload,
        omitted_sources=tuple(omitted),
        truncated_sources=tuple(truncated),
        part_number=part_number,
        total_parts=total_parts,
    )


def _enrich_packet(
    evidence: ReleaseEvidence,
    units: Sequence[DeliveryUnit],
    budget_chars: int,
    *,
    include_curated: bool,
    part_number: int = 1,
    total_parts: int = 1,
) -> GenerationPacket:
    payload = _minimal_payload(evidence, units)
    details = _details(evidence, units, include_curated=include_curated)
    omitted = [detail.source_id for detail in details]
    truncated = _summary_truncations(units)
    initial = _packet(payload, omitted, truncated, part_number, total_parts)
    if len(initial.prompt_payload()) > budget_chars:
        raise GenerationError("generation budget is too small for delivery identities and summaries")

    for detail in details:
        candidate_payload = _copy_payload(payload)
        _add_detail(candidate_payload, detail, detail.text)
        candidate_omitted = [source for source in omitted if source != detail.source_id]
        candidate = _packet(candidate_payload, candidate_omitted, truncated, part_number, total_parts)
        if len(candidate.prompt_payload()) <= budget_chars:
            payload, omitted = candidate_payload, candidate_omitted
            continue

        low, high, accepted = 24, len(detail.text), ""
        while low <= high:
            midpoint = (low + high) // 2
            shortened = detail.text[:midpoint].rstrip() + "…"
            partial_payload = _copy_payload(payload)
            _add_detail(partial_payload, detail, shortened)
            partial = _packet(
                partial_payload,
                candidate_omitted,
                [*truncated, detail.source_id],
                part_number,
                total_parts,
            )
            if len(partial.prompt_payload()) <= budget_chars:
                accepted = shortened
                low = midpoint + 1
            else:
                high = midpoint - 1
        if accepted:
            _add_detail(payload, detail, accepted)
            omitted = candidate_omitted
            truncated.append(detail.source_id)
    return _packet(payload, omitted, truncated, part_number, total_parts)


def build_minimal_packet(evidence: ReleaseEvidence) -> GenerationPacket:
    """Return identity/summary coverage plus a complete omitted-detail record."""

    details = _details(evidence, evidence.delivery_units, include_curated=True)
    return _packet(
        _minimal_payload(evidence, evidence.delivery_units),
        [detail.source_id for detail in details],
        _summary_truncations(evidence.delivery_units),
    )


def build_generation_packets(
    evidence: ReleaseEvidence,
    budget_chars: int = DEFAULT_BUDGET_CHARS,
) -> tuple[GenerationPacket, ...]:
    """Project complete evidence into deterministic packets without dropping units."""

    if budget_chars < 512:
        raise GenerationError("generation budget must be at least 512 characters")
    try:
        return (_enrich_packet(evidence, evidence.delivery_units, budget_chars, include_curated=True),)
    except GenerationError:
        pass

    groups: list[list[DeliveryUnit]] = []
    current: list[DeliveryUnit] = []
    for unit in evidence.delivery_units:
        proposed = [*current, unit]
        include_curated = not groups
        try:
            _enrich_packet(evidence, proposed, budget_chars - 64, include_curated=include_curated)
            current = proposed
        except GenerationError:
            if not current:
                raise GenerationError(
                    f"generation budget cannot fit delivery unit {unit.unit_id} identity and summary"
                )
            groups.append(current)
            current = [unit]
    if current:
        groups.append(current)

    total = len(groups)
    return tuple(
        _enrich_packet(
            evidence,
            group,
            budget_chars,
            include_curated=index == 1,
            part_number=index,
            total_parts=total,
        )
        for index, group in enumerate(groups, 1)
    )


# Codex structured output accepts a JSON Schema subset. Keep unsupported
# semantic constraints, including non-empty strings and uniqueness, in
# ``validate_result`` / ``ReleaseTheme`` rather than the transport schema.
OUTPUT_SCHEMA: dict[str, Any] = {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "type": "object",
    "additionalProperties": False,
    "required": sorted(_RESULT_FIELDS),
    "properties": {
        "recommended_bump": {"type": "string", "enum": ["minor", "patch"]},
        "rationale": {"type": "string"},
        "themes": {
            "type": "array",
            "minItems": 1,
            "maxItems": MAX_THEMES,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["release_note", "changelog_entry", "source_ids"],
                "properties": {
                    "release_note": {"type": "string"},
                    "changelog_entry": {"type": "string"},
                    "source_ids": {
                        "type": "array",
                        "minItems": 1,
                        "items": {"type": "string"},
                    },
                },
            },
        },
    },
}


def _known_sources(evidence: ReleaseEvidence) -> set[str]:
    sources = {unit.unit_id for unit in evidence.delivery_units}
    sources.update(f"unreleased:{index}" for index, _ in enumerate(evidence.unreleased_entries, 1))
    for unit in evidence.delivery_units:
        sources.update(f"effort:{effort.effort_id}" for effort in unit.efforts)
        if unit.pull_request:
            sources.add(f"pr:{unit.pull_request.number}")
        sources.update(f"pr:{pr.number}" for pr in unit.supporting_pull_requests)
        sources.update(f"commit:{commit.sha}" for commit in unit.commits)
    return sources


def validate_result(content: str, known_sources: set[str]) -> GenerationResult:
    try:
        value = json.loads(content)
    except json.JSONDecodeError as error:
        raise GenerationError(f"Codex output is not valid JSON: {error.msg}") from error
    if not isinstance(value, dict):
        raise GenerationError("Codex output must be a JSON object")
    unexpected = set(value) - _RESULT_FIELDS
    missing = _RESULT_FIELDS - set(value)
    if unexpected:
        raise GenerationError("Codex output has unexpected fields: " + ", ".join(sorted(unexpected)))
    if missing:
        raise GenerationError("Codex output is missing fields: " + ", ".join(sorted(missing)))
    bump = value["recommended_bump"]
    if bump not in {"minor", "patch"}:
        raise GenerationError("recommended_bump must be minor or patch")
    if not isinstance(value["rationale"], str) or not value["rationale"].strip():
        raise GenerationError("rationale must be a non-empty string")
    raw_themes = value["themes"]
    if not isinstance(raw_themes, list) or not raw_themes:
        raise GenerationError("themes must be a non-empty array")
    if len(raw_themes) > MAX_THEMES:
        raise GenerationError(f"release output must contain at most {MAX_THEMES} themes")
    try:
        themes = tuple(ReleaseTheme.from_dict(item) for item in raw_themes)
    except (KeyError, TypeError, ValueError) as error:
        raise GenerationError(f"invalid release theme: {error}") from error
    unknown = sorted(set(source for theme in themes for source in theme.source_ids) - known_sources)
    if unknown:
        raise GenerationError("unknown source attribution: " + ", ".join(unknown))
    if any(not theme.release_note.startswith("- ") for theme in themes):
        raise GenerationError("each release_note must begin with '- '")
    if any(theme.changelog_entry.startswith("- ") for theme in themes):
        raise GenerationError("changelog_entry must not include a Markdown bullet marker")
    return GenerationResult(
        recommended_bump=bump,
        rationale=value["rationale"].strip(),
        themes=themes,
    )


def _resolve_codex(codex_bin: str) -> str:
    if os.sep in codex_bin:
        path = Path(codex_bin)
        if path.is_file() and os.access(path, os.X_OK):
            return str(path)
    else:
        resolved = shutil.which(codex_bin)
        if resolved:
            return resolved
    raise GenerationError(f"Codex executable not found: {codex_bin}")


def _preflight(codex_bin: str, repo_root: Path) -> None:
    result = subprocess.run(
        [codex_bin, "login", "status"],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "login status failed"
        raise GenerationError(f"Codex authentication unavailable: {detail}")


def _invoke(
    prompt: str,
    *,
    codex_bin: str,
    repo_root: Path,
    model: str,
    reasoning_effort: str,
    known_sources: set[str],
) -> GenerationResult:
    with tempfile.TemporaryDirectory(prefix="rhizome-release-notes-") as temp_dir:
        schema_path = Path(temp_dir) / "schema.json"
        output_path = Path(temp_dir) / "output.json"
        schema_path.write_text(json.dumps(OUTPUT_SCHEMA), encoding="utf-8")
        command = [
            codex_bin,
            "exec",
            "--model",
            model,
            "-c",
            f'model_reasoning_effort="{reasoning_effort}"',
            "--sandbox",
            "read-only",
            "--ephemeral",
            "--cd",
            str(repo_root),
            "--output-schema",
            str(schema_path),
            "--output-last-message",
            str(output_path),
            "-",
        ]
        result = subprocess.run(command, cwd=repo_root, input=prompt, capture_output=True, text=True)
        if result.returncode != 0:
            detail = result.stderr.strip() or result.stdout.strip() or "unknown error"
            raise GenerationError(f"Codex model invocation failed: {detail}")
        if not output_path.exists():
            raise GenerationError("Codex model invocation produced no output")
        return validate_result(output_path.read_text(encoding="utf-8"), known_sources)


def _packet_prompt(packet: GenerationPacket, guidance: str) -> str:
    return generation_prompt(packet.prompt_payload(), guidance)


def generate_release_notes(
    evidence: ReleaseEvidence,
    *,
    extra_guidance: str = "",
    budget_chars: int = DEFAULT_BUDGET_CHARS,
    codex_bin: str = "codex",
    repo_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> GenerationResult:
    """Generate structured notes, chunking only when minimal unit coverage overflows."""

    env = os.environ if environ is None else environ
    model = env.get("RZM_RELEASE_MODEL", DEFAULT_MODEL).strip()
    reasoning = env.get("RZM_RELEASE_REASONING_EFFORT", DEFAULT_REASONING_EFFORT).strip()
    if not model or not reasoning:
        raise GenerationError("release model and reasoning effort overrides must be non-empty")
    root = Path.cwd() if repo_root is None else Path(repo_root)
    executable = _resolve_codex(codex_bin)
    _preflight(executable, root)
    packets = build_generation_packets(evidence, budget_chars)
    known_sources = _known_sources(evidence)
    results = [
        _invoke(
            _packet_prompt(packet, extra_guidance),
            codex_bin=executable,
            repo_root=root,
            model=model,
            reasoning_effort=reasoning,
            known_sources=known_sources,
        )
        for packet in packets
    ]
    generated = results[0]
    if len(results) > 1:
        synthesis = synthesis_prompt(
            canonical_json({"chunk_results": [result.to_dict() for result in results]})
        )
        generated = _invoke(
            synthesis,
            codex_bin=executable,
            repo_root=root,
            model=model,
            reasoning_effort=reasoning,
            known_sources=known_sources,
        )
    diagnostics = []
    if len(packets) > 1:
        diagnostics.append(Diagnostic(code="generation_chunked", message=f"release evidence required {len(packets)} generation packets", source="notes"))
    omitted = sum(len(packet.omitted_sources) for packet in packets)
    truncated = sum(len(packet.truncated_sources) for packet in packets)
    if omitted:
        sources = tuple(source for packet in packets for source in packet.omitted_sources)
        diagnostics.append(Diagnostic(code="generation_detail_omitted", message=f"{omitted} lower-priority evidence details were omitted by the generation budget", source="notes", details=tuple(("source", source) for source in sources)))
    if truncated:
        sources = tuple(source for packet in packets for source in packet.truncated_sources)
        diagnostics.append(Diagnostic(code="generation_detail_truncated", message=f"{truncated} evidence details were truncated by the generation budget", source="notes", details=tuple(("source", source) for source in sources)))
    return replace(generated, model=model, reasoning_effort=reasoning, diagnostics=tuple(diagnostics))
