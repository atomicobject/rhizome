import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from assemble_desktop import assemble
from desktop_candidates import MANIFEST, REQUIRED_TARGETS
from desktop_candidates_test import COMMIT, VERSION, make_candidate
import join_desktop
from release_gateway_runs import REPOSITORY


class JoinTests(unittest.TestCase):
    def test_mac_signed_sidecar_hash_is_separate_and_join_requires_all_native_platforms(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); prepared=root/'native'; prepared.mkdir(); output=root/'extra'; output.mkdir()
            for target in REQUIRED_TARGETS-{'darwin-aarch64'}:
                make_candidate(prepared,target)
            mac,_=make_candidate(root,'darwin-aarch64')
            archive=mac/'Rhizome-macos-arm64.app.tar.gz'
            shutil.copyfile(archive,output/archive.name)
            (output/(archive.name+'.sig')).write_text('existing Mac signature')
            from desktop_candidates import sha256
            files = {path.relative_to(prepared).as_posix(): sha256(path) for path in prepared.rglob('*') if path.is_file()}
            (prepared / 'native-inputs.json').write_text(json.dumps({'source_commit':COMMIT,'version':'v'+VERSION,'candidate_only':False,'files':files}))
            config=root/'desktop/src-tauri/tauri.conf.json'; config.parent.mkdir(parents=True)
            config.write_text('{"plugins":{"updater":{"pubkey":"cHVibGlj"}}}')
            calls=[]
            def runner(command,**kwargs):
                calls.append(command)
                if command[0]=='git': return subprocess.CompletedProcess(command,0,COMMIT+'\n','')
                if 'signer' in command:
                    Path(command[-1]+'.sig').write_text('synthetic native signature')
                return subprocess.CompletedProcess(command,0,'synthetic toolchain','')
            def assembled(*args,**kwargs):
                return assemble(*args,**kwargs,verifier=lambda *a,**k:None)
            source={'RZM_DESKTOP_CANDIDATES_DIR':str(prepared),
                    'TAURI_SIGNING_PRIVATE_KEY':'synthetic'}
            with patch.object(join_desktop,'assemble',side_effect=assembled):
                paths=join_desktop.join(root,mac/'rzm',VERSION,output,source=source,runner=runner)
            self.assertEqual('latest.json',paths[-1].name)
            feed=json.loads((output/'latest.json').read_text()); self.assertEqual(REQUIRED_TARGETS,set(feed['platforms']))
            self.assertEqual('existing Mac signature',feed['platforms']['darwin-aarch64']['signature'])
            manifest=json.loads((output/'desktop-assembly.json').read_text())['candidates']['darwin-aarch64']
            self.assertNotEqual(manifest['canonicalCliSha256'],manifest['packagedCliSha256'])
            # The Mac archive arrives signed; every other target is signed once.
            self.assertEqual(len(REQUIRED_TARGETS)-1,sum('signer' in command for command in calls))
            shutil.rmtree(prepared/'windows-x86_64')
            with self.assertRaises((OSError, ValueError)):
                join_desktop.join(root,mac/'rzm',VERSION,output,source=source,runner=runner)


if __name__=='__main__': unittest.main()
