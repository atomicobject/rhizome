#!/usr/bin/env python3
"""Structured, source-attributed release-note themes."""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING, Any, Mapping, Sequence

if TYPE_CHECKING:
    from release_types import Diagnostic


@dataclass(frozen=True, slots=True)
class ReleaseTheme:
    release_note: str
    changelog_entry: str
    source_ids: tuple[str, ...]

    def __post_init__(self) -> None:
        note = self.release_note.strip()
        entry = self.changelog_entry.strip()
        sources = tuple(source.strip() for source in self.source_ids)
        if not note or not entry:
            raise ValueError("release themes require release-note and changelog text")
        if not sources or any(not source for source in sources):
            raise ValueError("release themes require at least one source identifier")
        if len(set(sources)) != len(sources):
            raise ValueError("release theme source identifiers must be unique")
        object.__setattr__(self, "release_note", note)
        object.__setattr__(self, "changelog_entry", entry)
        object.__setattr__(self, "source_ids", sources)

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> ReleaseTheme:
        return cls(
            release_note=str(value["release_note"]),
            changelog_entry=str(value["changelog_entry"]),
            source_ids=tuple(str(item) for item in value.get("source_ids", ())),
        )


@dataclass(frozen=True, slots=True)
class GenerationResult:
    recommended_bump: str
    rationale: str
    themes: tuple[ReleaseTheme, ...]
    model: str = ""
    reasoning_effort: str = ""
    diagnostics: tuple[Diagnostic, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, "themes", tuple(self.themes))
        object.__setattr__(self, "diagnostics", tuple(self.diagnostics))
        if not self.themes:
            raise ValueError("release generation requires at least one theme")

    @property
    def release_notes(self) -> str:
        return "\n\n".join(theme.release_note for theme in self.themes)

    @property
    def changelog_entries(self) -> tuple[str, ...]:
        return tuple(theme.changelog_entry for theme in self.themes)

    @property
    def source_attributions(self) -> tuple[str, ...]:
        return _ordered_sources(self.themes)

    def to_dict(self) -> dict[str, Any]:
        return {
            "recommended_bump": self.recommended_bump,
            "rationale": self.rationale,
            "themes": [
                {
                    "release_note": theme.release_note,
                    "changelog_entry": theme.changelog_entry,
                    "source_ids": list(theme.source_ids),
                }
                for theme in self.themes
            ],
        }


def _ordered_sources(themes: Sequence[ReleaseTheme]) -> tuple[str, ...]:
    return tuple(dict.fromkeys(source for theme in themes for source in theme.source_ids))


def merge_curated_themes(
    entries: Sequence[str], generated: Sequence[ReleaseTheme]
) -> tuple[ReleaseTheme, ...]:
    """Use source coverage to avoid paraphrase duplicates and preserve omissions."""

    merged: list[ReleaseTheme] = []
    covered = {
        source
        for theme in generated
        for source in theme.source_ids
        if source.startswith("unreleased:")
    }
    for index, raw_entry in enumerate(entries, 1):
        entry = raw_entry.strip()
        source = f"unreleased:{index}"
        if source in covered:
            continue
        merged.append(ReleaseTheme(f"- {entry}", entry, (source,)))
    merged.extend(generated)
    return tuple(merged)
