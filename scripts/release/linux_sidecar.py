"""Preserve the canonical CLI while linuxdeploy prepares the GUI's libraries."""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import json
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile

from desktop_candidates import CandidateError, HASH, sha256


@contextmanager
def bundle_environment(root: Path, bundle_dir: Path, canonical_hash: str, env: dict):
    """linuxdeploy 07333c6 supports PATCHELF and NO_STRIP, but no binary exclusion."""
    product = json.loads((root / 'desktop/src-tauri/tauri.conf.json').read_text(encoding='utf-8'))['productName']
    if not isinstance(product, str) or product in {'', '.', '..'} or Path(product).name != product:
        raise CandidateError('invalid AppImage product name')
    real = shutil.which('patchelf', path=env.get('PATH'))
    if real is None:
        raise CandidateError('AppImage packaging requires the system patchelf')
    sidecar = bundle_dir / 'appimage' / (product + '.AppDir') / 'usr/bin/rzm'
    with tempfile.TemporaryDirectory(prefix='rhizome-linux-patchelf-') as temporary:
        wrapper = Path(temporary) / 'patchelf'
        command = [sys.executable, str(Path(__file__).resolve()), '--real-patchelf', real,
                   '--sidecar', str(sidecar.resolve()), '--sha256', canonical_hash, '--']
        wrapper.write_text('#!/bin/sh\nexec ' + shlex.join(command) + ' "$@"\n', encoding='utf-8')
        wrapper.chmod(0o755)
        yield {**env, 'NO_STRIP': '1', 'PATCHELF': str(wrapper)}


def patchelf(arguments: list[str], sidecar: Path, canonical_hash: str, real: Path, *, runner=subprocess.call) -> int:
    protected = [index for index, argument in enumerate(arguments) if Path(argument).resolve() == sidecar.resolve()]
    if protected:
        if sidecar.is_symlink() or not sidecar.is_file() or not HASH.fullmatch(canonical_hash) or sha256(sidecar) != canonical_hash:
            raise CandidateError('AppImage sidecar changed before canonical preservation')
        if len(arguments) == 2 and protected == [1] and arguments[0].startswith('--print-'):
            return runner([str(real), *arguments])
        if len(arguments) == 3 and protected == [2] and arguments[:2] == ['--set-rpath', '$ORIGIN/../lib']:
            # linuxdeploy does not read back this write; the CLI uses baseline system libraries.
            return 0
        raise CandidateError('unexpected AppImage sidecar patchelf operation')
    return runner([str(real), *arguments])


def mismatch_evidence(canonical: Path, extracted: Path, destination: Path, *, env: dict, runner):
    """Retain only public hashes and bounded ELF metadata, never tool output or bytes."""
    def metadata(path):
        value = {'sha256': sha256(path), 'size': path.stat().st_size}
        try:
            result = runner(['readelf', '-h', '-l', '-d', '--version-info', str(path)],
                env=env, check=True, capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=30)
            output = result.stdout
            value['elf'] = {key.lower(): item.strip()[:128] for key, item in
                            re.findall(r'^\s*(Class|Type|Machine):\s*(.*)$', output, re.MULTILINE)}
            value['needed'] = [name[:96] for name in sorted(set(re.findall(r'\(NEEDED\).*?\[([^\]]+)\]', output)))[:32]]
            value['rpath'] = [path[:256] for path in re.findall(r'\((?:RPATH|RUNPATH)\).*?\[([^\]]+)\]', output)[:2]]
            value['interpreter'] = [path[:256] for path in re.findall(r'\[Requesting program interpreter: ([^\]]+)\]', output)[:1]]
            value['glibc'] = [version[:16] for version in sorted(set(re.findall(r'\bGLIBC_([0-9]+(?:\.[0-9]+)+)\b', output)))[:32]]
        except (OSError, subprocess.SubprocessError):
            value['elfInspection'] = 'unavailable'
        return value
    try:
        sidecars = [path for path in extracted.rglob('rzm') if path.is_file() and not path.is_symlink()]
        evidence = {'canonical': metadata(canonical), 'extracted': [metadata(path) for path in sidecars[:2]]}
        destination.parent.mkdir(parents=True, exist_ok=True)
        encoded = json.dumps(evidence, indent=2)
        if len(encoded) > 16000:
            evidence = {'canonical': {'sha256': sha256(canonical)},
                        'extracted': [{'sha256': sha256(path)} for path in sidecars[:2]], 'elfInspection': 'metadata exceeded limit'}
        destination.write_text(json.dumps(evidence, indent=2) + '\n', encoding='utf-8')
    except OSError:
        pass  # Diagnostics must preserve the original rejecting hash/inventory error.


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--real-patchelf', type=Path, required=True)
    parser.add_argument('--sidecar', type=Path, required=True)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    try:
        arguments = args.arguments[1:] if args.arguments[:1] == ['--'] else args.arguments
        return patchelf(arguments, args.sidecar, args.sha256, args.real_patchelf)
    except (CandidateError, OSError) as error:
        print(f'canonical AppImage sidecar: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
