#!/usr/bin/env python3
"""Create an isolated updater test key/overlay; never alter production settings."""

from __future__ import annotations

import argparse
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit

from assemble_desktop import fixture_base_url
from desktop_candidates import CandidateError, ROOT, tauri_cli
from run_clean import sensitive_name


def read_overlay(path: Path) -> dict:
    overlay = json.loads(path.read_text())
    if (not isinstance(overlay, dict) or set(overlay) != {"plugins"}
            or not isinstance(overlay["plugins"], dict) or set(overlay["plugins"]) != {"updater"}):
        raise CandidateError("test overlay may change only updater endpoint/public key")
    updater = overlay["plugins"]["updater"]
    if (not isinstance(updater, dict) or set(updater) not in
            ({"endpoints", "pubkey"}, {"endpoints", "pubkey", "dangerousInsecureTransportProtocol"})):
        raise CandidateError("test overlay includes an unapproved updater setting")
    endpoints = updater["endpoints"]
    if not isinstance(endpoints, list) or len(endpoints) != 1:
        raise CandidateError("test overlay needs exactly one loopback endpoint")
    fixture_base_url(endpoints[0])
    try:
        key = base64.b64decode(updater["pubkey"], validate=True)
    except (ValueError, TypeError) as error:
        raise CandidateError("test updater public key is invalid") from error
    if not key or (urlsplit(endpoints[0]).scheme == "http") != (updater.get("dangerousInsecureTransportProtocol") is True):
        raise CandidateError("fixture-only transport setting must match the loopback endpoint")
    return overlay


def prepare(directory: Path, endpoint: str, *, root: Path = ROOT, source=None, runner=subprocess.run) -> Path:
    fixture_base_url(endpoint)
    if directory.exists():
        raise CandidateError("test-key directory already exists; refuse to overwrite key material")
    directory.mkdir(parents=True, mode=0o700)
    private = directory.resolve() / "updater"
    env = {name: value for name, value in (dict(os.environ) if source is None else source).items()
           if not sensitive_name(name)}
    env.update({"CI": "true", "TAURI_SIGNING_PRIVATE_KEY_PASSWORD": ""})
    try:
        runner([*tauri_cli(root), "signer", "generate", "--ci", "--write-keys", str(private)],
               cwd=root / "desktop", env=env, check=True, capture_output=True, timeout=60)
    except (subprocess.SubprocessError, OSError) as error:
        raise CandidateError("ephemeral updater-key generation failed") from error
    private.chmod(0o600)
    public = Path(str(private) + ".pub")
    updater = {"endpoints": [endpoint], "pubkey": public.read_text().strip()}
    if urlsplit(endpoint).scheme == "http":
        updater["dangerousInsecureTransportProtocol"] = True
    overlay = directory / "updater-test.conf.json"
    overlay.write_text(json.dumps({"plugins": {"updater": updater}}, indent=2) + "\n")
    read_overlay(overlay)
    return overlay


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--endpoint", default="http://127.0.0.1:9444/latest.json")
    args = parser.parse_args()
    try:
        print(prepare(args.output, args.endpoint))
    except (CandidateError, OSError, ValueError) as error:
        print(f"desktop updater fixture: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
