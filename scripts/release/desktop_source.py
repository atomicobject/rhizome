#!/usr/bin/env python3
"""Validate a native desktop producer's canonical public source and version."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

from desktop_candidates import CandidateError, identity
from release_gateway_runs import GatewayError, command
from release_gateway_source import REPOSITORY, SourceError, SourceReader
from release_version import source_release_version


def validate_source(root: Path, commit: str, version: str, *, runner=command) -> dict:
    version, commit = identity(version, commit)
    reader = SourceReader(root, runner)
    reader.verify_mode(False)
    if reader.ref("release") != commit:
        raise CandidateError("native desktop source must equal the canonical public release branch")
    reader.canonical(commit, reader.ref("main"))
    if reader.git("rev-parse", f"{commit}^{{commit}}").strip() != commit:
        raise CandidateError("native desktop source is not the selected local commit")
    actual = source_release_version(reader.git("show", f"{commit}:pkg/vault/version/version.go")).removeprefix("v")
    if actual != version:
        raise CandidateError("native desktop version does not match the canonical source version")
    return {"repository": REPOSITORY, "source_commit": commit, "version": version}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(validate_source(args.repo_root, args.commit, args.version)))
    except (CandidateError, GatewayError, SourceError, OSError, ValueError) as error:
        print(f"desktop source: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
