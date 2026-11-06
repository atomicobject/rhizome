#!/usr/bin/env python3
"""Verify native candidates and sign one complete updater feed without publishing."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit

from desktop_candidates import CandidateError, REQUIRED_TARGETS, ROOT, TARGETS, identity, read_candidate, sha256, tauri_cli
from run_clean import DESKTOP_SIGNING, sensitive_name
from verify_desktop_signing import DesktopSigningError, release_pubkey, verify_archive


SIGNING_PATH = "TAURI_SIGNING_PRIVATE_KEY_PATH"


def signing_environment(source: dict) -> dict:
    env = {name: value for name, value in source.items() if not sensitive_name(name)}
    if bool(source.get(DESKTOP_SIGNING[0], "").strip()) == bool(source.get(SIGNING_PATH, "").strip()):
        raise CandidateError("coordinator requires exactly one updater signing key source")
    env.update({name: source[name] for name in (*DESKTOP_SIGNING, SIGNING_PATH) if name in source})
    env.setdefault("TAURI_SIGNING_PRIVATE_KEY_PASSWORD", "")
    return env


def fixture_base_url(value: str) -> str:
    url = urlsplit(value)
    if (url.scheme not in {"http", "https"} or url.hostname not in {"localhost", "127.0.0.1", "::1"}
            or url.username is not None or url.password is not None or url.query or url.fragment):
        raise CandidateError("test feed downloads must use an explicit loopback origin")
    return value.rstrip("/")


def sign_payload(path: Path, version: str, *, root: Path, env: dict, runner) -> Path:
    command = [*tauri_cli(root), "signer", "sign", "--app-version", version, str(path)]
    try:
        runner(command, cwd=root / "desktop", env=env, check=True, capture_output=True, text=True, timeout=60)
    except (subprocess.SubprocessError, OSError) as error:
        raise CandidateError("coordinator updater signing failed; no complete assembly staged") from error
    signature = Path(str(path) + ".sig")
    if not signature.is_file():
        raise CandidateError("coordinator signer did not produce the expected signature")
    return signature


def assemble(directories: list[Path], version: str, commit: str, canonical_hashes: dict[str, str], output: Path,
             *, production: bool = True, required_targets=REQUIRED_TARGETS, test_public_key: str | None = None,
             test_base_url: str | None = None, root: Path = ROOT, source=None, runner=subprocess.run,
             verifier=verify_archive, now=lambda: datetime.now(timezone.utc)) -> Path:
    version, commit = identity(version, commit)
    required = frozenset(required_targets)
    if not required or not required.issubset(REQUIRED_TARGETS):
        raise CandidateError("assembly target set is invalid")
    if production and (required != REQUIRED_TARGETS or test_public_key is not None or test_base_url is not None):
        raise CandidateError("production assembly requires every platform and the production feed key/URLs")
    public_key = release_pubkey(root) if test_public_key is None else test_public_key.strip()
    base = (f"https://github.com/atomicobject/rhizome/releases/download/v{version}" if production
            else fixture_base_url(test_base_url or "http://127.0.0.1:9444"))
    candidates = {}
    for directory in directories:
        raw = json.loads((directory / "desktop-candidate.json").read_text())
        if not isinstance(raw, dict):
            raise CandidateError("candidate manifest must be an object")
        target = raw.get("target")
        if target not in required or target in candidates or target not in canonical_hashes:
            raise CandidateError("duplicate, unexpected or unbound candidate target")
        candidates[target] = read_candidate(directory, version=version, commit=commit,
                                            canonical_hash=canonical_hashes[target], production=production)
    if set(candidates) != required or set(canonical_hashes) != required:
        raise CandidateError("desktop assembly is missing a required platform or canonical CLI hash")
    source = dict(os.environ) if source is None else dict(source)
    clean_env = {name: value for name, value in source.items() if not sensitive_name(name)}
    receipt = {"schemaVersion": 1, "version": version, "sourceCommit": commit, "production": production,
               "publicKeySha256": hashlib.sha256(public_key.encode()).hexdigest(),
               "packages": {target: manifest["artifact"] for target, (manifest, _) in sorted(candidates.items())},
               "canonicalCliSha256": canonical_hashes,
               "candidates": {target: manifest for target, (manifest, _) in sorted(candidates.items())},
               "updaterVerification": {target: "verified" for target in sorted(candidates)}}
    output = output.resolve()
    if output.exists():
        if json.loads((output / "desktop-assembly.json").read_text()) != receipt:
            raise CandidateError("existing assembly differs; refuse to overwrite a prior candidate")
        for manifest, _ in candidates.values():
            path = output / manifest["artifact"]["file"]
            if sha256(path) != manifest["artifact"]["sha256"]:
                raise CandidateError("existing assembly artifact changed")
            verifier(root, path, Path(str(path) + ".sig"), environ=clean_env, runner=runner, public_key=public_key)
        feed = json.loads((output / "latest.json").read_text())
        expected_platforms = {target: {"signature": (output / (manifest["artifact"]["file"] + ".sig")).read_text().strip(),
                                       "url": f"{base}/{manifest['artifact']['file']}"}
                              for target, (manifest, _) in candidates.items()}
        if feed.get("platforms") != expected_platforms or feed.get("version") != version:
            raise CandidateError("existing assembly feed is incomplete")
        return output / "latest.json"
    env = signing_environment(source)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".desktop-assembly-", dir=output.parent) as temporary:
        staging = Path(temporary) / "complete"
        staging.mkdir()
        platforms = {}
        for target, (manifest, package) in sorted(candidates.items()):
            artifact = staging / manifest["artifact"]["file"]
            shutil.copyfile(package, artifact)
            existing_signature = Path(str(package) + ".sig")
            if target == "darwin-aarch64" and existing_signature.is_file():
                signature = Path(str(artifact) + ".sig")
                shutil.copyfile(existing_signature, signature)
            else:
                signature = sign_payload(artifact, version, root=root, env=env, runner=runner)
            verifier(root, artifact, signature, environ=clean_env, runner=runner, public_key=public_key)
            platforms[target] = {"signature": signature.read_text().strip(), "url": f"{base}/{artifact.name}"}
        feed = {"version": version, "notes": "", "pub_date": now().astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
                "platforms": platforms}
        (staging / "latest.json").write_text(json.dumps(feed, indent=2) + "\n")
        (staging / "desktop-assembly.json").write_text(json.dumps(receipt, indent=2) + "\n")
        staging.rename(output)
    return output / "latest.json"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, action="append", required=True)
    parser.add_argument("--canonical-cli-hashes", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--test-public-key", type=Path)
    parser.add_argument("--test-base-url")
    parser.add_argument("--test-target", action="append", choices=sorted(TARGETS))
    args = parser.parse_args()
    candidate_mode = args.test_public_key is not None
    try:
        if not candidate_mode and (args.test_base_url is not None or args.test_target is not None):
            raise CandidateError("test feed options require an explicit test public key")
        print(assemble(args.candidate, args.version, args.commit, json.loads(args.canonical_cli_hashes.read_text()),
                       args.output, production=not candidate_mode,
                       required_targets=args.test_target or REQUIRED_TARGETS,
                       test_public_key=args.test_public_key.read_text() if candidate_mode else None,
                       test_base_url=args.test_base_url))
    except (CandidateError, DesktopSigningError, OSError, ValueError) as error:
        print(f"desktop assembly: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
