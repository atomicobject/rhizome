#!/usr/bin/env python3
"""Validate signed artifacts or an installed acceptance build, without launching it."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
from build import ROOT, MANIFEST, clean_env, check_app

os.umask(0o022)


def verify_signature(label):
    verifier=ROOT/'tools/rhizome-verify-updater'
    env=clean_env()
    assert verifier.is_file(), 'Missing public verifier executable'
    artifact=ROOT/'artifacts'/label/'Rhizome-update-proof.app.tar.gz'
    public=(ROOT/'keys/local/updater.pub').read_text().strip()
    signature=Path(str(artifact)+'.sig').read_text().strip()
    with tempfile.TemporaryDirectory(prefix='rzm-update-proof-check-') as tmp:
        pub=Path(tmp)/'public'; sig=Path(tmp)/'signature'
        pub.write_bytes(base64.b64decode(public,validate=True))
        sig.write_bytes(base64.b64decode(signature,validate=True))
        subprocess.run([str(verifier),str(pub),str(artifact),str(sig)],check=True,env=env,capture_output=True)


def artifact(label):
    row=MANIFEST['builds'][label]
    folder=ROOT/'artifacts'/label
    receipt=json.loads((folder/'receipt.json').read_text())
    assert receipt['archive']=='Rhizome-update-proof.app.tar.gz'
    archive=folder/receipt['archive']
    assert receipt['status']=='signed-notarized'
    assert receipt['version']==row['version'] and receipt['commit']==row['commit']
    assert hashlib.sha256(archive.read_bytes()).hexdigest()==receipt['sha256']
    verify_signature(label)
    with tempfile.TemporaryDirectory(prefix='rzm-update-proof-check-') as temp:
        with tarfile.open(archive) as tar:
            tar.extractall(temp,filter='data')
        assert sorted(p.name for p in Path(temp).iterdir())==[MANIFEST['name']+'.app']
        check_app(Path(temp)/(MANIFEST['name']+'.app'),row['version'],receipt['team'])
    return receipt


def all_artifacts():
    receipts={label:artifact(label) for label in 'ABC'}
    assert len({r['team'] for r in receipts.values()})==1
    return receipts


def installed(label):
    assert os.getuid()==503, 'Run installed checks as rzm-update-test'
    app=Path('/Applications')/(MANIFEST['name']+'.app')
    row=MANIFEST['builds'][label]
    receipt=json.loads((ROOT/'artifacts'/label/'receipt.json').read_text())
    check_app(app,row['version'],receipt['team'])
    link=Path(MANIFEST['cliLink'])
    assert link.is_symlink() and str(link.resolve())==str(app/'Contents/MacOS/rzm')
    if label in 'BC':
        assert app.stat().st_uid==503 and (app/'Contents').stat().st_uid==503
        assert os.access(app,os.W_OK) and os.access(app/'Contents',os.W_OK)
    baseline=json.loads((ROOT/'sessions/503/baseline.json').read_text())
    assert link.lstat().st_ino==baseline['linkInode'] and os.readlink(link)==baseline['linkTarget']
    env=clean_env()
    result=subprocess.check_output([str(link),'--version'],cwd=ROOT, text=True,env=env).strip()
    assert result.split()[-1].lstrip('v')==row['version']
    evidence={'build':label,'version':row['version'],'appUid':app.stat().st_uid,'contentsUid':(app/'Contents').stat().st_uid,'appInode':app.stat().st_ino,'linkInode':link.lstat().st_ino,'cliVersion':result,'guiPromptsAndWindows':'must be recorded by the human'}
    (ROOT/f'sessions/503/checked-{label}.json').write_text(json.dumps(evidence,indent=2)+'\n')
    return evidence


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('target',choices=['all','A','B','C','installed'])
    p.add_argument('label',nargs='?',choices=['A','B','C'])
    args=p.parse_args()
    try:
        result=installed(args.label) if args.target=='installed' and args.label else all_artifacts() if args.target=='all' else artifact(args.target)
        print(json.dumps(result,indent=2))
    except Exception as e:
        raise SystemExit('NOT READY: '+str(e))
