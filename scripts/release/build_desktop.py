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
import tarfile
from typing import Any, Callable, Mapping, Sequence

from run_clean import sensitive_name
from verify_desktop_signing import PUBKEY_PLACEHOLDER, DesktopSigningError, verify_archive
from apple_signing import APPLE_SIGNING, AppleSigningError, credentials, desktop_credentials, notarizes, notarize_dmg, require_credentials, verify_distribution
from desktop_candidates import CandidateError, MANIFEST, candidate_manifest, mac_sidecar_inventory, sha256


ROOT = Path(__file__).resolve().parents[2]
OUTPUT_DIR = Path(".tmp/goreleaser-extra")
BUNDLE_DIR = Path("desktop/src-tauri/target/release/bundle")
DMG = "Rhizome-macos-arm64.dmg"
ARCHIVE = "Rhizome-macos-arm64.app.tar.gz"
SIGNATURE = ARCHIVE + ".sig"
FEED = "latest.json"
OUTPUTS = (DMG, ARCHIVE, SIGNATURE, FEED)
# CI release builds stage the unsigned Mac updater candidate here for the Linux coordinator.
CANDIDATE_DIR = "darwin-aarch64"
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


def ci_candidate(environ: Mapping[str, str]) -> bool:
    """A CI release build produces an unsigned updater candidate; the Linux coordinator signs it."""
    return environ.get("RZM_RELEASE_DESKTOP") == "1" and bool(environ.get("RZM_CI_RELEASE_VERSION"))


def signs_updater(environ: Mapping[str, str]) -> bool:
    if ci_candidate(environ):
        if any(environ.get(name, "").strip() for name in SIGNING_ENV):
            raise DesktopBuildError("CI release candidates must not receive the updater signing key; "
                                    "the protected Linux coordinator signs every updater package")
        return False
    if environ.get(SIGNING_KEY, "").strip():
        return True
    if environ.get("RZM_RELEASE_DESKTOP") == "1":
        raise DesktopBuildError(
            f"{SIGNING_KEY} is required to publish the desktop app; "
            "configure the protected release CI updater signing key"
        )
    return False


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


def tauri_command(version: str, *, updater: bool, notarize: bool = False, bundle: bool = False, prebuilt_frontend: bool = False) -> list[str]:
    command = [
        "npm", "run", "tauri", "--", "bundle" if bundle else "build",
        *(["--bundles", "app,dmg"] if bundle else ["--no-bundle"]),
        "--config", "src-tauri/tauri.release.conf.json",
        "--config", json.dumps({"version": version}),
    ]
    if prebuilt_frontend:
        command += ["--config", '{"build":{"beforeBuildCommand":"npm run rzm"}}']
    if not notarize:
        command += ["--config", '{"bundle":{"macOS":{"signingIdentity":"-"}}}']
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
    shutil.rmtree(output_dir / CANDIDATE_DIR, ignore_errors=True)


def updater_archive(app: Path) -> Path:
    """Pack the stapled app as Tauri's updater bundler does: one gzip tar of Rhizome.app, symlinks kept."""
    archive = app.with_name(app.name + ".tar.gz")
    with tarfile.open(archive, "w:gz", format=tarfile.GNU_FORMAT) as bundle:
        bundle.add(app, arcname=app.name)
    return archive


def stage_candidate(root: Path, rzm: Path, version: str, archive: Path, output: Path, *,
                    runner: Runner, env: Mapping[str, str]) -> list[Path]:
    """Record the notarized Mac updater archive as an unsigned same-run candidate."""
    commit = runner(["git", "rev-parse", "HEAD"], cwd=root, env=env, check=True,
                    capture_output=True, text=True).stdout.strip()
    tools = {}
    for name, command in [("go", ["go", "version"]), ("rust", ["rustc", "--version"]),
                          ("tauri", ["npm", "run", "tauri", "--", "--version"])]:
        tools[name] = runner(command, cwd=root / "desktop", env=env, check=True,
                             capture_output=True, text=True).stdout.strip()
    inventory = mac_sidecar_inventory(archive)
    manifest = candidate_manifest(target="darwin-aarch64", version=version, commit=commit, package=archive,
                                  cli_hash=sha256(rzm), packaged_cli_hash=inventory[0]["sha256"],
                                  inventory=inventory, native_signature={"status": "notarized"},
                                  toolchains=tools, production=True)
    candidate = output / CANDIDATE_DIR
    candidate.mkdir()
    shutil.copyfile(rzm, candidate / "rzm")
    shutil.copyfile(archive, candidate / ARCHIVE)
    (candidate / MANIFEST).write_text(json.dumps(manifest, indent=2) + "\n")
    return [candidate / "rzm", candidate / ARCHIVE, candidate / MANIFEST]


def clear_bundle(bundle_dir: Path) -> None:
    # Earlier builds leave differently named DMGs and stale updater archives.
    for kind in ("dmg", "macos"):
        shutil.rmtree(bundle_dir / kind, ignore_errors=True)


