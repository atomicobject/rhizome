#!/usr/bin/env python3
"""Export pinned source and apply only acceptance isolation changes."""
import hashlib
import difflib
import json
from pathlib import Path
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[1]
if len(sys.argv) != 2:
    raise SystemExit('usage: prepare.py <repository>')
REPO = Path(sys.argv[1]).resolve()
ID = 'com.atomicobject.rhizome.update-proof'
NAME = 'Rhizome Update Proof'
ENDPOINT = 'http://127.0.0.1:19452/latest.json'
HEAD = 'b8d5d8c3b77765dc94e2516762bddf01ef6b5c83'
BASE = '7c79303c925e03b38bef7b9aa3783c78b15130a4'
HASHES = {
    BASE: {'desktop/src-tauri/src/updater.rs': '90b1218853f68e358f6596ef16e5fdb779af02154aece3f96ef6c8638bd2b659'},
    HEAD: {
        'desktop/src-tauri/src/updater.rs': '07c4f693697728445bcc329b119eb1e7204e4667f6921822e8d18dac29965006',
        'desktop/src-tauri/src/updater/macos.rs': '896ebe740f97f353fc31b04c9d40954fe43c234c54cb56f6de49a9ad9f581c96',
        'desktop/src-tauri/src/updater/archive.rs': 'e6fa2c0d915f20afc7c1359db4f04ef6ccee450abb436a116e548ea412ed5193',
    },
}

def write_change(root, path, text):
    file = root / path
    patches.extend(difflib.unified_diff(file.read_text().splitlines(True), text.splitlines(True),
                                        fromfile='a/' + path, tofile='b/' + path))
    file.write_text(text)

def change(root, path, before, after):
    file = root / path
    text = file.read_text()
    assert before in text, f'missing expected source in {path}'
    write_change(root, path, text.replace(before, after))

manifest = {'schema': 1, 'expectedTeam': 'F6YUH468ZK', 'identifier': ID, 'name': NAME, 'cliLink': '/usr/local/bin/rzm-update-proof', 'endpoint': ENDPOINT, 'builds': {}}
public = (ROOT / 'keys/local/updater.pub').read_text().strip()
for label, ref, version in [('A', BASE, '0.52.1'), ('B', HEAD, '0.52.2'), ('C', HEAD, '0.52.3')]:
    patches = []
    source = ROOT / 'sources' / label
    commit = subprocess.check_output(['git', 'rev-parse', ref + '^{commit}'], cwd=REPO, text=True).strip()
    assert not source.exists(), f'refusing to overwrite {source}'
    source.mkdir()
    archive = ROOT / 'sources' / f'{label}.tar'
    with archive.open('wb') as output:
        subprocess.run(['git', 'archive', commit], cwd=REPO, stdout=output, check=True)
    with tarfile.open(archive) as bundle:
        bundle.extractall(source, filter='data')
    archive.unlink()
    baseline = source / 'desktop/src-tauri/tauri.conf.json'
    config = json.loads(baseline.read_text())
    production_updater = config['plugins']['updater']
    config.update(productName=NAME, identifier=ID, version=version)
    config['plugins']['updater'] = {'endpoints': [ENDPOINT], 'pubkey': public, 'dangerousInsecureTransportProtocol': True}
    config['build']['beforeBuildCommand'] = ''
    config['bundle'].update(targets=['app'], createUpdaterArtifacts=False, externalBin=['binaries/rzm'])
    config['bundle']['macOS']['hardenedRuntime'] = True
    write_change(source, 'desktop/src-tauri/tauri.conf.json', json.dumps(config, indent=2) + '\n')
    change(source, 'pkg/app/appbundle/bundle.go', 'i.Identifier == "com.atomicobject.rhizome"', f'i.Identifier == "{ID}"')
    change(source, 'pkg/app/desktop/cli_link.go', 'filepath.Join(cliLinkDirectory, "rzm")', 'filepath.Join(cliLinkDirectory, "rzm-update-proof")')
    change(source, 'pkg/app/desktop/cli_link.go', '"command -v rzm"', '"command -v rzm-update-proof"')
    change(source, 'pkg/app/update/app.go', 'https://github.com/atomicobject/rhizome/releases/latest/download/latest.json', ENDPOINT)
    change(source, 'pkg/vault/config/constants.go', 'RhizomeConfigDirectory                  = "rhizome"', 'RhizomeConfigDirectory                  = "rhizome-update-proof"')
    for file in ['pkg/repositorytrust/store.go', 'pkg/teamapi/session/store.go']:
        if (source/file).exists():
            change(source, file, '".config", "rhizome"', '".config", "rhizome-update-proof"')
    updater_files = ['desktop/src-tauri/src/updater.rs']
    if label != 'A':
        updater_files += ['desktop/src-tauri/src/updater/macos.rs', 'desktop/src-tauri/src/updater/archive.rs']
    hashes = {}
    for file in updater_files:
        actual = (source / file).read_bytes()
        expected = subprocess.check_output(['git', 'show', f'{commit}:{file}'], cwd=REPO)
        assert actual == expected, f'updater changed: {file}'
        hashes[file] = hashlib.sha256(actual).hexdigest()
    assert hashes == HASHES[commit], f'unexpected updater source for {label}'
    if label == 'A':
        assert not (source/'desktop/src-tauri/src/updater/macos.rs').exists()
    else:
        adapter = (source/'desktop/src-tauri/src/updater/macos.rs').read_text()
        assert 'anchor apple generic' in adapter and 'certificate leaf[subject.OU]' in adapter
    (ROOT/'configs'/f'{label}-isolation.patch').write_text(''.join(patches))
    (ROOT/'configs'/f'{label}.json').write_text(json.dumps(config,indent=2)+'\n')
    manifest['builds'][label] = {'commit': commit, 'version': version, 'source': f'sources/{label}', 'adapter': label != 'A', 'updaterHashes': hashes, 'description': 'Rebuilt v0.52.1 with test isolation; not shipped bytes' if label == 'A' else 'PR 145 adapter with synthetic test version', 'productionUpdaterChangedOnlyInExport': production_updater != config['plugins']['updater']}
(ROOT/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print('Prepared A/B/C source exports. Product checkout unchanged.')
