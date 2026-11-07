#!/usr/bin/env python3
"""Collect native builds and assemble one source-bound public release on Linux."""
from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tarfile

from build_installer import stage_installer
from release_actions import validate_release_version
from run_clean import scoped_environment
from desktop_candidates import (MANIFEST as CANDIDATE_MANIFEST, REQUIRED_TARGETS, TARGETS as DESKTOP_TARGETS,
                                CandidateError, mac_sidecar_inventory, read_candidate, validate_dependencies)
from verify_desktop_signing import DesktopSigningError, verify_archive

ROOT = Path(__file__).resolve().parents[2]
TARGETS = ("darwin-arm64", "darwin-amd64", "linux-arm64", "linux-amd64", "windows-amd64")
# The linux group builds both Linux CLIs and the unsigned Windows CLI. The
# linux-arm64 group packages the arm64 one on an Arm host, so it carries a
# copy of those same canonical bytes. Windows desktop packages are not published.
GROUPS = {"linux": TARGETS[2:], "linux-arm64": TARGETS[2:3], "macos": TARGETS[:2]}
NATIVE_GROUPS = {"linux": "linux-x86_64", "linux-arm64": "linux-aarch64"}
# The CLI archive whose bytes each desktop package carries as its sidecar.
DESKTOP_CLI = {"darwin-aarch64": "darwin-arm64", "linux-x86_64": "linux-amd64", "linux-aarch64": "linux-arm64"}
DESKTOP = ("Rhizome-macos-arm64.dmg", "Rhizome-macos-arm64.app.tar.gz",
           "Rhizome-macos-arm64.app.tar.gz.sig", "latest.json", "Rhizome-linux-x64.AppImage",
           "Rhizome-linux-x64.AppImage.sig", "Rhizome-linux-arm64.AppImage", "Rhizome-linux-arm64.AppImage.sig",
           "desktop-assembly.json")
PAYLOADS = tuple(f"rhizome-{target}.tar.gz" for target in TARGETS) + ("install-rzm.sh", "install-rzm.sh.sha256", *DESKTOP)
BUNDLE_FILES = {*PAYLOADS, "release-manifest.json", "checksums.txt"}


class ArtifactError(ValueError):
    pass


def identity(source, version):
    if not isinstance(source, str) or not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ArtifactError("release source must be a full commit SHA")
    validate_release_version(version)


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def regular(root, path):
    root, path = Path(root).absolute(), Path(path).absolute()
    if not path.is_relative_to(root) or root.is_symlink():
        raise ArtifactError("artifact path escapes its root")
    for part in (path, *path.parents):
        if part.is_symlink():
            raise ArtifactError("artifact symlinks are forbidden")
        if part == root:
            break
    if not stat.S_ISREG(path.stat().st_mode):
        raise ArtifactError("artifact is not a regular file")
    return path


def inventory(root):
    if root.is_symlink() or not root.is_dir():
        raise ArtifactError("artifact input must be a real directory")
    result = set()
    for path in root.rglob("*"):
        if path.is_symlink():
            raise ArtifactError("artifact symlinks are forbidden")
        if not path.is_dir():
            regular(root, path)
            result.add(path.relative_to(root).as_posix())
    return result


def binary_name(target):
    return "rzm.exe" if target.startswith("windows-") else "rzm"


def scan(path):
    result = subprocess.run([sys.executable, str(ROOT / "scripts/teamkeys/release.py"), "--check-artifact", str(path)],
                            capture_output=True, env=scoped_environment(os.environ, "build"))
    if result.returncode:
        raise ArtifactError(f"public content check failed: {path.name}")


def derived_changes_only(runner):
    changed = []
    for argv in (("git", "diff", "--name-only", "-z", "HEAD"),
                 ("git", "ls-files", "--others", "--exclude-standard", "-z")):
        result = runner(argv, cwd=ROOT, capture_output=True, text=True,
                        env=scoped_environment(os.environ, "build"), timeout=30)
        if result.returncode:
            raise ArtifactError("cannot verify derived build changes")
        changed.extend(name for name in result.stdout.split("\0") if name)
    if any(not name.startswith("pkg/app/web/assets/dist/") for name in changed):
        raise ArtifactError("binary was built with non-derived source changes")


