#!/usr/bin/env python3
"""Transfer source-bound frontend output from Linux to native build jobs."""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile

from release_actions import validate_release_version

PREFIXES = ("pkg/app/web/assets/dist/", "desktop/dist/")
MANIFEST = "frontend-manifest.json"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def asset_path(name):
    path = PurePosixPath(name)
    if path.is_absolute() or path.as_posix() != name or ".." in path.parts or "\\" in name or not name.startswith(PREFIXES):
        raise ValueError("frontend archive contains an unexpected path")
    return path


def checkout_path(root, path):
    if not path.resolve().is_relative_to(root.resolve()):
        raise ValueError("frontend output escapes checkout")
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise ValueError("frontend output must not contain symlinks")
        if parent == root:
            return
    raise ValueError("frontend output escapes checkout")


def source_check(root, source, version):
    validate_release_version(version)
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("frontend source must be an exact commit SHA")
    current = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    if current != source:
        raise ValueError("frontend checkout differs from selected source")
    dirty = []
    for command in (["git", "diff", "--name-only", "-z", "HEAD"], ["git", "ls-files", "--others", "--exclude-standard", "-z"]):
        dirty.extend(filter(None, subprocess.check_output(command, cwd=root, text=True).split("\0")))
    if any(not name.startswith("pkg/app/web/assets/dist/") for name in dirty):
        raise ValueError("frontend source has unrelated uncommitted changes")


def create(root, source, version, output):
    source_check(root, source, version)
    contents = {}
    for prefix in PREFIXES:
        directory = root / prefix
        checkout_path(root, directory)
        if not (directory / "index.html").is_file():
            raise ValueError(f"frontend has not been built: {prefix}")
        for path in sorted(directory.rglob("*")):
            if path.is_symlink():
                raise ValueError("frontend output must not contain symlinks")
            if path.is_file():
                name = path.relative_to(root).as_posix()
                asset_path(name)
                contents[name] = path.read_bytes()
    manifest = {"schema_version": 1, "source": source, "version": version,
                "files": {name: digest(data) for name, data in contents.items()}}
    contents[MANIFEST] = json.dumps(manifest, sort_keys=True).encode()
    output.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(output, "w:gz") as archive:
        for name, data in contents.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode, info.mtime = len(data), 0o644, 0
            archive.addfile(info, io.BytesIO(data))


def extract(root, source, version, archive_path):
    source_check(root, source, version)
    with tarfile.open(archive_path, "r:gz") as archive:
        members = archive.getmembers()
        names = [member.name for member in members]
        if len(names) != len(set(names)) or any(not member.isfile() for member in members):
            raise ValueError("frontend archive requires unique ordinary files")
        if MANIFEST not in names:
            raise ValueError("frontend archive is missing its source manifest")
        manifest = json.loads(archive.extractfile(MANIFEST).read())
        if (set(manifest) != {"schema_version", "source", "version", "files"}
                or type(manifest["schema_version"]) is not int or manifest["schema_version"] != 1
                or manifest["source"] != source or manifest["version"] != version
                or not isinstance(manifest["files"], dict)):
            raise ValueError("frontend manifest differs from selected source/version")
        expected = set(manifest["files"])
        if set(names) != expected | {MANIFEST} or not all(prefix + "index.html" in expected for prefix in PREFIXES):
            raise ValueError("frontend archive has missing or extra files")
        contents = {}
        for name in expected:
            asset_path(name)
            data = archive.extractfile(name).read()
            if digest(data) != manifest["files"][name]:
                raise ValueError("frontend artifact hash mismatch")
            path = root / name
            checkout_path(root, path)
            contents[path] = data
        # Replace only these generated directories after every byte is verified.
        for prefix in PREFIXES:
            directory = root / prefix
            checkout_path(root, directory)
        for prefix in PREFIXES:
            shutil.rmtree(root / prefix, ignore_errors=True)
        for path, data in contents.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("create", "extract"))
    parser.add_argument("--source", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--archive", type=Path, required=True)
    args = parser.parse_args()
    (create if args.mode == "create" else extract)(Path.cwd(), args.source, args.version, args.archive)


if __name__ == "__main__":
    main()
