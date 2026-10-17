#!/usr/bin/env python3
"""Build the macOS desktop app around GoReleaser's darwin/arm64 rzm (SPEC-0122).

GoReleaser runs this as a post-build hook for every rzm-darwin binary. Only the
arm64 binary of a public snapshot or a public release (RZM_RELEASE_DESKTOP=1)
builds the app; everything else publishes the CLI only.
"""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
from typing import Any, Callable, Mapping, Sequence

from run_clean import sensitive_name


ROOT = Path(__file__).resolve().parents[2]
OUTPUT_DIR = Path(".tmp/goreleaser-extra")
BUNDLE_DIR = Path("desktop/src-tauri/target/release/bundle")
DMG = "Rhizome-macos-arm64.dmg"
ARCHIVE = "Rhizome-macos-arm64.app.tar.gz"
SIGNATURE = ARCHIVE + ".sig"
FEED = "latest.json"
OUTPUTS = (DMG, ARCHIVE, SIGNATURE, FEED)
DOWNLOAD_URL = "https://github.com/atomicobject/rhizome/releases/download/v{version}/" + ARCHIVE
SIGNING_KEY = "TAURI_SIGNING_PRIVATE_KEY"
# The Tauri CLI reads both from its environment to sign the updater archive.
SIGNING_ENV = (SIGNING_KEY, "TAURI_SIGNING_PRIVATE_KEY_PASSWORD")
NO_UPDATER_ARTIFACTS = '{"bundle":{"createUpdaterArtifacts":false}}'

Runner = Callable[..., Any]


class DesktopBuildError(RuntimeError):
    """The desktop app could not be built or staged."""


def skip_reason(arch: str, snapshot: bool, environ: Mapping[str, str]) -> str | None:
    if arch != "arm64":
        return f"darwin/{arch} has no desktop app"
    if environ.get("RZM_INTERNAL_S3_BUCKET", "").strip():
        return "internal releases publish the CLI only"
    if not snapshot and environ.get("RZM_RELEASE_DESKTOP") != "1":
        return "RZM_RELEASE_DESKTOP is not set, so this release publishes the CLI only"
    return None


def signs_updater(environ: Mapping[str, str]) -> bool:
    if environ.get(SIGNING_KEY, "").strip():
        return True
    if environ.get("RZM_RELEASE_DESKTOP") == "1":
        raise DesktopBuildError(
            f"{SIGNING_KEY} is required to publish the desktop app; "
            "run the release through ./scripts/with-secrets release"
        )
    return False


PUBKEY_PLACEHOLDER = "REPLACE_WITH_RELEASE_UPDATER_PUBLIC_KEY"


def require_release_pubkey(root: Path) -> None:
    """Installed apps verify updates against the committed key, so a release
    must not ship until the placeholder is replaced by the release key."""
    config = json.loads((root / "desktop/src-tauri/tauri.conf.json").read_text())
    pubkey = config.get("plugins", {}).get("updater", {}).get("pubkey", "")
    if not pubkey.strip() or pubkey == PUBKEY_PLACEHOLDER:
        raise DesktopBuildError(
            "desktop/src-tauri/tauri.conf.json has no release updater public key; "
            "see Updater signing key in docs/RELEASING.md"
        )


def tauri_command(version: str, *, updater: bool) -> list[str]:
    command = [
        "npm", "run", "tauri", "--", "build", "--bundles", "app,dmg",
        "--config", "src-tauri/tauri.release.conf.json",
        "--config", json.dumps({"version": version}),
    ]
    if not updater:
        command += ["--config", NO_UPDATER_ARTIFACTS]
    return command


def bundle_outputs(bundle_dir: Path, *, updater: bool) -> dict[str, Path]:
    """Map stable release asset names to the files Tauri produced."""
    dmgs = sorted((bundle_dir / "dmg").glob("*.dmg"))
    if len(dmgs) != 1:
        raise DesktopBuildError(f"expected one DMG in {bundle_dir / 'dmg'}, found {len(dmgs)}")
    outputs = {DMG: dmgs[0]}
    if updater:
        archive = bundle_dir / "macos/Rhizome.app.tar.gz"
        outputs[ARCHIVE] = archive
        outputs[SIGNATURE] = archive.with_name(archive.name + ".sig")
    missing = [str(path) for path in outputs.values() if not path.is_file()]
    if missing:
        raise DesktopBuildError("desktop build did not produce " + ", ".join(missing))
    return outputs


def update_feed(version: str, signature: str, published: datetime) -> dict[str, Any]:
    """Tauri's static updater format, pointing at this release's archive."""
    version = version.removeprefix("v")
    return {
        "version": version,
        "notes": "",
        "pub_date": published.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "platforms": {
            "darwin-aarch64": {
                "signature": signature.strip(),
                "url": DOWNLOAD_URL.format(version=version),
            }
        },
    }