def build_info(path, target, source, version, runner=subprocess.run):
    result = runner(["go", "version", "-m", str(path)], capture_output=True, text=True,
                    env=scoped_environment(os.environ, "build"), timeout=30)
    if result.returncode:
        raise ArtifactError(f"unreadable Go build information: {target}")
    settings = {}
    for line in result.stdout.splitlines():
        fields = line.strip().split("\t", 1)
        if len(fields) == 2 and fields[0] == "build" and "=" in fields[1]:
            key, value = fields[1].split("=", 1)
            settings[key] = json.loads(value) if value.startswith('"') else value
    goos, goarch = target.split("-")
    flags = shlex.split(settings.get("-ldflags", ""))
    expected = f"github.com/atomicobject/rhizome/pkg/vault/version.Version={version.removeprefix('v')}"
    versions = [flags[index + 1] for index, flag in enumerate(flags[:-1]) if flag == "-X" and "/pkg/vault/version.Version=" in flags[index + 1]]
    if (settings.get("GOOS") != goos or settings.get("GOARCH") != goarch
            or settings.get("vcs.revision") != source or settings.get("vcs.modified") not in {"true", "false"}
            or ("-ldflags" in settings and versions != [expected])):
        raise ArtifactError(f"Go binary target, version or source mismatch: {target}")
    if settings["vcs.modified"] == "true":
        derived_changes_only(runner)


def build_metadata(dist, source, version):
    value = json.loads(regular(dist, dist / "metadata.json").read_text())
    if (not isinstance(value, dict) or value.get("commit") != source
            or not isinstance(value.get("version"), str)
            or value["version"].removeprefix("v") != version.removeprefix("v")):
        raise ArtifactError("GoReleaser metadata version or source mismatch")


def expected_group(platform):
    return {f"bin/{target}/{binary_name(target)}" for target in GROUPS[platform]} | (
        {f"desktop/{name}" for name in DESKTOP} if platform == "macos" else {
            f"candidate/{name}" for name in (CANDIDATE_MANIFEST, DESKTOP_TARGETS[NATIVE_GROUPS[platform]]["cli"],
                                            DESKTOP_TARGETS[NATIVE_GROUPS[platform]]["asset"])})


def write_manifest(directory, source, version, files, platform=None, *, candidate_only=False):
    value = {"schema_version": 1, "source_commit": source, "version": version,
             "candidate_only": candidate_only,
             "files": {name: digest(directory / name) for name in sorted(files)}}
    if platform:
        value["platform"] = platform
    name = "manifest.json" if platform else "release-manifest.json"
    (directory / name).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    return value


def validate_manifest(directory, source, version, *, platform=None, candidate_only=False):
    identity(source, version)
    name = "manifest.json" if platform else "release-manifest.json"
    value = json.loads(regular(directory, directory / name).read_text())
    keys = {"schema_version", "source_commit", "version", "files", "candidate_only"} | ({"platform"} if platform else set())
    expected = expected_group(platform) if platform else set(PAYLOADS)
    if (not isinstance(value, dict) or set(value) != keys or type(value["schema_version"]) is not int or value["schema_version"] != 1
            or value["source_commit"] != source or value["version"] != version
            or value["candidate_only"] is not candidate_only
            or (platform and value["platform"] != platform) or not isinstance(value["files"], dict)
            or set(value["files"]) != expected):
        raise ArtifactError("artifact manifest identity or asset set mismatch")
    allowed = expected | {name} if platform else BUNDLE_FILES
    if inventory(directory) != allowed:
        raise ArtifactError("unexpected or missing artifact files")
    for relative, checksum in value["files"].items():
        if not isinstance(checksum, str) or not re.fullmatch(r"[0-9a-f]{64}", checksum) or digest(regular(directory, directory / relative)) != checksum:
            raise ArtifactError(f"artifact checksum mismatch: {relative}")
    if platform in NATIVE_GROUPS:
        target = NATIVE_GROUPS[platform]
        cli_target = DESKTOP_CLI[target]
        canonical = digest(directory / "bin" / cli_target / binary_name(cli_target))
        candidate, _ = read_candidate(directory / "candidate", version=version, commit=source,
                                      canonical_hash=canonical, production=not candidate_only)
        if candidate["target"] != target:
            raise ArtifactError("native candidate target differs from its selected CI group")
    return value


