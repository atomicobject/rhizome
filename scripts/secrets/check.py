#!/usr/bin/env python3
"""Scan the Git index and new history for credentials.

Local credential values (.env files and sensitive environment variables) are
read only to block staged files that contain them; values are never printed.
"""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
# Existing history is retained by explicit user instruction. This is a history
# boundary, not an allowlist: every current tracked file is always scanned.
HISTORY_FLOOR = "961e3b41feb9e082985357acb710f7c6ab243b54"


def forbidden_path(name: str) -> bool:
    path = Path(name)
    return path.name == ".env" or (path.name.startswith(".env.") and path.name != ".env.example")


# Short values such as "1" or "true" would match everywhere; the shortest
# private value (the 10-character legacy mirror suffix) must still count.
MIN_SECRET_LENGTH = 8
SENSITIVE_NAME = re.compile(
    r"(?:^|_)(?:API_KEY|ACCESS_KEY(?:_ID)?|PRIVATE_KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIALS?)(?:$|_)"
)


def sensitive_name(name: str) -> bool:
    # The internal mirror location is private too, though not a credential.
    return name == "ATOMIC_RHIZOME_KEY" or name.startswith("RZM_INTERNAL_S3_") or SENSITIVE_NAME.search(name) is not None


def parse_env_file(text: str) -> dict[str, str]:
    values = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.removeprefix("export ").split("=", 1)
        values[name.strip()] = value.strip().strip("'\"")
    return values


def local_secret_values(env_files: list[Path], environ: dict[str, str]) -> dict[str, str]:
    """Map each local credential value to the variable name that holds it."""
    candidates = [(name, value) for name, value in environ.items() if sensitive_name(name)]
    for env_file in env_files:
        if env_file.is_file():
            # Every .env value counts: the file exists to hold local secrets.
            candidates.extend(parse_env_file(env_file.read_text(errors="replace")).items())
    return {value: name for name, value in candidates if len(value) >= MIN_SECRET_LENGTH}


def leaked_values(root: Path, names: list[str], secrets: dict[str, str]) -> list[str]:
    """Return "file (VARIABLE)" for each file under root containing a secret value."""
    findings = []
    for name in names:
        path = root / name
        if not path.is_file():
            continue
        data = path.read_bytes()
        findings.extend(f"{name} ({var})" for value, var in secrets.items() if value.encode() in data)
    return sorted(findings)


def env_files() -> list[Path]:
    common = subprocess.check_output(["git", "rev-parse", "--path-format=absolute", "--git-common-dir"], cwd=ROOT)
    # Worktrees usually keep .env only in the main checkout.
    return sorted({ROOT / ".env", Path(common.decode().strip()).parent / ".env"})


def history_log_opts() -> str:
    ancestor = subprocess.run(
        ["git", "merge-base", "--is-ancestor", HISTORY_FLOOR, "HEAD"], cwd=ROOT
    )
    if ancestor.returncode == 0:
        return HISTORY_FLOOR + "..HEAD"
    # A fresh public repository may not contain the private-history boundary.
    # Scan every reachable ref instead of weakening or skipping history checks.
    return "--all"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--history", action="store_true")
    args = parser.parse_args()
    executable = shutil.which("gitleaks")
    if not executable:
        parser.error("install gitleaks (brew install gitleaks); credential checks cannot be skipped")
    names = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode().split("\0")
    forbidden = sorted(name for name in names if name and forbidden_path(name))
    if forbidden:
        print("Do not track credential-bearing paths: " + ", ".join(forbidden), file=sys.stderr)
        return 1
    flags = ["--redact=100", "--no-banner", "--ignore-gitleaks-allow", "--config", str(ROOT / ".gitleaks.toml")]
    with tempfile.TemporaryDirectory(prefix="rzm-secret-check-") as temporary:
        subprocess.run(["git", "checkout-index", "--all", "--prefix=" + temporary + "/"], cwd=ROOT, check=True)
        staged = subprocess.check_output(
            ["git", "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR"], cwd=ROOT
        ).decode().split("\0")
        leaks = leaked_values(Path(temporary), [n for n in staged if n], local_secret_values(env_files(), dict(os.environ)))
        if leaks:
            print("Staged files contain local credential values: " + ", ".join(leaks), file=sys.stderr)
            return 1
        bundle = Path(temporary) / "pkg/teamkeys/keys_encrypted.go"
        if bundle.is_file() and 'var encryptedKeys = ""' not in bundle.read_text():
            print("The tracked credential bundle must be empty; release builds supply it through an overlay.", file=sys.stderr)
            return 1
        result = subprocess.call([executable, "dir", *flags, temporary], cwd=ROOT)
        if result:
            return result
    if args.history:
        return subprocess.call([executable, "git", *flags, "--log-opts=" + history_log_opts(), str(ROOT)], cwd=ROOT)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
