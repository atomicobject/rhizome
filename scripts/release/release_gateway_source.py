#!/usr/bin/env python3
"""Select public gateway source and compare committed Docker build inputs."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re
import shlex
import subprocess
from typing import Callable, Sequence


REPOSITORY = "atomicobject/rhizome"
REQUIRED = (
    "go.mod", "go.sum", "pkg/diagnostics", "pkg/fileio", "pkg/typesafe",
    "pkg/teamapi/wire", "services/team-api",
)
OPTIONAL = (".dockerignore", "services/team-api/Dockerfile.dockerignore")
DOCKERFILE = "services/team-api/Dockerfile"
SHA = re.compile(r"[0-9a-f]{40}\Z")
Runner = Callable[[Sequence[str]], str]


class SourceError(RuntimeError):
    """Source cannot be selected or proven to agree without guessing."""


def _run(argv: Sequence[str]) -> str:
    try:
        result = subprocess.run(argv, capture_output=True, text=True, timeout=90)
    except (OSError, subprocess.SubprocessError) as error:
        raise SourceError("gateway source lookup failed") from error
    if result.returncode or len(result.stdout) > 16 * 1024 * 1024:
        # Command arguments and diagnostics can contain private repository names.
        raise SourceError("gateway source lookup failed")
    return result.stdout


def _sha(value: object) -> str:
    if not isinstance(value, str) or not SHA.fullmatch(value):
        raise SourceError("gateway source requires a full Git commit or object hash")
    return value


class SourceReader:
    def __init__(self, root: Path, runner: Runner) -> None:
        self.root, self.runner = root, runner
        self.trees: dict[str, dict] = {}
        self.commits: dict[str, dict] = {}

    def run(self, argv: Sequence[str]) -> str:
        try:
            result = self.runner(argv)
            if not isinstance(result, str) or len(result) > 16 * 1024 * 1024:
                raise SourceError("gateway source lookup failed")
            return result
        except Exception as error:
            raise SourceError("gateway source lookup failed") from error

    def git(self, *args: str) -> str:
        return self.run(("git", "-C", str(self.root), *args))

    def api(self, path: str) -> dict:
        try:
            value = json.loads(self.run(("gh", "api", f"repos/{path}")))
        except (ValueError, TypeError) as error:
            raise SourceError("gateway source lookup returned invalid metadata") from error
        if not isinstance(value, dict):
            raise SourceError("gateway source lookup returned invalid metadata")
        return value

    def verify_mode(self, internal: bool) -> None:
        origin = self.git("remote", "get-url", "origin").strip()
        match = re.fullmatch(
            r"(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)"
            r"([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?", origin,
        )
        if not match:
            raise SourceError("release origin must identify a GitHub repository")
        name = match[1]
        metadata = self.api(name)
        if not isinstance(metadata.get("full_name"), str) or metadata["full_name"].lower() != name.lower():
            raise SourceError("release repository identity could not be verified")
        if internal:
            if metadata.get("private") is not True or name.lower() == REPOSITORY:
                raise SourceError("internal gateway gate requires a private release repository")
            public = self.api(REPOSITORY)
            if public.get("full_name") != REPOSITORY or public.get("private") is not False:
                raise SourceError("canonical gateway repository must be public")
        elif name.lower() != REPOSITORY or metadata.get("private") is not False:
            raise SourceError("public gateway gate requires the canonical public repository")

    def ref(self, branch: str) -> str:
        try:
            value = self.api(f"{REPOSITORY}/git/ref/heads/{branch}")
            if value["object"]["type"] != "commit":
                raise KeyError("type")
            return _sha(value["object"]["sha"])
        except (SourceError, KeyError, TypeError) as error:
            if branch == "release":
                raise SourceError(
                    "public release branch is unavailable; publish canonical release "
                    "branches through the existing release flow before deploying"
                ) from error
            raise SourceError("public main branch could not be verified") from error

    def commit(self, sha: str) -> dict:
        if sha not in self.commits:
            value = self.api(f"{REPOSITORY}/git/commits/{_sha(sha)}")
            if value.get("sha") != sha or not isinstance(value.get("parents"), list):
                raise SourceError("public commit metadata could not be verified")
            try:
                _sha(value["tree"]["sha"])
                for parent in value["parents"]:
                    _sha(parent["sha"])
            except (KeyError, TypeError) as error:
                raise SourceError("public commit metadata could not be verified") from error
            self.commits[sha] = value
        return self.commits[sha]

    def canonical(self, source: str, main: str) -> None:
        current = main
        seen: set[str] = set()
        for _ in range(2000):
            if current == source:
                return
            if current in seen:
                break
            seen.add(current)
            parents = self.commit(current)["parents"]
            if len(parents) == 2 and parents[1]["sha"] == source:
                return
            if not parents:
                break
            current = parents[0]["sha"]
        raise SourceError(f"public release {source} lacks canonical main integration")

    def tree(self, sha: str) -> dict:
        if sha not in self.trees:
            value = self.api(f"{REPOSITORY}/git/trees/{_sha(sha)}")
            if value.get("sha") != sha or value.get("truncated") is not False:
                raise SourceError("public source tree is incomplete")
            entries = value.get("tree")
            if not isinstance(entries, list):
                raise SourceError("public source tree is invalid")
            result = {}
            for entry in entries:
                try:
                    name, mode, kind, oid = (
                        entry["path"], entry["mode"], entry["type"], _sha(entry["sha"])
                    )
                except (KeyError, TypeError) as error:
                    raise SourceError("public source tree is invalid") from error
                if not isinstance(name, str) or "/" in name or name in result:
                    raise SourceError("public source tree is invalid")
                if not isinstance(mode, str) or not isinstance(kind, str) or (mode, kind) not in {
                    ("040000", "tree"), ("100644", "blob"), ("100755", "blob"),
                    ("120000", "blob"), ("160000", "commit"),
                }:
                    raise SourceError("public source tree is invalid")
                result[name] = (mode, kind, oid)
            self.trees[sha] = result
        return self.trees[sha]

    def remote_entry(self, tree: str, path: str) -> tuple | None:
        parts = path.split("/")
        for index, name in enumerate(parts):
            entry = self.tree(tree).get(name)
            if entry is None:
                return None
            if index == len(parts) - 1:
                return entry
            if entry[1] != "tree":
                raise SourceError(f"public build input parent is not a tree: {path}")
            tree = entry[2]
        return None

    def local_entry(self, commit: str, path: str) -> tuple | None:
        output = self.git("ls-tree", "-z", commit, "--", path)
        if not output:
            return None
        try:
            metadata, found = output.removesuffix("\0").split("\t")
            mode, kind, oid = metadata.split()
            if found != path:
                raise ValueError("path")
            return mode, kind, _sha(oid)
        except ValueError as error:
            raise SourceError(f"committed build input could not be read: {path}") from error


def validate_copy_sources(dockerfile: str) -> None:
    """Support simple current COPY instructions, rejecting uncovered context reads."""
    if re.search(r"(?im)^\s*#\s*(?:escape|syntax)\s*=", dockerfile):
        raise SourceError(f"unsupported Dockerfile parser directive: {DOCKERFILE}")
    stages: set[str] = set()
    logical, prefix, continued = [], "", False
    for raw in dockerfile.splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        line = prefix + raw.lstrip()
        if line.endswith("\\"):
            prefix, continued = line[:-1], True
            continue
        logical.append((line, continued))
        prefix, continued = "", False
    if prefix:
        raise SourceError(f"unsupported Dockerfile continuation: {DOCKERFILE}")
    for line, continued in logical:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        split = stripped.split(None, 1)
        instruction, body = split[0], split[1] if len(split) == 2 else ""
        if instruction.upper() == "RUN" and re.search(r"(?:^|\s)--mount(?:=|\s)", body):
            raise SourceError(f"unreviewed Dockerfile RUN mount input: {DOCKERFILE}")
        if instruction.upper() == "FROM":
            words = body.split()
            if len(words) >= 3 and words[-2].upper() == "AS":
                stages.add(words[-1])
        if instruction.upper() != "COPY":
            # ADD also reads the build context, and is deliberately unsupported.
            if instruction.upper() == "ADD":
                raise SourceError(f"unreviewed Dockerfile ADD input: {DOCKERFILE}")
            continue
        if continued or any(character in body for character in "[]\\\n"):
            raise SourceError(f"unsupported Dockerfile COPY syntax: {DOCKERFILE}")
        try:
            words = shlex.split(body)
        except ValueError as error:
            raise SourceError(f"unsupported Dockerfile COPY syntax: {DOCKERFILE}") from error
        stage = None
        while words and words[0].startswith("--"):
            flag = words.pop(0)
            if flag.startswith("--from=") and stage is None:
                stage = flag.removeprefix("--from=")
            elif not flag.startswith("--chown="):
                raise SourceError(f"unsupported Dockerfile COPY flag: {DOCKERFILE}")
        if len(words) < 2:
            raise SourceError(f"unsupported Dockerfile COPY syntax: {DOCKERFILE}")
        if stage is not None:
            if stage not in stages:
                raise SourceError(f"unreviewed Dockerfile COPY stage: {DOCKERFILE}")
            continue
        for source in words[:-1]:
            allowed = source in REQUIRED or source in {
                "services/team-api/go.mod", "services/team-api/go.sum"
            }
            if not allowed or any(character in source for character in "*?$!"):
                raise SourceError(f"unreviewed Dockerfile COPY input: {DOCKERFILE}")


def resolve_source(
    repo_root: Path, release_commit: str, *, saved_source: str | None = None,
    internal: bool = False, runner: Runner | None = None,
) -> dict:
    release_commit = _sha(release_commit)
    if saved_source is not None:
        saved_source = _sha(saved_source)
    reader = SourceReader(repo_root, runner or _run)
    reader.verify_mode(internal)
    if reader.git("rev-parse", f"{release_commit}^{{commit}}").strip() != release_commit:
        raise SourceError("local client release commit could not be verified")
    if internal and saved_source:
        source = saved_source
    else:
        source = reader.ref("release")
        if not internal and (source != release_commit or saved_source not in {None, source}):
            raise SourceError("public client release must equal the current public release branch")
    main = reader.ref("main")
    reader.canonical(source, main)
    public_tree = reader.commit(source)["tree"]["sha"]
    entries = []
    for path in sorted(REQUIRED + OPTIONAL):
        local = reader.local_entry(release_commit, path)
        public = reader.remote_entry(public_tree, path)
        if local is None or public is None:
            if path not in OPTIONAL or local != public:
                raise SourceError(f"missing committed gateway build input: {path}")
        if local != public:
            raise SourceError(
                f"gateway build input differs: {path} "
                f"{local[2] if local else 'absent'} {public[2] if public else 'absent'}"
            )
        expected = "tree" if path in REQUIRED and path not in {"go.mod", "go.sum"} else "blob"
        if local is not None and (
            local[1] != expected or (expected == "blob" and local[0] not in {"100644", "100755"})
        ):
            raise SourceError(f"unsupported committed gateway build input type: {path}")
        entries.append([path, *(local or ("absent", "absent", "absent"))])
    docker = reader.local_entry(release_commit, DOCKERFILE)
    if docker is None or docker[:2] not in {("100644", "blob"), ("100755", "blob")}:
        raise SourceError(f"required Dockerfile is not a committed regular file: {DOCKERFILE}")
    validate_copy_sources(reader.git("show", f"{release_commit}:{DOCKERFILE}"))
    fingerprint = hashlib.sha256(
        json.dumps(entries, separators=(",", ":"), ensure_ascii=True).encode()
    ).hexdigest()
    return {
        "repository": REPOSITORY, "release_commit": release_commit,
        "source_commit": source, "fingerprint": fingerprint,
    }
