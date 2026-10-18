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
# Public releases sign the desktop updater archive; run_clean.py passes these
# only to GoReleaser.
DESKTOP_SIGNING = {"TAURI_SIGNING_PRIVATE_KEY", "TAURI_SIGNING_PRIVATE_KEY_PASSWORD"}
SENSITIVE = re.compile(
    r"(?:^|_)(?:API_KEY|ACCESS_KEY(?:_ID)?|PRIVATE_KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIALS?)(?:$|_)"
)


def sensitive_name(name: str) -> bool:
    return name == "ATOMIC_RHIZOME_KEY" or SENSITIVE.search(name) is not None


def scoped_environment(source: dict[str, str], profile: str) -> dict[str, str]:
    allowed = DEVELOPMENT if profile == "development" else RELEASE | TEAM_KEYS | AWS_RELEASE | DESKTOP_SIGNING
    result = {key: value for key, value in source.items() if not sensitive_name(key) or key in allowed}
    if profile == "release":
        # Keep publishing auth out of ordinary gh commands. GoReleaser's adapter
        # maps the dedicated release token back to GITHUB_TOKEN only at publish.
        token = source.get("RZM_RELEASE_GITHUB_TOKEN") or source.get("GITHUB_TOKEN")
        if token:
            result["RZM_RELEASE_GITHUB_TOKEN"] = token
    return result


# Development API keys may be copied into the login keychain, so routine runs
# need no 1Password approval. Release credentials never are.
KEYCHAIN_SERVICE = "rhizome-development"
KEYCHAIN_VALUE = re.compile(r"[A-Za-z0-9._~+/=:-]+")


def keychain_values(run=subprocess.run) -> dict[str, str] | None:
    """The development keys stored by sync-keychain, or None if any is missing."""
    values = {}
    for name in sorted(DEVELOPMENT):
        try:
            result = run(["security", "find-generic-password", "-s", KEYCHAIN_SERVICE, "-a", name, "-w"], capture_output=True, text=True)
        except FileNotFoundError:
            return None
        value = result.stdout.strip() if result.returncode == 0 else ""
        if not value:
            return None
        values[name] = value
    return values


def keychain_store_script(values: dict[str, str]) -> str:
    """Commands for `security -i`, which reads them on standard input so no
    value appears in a process argument list."""
    lines = []
    for name in sorted(values):
        value = values[name].strip()
        if not KEYCHAIN_VALUE.fullmatch(value):
            raise ValueError(f"{name} has characters the keychain import does not accept")
        lines.append(f'add-generic-password -U -s {KEYCHAIN_SERVICE} -a {name} -w "{value}"')
    return "\n".join(lines) + "\n"


def environment_id(profile: str, environ: dict[str, str]) -> str:
    """Release credentials may live in their own Environment, so routine
    development runs never unlock publishing tokens or the updater key."""
    if profile == "release" and environ.get("RZM_OP_RELEASE_ENVIRONMENT_ID", "").strip():
        return environ["RZM_OP_RELEASE_ENVIRONMENT_ID"].strip()
    return environ.get("RZM_OP_ENVIRONMENT_ID", "").strip()


def op_executable(environ: dict[str, str]) -> str | None:
    """1Password CLI 2.41.0 supports Environments and shares one desktop app
    authorization across repeated runs, so the PATH copy is preferred."""
    return environ.get("RZM_OP_BIN") or shutil.which("op")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--loaded", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("profile", choices=("development", "release", "sync-keychain"))
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command
    if command[:1] == ["--"]:
        command = command[1:]
    if not command and args.profile != "sync-keychain":
        parser.error("provide a command to run")
    if args.loaded and args.profile == "sync-keychain":
        values = {key: os.environ.get(key, "") for key in DEVELOPMENT}
        missing = sorted(key for key, value in values.items() if not value.strip())
        if missing:
            parser.error("1Password Environment is missing: " + ", ".join(missing))
        try:
            script = keychain_store_script(values)
        except ValueError as error:
            parser.error(str(error))
        result = subprocess.run(["security", "-i"], input=script, capture_output=True, text=True)
        if result.returncode:
            parser.error("could not write the login keychain")
        print(f"Stored {', '.join(sorted(values))} in the login keychain ({KEYCHAIN_SERVICE}).")
        return 0
    if args.loaded:
        env = scoped_environment(dict(os.environ), args.profile)
        required = DEVELOPMENT if args.profile == "development" else RELEASE
        missing = sorted(key for key in required if not env.get(key, "").strip())
        if missing:
            parser.error("1Password Environment is missing: " + ", ".join(missing))
        os.execvpe(command[0], command, env)
    if args.profile == "development":
        stored = keychain_values()
        if stored:
            env = {key: value for key, value in os.environ.items() if not sensitive_name(key)}
            os.execvpe(command[0], command, scoped_environment({**env, **stored}, "development"))
        print("with-secrets: API keys are not in the login keychain; ./scripts/with-secrets sync-keychain stops these 1Password prompts.", file=sys.stderr)
    selected = environment_id(args.profile, dict(os.environ))
    if not re.fullmatch(r"[a-z0-9]{26}", selected):
        variable = "RZM_OP_RELEASE_ENVIRONMENT_ID or RZM_OP_ENVIRONMENT_ID" if args.profile == "release" else "RZM_OP_ENVIRONMENT_ID"
        parser.error(f"set {variable} to the 26-character 1Password Environment ID")
    executable = op_executable(dict(os.environ))
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
        parser.error("this op version lacks Environment support; update 1Password CLI to 2.41.0 or newer")
    os.execvpe(executable, [executable, "run", "--environment", selected, "--", sys.executable, str(Path(__file__).resolve()), "--loaded", args.profile, *command], env)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
