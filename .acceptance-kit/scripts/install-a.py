#!/usr/bin/env python3
"""Explicit, one-time setup of the disposable acceptance app. Never overwrites."""
import argparse
import json
import os
from pathlib import Path
import pwd
import subprocess
import tarfile
import tempfile
from build import ROOT, MANIFEST
from check import all_artifacts

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--ownership',choices=['root','user'],required=True)
p.add_argument('--quarantine',action='store_true')
a=p.parse_args()
assert os.geteuid()==0,'Run from the administrator account with sudo only after reviewing the command'
account=pwd.getpwnam('rzm-update-test')
assert account.pw_uid==503 and account.pw_gid==20
all_artifacts()
app=Path('/Applications')/(MANIFEST['name']+'.app')
link=Path(MANIFEST['cliLink'])
assert not app.exists() and not app.is_symlink(),'Refusing to overwrite an app'
assert not link.exists() and not link.is_symlink(),'Refusing to overwrite a CLI entry'
with tempfile.TemporaryDirectory(prefix='rzm-update-proof-install-') as temporary:
    with tarfile.open(ROOT/'artifacts/A/Rhizome-update-proof.app.tar.gz') as archive:
        archive.extractall(temporary, filter='data')
    subprocess.run(['/usr/bin/ditto',str(Path(temporary)/app.name),str(app)],check=True)
uid=0 if a.ownership=='root' else 503
subprocess.run(['/usr/sbin/chown','-R',f'{uid}:20',str(app)],check=True)
if a.quarantine:
    subprocess.run(['/usr/bin/xattr','-w','com.apple.quarantine','0081;00000000;RhizomeUpdateProof;',str(app)],check=True)
link.parent.mkdir(parents=True,exist_ok=True)
link.symlink_to(app/'Contents/MacOS/rzm')
session=ROOT/'sessions/503'
session.mkdir(parents=True,exist_ok=True)
os.chown(session,503,20)
session.chmod(0o700)
for name in ['desktop','fixtures']:
    child=session/name; child.mkdir(exist_ok=True); os.chown(child,503,20)
for name in ['repo-one','repo-two']:
    repo=session/'fixtures'/name
    repo.mkdir(exist_ok=True); os.chown(repo,503,20)
    note=repo/'README.md'; note.write_text('# '+name+'\n\nDisposable update acceptance fixture.\n'); os.chown(note,503,20)
    subprocess.run(['/usr/bin/sudo','-u','rzm-update-test','git','init','--quiet',str(repo)],check=True)
baseline={'ownership':a.ownership,'quarantine':a.quarantine,'linkInode':link.lstat().st_ino,'linkTarget':os.readlink(link),'appInode':app.stat().st_ino}
file=session/'baseline.json'; file.write_text(json.dumps(baseline,indent=2)+'\n'); os.chown(file,503,20)
print('Installed only the new disposable app and CLI entry. No GUI session action performed.')