def validate_feed(directory, version, source, *, candidate_only=False):
    value = json.loads((directory / "latest.json").read_text())
    if not isinstance(value, dict) or not isinstance(value.get("platforms"), dict):
        raise ArtifactError("invalid desktop updater feed")
    assembly = json.loads(regular(directory, directory / "desktop-assembly.json").read_text())
    if (set(value["platforms"]) != REQUIRED_TARGETS or value.get("version") != version.removeprefix("v")
            or not isinstance(assembly, dict) or assembly.get("production") is not (not candidate_only)
            or assembly.get("candidateOnly") is not candidate_only
            or assembly.get("schemaVersion") != 1
            or assembly.get("sourceCommit") != source or assembly.get("version") != version.removeprefix("v")
            or any(not isinstance(assembly.get(key), dict) for key in ("packages", "candidates", "canonicalCliSha256"))
            or set(assembly.get("packages", {})) != REQUIRED_TARGETS
            or set(assembly.get("candidates", {})) != REQUIRED_TARGETS
            or set(assembly.get("canonicalCliSha256", {})) != REQUIRED_TARGETS
            or assembly.get("updaterVerification") != {target: "verified" for target in REQUIRED_TARGETS}):
        raise ArtifactError("desktop updater feed does not match the release")
    for target in REQUIRED_TARGETS:
        name = DESKTOP_TARGETS[target]["asset"]
        package = regular(directory, directory / name)
        signature = regular(directory, directory / (name + ".sig")).read_text().strip()
        expected = {"signature": signature, "url": f"https://github.com/atomicobject/rhizome/releases/download/{version}/{name}"}
        candidate = assembly["candidates"][target]
        if (not signature or value["platforms"][target] != expected
                or assembly["packages"][target] != {"file": name, "sha256": digest(package), "size": package.stat().st_size}
                or not isinstance(candidate, dict) or candidate.get("artifact") != assembly["packages"][target] or candidate.get("target") != target
                or candidate.get("sourceCommit") != source or candidate.get("version") != version.removeprefix("v")
                or (not candidate_only and candidate.get("production") is not True)
                or candidate.get("canonicalCliSha256") != assembly["canonicalCliSha256"][target]):
            raise ArtifactError("desktop updater feed does not match the signed native candidates")
        if target == "darwin-aarch64":
            if mac_sidecar_inventory(package) != candidate.get("sidecars"):
                raise ArtifactError("signed Mac sidecar inventory differs from the recorded package")
        else:
            validate_dependencies(candidate.get("nativeDependencies"), target, version.removeprefix("v"))
            if candidate.get("sidecars") != [{"name": DESKTOP_TARGETS[target]["cli"], "sha256": candidate["canonicalCliSha256"]}]:
                raise ArtifactError("native sidecar inventory differs from its canonical archived CLI")
    return assembly


def collect(platform, source, version, dist, extras, output, *, candidate=None, scanner=scan, inspector=build_info, candidate_only=False):
    identity(source, version)
    build_metadata(dist, source, version)
    if output.exists():
        raise ArtifactError("collection output must not already exist")
    output.mkdir(parents=True)
    for target in GROUPS[platform]:
        goos, goarch = target.split("-")
        build_id = {"darwin": "rzm-darwin", "linux": "rzm-linux", "windows": "rzm-windows-amd64"}[goos]
        paths = list(dist.glob(f"{build_id}_{goos}_{goarch}*/{binary_name(target)}"))
        if len(paths) != 1:
            raise ArtifactError(f"expected exactly one GoReleaser binary: {target}")
        path = regular(dist, paths[0])
        inspector(path, target, source, version)
        scanner(path)
        destination = output / "bin" / target / binary_name(target)
        destination.parent.mkdir(parents=True)
        shutil.copyfile(path, destination)
        destination.chmod(0o755)
    if platform == "macos":
        (output / "desktop").mkdir()
        for name in DESKTOP:
            shutil.copyfile(regular(extras, extras / name), output / "desktop" / name)
        assembly = validate_feed(output / "desktop", version, source, candidate_only=candidate_only)
        if assembly["canonicalCliSha256"]["darwin-aarch64"] != digest(output / "bin/darwin-arm64/rzm"):
            raise ArtifactError("Mac desktop sidecar source differs from the canonical archived CLI")
    else:
        if candidate is None:
            raise ArtifactError("native CLI collection requires its final packaged candidate")
        if inventory(candidate) != {CANDIDATE_MANIFEST, DESKTOP_TARGETS[NATIVE_GROUPS[platform]]["cli"], DESKTOP_TARGETS[NATIVE_GROUPS[platform]]["asset"]}:
            raise ArtifactError("native candidate contains unexpected producer files")
        for name in (CANDIDATE_MANIFEST, DESKTOP_TARGETS[NATIVE_GROUPS[platform]]["cli"], DESKTOP_TARGETS[NATIVE_GROUPS[platform]]["asset"]):
            (output / "candidate").mkdir(exist_ok=True)
            shutil.copyfile(regular(candidate, candidate / name), output / "candidate" / name)
    write_manifest(output, source, version, expected_group(platform), platform, candidate_only=candidate_only)
    return validate_manifest(output, source, version, platform=platform, candidate_only=candidate_only)


