#!/usr/bin/env python3
"""Validate protected deployment inputs before checking out selected source."""

import argparse
from dataclasses import dataclass
from pathlib import Path
import re
import subprocess


PUBLIC_REPOSITORY = "atomicobject/rhizome"
SHA = re.compile(r"[a-f0-9]{40}")
REQUEST_ID = re.compile(r"[a-f0-9]{32}")


@dataclass(frozen=True)
class DeploymentInputs:
    mode: str
    image_tag: str
    source_commit: str = ""


def validate_inputs(repository, ref, source_commit, request_id, image_tag):
    if repository != PUBLIC_REPOSITORY or ref != "refs/heads/main":
        raise ValueError("deployment must run on public main")
    if image_tag:
        if source_commit or request_id:
            raise ValueError("image_tag cannot be combined with source inputs")
        if not SHA.fullmatch(image_tag):
            raise ValueError("image_tag must be a full lowercase commit SHA")
        return DeploymentInputs("image", image_tag)
    if not SHA.fullmatch(source_commit) or not REQUEST_ID.fullmatch(request_id):
        raise ValueError("source mode requires a full lowercase SHA and a 32-character hex request_id")
    return DeploymentInputs("source", source_commit, source_commit)


def validate_source(repo_root, source_commit):
    """Require a current or historical public main ancestor.

    The caller refreshes origin/main from the fixed public repository first.
    Publication separately requires canonical release topology; deployment-only
    smoke tests do not require a release branch to exist yet.
    """
    if not SHA.fullmatch(source_commit):
        raise ValueError("source_commit must be a full lowercase commit SHA")
    root = Path(repo_root)
    for args in (("cat-file", "-e", f"{source_commit}^{{commit}}"),
                 ("merge-base", "--is-ancestor", source_commit, "refs/remotes/origin/main")):
        result = subprocess.run(["git", *args], cwd=root, capture_output=True, text=True)
        if result.returncode:
            raise ValueError("source_commit must exist in canonical public main history")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--ref", required=True)
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--source-commit", default="")
    parser.add_argument("--request-id", default="")
    parser.add_argument("--image-tag", default="")
    args = parser.parse_args()
    selected = validate_inputs(args.repository, args.ref, args.source_commit,
                               args.request_id, args.image_tag)
    if selected.mode == "source":
        subprocess.run(["git", "fetch", "--no-tags", "origin",
                        "+refs/heads/main:refs/remotes/origin/main"],
                       cwd=args.repo_root, check=True)
        validate_source(args.repo_root, selected.source_commit)
    print(f"mode={selected.mode}")
    print(f"image_tag={selected.image_tag}")
    print(f"source_commit={selected.source_commit}")


if __name__ == "__main__":
    main()
