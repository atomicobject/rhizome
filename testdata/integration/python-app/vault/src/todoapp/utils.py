"""Utility helpers for the TODO app."""

import re


def slugify(title: str) -> str:
    """Make a filesystem-friendly slug from a title."""
    return re.sub(r"[^a-z0-9]+", "-", title.strip().lower()).strip("-")
