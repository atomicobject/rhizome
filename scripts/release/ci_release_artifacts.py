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

ROOT = Path(__file__).resolve().parents[2]
TARGETS = ("darwin-arm64", "darwin-amd64", "linux-arm64", "linux-amd64", "windows-amd64")
GROUPS = {"linux": TARGETS[2:], "macos": TARGETS[:2]}
DESKTOP = ("Rhizome-macos-arm64.dmg", "Rhizome-macos-arm64.app.tar.gz",
           "Rhizome-macos-arm64.app.tar.gz.sig", "latest.json")
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
        {f"desktop/{name}" for name in DESKTOP} if platform == "macos" else set())


def write_manifest(directory, source, version, files, platform=None):
    value = {"schema_version": 1, "source_commit": source, "version": version,
             "files": {name: digest(directory / name) for name in sorted(files)}}
    if platform:
        value["platform"] = platform
    name = "manifest.json" if platform else "release-manifest.json"
    (directory / name).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    return value


def validate_manifest(directory, source, version, *, platform=None):
    identity(source, version)
    name = "manifest.json" if platform else "release-manifest.json"
    value = json.loads(regular(directory, directory / name).read_text())
    keys = {"schema_version", "source_commit", "version", "files"} | ({"platform"} if platform else set())
    expected = expected_group(platform) if platform else set(PAYLOADS)
    if (not isinstance(value, dict) or set(value) != keys or type(value["schema_version"]) is not int or value["schema_version"] != 1
            or value["source_commit"] != source or value["version"] != version
            or (platform and value["platform"] != platform) or not isinstance(value["files"], dict)
            or set(value["files"]) != expected):
        raise ArtifactError("artifact manifest identity or asset set mismatch")
    allowed = expected | {name} if platform else BUNDLE_FILES
    if inventory(directory) != allowed:
        raise ArtifactError("unexpected or missing artifact files")
    for relative, checksum in value["files"].items():
        if not isinstance(checksum, str) or not re.fullmatch(r"[0-9a-f]{64}", checksum) or digest(regular(directory, directory / relative)) != checksum:
            raise ArtifactError(f"artifact checksum mismatch: {relative}")
    return value


def validate_feed(directory, version):
    value = json.loads((directory / "latest.json").read_text())
    if not isinstance(value, dict) or not isinstance(value.get("platforms"), dict):
        raise ArtifactError("invalid desktop updater feed")
    platform = value["platforms"].get("darwin-aarch64", {})
    signature = (directory / DESKTOP[2]).read_text().strip()
    if (not isinstance(platform, dict) or set(value["platforms"]) != {"darwin-aarch64"}
            or value.get("version") != version.removeprefix("v") or not signature
            or platform.get("signature") != signature
            or platform.get("url") != f"https://github.com/atomicobject/rhizome/releases/download/{version}/{DESKTOP[1]}"):
        raise ArtifactError("desktop updater feed does not match the release")


def collect(platform, source, version, dist, extras, output, *, scanner=scan, inspector=build_info):
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
        validate_feed(output / "desktop", version)
    write_manifest(output, source, version, expected_group(platform), platform)
    return validate_manifest(output, source, version, platform=platform)


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


def assemble(source, version, inputs, output, *, root=ROOT, scanner=scan):
    identity(source, version)
    groups = {}
    for directory in inputs:
        value = json.loads(regular(directory, directory / "manifest.json").read_text())
        if not isinstance(value, dict):
            raise ArtifactError("invalid artifact manifest")
        platform = value.get("platform")
        if platform not in GROUPS or platform in groups:
            raise ArtifactError("expected one Linux and one macOS input")
        validate_manifest(directory, source, version, platform=platform)
        groups[platform] = directory
    if set(groups) != set(GROUPS) or output.exists():
        raise ArtifactError("assembly needs both platforms and a new output directory")
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
    validate_feed(output, version)
    write_manifest(output, source, version, PAYLOADS)
    checksums = {name: digest(output / name) for name in (*PAYLOADS, "release-manifest.json")}
    (output / "checksums.txt").write_text("".join(f"{checksums[name]}  {name}\n" for name in sorted(checksums)))
    return verify_bundle(source, version, output)


def verify_bundle(source, version, directory):
    value = validate_manifest(directory, source, version)
    expected = {name: digest(directory / name) for name in (*PAYLOADS, "release-manifest.json")}
    checksum_text = "".join(f"{expected[name]}  {name}\n" for name in sorted(expected))
    if (directory / "checksums.txt").read_text() != checksum_text:
        raise ArtifactError("release checksums do not cover the complete bundle")
    validate_feed(directory, version)
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
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("collect", "assemble", "verify"))
    parser.add_argument("--source", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--platform", choices=tuple(GROUPS))
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--extras", type=Path, default=Path(".tmp/goreleaser-extra"))
    parser.add_argument("--input", type=Path, action="append", default=[])
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "collect" and args.platform and args.output:
            collect(args.platform, args.source, args.version, args.dist, args.extras, args.output)
        elif args.command == "assemble" and args.output:
            assemble(args.source, args.version, args.input, args.output)
        elif args.command == "verify" and len(args.input) == 1:
            verify_bundle(args.source, args.version, args.input[0])
        else:
            parser.error("provide the command's platform, input and output arguments")
    except (ValueError, OSError, tarfile.TarError, subprocess.SubprocessError) as error:
        print(f"release artifacts: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
