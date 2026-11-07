#!/usr/bin/env python3
"""Render and publish the two owned tap recipes after verified CI advertisement."""
from __future__ import annotations

import base64
import hashlib
import json
from pathlib import Path
import re
import tempfile

from desktop_candidates import sha256
from release_advertise import DOWNLOAD, advertisement_resume_available
from release_gateway import _save

TAP = "atomicobject/homebrew-tap"
RECIPES = {"rhizome.rb": (".rhizome.rb", "homebrewFormulaSha256"),
           "Casks/rhizome-desktop.rb": (".rhizome-desktop.rb", "homebrewCaskSha256")}
ARCHIVES = ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64")


class HomebrewError(RuntimeError):
    """The verified public release's tap update needs inspection or retry."""


def recipe_contents(version, assets):
    version = version.removeprefix("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise HomebrewError("Homebrew recipes require an exact stable release version")
    def checksum(name):
        value = assets[name]["sha256"]
        if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{64}", value):
            raise HomebrewError("invalid verified Homebrew asset hash")
        return value
    # Preserve the CI recipes introduced with the desktop cask.
    lines = ['# frozen_string_literal: true', 'class Rhizome < Formula',
             '  desc "Repository knowledge graph for agents"',
             '  homepage "https://github.com/atomicobject/rhizome"', f'  version "{version}"', '']
    for os_name, targets in (("macos", ("darwin-arm64", "darwin-amd64")), ("linux", ("linux-arm64", "linux-amd64"))):
        lines += [f"  on_{os_name} do", "    if Hardware::CPU.arm?"]
        for index, target in enumerate(targets):
            name = f"rhizome-{target}.tar.gz"
            if index:
                lines.append("    else")
            lines += [f'      url "{DOWNLOAD}/download/v{version}/{name}"', f'      sha256 "{checksum(name)}"']
        lines += ["    end", "  end", ""]
    lines += ["  def install", '    bin.install "rzm"', "  end", "", "  test do", '    system "#{bin}/rzm", "--version"', "  end", "end", ""]
    cask = "\n".join(('cask "rhizome-desktop" do', f'  version "{version}"',
        f'  sha256 "{checksum("Rhizome-macos-arm64.dmg")}"', '',
        f'  url "{DOWNLOAD}/download/v#{{version}}/Rhizome-macos-arm64.dmg"',
        '  name "Rhizome"', '  desc "Open repositories in Rhizome"',
        '  homepage "https://github.com/atomicobject/rhizome"', '', '  auto_updates true',
        '  depends_on arch: :arm64', '  depends_on macos: :monterey', '', '  app "Rhizome.app"',
        '  binary "#{appdir}/Rhizome.app/Contents/MacOS/rzm"', 'end', ''))
    return {"rhizome.rb": "\n".join(lines).encode(), "Casks/rhizome-desktop.rb": cask.encode()}


def retain_recipes(path, version, assets):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    hashes = {}
    for name, content in recipe_contents(version, assets).items():
        suffix, field = RECIPES[name]
        hashes[field] = hashlib.sha256(content).hexdigest()
        retained = path.with_suffix(suffix)
        if retained.exists() and (retained.is_symlink() or retained.read_bytes() != content):
            raise HomebrewError("retained Homebrew recipe differs from the immutable release")
        with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as output:
            temporary = Path(output.name)
            output.write(content)
        temporary.replace(retained)
    return hashes


def _current(tap, path):
    result = tap.request(path)
    if result is None:
        return None, b""
    if (result.get("type") != "file" or result.get("path") != path or result.get("encoding") != "base64"
            or not re.fullmatch(r"[0-9a-f]{40}", result.get("sha", ""))):
        raise HomebrewError("unexpected tap contents identity")
    try:
        content = base64.b64decode("".join(result["content"].split()), validate=True)
    except (KeyError, ValueError) as error:
        raise HomebrewError("invalid tap contents") from error
    return result["sha"], content


def _version(content):
    matches = re.findall(r'^\s*version "([0-9]+\.[0-9]+\.[0-9]+)"\s*$', content.decode(), re.MULTILINE)
    if len(matches) != 1:
        raise HomebrewError("tap recipe has no unique stable version; inspect before overwriting")
    return tuple(int(part) for part in matches[0].split("."))


def publish_tap(advertisement_receipt, *, expected_commit, expected_version, tap, before_update=None):
    path = Path(advertisement_receipt)
    if not advertisement_resume_available(path, expected_commit=expected_commit, expected_version=expected_version):
        raise HomebrewError("tap update requires an exact verified release receipt")
    advertisement = json.loads(path.read_text())
    if advertisement["phase"] != "complete":
        raise HomebrewError("tap update requires completed release advertisement")
    expected = advertisement["expected"]
    desired = recipe_contents(expected["version"], expected["assets"])
    for name, content in desired.items():
        suffix, field = RECIPES[name]
        retained = path.with_suffix(suffix)
        if (retained.is_symlink() or not retained.is_file() or sha256(retained) != expected[field]
                or retained.read_bytes() != content):
            raise HomebrewError("retained Homebrew recipe differs from verified source/version/assets")
    receipt_path = path.with_suffix(".homebrew.json")
    identity = {"schemaVersion": 2, "sourceCommit": expected_commit, "version": expected["version"],
                "repository": TAP, "recipeHashes": {name: expected[field] for name, (_, field) in RECIPES.items()}}
    receipt = json.loads(receipt_path.read_text()) if receipt_path.exists() else {**identity, "recipes": {}}
    if (any(receipt.get(k) != v for k, v in identity.items()) or not isinstance(receipt.get("recipes"), dict)
            or not set(receipt["recipes"]).issubset(RECIPES)):
        raise HomebrewError("tap receipt differs from the exact release")
    pending = []
    for name, content in desired.items():
        current_sha, current = _current(tap, name)
        saved = receipt["recipes"].get(name)
        if saved is not None and (saved.get("phase") not in {"publishing", "complete"}
                or (saved.get("previousSha") is not None and not re.fullmatch(r"[0-9a-f]{40}", saved["previousSha"]))):
            raise HomebrewError("invalid tap recipe receipt")
        if current == content:
            receipt["recipes"][name] = {"phase": "complete", "previousSha": saved["previousSha"] if saved else current_sha}
            continue
        if saved is not None and saved["phase"] == "publishing" and current_sha != saved["previousSha"]:
            raise HomebrewError("tap changed after this release attempt; refuse to overwrite it")
        if current and _version(current) >= _version(content):
            raise HomebrewError("tap already contains this version or a newer release; inspect before overwriting")
        pending.append((name, content, current_sha))
    if pending and before_update:
        before_update()
    for name, content, current_sha in pending:
        receipt["recipes"][name] = {"previousSha": current_sha, "phase": "publishing"}
        _save(receipt_path, receipt)
        data = {"message": f"Update Rhizome to v{expected['version']}", "content": base64.b64encode(content).decode(), "branch": "main"}
        if current_sha is not None:
            data["sha"] = current_sha
        tap.request(name, data=data)
        if _current(tap, name)[1] != content:
            raise HomebrewError("tap update could not be verified; retry the retained receipt")
        receipt["recipes"][name]["phase"] = "complete"
        _save(receipt_path, receipt)
    _save(receipt_path, receipt)
    return receipt


if __name__ == "__main__":
    raise SystemExit("Public publication runs only through the approved ci_release_publish workflow")
