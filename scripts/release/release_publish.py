#!/usr/bin/env python3
"""Scope the release PAT to GoReleaser instead of every GitHub client."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from typing import Callable, Mapping, Sequence

from run_clean import DESKTOP_SIGNING, scoped_environment
from apple_signing import APPLE_SIGNING
from release_gateway import GatewayError
from release_gateway_source import SourceError
from release_publication import PublicationError, gate_direct_publication, tagged_release_identity
from release_advertise import AdvertisementError, advertisement_environment, advertisement_resume_available
from prepare_native_desktop import ensure_candidates, verify_prepared
from desktop_candidates import CandidateError
from release_homebrew import HomebrewError, homebrew_environment


PUBLISH_TOKEN_ENV = "RZM_RELEASE_GITHUB_TOKEN"
# Internal releases bundle these; scripts/teamkeys/release.py consumes them and
# never passes them to GoReleaser.
TEAM_KEY_INPUTS = ("ATOMIC_RHIZOME_KEY", "VOYAGE_API_KEY", "TYPESAFE_API_KEY")


class PublishCredentialError(RuntimeError):
    """The release-only GitHub credential is missing or publishing failed."""


def publish_command(repo_root: Path, notes_path: Path, receipt_path: Path, *, desktop_candidates: Path | None = None,
                    desktop_receipt: Path | None = None, advertisement_receipt: Path | None = None) -> tuple[str, ...]:
    script = Path(repo_root) / "scripts/release/release_publish.py"
    return (sys.executable, str(script), "--release-notes", str(notes_path), "--gateway-receipt", str(receipt_path),
            *(("--desktop-candidates", str(desktop_candidates)) if desktop_candidates is not None else ()),
            *(("--desktop-receipt", str(desktop_receipt)) if desktop_receipt is not None else ()),
            *(("--advertisement-receipt", str(advertisement_receipt)) if advertisement_receipt is not None else ()))


def require_publish_token(environ: Mapping[str, str]) -> str:
    token = environ.get(PUBLISH_TOKEN_ENV, "").strip()
    if not token:
        raise PublishCredentialError(
            f"set {PUBLISH_TOKEN_ENV} to the GitHub token used by GoReleaser"
        )
    return token


def publish_github_release(
    notes_path: str | None = None,
    *,
    environ: Mapping[str, str] | None = None,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    repo_root: Path | None = None,
    gateway_receipt: Path | None = None,
    desktop_candidates: Path | None = None,
    desktop_receipt: Path | None = None,
    advertisement_receipt: Path | None = None,
    gate: Callable[..., dict] = gate_direct_publication,
    release_identity: Callable = tagged_release_identity,
    prepare: Callable = ensure_candidates,
    verify: Callable = verify_prepared,
) -> None:
    source = os.environ if environ is None else environ
    require_publish_token(source)
    repo_root = Path(repo_root or Path.cwd()).resolve()
    internal = bool(source.get("RZM_INTERNAL_S3_BUCKET", "").strip())
    resume = False
    if not internal:
        homebrew_environment(source)
        if (desktop_candidates is None) != (desktop_receipt is None):
            raise PublishCredentialError("provide both desktop candidates and their receipt")
        # Packaging may have left its generated entry after a verified attempt.
        # The strict first-upload gate below still requires a clean checkout.
        version, commit = release_identity(repo_root, allow_generated=True)
        explicit_candidates = desktop_candidates is not None
        desktop_receipt = Path(desktop_receipt or repo_root / ".tmp" / f"native-desktop-{commit}.json")
        desktop_candidates = Path(desktop_candidates or repo_root / ".tmp" / f"native-desktop-{commit}")
        advertisement_receipt = Path(advertisement_receipt or repo_root / ".tmp" / f"advertisement-{commit}.json")
        if advertisement_receipt.exists():
            resume = advertisement_resume_available(advertisement_receipt, expected_commit=commit, expected_version=version)
            if not resume:
                raise PublishCredentialError("advertisement receipt is invalid or stale; inspect it before publishing")
        upload_marker = advertisement_receipt.with_suffix(".upload-started.json")
        if upload_marker.exists() and not resume:
            raise PublishCredentialError("an upload started without verified completion; inspect its draft before retrying")
        if not explicit_candidates:
            prepare(repo_root, commit, version, desktop_receipt, desktop_candidates)
        verify(repo_root, commit, version, desktop_receipt, desktop_candidates)
    gate_result = gate(repo_root, internal=internal, receipt_path=gateway_receipt, allow_generated=resume)
    effective_gateway_receipt = gateway_receipt or (gate_result or {}).get("receiptPath")
    publish_source = dict(source)
    publish_source["GITHUB_TOKEN"] = require_publish_token(source)
    publish_env = scoped_environment(publish_source, "publish")
    publish_env.update({key: source[key] for key in (*TEAM_KEY_INPUTS, *DESKTOP_SIGNING, *APPLE_SIGNING) if key in source})
    if not internal:
        # A public release must ship the desktop app; the build hook fails without it.
        publish_env["RZM_RELEASE_DESKTOP"] = "1"
        publish_env.pop("BREW_GITHUB_TOKEN", None)
        publish_env["RZM_DESKTOP_CANDIDATES_DIR"] = str(desktop_candidates.resolve())
        publish_env["RZM_DESKTOP_CANDIDATES_RECEIPT"] = str(desktop_receipt.resolve())
    bundler = Path(__file__).resolve().parents[1] / "teamkeys/release.py"
    wrapper = Path(__file__).with_name("run_clean.py")
    command = (
        sys.executable, str(bundler), sys.executable, str(wrapper), "publish", "--",
        "goreleaser", "release", "--clean", *(("--draft",) if not internal else ()),
        *(("--release-notes", notes_path) if notes_path else ()),
    )
    if not resume:
        if not internal:
            upload_marker.parent.mkdir(parents=True, exist_ok=True)
            with upload_marker.open("x") as marker:
                json.dump({"schemaVersion": 1, "sourceCommit": commit, "version": version,
                           "advertisementReceipt": str(advertisement_receipt)}, marker, sort_keys=True)
                marker.flush()
                os.fsync(marker.fileno())
        result = runner(command, cwd=repo_root, env=publish_env, capture_output=True, text=True)
        if result.returncode:
            raise PublishCredentialError(result.stderr.strip() or result.stdout.strip() or "GoReleaser publish failed")
        if result.stdout:
            print(result.stdout, end="")
    if not internal:
        command = (sys.executable, str(Path(__file__).with_name("release_advertise.py")),
                   "--repo-root", str(repo_root), "--receipt", str(advertisement_receipt),
                   "--version", version, "--commit", commit,
                   *(("--gateway-receipt", str(effective_gateway_receipt)) if effective_gateway_receipt is not None else ()),
                   *(("--resume",) if resume else ()))
        result = runner(command, cwd=repo_root, env=advertisement_environment(source), capture_output=True, text=True)
        if result.returncode:
            raise PublishCredentialError("release advertisement failed; inspect its receipt before resuming")
        if result.stdout:
            print(result.stdout, end="")
        # The first gateway check protects uploads. Recheck before handing the tap
        # worker a live receipt after the potentially long build/advertisement.
        tap_gate = gate(repo_root, internal=False, receipt_path=Path(effective_gateway_receipt)
                        if effective_gateway_receipt else None, allow_generated=True)
        tap_receipt = effective_gateway_receipt or (tap_gate or {}).get("receiptPath")
        command = (sys.executable, str(Path(__file__).with_name("release_homebrew.py")), "--apply",
                   "--repo-root", str(repo_root), "--advertisement-receipt", str(advertisement_receipt),
                   "--version", version, "--commit", commit,
                   *(("--gateway-receipt", str(tap_receipt)) if tap_receipt is not None else ()))
        result = runner(command, cwd=repo_root, env=homebrew_environment(source), capture_output=True, text=True)
        if result.returncode:
            raise PublishCredentialError("GitHub release is verified; tap update failed, resume its advertisement receipt")


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Publish a GitHub release with GoReleaser.")
    parser.add_argument("--release-notes")
    parser.add_argument("--gateway-receipt", type=Path)
    parser.add_argument("--desktop-candidates", type=Path)
    parser.add_argument("--desktop-receipt", type=Path)
    parser.add_argument("--advertisement-receipt", type=Path)
    args = parser.parse_args(argv)
    try:
        publish_github_release(args.release_notes, gateway_receipt=args.gateway_receipt,
                               desktop_candidates=args.desktop_candidates, desktop_receipt=args.desktop_receipt,
                               advertisement_receipt=args.advertisement_receipt)
    except (PublishCredentialError, PublicationError, GatewayError, SourceError, AdvertisementError,
            CandidateError, HomebrewError, OSError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
