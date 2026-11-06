import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, readdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { check } from '../../templates/starters/ao-kb/module/scripts/check.mjs';

function fixture(t, overrides = {}) {
  const root = mkdtempSync(join(tmpdir(), 'ao-kb-check-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const files = {
    '.kb/config/vault.md': '---\nschemaVersion: 8\nproductMode: single\n---\n',
    '.rhizome/config.yml': "notes:\n  includes:\n    - '**/*.md'\n",
    '.rhizome/modules/ao-kb/actions/check.yaml': 'run: {}\n',
    '.rhizome/modules/ao-kb/ontology/knowledge.graphql': 'type Assertion { title: String }',
    ...overrides,
  };
  for (const [path, text] of Object.entries(files)) {
    if (text === null) continue;
    mkdirSync(join(root, path, '..'), { recursive: true });
    writeFileSync(join(root, path), text);
  }
  return root;
}
const named = (root, name) => check(root).checks.find(c => c.name === name);

test('checks static readiness, limitations and unavailable analysis without writes', t => {
  const root = fixture(t);
  const snapshot = () => readdirSync(root, { recursive: true, withFileTypes: true }).filter(e => e.isFile())
    .map(e => [join(e.parentPath, e.name), readFileSync(join(e.parentPath, e.name), 'utf8')]);
  const before = snapshot();
  const result = check(root);
  for (const name of ['starter installed', 'AO KB root', 'schemaVersion', 'product mode', 'note includes', 'knowledge bindings']) assert.equal(named(root, name).status, 'ok');
  assert.equal(named(root, 'source ownership').status, 'pending');
  assert.equal(named(root, 'product-model bindings').status, 'unsupported');
  assert.match(named(root, 'saved analysis').message, /unavailable/);
  assert.equal(result.checks.length, 10);
  for (const item of result.checks) assert.ok(item.path && item.message);
  assert.deepEqual(snapshot(), before);
});

test('missing root, unsupported versions and product modes are explicit', t => {
  assert.equal(named(fixture(t, { '.kb/config/vault.md': null }), 'AO KB root').status, 'error');
  for (const version of [7, 9]) {
    const root = fixture(t, { '.kb/config/vault.md': `---\nschemaVersion: ${version}\nproductMode: multi-flat\n---\n` });
    assert.equal(named(root, 'schemaVersion').status, 'unsupported');
    assert.match(named(root, 'product mode').message, /all products/);
    if (version === 7) assert.match(named(root, 'schemaVersion').message, /upgrade AO KB first/);
  }
});

test('reports optional recorded stamp and dated saved output without a health verdict', t => {
  const root = fixture(t, {
    '.kb/config/manifest.json': JSON.stringify({ toolkit: { version: '4.12.0', commit: 'synthetic' } }),
    '.kb/index/kb-weights.json': JSON.stringify({ meta: { asOf: '2026-10-01', generatedAt: '2026-10-02', builder: 'AO' } }),
  });
  assert.match(named(root, 'toolkit stamp').message, /4.12.0/);
  assert.match(named(root, 'saved analysis').message, /meta.asOf 2026-10-01/);
  assert.doesNotMatch(JSON.stringify(check(root)), /healthy/);
});

test('malformed saved output and stamp report errors independently', t => {
  const root = fixture(t, { '.kb/config/manifest.json': '{', '.kb/index/kb-weights.json': '{' });
  assert.equal(named(root, 'toolkit stamp').status, 'error');
  assert.equal(named(root, 'saved analysis').status, 'error');
  assert.equal(named(root, 'knowledge bindings').status, 'ok');
});

test('include coverage preserves uncertainty and requires hidden registry coverage', t => {
  for (const config of ['notes:\n  includes: ["assertions/**", "sources/**", "concepts/**", "artifacts/**"]\n', 'notes: {includes: ["**/*.md"]}\n']) {
    assert.equal(named(fixture(t, { '.rhizome/config.yml': config }), 'note includes').status, 'warning');
  }
  const config = 'notes:\n  includes: ["assertions/**", "sources/**", "concepts/**", "artifacts/**", ".kb/curation/_conflicts.md"]\n';
  assert.equal(named(fixture(t, { '.rhizome/config.yml': config }), 'note includes').status, 'ok');
});

 test('hidden conflict registry needs discovery support even with includes', t => {
  const root = fixture(t, { '.kb/curation/_conflicts.md': '# Conflicts\n' });
  assert.equal(named(root, 'note includes').status, 'warning');
  assert.match(named(root, 'note includes').message, /hidden directories/);
});
