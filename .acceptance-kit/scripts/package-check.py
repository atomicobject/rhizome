#!/usr/bin/env python3
"""Check Tauri packaging shape without signing or treating it as GUI proof."""
import plistlib
import shutil
import subprocess
from build import ROOT, MANIFEST, clean_env, run

for label,row in MANIFEST['builds'].items():
    source=ROOT/row['source']
    compiled=ROOT/'compiled'/label
    shutil.copy2(compiled/'rhizome-desktop',ROOT/'cache/rust/release/rhizome-desktop')
    shutil.copy2(compiled/'rzm',source/'desktop/src-tauri/binaries/rzm-aarch64-apple-darwin')
    run(['npm','run','tauri','--','bundle','--bundles','app','--no-sign'],source/'desktop',clean_env(),label+'-package-check')
    app=ROOT/'cache/rust/release/bundle/macos'/(MANIFEST['name']+'.app')
    info=plistlib.loads((app/'Contents/Info.plist').read_bytes())
    assert info['CFBundleIdentifier']==MANIFEST['identifier'] and info['CFBundleShortVersionString']==row['version']
    assert sorted(p.name for p in app.iterdir())==['Contents']
    assert (app/'Contents/MacOS/rzm').read_bytes()==(compiled/'rzm').read_bytes()
    output=compiled/app.name
    assert not output.exists(),'Refusing to replace existing package check'
    subprocess.run(['/usr/bin/ditto',str(app),str(output)],check=True,env=clean_env())
    print(label+' package shape passed. Unsigned package, not GUI proof.',flush=True)
