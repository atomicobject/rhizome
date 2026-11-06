"""npm metadata remains UTF-8 on Windows' legacy system code page."""
import json
from pathlib import Path, PureWindowsPath
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import generate


class LicenseMetadataTests(unittest.TestCase):
    def test_non_ascii_package_metadata_is_read_as_utf8_under_cp1252_defaults(self):
        original_read = Path.read_text
        def legacy_default(path, *args, **kwargs):
            if not kwargs.get('encoding') and not args:
                return path.read_bytes().decode('cp1252')
            return original_read(path, *args, **kwargs)
        for unicode_lock in (False, True):
            with self.subTest(unicode_lock=unicode_lock), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                web = root / 'web'
                package = web / 'node_modules/example'
                package.mkdir(parents=True)
                lock = {'packages': {'node_modules/example': {'version':'1.0.0', 'license':'MIT'}}}
                if unicode_lock:
                    lock['description'] = 'Алексей'
                (web / 'package-lock.json').write_text(json.dumps(lock, ensure_ascii=False), encoding='utf-8')
                metadata = {'version':'1.0.0', 'author':'Алексей', 'repository':'https://example.test/Алексей'}
                (package / 'package.json').write_text(json.dumps(metadata, ensure_ascii=False), encoding='utf-8')
                notice = 'Copyright Алексей\nMIT license\n'.encode('utf-8')
                (package / 'LICENSE').write_bytes(notice)
                outputs = {}
                with patch.object(generate, 'ROOT', root), patch.object(generate, 'WEB', web), \
                        patch.object(generate, 'LICENSES', root / 'licenses'), patch.object(Path, 'read_text', legacy_default):
                    entries = generate.generate_web(outputs)
                self.assertEqual('https://example.test/Алексей', entries[0].source)
                self.assertEqual([notice], list(outputs.values()))
                self.assertFalse((root / 'licenses').exists())

    def test_index_uses_portable_links_for_windows_directories(self):
        root = PureWindowsPath('C:/rhizome')
        entry = generate.NoticeEntry('Go', 'example', 'v1.0.0', 'MIT', 'vendor/example',
            root / 'licenses/go/example@v1.0.0', ())
        with patch.object(generate, 'ROOT', root):
            index = generate.render_index([entry])
        self.assertIn(b'`licenses/go/example@v1.0.0`', index)
        self.assertNotIn(b'\\', index)

    def test_windows_checkout_preserves_canonical_notice_bytes_only(self):
        attributes = (generate.ROOT / '.gitattributes').read_bytes()
        notice = 'Copyright Алексей\nLicense text\n'.encode('utf-8')
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            root = base / 'repo'
            root.mkdir()
            (root / '.gitattributes').write_bytes(attributes)
            sources = root / 'licenses/sources'
            for name in ('skills/impeccable-4.7.0/LICENSE', 'skills/impeccable-4.7.0/NOTICE.md',
                         'skills/greptileai/LICENSE'):
                path = sources / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(notice)
            outputs = {}
            with patch.object(generate, 'ROOT', root), patch.object(generate, 'SOURCES', sources), \
                    patch.object(generate, 'LICENSES', root / 'licenses'):
                generate.generate_skills(outputs)
            outputs.update({root / path: notice for path in (
                'licenses/go/example@v1.0.0/LICENSE', 'licenses/web/example@1.0.0/LICENSE',
                'licenses/fonts/example/OFL.txt', 'THIRD_PARTY_LICENSES.md',
                'web/public/third-party-licenses.txt')})
            generate.apply_outputs(outputs)
            (root / 'unrelated.txt').write_bytes(b'ordinary\ntext\n')
            binary = b'\x00binary\nbytes\r\n'
            (root / 'unrelated.bin').write_bytes(binary)
            def git(*args):
                subprocess.run(['git', '-c', 'core.autocrlf=true', *args], cwd=root,
                    check=True, capture_output=True)
            git('init', '--quiet')
            git('add', '--', '.')
            checkout = base / 'checkout'
            git('checkout-index', '--all', '--prefix=' + checkout.as_posix() + '/')
            canonical = [*outputs, *(path for path in sources.rglob('*') if path.is_file())]
            for path in canonical:
                with self.subTest(path=path.relative_to(root)):
                    self.assertEqual(notice, (checkout / path.relative_to(root)).read_bytes())
            self.assertEqual(b'ordinary\r\ntext\r\n', (checkout / 'unrelated.txt').read_bytes())
            self.assertEqual(binary, (checkout / 'unrelated.bin').read_bytes())


if __name__ == '__main__':
    unittest.main()
