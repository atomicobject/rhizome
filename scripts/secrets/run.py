#!/usr/bin/env python3
"""Run one command with credentials loaded from a 1Password Environment."""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
DEVELOPMENT = {"VOYAGE_API_KEY", "TYPESAFE_API_KEY"}
RELEASE = {"BREW_GITHUB_TOKEN", "RZM_RELEASE_GITHUB_TOKEN"}
# Internal releases bundle team keys; public releases run without them.
TEAM_KEYS = {"ATOMIC_RHIZOME_KEY", "VOYAGE_API_KEY", "TYPESAFE_API_KEY"}
# Only the internal S3 publish step uses AWS; run_clean.py strips it from builds.
AWS_RELEASE = {"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}
SENSITIVE = re.compile(
    r"(?:^|_)(?:API_KEY|ACCESS_KEY(?:_ID)?|PRIVATE_KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIALS?)(?:$|_)"
)


def sensitive_name(name: str) -> bool:
    return name == "ATOMIC_RHIZOME_KEY" or SENSITIVE.search(name) is not None


def scoped_environment(source: dict[str, str], profile: str) -> dict[str, str]:
    allowed = DEVELOPMENT if profile == "development" else RELEASE | TEAM_KEYS | AWS_RELEASE
    result = {key: value for key, value in source.items() if not sensitive_name(key) or key in allowed}
    if profile == "release":
        # Keep publishing auth out of ordinary gh commands. GoReleaser's adapter
        # maps the dedicated release token back to GITHUB_TOKEN only at publish.
        token = source.get("RZM_RELEASE_GITHUB_TOKEN") or source.get("GITHUB_TOKEN")
        if token:
            result["RZM_RELEASE_GITHUB_TOKEN"] = token
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--loaded", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("profile", choices=("development", "release"))
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command
    if command[:1] == ["--"]:
        command = command[1:]
    if not command:
        parser.error("provide a command to run")
    if args.loaded:
        env = scoped_environment(dict(os.environ), args.profile)
        required = DEVELOPMENT if args.profile == "development" else RELEASE
        missing = sorted(key for key in required if not env.get(key, "").strip())
        if missing:
            parser.error("1Password Environment is missing: " + ", ".join(missing))
        os.execvpe(command[0], command, env)
    environment_id = os.environ.get("RZM_OP_ENVIRONMENT_ID", "").strip()
    if not re.fullmatch(r"[a-z0-9]{26}", environment_id):
        parser.error("set RZM_OP_ENVIRONMENT_ID to the 26-character 1Password Environment ID")
    beta = Path.home() / ".local/bin/op-beta"
    executable = os.environ.get("RZM_OP_BIN") or (str(beta) if beta.is_file() else shutil.which("op"))
    if not executable:
        parser.error("install 1Password CLI with Environment support; see docs/engineering/secrets.md")
    account = os.environ.get("OP_ACCOUNT", "").strip()
    if not account:
        parser.error("set OP_ACCOUNT to the 1Password account used for this Environment")
    # Remove stale shell credentials before resolving the central environment.
    bootstrap_credentials = {"OP_SERVICE_ACCOUNT_TOKEN"}
    env = {key: value for key, value in os.environ.items() if not sensitive_name(key) or key in bootstrap_credentials}
    env["OP_ACCOUNT"] = account
    help_result = subprocess.run([executable, "run", "--help"], capture_output=True, text=True, env=env)
    if help_result.returncode or "--environment " not in help_result.stdout:
        parser.error("this op version lacks Environment support; install the beta CLI and set RZM_OP_BIN")
    os.execvpe(executable, [executable, "run", "--environment", environment_id, "--", sys.executable, str(Path(__file__).resolve()), "--loaded", args.profile, *command], env)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
