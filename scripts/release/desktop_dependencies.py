"""Inspect the extracted CLI's native dependencies without developer search paths."""
from __future__ import annotations

import json
from pathlib import Path
import re
import subprocess
import shutil
import tempfile

from desktop_candidates import CandidateError, validate_dependencies

BASELINES = {'linux-x86_64': 'ubuntu-24.04-x86_64', 'windows-x86_64': 'windows-11-x86_64'}


def _run(command, *, root, env, runner):
    try:
        result = runner(command, cwd=root, env=env, check=True, capture_output=True, text=True, timeout=60)
        return result.stdout.strip()
    except (OSError, subprocess.SubprocessError) as error:
        raise CandidateError(f'native dependency inspection failed: {Path(command[0]).name}') from error


def _version(value):
    return tuple(int(part) for part in value.split('.'))


def linux_dependencies(cli: Path, version: str, *, root: Path, env: dict, runner) -> dict:
    env = {key: value for key, value in env.items() if not key.startswith(('LD_', 'DYLD_')) and key != 'GLIBC_TUNABLES'}
    host = _run(['cat', '/etc/os-release'], root=root, env=env, runner=runner)
    fields = dict(re.findall(r'^([A-Z_]+)="?([^"\n]*)"?$', host, re.MULTILINE))
    if fields.get('ID') != 'ubuntu' or fields.get('VERSION_ID') != '24.04':
        raise CandidateError('native Linux dependency evidence requires the Ubuntu 24.04 baseline host')
    libc = _run(['getconf', 'GNU_LIBC_VERSION'], root=root, env=env, runner=runner)
    if libc != 'glibc 2.39':
        raise CandidateError('Ubuntu 24.04 dependency evidence requires its glibc 2.39 baseline')
    dynamic = _run(['readelf', '-d', str(cli)], root=root, env=env, runner=runner)
    if re.search(r'\((?:RPATH|RUNPATH)\)', dynamic):
        raise CandidateError('copied CLI must not depend on installation-local RPATH/RUNPATH libraries')
    needed = sorted(set(re.findall(r'\(NEEDED\).*?\[([^\]]+)\]', dynamic)))
    headers = _run(['readelf', '-l', str(cli)], root=root, env=env, runner=runner)
    interpreter = re.findall(r'\[Requesting program interpreter: ([^\]]+)\]', headers)
    if (needed and len(interpreter) != 1) or len(interpreter) > 1:
        raise CandidateError('cannot identify the CLI system ELF interpreter')
    if interpreter and not any(Path(interpreter[0]).resolve().is_relative_to(base)
                               for base in (Path('/lib'), Path('/lib64'), Path('/usr/lib'), Path('/usr/lib64'))):
        raise CandidateError('CLI ELF interpreter is outside the baseline system directories')
    symbols = _run(['readelf', '--version-info', str(cli)], root=root, env=env, runner=runner)
    glibc = sorted(set(re.findall(r'\bGLIBC_([0-9]+(?:\.[0-9]+)+)\b', symbols)), key=_version)
    if glibc and _version(glibc[-1]) > (2, 39):
        raise CandidateError('extracted CLI requires glibc newer than Ubuntu 24.04')
    imports = []
    if needed:
        listing = _run(['ldd', str(cli)], root=root, env=env, runner=runner)
        if 'not found' in listing:
            raise CandidateError('extracted CLI has unresolved Linux shared libraries')
        resolved = {}
        for line in listing.splitlines():
            match = re.fullmatch(r'\s*(\S+)\s+=>\s+(\S+)\s+\(0x[0-9a-fA-F]+\)\s*', line)
            if not match:
                continue
            name, path = match.groups()
            actual = Path(path).resolve()
            if not any(actual.is_relative_to(base) for base in (Path('/lib'), Path('/lib64'), Path('/usr/lib'), Path('/usr/lib64'))):
                raise CandidateError('extracted CLI resolves a library outside the baseline system directories')
            resolved[name] = str(actual)
        if set(needed) - resolved.keys():
            raise CandidateError('ldd did not resolve every declared CLI dependency')
        imports = [{'name': name, 'path': path, 'origin': 'system'} for name, path in sorted(resolved.items())]
    # Executing only after resolution checks avoids inheriting developer loader overrides.
    with tempfile.TemporaryDirectory(prefix='rhizome-cli-dependencies-') as temporary:
        copy = Path(temporary) / 'rzm'
        shutil.copyfile(cli, copy)
        copy.chmod(0o755)
        reported = _run([str(copy), '--version'], root=Path(temporary),
                        env={**env, 'PATH': '/usr/bin:/bin'}, runner=runner)
    if not reported.split() or reported.split()[-1].removeprefix('v') != version:
        raise CandidateError('extracted Linux CLI cannot run with the baseline system environment')
    evidence = {'status': 'verified', 'baseline': BASELINES['linux-x86_64'],
            'host': {'os': 'Ubuntu', 'version': '24.04', 'architecture': 'x86_64', 'libc': libc},
            'minimumGlibc': glibc[-1] if glibc else None, 'imports': imports, 'interpreter': interpreter[0] if interpreter else None,
            'probeVersion': reported,
            'methods': ['readelf-dynamic', 'readelf-symbol-versions', 'ldd-without-loader-overrides', 'system-path-version']}
    validate_dependencies(evidence, 'linux-x86_64', version)
    return evidence


