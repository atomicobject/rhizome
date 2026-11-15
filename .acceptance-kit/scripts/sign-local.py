#!/usr/bin/env python3
"""Use the existing release credential runner. Never imports an identity."""
import os
from pathlib import Path
import subprocess
import sys

root=Path(__file__).resolve().parents[1]
if len(sys.argv) != 2:
    raise SystemExit('usage: sign-local.py <repository-with-release-credential-runner>')
source=Path(sys.argv[1]).resolve()
runner=source/'scripts/secrets/run.py'
result=subprocess.run([sys.executable,str(runner),'release',sys.executable,str(root/'scripts/build.py'),'sign','all'],env=dict(os.environ))
raise SystemExit(result.returncode)
