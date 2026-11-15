#!/usr/bin/env python3
"""Prove the fixture key verifies and rejects tampered bytes. No GUI claim."""
import base64
from pathlib import Path
import subprocess
import tempfile
from build import ROOT, clean_env

source=ROOT/'sources/B'
env=clean_env()
verifier=ROOT/'tools/rhizome-verify-updater'
with tempfile.TemporaryDirectory(prefix='signature-selfcheck-',dir=ROOT/'cache') as temp:
    temp=Path(temp); probe=temp/'probe';probe.write_bytes(b'Isolated updater signature self-check\n')
    result=subprocess.run(['npm','run','tauri','--','signer','sign',str(probe)],cwd=source/'desktop',env={**env,'TAURI_SIGNING_PRIVATE_KEY_PATH':str(ROOT/'keys/local/updater'),'TAURI_SIGNING_PRIVATE_KEY_PASSWORD':''},capture_output=True)
    assert result.returncode==0,'Fixture key signing failed; diagnostics suppressed'
    public=temp/'key.pub'; public.write_bytes(base64.b64decode((ROOT/'keys/local/updater.pub').read_text(),validate=True))
    signature=temp/'sig';signature.write_bytes(base64.b64decode(probe.with_suffix('.sig').read_text(),validate=True))
    args=[str(verifier),str(public),str(probe),str(signature)]
    assert subprocess.run(args,capture_output=True,env=env).returncode==0
    probe.write_bytes(b'Tampered bytes\n')
    assert subprocess.run(args,capture_output=True,env=env).returncode!=0
print('PASS real fixture signature verification and tampered-byte rejection. Not GUI acceptance.')
