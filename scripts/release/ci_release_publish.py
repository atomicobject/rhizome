#!/usr/bin/env python3
"""Publish verified CI artifacts without rebuilding or rewriting reviewed notes."""
from __future__ import annotations

import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

from ci_release_artifacts import BUNDLE_FILES, digest, identity, verify_bundle
from release_gateway import GatewayError, ensure_gateway
from release_intent import notes_path, validate_prepared
from release_publication import PublicationError, gate_direct_publication, tagged_release, validate_topology
from run_clean import scoped_environment

REPOSITORY = "atomicobject/rhizome"
TAP = "atomicobject/homebrew-tap"
API = f"repos/{REPOSITORY}"


class PublishError(ValueError):
    pass


class Commands:
    def __init__(self, root, environment, runner=subprocess.run):
        self.root, self.runner = root, runner
        self.environment = scoped_environment(environment, "build")
        self.github_token = environment.get("GH_TOKEN") or environment.get("GITHUB_TOKEN")
        self.brew_token = environment.get("BREW_GITHUB_TOKEN")
        if not self.github_token or not self.brew_token:
            raise PublishError("CI publication requires GitHub and Homebrew credentials")

    def run(self, argv, *, payload=None, brew=False, check=True, timeout=120):
        env = {**self.environment, "GH_TOKEN": self.brew_token if brew else self.github_token}
        try:
            result = self.runner(tuple(argv), cwd=self.root, env=env, capture_output=True,
                                 text=True, input=None if payload is None else json.dumps(payload), timeout=timeout)
        except (OSError, subprocess.SubprocessError) as error:
            raise PublishError("publication command failed; retain the CI run for recovery") from error
        if check and result.returncode:
            raise PublishError("publication command failed; retain the CI run for recovery")
        return result

    def text(self, argv):
        return self.run(argv).stdout

    def api(self, endpoint, *, method="GET", payload=None, brew=False):
        argv = ["gh", "api", endpoint, "--method", method]
        if payload is not None:
            argv += ["--input", "-"]
        try:
            return json.loads(self.run(argv, payload=payload, brew=brew).stdout)
        except (TypeError, ValueError) as error:
            raise PublishError("GitHub returned invalid publication metadata") from error


def remote_refs(commands, version):
    output = commands.text(("git", "ls-remote", "origin", "refs/heads/release", f"refs/tags/{version}", f"refs/tags/{version}^{{}}"))
    refs = {}
    for line in output.splitlines():
        fields = line.split()
        if len(fields) != 2:
            raise PublishError("invalid remote release refs")
        refs[fields[1]] = fields[0]
    return refs.get("refs/heads/release"), refs.get(f"refs/tags/{version}^{{}}") or refs.get(f"refs/tags/{version}")


def restore_gateway_receipt(commands, receipt, environment):
    """Recover correlation, or prove this is the publisher's first invocation."""
    attempt = environment.get("GITHUB_RUN_ATTEMPT", "1")
    if attempt == "1":
        return
    run_id = environment.get("GITHUB_RUN_ID", "")
    if not re.fullmatch(r"[1-9][0-9]*", attempt) or not re.fullmatch(r"[1-9][0-9]*", run_id):
        raise PublishError("invalid CI retry identity")

    def listing(endpoint, field):
        items, page = [], 1
        while True:
            value = commands.api(f"{API}/actions/runs/{run_id}/{endpoint}?per_page=100&page={page}")
            if not isinstance(value, dict) or not isinstance(value.get(field), list):
                raise PublishError("cannot verify prior publisher state")
            items.extend(value[field])
            if len(value[field]) < 100:
                return items
            page += 1

    artifacts = listing("artifacts", "artifacts")
    saved = [item for item in artifacts if item.get("name") == "release-gateway-receipt" and not item.get("expired")]
    if len(saved) > 1:
        raise PublishError("ambiguous retained gateway receipts")
    if saved:
        receipt.parent.mkdir(parents=True, exist_ok=True)
        commands.run(("gh", "run", "download", run_id, "--repo", REPOSITORY,
                      "--name", "release-gateway-receipt", "--dir", str(receipt.parent)))
        if not receipt.is_file() or receipt.is_symlink():
            raise PublishError("retained gateway receipt is missing")
        return
    for number in range(1, int(attempt)):
        for job in listing(f"attempts/{number}/jobs", "jobs"):
            if job.get("name") != "Publish approved release and Homebrew update":
                continue
            for step in job.get("steps", []):
                if (step.get("name") == "Publish approved release and update Homebrew"
                        and step.get("started_at") and step.get("conclusion") != "skipped"):
                    raise PublishError("prior publisher started without a retained gateway receipt; reconcile its deployment before retrying")


