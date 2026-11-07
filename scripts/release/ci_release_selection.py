#!/usr/bin/env python3
"""Select one approved release snapshot before CI exposes signing credentials."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from release_actions import VERSION_PATH
from release_intent import MANIFEST_PATH, parse_manifest, release_files, validate_prepared
from release_version import source_release_version, validate_next_version

REPOSITORY = "atomicobject/rhizome"
SHA = re.compile(r"[0-9a-f]{40}")


def run(command, root):
    result = subprocess.run(command, cwd=root, capture_output=True, text=True)
    if result.returncode:
        raise ValueError(f"release selection command failed: {command[0]} {command[1]}")
    return result.stdout.strip()


def git(root, *args):
    return run(["git", *args], root)


def prior_tag(root, source, selected):
    tags = [tag for tag in git(root, "tag", "--list", "v[0-9]*").splitlines() if tag != selected]
    if not tags:
        return ""
    return git(root, "describe", "--tags", "--match", "v[0-9]*", "--abbrev=0", source)


def _ordinary_files(root, commit, paths):
    for path in paths:
        record = git(root, "ls-tree", commit, "--", path.as_posix()).split()
        if len(record) != 4 or record[0] != "100644" or record[1] != "blob":
            raise ValueError(f"release input must be an ordinary tracked file: {path}")


def validate_pull_request(root: Path, pr: dict, *, event_sha: str | None = None) -> dict:
    """Validate reviewed intent, exact merge history and the release-file boundary."""
    base, head = pr.get("base", {}), pr.get("head", {})
    if (pr.get("merged") is not True or base.get("ref") != "main"
            or base.get("repo", {}).get("full_name") != REPOSITORY
            or head.get("repo", {}).get("full_name") != REPOSITORY):
        raise ValueError("release requires a merged same-repository PR into main")
    if type(pr.get("number")) is not int or pr["number"] < 1:
        raise ValueError("release PR number must be a positive integer")
    commit, head_sha = pr.get("merge_commit_sha", ""), head.get("sha", "")
    if not SHA.fullmatch(commit) or not SHA.fullmatch(head_sha):
        raise ValueError("release PR must identify exact head and merge commits")
    if event_sha is not None and commit != event_sha:
        raise ValueError("release event SHA differs from the approved merge commit")
    present = subprocess.run(["git", "cat-file", "-e", f"{head_sha}^{{commit}}"], cwd=root, capture_output=True)
    if present.returncode:
        git(root, "fetch", "--no-tags", "origin", f"refs/pull/{pr['number']}/head")
        if git(root, "rev-parse", "FETCH_HEAD") != head_sha:
            raise ValueError("fetched PR head differs from reviewed head")
    manifest = parse_manifest(git(root, "show", f"{commit}:{MANIFEST_PATH.as_posix()}"))
    version, source = manifest["version"], manifest["source_commit"]
    if head.get("ref") != f"release-prep/{version}":
        raise ValueError("release branch must match the reviewed version")
    if git(root, "rev-parse", f"{head_sha}^{{tree}}") != git(root, "rev-parse", f"{commit}^{{tree}}"):
        raise ValueError("merged release tree differs from the reviewed PR head")
    parents = git(root, "rev-list", "--parents", "-n", "1", commit).split()[1:]
    if len(parents) == 2:
        fresh = parents == [source, head_sha]
    elif len(parents) == 1:
        count = pr.get("commits")
        # Squash integrates one commit; rebase preserves the reviewed commit count.
        fresh = parents[0] == source
        if not fresh and type(count) is int and 1 <= count <= 1000:
            fresh = git(root, "rev-parse", f"{commit}~{count}") == source
    else:
        fresh = False
    if not fresh:
        raise ValueError("main changed before release approval; refresh the release PR")
    if prior_tag(root, source, version) != manifest["base_tag"]:
        raise ValueError("previous version tag changed since release preparation")
    validate_next_version(source_release_version(git(root, "show", f"{source}:{VERSION_PATH.as_posix()}")), version)
    if manifest["base_tag"]:
        validate_next_version(manifest["base_tag"], version)
    paths = release_files(version)
    _ordinary_files(root, commit, paths)
    changed = set(git(root, "diff", "--name-only", source, commit).splitlines())
    allowed = {path.as_posix() for path in paths}
    if changed != allowed:
        raise ValueError("release PR must change only the approved release files")
    if git(root, "status", "--porcelain") or git(root, "rev-parse", "HEAD") != commit:
        raise ValueError("release validation checkout must be clean at the approved merge SHA")
    validate_prepared(root)
    return {"source": commit, "version": version, "release_pr": str(pr["number"]), "candidate_only": "false"}


class GhPulls:
    """Read pull requests through the workflow's read-only token."""

    def __init__(self, root: Path) -> None:
        self.root = root

    def pull(self, number: int) -> dict:
        return json.loads(run(["gh", "api", f"repos/{REPOSITORY}/pulls/{number}"], self.root))

    def for_commit(self, sha: str) -> list[dict]:
        return json.loads(run(["gh", "api", f"repos/{REPOSITORY}/commits/{sha}/pulls"], self.root))


