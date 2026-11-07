#!/usr/bin/env python3
"""Sign GUI/installer files without modifying the already signed canonical CLI."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

from desktop_candidates import CandidateError, HASH, sha256
from run_clean import sensitive_name


def configuration(path: Path) -> dict:
    config = json.loads(path.read_text())
    if not isinstance(config, dict):
        raise CandidateError("Windows signer config must be an object")
    command, environment = config.get("command"), config.get("environment", [])
    if (set(config) - {"command", "environment"} or not isinstance(command, list) or not command
            or any(not isinstance(v, str) or not v for v in command) or command.count("%1") != 1
            or not isinstance(environment, list) or any(not isinstance(v, str) or not v for v in environment)):
        raise CandidateError("Windows signer config requires an argv command containing one %1 and environment names")
    if any(not name.startswith(("AZURE_", "WINDOWS_SIGN_", "WINDOWS_CERTIFICATE")) for name in environment):
        raise CandidateError("Windows signer may receive only its declared native signing credentials")
    return config


def signing_source(config: dict, source: dict) -> dict:
    """Expand the protected signing-only secret without passing it to child builds."""
    result = dict(source)
    secret = result.pop("WINDOWS_SIGNING_SECRET", "")
    if secret:
        try:
            values = json.loads(secret)
        except ValueError as error:
            raise CandidateError("Windows signing credential map is invalid") from error
        if (not isinstance(values, dict) or set(values) - set(config.get("environment", []))
                or any(not isinstance(value, str) for value in values.values())):
            raise CandidateError("Windows signing credential map exceeds its declared scope")
        result.update(values)
    return result


def sign(path: Path, config: dict, canonical_hash: str | None, source: dict, *, runner=subprocess.run) -> None:
    if canonical_hash is not None and not HASH.fullmatch(canonical_hash):
        raise CandidateError("invalid canonical CLI hash")
    if sha256(path) == canonical_hash:
        return
    source = signing_source(config, source)
    env = {name: value for name, value in source.items() if not sensitive_name(name)}
    env.update({name: source[name] for name in config.get("environment", []) if name in source})
    command = [str(path) if value == "%1" else value for value in config["command"]]
    try:
        runner(command, env=env, check=True, capture_output=True, timeout=180)
    except (subprocess.SubprocessError, OSError) as error:
        raise CandidateError("Windows signing failed; no artifacts may be published") from error


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--canonical-sha256")
    group.add_argument("--initial-cli", action="store_true", help="sign the canonical CLI once, before archiving")
    parser.add_argument("artifact", type=Path)
    args = parser.parse_args()
    try:
        sign(args.artifact, configuration(args.config), args.canonical_sha256, dict(os.environ))
    except (CandidateError, OSError, ValueError) as error:
        print(f"desktop Windows signing: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
