import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

from ci_release_frontend import PREFIXES, create, extract


class FrontendTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repo"
        self.root.mkdir()
        for command in (["git", "init"], ["git", "config", "user.name", "Test"], ["git", "config", "user.email", "test@example.invalid"]):
            subprocess.run(command, cwd=self.root, check=True, capture_output=True)
        (self.root / ".gitignore").write_text("desktop/dist/\n")
        self.web = self.root / PREFIXES[0]
        self.desktop = self.root / PREFIXES[1]
        self.web.mkdir(parents=True)
        self.desktop.mkdir(parents=True)
        (self.web / "index.html").write_text("built web")
        (self.desktop / "index.html").write_text("built desktop")
        (self.root / "source.txt").write_text("source")
        subprocess.run(["git", "add", "."], cwd=self.root, check=True, capture_output=True)
        subprocess.run(["git", "commit", "-m", "Source"], cwd=self.root, check=True, capture_output=True)
        self.source = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=self.root, text=True).strip()
        self.archive = Path(self.temp.name) / "frontend.tar.gz"
        create(self.root, self.source, "v1.2.3", self.archive)

    def rewrite_archive(self, transform):
        with tarfile.open(self.archive) as archive:
            files = {member.name: archive.extractfile(member).read() for member in archive.getmembers()}
        transform(files)
        with tarfile.open(self.archive, "w:gz") as archive:
            for name, data in files.items():
                info = tarfile.TarInfo(name)
                info.size = len(data)
                archive.addfile(info, io.BytesIO(data))

    def test_exact_roundtrip_replaces_stale_generated_files(self):
        (self.web / "obsolete.js").write_text("stale")
        (self.web / "index.html").write_text("stale web")
        extract(self.root, self.source, "v1.2.3", self.archive)
        self.assertEqual((self.web / "index.html").read_text(), "built web")
        self.assertEqual((self.desktop / "index.html").read_text(), "built desktop")
        self.assertFalse((self.web / "obsolete.js").exists())

    def test_tamper_rejection_precedes_output_mutation(self):
        (self.web / "index.html").write_text("keep current output")
        self.rewrite_archive(lambda files: files.update({PREFIXES[0] + "index.html": b"tampered"}))
        with self.assertRaisesRegex(ValueError, "hash mismatch"):
            extract(self.root, self.source, "v1.2.3", self.archive)
        self.assertEqual((self.web / "index.html").read_text(), "keep current output")

    def test_source_and_version_mismatch_are_rejected(self):
        for source, version in (("0" * 40, "v1.2.3"), (self.source, "v1.2.4")):
            with self.subTest(version=version), self.assertRaises(ValueError):
                extract(self.root, source, version, self.archive)

    def test_unexpected_path_and_symlink_are_rejected(self):
        self.rewrite_archive(lambda files: files.update({"../../outside": b"escape"}))
        with self.assertRaises(ValueError):
            extract(self.root, self.source, "v1.2.3", self.archive)
        create(self.root, self.source, "v1.2.3", self.archive)
        with tarfile.open(self.archive, "w:gz") as archive:
            info = tarfile.TarInfo(PREFIXES[0] + "index.html")
            info.type, info.linkname = tarfile.SYMTYPE, "/outside"
            archive.addfile(info)
        with self.assertRaisesRegex(ValueError, "ordinary files"):
            extract(self.root, self.source, "v1.2.3", self.archive)

    def test_dirty_source_is_not_mislabeled_as_approved_frontend(self):
        (self.root / "source.txt").write_text("unreviewed edit")
        with self.assertRaisesRegex(ValueError, "unrelated"):
            create(self.root, self.source, "v1.2.3", self.archive)

    def test_symlinked_parent_inside_checkout_is_rejected(self):
        saved = self.root / "saved-desktop"
        (self.root / "desktop").rename(saved)
        (self.root / "desktop").symlink_to(saved, target_is_directory=True)
        with (self.root / ".git/info/exclude").open("a") as excludes:
            excludes.write("\ndesktop\nsaved-desktop\n")
        with self.assertRaisesRegex(ValueError, "symlinks"):
            extract(self.root, self.source, "v1.2.3", self.archive)
        self.assertEqual((saved / "dist/index.html").read_text(), "built desktop")


if __name__ == "__main__":
    unittest.main()
