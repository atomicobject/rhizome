#!/usr/bin/env python3
"""Package only public files, preserving executable modes across artifact upload."""
import hashlib
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile

FILES = (
    'manifest.json', 'keys/local/updater.pub', 'tools/rhizome-verify-updater',
    *(f'scripts/{name}.py' for name in ('build', 'check', 'feed', 'install-a', 'launch')),
    *(f'configs/{label}{suffix}' for label in 'ABC' for suffix in ('.json', '-isolation.patch')),
    *(f'artifacts/{label}/{name}' for label in 'ABC' for name in (
        'Rhizome-update-proof.app.tar.gz', 'Rhizome-update-proof.app.tar.gz.sig', 'receipt.json')),
)


def public_metadata(member):
    member.uid = member.gid = 0
    member.uname = member.gname = ''
    member.mode = 0o755 if member.name.startswith('tools/') else 0o644
    return member


def bundle(root, output):
    assert not output.exists(), 'Refusing to overwrite public bundle'
    with tarfile.open(output, 'w:gz') as archive:
        for name in FILES:
            path = root / name
            assert path.is_file() and not path.is_symlink(), f'Expected a regular public file: {name}'
            archive.add(path, arcname=name, recursive=False, filter=public_metadata)
    with tarfile.open(output) as archive:
        members = archive.getmembers()
        assert [member.name for member in members] == list(FILES)
        for member in members:
            path = root / member.name
            assert member.isfile()
            assert member.mode == (0o755 if member.name.startswith('tools/') else 0o644)
            assert hashlib.sha256(archive.extractfile(member).read()).digest() == hashlib.sha256(path.read_bytes()).digest()


if __name__ == '__main__':
    from check import all_artifacts
    from build import ROOT, clean_env
    all_artifacts()
    output = ROOT.parent / 'rhizome-updater-acceptance.tar.gz'
    bundle(ROOT, output)
    with tempfile.TemporaryDirectory(prefix='rzm-public-bundle-') as temporary:
        with tarfile.open(output) as archive:
            archive.extractall(temporary, filter='data')
        subprocess.run([sys.executable, str(Path(temporary)/'scripts/check.py'), 'all'],
                       check=True, env=clean_env())
    print(f'Verified {len(FILES)} public files in {output.name}')