def _release_pr(pr: dict) -> bool:
    return bool(pr.get("merged")) and pr.get("head", {}).get("ref", "").startswith("release-prep/")


def select(root: Path, event: dict, event_name: str, sha: str, *, candidate=False,
           release_pr: int | None = None, pulls=None) -> dict | None:
    if not SHA.fullmatch(sha):
        raise ValueError("workflow source must be an exact commit SHA")
    if event.get("repository", {}).get("full_name") != REPOSITORY:
        raise ValueError("release workflow is restricted to the public Rhizome repository")
    pulls = pulls or GhPulls(root)
    if candidate:
        if event_name != "workflow_dispatch" or release_pr is not None:
            raise ValueError("candidate mode requires an explicit manual dispatch without a release PR")
        if git(root, "rev-parse", "HEAD") != sha or git(root, "status", "--porcelain"):
            raise ValueError("candidate checkout must be clean at the selected workflow SHA")
        version = source_release_version((root / VERSION_PATH).read_text())
        return {"source": sha, "version": version, "release_pr": "", "candidate_only": "true"}
    if event_name == "push":
        # The protected release environment admits only main, so publication runs
        # from the push a release-PR merge makes, never from the PR's own ref.
        numbers = {pr["number"] for pr in pulls.for_commit(sha)
                   if pr.get("merge_commit_sha") == sha and pr.get("head", {}).get("ref", "").startswith("release-prep/")}
        if not numbers:
            return None
        if len(numbers) != 1:
            raise ValueError("release commit must belong to exactly one release PR")
        pr = pulls.pull(numbers.pop())
        return validate_pull_request(root, pr, event_sha=sha) if _release_pr(pr) else None
    if event_name == "workflow_dispatch" and release_pr is not None:
        # Starts a release whose merge started no publishing run. Anything
        # already published is recovered by retrying the original failed job.
        pr = pulls.pull(release_pr)
        if not _release_pr(pr):
            raise ValueError("release_pr must name a merged release-prep PR")
        version = pr["head"]["ref"].removeprefix("release-prep/")
        if git(root, "ls-remote", "--tags", "origin", f"refs/tags/{version}"):
            raise ValueError(f"{version} is already tagged; recover by retrying the original failed publisher job")
        commit = pr.get("merge_commit_sha", "")
        if not SHA.fullmatch(commit):
            raise ValueError("release PR must identify exact head and merge commits")
        git(root, "merge-base", "--is-ancestor", commit, sha)
        git(root, "checkout", "--quiet", "--detach", commit)
        return validate_pull_request(root, pr)
    raise ValueError("manual dispatch is for candidates or a release PR whose merge started no release; "
                     "recover a started release by retrying the original failed publisher job")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event-file", type=Path, required=True)
    parser.add_argument("--event-name", required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--candidate-only", choices=("true", "false"), default="false")
    parser.add_argument("--release-pr", default="")
    args = parser.parse_args(argv)
    try:
        release_pr = int(args.release_pr) if args.release_pr else None
        chosen = select(Path.cwd(), json.loads(args.event_file.read_text()), args.event_name,
                        args.source, candidate=args.candidate_only == "true", release_pr=release_pr)
        if chosen:
            output = os.environ.get("GITHUB_OUTPUT")
            if output:
                with open(output, "a") as stream:
                    for key, value in chosen.items():
                        stream.write(f"{key}={value}\n")
            print(json.dumps(chosen))
        else:
            print("No release PR merged in this commit: no release selected.")
    except (ValueError, OSError, KeyError, TypeError, json.JSONDecodeError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
