#!/usr/bin/env python3
"""Install release signing assets into an isolated GitHub runner Keychain."""

from __future__ import annotations

import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import re
import shlex
import subprocess
import sys
from typing import Mapping
from urllib.request import urlopen


INTERMEDIATE_URL = "https://www.apple.com/certificateauthority/DeveloperIDG2CA.cer"
REQUIRED = ("APPLE_CERTIFICATE", "APPLE_CERTIFICATE_PASSWORD", "APPLE_API_PRIVATE_KEY")


class SigningSetupError(RuntimeError):
    pass


def security(arguments: list[str], *, runner=subprocess.run) -> str:
    # security's interactive mode keeps passwords out of process arguments.
    if any("\n" in arg or "\r" in arg for arg in arguments):
        raise SigningSetupError("Keychain arguments must not contain line breaks")
    command = " ".join('"' + arg.replace("\\", "\\\\").replace('"', '\\"') + '"' for arg in arguments)
    env = {name: os.environ[name] for name in ("PATH", "HOME", "TMPDIR") if name in os.environ}
    result = runner(["/usr/bin/security", "-i"], input=command + "\n", text=True,
                    capture_output=True, env=env)
    if result.returncode or result.stderr.strip():
        raise SigningSetupError("Keychain operation failed: " + arguments[0])
    return result.stdout


def directory(environ: Mapping[str, str]) -> Path:
    temporary = environ.get("RUNNER_TEMP", "")
    if not temporary or not Path(temporary).is_absolute():
        raise SigningSetupError("RUNNER_TEMP must be an absolute runner temporary directory")
    return Path(temporary) / "rhizome-desktop-signing"


def private_file(path: Path, contents: bytes) -> None:
    with path.open("xb") as target:
        path.chmod(0o600)
        target.write(contents)


def download_intermediate() -> bytes:
    with urlopen(INTERMEDIATE_URL, timeout=30) as response:
        return response.read()


def cleanup(environ: Mapping[str, str], *, runner=subprocess.run) -> None:
    root = directory(environ)
    if not root.exists():
        return
    state = root / "search-list.json"
    failure = None
    if state.exists():
        try:
            previous = json.loads(state.read_text(encoding="utf-8"))
            security(["list-keychains", "-d", "user", "-s", *previous], runner=runner)
        except (SigningSetupError, ValueError, OSError) as error:
            failure = error
    keychain = root / "signing.keychain-db"
    if keychain.exists():
        try:
            security(["delete-keychain", str(keychain)], runner=runner)
        except SigningSetupError as error:
            failure = error
    # Remove only this helper's files, including failed or interrupted imports.
    for path in root.iterdir():
        if path.is_file():
            path.unlink()
    root.rmdir()
    if failure:
        raise SigningSetupError("Could not fully restore the runner Keychain state") from failure


def prepare(environ: Mapping[str, str], *, runner=subprocess.run, download=download_intermediate) -> None:
    missing = [name for name in REQUIRED if not environ.get(name, "").strip()]
    if missing:
        raise SigningSetupError("Missing signing secrets: " + ", ".join(missing))
    if not environ.get("GITHUB_ENV"):
        raise SigningSetupError("GITHUB_ENV is required")
    # Decode before touching the Keychain; malformed input must not leave partial state.
    try:
        certificate = base64.b64decode(environ["APPLE_CERTIFICATE"], validate=True)
    except ValueError as error:
        raise SigningSetupError("APPLE_CERTIFICATE must contain base64-encoded p12 data") from error
    if not certificate:
        raise SigningSetupError("APPLE_CERTIFICATE is empty")
    password = environ["APPLE_CERTIFICATE_PASSWORD"]
    if "\n" in password or "\r" in password:
        raise SigningSetupError("The p12 password must not contain line breaks")
    root = directory(environ)
    root.mkdir(mode=0o700)
    keychain = root / "signing.keychain-db"
    try:
        previous = shlex.split(security(["list-keychains", "-d", "user"], runner=runner))
        private_file(root / "search-list.json", json.dumps(previous).encode())
        private_file(root / "certificate.p12", certificate)
        private_file(root / "intermediate.cer", download())
        private_file(root / "notarization.p8", environ["APPLE_API_PRIVATE_KEY"].encode())
        keychain_password = secrets.token_urlsafe(32)
        security(["create-keychain", "-p", keychain_password, str(keychain)], runner=runner)
        security(["set-keychain-settings", "-lut", "21600", str(keychain)], runner=runner)
        security(["unlock-keychain", "-p", keychain_password, str(keychain)], runner=runner)
        security(["import", str(root / "certificate.p12"), "-k", str(keychain),
                  "-P", password, "-T", "/usr/bin/codesign"], runner=runner)
        security(["import", str(root / "intermediate.cer"), "-k", str(keychain)], runner=runner)
        security(["set-key-partition-list", "-S", "apple-tool:,apple:,codesign:", "-s",
                  "-k", keychain_password, str(keychain)], runner=runner)
        security(["list-keychains", "-d", "user", "-s", *previous, str(keychain)], runner=runner)
        identities = security(["find-identity", "-v", "-p", "codesigning", str(keychain)], runner=runner)
        if not re.search(r"\b[1-9]\d* valid identities found", identities):
            raise SigningSetupError("The temporary Keychain has no valid code-signing identity")
        (root / "certificate.p12").unlink()
        (root / "intermediate.cer").unlink()
        with Path(environ["GITHUB_ENV"]).open("a", encoding="utf-8") as output:
            output.write(f"APPLE_API_KEY_PATH={root / 'notarization.p8'}\n")
    except Exception:
        cleanup(environ, runner=runner)
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("prepare", "cleanup"))
    args = parser.parse_args()
    if sys.platform != "darwin" or os.environ.get("GITHUB_ACTIONS") != "true":
        parser.error("this helper runs only on a GitHub-hosted macOS runner")
    try:
        {"prepare": prepare, "cleanup": cleanup}[args.operation](os.environ)
    except Exception:
        # Subprocess/HTTP errors may carry credentials. Report no exception values.
        print(f"CI signing {args.operation} failed; check required secrets and runner Keychain access", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
