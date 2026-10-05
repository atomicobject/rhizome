#!/usr/bin/env python3
"""Bundle Atomic team keys into an internal release build, without modifying Git files.

A release is internal when RZM_INTERNAL_S3_BUCKET is set (release 1Password Environment). Internal
releases require ATOMIC_RHIZOME_KEY, VOYAGE_API_KEY, and TYPESAFE_API_KEY, and
refuse to run unless the GitHub repository is private, because GoReleaser also
publishes the bundled binaries there. Other releases run unchanged.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from typing import Callable, Mapping, Sequence

ROOT = Path(__file__).resolve().parents[2]
BUNDLE_INPUTS = ("ATOMIC_RHIZOME_KEY", "VOYAGE_API_KEY", "TYPESAFE_API_KEY")
BUNDLE_MARKER = "rhizome-internal-keys-v1:"


def public_environment(environment: Mapping[str, str]) -> dict[str, str]:
    """Public builds cannot inherit private mirror settings or compiler overlays."""
    if "-overlay" in environment.get("GOFLAGS", ""):
        raise ValueError("public builds cannot use a Go compiler overlay")
    source = ROOT / "pkg/teamkeys/keys_encrypted.go"
    if not re.search(r'^var encryptedKeys = ""\s*$', source.read_text(), re.MULTILINE):
        raise ValueError("the checked-in credential bundle must be empty")
    result = {key: value for key, value in environment.items()
              if key not in BUNDLE_INPUTS and not key.startswith("RZM_INTERNAL_S3_")}
    # Persisted `go env -w GOFLAGS=...` must not inject an overlay either.
    result["GOENV"] = "off"
    return result


def verify_artifact(path: Path, environment: Mapping[str, str]) -> None:
    """Check the actual public binary or installer before GoReleaser can upload it."""
    if environment.get("RZM_INTERNAL_S3_BUCKET", "").strip():
        require_private_repository()
        return
    public_environment(environment)
    data = path.read_bytes()
    if BUNDLE_MARKER.encode() in data or b".s3.amazonaws.com/releases" in data:
        raise ValueError(f"public artifact contains an internal bundle or mirror URL: {path.name}")
    result = subprocess.run(
        ["gitleaks", "stdin", "--redact=100", "--no-banner", "--ignore-gitleaks-allow",
         "--config", str(ROOT / ".gitleaks.toml")],
        # Gitleaks skips binary MIME types even on stdin. Scan printable strings.
        input=b"\n".join(re.findall(rb"[\x20-\x7e]{4,}", data)), capture_output=True,
    )
    if result.returncode:
        # Do not relay scanner output, including excerpts of binary contents.
        raise ValueError(f"public artifact failed credential scanning: {path.name}")


def repo_visibility() -> str:
    result = subprocess.run(
        ["gh", "repo", "view", "--json", "visibility", "--jq", ".visibility"],
        cwd=ROOT, capture_output=True, text=True,
    )
    return result.stdout.strip() if result.returncode == 0 else ""


def require_private_repository() -> None:
    if repo_visibility() != "PRIVATE":
        raise ValueError("internal releases and artifacts require a private GitHub repository")


def run_release(
    command: Sequence[str],
    environment: Mapping[str, str],
    runner: Callable[..., int] = subprocess.call,
    visibility: Callable[[], str] | None = None,
) -> int:
    build_env = {key: value for key, value in environment.items() if key not in BUNDLE_INPUTS}
    visible = (visibility or repo_visibility)()
    if not environment.get("RZM_INTERNAL_S3_BUCKET", "").strip():
        # A private repository serves Atomic users, so its releases must be
        # internal (keyed and mirrored); only a public repository releases plainly.
        if visible != "PUBLIC":
            raise ValueError(
                "releases from a private (or unverifiable) repository must be internal; "
                "run through ./scripts/with-secrets release so RZM_INTERNAL_S3_BUCKET is set"
            )
        return runner(list(command), env=public_environment(environment))
    if visible != "PRIVATE":
        raise ValueError("internal releases bundle team keys; the GitHub repository must be private")
    if "-overlay" in environment.get("GOFLAGS", ""):
        raise ValueError("credential bundling cannot be combined with another Go overlay")
    source = ROOT / "pkg/teamkeys/keys_encrypted.go"
    if 'var encryptedKeys = ""' not in source.read_text():
        raise ValueError("the checked-in credential bundle must be empty")
    # Generate for the build host, including when the release target differs.
    generator_env = dict(environment)
    for key in ("GOOS", "GOARCH", "GOFLAGS", "CC", "CXX", "CGO_ENABLED"):
        generator_env.pop(key, None)
    generated = subprocess.run(["go", "run", str(ROOT / "scripts/teamkeys/encrypt.go")], env=generator_env, capture_output=True)
    if generated.returncode:
        # Generator errors name variables, never values. Do not relay arbitrary
        # subprocess output because compiler diagnostics can include source.
        raise ValueError("credential generation failed; provide a 32-byte base64 " + ", ".join(BUNDLE_INPUTS))
    with tempfile.TemporaryDirectory(prefix="rzm-release-keys-") as temporary:
        directory = Path(temporary)
        bundle = directory / "keys_encrypted.go"
        bundle.write_bytes(generated.stdout)
        bundle.chmod(0o600)
        overlay = directory / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(source): str(bundle)}}))
        overlay.chmod(0o600)
        if any(character.isspace() for character in str(overlay)):
            raise ValueError("the overlay path contains whitespace, which GOFLAGS cannot carry; set TMPDIR")
        build_env["GOFLAGS"] = (build_env.get("GOFLAGS", "") + " -overlay=" + str(overlay)).strip()
        return runner(list(command), env=build_env)


def check(environment: Mapping[str, str], visibility: Callable[[], str] | None = None) -> None:
    """Fail before any release mutation unless the release mode fits the repository."""
    run_release(["true"], environment, runner=lambda command, env: 0, visibility=visibility)


def main() -> int:
    command = sys.argv[1:]
    if command[:1] == ["--check-artifact"]:
        if len(command) != 2:
            print("usage: release.py --check-artifact <path>", file=sys.stderr)
            return 2
        try:
            verify_artifact(Path(command[1]), os.environ)
        except (ValueError, OSError) as error:
            print(f"release artifacts: {error}", file=sys.stderr)
            return 1
        return 0
    if command == ["--check"]:
        try:
            check(os.environ)
        except (ValueError, OSError) as error:
            print(f"release credentials: {error}", file=sys.stderr)
            return 1
        return 0
    if command[:1] == ["--"]:
        command = command[1:]
    if not command:
        print("usage: release.py <release-build-command> [args...]", file=sys.stderr)
        return 2
    try:
        return run_release(command, dict(os.environ))
    except (ValueError, OSError) as error:
        print(f"release credentials: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
