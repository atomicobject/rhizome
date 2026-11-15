#!/usr/bin/env python3
"""Launch only the disposable app after the human switches to the test account."""
import argparse
import os
from pathlib import Path
import pwd
import subprocess
from build import ROOT, MANIFEST, clean_env

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('mode',choices=['gui','cli-update'])
a=p.parse_args()
assert os.getuid()==503 and pwd.getpwuid(os.getuid()).pw_name=='rzm-update-test','Fast User Switch to rzm-update-test yourself before running this command'
assert Path('/dev/console').stat().st_uid==503,'The test account must own the real GUI console'
assert not subprocess.check_output(['id','-Gn'],text=True).split().count('admin'),'Test account must remain standard'
app=Path('/Applications')/(MANIFEST['name']+'.app')
assert app.exists() and Path(MANIFEST['cliLink']).is_symlink()
env=clean_env()
env['RZM_SKIP_REPO_DELEGATE']='1'
if a.mode=='gui':
    subprocess.Popen([str(app/'Contents/MacOS/rhizome-desktop')],env=env,cwd=ROOT/'sessions/503',start_new_session=True)
else:
    subprocess.run([MANIFEST['cliLink'],'update','--yes'],env=env,cwd=ROOT/'sessions/503',check=True)
