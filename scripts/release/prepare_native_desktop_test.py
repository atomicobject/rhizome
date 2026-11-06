"""Native preparation binds exact producer runs, immutable artifacts and retained bytes."""
import copy
import json
import io
import hashlib
from pathlib import Path
import tempfile
import unittest
import zipfile
from unittest.mock import patch

from desktop_candidates import CandidateError, MANIFEST, NATIVE_TARGETS
from desktop_candidates_test import COMMIT, VERSION, make_candidate
import prepare_native_desktop as native
from release_gateway_runs import GatewayError, REPOSITORY
from release_gateway_test import Clock

SOURCE = {'repository': REPOSITORY, 'source_commit': COMMIT, 'version': VERSION}


class GitHub:
    def __init__(self):
        self.runs, self.calls = [], []
        self.uncertain, self.create = False, True
        self.artifacts = [{'id': 40+i, 'name': native.ARTIFACT_NAMES[target], 'digest': 'sha256:'+'d'*64,
                           'expired': False, 'workflow_run': {'id': 100}}
                          for i, target in enumerate(sorted(NATIVE_TARGETS))]

    def __call__(self, argv):
        self.calls.append(tuple(argv))
        if tuple(argv[:3]) == ('gh', 'workflow', 'run'):
            request = next(value.split('=', 1)[1] for value in argv if value.startswith('request_id='))
            if self.create:
                self.runs.append({'id':100, 'workflow_id':8, 'path':'.github/workflows/'+native.WORKFLOW,
                    'repository':{'full_name':REPOSITORY}, 'event':'workflow_dispatch', 'head_branch':'main',
                    'display_title':native.title({'request_id':request}, SOURCE),
                    'html_url':f'https://github.com/{REPOSITORY}/actions/runs/100',
                    'status':'completed', 'conclusion':'success'})
            if self.uncertain:
                raise RuntimeError('response lost after acceptance')
            return ''
        path = argv[2]
        if path.endswith('/artifacts?per_page=100'):
            return json.dumps([{'total_count':len(self.artifacts), 'artifacts':self.artifacts}])
        if '/runs?' in path:
            return json.dumps([{'total_count':len(self.runs), 'workflow_runs':self.runs}])
        if '/workflows/' in path:
            return json.dumps({'id':8, 'path':'.github/workflows/'+native.WORKFLOW})
        if path.endswith('/runs/100'):
            return json.dumps(self.runs[-1])
        raise AssertionError(argv)

    @property
    def dispatches(self):
        return [call for call in self.calls if call[:3] == ('gh', 'workflow', 'run')]


class PreparationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.receipt, self.output = self.root/'receipt.json', self.root/'native'
        self.github, self.clock = GitHub(), Clock()
        self.sources = []

    def validator(self, *args, **kwargs):
        self.sources.append(1)
        return copy.deepcopy(SOURCE)

    def downloader(self, artifact, output):
        target = next(target for target, item in native.artifact_listing(self.github,100).items()
                      if item['id'] == artifact['id'])
        make_candidate(output.parent, target)

    def ensure(self):
        return native.ensure_candidates(self.root, COMMIT, VERSION, self.receipt, self.output,
            runner=self.github, validator=self.validator, downloader=self.downloader,
            clock=self.clock, sleep=self.clock.sleep, output_fn=lambda _:None)

    def verify(self):
        return native.verify_prepared(self.root, COMMIT, VERSION, self.receipt, self.output,
                                     runner=self.github, validator=self.validator)

    def test_exact_dispatch_download_resume_and_publication_recheck(self):
        self.github.uncertain = True
        self.assertEqual(self.ensure(), self.output)
        saved = json.loads(self.receipt.read_text())
        self.assertEqual(saved['attempt']['state'], 'success')
        self.assertEqual(set(saved['files']), NATIVE_TARGETS)
        self.assertEqual(len(self.sources), 2)
        self.ensure()
        self.verify()
        self.assertEqual(len(self.github.dispatches), 1)
        self.assertIn(f'version={VERSION}', self.github.dispatches[0])
        self.assertIn(f'source_commit={COMMIT}', self.github.dispatches[0])

    def test_unknown_dispatch_never_silently_redispatches(self):
        self.github.uncertain, self.github.create = True, False
        with patch.object(native,'DISCOVERY_SECONDS', 15):
            for _ in range(2):
                with self.assertRaisesRegex(CandidateError,'uncertain'):
                    self.ensure()
        self.assertEqual(len(self.github.dispatches),1)

    def test_listing_collision_or_wrong_ref_rejected_before_download(self):
        self.ensure()
        for change in ({'head_branch':'release'}, {'event':'push'}, {'workflow_id':9}):
            original=copy.deepcopy(self.github.runs)
            self.github.runs[-1].update(change)
            with self.assertRaises(GatewayError):
                self.verify()
            self.github.runs=original
        self.github.runs.append(copy.deepcopy(self.github.runs[-1]))
        with self.assertRaisesRegex(GatewayError,'Multiple'):
            self.verify()

    def test_changed_or_duplicate_artifacts_and_retained_file_tampering_rejected(self):
        self.ensure()
        original=copy.deepcopy(self.github.artifacts)
        self.github.artifacts[0]['digest']='sha256:'+'e'*64
        with self.assertRaisesRegex(CandidateError,'identities'):
            self.verify()
        self.github.artifacts=original+[copy.deepcopy(original[0])]
        with self.assertRaisesRegex(CandidateError,'exactly one'):
            self.verify()
        self.github.artifacts=original
        path=self.output/'linux-x86_64'/MANIFEST
        content=json.loads(path.read_text()); content['toolchains']['go']='changed'; path.write_text(json.dumps(content))
        with self.assertRaisesRegex(CandidateError,'changed'):
            self.verify()

    def test_source_revalidated_after_wait_and_no_complete_receipt_on_change(self):
        def changed(*args,**kwargs):
            result=self.validator()
            if len(self.sources)>1:
                result['source_commit']='b'*40
            return result
        with self.assertRaisesRegex(CandidateError,'changed while waiting'):
            native.ensure_candidates(self.root,COMMIT,VERSION,self.receipt,self.output,runner=self.github,
                validator=changed,downloader=self.downloader,clock=self.clock,sleep=self.clock.sleep,output_fn=lambda _:None)
        self.assertEqual(json.loads(self.receipt.read_text())['attempt']['state'],'verified')

    def test_exact_zip_digest_checked_before_extracting_and_unsafe_paths_refused(self):
        for filename in ('../escape', 'C:/escape', 'folder\\escape', '/escape'):
            with self.subTest(filename=filename):
                buffer=io.BytesIO()
                with zipfile.ZipFile(buffer,'w') as archive:
                    archive.writestr(filename,b'synthetic')
                payload=buffer.getvalue()
                artifact={'id':42,'digest':'sha256:'+hashlib.sha256(payload).hexdigest(),'expired':False}
                def fetch(_command,**kwargs):
                    kwargs['stdout'].write(payload)
                with patch.object(native.subprocess,'run',side_effect=fetch):
                    with self.assertRaisesRegex(CandidateError,'unsafe'):
                        native.download(artifact,self.root/'download')
        artifact['digest']='sha256:'+'0'*64
        with patch.object(native.subprocess,'run',side_effect=fetch):
            with self.assertRaisesRegex(CandidateError,'digest'):
                native.download(artifact,self.root/'download')


if __name__ == '__main__':
    unittest.main()
