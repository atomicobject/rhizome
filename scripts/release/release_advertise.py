#!/usr/bin/env python3
"""Verify the CI publisher's populated draft before advertising its desktop feed.

ci_release_publish alone uploads assets. This verifier holds only the release GitHub
token and verifies updater signatures with the committed public key. A receipt is
written only after authenticated retrieval proves every uploaded byte. Unknown
uploads without that receipt still require the release orchestrator's inspection.
GitHub API: https://docs.github.com/en/rest/releases/releases#update-a-release
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

from desktop_candidates import CandidateError, REQUIRED_TARGETS, TARGETS, identity, sha256
from run_clean import sensitive_name
from verify_desktop_signing import DesktopSigningError, release_pubkey, verify_archive

REPOSITORY = "atomicobject/rhizome"
API = f"https://api.github.com/repos/{REPOSITORY}"
DOWNLOAD = f"https://github.com/{REPOSITORY}/releases"
PHASES = {"verified-draft", "publishing", "public", "promoting", "recovering", "recovered", "complete"}


class AdvertisementError(RuntimeError):
    """Advertisement failed; its receipt records any uncertain remote mutation."""


class ApiError(AdvertisementError):
    def __init__(self, status):
        self.status = status
        super().__init__(f"GitHub release request failed (HTTP {status}); inspect the retained receipt")


def advertisement_environment(source):
    token = source.get("RZM_RELEASE_GITHUB_TOKEN") or source.get("GITHUB_TOKEN", "")
    if not token.strip():
        raise AdvertisementError("release GitHub token is required for advertisement")
    return {**{k: v for k, v in source.items() if not sensitive_name(k)}, "GITHUB_TOKEN": token}


class SafeRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urlsplit(newurl).scheme != "https":
            raise AdvertisementError("release download redirected outside HTTPS")
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected is not None:
            redirected.remove_header("Authorization")
        return redirected


class GitHub:
    def __init__(self, token):
        self.token = token
        self.opener = build_opener(SafeRedirect())

    def _open(self, url, *, method="GET", data=None, authenticated=False, binary=False):
        headers = {"Accept": "application/octet-stream" if binary else "application/vnd.github+json",
                   "User-Agent": "rhizome-release-coordinator", "X-GitHub-Api-Version": "2022-11-28"}
        if authenticated:
            if not url.startswith(API + "/"):
                raise AdvertisementError("refusing authenticated download outside the release repository")
            headers["Authorization"] = "Bearer " + self.token
        payload = None if data is None else json.dumps(data).encode()
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            return self.opener.open(Request(url, data=payload, headers=headers, method=method), timeout=60)
        except HTTPError as error:
            raise ApiError(error.code) from None
        except (URLError, OSError) as error:
            raise AdvertisementError("release request failed or timed out; inspect the retained receipt") from error

    def api(self, endpoint, *, method="GET", data=None):
        with self._open(API + "/" + endpoint, method=method, data=data, authenticated=True) as response:
            value = response.read(8 * 1024 * 1024 + 1)
        if len(value) > 8 * 1024 * 1024:
            raise AdvertisementError("GitHub release response exceeded its limit")
        return json.loads(value)

    def latest(self):
        try:
            return self.api("releases/latest")
        except ApiError as error:
            if error.status != 404:
                raise
            return None

    def download(self, url, path, *, size=None, authenticated=False, limit=1024 * 1024):
        limit = size if size is not None else limit
        deadline = time.monotonic() + 600
        with self._open(url, authenticated=authenticated, binary=True) as response, path.open("wb") as output:
            remaining = limit + 1
            while remaining:
                if time.monotonic() > deadline:
                    raise AdvertisementError("release download exceeded its deadline")
                block = response.read(min(1024 * 1024, remaining))
                if not block:
                    break
                output.write(block)
                remaining -= len(block)
        if path.stat().st_size > limit or (size is not None and path.stat().st_size != size):
            raise AdvertisementError("release download has an unexpected size")


def _asset_name(name):
    if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", name):
        raise AdvertisementError("release asset has an unsafe filename")
    return name


def _read(path):
    return json.loads(path.read_text())


def _write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, prefix=".advertisement-", delete=False) as output:
        temporary = Path(output.name)
        json.dump(value, output, indent=2, sort_keys=True)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    temporary.replace(path)


def expected_bundle(root, directory, *, version, commit, recipe_hashes):
    """Bind the verified immutable CI bundle and both recipes to exact source."""
    version, commit = identity(version, commit)
    if set(recipe_hashes) != {"homebrewFormulaSha256", "homebrewCaskSha256"}:
        raise AdvertisementError("both retained Homebrew recipe hashes are required")
    directory = Path(directory)
    manifest = _read(directory / "release-manifest.json")
    if manifest.get("candidate_only") is not False:
        raise AdvertisementError("candidate-only bundles cannot be published")
    if (manifest.get("schema_version") != 1 or manifest.get("source_commit") != commit
            or manifest.get("version") != "v" + version or not isinstance(manifest.get("files"), dict)):
        raise AdvertisementError("CI bundle differs from the accepted release source")
    assets = {}
    for name, checksum in manifest["files"].items():
        _asset_name(name)
        path = directory / name
        if path.is_symlink() or not path.is_file() or sha256(path) != checksum:
            raise AdvertisementError("immutable release bundle asset changed")
        assets[name] = {"sha256": checksum, "size": path.stat().st_size}
    for name in ("release-manifest.json", "checksums.txt"):
        path = directory / name
        if path.is_symlink() or not path.is_file():
            raise AdvertisementError("release bundle metadata is missing")
        assets[name] = {"sha256": sha256(path), "size": path.stat().st_size}
    if {path.name for path in directory.iterdir()} != set(assets):
        raise AdvertisementError("release bundle contains an unexpected file")
    checksums = "".join(f"{asset['sha256']}  {name}\n" for name, asset in sorted(assets.items()) if name != "checksums.txt")
    if (directory / "checksums.txt").read_text() != checksums:
        raise AdvertisementError("release checksums do not cover the complete bundle")
    assembly = _read(directory / "desktop-assembly.json")
    key_hash = hashlib.sha256(release_pubkey(root).encode()).hexdigest()
    if (assembly.get("schemaVersion") != 1 or assembly.get("production") is not True or assembly.get("candidateOnly") is not False
            or assembly.get("version") != version or assembly.get("sourceCommit") != commit
            or assembly.get("publicKeySha256") != key_hash or set(assembly.get("packages", {})) != REQUIRED_TARGETS):
        raise AdvertisementError("desktop assembly is incomplete or differs from canonical source/key")
    for target, package in assembly["packages"].items():
        if package != {"file": TARGETS[target]["asset"], **assets.get(TARGETS[target]["asset"], {})}:
            raise AdvertisementError("assembled package differs from the immutable CI bundle")
        if package["file"] + ".sig" not in assets:
            raise AdvertisementError("updater signature is missing from release checksums")
    expected = {"repository": REPOSITORY, "version": version, "sourceCommit": commit,
                "publicKeySha256": key_hash, **recipe_hashes, "assets": assets}
    _validate_expected(expected)
    return expected


def _validate_expected(expected):
    identity(expected["version"], expected["sourceCommit"])
    if expected.get("repository") != REPOSITORY or not re.fullmatch(r"[0-9a-f]{64}", expected.get("publicKeySha256", "")):
        raise AdvertisementError("invalid advertisement identity")
    if any(not re.fullmatch(r"[0-9a-f]{64}", expected.get(field, ""))
           for field in ("homebrewFormulaSha256", "homebrewCaskSha256")):
        raise AdvertisementError("advertisement has no retained Homebrew recipe identity")
    assets = expected["assets"]
    required = {"latest.json", "checksums.txt", "install-rzm.sh", "install-rzm.sh.sha256",
                "desktop-assembly.json", "Rhizome-macos-arm64.dmg", "release-manifest.json"}
    required.update(f"rhizome-{target}.tar.gz" for target in
                    ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64"))
    required.update(value["asset"] + suffix for value in TARGETS.values() for suffix in ("", ".sig"))
    if not isinstance(assets, dict) or not required.issubset(assets):
        raise AdvertisementError("advertisement is missing required release assets")
    for name, asset in assets.items():
        _asset_name(name)
        if (not isinstance(asset, dict) or not re.fullmatch(r"[0-9a-f]{64}", asset.get("sha256", ""))
                or type(asset.get("size")) is not int or asset["size"] <= 0):
            raise AdvertisementError("invalid release asset expectation")


def advertisement_resume_available(path, *, expected_commit, expected_version):
    """Local proof for the caller's uncertain-tag guard, never permission to reupload."""
    try:
        receipt = _read(Path(path))
        _validate_expected(receipt["expected"])
        previous = receipt["previousLatest"]
        if previous is not None:
            if (not isinstance(previous, dict) or type(previous.get("id")) is not int or previous["id"] <= 0
                    or not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", previous.get("tag_name", ""))):
                return False
            if "feedSha256" in previous and (not re.fullmatch(r"[0-9a-f]{64}", previous["feedSha256"])
                    or "v" + previous.get("version", "") != previous["tag_name"]
                    or not isinstance(previous.get("targets"), list) or not previous["targets"]
                    or not set(previous["targets"]).issubset(REQUIRED_TARGETS)):
                return False
        return (receipt.get("schemaVersion") == 1 and receipt.get("phase") in PHASES
                and type(receipt.get("releaseId")) is int and receipt["releaseId"] > 0
                and (previous is None or previous["id"] != receipt["releaseId"])
                and receipt["expected"]["sourceCommit"] == expected_commit
                and receipt["expected"]["version"] == expected_version.removeprefix("v")
                and "previousLatest" in receipt)
    except (OSError, ValueError, KeyError, TypeError, AdvertisementError, CandidateError):
        return False


def _release_id(release):
    return release["id"] if release is not None else None


def _release(api, expected, release_id=None):
    if release_id is None:
        matches = []
        for page in range(1, 101):
            releases = api.api(f"releases?per_page=100&page={page}")
            matches.extend(item for item in releases if item.get("tag_name") == "v" + expected["version"])
            if len(releases) < 100:
                break
        else:
            raise AdvertisementError("release listing exceeded its bounded search")
        if len(matches) != 1:
            raise AdvertisementError("expected exactly one GoReleaser draft for this tag")
        release = matches[0]
    else:
        release = api.api(f"releases/{release_id}")
    if (type(release.get("id")) is not int or release["id"] <= 0
            or release.get("tag_name") != "v" + expected["version"]
            or release.get("target_commitish") != expected["sourceCommit"]
            or release.get("prerelease") is not ("-" in expected["version"])
            or type(release.get("draft")) is not bool):
        raise AdvertisementError("GitHub release identity differs from the canonical stable release")
    if ("releaseNotesSha256" in expected and (not isinstance(release.get("body"), str)
            or hashlib.sha256(release["body"].encode()).hexdigest() != expected["releaseNotesSha256"])):
        raise AdvertisementError("GitHub release notes differ from the committed narrative")
    return release


def _verify_tag(api, expected):
    result = api.api("git/ref/tags/" + quote("v" + expected["version"], safe=""))["object"]
    for _ in range(8):
        if result.get("type") == "commit" and result.get("sha") == expected["sourceCommit"]:
            return
        if result.get("type") != "tag" or not re.fullmatch(r"[0-9a-f]{40}", result.get("sha", "")):
            break
        result = api.api("git/tags/" + result["sha"])["object"]
    raise AdvertisementError("remote release tag differs from the canonical commit")


def _feed(api, path, *, version, directory, root, env, verifier, required=REQUIRED_TARGETS):
    feed = _read(path)
    platforms = feed.get("platforms", {})
    if feed.get("version") != version or set(platforms) != set(required):
        raise AdvertisementError("updater feed is missing a supported platform or has the wrong version")
    for target, entry in platforms.items():
        name = TARGETS[target]["asset"]
        url = f"{DOWNLOAD}/download/v{version}/{name}"
        if entry.get("url") != url or not isinstance(entry.get("signature"), str) or not entry["signature"].strip():
            raise AdvertisementError("updater feed has an unexpected asset URL or signature")
        archive, signature = directory / name, directory / (name + ".sig")
        if not archive.exists():
            api.download(url, archive, limit=4 * 1024 * 1024 * 1024)
        signature.write_text(entry["signature"])
        verifier(root, archive, signature, environ=env)
    return feed


def _verify_assets(api, release, expected, directory, *, authenticated):
    if authenticated:
        items = api.api(f"releases/{release['id']}/assets?per_page=100")
        remote = {item["name"]: item for item in items}
        if len(remote) != len(items) or set(remote) != set(expected["assets"]):
            raise AdvertisementError("draft release asset inventory is incomplete or unexpected")
    for name, asset in expected["assets"].items():
        path = directory / name
        url = f"{DOWNLOAD}/download/v{expected['version']}/{name}"
        if authenticated:
            item = remote[name]
            if (item.get("state") != "uploaded" or item.get("size") != asset["size"]
                    or type(item.get("id")) is not int or item["id"] <= 0):
                raise AdvertisementError("draft asset is incomplete")
            url = f"{API}/releases/assets/{item['id']}"
        api.download(url, path, size=asset["size"], authenticated=authenticated)
        if sha256(path) != asset["sha256"]:
            raise AdvertisementError("release asset checksum mismatch")


def advertise(root, receipt_path, *, expected=None, api, env, verifier=verify_archive, before_change=lambda: None):
    """Resume only a verified upload. Persist intent before each public mutation."""
    root, receipt_path = Path(root), Path(receipt_path)
    if receipt_path.exists():
        receipt = _read(receipt_path)
        if not advertisement_resume_available(receipt_path, expected_commit=receipt["expected"]["sourceCommit"],
                                               expected_version=receipt["expected"]["version"]):
            raise AdvertisementError("invalid advertisement receipt; manual inspection required")
        if expected is not None and expected != receipt["expected"]:
            raise AdvertisementError("local assets differ from the saved advertisement")
        expected = receipt["expected"]
    else:
        if expected is None:
            raise AdvertisementError("advertisement resume requires a verified-upload receipt")
        _validate_expected(expected)
        receipt = None
    if expected["publicKeySha256"] != hashlib.sha256(release_pubkey(root).encode()).hexdigest():
        raise AdvertisementError("committed updater key differs from this advertisement")
    release = _release(api, expected, receipt["releaseId"] if receipt else None)
    if receipt is None and not release["draft"]:
        raise AdvertisementError("public release without a verified-upload receipt requires manual inspection")

    def save(phase):
        receipt["phase"] = phase
        _write(receipt_path, receipt)

    def latest_is(previous):
        if _release_id(api.latest()) != _release_id(previous):
            raise AdvertisementError("latest changed concurrently; manual inspection required")

    def smoke(snapshot=None):
        latest_is(snapshot if snapshot is not None else release)
        if snapshot is not None and "feedSha256" not in snapshot:
            return
        with tempfile.TemporaryDirectory(prefix="rzm-feed-") as temporary:
            directory = Path(temporary)
            path = directory / "latest.json"
            api.download(f"{DOWNLOAD}/latest/download/latest.json", path)
            digest = snapshot["feedSha256"] if snapshot is not None else expected["assets"]["latest.json"]["sha256"]
            if sha256(path) != digest:
                raise AdvertisementError("latest feed does not resolve to the verified release")
            _feed(api, path, version=snapshot["version"] if snapshot else expected["version"], directory=directory,
                  root=root, env=env, verifier=verifier, required=snapshot["targets"] if snapshot else REQUIRED_TARGETS)

    def recover():
        save("recovering")
        current, previous = api.latest(), receipt["previousLatest"]
        if _release_id(current) not in {_release_id(release), _release_id(previous)}:
            raise AdvertisementError("latest changed concurrently during recovery; manual inspection required")
        api.api(f"releases/{release['id']}", method="PATCH", data={"draft": True, "make_latest": "false"})
        if previous is not None:
            api.api(f"releases/{previous['id']}", method="PATCH", data={"make_latest": "true"})
            smoke(previous)
        else:
            latest_is(None)
        save("recovered")

    with tempfile.TemporaryDirectory(prefix="rzm-advertise-") as temporary:
        directory = Path(temporary)
        try:
            _verify_assets(api, release, expected, directory, authenticated=True)
            _feed(api, directory / "latest.json", version=expected["version"], directory=directory,
                  root=root, env=env, verifier=verifier)
        except (AdvertisementError, DesktopSigningError, OSError, ValueError, KeyError):
            if receipt is not None and _release_id(api.latest()) == release["id"]:
                recover()
            raise
        if receipt is None:
            previous = api.latest()
            if previous is not None:
                previous = {"id": previous["id"], "tag_name": previous["tag_name"]}
                # Existing pre-desktop releases have no feed to preserve.
                try:
                    path = directory / "previous-feed.json"
                    api.download(f"{DOWNLOAD}/latest/download/latest.json", path)
                except ApiError as error:
                    if error.status != 404:
                        raise
                else:
                    feed = _read(path)
                    targets = set(feed.get("platforms", {}))
                    if not targets or not targets.issubset(REQUIRED_TARGETS) or "v" + feed.get("version", "") != previous["tag_name"]:
                        raise AdvertisementError("previous latest feed has an invalid identity")
                    previous.update(feedSha256=sha256(path), version=feed["version"], targets=sorted(targets))
            receipt = {"schemaVersion": 1, "expected": expected, "releaseId": release["id"], "previousLatest": previous}
            save("verified-draft")
        if receipt["phase"] == "complete":
            if release["draft"]:
                raise AdvertisementError("completed release returned to draft; manual inspection required")
            try:
                _verify_tag(api, expected)
                _verify_assets(api, release, expected, directory, authenticated=False)
                if _release_id(api.latest()) == release["id"]:
                    smoke()
            except (AdvertisementError, DesktopSigningError, OSError, ValueError, KeyError):
                if _release_id(api.latest()) == release["id"]:
                    recover()
                raise
            return receipt
        if receipt["phase"] in {"recovering", "recovered"}:
            recover()
            release = _release(api, expected, receipt["releaseId"])
        try:
            if _release_id(api.latest()) != release["id"]:
                latest_is(receipt["previousLatest"])
                if release["draft"]:
                    before_change()
                    save("publishing")
                    api.api(f"releases/{release['id']}", method="PATCH", data={"draft": False, "make_latest": "false"})
                save("public")
                latest_is(receipt["previousLatest"])
                _verify_tag(api, expected)
                _verify_assets(api, release, expected, directory, authenticated=False)
                _feed(api, directory / "latest.json", version=expected["version"], directory=directory,
                      root=root, env=env, verifier=verifier)
                latest_is(receipt["previousLatest"])
                if release["prerelease"]:
                    save("complete")
                    return receipt
                save("promoting")
                before_change()
                api.api(f"releases/{release['id']}", method="PATCH", data={"make_latest": "true"})
            else:
                _verify_tag(api, expected)
            smoke()
            save("complete")
        except (AdvertisementError, DesktopSigningError, OSError, ValueError, KeyError) as error:
            if receipt["phase"] == "promoting" or _release_id(api.latest()) == release["id"]:
                try:
                    recover()
                except (AdvertisementError, DesktopSigningError, OSError, ValueError, KeyError) as recovery:
                    raise AdvertisementError("advertisement failed and recovery is incomplete; inspect the retained receipt") from recovery
            raise AdvertisementError("advertisement failed; previous latest retained, inspect or resume its receipt") from error
    return receipt


def resume_advertisement(root, receipt_path, *, expected_commit, expected_version, environ=None, api=None,
                         verifier=verify_archive):
    if not advertisement_resume_available(receipt_path, expected_commit=expected_commit, expected_version=expected_version):
        raise AdvertisementError("no exact verified-upload receipt; manual inspection required")
    env = advertisement_environment(os.environ if environ is None else environ)
    return advertise(root, receipt_path, api=api or GitHub(env["GITHUB_TOKEN"]), env=env, verifier=verifier)


if __name__ == "__main__":
    raise SystemExit("Public publication runs only through the approved ci_release_publish workflow")
