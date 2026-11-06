#!/usr/bin/env python3
"""Publish GoReleaser's retained formula only after release advertisement succeeds.

GoReleaser generates the formula with brews.skip_upload=true for public desktop
releases. This small contents-API handoff keeps the tap from referencing drafts.
"""
from __future__ import annotations

import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.error import HTTPError, URLError
from urllib.request import HTTPRedirectHandler, Request, build_opener

from desktop_candidates import sha256
from release_advertise import DOWNLOAD, advertisement_resume_available
from release_gateway import GatewayError, _live_handoff, _load, _save
from release_publication import PublicationError, gate_direct_publication, tagged_release_identity, validate_topology
from run_clean import sensitive_name

TAP = "atomicobject/homebrew-tap"
FORMULA = "rhizome.rb"  # Matches the existing brews.directory default.
CONTENTS = f"https://api.github.com/repos/{TAP}/contents/{FORMULA}"
ARCHIVES = ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64")


class HomebrewError(RuntimeError):
    """The release is public but its tap update needs inspection or retry."""


def homebrew_environment(source):
    token = source.get("BREW_GITHUB_TOKEN", "")
    if not token.strip():
        raise HomebrewError("BREW_GITHUB_TOKEN is required for the tap update")
    return {**{k: v for k, v in source.items() if not sensitive_name(k)}, "BREW_GITHUB_TOKEN": token}


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


class Tap:
    def __init__(self, token):
        self.token = token
        self.opener = build_opener(NoRedirect())

    def request(self, *, data=None):
        url = CONTENTS + ("?ref=main" if data is None else "")
        request = Request(url, data=None if data is None else json.dumps(data).encode(),
                          method="GET" if data is None else "PUT", headers={
                              "Authorization": "Bearer " + self.token, "Accept": "application/vnd.github+json",
                              "Content-Type": "application/json", "X-GitHub-Api-Version": "2022-11-28",
                              "User-Agent": "rhizome-release-coordinator"})
        try:
            with self.opener.open(request, timeout=60) as response:
                value = response.read(1024 * 1024 + 1)
            if len(value) > 1024 * 1024:
                raise HomebrewError("tap response exceeded its limit")
            return json.loads(value)
        except HTTPError as error:
            if data is None and error.code == 404:
                return None
            raise HomebrewError("tap request failed; retain the release receipt and retry") from None
        except (URLError, OSError, ValueError) as error:
            raise HomebrewError("tap request failed or timed out; retain the release receipt and retry") from error


def _current(tap):
    result = tap.request()
    if result is None:
        return None, b""
    if (result.get("type") != "file" or result.get("path") != FORMULA or result.get("encoding") != "base64"
            or not re.fullmatch(r"[0-9a-f]{40}", result.get("sha", ""))):
        raise HomebrewError("unexpected tap contents identity")
    try:
        content = base64.b64decode("".join(result["content"].split()), validate=True)
    except (KeyError, ValueError) as error:
        raise HomebrewError("invalid tap contents") from error
    return result["sha"], content


def _version(text):
    matches = re.findall(r'^\s*version "([0-9]+\.[0-9]+\.[0-9]+)"\s*$', text, re.MULTILINE)
    if len(matches) != 1:
        raise HomebrewError("formula must contain one stable release version")
    return tuple(int(part) for part in matches[0].split("."))


def validate_formula(path, expected):
    if path.is_symlink() or not path.is_file() or sha256(path) != expected["homebrewFormulaSha256"]:
        raise HomebrewError("retained GoReleaser formula is missing or changed")
    content = path.read_bytes()
    text = content.decode()
    if _version(text) != tuple(int(part) for part in expected["version"].split(".")):
        raise HomebrewError("formula version differs from the advertised release")
    pairs = re.findall(r'^\s*url "([^"\n]+)"\s*\n\s*sha256 "([0-9a-f]{64})"\s*$', text, re.MULTILINE)
    wanted = {f"{DOWNLOAD}/download/v{expected['version']}/rhizome-{target}.tar.gz":
              expected["assets"][f"rhizome-{target}.tar.gz"]["sha256"] for target in ARCHIVES}
    if (len(pairs) != len(wanted) or dict(pairs) != wanted
            or len(re.findall(r"^\s*url\b", text, re.MULTILINE)) != len(wanted)):
        raise HomebrewError("formula URLs or archive hashes differ from the verified release")
    return content


