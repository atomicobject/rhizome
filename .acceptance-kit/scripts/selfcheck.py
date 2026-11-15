#!/usr/bin/env python3
"""Check isolation, source pinning, feed routing, and fail-closed readiness."""
import hashlib
import json
import subprocess
import threading
from urllib.error import HTTPError
from urllib.request import urlopen
from build import ROOT, MANIFEST
import feed

for label,row in MANIFEST['builds'].items():
    source=ROOT/row['source']
    config=json.loads((source/'desktop/src-tauri/tauri.conf.json').read_text())
    assert config['identifier']==MANIFEST['identifier']
    assert config['productName']==MANIFEST['name'] and config['version']==row['version']
    assert config['plugins']['updater']['endpoints']==[MANIFEST['endpoint']]
    assert config['plugins']['updater']['dangerousInsecureTransportProtocol'] is True
    assert config['plugins']['updater']['pubkey']==(ROOT/'keys/local/updater.pub').read_text().strip()
    for name,expected in row['updaterHashes'].items():
        assert hashlib.sha256((source/name).read_bytes()).hexdigest()==expected
    assert '"rzm-update-proof"' in (source/'pkg/app/desktop/cli_link.go').read_text()
    assert 'com.atomicobject.rhizome.update-proof' in (source/'pkg/app/appbundle/bundle.go').read_text()
    assert MANIFEST['endpoint'] in (source/'pkg/app/update/app.go').read_text()
    assert '"rhizome-update-proof"' in (source/'pkg/vault/config/constants.go').read_text()
assert MANIFEST['builds']['B']['updaterHashes']==MANIFEST['builds']['C']['updaterHashes']
from tempfile import TemporaryDirectory
from pathlib import Path
with TemporaryDirectory() as temp:
    fake=Path(temp); (fake/'feed').mkdir()
    (fake/'feed/selected.json').write_text('{"test":"synthetic"}')
    package=fake/'artifacts/B/Rhizome-update-proof.app.tar.gz'
    package.parent.mkdir(parents=True); package.write_bytes(b'synthetic self-check bytes')
    original=feed.ROOT; feed.ROOT=fake
    server=feed.ThreadingHTTPServer(('127.0.0.1',0),feed.Handler)
    worker=threading.Thread(target=server.serve_forever,daemon=True); worker.start()
    base=f'http://127.0.0.1:{server.server_port}'
    assert json.load(urlopen(base+'/latest.json'))=={'test':'synthetic'}
    assert urlopen(base+'/artifacts/B/Rhizome-update-proof.app.tar.gz').read()==package.read_bytes()
    for path in ['/../manifest.json','/keys/local/updater','/artifacts/A/../../keys/local/updater']:
        try: urlopen(base+path)
        except HTTPError as error: assert error.code==404
        else: raise AssertionError('feed exposed a non-artifact path')
    server.shutdown(); server.server_close(); worker.join(); feed.ROOT=original
if not (ROOT/'artifacts/A/receipt.json').exists():
    result=subprocess.run(['python3',str(ROOT/'scripts/check.py'),'all'],capture_output=True,text=True)
    assert result.returncode!=0 and 'NOT READY' in result.stdout+result.stderr
print('PASS source isolation, unchanged adapters, synthetic HTTP routing, and missing-signature refusal.')
