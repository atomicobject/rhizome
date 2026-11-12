import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from build_upgrade_fixture import VERSIONS, build_fixture
from desktop_candidates import MANIFEST, ROOT


class FixtureTests(unittest.TestCase):
    def test_two_versions_share_test_key_and_only_public_fixture_material_is_retained(self):
        calls=[]
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            def runner(command,**kwargs):
                calls.append((command,kwargs))
                if command[0]=='git':
                    return subprocess.CompletedProcess(command,0,'a'*40+'\n','')
                if command[0]=='node':
                    self.assertIn('desktopTeamAPIOrigin(process.env,releaseTeamAPIOrigin)', command[-1])
                    return subprocess.CompletedProcess(command,0,
                        kwargs['env']['RZM_DESKTOP_TEAM_API_ORIGIN'] or 'https://canonical.example.test','')
                self.assertNotIn('GITHUB_TOKEN',kwargs['env'])
                self.assertNotIn('TAURI_SIGNING_PRIVATE_KEY',kwargs['env'])
                if command[0]=='goreleaser':
                    self.assertNotIn('TARGET', kwargs['env'])
                    self.assertEqual(('windows', 'amd64'), (kwargs['env']['GOOS'], kwargs['env']['GOARCH']))
                    config=Path(command[command.index('--config')+1]).read_text(encoding='utf-8')
                    self.assertEqual(4, config.count('DefaultOrigin=https://canonical.example.test'))
                Path(command[-1]).write_bytes(command[-1].encode())
                return subprocess.CompletedProcess(command,0,'','')
            def key(directory,endpoint,**kwargs):
                directory.mkdir(); (directory/'updater').write_text('synthetic private fixture')
                overlay=directory/'overlay.json'
                overlay.write_text(json.dumps({'plugins':{'updater':{'endpoints':[endpoint], 'pubkey':'cHVibGlj',
                                                                  'dangerousInsecureTransportProtocol':True}}}))
                return overlay
            def builder(cli,version,commit,target,output,**kwargs):
                self.assertEqual('a'*40,commit)
                self.assertIsNotNone(kwargs['test_overlay'])
                self.assertEqual('windows-x86_64', kwargs['source']['TARGET'])
                output.mkdir(); (output/MANIFEST).write_text(json.dumps({'version':version}))
            def assembler(candidates,version,commit,hashes,output,**kwargs):
                self.assertFalse(kwargs['production'])
                self.assertEqual({'windows-x86_64'},kwargs['required_targets'])
                self.assertEqual('cHVibGlj',kwargs['test_public_key'])
                self.assertTrue(Path(kwargs['source']['TAURI_SIGNING_PRIVATE_KEY_PATH']).exists())
                output.mkdir(); feed=output/'latest.json'; feed.write_text(json.dumps({'version':version})); return feed
            output=build_fixture('windows-x86_64',root/'fixture',root=ROOT,
                source={'PATH':'/bin','GITHUB_TOKEN':'synthetic','TAURI_SIGNING_PRIVATE_KEY':'synthetic',
                        'GORELEASER_CURRENT_TAG':'v99.0.0', 'TARGET':'windows-x86_64'},
                runner=runner,builder=builder,assembler=assembler,prepare_key=key)
            self.assertEqual(VERSIONS[-1],json.loads((output/'latest.json').read_text())['version'])
            self.assertEqual(set(VERSIONS),{path.name for path in output.iterdir() if path.is_dir()})
            self.assertFalse(any('synthetic private fixture' in path.read_text() for path in output.rglob('*') if path.is_file()))
            self.assertIn('Team API origin: https://canonical.example.test.', (output/'EVIDENCE.txt').read_text())
            self.assertEqual(list(VERSIONS),
                [kwargs['env']['RZM_CI_RELEASE_VERSION'] for command,kwargs in calls if command[0]=='goreleaser'])
            for command, kwargs in calls:
                if command[0] == 'goreleaser':
                    self.assertIn('--snapshot', command)
                    self.assertNotIn('GORELEASER_CURRENT_TAG', kwargs['env'])


if __name__=='__main__':
    unittest.main()