def publish_tap(advertisement_receipt, *, expected_commit, expected_version, tap):
    path = Path(advertisement_receipt)
    if not advertisement_resume_available(path, expected_commit=expected_commit, expected_version=expected_version):
        raise HomebrewError("tap update requires an exact verified release receipt")
    advertisement = json.loads(path.read_text())
    if advertisement["phase"] != "complete":
        raise HomebrewError("tap update requires completed release advertisement")
    expected = advertisement["expected"]
    if "-" in expected["version"]:
        return None  # Stable formula remains on the last stable release.
    content = validate_formula(path.with_suffix(".rhizome.rb"), expected)
    receipt_path = path.with_suffix(".homebrew.json")
    identity = {"schemaVersion": 1, "sourceCommit": expected_commit, "version": expected["version"],
                "formulaSha256": expected["homebrewFormulaSha256"], "repository": TAP, "path": FORMULA}
    receipt = json.loads(receipt_path.read_text()) if receipt_path.exists() else None
    if receipt is not None and (any(receipt.get(k) != v for k, v in identity.items())
                                or receipt.get("phase") not in {"publishing", "complete"}
                                or (receipt.get("previousSha") is not None
                                    and not re.fullmatch(r"[0-9a-f]{40}", receipt["previousSha"]))):
        raise HomebrewError("tap receipt differs from the exact release")
    current_sha, current = _current(tap)
    if current == content:
        receipt = {**identity, "previousSha": receipt["previousSha"] if receipt else current_sha, "phase": "complete"}
        _save(receipt_path, receipt)
        return receipt
    if receipt is not None and (receipt["phase"] == "complete" or current_sha != receipt["previousSha"]):
        raise HomebrewError("tap changed after this release attempt; refuse to overwrite it")
    if current and _version(current.decode()) >= _version(content.decode()):
        raise HomebrewError("tap already contains this version or a newer release; inspect before overwriting")
    receipt = {**identity, "previousSha": current_sha, "phase": "publishing"}
    _save(receipt_path, receipt)
    data = {"message": f"Brew formula update for rhizome version v{expected['version']}",
            "content": base64.b64encode(content).decode(), "branch": "main"}
    if current_sha is not None:
        data["sha"] = current_sha
    tap.request(data=data)
    if _current(tap)[1] != content:
        raise HomebrewError("tap update could not be verified; retry the retained receipt")
    receipt["phase"] = "complete"
    _save(receipt_path, receipt)
    return receipt


def verify_live_handoff(root, receipt_path, commit, version):
    if tagged_release_identity(root, allow_generated=True) != (version.removeprefix("v"), commit):
        raise HomebrewError("tap handoff differs from the canonical tagged release")
    validate_topology(root, commit)
    receipt = _load(receipt_path)
    if (receipt is None or receipt["source"].get("release_commit") != commit
            or receipt["source"].get("source_commit") != commit or receipt["attempt"]["state"] != "success"
            or not _live_handoff(receipt["attempt"])):
        raise HomebrewError("tap worker requires a live verified gateway handoff from its parent")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path.cwd())
    parser.add_argument("--advertisement-receipt", type=Path, required=True)
    parser.add_argument("--gateway-receipt", type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--apply", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    try:
        if not args.apply:
            if tagged_release_identity(args.repo_root, allow_generated=True) != (args.version.removeprefix("v"), args.commit):
                raise HomebrewError("tap publication source differs from the canonical tagged release")
            gate = gate_direct_publication(args.repo_root, receipt_path=args.gateway_receipt, allow_generated=True)
            command = (sys.executable, str(Path(__file__).resolve()), "--apply", "--repo-root", str(args.repo_root),
                       "--advertisement-receipt", str(args.advertisement_receipt), "--gateway-receipt", gate["receiptPath"],
                       "--version", args.version, "--commit", args.commit)
            return subprocess.run(command, env=homebrew_environment(os.environ)).returncode
        if args.gateway_receipt is None:
            raise HomebrewError("tap worker has no gateway receipt")
        verify_live_handoff(args.repo_root, args.gateway_receipt, args.commit, args.version)
        env = homebrew_environment(os.environ)
        publish_tap(args.advertisement_receipt, expected_commit=args.commit, expected_version=args.version,
                    tap=Tap(env["BREW_GITHUB_TOKEN"]))
    except (HomebrewError, GatewayError, PublicationError, OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f"homebrew: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
