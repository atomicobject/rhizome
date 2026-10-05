#!/usr/bin/env python3
"""Scope the release PAT to GoReleaser instead of every GitHub client."""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import sys
from typing import Callable, Mapping, Sequence

from run_clean import scoped_environment


PUBLISH_TOKEN_ENV = "RZM_RELEASE_GITHUB_TOKEN"
# Internal releases bundle these; scripts/teamkeys/release.py consumes them and
# never passes them to GoReleaser.
TEAM_KEY_INPUTS = ("ATOMIC_RHIZOME_KEY", "VOYAGE_API_KEY", "TYPESAFE_API_KEY")


class PublishCredentialError(RuntimeError):
    """The release-only GitHub credential is missing or publishing failed."""


def publish_command(repo_root: Path, notes_path: Path) -> tuple[str, ...]:
    script = Path(repo_root) / "scripts/release/release_publish.py"
    return (sys.executable, str(script), "--release-notes", str(notes_path))


def require_publish_token(environ: Mapping[str, str]) -> str:
    token = environ.get(PUBLISH_TOKEN_ENV, "").strip()
    if not token:
        raise PublishCredentialError(
            f"set {PUBLISH_TOKEN_ENV} to the GitHub token used by GoReleaser"
        )
    return token


def publish_github_release(
    notes_path: str,
    *,
    environ: Mapping[str, str] | None = None,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> None:
    source = os.environ if environ is None else environ
    publish_source = dict(source)
    publish_source["GITHUB_TOKEN"] = require_publish_token(source)
    publish_env = scoped_environment(publish_source, "publish")
    publish_env.update({key: source[key] for key in TEAM_KEY_INPUTS if key in source})
    bundler = Path(__file__).resolve().parents[1] / "teamkeys/release.py"
    wrapper = Path(__file__).with_name("run_clean.py")
    command = (
        sys.executable, str(bundler), sys.executable, str(wrapper), "publish", "--",
        "goreleaser", "release", "--clean", "--release-notes", notes_path,
    )
    result = runner(command, env=publish_env, capture_output=True, text=True)
    if result.returncode:
        raise PublishCredentialError(
            result.stderr.strip() or result.stdout.strip() or "GoReleaser publish failed"
        )
    if result.stdout:
        print(result.stdout, end="")


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Publish a GitHub release with GoReleaser.")
    parser.add_argument("--release-notes", required=True)
    args = parser.parse_args(argv)
    try:
        publish_github_release(args.release_notes)
    except PublishCredentialError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
