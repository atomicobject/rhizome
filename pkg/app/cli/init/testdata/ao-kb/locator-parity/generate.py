#!/usr/bin/env python3
"""Rerun: python3 -I generate.py /path/to/atomic-ai-prompt-toolkit > golden.json
Read-only import of AO; all generated vault inputs live in a TemporaryDirectory.
Cases are the portable fixture definition; golden.json records AO's actual result.
"""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
spec = importlib.util.spec_from_file_location('ao_locators', root / 'plugins/ao-kb/skills/process/locators.py')
ao = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ao)
cases = json.loads(Path(__file__).with_name('cases.json').read_text())
results = []
for case in cases:
    with tempfile.TemporaryDirectory() as temp:
        vault = Path(temp)
        for name, text in case['files'].items():
            p = vault / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(text.encode('utf-8'))
        source = vault / 'sources/source.md'
        source.parent.mkdir(parents=True, exist_ok=True)
        source.write_text('---\npath: ' + json.dumps(case['paths']) + '\nlocator_scheme: ' + case.get('scheme', '') + '\n---\n' + case.get('body', ''))
        src = ao.Source(temp, str(source))
        try:
            text, span = src.locate(case['locator'])
            if text is None:
                result = {'status': 'source-text-missing'}
            else:
                result = {'status': 'resolved', 'lines': ao.resolve_span(text, span, src.scheme).split('\n')}
        except ao.Malformed:
            result = {'status': 'malformed'}
        except ao.Unresolved:
            result = {'status': 'unresolved'}
        results.append({'name': case['name'], **result})
commit = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
print(json.dumps({'toolkitCommit': commit, 'results': results}, ensure_ascii=False, indent=2))
