"""Contracts shared by native desktop producers and the release coordinator."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path, PurePosixPath, PureWindowsPath
import re
import sys
import tarfile
from typing import Mapping


ROOT = Path(__file__).resolve().parents[2]
TARGETS = {
    "windows-x86_64": {"host": "Windows", "machine": {"AMD64", "x86_64"}, "bundle": "nsis",
                       "glob": "nsis/*-setup.exe", "asset": "Rhizome-windows-x64-setup.exe", "cli": "rzm.exe"},
    "linux-x86_64": {"host": "Linux", "machine": {"x86_64", "AMD64"}, "bundle": "appimage",
                     "glob": "appimage/*.AppImage", "asset": "Rhizome-linux-x64.AppImage", "cli": "rzm"},
    "linux-aarch64": {"host": "Linux", "machine": {"aarch64", "arm64"}, "bundle": "appimage",
                      "glob": "appimage/*.AppImage", "asset": "Rhizome-linux-arm64.AppImage", "cli": "rzm"},
    "darwin-aarch64": {"host": "Darwin", "machine": {"arm64", "aarch64"}, "bundle": "app,dmg",
                       "asset": "Rhizome-macos-arm64.app.tar.gz", "cli": "rzm"},
}
NATIVE_TARGETS = frozenset(("windows-x86_64", "linux-x86_64", "linux-aarch64"))
# Each native target's dependency evidence must come from this baseline host.
BASELINES = {"windows-x86_64": "windows-11-x86_64", "linux-x86_64": "ubuntu-24.04-x86_64",
             "linux-aarch64": "ubuntu-24.04-aarch64"}
# Public releases publish every target except Windows. Its desktop build and
# PR checks remain, but no production Windows signing is configured, so its
# packages are not published.
RELEASE_NATIVE_TARGETS = frozenset(("linux-x86_64", "linux-aarch64"))
REQUIRED_TARGETS = RELEASE_NATIVE_TARGETS | {"darwin-aarch64"}
MANIFEST = "desktop-candidate.json"
COMMIT = re.compile(r"[0-9a-f]{40}\Z")
HASH = re.compile(r"[0-9a-f]{64}\Z")
VERSION = re.compile(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?\Z")


class CandidateError(RuntimeError):
    """A candidate is incomplete, inconsistent or unsuitable for publication."""


def tauri_cli(root: Path, *, windows: bool | None = None) -> list[str]:
    """Avoid npm.cmd shell parsing when passing config JSON and paths on Windows."""
    is_windows = sys.platform == "win32" if windows is None else windows
    if is_windows:
        return ["node", str(root / "desktop/node_modules/@tauri-apps/cli/tauri.js")]
    return ["npm", "run", "tauri", "--"]


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def identity(version: str, commit: str) -> tuple[str, str]:
    version = version.removeprefix("v")
    if not VERSION.fullmatch(version) or not COMMIT.fullmatch(commit):
        raise CandidateError("candidate requires an exact version and full canonical commit")
    return version, commit


def profile(root: Path, target: str) -> Path:
    if target not in NATIVE_TARGETS:
        raise CandidateError("native packaging supports " + ", ".join(sorted(NATIVE_TARGETS)))
    return root / f"desktop/src-tauri/tauri.release.{target.split('-')[0]}.conf.json"


def validate_profile(path: Path, target: str) -> dict:
    config = json.loads(path.read_text())
    bundle = config.get("bundle", {})
    if bundle.get("externalBin") != ["binaries/rzm"] or bundle.get("targets") != [TARGETS[target]["bundle"]]:
        raise CandidateError("native release profile must package only rzm and its selected installer")
    if bundle.get("createUpdaterArtifacts") is not False:
        raise CandidateError("native producers must leave updater signing to the coordinator")
    if target == "windows-x86_64" and bundle.get("windows", {}).get("nsis", {}).get("installMode") != "currentUser":
        raise CandidateError("Windows desktop requires a per-user NSIS installation")
    return config


def package_file(bundle_dir: Path, target: str) -> Path:
    files = sorted(bundle_dir.glob(TARGETS[target]["glob"]))
    if len(files) != 1 or not files[0].is_file():
        raise CandidateError(f"expected exactly one {TARGETS[target]['bundle']} package")
    return files[0]


def sidecar_inventory(extracted: Path, target: str, expected_hash: str) -> list[dict]:
    cli = TARGETS[target]["cli"]
    files = [p for p in extracted.rglob("*") if p.name in {"rzm", "rzm.exe", "team-api", "team-api.exe"}]
    if len(files) != 1 or files[0].name != cli or files[0].is_symlink() or not files[0].is_file():
        raise CandidateError("final package must contain exactly the rzm sidecar and no local gateway")
    if not HASH.fullmatch(expected_hash) or sha256(files[0]) != expected_hash:
        raise CandidateError("extracted rzm differs from the canonical CLI artifact")
    return [{"name": cli, "sha256": expected_hash}]


def mac_sidecar_inventory(archive: Path) -> list[dict]:
    """Hash the signed Mac sidecar without executing or extracting the archive."""
    with tarfile.open(archive, "r:gz") as source:
        members = source.getmembers()
        if any(Path(member.name).name in {"team-api", "team-api.exe"} for member in members):
            raise CandidateError("Mac release archive contains the development gateway")
        sidecars = [member for member in members if member.name.endswith("/Contents/MacOS/rzm")]
        if len(sidecars) != 1 or not sidecars[0].isfile():
            raise CandidateError("Mac archive must contain one regular rzm sidecar")
        stream = source.extractfile(sidecars[0])
        if stream is None:
            raise CandidateError("cannot read the signed Mac sidecar")
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
        return [{"name": "rzm", "sha256": digest}]


def validate_dependencies(dependencies: object, target: str, version: str) -> None:
    baseline = BASELINES[target]
    if not isinstance(dependencies, dict):
        raise CandidateError("native candidate lacks measured dependency evidence")
    host, imports, probe = dependencies.get("host"), dependencies.get("imports"), dependencies.get("probeVersion")
    if (dependencies.get("status") != "verified" or dependencies.get("baseline") != baseline
            or not isinstance(host, dict) or not host.get("os") or not host.get("version")
            or not isinstance(imports, list) or any(not isinstance(item, dict) or not item.get("name")
                or not item.get("path") or item.get("origin") != "system" for item in imports)
            or not isinstance(probe, str) or not probe.split() or probe.split()[-1].removeprefix("v") != version
            or not dependencies.get("methods")):
        raise CandidateError("native dependency evidence is incomplete or does not match the candidate")
    if target.startswith("windows-") and (not imports or any(item.get("signature") != "microsoft-valid" for item in imports)):
        raise CandidateError("Windows CLI imports lack verified baseline system components")
    if target.startswith("windows-"):
        system = PureWindowsPath(host.get("systemDirectory", ""))
        if not system.is_absolute() or system.name.lower() != "system32" or any(
                PureWindowsPath(item["path"]).parent != system for item in imports):
            raise CandidateError("Windows CLI dependency resolves outside the baseline System32 directory")
        if not re.fullmatch(r"10\.0\.[0-9]+(?:\.[0-9]+)?", str(host["version"])) or int(str(host["version"]).split(".")[2]) < 22000:
            raise CandidateError("Windows dependency host is below the Windows 11 baseline")
    else:
        if (host.get("os") != "Ubuntu" or host.get("version") != "24.04" or host.get("libc") != "glibc 2.39"
                or host.get("architecture") != target.split("-", 1)[1]):
            raise CandidateError("Linux dependency host differs from the Ubuntu 24.04 baseline")
        for item in imports:
            path = PurePosixPath(item["path"])
            if not path.is_absolute() or ".." in path.parts or not any(path.is_relative_to(base)
                    for base in ("/lib", "/lib64", "/usr/lib", "/usr/lib64")):
                raise CandidateError("Linux CLI dependency resolves outside the baseline system directories")


def candidate_manifest(*, target: str, version: str, commit: str, package: Path, cli_hash: str,
                       inventory: list[dict], toolchains: Mapping[str, str], native_signature: dict,
                       production: bool, packaged_cli_hash: str | None = None,
                       dependencies: dict | None = None) -> dict:
    version, commit = identity(version, commit)
    if target not in TARGETS or not HASH.fullmatch(cli_hash):
        raise CandidateError("invalid candidate target or canonical CLI hash")
    packaged_cli_hash = packaged_cli_hash or cli_hash
    if not HASH.fullmatch(packaged_cli_hash) or (target != "darwin-aarch64" and packaged_cli_hash != cli_hash):
        raise CandidateError("native package sidecar must equal its final canonical CLI bytes")
    if inventory != [{"name": TARGETS[target]["cli"], "sha256": packaged_cli_hash}]:
        raise CandidateError("candidate sidecar inventory does not match its canonical CLI")
    if not toolchains or any(not isinstance(v, str) or not v.strip() for v in toolchains.values()):
        raise CandidateError("candidate toolchain identity is required")
    if target in NATIVE_TARGETS:
        validate_dependencies(dependencies, target, version)
    return {
        "schemaVersion": 1, "target": target, "version": version, "sourceCommit": commit,
        "production": production, "canonicalCliSha256": cli_hash, "packagedCliSha256": packaged_cli_hash,
        "sidecars": inventory,
        "toolchains": dict(toolchains), "nativeSignature": native_signature,
        **({"nativeDependencies": dependencies} if target in NATIVE_TARGETS else {}),
        "artifact": {"file": TARGETS[target]["asset"], "sha256": sha256(package), "size": package.stat().st_size},
    }


def read_candidate(directory: Path, *, version: str, commit: str, canonical_hash: str,
                   production: bool) -> tuple[dict, Path]:
    """Revalidate immutable producer files before any signing/staging mutation."""
    version, commit = identity(version, commit)
    manifest = json.loads((directory / MANIFEST).read_text())
    if not isinstance(manifest, dict):
        raise CandidateError("candidate manifest must be an object")
    target = manifest.get("target")
    if not isinstance(target, str) or target not in TARGETS or manifest.get("schemaVersion") != 1:
        raise CandidateError("unknown candidate schema or target")
    if manifest.get("version") != version or manifest.get("sourceCommit") != commit:
        raise CandidateError("candidate version/source differs from the canonical release")
    if not isinstance(canonical_hash, str) or not HASH.fullmatch(canonical_hash) or manifest.get("canonicalCliSha256") != canonical_hash:
        raise CandidateError("candidate canonical CLI hash does not match coordinator input")
    packaged_hash = manifest.get("packagedCliSha256")
    if (not isinstance(packaged_hash, str) or not HASH.fullmatch(packaged_hash)
            or (target != "darwin-aarch64" and packaged_hash != canonical_hash)):
        raise CandidateError("candidate packaged CLI hash is invalid")
    if manifest.get("sidecars") != [{"name": TARGETS[target]["cli"], "sha256": packaged_hash}]:
        raise CandidateError("candidate contains an unexpected sidecar inventory")
    cli = directory / TARGETS[target]["cli"]
    if cli.is_symlink() or not cli.is_file() or sha256(cli) != canonical_hash:
        raise CandidateError("candidate canonical CLI copy is missing or changed")
    artifact = manifest.get("artifact", {})
    if not isinstance(artifact, dict) or artifact.get("file") != TARGETS[target]["asset"]:
        raise CandidateError("candidate artifact name must be the stable target name")
    path = directory / artifact["file"]
    if path.is_symlink() or not path.is_file() or path.stat().st_size != artifact.get("size") or sha256(path) != artifact.get("sha256"):
        raise CandidateError("candidate package is missing or its bytes changed")
    if target == "darwin-aarch64" and mac_sidecar_inventory(path) != manifest["sidecars"]:
        raise CandidateError("Mac packaged sidecar differs from its recorded signed bytes")
    tools = manifest.get("toolchains")
    if not isinstance(tools, dict) or not tools or any(not isinstance(v, str) or not v.strip() for v in tools.values()):
        raise CandidateError("candidate toolchain identity is missing")
    if target in NATIVE_TARGETS:
        validate_dependencies(manifest.get("nativeDependencies"), target, version)
    if production:
        signature = manifest.get("nativeSignature", {})
        expected = "authenticode" if target.startswith("windows-") else "notarized" if target.startswith("darwin-") else "not-applicable"
        if not isinstance(signature, dict) or manifest.get("production") is not True or signature.get("status") != expected:
            raise CandidateError("unsigned/test candidates cannot enter production desktop publication")
        if expected == "authenticode" and not signature.get("thumbprint"):
            raise CandidateError("Windows production candidate lacks its verified publisher identity")
    return manifest, path
