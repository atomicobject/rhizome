"""The linuxdeploy tool seam preserves only the verified AppDir companion."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from desktop_candidates import sha256
from linux_sidecar import bundle_environment


@unittest.skipUnless(os.name == 'posix', 'Linux packaging uses a POSIX tool launcher')
class LinuxSidecarTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="AppImage ; fixture ' Δ ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        config = self.root / 'desktop/src-tauri/tauri.conf.json'
        config.parent.mkdir(parents=True)
        config.write_text('{"productName":"Rhizome"}', encoding='utf-8')
        self.bundle = self.root / 'bundle'
        self.sidecar = self.bundle / 'appimage/Rhizome.AppDir/usr/bin/rzm'
        self.sidecar.parent.mkdir(parents=True)
        self.original = b'canonical executable bytes'
        self.sidecar.write_bytes(self.original)
        self.digest = sha256(self.sidecar)
        tools = self.root / 'tools'
        tools.mkdir()
        self.record = self.root / 'patchelf-calls.jsonl'
        real = tools / 'patchelf'
        real.write_text(f'#!{sys.executable}\n' +
            'import json,sys\nfrom pathlib import Path\n' +
            f'with Path({str(self.record)!r}).open("a") as file: file.write(json.dumps(sys.argv[1:])+"\\n")\n' +
            'if sys.argv[1] == "--print-rpath": print("actual-rpath")\n' +
            'if sys.argv[1] == "--set-rpath":\n' +
            '    path=Path(sys.argv[-1]); path.write_bytes(path.read_bytes()+b" mutated")\n' +
            'if sys.argv[1] == "--error": sys.exit(7)\n', encoding='utf-8')
        real.chmod(0o755)
        self.env = {'PATH': str(tools) + os.pathsep + os.defpath}

    def invoke(self, env, *arguments):
        return subprocess.run([env['PATCHELF'], *arguments], env=env, capture_output=True,
                              text=True, encoding='utf-8', timeout=10)

    def test_verified_companion_write_is_skipped_and_queries_other_files_and_exit_codes_delegate(self):
        gui = self.sidecar.with_name('rhizome-desktop')
        unrelated = self.root / 'another AppDir/usr/bin/rzm'
        for path in (gui, unrelated):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'other executable')
        with bundle_environment(self.root, self.bundle, self.digest, self.env) as env:
            self.assertEqual('1', env['NO_STRIP'])
            self.assertEqual(0, self.invoke(env, '--set-rpath', '$ORIGIN/../lib', str(self.sidecar)).returncode)
            self.assertFalse(self.record.exists())
            query = self.invoke(env, '--print-rpath', str(self.sidecar))
            self.assertEqual((0, 'actual-rpath\n'), (query.returncode, query.stdout))
            for path in (gui, unrelated):
                self.assertEqual(0, self.invoke(env, '--set-rpath', '$ORIGIN/../lib', str(path)).returncode)
                self.assertEqual(b'other executable mutated', path.read_bytes())
            self.assertEqual(7, self.invoke(env, '--error').returncode)
        self.assertEqual(self.original, self.sidecar.read_bytes())
        self.assertEqual(4, len(self.record.read_text(encoding='utf-8').splitlines()))

    def test_changed_companion_or_unexpected_mutations_fail_closed(self):
        with bundle_environment(self.root, self.bundle, self.digest, self.env) as env:
            for arguments in (('--remove-rpath',), ('--set-rpath', '/unexpected'),
                              ('--force-rpath', '--set-rpath', '$ORIGIN/../lib')):
                with self.subTest(arguments=arguments):
                    self.assertNotEqual(0, self.invoke(env, *arguments, str(self.sidecar)).returncode)
                    self.assertEqual(self.original, self.sidecar.read_bytes())
            self.assertNotEqual(0, self.invoke(env, '--set-rpath', '$ORIGIN/../lib', str(self.sidecar), '--debug').returncode)
            self.sidecar.write_bytes(b'changed before packaging')
            for operation in (('--print-rpath',), ('--set-rpath', '$ORIGIN/../lib')):
                self.assertNotEqual(0, self.invoke(env, *operation, str(self.sidecar)).returncode)
        self.assertFalse(self.record.exists())


if __name__ == '__main__':
    unittest.main()