def stapled(app: Path, *, runner: Runner, env: Mapping[str, str]) -> bool:
    result = runner(["xcrun", "stapler", "validate", str(app)], env=env, check=False, capture_output=True, text=True)
    return result.returncode == 0


def report_notarization(result: Any) -> None:
    """Echo Tauri's notarization status lines, which name no credentials."""
    for line in f"{getattr(result, 'stdout', '') or ''}\n{getattr(result, 'stderr', '') or ''}".splitlines():
        if "otariz" in line or "Stapling" in line:
            print(f"desktop: tauri: {line.strip()}", flush=True)


def build_environment(environ: Mapping[str, str], rzm: Path, home: Path) -> dict[str, str]:
    """The Tauri build gets the signing key, not the publishing tokens."""
    env = {
        name: value for name, value in environ.items()
        if not sensitive_name(name) or name in SIGNING_ENV or (notarizes(environ) and name in APPLE_SIGNING)
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
    environ = desktop_credentials(environ)
    updater = signs_updater(environ)
    if environ.get("RZM_RELEASE_DESKTOP") == "1":
        require_release_pubkey(root)
    notarize = notarizes(environ)
    if notarize:
        require_credentials(environ)
    host = machine or platform.machine()
    if host != "arm64":
        raise DesktopBuildError(f"the desktop app builds only on Apple Silicon, not {host}")
    rzm = Path(rzm).resolve()
    if not rzm.is_file():
        raise DesktopBuildError(f"rzm binary not found: {rzm}")
    version = version.removeprefix("v")
    env = build_environment(environ, rzm, home or Path.home())
    desktop = root / "desktop"
    clean_env = {name: value for name, value in env.items() if not sensitive_name(name)}
    if not (desktop / "node_modules").is_dir():
        runner(["make", "desktop-deps"], cwd=root, env=clean_env, check=True)
    prebuilt_frontend = environ.get("RZM_DESKTOP_PREBUILT_FRONTEND") == "1"
    if prebuilt_frontend and not (desktop / "dist/index.html").is_file():
        raise DesktopBuildError("prepared desktop frontend is missing; restore the verified CI frontend artifact")
    bundle_dir = root / BUNDLE_DIR
    clear_bundle(bundle_dir)
    runner(tauri_command(version, updater=updater, notarize=notarize, prebuilt_frontend=prebuilt_frontend), cwd=desktop, env=clean_env, check=True)
    command = tauri_command(version, updater=updater, notarize=notarize, bundle=True, prebuilt_frontend=prebuilt_frontend)
    if notarize:
        with credentials(env) as signing_env:
            # A signer failure must not expose credentials through captured output.
            try:
                for attempt in range(2):
                    result = runner(command, cwd=desktop, env=signing_env, check=True, capture_output=True, text=True, timeout=3600)
                    report_notarization(result)
                    if stapled(bundle_dir / "macos/Rhizome.app", runner=runner, env=clean_env):
                        break
                    if attempt:
                        raise DesktopBuildError("Rhizome.app has no stapled ticket after rebuilding; no desktop assets staged")
                    # Tauri ignores `xcrun stapler staple` failures (tauri-macos-sign
                    # 2.12), so a ticket Apple has not yet propagated leaves the app,
                    # DMG and updater archive unstapled. A rebuild resubmits the same app.
                    print("desktop: Tauri left Rhizome.app without a stapled ticket; rebuilding the bundle once", flush=True)
                    clear_bundle(bundle_dir)
                outputs = bundle_outputs(bundle_dir, updater=updater)
                notarize_dmg(outputs[DMG], runner=runner, env=signing_env)
            except subprocess.SubprocessError as error:
                raise DesktopBuildError("Tauri signing/notarization failed; no desktop assets staged") from error
    else:
        runner(command, cwd=desktop, env=env, check=True)
    env = clean_env
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
    outputs = bundle_outputs(bundle_dir, updater=updater)
    candidate = ci_candidate(environ)
    if candidate:
        outputs[ARCHIVE] = updater_archive(bundle_dir / "macos/Rhizome.app")
    if updater:
        try:
            verify_archive(root, outputs[ARCHIVE], outputs[SIGNATURE], environ=env, runner=runner)
        except DesktopSigningError as error:
            raise DesktopBuildError(str(error)) from error
    if notarize:
        verify_distribution(bundle_dir / "macos/Rhizome.app", outputs[DMG], outputs.get(ARCHIVE),
                            environ["APPLE_SIGNING_IDENTITY"], runner=runner, env=env)
    output_dir = root / OUTPUT_DIR
    output_dir.mkdir(parents=True, exist_ok=True)
    if candidate:
        shutil.copyfile(outputs[DMG], output_dir / DMG)
        return [output_dir / DMG, *stage_candidate(root, rzm, version, outputs[ARCHIVE], output_dir,
                                                   runner=runner, env=env)]
    staged = []
    for name, source in outputs.items():
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
    except (DesktopBuildError, CandidateError, DesktopSigningError, AppleSigningError, subprocess.CalledProcessError, OSError, ValueError) as error:
        print(f"desktop: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
