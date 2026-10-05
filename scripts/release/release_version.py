#!/usr/bin/env python3
"""Recommendation-first interactive release version selection."""

from __future__ import annotations

import re
from typing import Callable, Mapping

from release_actions import validate_release_version


def _increments(base: str) -> dict[str, str]:
    match = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)", base)
    if not match:
        raise ValueError(f"base tag is not semantic versioned: {base}")
    major, minor, patch = (int(item) for item in match.groups())
    return {
        "major": f"v{major + 1}.0.0",
        "minor": f"v{major}.{minor + 1}.0",
        "patch": f"v{major}.{minor}.{patch + 1}",
    }


def present_release_preview(
    release_notes: str,
    rationale: str,
    recommendation: str,
    *,
    output_fn: Callable[[str], None] = print,
) -> None:
    """Present the reviewed content before asking for a release version."""

    output_fn("=== Release Notes Preview ===")
    output_fn(release_notes.rstrip())
    output_fn("=============================")
    output_fn(f"Recommendation rationale ({recommendation}): {rationale.strip()}")


def choose_release_version(
    base: str,
    recommendation: str,
    *,
    supplied: str | None,
    environ: Mapping[str, str],
    input_fn: Callable[[str], str] = input,
    output_fn: Callable[[str], None] = print,
) -> str:
    """Put the model recommendation first and make Enter accept it."""

    version = supplied or environ.get("VERSION", "").strip()
    if version:
        return validate_release_version(version)
    if recommendation not in {"minor", "patch"}:
        raise ValueError("release recommendation must be minor or patch")

    increments = _increments(base)
    alternate = "patch" if recommendation == "minor" else "minor"
    options = (
        (recommendation, increments[recommendation]),
        (alternate, increments[alternate]),
        ("major", increments["major"]),
    )
    output_fn("Version options:")
    output_fn(f"1. {options[0][0]}  {options[0][1]}  (recommended, default)")
    output_fn(f"2. {options[1][0]}  {options[1][1]}")
    output_fn(f"3. major  {options[2][1]}")
    output_fn("4. other  (enter custom version)")

    choice = input_fn("Choose version [1-4, Enter for recommended]: ").strip() or "1"
    if choice == "4":
        version = input_fn("Enter version (e.g., v1.2.3): ").strip()
    elif choice in {"1", "2", "3"}:
        version = options[int(choice) - 1][1]
    else:
        version = options[0][1]
    return validate_release_version(version)