def prepare_native(source, version, inputs, output, *, candidate_only=False):
    """Stage only manifests/bytes verified from this workflow's exact native groups."""
    if output.exists():
        raise ArtifactError("native coordinator input directory must be fresh")
    groups = {}
    for directory in inputs:
        platform = json.loads(regular(directory, directory / "manifest.json").read_text()).get("platform")
        if platform not in NATIVE_GROUPS or platform in groups:
            raise ArtifactError("coordinator requires exactly one group per native desktop target")
        validate_manifest(directory, source, version, platform=platform, candidate_only=candidate_only)
        groups[platform] = directory
    if set(groups) != set(NATIVE_GROUPS):
        raise ArtifactError("coordinator is missing a native desktop platform")
    output.mkdir(parents=True)
    files = {}
    for platform, directory in groups.items():
        target = NATIVE_GROUPS[platform]
        shutil.copytree(directory / "candidate", output / target)
        files.update({f"{target}/{path.name}": digest(path) for path in (output / target).iterdir()})
    (output / "native-inputs.json").write_text(json.dumps({"source_commit": source, "version": version, "candidate_only": candidate_only, "files": files}, sort_keys=True) + "\n")
    return validate_native_inputs(output, source, version, candidate_only=candidate_only)


def validate_native_inputs(directory, source, version, *, candidate_only=False):
    identity(source, version)
    value = json.loads(regular(directory, directory / "native-inputs.json").read_text())
    expected = {f"{target}/{name}" for target in NATIVE_GROUPS.values() for name in
                (CANDIDATE_MANIFEST, DESKTOP_TARGETS[target]["cli"], DESKTOP_TARGETS[target]["asset"])}
    if (not isinstance(value, dict) or value.get("source_commit") != source or value.get("version") != version
            or value.get("candidate_only") is not candidate_only
            or not isinstance(value.get("files"), dict) or set(value["files"]) != expected
            or inventory(directory) != expected | {"native-inputs.json"}):
        raise ArtifactError("native coordinator input identity or inventory mismatch")
    for name, checksum in value["files"].items():
        if digest(regular(directory, directory / name)) != checksum:
            raise ArtifactError("native coordinator input changed after verification")
    return value


def license_files(root):
    files = [root / "LICENSE", root / "THIRD_PARTY_LICENSES.md"]
    files += sorted(path for path in (root / "licenses").rglob("*") if not path.is_dir())
    if len(files) < 3:
        raise ArtifactError("release license files are missing")
    return [regular(root, path) for path in files]


def write_archive(path, binary, licenses, root):
    # Stable archive metadata makes recovery builds of identical inputs comparable.
    with path.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as archive:
            for source, name, mode in [(binary, binary.name, 0o755),
                    *((item, item.relative_to(root).as_posix(), 0o644) for item in licenses)]:
                entry = tarfile.TarInfo(name)
                entry.size, entry.mode = source.stat().st_size, mode
                with source.open("rb") as content:
                    archive.addfile(entry, content)


def assemble(source, version, inputs, output, *, root=ROOT, scanner=scan, verifier=verify_archive, candidate_only=False):
    identity(source, version)
    groups = {}
    for directory in inputs:
        value = json.loads(regular(directory, directory / "manifest.json").read_text())
        if not isinstance(value, dict):
            raise ArtifactError("invalid artifact manifest")
        platform = value.get("platform")
        if platform not in GROUPS or platform in groups:
            raise ArtifactError("expected one input for every native build group")
        validate_manifest(directory, source, version, platform=platform, candidate_only=candidate_only)
        groups[platform] = directory
    if set(groups) != set(GROUPS) or output.exists():
        raise ArtifactError("assembly needs every native build group and a new output directory")
    licenses = license_files(root)
    output.mkdir(parents=True)
    for target in TARGETS:
        directory = groups["macos" if target.startswith("darwin-") else "linux"]
        binary = regular(directory, directory / "bin" / target / binary_name(target))
        scanner(binary)
        write_archive(output / f"rhizome-{target}.tar.gz", binary, licenses, root)
    installer = stage_installer(output, root / "scripts/install/install-rzm.sh")
    scanner(installer)
    for name in DESKTOP:
        shutil.copyfile(groups["macos"] / "desktop" / name, output / name)
    desktop = validate_feed(output, version, source, candidate_only=candidate_only)
    for platform, target in NATIVE_GROUPS.items():
        name = DESKTOP_TARGETS[target]["asset"]
        if digest(groups[platform] / "candidate" / name) != desktop["packages"][target]["sha256"]:
            raise ArtifactError("signed desktop package differs from the selected native producer bytes")
    write_manifest(output, source, version, PAYLOADS, candidate_only=candidate_only)
    checksums = {name: digest(output / name) for name in (*PAYLOADS, "release-manifest.json")}
    (output / "checksums.txt").write_text("".join(f"{checksums[name]}  {name}\n" for name in sorted(checksums)))
    return verify_bundle(source, version, output, root=root, verifier=verifier, candidate_only=candidate_only)


