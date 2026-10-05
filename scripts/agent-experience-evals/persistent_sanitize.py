#!/usr/bin/env python3
"""Copy campaign evidence into a reviewable tree with local paths removed."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import re


SECRET_MARKERS = (b'"access_token"', b'"refresh_token"', b'"id_token"', b"OPENAI_API_KEY")


def sanitize(source: Path, destination: Path, manifest: Path, environment: Path) -> None:
    source, destination = source.resolve(), destination.resolve()
    if destination.exists():
        raise FileExistsError("sanitized destination already exists")
    manifest_value = json.loads(manifest.read_text())
    environment_value = json.loads(environment.read_text())
    replacements = {
        str(source): "<RESULTS>",
        str(manifest.resolve().parent): "<CAMPAIGN_ROOT>",
        str(Path.home()): "<USER_HOME>",
    }
    for run in manifest_value["runs"]:
        replacements[run["repo"]] = f"<PREPARED_{run['id'].upper()}>"
        replacements[run["support"]] = f"<SUPPORT_{run['id'].upper()}>"
        replacements[run["binary"]] = "<CANDIDATE_RZM>"
        replacements[run["build_receipt"]] = "<BUILD_RECEIPT>"
    for run_id, entry in environment_value["runs"].items():
        replacements[entry["clean_home"]] = f"<HOME_{run_id.upper()}>"
        replacements[entry["codex_home"]] = f"<CODEX_HOME_{run_id.upper()}>"
        replacements[entry["execution_repo"]] = f"<EXECUTION_{run_id.upper()}>"
    ordered = sorted(((key.encode(), value.encode()) for key, value in replacements.items()),
                     key=lambda item: len(item[0]), reverse=True)
    for path in sorted(source.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"evidence symlink is not allowed: {path}")
        if not path.is_file() or path.name == ".runner.lock":
            continue
        data = path.read_bytes()
        for original, replacement in ordered:
            data = data.replace(original, replacement)
        if any(marker in data for marker in SECRET_MARKERS) or re.search(rb"\bsk-[A-Za-z0-9_-]{20,}\b", data):
            raise ValueError(f"possible credential material in {path.relative_to(source)}")
        if b"/Users/" in data:
            raise ValueError(f"unsanitized user path in {path.relative_to(source)}")
        target = destination / path.relative_to(source)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--environment", type=Path, required=True)
    args = parser.parse_args()
    sanitize(args.input, args.output, args.manifest, args.environment)


if __name__ == "__main__":
    main()
