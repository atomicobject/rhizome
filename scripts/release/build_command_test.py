"""Build diagnostics are bounded and cannot be enabled for signing steps."""
from pathlib import Path
import subprocess
import tempfile
import unittest

from build_native_desktop import run
from desktop_candidates import CandidateError


class BuildCommandTests(unittest.TestCase):
    def test_failed_non_signing_build_retains_bounded_utf8_evidence_and_safe_tail(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evidence = root / 'evidence/goreleaser.log'
            def failed(command, **kwargs):
                self.assertEqual('utf-8', kwargs['encoding'])
                self.assertEqual('replace', kwargs['errors'])
                raise subprocess.CalledProcessError(1, command, output='prefix\n' * 5000,
                    stderr='\x1b[31m▶ configuration failed\x1b[0m\ntoken=synthetic-value\nhttps://user:password@example.test/path'.encode('utf-8'))
            with self.assertRaises(CandidateError) as raised:
                run(['goreleaser','build'], root=root, env={}, runner=failed, diagnostic_path=evidence)
            content = evidence.read_text(encoding='utf-8')
            self.assertIn('▶ configuration failed', content)
            self.assertIn('exit 1', content)
            self.assertNotIn('synthetic-value', content)
            self.assertNotIn('user:password', content)
            self.assertNotIn('\x1b', content)
            self.assertLess(len(content), 12500)
            self.assertLess(len(str(raised.exception)), 4300)

    def test_signing_outputs_stay_suppressed_and_diagnostics_refuse_signing_or_credentials(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evidence = root / 'not-written.log'
            calls = []
            def failed(command, **kwargs):
                calls.append(command)
                raise subprocess.CalledProcessError(1, command, stderr='synthetic-private-signing-value')
            with self.assertRaises(CandidateError) as raised:
                run(['node','tauri.js','bundle'], root=root, env={}, runner=failed)
            self.assertNotIn('synthetic-private-signing-value', str(raised.exception))
            for command, env in ((['node','tauri.js','signer','sign'], {}),
                                 (['node','tauri.js','bundle'], {}),
                                 (['goreleaser','build'], {'TAURI_SIGNING_PRIVATE_KEY':'synthetic-private-signing-value'})):
                with self.subTest(command=command), self.assertRaisesRegex(CandidateError, 'unavailable'):
                    run(command, root=root, env=env, runner=failed, diagnostic_path=evidence)
            self.assertEqual(1, len(calls))
            self.assertFalse(evidence.exists())


if __name__ == '__main__':
    unittest.main()