def clear_outputs(output_dir: Path) -> None:
    for name in OUTPUTS:
        (output_dir / name).unlink(missing_ok=True)


def build_environment(environ: Mapping[str, str], rzm: Path, home: Path) -> dict[str, str]:
    """The Tauri build gets the signing key, not the publishing tokens."""
    env = {
        name: value for name, value in environ.items()
        if not sensitive_name(name) or name in SIGNING_ENV
    }
    path = env.get("PATH", "")
    if shutil.which("cargo", path=path) is None:
        cargo_bin = home / ".cargo/bin"
        if not (cargo_bin / "cargo").is_file():
            raise DesktopBuildError("cargo not found; install Rust with rustup to build the desktop app")
        env["PATH"] = os.pathsep.join(filter(None, (str(cargo_bin), path)))
    env["RZM_DESKTOP_PREBUILT_RZM"] = str(rzm)
    # Tauri skips the Finder AppleScript that lays out the DMG window when CI is
    # set; without Automation permission that script fails the build (-1743).
    env["CI"] = "true"
    return env


def build(
    rzm: Path,
    version: str,
    *,
    environ: Mapping[str, str],
    root: Path = ROOT,
    runner: Runner = subprocess.run,
    machine: str | None = None,
    home: Path | None = None,
    now: Callable[[], datetime] = lambda: datetime.now(timezone.utc),
) -> list[Path]:
    """Build, verify, and stage the app; return the staged release assets."""
    updater = signs_updater(environ)
    if environ.get("RZM_RELEASE_DESKTOP") == "1":
        require_release_pubkey(root)
    host = machine or platform.machine()
    if host != "arm64":
        raise DesktopBuildError(f"the desktop app builds only on Apple Silicon, not {host}")
    rzm = Path(rzm).resolve()
    if not rzm.is_file():
        raise DesktopBuildError(f"rzm binary not found: {rzm}")
    version = version.removeprefix("v")
    env = build_environment(environ, rzm, home or Path.home())
    desktop = root / "desktop"
    if not (desktop / "node_modules").is_dir():
        runner(["make", "desktop-deps"], cwd=root, env=env, check=True)
    bundle_dir = root / BUNDLE_DIR
    # Earlier builds leave differently named DMGs and stale updater archives.
    for kind in ("dmg", "macos"):
        shutil.rmtree(bundle_dir / kind, ignore_errors=True)
    runner(tauri_command(version, updater=updater), cwd=desktop, env=env, check=True)
    runner(
        ["codesign", "--verify", "--deep", "--strict", str(bundle_dir / "macos/Rhizome.app")],
        env=env, check=True,
    )
    # GoReleaser already checked the bundled rzm; the app's own executable,
    # which embeds the shell frontend, gets the same public-artifact check.
    runner(
        [sys.executable, str(root / "scripts/teamkeys/release.py"), "--check-artifact",
         str(bundle_dir / "macos/Rhizome.app/Contents/MacOS/rhizome-desktop")],
        env=env, check=True,
    )
    output_dir = root / OUTPUT_DIR
    output_dir.mkdir(parents=True, exist_ok=True)
    staged = []
    for name, source in bundle_outputs(bundle_dir, updater=updater).items():
        shutil.copyfile(source, output_dir / name)
        staged.append(output_dir / name)
    if updater:
        signature = (output_dir / SIGNATURE).read_text(encoding="utf-8")
        feed = update_feed(version, signature, now())
        (output_dir / FEED).write_text(json.dumps(feed, indent=2) + "\n", encoding="utf-8")
        staged.append(output_dir / FEED)
    return staged


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("rzm", help="GoReleaser's built rzm binary ({{ .Path }})")
    parser.add_argument("version", help="release version ({{ .Version }})")
    parser.add_argument("arch", help="GOARCH of the binary ({{ .Arch }})")
    parser.add_argument("snapshot", choices=("true", "false"), help="{{ .IsSnapshot }}")
    args = parser.parse_args(argv)
    try:
        if args.arch == "arm64":
            # Never let a previous build's app leak into a CLI-only release.
            clear_outputs(ROOT / OUTPUT_DIR)
        reason = skip_reason(args.arch, args.snapshot == "true", os.environ)
        if reason:
            print(f"desktop: skipped; {reason}", file=sys.stderr)
            return 0
        for path in build(Path(args.rzm), args.version, environ=os.environ):
            print(f"desktop: staged {path.relative_to(ROOT)}", file=sys.stderr)
    except (DesktopBuildError, subprocess.CalledProcessError, OSError) as error:
        print(f"desktop: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
