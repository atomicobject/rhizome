from pathlib import Path
import tempfile
import unittest

from desktop_candidates import CandidateError
from desktop_candidates_test import COMMIT, VERSION, make_candidate
from stage_native_cli import stage


class CanonicalCliTests(unittest.TestCase):
    def test_native_final_canonical_bytes_replace_only_selected_public_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            directory,_=make_candidate(root,'windows-x86_64')
            output=root/'cross-built.exe'; output.write_bytes(b'unsigned cross build')
            source={'RZM_RELEASE_DESKTOP':'1','RZM_DESKTOP_CANDIDATES_DIR':str(root)}
            self.assertTrue(stage(output,VERSION,'windows','amd64',False,source=source,commit=COMMIT))
            self.assertEqual((directory/'rzm.exe').read_bytes(),output.read_bytes())
            output.write_bytes(b'internal')
            self.assertFalse(stage(output,VERSION,'windows','amd64',True,source=source,commit=COMMIT))
            self.assertEqual(b'internal',output.read_bytes())
            with self.assertRaisesRegex(CandidateError,'version/source'):
                stage(output,VERSION,'windows','amd64',False,source=source,commit='b'*40)
            self.assertEqual(b'internal',output.read_bytes())

    def test_production_missing_preparation_fails_before_replacing_cross_build(self):
        with self.assertRaisesRegex(CandidateError,'preparation'):
            stage(Path('unused'),VERSION,'linux','amd64',False,source={'RZM_RELEASE_DESKTOP':'1'},commit=COMMIT)


if __name__=='__main__':
    unittest.main()
