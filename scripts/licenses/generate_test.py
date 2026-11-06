"""npm metadata remains UTF-8 on Windows' legacy system code page."""
import json
from pathlib import Path
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


if __name__ == '__main__':
    unittest.main()
