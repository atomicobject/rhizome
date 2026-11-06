#!/usr/bin/env python3
"""Replace native public CLI output with the verified prepared canonical bytes before archiving."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from desktop_candidates import CandidateError, MANIFEST, ROOT, TARGETS, read_candidate


def stage(path: Path, version: str, goos: str, arch: str, snapshot: bool, *, source=None, root=ROOT,
          commit: str | None = None) -> bool:
    source = os.environ if source is None else source
    if (snapshot or source.get('RZM_RELEASE_DESKTOP') != '1' or source.get('RZM_INTERNAL_S3_BUCKET', '').strip()
            or arch != 'amd64' or goos not in {'windows', 'linux'}):
        return False
    directory = source.get('RZM_DESKTOP_CANDIDATES_DIR', '').strip()
    if not directory:
        raise CandidateError('public desktop release requires the explicit native preparation stage')
    target = goos + '-x86_64'
    candidate = Path(directory) / target
    commit = commit or subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
    manifest = json.loads((candidate / MANIFEST).read_text())
    checked, _ = read_candidate(candidate, version=version, commit=commit,
                                canonical_hash=manifest.get('canonicalCliSha256'), production=True)
    if checked['target'] != target:
        raise CandidateError('prepared canonical CLI target differs from its selected GoReleaser output')
    path = path.resolve()
    with tempfile.NamedTemporaryFile(prefix='.canonical-cli-', dir=path.parent, delete=False) as temporary:
        pending = Path(temporary.name)
    try:
        shutil.copyfile(candidate / TARGETS[target]['cli'], pending)
        pending.chmod(0o755)
        pending.replace(path)
    finally:
        pending.unlink(missing_ok=True)
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('path', type=Path)
    parser.add_argument('version')
    parser.add_argument('goos')
    parser.add_argument('arch')
    parser.add_argument('snapshot', choices=('true', 'false'))
    args = parser.parse_args()
    try:
        stage(args.path, args.version, args.goos, args.arch, args.snapshot == 'true')
    except (CandidateError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f'canonical desktop CLI: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
