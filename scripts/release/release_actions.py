#!/usr/bin/env python3
"""Pure renderers and retry-safe file mutations for a reviewed release plan.

Only the root changelog, compiled-in version, and embedded changelog are owned
by this module. Re-rendering from a partially applied repository converges on
the same contents, so orchestration can checkpoint after this operation.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
import re
from typing import Iterable


CHANGELOG_PATH = Path("CHANGELOG.md")
VERSION_PATH = Path("pkg/vault/version/version.go")
EMBEDDED_CHANGELOG_PATH = Path("pkg/vault/changelog/CHANGELOG.md")
RELEASE_FILE_PATHS = (CHANGELOG_PATH, VERSION_PATH, EMBEDDED_CHANGELOG_PATH)

_SEMVER_PATTERN = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)")
_H2_PATTERN = re.compile(r"(?m)^## .+$")


def validate_release_version(version: str) -> str:
    """Return a canonical release version or reject non-vMAJOR.MINOR.PATCH input."""

    if _SEMVER_PATTERN.fullmatch(version) is None:
        raise ValueError("release version must match vMAJOR.MINOR.PATCH")
    return version


def _validate_release_date(release_date: str) -> str:
    try:
        parsed = date.fromisoformat(release_date)
    except ValueError as error:
        raise ValueError("release date must match YYYY-MM-DD") from error
    if parsed.isoformat() != release_date:
        raise ValueError("release date must match YYYY-MM-DD")
    return release_date


def _render_entries(changelog_entries: Iterable[str]) -> str:
    rendered: list[str] = []
    for entry in changelog_entries:
        normalized = str(entry).strip()
        if not normalized:
            raise ValueError("changelog entries must be non-empty")
        lines = normalized.splitlines()
        if not lines[0].startswith("- "):
            lines[0] = f"- {lines[0]}"
        rendered.append("\n".join(lines))
    if not rendered:
        raise ValueError("at least one changelog entry is required")
    return "\n".join(rendered)


def _changelog_blocks(content: str) -> tuple[str, list[str]]:
    matches = list(_H2_PATTERN.finditer(content))
    if not matches:
        prefix = content.strip()
        return (prefix or "# Changelog"), []

    prefix = content[: matches[0].start()].strip() or "# Changelog"
    sections = [
        content[match.start() : matches[index + 1].start()].strip()
        if index + 1 < len(matches)
        else content[match.start() :].strip()
        for index, match in enumerate(matches)
    ]
    return prefix, sections


def render_changelog(
    current: str,
    *,
    version: str,
    release_date: str,
    changelog_entries: Iterable[str],
) -> str:
    """Stamp exactly one release section and leave ``Unreleased`` empty.

    Any pre-existing section for the selected version is replaced. This is the
    important retry invariant when a previous apply stopped after writing only
    the root changelog.
    """

    validate_release_version(version)
    _validate_release_date(release_date)
    release_section = (
        f"## [{version}] - {release_date}\n\n{_render_entries(changelog_entries)}"
    )
    prefix, sections = _changelog_blocks(current)
    target_prefix = f"## [{version}] -"
    filtered = [section for section in sections if not section.startswith(target_prefix)]

    unreleased_index = next(
        (
            index
            for index, section in enumerate(filtered)
            if section.splitlines()[0] == "## [Unreleased]"
        ),
        None,
    )
    if unreleased_index is None:
        filtered.insert(0, release_section)
    else:
        # Releasing consumes the reviewed plan entries rather than carrying
        # arbitrary old Unreleased prose into the stamped release.
        filtered[unreleased_index] = "## [Unreleased]"
        filtered.insert(unreleased_index + 1, release_section)

    return "\n\n".join((prefix, *filtered)).rstrip() + "\n"


def render_version_go(version: str) -> str:
    """Render the complete compiled-in Go version source."""

    validate_release_version(version)
    return f'''package version

// Version is the current rhizome version. Goreleaser overrides it at build
// time via -X so tagged releases reflect the cut version.
var Version = "{version}"
'''


def apply_release_files(
    repo_root: Path,
    *,
    version: str,
    release_date: str,
    changelog_entries: Iterable[str],
) -> tuple[Path, ...]:
    """Apply the reviewed release contents to exactly three repository files.

    The returned paths are repository-relative files whose contents changed.
    Files are compared before writing, which makes a completed retry a no-op.
    A partial write is also safe to retry because every desired output is a
    deterministic function of the selected version and plan entries.
    """

    root = Path(repo_root)
    changelog_path = root / CHANGELOG_PATH
    current_changelog = (
        changelog_path.read_text(encoding="utf-8") if changelog_path.exists() else ""
    )
    desired_changelog = render_changelog(
        current_changelog,
        version=version,
        release_date=release_date,
        changelog_entries=changelog_entries,
    )
    desired = {
        CHANGELOG_PATH: desired_changelog,
        VERSION_PATH: render_version_go(version),
        EMBEDDED_CHANGELOG_PATH: desired_changelog,
    }

    changed: list[Path] = []
    for relative_path in RELEASE_FILE_PATHS:
        path = root / relative_path
        existing = path.read_text(encoding="utf-8") if path.exists() else None
        if existing == desired[relative_path]:
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(desired[relative_path], encoding="utf-8")
        changed.append(relative_path)
    return tuple(changed)
