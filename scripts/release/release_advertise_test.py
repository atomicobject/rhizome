#!/usr/bin/env python3
"""Synthetic release/API/HTTP evidence. No GitHub calls or signing credentials."""
from __future__ import annotations

import copy
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
from pathlib import Path
import ssl
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import patch
from urllib.request import HTTPSHandler, Request, build_opener

from desktop_candidates import TARGETS
from release_advertise import (API, DOWNLOAD, AdvertisementError, ApiError, GitHub, SafeRedirect,
                               advertisement_environment, advertisement_resume_available, advertise,
                               expected_bundle, resume_advertisement)
from verify_desktop_signing import DesktopSigningError

COMMIT = "a" * 40
VERSION = "2.0.0"
KEY = "synthetic-public-key"


def digest(value):
    return hashlib.sha256(value).hexdigest()


def feed(version, targets=TARGETS):
    return json.dumps({"version": version, "platforms": {
        target: {"url": f"{DOWNLOAD}/download/v{version}/{TARGETS[target]['asset']}",
                 "signature": "synthetic-signature"} for target in targets}}).encode()


class ReleaseServer:
    """Records public visibility and simulates interrupted GitHub PATCH requests."""
    def __init__(self, files):
        self.files = files
        self.release = {"id": 2, "tag_name": "v" + VERSION, "target_commitish": COMMIT,
                        "draft": True, "prerelease": False}
        self.previous = {"id": 1, "tag_name": "v1.0.0", "draft": False, "prerelease": False}
        self.latest_id = 1
        self.assets = [{"id": index, "name": name, "size": len(value), "state": "uploaded"}
                       for index, (name, value) in enumerate(files.items(), 10)]
        self.patches, self.downloads = [], []
        self.missing_asset = False
        self.wrong_hash = False
        self.anonymous_failure = False
        self.bad_latest = False
        self.interrupt_publish = False
        self.crash_promote = False
        self.recovery_failure = False
        self.concurrent_latest = False

    def latest(self):
        if self.latest_id == 2:
            return copy.deepcopy(self.release)
        return copy.deepcopy(self.previous) if self.latest_id == 1 else None

    def api(self, endpoint, *, method="GET", data=None):
        if method == "PATCH":
            self.patches.append((endpoint, data.copy()))
            if endpoint == "releases/2":
                if data.get("draft") is True:
                    if self.recovery_failure:
                        raise ApiError(503)
                    self.release["draft"] = True
                    if self.latest_id == 2:
                        self.latest_id = 1
                if data.get("draft") is False:
                    self.release["draft"] = False
                    if self.interrupt_publish:
                        self.interrupt_publish = False
                        raise ApiError(503)
                if data.get("make_latest") == "true":
                    self.latest_id = 2
                    if self.crash_promote:
                        self.crash_promote = False
                        raise KeyboardInterrupt("simulated terminated publisher")
                return copy.deepcopy(self.release)
            if endpoint == "releases/1":
                self.latest_id = 1
                return copy.deepcopy(self.previous)
        if endpoint.startswith("releases?"):
            return [copy.deepcopy(self.release), copy.deepcopy(self.previous)]
        if endpoint == "releases/2":
            return copy.deepcopy(self.release)
        if endpoint == "releases/2/assets?per_page=100":
            return copy.deepcopy(self.assets[:-1] if self.missing_asset else self.assets)
        if endpoint == "git/ref/tags/v" + VERSION:
            return {"object": {"type": "commit", "sha": COMMIT}}
        raise AssertionError(f"unexpected API request: {method} {endpoint}")

    def download(self, url, path, *, size=None, authenticated=False, limit=None):
        self.downloads.append((url, authenticated, self.latest_id))
        if authenticated:
            assert url.startswith(API + "/releases/assets/")
            identifier = int(url.rsplit("/", 1)[1])
            asset = next(item for item in self.assets if item["id"] == identifier)
            value = self.files[asset["name"]]
            if self.wrong_hash and asset["name"] == TARGETS["windows-x86_64"]["asset"]:
                value = b"x" * len(value)
        elif "/latest/download/" in url:
            if self.latest_id == 2:
                value = b"{}" if self.bad_latest else self.files["latest.json"]
            else:
                value = feed("1.0.0", ["darwin-aarch64"])
        elif "/download/v1.0.0/" in url:
            value = b"previous-package"
        else:
            assert url.startswith(f"{DOWNLOAD}/download/v{VERSION}/")
            if self.anonymous_failure:
                raise ApiError(503)
            if self.release["draft"]:
                raise ApiError(404)
            value = self.files[url.rsplit("/", 1)[1]]
            if self.concurrent_latest:
                self.previous = {"id": 3, "tag_name": "v3.0.0"}
                self.latest_id = 1  # latest() now returns the competing release's ID.
        if size is not None and len(value) != size:
            raise AdvertisementError("release download has an unexpected size")
        path.write_bytes(value)


class AdvertisementTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        config = self.root / "desktop/src-tauri/tauri.conf.json"
        config.parent.mkdir(parents=True)
        config.write_text(json.dumps({"plugins": {"updater": {"pubkey": KEY}}}))
        self.bundle = self.root / "bundle"
        self.bundle.mkdir()
        self.files = {"latest.json": feed(VERSION), "Rhizome-macos-arm64.dmg": b"dmg"}
        packages = {}
        for target, info in TARGETS.items():
            value = ("package:" + info["asset"]).encode()
            self.files[info["asset"]] = value
            self.files[info["asset"] + ".sig"] = b"synthetic-signature"
            packages[target] = {"file": info["asset"], "sha256": digest(value), "size": len(value)}
        assembly = {"schemaVersion": 1, "production": True, "candidateOnly": False, "version": VERSION, "sourceCommit": COMMIT,
                    "publicKeySha256": digest(KEY.encode()), "packages": packages}
        self.files["desktop-assembly.json"] = json.dumps(assembly).encode()
        for target in ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64"):
            self.files[f"rhizome-{target}.tar.gz"] = ("canonical-cli:" + target).encode()
        self.files.update({"install-rzm.sh": b"synthetic-installer", "install-rzm.sh.sha256": b"synthetic-installer-checksum"})
        manifest = {"schema_version": 1, "candidate_only": False, "source_commit": COMMIT, "version": "v" + VERSION,
                    "files": {name: digest(value) for name, value in self.files.items()}}
        self.files["release-manifest.json"] = json.dumps(manifest).encode()
        self.files["checksums.txt"] = "".join(f"{digest(value)}  {name}\n" for name, value in sorted(self.files.items())).encode()
        for name, value in self.files.items():
            (self.bundle / name).write_bytes(value)
        self.recipe_hashes = {"homebrewFormulaSha256": "c" * 64, "homebrewCaskSha256": "d" * 64}
        self.expected = expected_bundle(self.root, self.bundle, version=VERSION, commit=COMMIT, recipe_hashes=self.recipe_hashes)
        self.receipt = self.root / "advertisement-receipt.json"
        self.api = ReleaseServer(self.files)
        self.verified = []

    def verifier(self, root, archive, signature, *, environ):
        self.assertEqual(root, self.root)
        self.assertTrue(archive.read_bytes().startswith((b"package:", b"previous-package")))
        self.assertEqual(signature.read_text(), "synthetic-signature")
        self.verified.append(archive.name)

    def advertise(self, expected=True):
        return advertise(self.root, self.receipt, expected=self.expected if expected else None,
                         api=self.api, env={"PATH": "/bin"}, verifier=self.verifier)

    def test_complete_release_publishes_nonlatest_then_promotes_after_anonymous_verification(self):
        result = self.advertise()
        self.assertEqual(result["phase"], "complete")
        self.assertEqual(self.api.latest_id, 2)
        self.assertEqual(self.api.patches, [("releases/2", {"draft": False, "make_latest": "false"}),
                                           ("releases/2", {"make_latest": "true"})])
        versioned = [latest for url, authenticated, latest in self.api.downloads
                     if not authenticated and f"/download/v{VERSION}/" in url]
        self.assertIn(1, versioned)
        self.assertTrue(all(latest == 1 for latest in versioned[:len(self.files)]))
        self.assertTrue(all(info["asset"] in self.verified for info in TARGETS.values()))
        patches = self.api.patches.copy()
        self.advertise(expected=False)
        self.assertEqual(self.api.patches, patches)

    def test_missing_platform_or_wrong_authenticated_hash_never_publishes(self):
        for flag in ("missing_asset", "wrong_hash"):
            with self.subTest(flag=flag):
                setattr(self.api, flag, True)
                with self.assertRaises(AdvertisementError):
                    self.advertise()
                self.assertEqual(self.api.latest_id, 1)
                self.assertEqual(self.api.patches, [])
                self.assertFalse(self.receipt.exists())
                setattr(self.api, flag, False)

    def test_incomplete_feed_fails_before_receipt_or_publication(self):
        broken = feed(VERSION, ["darwin-aarch64"])
        self.api.files["latest.json"] = broken
        self.expected["assets"]["latest.json"] = {"sha256": digest(broken), "size": len(broken)}
        self.api.assets = [{**asset, "size": len(broken)} if asset["name"] == "latest.json" else asset
                           for asset in self.api.assets]
        with self.assertRaisesRegex(AdvertisementError, "missing a supported platform"):
            self.advertise()
        self.assertEqual(self.api.patches, [])
        self.assertFalse(self.receipt.exists())

    def test_bad_signature_blocks_advertisement(self):
        def reject(*args, **kwargs):
            raise DesktopSigningError("synthetic invalid signature")
        with self.assertRaises(DesktopSigningError):
            advertise(self.root, self.receipt, expected=self.expected, api=self.api, env={}, verifier=reject)
        self.assertEqual(self.api.patches, [])

    def test_anonymous_failure_keeps_previous_latest_and_can_resume_verified_upload(self):
        self.api.anonymous_failure = True
        with self.assertRaisesRegex(AdvertisementError, "previous latest retained"):
            self.advertise()
        self.assertFalse(self.api.release["draft"])
        self.assertEqual(self.api.latest_id, 1)
        self.assertTrue(advertisement_resume_available(self.receipt, expected_commit=COMMIT, expected_version=VERSION))
        self.api.anonymous_failure = False
        resume_advertisement(self.root, self.receipt, expected_commit=COMMIT, expected_version=VERSION,
                             environ={"GITHUB_TOKEN": "synthetic"}, api=self.api, verifier=self.verifier)
        self.assertEqual(self.api.patches.count(("releases/2", {"draft": False, "make_latest": "false"})), 1)
        self.assertEqual(self.api.latest_id, 2)

    def test_interrupted_publish_reconciles_remote_state_without_uploading_again(self):
        self.api.interrupt_publish = True
        with self.assertRaises(AdvertisementError):
            self.advertise()
        self.assertEqual(json.loads(self.receipt.read_text())["phase"], "publishing")
        self.assertFalse(self.api.release["draft"])
        self.advertise(expected=False)
        self.assertEqual(len(self.api.patches), 2)
        self.assertEqual(self.api.latest_id, 2)

    def test_interrupted_promotion_finishes_from_durable_intent(self):
        self.api.crash_promote = True
        with self.assertRaises(KeyboardInterrupt):
            self.advertise()
        self.assertEqual(json.loads(self.receipt.read_text())["phase"], "promoting")
        self.assertEqual(self.api.latest_id, 2)
        self.advertise(expected=False)
        self.assertEqual(len(self.api.patches), 2)
        self.assertEqual(json.loads(self.receipt.read_text())["phase"], "complete")

    def test_bad_promoted_feed_restores_old_latest_and_drafts_faulty_release(self):
        self.api.bad_latest = True
        with self.assertRaisesRegex(AdvertisementError, "previous latest retained"):
            self.advertise()
        self.assertTrue(self.api.release["draft"])
        self.assertEqual(self.api.latest_id, 1)
        self.assertEqual(json.loads(self.receipt.read_text())["phase"], "recovered")
        self.assertEqual(self.api.patches[-2:], [("releases/2", {"draft": True, "make_latest": "false"}),
                                               ("releases/1", {"make_latest": "true"})])
        self.assertTrue(any("/download/v1.0.0/" in url for url, authenticated, latest in self.api.downloads))
        self.api.bad_latest = False
        self.advertise(expected=False)
        self.assertEqual(self.api.latest_id, 2)

    def test_failed_recovery_is_explicit_and_retains_intent_for_resume(self):
        self.api.bad_latest = self.api.recovery_failure = True
        with self.assertRaisesRegex(AdvertisementError, "recovery is incomplete"):
            self.advertise()
        self.assertEqual(json.loads(self.receipt.read_text())["phase"], "recovering")
        self.api.bad_latest = self.api.recovery_failure = False
        self.advertise(expected=False)
        self.assertEqual(self.api.latest_id, 2)

    def test_completed_receipt_never_repromotes_over_a_later_release(self):
        self.advertise()
        self.api.previous = {"id": 3, "tag_name": "v3.0.0"}
        self.api.latest_id = 1
        patches = self.api.patches.copy()
        self.advertise(expected=False)
        self.assertEqual(self.api.patches, patches)

    def test_completed_receipt_rechecks_latest_and_recovers_a_broken_feed(self):
        self.advertise()
        self.api.bad_latest = True
        with self.assertRaises(AdvertisementError):
            self.advertise(expected=False)
        self.assertEqual(self.api.latest_id, 1)
        self.assertTrue(self.api.release["draft"])

    def test_concurrent_release_selection_is_not_overwritten(self):
        self.api.concurrent_latest = True
        with self.assertRaises(AdvertisementError):
            self.advertise()
        self.assertEqual(self.api.latest()["id"], 3)
        self.assertEqual(self.api.patches, [("releases/2", {"draft": False, "make_latest": "false"})])

    def test_wrong_remote_tag_after_an_interrupted_promotion_recovers_previous_latest(self):
        self.api.crash_promote = True
        with self.assertRaises(KeyboardInterrupt):
            self.advertise()
        api = self.api.api
        def wrong_tag(endpoint, **kwargs):
            if endpoint.startswith("git/ref/tags/"):
                return {"object": {"type": "commit", "sha": "b" * 40}}
            return api(endpoint, **kwargs)
        self.api.api = wrong_tag
        with self.assertRaises(AdvertisementError):
            self.advertise(expected=False)
        self.assertEqual(self.api.latest_id, 1)
        self.assertTrue(self.api.release["draft"])

    def test_stale_manifest_and_changed_assets_are_rejected(self):
        with self.assertRaises(AdvertisementError):
            expected_bundle(self.root, self.bundle, version=VERSION, commit="b" * 40, recipe_hashes=self.recipe_hashes)
        (self.bundle / TARGETS["windows-x86_64"]["asset"]).write_bytes(b"mutated")
        with self.assertRaises(AdvertisementError):
            expected_bundle(self.root, self.bundle, version=VERSION, commit=COMMIT, recipe_hashes=self.recipe_hashes)

    def test_unproven_or_mismatched_receipt_cannot_relax_uncertain_tag_guard(self):
        self.assertFalse(advertisement_resume_available(self.receipt, expected_commit=COMMIT, expected_version=VERSION))
        self.api.anonymous_failure = True
        with self.assertRaises(AdvertisementError):
            self.advertise()
        self.assertFalse(advertisement_resume_available(self.receipt, expected_commit="b" * 40, expected_version=VERSION))
        self.assertFalse(advertisement_resume_available(self.receipt, expected_commit=COMMIT, expected_version="3.0.0"))
        with self.assertRaisesRegex(AdvertisementError, "no exact verified-upload"):
            resume_advertisement(self.root, self.receipt, expected_commit="b" * 40, expected_version=VERSION, environ={})