def verify_bundle(source, version, directory, *, root=ROOT, verifier=verify_archive, candidate_only=False):
    value = validate_manifest(directory, source, version, candidate_only=candidate_only)
    expected = {name: digest(directory / name) for name in (*PAYLOADS, "release-manifest.json")}
    checksum_text = "".join(f"{expected[name]}  {name}\n" for name in sorted(expected))
    if (directory / "checksums.txt").read_text() != checksum_text:
        raise ArtifactError("release checksums do not cover the complete bundle")
    desktop = validate_feed(directory, version, source, candidate_only=candidate_only)
    clean = scoped_environment(os.environ, "build")
    for target in REQUIRED_TARGETS:
        package = directory / DESKTOP_TARGETS[target]["asset"]
        verifier(root, package, package.with_name(package.name + ".sig"), environ=clean)
    if (directory / "install-rzm.sh.sha256").read_text() != f"{digest(directory / 'install-rzm.sh')}  install-rzm.sh\n":
        raise ArtifactError("installer checksum mismatch")
    for target in TARGETS:
        with tarfile.open(directory / f"rhizome-{target}.tar.gz", "r:gz") as archive:
            members = archive.getmembers()
            names = {item.name for item in members}
            if (len(names) != len(members) or not {binary_name(target), "LICENSE", "THIRD_PARTY_LICENSES.md"} <= names
                    or not any(name.startswith("licenses/") for name in names)
                    or any(not item.isfile() or item.name.startswith("/") or ".." in Path(item.name).parts
                           or "\\" in item.name or ":" in item.name or Path(item.name).as_posix() != item.name
                           or (item.name not in {binary_name(target), "LICENSE", "THIRD_PARTY_LICENSES.md"} and not item.name.startswith("licenses/")) for item in members)):
                raise ArtifactError(f"archive contents do not match the release contract: {target}")
            desktop_target = {cli: desktop for desktop, cli in DESKTOP_CLI.items()}.get(target)
            if desktop_target:
                stream = archive.extractfile(binary_name(target))
                if stream is None or hashlib.file_digest(stream, "sha256").hexdigest() != desktop["canonicalCliSha256"][desktop_target]:
                    raise ArtifactError("CLI archive differs from the exact canonical desktop sidecar source")
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("collect", "prepare-native", "assemble", "verify"))
    parser.add_argument("--source", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--platform", choices=tuple(GROUPS))
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--extras", type=Path, default=Path(".tmp/goreleaser-extra"))
    parser.add_argument("--candidate", type=Path)
    parser.add_argument("--candidate-only", action="store_true")
    parser.add_argument("--input", type=Path, action="append", default=[])
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "collect" and args.platform and args.output:
            collect(args.platform, args.source, args.version, args.dist, args.extras, args.output, candidate=args.candidate, candidate_only=args.candidate_only)
        elif args.command == "prepare-native" and args.output:
            prepare_native(args.source, args.version, args.input, args.output, candidate_only=args.candidate_only)
        elif args.command == "assemble" and args.output:
            assemble(args.source, args.version, args.input, args.output, candidate_only=args.candidate_only)
        elif args.command == "verify" and len(args.input) == 1:
            verify_bundle(args.source, args.version, args.input[0], candidate_only=args.candidate_only)
        else:
            parser.error("provide the command's platform, input and output arguments")
    except (CandidateError, DesktopSigningError, ValueError, OSError, tarfile.TarError, subprocess.SubprocessError) as error:
        print(f"release artifacts: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
