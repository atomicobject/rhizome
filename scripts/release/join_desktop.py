#!/usr/bin/env python3
"""Sign this run's verified Mac and Linux desktop candidates into one complete updater feed.

The protected Linux coordinator is the only process that receives the updater
signing key. Native jobs hand over unsigned candidates; `prepare-native` binds
their exact bytes before this step sees the key.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

from assemble_desktop import assemble
from ci_release_artifacts import validate_native_inputs
from desktop_candidates import ROOT, CandidateError, MANIFEST, REQUIRED_TARGETS
from verify_desktop_signing import DesktopSigningError, verify_archive


def join(prepared: Path, source: str, version: str, output: Path, *, environ, root: Path = ROOT,
         runner=subprocess.run, verifier=verify_archive, candidate_only: bool = False) -> Path:
    validate_native_inputs(prepared, source, version, candidate_only=candidate_only)
    directories = [prepared / target for target in sorted(REQUIRED_TARGETS)]
    # prepare-native already bound each candidate's hash to its group's archived CLI bytes.
    hashes = {directory.name: json.loads((directory / MANIFEST).read_text()).get("canonicalCliSha256")
              for directory in directories}
    return assemble(directories, version, source, hashes, output, production=not candidate_only,
                    candidate_only=candidate_only, root=root, source=environ, runner=runner, verifier=verifier)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--input", type=Path, required=True, help="prepare-native output")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--candidate-only", action="store_true")
    args = parser.parse_args(argv)
    try:
        join(args.input, args.source, args.version, args.output, environ=dict(os.environ),
             candidate_only=args.candidate_only)
    except (CandidateError, DesktopSigningError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"desktop join: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