class TransportTest(unittest.TestCase):
    def test_promotion_environment_has_only_release_credential(self):
        env = advertisement_environment({"PATH": "/bin", "RZM_RELEASE_GITHUB_TOKEN": "synthetic-release",
            "BREW_GITHUB_TOKEN": "synthetic-brew", "GH_TOKEN": "synthetic-other", "APPLE_API_PRIVATE_KEY": "synthetic-apple",
            "TAURI_SIGNING_PRIVATE_KEY": "synthetic-updater", "VOYAGE_API_KEY": "synthetic-provider"})
        self.assertEqual(env, {"PATH": "/bin", "GITHUB_TOKEN": "synthetic-release"})

    def test_authenticated_redirect_drops_token_and_rejects_downgrade(self):
        request = Request(API + "/releases/assets/1", headers={"Authorization": "Bearer synthetic"})
        redirected = SafeRedirect().redirect_request(request, None, 302, "Found", {}, "https://objects.githubusercontent.com/file")
        self.assertIsNone(redirected.get_header("Authorization"))
        with self.assertRaises(AdvertisementError):
            SafeRedirect().redirect_request(request, None, 302, "Found", {}, "http://example.invalid/file")

    def test_anonymous_http_transport_never_sends_authorization_and_checks_size(self):
        headers = []
        class Responses:
            def open(self, request, **kwargs):
                headers.append(dict(request.header_items()))
                return io.BytesIO(b"candidate")
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "asset"
            api = GitHub("synthetic-release")
            api.opener = Responses()
            url = f"{DOWNLOAD}/download/v2.0.0/candidate"
            api.download(url, path, size=9)
            self.assertEqual(path.read_bytes(), b"candidate")
            self.assertNotIn("Authorization", headers[0])
            with self.assertRaises(AdvertisementError):
                api.download(url, path, size=8)
            with self.assertRaisesRegex(AdvertisementError, "outside the release repository"):
                api.download(url, path, authenticated=True)

    def test_real_https_redirect_strips_token_streams_bytes_and_rejects_downgrade(self):
        requests = []
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            certificate, key = directory / "localhost.crt", directory / "localhost.key"
            subprocess.run(("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                            "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1",
                            "-keyout", str(key), "-out", str(certificate)), check=True,
                           capture_output=True, timeout=15)
            class Handler(BaseHTTPRequestHandler):
                def do_GET(self):
                    requests.append((self.path, self.headers.get("Authorization")))
                    if self.path.startswith("/repos/"):
                        scheme = "http" if self.path.endswith("/2") else "https"
                        self.send_response(302)
                        self.send_header("Location", f"{scheme}://localhost:{self.server.server_port}/package")
                        self.end_headers()
                    else:
                        self.send_response(200)
                        self.send_header("Content-Length", "9")
                        self.end_headers()
                        self.wfile.write(b"candidate")
                def log_message(self, *args):
                    pass
            server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
            tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            tls.load_cert_chain(certificate, key)
            server.socket = tls.wrap_socket(server.socket, server_side=True)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                base = f"https://127.0.0.1:{server.server_port}/repos/atomicobject/rhizome"
                api = GitHub("synthetic-loopback-token")
                api.opener = build_opener(SafeRedirect(), HTTPSHandler(context=ssl.create_default_context(cafile=certificate)))
                path = directory / "download"
                with patch("release_advertise.API", base):
                    api.download(base + "/releases/assets/1", path, size=9, authenticated=True)
                    self.assertEqual(path.read_bytes(), b"candidate")
                    self.assertEqual(requests, [("/repos/atomicobject/rhizome/releases/assets/1", "Bearer synthetic-loopback-token"),
                                                ("/package", None)])
                    with self.assertRaisesRegex(AdvertisementError, "unexpected size"):
                        api.download(base + "/releases/assets/1", path, size=8, authenticated=True)
                    api.download(f"https://localhost:{server.server_port}/package", path, size=9)
                    self.assertTrue(all(token is None for route, token in requests if route == "/package"))
                    count = len(requests)
                    with self.assertRaisesRegex(AdvertisementError, "outside HTTPS"):
                        api.download(base + "/releases/assets/2", path, size=9, authenticated=True)
                    self.assertEqual(len(requests), count + 1)
            finally:
                server.shutdown()
                server.server_close()
                thread.join(timeout=5)


if __name__ == "__main__":
    unittest.main()
