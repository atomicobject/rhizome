#!/usr/bin/env python3
"""Stage the hosted Rhizome installer script for release publishing."""

from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import shutil
import stat
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from scripts.teamkeys.release import verify_artifact


DEFAULT_OUTPUT_DIR = Path("dist/releases")
DEFAULT_INSTALLER = Path("scripts/install/install-rzm.sh")
PUBLIC_RELEASES_URL = "https://api.github.com/repos/atomicobject/rhizome/releases"


def internal_releases_url(environ: dict[str, str]) -> str:
    """Internal releases point installers at the private S3 mirror named in the release Environment."""
    bucket = environ.get("RZM_INTERNAL_S3_BUCKET", "").strip()
    return f"https://{bucket}.s3.amazonaws.com/releases" if bucket else ""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", default=str(DEFAULT_OUTPUT_DIR), help="Directory for staged installer artifact.")
    parser.add_argument("--installer", default=str(DEFAULT_INSTALLER), help="Path to install-rzm.sh.")
    return parser.parse_args()


def stage_installer(output_dir: Path, installer: Path, releases_url: str = "") -> Path:
    if not installer.is_file():
        raise SystemExit(f"error: installer not found: {installer}")
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / "install-rzm.sh"
    text = installer.read_text(encoding="utf-8")
    if releases_url:
        if PUBLIC_RELEASES_URL not in text:
            raise SystemExit("error: installer no longer contains the public releases URL to replace")
        text = text.replace(PUBLIC_RELEASES_URL, releases_url)
    output_path.write_text(text, encoding="utf-8")
    shutil.copymode(installer, output_path)
    output_path.chmod(output_path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    digest = hashlib.sha256(output_path.read_bytes()).hexdigest()
    (output_dir / "install-rzm.sh.sha256").write_text(
        f"{digest}  install-rzm.sh\n", encoding="utf-8"
    )
    return output_path


def main() -> int:
    args = parse_args()
    output_path = stage_installer(Path(args.output_dir), Path(args.installer), internal_releases_url(dict(os.environ)))
    try:
        verify_artifact(output_path, os.environ)
    except (ValueError, OSError) as error:
        raise SystemExit(f"error: {error}") from error
    print(output_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
