from pathlib import Path
import unittest

from desktop_candidates import CandidateError
from desktop_candidates_test import COMMIT, VERSION
from desktop_source import validate_source
from release_gateway_source import SourceError


class SourceTests(unittest.TestCase):
    def runner(self,args):
        if args[0]=='git':
            if 'remote' in args: return 'https://github.com/atomicobject/rhizome.git\n'
            if 'rev-parse' in args: return COMMIT+'\n'
            if 'show' in args: return 'package version\nvar Version = "v'+VERSION+'"\n'
        import json
        endpoint=args[2]
        if endpoint=='repos/atomicobject/rhizome':
            return json.dumps({'full_name':'atomicobject/rhizome','private':False})
        if '/git/ref/heads/' in endpoint:
            return json.dumps({'object':{'type':'commit','sha':COMMIT}})
        raise AssertionError(args)

    def test_exact_canonical_source_version_required_before_native_build(self):
        self.assertEqual({'repository':'atomicobject/rhizome','source_commit':COMMIT,'version':VERSION},
                         validate_source(Path('/fixture'),COMMIT,'v'+VERSION,runner=self.runner))
        with self.assertRaisesRegex(CandidateError,'source version'):
            validate_source(Path('/fixture'),COMMIT,'9.9.9',runner=self.runner)
        with self.assertRaisesRegex(CandidateError,'canonical public release'):
            validate_source(Path('/fixture'),'b'*40,VERSION,runner=self.runner)


if __name__=='__main__': unittest.main()
