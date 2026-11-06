"""Join the existing notarized Mac result with same-run source-bound native producer candidates."""
from __future__ import annotations

import json
from pathlib import Path
import shutil
import tempfile

from assemble_desktop import assemble
from desktop_candidates import (CandidateError, MANIFEST, NATIVE_TARGETS, candidate_manifest,
                                mac_sidecar_inventory, read_candidate, sha256)
from ci_release_artifacts import validate_native_inputs


def join(root: Path, rzm: Path, version: str, output: Path, *, source: dict, runner, candidate_only=False) -> list[Path]:
    version = version.removeprefix('v')
    prepared = source.get('RZM_DESKTOP_CANDIDATES_DIR', '').strip()
    if not prepared:
        raise CandidateError('production desktop assembly requires the verified same-run native candidates')
    commit_result = runner(['git', 'rev-parse', 'HEAD'], cwd=root, check=True, capture_output=True, text=True)
    commit = commit_result.stdout.strip()
    validate_native_inputs(Path(prepared), commit, 'v' + version, candidate_only=candidate_only)
    hashes = {}
    for target in sorted(NATIVE_TARGETS):
        directory = Path(prepared) / target
        value = json.loads((directory / MANIFEST).read_text())
        manifest, _ = read_candidate(directory, version=version, commit=commit,
                                     canonical_hash=value.get('canonicalCliSha256'), production=not candidate_only)
        if manifest['target'] != target:
            raise CandidateError('native candidate target differs from its selected CI group')
        hashes[target] = manifest['canonicalCliSha256']
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
        canonical = dict(hashes)
        canonical['darwin-aarch64'] = manifest['canonicalCliSha256']
        assembled = temporary / 'assembly'
        assemble([mac, *(Path(prepared) / target for target in sorted(NATIVE_TARGETS))], version, commit,
                 canonical, assembled, source=source, root=root, runner=runner,
                 production=not candidate_only, candidate_only=candidate_only)
        # The joined feed is installed last; failure leaves the prior complete feed usable.
        paths = sorted(assembled.iterdir(), key=lambda path: path.name == 'latest.json')
        staged = []
        for path in paths:
            pending = output / ('.' + path.name + '.pending')
            shutil.copyfile(path, pending)
            pending.replace(output / path.name)
            staged.append(output / path.name)
    return staged
