#!/usr/bin/env python3
"""Exercise signing handoff and public packaging with synthetic inputs only."""
import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import runpy
import sys
import tarfile
import tempfile
import types
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO/'scripts/release'))
from ci_desktop_signing import prepare, cleanup
from ci_desktop_signing_test import Runner

spec = importlib.util.spec_from_file_location('public_bundle', Path(__file__).with_name('public-bundle.py'))
public_bundle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(public_bundle)


class AcceptanceCI(unittest.TestCase):
    def test_compile_environment_excludes_credentials_and_persisted_go_settings(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root/'manifest.json').write_text('{}')
            with patch.dict(os.environ, {'APPLE_API_PRIVATE_KEY': 'synthetic', 'GITHUB_TOKEN': 'synthetic',
                                         'TAURI_SIGNING_PRIVATE_KEY': 'synthetic', 'GOFLAGS': '-overlay=synthetic'}, clear=True):
                code = compile(Path(__file__).with_name('build.py').read_bytes(), 'build.py', 'exec')
                module = {'__file__': str(root/'scripts/build.py'), '__name__': 'acceptance_build'}
                exec(code, module)
                env = module['clean_env']()
            self.assertFalse({'APPLE_API_PRIVATE_KEY', 'GITHUB_TOKEN', 'TAURI_SIGNING_PRIVATE_KEY', 'GOFLAGS'} & env.keys())
            self.assertEqual(env['GOENV'], 'off')

    def test_helper_exports_path_consumed_by_signing_step(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            environment = {
                'RUNNER_TEMP': str(root), 'GITHUB_ENV': str(root/'github-env'),
                'APPLE_CERTIFICATE': base64.b64encode(b'synthetic-p12').decode(),
                'APPLE_CERTIFICATE_PASSWORD': 'synthetic-password',
                'APPLE_API_PRIVATE_KEY': 'synthetic-p8',
            }
            runner = Runner()
            try:
                prepare(environment, runner=runner, download=lambda: b'synthetic-intermediate')
                exported = dict(line.split('=', 1) for line in (root/'github-env').read_text().splitlines())
                self.assertEqual(set(exported), {'APPLE_API_KEY_PATH'})
                self.assertTrue(Path(exported['APPLE_API_KEY_PATH']).is_file())
                called = []
                build = types.ModuleType('build')
                build.sign_one = called.append
                with patch.dict(sys.modules, {'build': build}), patch.dict(os.environ, exported, clear=True):
                    runpy.run_path(str(Path(__file__).with_name('sign-ci.py')), run_name='__main__')
                self.assertEqual(called, list('ABC'))
            finally:
                cleanup(environment, runner=runner)
            self.assertFalse((root/'rhizome-desktop-signing').exists())

    def test_signing_requires_helper_export(self):
        build = types.ModuleType('build')
        build.sign_one = lambda label: self.fail('Signed without the helper export')
        with patch.dict(sys.modules, {'build': build}), patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(SystemExit):
                runpy.run_path(str(Path(__file__).with_name('sign-ci.py')), run_name='__main__')

    def test_public_bundle_excludes_private_trees_and_preserves_modes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)/'acceptance'
            for name in (*public_bundle.FILES, 'keys/local/updater', 'sources/private', 'cache/private', 'evidence/private'):
                path = root/name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'synthetic public file' if name in public_bundle.FILES else b'synthetic private file')
                path.chmod(0o755 if name.startswith('tools/') else 0o600)
            output = Path(temporary)/'public.tar.gz'
            public_bundle.bundle(root, output)
            with tarfile.open(output) as archive:
                self.assertEqual(archive.getnames(), list(public_bundle.FILES))
                archive.extractall(Path(temporary)/'unpacked', filter='data')
            self.assertEqual((Path(temporary)/'unpacked/tools/rhizome-verify-updater').stat().st_mode & 0o777, 0o755)
            self.assertEqual((Path(temporary)/'unpacked/manifest.json').stat().st_mode & 0o777, 0o644)
            link = root/public_bundle.FILES[0]
            link.unlink()
            link.symlink_to(root/'keys/local/updater')
            with self.assertRaisesRegex(AssertionError, 'regular public file'):
                public_bundle.bundle(root, Path(temporary)/'rejected.tar.gz')

    def test_tar_filter_rejects_traversal_and_external_symlinks(self):
        for name, link in (('../escape', None), ('app/link', '../../escape')):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                data = io.BytesIO()
                with tarfile.open(fileobj=data, mode='w') as archive:
                    member = tarfile.TarInfo(name)
                    if link:
                        member.type = tarfile.SYMTYPE
                        member.linkname = link
                    archive.addfile(member)
                data.seek(0)
                with tarfile.open(fileobj=data) as archive, self.assertRaises(tarfile.FilterError):
                    archive.extractall(temporary, filter='data')

    def test_artifact_checks_archived_app_without_loose_bundle(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            folder = root/'artifacts/A'
            folder.mkdir(parents=True)
            archive = folder/'Rhizome-update-proof.app.tar.gz'
            with tarfile.open(archive, 'w:gz') as package:
                member = tarfile.TarInfo('Synthetic.app')
                member.type = tarfile.DIRTYPE
                package.addfile(member)
            receipt = {'status': 'signed-notarized', 'version': '0.52.1', 'commit': 'synthetic',
                       'team': 'synthetic', 'archive': archive.name,
                       'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()}
            (folder/'receipt.json').write_text(json.dumps(receipt))
            build = types.ModuleType('build')
            build.ROOT = root
            build.MANIFEST = {'name': 'Synthetic', 'builds': {'A': {'version': '0.52.1', 'commit': 'synthetic'}}}
            build.clean_env = lambda: {}
            inspected = []
            build.check_app = lambda app, version, team: inspected.append(app.is_dir())
            with patch.dict(sys.modules, {'build': build}):
                module = runpy.run_path(str(Path(__file__).with_name('check.py')))
            with patch.dict(module['artifact'].__globals__, {'verify_signature': lambda label: None}):
                self.assertEqual(module['artifact']('A'), receipt)
                archive.write_bytes(b'tampered')
                with self.assertRaises(AssertionError):
                    module['artifact']('A')
            self.assertEqual(inspected, [True])


if __name__ == '__main__':
    unittest.main()