def advance_refs(commands, source, intent):
    version = intent["version"]
    if commands.text(("git", "rev-parse", "HEAD")).strip() != source or commands.text(("git", "status", "--porcelain")).strip():
        raise PublishError("publication checkout must be clean and pinned to the approved merge")
    commands.run(("git", "fetch", "origin", "main", "--tags"))
    commands.run(("git", "merge-base", "--is-ancestor", source, "refs/remotes/origin/main"))
    previous = commands.text(("git", "rev-parse", f"{intent['base_tag']}^{{commit}}")).strip() if intent["base_tag"] else None
    release, tag = remote_refs(commands, version)
    if release not in {previous, source} or tag not in {None, source}:
        raise PublishError("conflicting canonical release branch or tag")
    local = commands.run(("git", "rev-parse", "--verify", f"refs/tags/{version}^{{commit}}"), check=False)
    if local.returncode == 0 and local.stdout.strip() != source:
        raise PublishError("conflicting local release tag")
    if local.returncode != 0:
        commands.run(("git", "tag", version, source))
    if release != source or tag != source:
        commands.run(("git", "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential",
                      "push", "--atomic", "origin", f"{source}:refs/heads/release", f"{source}:refs/tags/{version}"))
    if remote_refs(commands, version) != (source, source):
        raise PublishError("release refs did not resolve to the approved source")


def run_gateway(root, source, receipt, commands):
    # The shared gate owns correlation, fresh verification and topology checks.
    return gate_direct_publication(root,
        runner=lambda argv, cwd: commands.run(argv, check=False),
        gateway=lambda repo, commit, path, **kwargs: ensure_gateway(repo, commit, receipt, runner=commands.text))


def revalidate_refs(root, source, commands):
    runner = lambda argv, cwd: commands.run(argv, check=False)
    if tagged_release(root, runner=runner) != source:
        raise PublishError("publication checkout changed")
    validate_topology(root, source, runner=runner)


def release_for_tag(commands, version):
    # A paginated listing distinguishes an absent tag from authentication/API errors.
    pages = commands.api(f"{API}/releases?per_page=100", method="GET")
    if not isinstance(pages, list) or any(not isinstance(item, dict) for item in pages):
        raise PublishError("invalid GitHub release listing")
    matches = [item for item in pages if item.get("tag_name") == version]
    page = 1
    while len(pages) == 100:
        page += 1
        pages = commands.api(f"{API}/releases?per_page=100&page={page}")
        if not isinstance(pages, list) or any(not isinstance(item, dict) for item in pages):
            raise PublishError("invalid GitHub release listing")
        matches += [item for item in pages if item.get("tag_name") == version]
    if len(matches) > 1:
        raise PublishError("ambiguous GitHub release state")
    return matches[0] if matches else None


def asset_matches(commands, asset, path, version):
    if asset.get("size") != path.stat().st_size:
        return False
    expected = "sha256:" + digest(path)
    checksum = asset.get("digest")
    if checksum is not None:
        return checksum == expected
    asset_id = asset.get("id")
    if type(asset_id) is not int or asset_id <= 0:
        return False
    # Older GitHub responses omit digest; verify downloaded bytes rather than size alone.
    with tempfile.TemporaryDirectory(prefix="rzm-release-asset-") as temporary:
        destination = Path(temporary) / path.name
        commands.run(("gh", "release", "download", version, "--repo", REPOSITORY,
                      "--pattern", path.name, "--output", str(destination)))
        return destination.is_file() and digest(destination) == expected.removeprefix("sha256:")


def checked_assets(commands, release, source, version, notes, assets, *, complete=False):
    if (not isinstance(release, dict) or release.get("tag_name") != version
            or release.get("target_commitish") != source or release.get("body") != notes
            or type(release.get("draft")) is not bool or release.get("prerelease") is not False
            or type(release.get("id")) is not int or release["id"] <= 0):
        raise PublishError("existing release does not match the approved notes/source")
    listed = commands.api(f"{API}/releases/{release['id']}/assets?per_page=100")
    if (not isinstance(listed, list) or len(listed) >= 100
            or any(not isinstance(item, dict) or not isinstance(item.get("name"), str) for item in listed)):
        raise PublishError("invalid or incomplete release asset listing")
    names = [item.get("name") for item in listed]
    if len(names) != len(set(names)) or not set(names) <= BUNDLE_FILES or (complete and set(names) != BUNDLE_FILES):
        raise PublishError("release contains conflicting or missing assets")
    for item in listed:
        if not asset_matches(commands, item, assets / item["name"], version):
            raise PublishError("existing release asset differs from the verified bundle")
    return set(names)


