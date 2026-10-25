#!/usr/bin/env python3
"""Verify Tauri updater artifacts and the user's current release signing key."""

from __future__ import annotations

import base64
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from typing import Callable, Mapping

from run_clean import DESKTOP_SIGNING, sensitive_name


ROOT = Path(__file__).resolve().parents[2]
MANIFEST = Path(__file__).with_name("updater-signature") / "Cargo.toml"
PUBKEY_PLACEHOLDER = "REPLACE_WITH_RELEASE_UPDATER_PUBLIC_KEY"


class DesktopSigningError(RuntimeError):
    """The current release key or updater archive cannot be verified."""


def release_pubkey(root: Path) -> str:
    config = json.loads((root / "desktop/src-tauri/tauri.conf.json").read_text())
    key = config.get("plugins", {}).get("updater", {}).get("pubkey", "").strip()
    if not key or key == PUBKEY_PLACEHOLDER:
        raise DesktopSigningError("desktop/src-tauri/tauri.conf.json has no release updater public key")
    return key


def _run(command, *, runner: Callable, cwd: Path, env: Mapping[str, str], timeout: int, step: str):
    try:
        runner(command, cwd=cwd, env=env, check=True, capture_output=True, text=True, timeout=timeout)
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, OSError) as error:
        # Signers can print sensitive input on failure; never expose their output.
        raise DesktopSigningError(f"{step} failed or timed out; check the signing key and Rust/Tauri prerequisites") from error


def verify_archive(root: Path, archive: Path, signature: Path, *, environ: Mapping[str, str],
                   runner: Callable = subprocess.run) -> None:
    """Check the actual archive with the same pinned verifier used by Tauri."""
    try:
        key = base64.b64decode(release_pubkey(root), validate=True)
        signed = base64.b64decode(signature.read_text().strip(), validate=True)
    except (ValueError, OSError) as error:
        raise DesktopSigningError("updater public key or signature is invalid") from error
    env = {name: value for name, value in environ.items() if not sensitive_name(name)}
    cache = root / ".tmp/updater-signature"
    cargo = shutil.which("cargo", path=env.get("PATH", "")) or str(Path.home() / ".cargo/bin/cargo")
    _run([cargo, "build", "--quiet", "--offline", "--locked", "--manifest-path", str(MANIFEST),
          "--target-dir", str(cache)], runner=runner, cwd=root, env=env, timeout=120, step="updater verifier build")
    with tempfile.TemporaryDirectory(prefix="rzm-updater-verify-") as directory:
        public_key = Path(directory) / "key.pub"
        public_key.write_bytes(key)
        signature_file = Path(directory) / "archive.sig"
        signature_file.write_bytes(signed)
        _run([str(cache / "debug/rhizome-verify-updater"), str(public_key), str(archive), str(signature_file)],
             runner=runner, cwd=root, env=env, timeout=30, step="updater signature verification")


def check_signing_key(root: Path, *, environ: Mapping[str, str], runner: Callable = subprocess.run) -> None:
    """Future release preflight; callers supply the key only to Tauri's signer."""
    release_pubkey(root)
    if not environ.get(DESKTOP_SIGNING[0], "").strip():
        raise DesktopSigningError("TAURI_SIGNING_PRIVATE_KEY is required for desktop signing preflight")
    env = {name: value for name, value in environ.items()
           if not sensitive_name(name) or name in DESKTOP_SIGNING}
    env.setdefault("TAURI_SIGNING_PRIVATE_KEY_PASSWORD", "")
    with tempfile.TemporaryDirectory(prefix="rzm-signing-probe-") as directory:
        probe = Path(directory) / "probe"
        probe.write_bytes(b"Rhizome updater signing preflight\n")
        _run(["npm", "run", "tauri", "--", "signer", "sign", str(probe)], runner=runner,
             cwd=root / "desktop", env=env, timeout=30, step="desktop signing preflight")
        verify_archive(root, probe, probe.with_suffix(".sig"), environ=environ, runner=runner)


if __name__ == "__main__":
    try:
        check_signing_key(ROOT, environ=os.environ)
    except (DesktopSigningError, ValueError, OSError) as error:
        print(f"desktop signing: {error}", file=sys.stderr)
        raise SystemExit(1)
