import json
from pathlib import Path
import tempfile
import unittest

from desktop_candidates import CandidateError, sha256
from windows_sign import configuration, sign, signing_source


class WindowsSigningTests(unittest.TestCase):
    def test_final_canonical_payload_is_never_resigned_and_other_tools_receive_only_declared_credentials(self):
        with tempfile.TemporaryDirectory() as temporary:
            cli=Path(temporary)/'rzm.exe'; cli.write_bytes(b'final canonical CLI')
            config={'command':['signer','%1'],'environment':['WINDOWS_SIGN_TOKEN']}
            source={'PATH':'/bin','GITHUB_TOKEN':'synthetic-other','TAURI_SIGNING_PRIVATE_KEY':'synthetic-updater',
                    'WINDOWS_SIGNING_SECRET':json.dumps({'WINDOWS_SIGN_TOKEN':'synthetic-signer'})}
            calls=[]
            sign(cli,config,sha256(cli),source,runner=lambda *a,**k:calls.append((a,k)))
            self.assertEqual([],calls)
            sign(cli,config,None,source,runner=lambda *a,**k:calls.append((a,k)))
            env=calls[0][1]['env']
            self.assertEqual('synthetic-signer',env['WINDOWS_SIGN_TOKEN'])
            self.assertNotIn('WINDOWS_SIGNING_SECRET',env); self.assertNotIn('GITHUB_TOKEN',env)
            self.assertNotIn('TAURI_SIGNING_PRIVATE_KEY',env)
            source['WINDOWS_SIGNING_SECRET']=json.dumps({'GITHUB_TOKEN':'forbidden'})
            with self.assertRaisesRegex(CandidateError,'scope'):
                signing_source(config,source)


if __name__=='__main__': unittest.main()
