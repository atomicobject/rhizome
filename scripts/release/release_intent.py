#!/usr/bin/env python3
"""Read the small, reviewed public-release intent shared by local prep and CI."""

from __future__ import annotations

from datetime import date
import json
from pathlib import Path
import re

from release_actions import (
    CHANGELOG_PATH, EMBEDDED_CHANGELOG_PATH, RELEASE_FILE_PATHS, VERSION_PATH,
    render_version_go, validate_release_version,
)
from release_version import validate_next_version


MANIFEST_PATH = Path(".github/release-plan.json")
_FIELDS = {"schema_version", "source_commit", "base_tag", "version", "release_date"}


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"release manifest repeats field {key}")
        result[key] = value
    return result


def parse_manifest(content: str) -> dict:
    """Validate release intent without accepting arbitrary paths or extra fields."""
    try:
        value = json.loads(content, object_pairs_hook=_unique_object)
    except json.JSONDecodeError as error:
        raise ValueError("release manifest is not valid JSON") from error
    if not isinstance(value, dict) or set(value) != _FIELDS:
        raise ValueError("release manifest must contain exactly the five release intent fields")
    if type(value["schema_version"]) is not int or value["schema_version"] != 1:
        raise ValueError("unsupported release manifest schema version")
    if not all(isinstance(value[key], str) for key in _FIELDS - {"schema_version"}):
        raise ValueError("release manifest values must be strings")
    if re.fullmatch(r"[0-9a-f]{40}", value["source_commit"]) is None:
        raise ValueError("release source must be a full lowercase Git commit SHA")
    validate_release_version(value["version"])
    if value["base_tag"]:
        validate_release_version(value["base_tag"])
        validate_next_version(value["base_tag"], value["version"])
    try:
        canonical_date = date.fromisoformat(value["release_date"]).isoformat()
    except ValueError as error:
        raise ValueError("release date must match YYYY-MM-DD") from error
    if canonical_date != value["release_date"]:
        raise ValueError("release date must match YYYY-MM-DD")
    return value


def notes_path(version: str) -> Path:
    return Path("docs/releases") / f"{validate_release_version(version)}.md"


def release_files(version: str) -> tuple[Path, ...]:
    return (*RELEASE_FILE_PATHS, MANIFEST_PATH, notes_path(version))


def read_release_file(root: Path, relative: Path) -> str:
    """Read only ordinary release files contained in this checkout."""
    root = root.resolve()
    path = root / relative
    if path.is_symlink() or not path.resolve().is_relative_to(root) or not path.is_file():
        raise ValueError(f"release file must be an ordinary checkout file: {relative}")
    try:
        return path.read_bytes().decode("utf-8")
    except UnicodeDecodeError as error:
        raise ValueError(f"release file must be UTF-8: {relative}") from error


def validate_prepared(root: Path) -> dict:
    """Validate the committed release content; Git/source approval is a caller gate."""
    root = Path(root)
    manifest = parse_manifest(read_release_file(root, MANIFEST_PATH))
    version = manifest["version"]
    if read_release_file(root, VERSION_PATH) != render_version_go(version):
        raise ValueError("compiled version source differs from the release version renderer")
    changelog = read_release_file(root, CHANGELOG_PATH)
    if read_release_file(root, EMBEDDED_CHANGELOG_PATH) != changelog:
        raise ValueError("root and embedded changelogs differ; run prepare_release.py sync")
    headings = re.findall(r"(?m)^## \[([^\]]+)\](?: - ([^\n]+))?$", changelog)
    releases = [(name, stamp) for name, stamp in headings if name != "Unreleased"]
    if not releases or releases[0] != (version, manifest["release_date"]):
        raise ValueError("first changelog release must match the selected version and date")
    if sum(name == version for name, _ in releases) != 1:
        raise ValueError("changelog must contain exactly one selected release section")
    section = re.search(rf"(?ms)^## \[{re.escape(version)}\] - {re.escape(manifest['release_date'])}\n(.*?)(?=^## |\Z)", changelog)
    if section is None or not section.group(1).strip():
        raise ValueError("selected changelog release must contain reviewed entries")
    notes = read_release_file(root, notes_path(version))
    if not notes.strip() or "\x00" in notes:
        raise ValueError("release narrative must be nonempty Markdown text")
    return manifest