def formula(version, checksums):
    lines = ['# frozen_string_literal: true', 'class Rhizome < Formula',
             '  desc "Repository knowledge graph for agents"',
             f'  homepage "https://github.com/{REPOSITORY}"', f'  version "{version.removeprefix("v")}"', '']
    for os_name, targets in (("macos", ("darwin-arm64", "darwin-amd64")), ("linux", ("linux-arm64", "linux-amd64"))):
        lines += [f"  on_{os_name} do", "    if Hardware::CPU.arm?"]
        for index, target in enumerate(targets):
            name = f"rhizome-{target}.tar.gz"
            if index:
                lines.append("    else")
            lines += [f'      url "https://github.com/{REPOSITORY}/releases/download/{version}/{name}"',
                      f'      sha256 "{checksums[name]}"']
        lines += ["    end", "  end", ""]
    lines += ["  def install", '    bin.install "rzm"', "  end", "", "  test do", '    system "#{bin}/rzm", "--version"', "  end", "end", ""]
    return "\n".join(lines)


def update_homebrew(commands, version, assets, *, before_update=None):
    content = formula(version, {name: digest(assets / name) for name in BUNDLE_FILES})
    endpoint = f"repos/{TAP}/contents/rhizome.rb"
    current = commands.api(endpoint + "?ref=main", brew=True)
    try:
        existing = base64.b64decode(current["content"], validate=False).decode()
        sha = current["sha"]
    except (KeyError, TypeError, ValueError) as error:
        raise PublishError("invalid Homebrew formula metadata") from error
    if existing == content:
        return
    if before_update:
        before_update()
    commands.api(endpoint, method="PUT", brew=True, payload={"message": f"Update Rhizome to {version}",
        "content": base64.b64encode(content.encode()).decode(), "sha": sha, "branch": "main"})


def publish(root, source, assets, receipt, *, environment=None, runner=subprocess.run,
            validator=validate_prepared, bundle_verifier=verify_bundle, gate=run_gateway):
    root, assets = Path(root).resolve(), Path(assets).resolve()
    environment = os.environ if environment is None else environment
    if (environment.get("GITHUB_ACTIONS") != "true" or environment.get("GITHUB_REPOSITORY") != REPOSITORY
            or environment.get("GITHUB_REF") != "refs/heads/main"):
        raise PublishError("public publication runs only in the approved GitHub release workflow")
    intent = validator(root)
    version = intent["version"]
    identity(source, version)
    bundle_verifier(source, version, assets)
    notes = (root / notes_path(version)).read_bytes().decode("utf-8")
    commands = Commands(root, environment, runner)
    restore_gateway_receipt(commands, receipt, environment)
    # Inspect existing releases before mutating refs, then recheck after the gate.
    existing = release_for_tag(commands, version)
    if existing:
        checked_assets(commands, existing, source, version, notes, assets, complete=existing.get("draft") is False)
    advance_refs(commands, source, intent)
    if existing and not existing["draft"]:
        revalidate_refs(root, source, commands)
        def gate_recovery():
            gate(root, source, receipt, commands)
            revalidate_refs(root, source, commands)
            bundle_verifier(source, version, assets)
        update_homebrew(commands, version, assets, before_update=gate_recovery)
        return
    gate(root, source, receipt, commands)
    revalidate_refs(root, source, commands)
    bundle_verifier(source, version, assets)
    release = release_for_tag(commands, version)
    if release is None:
        release = commands.api(f"{API}/releases", method="POST", payload={"tag_name": version,
            "target_commitish": source, "name": version, "body": notes, "draft": True, "prerelease": False})
    if not release.get("draft"):
        checked_assets(commands, release, source, version, notes, assets, complete=True)
    else:
        present = checked_assets(commands, release, source, version, notes, assets)
        for name in sorted(BUNDLE_FILES - present):
            commands.run(("gh", "release", "upload", version, str(assets / name), "--repo", REPOSITORY), timeout=600)
        bundle_verifier(source, version, assets)
        checked_assets(commands, release, source, version, notes, assets, complete=True)
        revalidate_refs(root, source, commands)
        commands.api(f"{API}/releases/{release['id']}", method="PATCH", payload={"draft": False})
    update_homebrew(commands, version, assets)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    parser.add_argument("--repo-root", type=Path, default=Path.cwd())
    args = parser.parse_args(argv)
    try:
        publish(args.repo_root, args.source, args.assets, args.receipt)
    except (ValueError, OSError, GatewayError, PublicationError, subprocess.SubprocessError) as error:
        print(f"release publication: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
