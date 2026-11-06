"""Manual oracle regeneration, never run by CI.
python3.11 -m venv /private/tmp/aokb-laneY-oracle
/private/tmp/aokb-laneY-oracle/bin/pip install pyyaml==6.0.3
/private/tmp/aokb-laneY-oracle/bin/python pkg/noteformat/structured/testdata/regenerate.py
rm -rf /private/tmp/aokb-laneY-oracle
AO revision: e9dce951ab4cd6aa9ccc734df96590bde25ccde9.
Pin: AO packages/kb-site/uv.lock; reader: kits/core/_build/metamodel.py::_yaml.
"""
import datetime
import json
from pathlib import Path
import yaml

assert yaml.__version__ == "6.0.3"
inputs = [
    '123', '"123"', "'00123'", '00123', '0', '-0', '+12', '0xFF', '-0xFF',
    '0b101', '0o12', '012', '08', '09', '1_000', '0_7', '0xF_F', '1__0',
    '99999999999999999999999999999999999999',
    '1e3', '1e+3', '1.0e3', '1.0e+3', '1.0E-3', '1.25', '0.1', '.5', '-.5', '+.5',
    '1.', '01.20', '1_000.5', '1.0_5', '-0.0',
    '2026-11-06', '2026-1-6', '2026-11-06T12:30:00Z',
    '2026-11-06t12:30:00.123400+02:30', '2026-1-6 1:02:03 -5',
    'null', 'Null', 'NULL', '~', '', '""', '"null"', '[]', '{}',
]
for word in ('yes', 'no', 'on', 'off', 'true', 'false'):
    inputs += [word, word.title(), word.upper(), word[0].lower() + word[1:].upper(), json.dumps(word)]
# Each intentional unsupported family has concrete examples and the AO result.
unsupported = {
    '1:30': 'legacy sexagesimal integer',
    '-1:30': 'legacy sexagesimal integer',
    '1:30.5': 'legacy sexagesimal float',
    '.inf': 'nonfinite float', '-.Inf': 'nonfinite float', '.NaN': 'nonfinite float',
    '1.0e+999': 'float overflow', '1.0e-999': 'float underflow',
    '1.234567890123456789': 'lossy float',
    '2026-11-06T12:30:00': 'timezone-free timestamp',
    '2026-11-06T12:30:00.1234567Z': 'sub-microsecond timestamp',
    '2026-11-06T12:30:00+01:60': 'noncivil offset',
    '2026-02-30': 'invalid date', '0000-01-01': 'out-of-domain date',
    '2026-11-06T25:00:00Z': 'invalid timestamp',
}

def expectation(source, loader):
    try:
        v = yaml.load(source, Loader=loader)
    except (ValueError, yaml.YAMLError) as e:
        return {'kind': 'error', 'text': type(e).__name__}
    if v is None: kind, text = 'null', ''
    elif isinstance(v, bool): kind, text = 'bool', str(v).lower()
    elif isinstance(v, (int, float)): kind, text = 'number', str(v)
    elif isinstance(v, datetime.datetime): kind, text = 'timestamp', v.isoformat()
    elif isinstance(v, datetime.date): kind, text = 'date', v.isoformat()
    elif isinstance(v, list): kind, text = 'array', ''
    elif isinstance(v, dict): kind, text = 'object', ''
    else: kind, text = 'string', v
    return {'kind': kind, 'text': text}

rows = []
for source in inputs + list(unsupported):
    expected = expectation(source, yaml.SafeLoader)
    assert expected == expectation(source, yaml.CSafeLoader), source
    row = {'source': source, **expected}
    if source in unsupported: row['unsupported'] = unsupported[source]
    rows.append(row)
Path(__file__).with_name('scalar-oracle.json').write_text(
    json.dumps({'pyyaml': yaml.__version__, 'profile': 'pyyaml-6.0.3-v1', 'cases': rows}, indent=2) + '\n')
print(f'Generated {len(rows)} cases; SafeLoader and CSafeLoader agree')
