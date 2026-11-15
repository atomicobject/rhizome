#!/usr/bin/env python3
"""Compile isolated test exports, then optionally sign and notarize them."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = json.loads((ROOT/'manifest.json').read_text())
BUILD_ENV_NAMES = ('PATH','HOME','TMPDIR','DEVELOPER_DIR','SDKROOT','CARGO_HOME','RUSTUP_HOME','GOPATH','GOMODCACHE','GOCACHE')

def clean_env():
    env = {k: os.environ[k] for k in BUILD_ENV_NAMES if k in os.environ}
    env['PATH'] = str(Path.home()/'.cargo/bin') + os.pathsep + env.get('PATH','/usr/bin:/bin')
    env['GOCACHE'] = str(ROOT/'cache/go')
    env['CARGO_TARGET_DIR'] = str(ROOT/'cache/rust')
    env['CI'] = 'true'
    env['GOENV'] = 'off'
    env['PYTHONDONTWRITEBYTECODE'] = '1'
    env['RZM_SKIP_REPO_DELEGATE'] = '1'
    return env


def run(command, cwd, env, label):
    log = ROOT/'evidence'/f'{label}.log'
    with log.open('ab') as out:
        result = subprocess.run(command, cwd=cwd, env=env, stdout=out, stderr=subprocess.STDOUT)
    if result.returncode:
        raise RuntimeError(f'{label} failed, inspect {log}')


def compile_one(label):
    row = MANIFEST['builds'][label]
    source = ROOT/row['source']
    env = clean_env()
    for folder in ['desktop', 'web']:
        if not (source/folder/'node_modules').exists():
            run(['npm','ci','--no-audit','--no-fund'], source/folder, env, label+'-npm-'+folder)
    run(['bash','scripts/vendor/ensure_tree_sitter_sources.sh'], source, env, label+'-vendor')
    run(['npm','run','generate:api'], source/'web',env,label+'-web-api')
    run(['npm','run','build'], source/'web',env,label+'-web')
    run(['npm','run','build'],source/'desktop',env,label+'-desktop')
    output=source/'desktop/src-tauri/binaries/rzm-aarch64-apple-darwin'
    output.parent.mkdir(exist_ok=True)
    flags = f'-s -w -X github.com/atomicobject/rhizome/pkg/vault/version.Version={row["version"]} -X github.com/atomicobject/rhizome/pkg/vault/version.Channel=release -X github.com/atomicobject/rhizome/pkg/teamapi/session.DefaultOrigin=http://127.0.0.1:19453'
    run(['go','build','-mod=vendor','-tags=fts5','-trimpath','-ldflags',flags,'-o',str(output),'.'],source,env,label+'-go')
    run(['npm','run','tauri','--','build','--no-bundle','--','--locked'],source/'desktop',env,label+'-rust')
    target = ROOT/'compiled'/label
    target.mkdir(parents=True,exist_ok=True)
    shutil.copy2(ROOT/'cache/rust/release/rhizome-desktop',target/'rhizome-desktop')
    shutil.copy2(output,target/'rzm')
    (target/'status.json').write_text(json.dumps({'status':'compiled-only-not-GUI-proof','version':row['version'],'commit':row['commit']},indent=2)+'\n')
    print(f'{label} compiled; not signed, notarized, or ready for GUI proof.',flush=True)


def sign_one(label):
    row=MANIFEST['builds'][label]
    source=ROOT/row['source']
    sys.path.insert(0,str(ROOT/'sources/B/scripts/release'))
    from apple_signing import require_credentials, credentials
    required=('APPLE_SIGNING_IDENTITY','APPLE_API_KEY','APPLE_API_ISSUER')
    require_credentials(os.environ)
    identity=os.environ['APPLE_SIGNING_IDENTITY']
    team=identity.rsplit('(',1)[1].rstrip(')')
    assert team==MANIFEST['expectedTeam'], 'Signing team differs from the public reference'
    identities=subprocess.check_output(['/usr/bin/security','find-identity','-v','-p','codesigning'],text=True,env=clean_env())
    if identity not in identities:
        raise RuntimeError('Requested Developer ID identity is not installed; no import attempted')
    assert (ROOT/'compiled'/label/'rhizome-desktop').exists(), 'Compile this build first'
    env=clean_env()
    for name in (*required,'APPLE_API_PRIVATE_KEY','APPLE_API_KEY_PATH'):
        if name in os.environ: env[name]=os.environ[name]
    shutil.copy2(ROOT/'compiled'/label/'rhizome-desktop',ROOT/'cache/rust/release/rhizome-desktop')
    shutil.copy2(ROOT/'compiled'/label/'rzm',source/'desktop/src-tauri/binaries/rzm-aarch64-apple-darwin')
    # Bundling logs may contain signing diagnostics. Keep them private and suppress failures.
    with credentials(env) as signing_env:
        result=subprocess.run(['npm','run','tauri','--','bundle','--bundles','app'],cwd=source/'desktop',env=signing_env,capture_output=True,timeout=3600)
        if result.returncode: raise RuntimeError(f'{label} signing or notarization failed; secret-bearing diagnostics suppressed')
    app=ROOT/'cache/rust/release/bundle/macos'/f'{MANIFEST["name"]}.app'
    target=ROOT/'artifacts'/label
    assert not target.exists(), 'Refusing to overwrite signed artifacts'
    target.mkdir()
    subprocess.run(['/usr/bin/ditto',str(app),str(target/app.name)],check=True)
    app=target/app.name
    check_app(app,row['version'],team)
    archive=target/'Rhizome-update-proof.app.tar.gz'
    subprocess.run(['/usr/bin/tar','--disable-copyfile','-czf',str(archive),'-C',str(target),app.name],check=True,env=clean_env())
    result=subprocess.run(['npm','run','tauri','--','signer','sign',str(archive)],cwd=source/'desktop',env={**clean_env(),'TAURI_SIGNING_PRIVATE_KEY_PATH':str(ROOT/'keys/local/updater'),'TAURI_SIGNING_PRIVATE_KEY_PASSWORD':''},capture_output=True)
    if result.returncode: raise RuntimeError('Test updater signing failed; diagnostics suppressed')
    receipt={'status':'signed-notarized','commit':row['commit'],'version':row['version'],'team':team,'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'archive':archive.name}
    (target/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    print(f'{label} signed and notarized. Run check.py all before serving.',flush=True)


def check_app(app,version,team):
    import plistlib
    info=plistlib.loads((app/'Contents/Info.plist').read_bytes())
    assert info['CFBundleIdentifier']==MANIFEST['identifier']
    assert info['CFBundleShortVersionString']==version
    assert team==MANIFEST['expectedTeam'], 'Signing team differs from the public reference'
    requirement=f'anchor apple generic and identifier "{MANIFEST["identifier"]}" and certificate leaf[subject.OU] = "{team}"'
    env=clean_env()
    for args in [['codesign','--verify','--deep','--strict','-R','='+requirement,str(app)],['xcrun','stapler','validate',str(app)],['spctl','--assess','--type','execute','--verbose=2',str(app)]]:
        subprocess.run(args,check=True,env=env,capture_output=True)
    for executable in [app, app/'Contents/MacOS/rzm']:
        result=subprocess.run(['codesign','-dv','--verbose=4',str(executable)],env=env,capture_output=True,text=True,check=True)
        assert 'TeamIdentifier='+team+'\n' in result.stderr
        assert re.search(r'flags=.*\bruntime\b',result.stderr)
    result=subprocess.check_output([str(app/'Contents/MacOS/rzm'),'--version'],cwd=ROOT,env=env,text=True)
    assert result.strip().split()[-1].lstrip('v')==version


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('operation',choices=['compile','sign'])
    p.add_argument('label',choices=['A','B','C','all'])
    a=p.parse_args()
    try:
        for label in ('ABC' if a.label=='all' else a.label):
            (compile_one if a.operation=='compile' else sign_one)(label)
    except Exception as e:
        print(str(e),file=sys.stderr)
        sys.exit(1)
