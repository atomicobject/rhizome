"""Explicit synthetic-key integration check; never uses a production signing key."""

from __future__ import annotations

import json
import os
from pathlib import Path
import tempfile
import unittest

from assemble_desktop import assemble
from desktop_candidates import MANIFEST, ROOT, TARGETS, candidate_manifest, sha256
from updater_fixture import prepare
from desktop_candidates_test import dependency_fixture
from verify_desktop_signing import DesktopSigningError, verify_archive


@unittest.skipUnless(os.environ.get("RZM_UPDATER_SIGNER_TEST") == "1", "explicit synthetic signer check")
class SignerRoundtripTest(unittest.TestCase):
    def test_detached_signer_and_pinned_verifier_accept_exact_bytes_and_reject_corruption(self):
        with tempfile.TemporaryDirectory(prefix="rhizome-updater-fixture-") as temporary:
            root = Path(temporary)
            source = {"PATH": os.environ.get("PATH", ""), "CI": "true"}
            overlay = prepare(root / "key", "http://127.0.0.1:9444/latest.json", source=source)
            public_key = json.loads(overlay.read_text())["plugins"]["updater"]["pubkey"]
            candidate = root / "candidate"
            candidate.mkdir()
            cli = candidate / "rzm.exe"
            cli.write_bytes(b"synthetic canonical CLI\n")
            package = candidate / TARGETS["windows-x86_64"]["asset"]
            package.write_bytes(b"synthetic updater payload, not an executable installer\n")
            digest = sha256(cli)
            manifest = candidate_manifest(target="windows-x86_64", version="1.2.3", commit="a" * 40,
                                          package=package, cli_hash=digest,
                                          inventory=[{"name": "rzm.exe", "sha256": digest}],
                                          toolchains={"fixture": "synthetic"},
                                          native_signature={"status": "unsigned"}, production=False,
                                          dependencies=dependency_fixture("windows-x86_64"))
            (candidate / MANIFEST).write_text(json.dumps(manifest))
            feed_path = assemble([candidate], "1.2.3", "a" * 40, {"windows-x86_64": digest}, root / "feed",
                                 production=False, required_targets={"windows-x86_64"},
                                 test_public_key=public_key, test_base_url="http://127.0.0.1:9444",
                                 source={**source, "TAURI_SIGNING_PRIVATE_KEY_PATH": str(root / "key/updater")})
            feed = json.loads(feed_path.read_text())
            self.assertEqual("1.2.3", feed["version"])
            artifact = feed_path.parent / package.name
            signature = Path(str(artifact) + ".sig")
            verify_archive(ROOT, artifact, signature, environ=source, public_key=public_key)
            artifact.write_bytes(artifact.read_bytes() + b"corrupted")
            with self.assertRaises(DesktopSigningError):
                verify_archive(ROOT, artifact, signature, environ=source, public_key=public_key)


if __name__ == "__main__":
    unittest.main()
