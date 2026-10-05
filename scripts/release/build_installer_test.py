#!/usr/bin/env python3
"""Tests for the hosted installer staging helper."""

from __future__ import annotations

import stat
import tempfile
from pathlib import Path
import unittest

from build_installer import PUBLIC_RELEASES_URL, internal_releases_url, stage_installer


class StageInstallerTest(unittest.TestCase):
    def test_stages_installer_script_with_executable_mode(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir_name:
            temp_dir = Path(temp_dir_name)
            installer = temp_dir / "install-rzm.sh"
            output_dir = temp_dir / "dist"

            installer.write_text("#!/usr/bin/env bash\necho installer\n")
            output_path = stage_installer(output_dir, installer)

            self.assertEqual(output_dir / "install-rzm.sh", output_path)
            self.assertTrue(output_path.is_file())
            mode = stat.S_IMODE(output_path.stat().st_mode)
            self.assertTrue(mode & stat.S_IXUSR)
            checksum = (output_dir / "install-rzm.sh.sha256").read_text()
            self.assertRegex(checksum, r"^[0-9a-f]{64}  install-rzm\.sh\n$")

    def test_internal_release_points_every_default_at_the_mirror(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir_name:
            temp_dir = Path(temp_dir_name)
            installer = temp_dir / "install-rzm.sh"
            installer.write_text(f'A="${{X:-{PUBLIC_RELEASES_URL}}}"\nB="${{X:-{PUBLIC_RELEASES_URL}}}"\n')
            mirror = internal_releases_url({"RZM_INTERNAL_S3_BUCKET": "example-bucket"})

            staged = stage_installer(temp_dir / "dist", installer, mirror).read_text()

        self.assertEqual("https://example-bucket.s3.amazonaws.com/releases", mirror)
        self.assertEqual(2, staged.count(mirror))
        self.assertNotIn(PUBLIC_RELEASES_URL, staged)
        self.assertEqual("", internal_releases_url({}))


if __name__ == "__main__":
    unittest.main()
