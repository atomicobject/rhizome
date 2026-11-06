"""Join the existing notarized Mac result with exact-run native producer candidates."""
from __future__ import annotations

import json
from pathlib import Path
import shutil
import tempfile

from assemble_desktop import assemble
from desktop_candidates import (CandidateError, MANIFEST, NATIVE_TARGETS, candidate_manifest,
                                mac_sidecar_inventory, sha256)
from prepare_native_desktop import load, verify_files


def join(root: Path, rzm: Path, version: str, output: Path, *, source: dict, runner) -> list[Path]:
    version = version.removeprefix('v')
    prepared = source.get('RZM_DESKTOP_CANDIDATES_DIR', '').strip()
    receipt_path = source.get('RZM_DESKTOP_CANDIDATES_RECEIPT', '').strip()
    if not prepared or not receipt_path:
        raise CandidateError('production desktop assembly requires verified native candidates and their receipt')
    commit_result = runner(['git', 'rev-parse', 'HEAD'], cwd=root, check=True, capture_output=True, text=True)
    commit = commit_result.stdout.strip()
    identity = {'repository': 'atomicobject/rhizome', 'source_commit': commit, 'version': version}
    receipt = load(Path(receipt_path), identity)
    if receipt is None or receipt['attempt']['state'] != 'success' or 'files' not in receipt:
        raise CandidateError('native candidates lack a successful coordinator receipt')
    hashes = verify_files(Path(prepared), identity, expected=receipt['files'])
    archive = output / 'Rhizome-macos-arm64.app.tar.gz'
    inventory = mac_sidecar_inventory(archive)
    tools = {}
    for name, command in [('go', ['go', 'version']), ('rust', ['rustc', '--version']),
                          ('tauri', ['npm', 'run', 'tauri', '--', '--version'])]:
        value = runner(command, cwd=root / 'desktop', check=True, capture_output=True, text=True)
        tools[name] = value.stdout.strip()
    manifest = candidate_manifest(target='darwin-aarch64', version=version, commit=commit, package=archive,
                                  cli_hash=sha256(rzm), packaged_cli_hash=inventory[0]['sha256'], inventory=inventory,
                                  native_signature={'status': 'notarized'}, toolchains=tools, production=True)
    with tempfile.TemporaryDirectory(prefix='.desktop-join-', dir=output.parent) as temporary:
        temporary = Path(temporary)
        mac = temporary / 'darwin-aarch64'
        mac.mkdir()
        shutil.copyfile(rzm, mac / 'rzm')
        shutil.copyfile(archive, mac / archive.name)
        shutil.copyfile(Path(str(archive) + '.sig'), mac / (archive.name + '.sig'))
        (mac / MANIFEST).write_text(json.dumps(manifest, indent=2) + '\n')
        canonical = {target: value['cli'] for target, value in hashes.items()}
        canonical['darwin-aarch64'] = manifest['canonicalCliSha256']
        assembled = temporary / 'assembly'
        assemble([mac, *(Path(prepared) / target for target in sorted(NATIVE_TARGETS))], version, commit,
                 canonical, assembled, source=source, root=root, runner=runner)
        # The joined feed is installed last; failure leaves the prior complete feed usable.
        paths = sorted(assembled.iterdir(), key=lambda path: path.name == 'latest.json')
        staged = []
        for path in paths:
            pending = output / ('.' + path.name + '.pending')
            shutil.copyfile(path, pending)
            pending.replace(output / path.name)
            staged.append(output / path.name)
    return staged
