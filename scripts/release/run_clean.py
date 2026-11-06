#!/usr/bin/env python3
"""Run release build or publish commands with a minimal credential scope."""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
from typing import Mapping, Sequence

from apple_signing import release_credentials, SIGNING_FILE

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from scripts.teamkeys.release import public_environment, require_private_repository


SENSITIVE = re.compile(
    r"(?:^|_)(?:API_KEY|ACCESS_KEY(?:_ID)?|PRIVATE_KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIALS?)(?:$|_)"
)
PUBLISH_ALLOWED = {"GITHUB_TOKEN", "BREW_GITHUB_TOKEN"}
# The updater probe receives these directly; GoReleaser uses an owner-only file.
DESKTOP_SIGNING = ("TAURI_SIGNING_PRIVATE_KEY", "TAURI_SIGNING_PRIVATE_KEY_PASSWORD")


def sensitive_name(name: str) -> bool:
    return (
        name == "ATOMIC_RHIZOME_KEY"
        or name == SIGNING_FILE
        or name.startswith(("AWS_", "OP_", "APPLE_"))
        or SENSITIVE.search(name) is not None
    )


def scoped_environment(source: Mapping[str, str], mode: str) -> dict[str, str]:
    if source.get("RZM_INTERNAL_S3_BUCKET", "").strip():
        require_private_repository()
    else:
        source = public_environment(source)
    desktop = source.get("RZM_RELEASE_DESKTOP") == "1" and not source.get("RZM_INTERNAL_S3_BUCKET", "").strip()
    allowed = ({"GITHUB_TOKEN"} if desktop else PUBLISH_ALLOWED) if mode == "publish" else set()
    result = {
        name: value
        for name, value in source.items()
        if not sensitive_name(name) or name in allowed
    }
    if mode == "publish":
        github_token = source.get("RZM_RELEASE_GITHUB_TOKEN") or source.get("GITHUB_TOKEN")
        if github_token:
            result["GITHUB_TOKEN"] = github_token
        brew_token = source.get("BREW_GITHUB_TOKEN")
        if brew_token and "BREW_GITHUB_TOKEN" in allowed:
            result["BREW_GITHUB_TOKEN"] = brew_token
    return result


def run(mode: str, command: Sequence[str], source: Mapping[str, str] | None = None) -> int:
    if not command:
        raise ValueError("provide a command to run")
    source = os.environ if source is None else source
    environment = scoped_environment(source, mode)
    if Path(command[0]).name == "goreleaser":
        with release_credentials(source, environment) as signing_env:
            return _run(mode, command, signing_env)
    return _run(mode, command, environment)


def _run(mode, command, environment):
    if mode == "publish":
        desktop = environment.get("RZM_RELEASE_DESKTOP") == "1" and not environment.get("RZM_INTERNAL_S3_BUCKET", "").strip()
        required = {"GITHUB_TOKEN"} if desktop else PUBLISH_ALLOWED
        missing = sorted(name for name in required if not environment.get(name, "").strip())
        if missing:
            raise ValueError("missing release credential environment: " + ", ".join(missing))
    return subprocess.run(command, env=environment).returncode


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("build", "publish"))
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args(argv)
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    try:
        return run(args.mode, command)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
