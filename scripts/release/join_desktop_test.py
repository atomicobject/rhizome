import json
from pathlib import Path
import shutil
import tempfile
import unittest

from ci_release_artifacts import ArtifactError
from ci_release_artifacts_test import JOINED, SOURCE, VERSION, fixture, ignore_signature
from desktop_candidates import REQUIRED_TARGETS
from join_desktop import join


class JoinTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        # The fixture runs prepare-native and join, and checks each package was signed exactly once.
        fixture(self.root)

    def join(self, output):
        calls = []
        def runner(command, **kwargs):
            calls.append(command)
            raise AssertionError("signing must not start")
        with self.assertRaises(ArtifactError):
            join(self.root / "native", SOURCE, VERSION, self.root / output, environ={"TAURI_SIGNING_PRIVATE_KEY": "synthetic"},
                 root=self.root, runner=runner, verifier=ignore_signature)
        self.assertEqual([], calls)
        self.assertFalse((self.root / output).exists())

    def test_coordinator_signs_the_mac_archive_with_every_linux_package(self):
        feed = json.loads((self.root / JOINED / "latest.json").read_text())
        self.assertEqual(REQUIRED_TARGETS, set(feed["platforms"]))
        mac = json.loads((self.root / JOINED / "desktop-assembly.json").read_text())["candidates"]["darwin-aarch64"]
        # The notarized sidecar differs from the raw archived CLI; Linux sidecars must not.
        self.assertNotEqual(mac["canonicalCliSha256"], mac["packagedCliSha256"])

    def test_changed_or_missing_native_input_stops_before_signing(self):
        archive = self.root / "native/darwin-aarch64/Rhizome-macos-arm64.app.tar.gz"
        original = archive.read_bytes()
        archive.write_bytes(original + b"changed after verification")
        self.join("changed")
        archive.write_bytes(original)
        shutil.rmtree(self.root / "native/linux-aarch64")
        self.join("missing")


if __name__ == "__main__":
    unittest.main()