def windows_dependencies(cli: Path, version: str, *, root: Path, env: dict, runner) -> dict:
    dump = _run(['objdump', '-p', str(cli)], root=root, env=env, runner=runner)
    names = sorted(set(name.lower() for name in re.findall(r'^\s*DLL Name:\s*(\S+)\s*$', dump, re.MULTILINE)))
    if not names or any(not re.fullmatch(r'[a-z0-9_.+-]+\.dll', name) for name in names):
        raise CandidateError('objdump did not provide a valid Windows CLI import inventory')
    result = _run(['powershell.exe', '-NoProfile', '-NonInteractive', '-File',
                   str(root / 'scripts/release/verify_windows_dependencies.ps1'), '-Path', str(cli),
                   '-Imports', ','.join(names)], root=root, env=env, runner=runner)
    try:
        checked = json.loads(result)
        imports = checked['imports']
        host = checked['host']
        reported = checked['probeVersion']
        if (not isinstance(imports, list) or any(not isinstance(item, dict) for item in imports)
                or sorted(item['name'] for item in imports) != names
                or any(item.get('origin') != 'system' or item.get('signature') != 'microsoft-valid' for item in imports)
                or not isinstance(host, dict) or not host.get('version') or not host.get('os')
                or not isinstance(reported, str) or not reported.split()
                or reported.split()[-1].removeprefix('v') != version):
            raise ValueError('Windows dependency evidence')
    except (ValueError, KeyError, TypeError) as error:
        raise CandidateError('Windows system-only dependency evidence is invalid or incomplete') from error
    evidence = {'status': 'verified', 'baseline': BASELINES['windows-x86_64'], 'host': host,
            'imports': imports, 'probeVersion': reported,
            'methods': ['objdump-imports', 'system32-only-loader', 'microsoft-system-signatures', 'standalone-system-path-version']}
    validate_dependencies(evidence, 'windows-x86_64', version)
    return evidence


def inspect_dependencies(cli: Path, target: str, version: str, *, root: Path, env: dict, runner=subprocess.run) -> dict:
    if target == 'linux-x86_64':
        return linux_dependencies(cli, version, root=root, env=env, runner=runner)
    if target == 'windows-x86_64':
        return windows_dependencies(cli, version, root=root, env=env, runner=runner)
    raise CandidateError('dependency inspection is only defined for the native desktop targets')
