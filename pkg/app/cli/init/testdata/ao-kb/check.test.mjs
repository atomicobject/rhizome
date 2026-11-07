import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, readdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { check } from '../../templates/starters/ao-kb/module/scripts/check.mjs';
import { globMatch, runCompilerCheck } from '../../templates/starters/ao-kb/module/scripts/lib/bindings-check.mjs';

// The compiler's report on a fresh install: dependencies present, no product selected yet.
const unselected = () => ({ starterInstalled: true, bindingsReady: false, indexReady: false, status: 'missing',
  dependencies: { uv: { ready: true, version: 'uv 0.9.0' }, rzm: { ready: true, version: 'rzm 0.51.0' } },
  diagnostics: [{ code: 'ao_selection_required', severity: 'error', message: 'ao_selection_required: persist a product' }] });

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
const named = (root, name, runCompiler = unselected) => check(root, { runCompiler }).checks.find(c => c.name === name);

test('checks static readiness, limitations and unavailable analysis without writes', t => {
  const root = fixture(t);
  const snapshot = () => readdirSync(root, { recursive: true, withFileTypes: true }).filter(e => e.isFile())
    .map(e => [join(e.parentPath, e.name), readFileSync(join(e.parentPath, e.name), 'utf8')]);
  const before = snapshot();
  const result = check(root, { runCompiler: unselected });
  for (const name of ['starter installed', 'AO KB root', 'schemaVersion', 'product mode', 'note includes', 'knowledge bindings']) assert.equal(named(root, name).status, 'ok');
  assert.equal(named(root, 'source ownership').status, 'error', 'no declaration means no protection');
  assert.equal(named(root, 'product-model bindings'), undefined, 'the knowledge check leaves model bindings to their own check');
  assert.match(named(root, 'saved analysis').message, /unavailable/);
  assert.equal(result.checks.length, 16);
  for (const item of result.checks) assert.ok(item.path && item.message);
  assert.deepEqual(snapshot(), before);
});

test('missing root, unsupported versions and product modes are explicit', t => {
  assert.equal(named(fixture(t, { '.kb/config/vault.md': null }), 'AO KB root').status, 'error');
  for (const version of [7, 9]) {
    const root = fixture(t, { '.kb/config/vault.md': `---\nschemaVersion: ${version}\nproductMode: multi-flat\n---\n` });
    assert.equal(named(root, 'schemaVersion').status, 'unsupported');
    assert.equal(named(root, 'product mode').status, 'warning');
    assert.match(named(root, 'product mode').message, /knowledge admission and model bindings/);
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
  assert.doesNotMatch(JSON.stringify(check(root, { runCompiler: unselected })), /healthy/);
});

test('malformed saved output and stamp report errors independently', t => {
  const root = fixture(t, { '.kb/config/manifest.json': '{', '.kb/index/kb-weights.json': '{' });
  assert.equal(named(root, 'toolkit stamp').status, 'error');
  assert.equal(named(root, 'saved analysis').status, 'error');
  assert.equal(named(root, 'knowledge bindings').status, 'ok');
});

test('include coverage needs the AO knowledge paths and an explicit include for the hidden registry', t => {
  const narrow = 'notes:\n  includes: ["notes/**"]\n';
  assert.equal(named(fixture(t, { '.rhizome/config.yml': narrow }), 'note includes').status, 'warning');
  const broad = 'notes:\n  includes: ["**/*.md"]\n';
  assert.equal(named(fixture(t, { '.rhizome/config.yml': 'notes: {includes: ["**/*.md"]}\n' }), 'note includes').status, 'warning', 'flow-style config is uncertain');
  assert.equal(named(fixture(t, { '.rhizome/config.yml': broad }), 'note includes').status, 'ok', 'no registry to reach');
  const missing = named(fixture(t, { '.rhizome/config.yml': broad, '.kb/curation/_conflicts.md': '# Conflicts\n' }), 'note includes');
  assert.equal(missing.status, 'warning');
  assert.match(missing.message, /\.kb\/curation\/_conflicts\.md/);
  const explicit = 'notes:\n  includes: ["**/*.md", ".kb/curation/_conflicts.md"]\n';
  assert.equal(named(fixture(t, { '.rhizome/config.yml': explicit, '.kb/curation/_conflicts.md': '# Conflicts\n' }), 'note includes').status, 'ok');
  const manifest = JSON.stringify({ sources: [{ path: 'artifacts/_assets/_model/features.yaml' }, { path: 'artifacts/_assets/engine-blocks/rows.json' }] });
  const bound = { '.rhizome/modules/ao-kb/bindings/manifest.json': manifest };
  const uncovered = named(fixture(t, { ...bound, '.rhizome/config.yml': broad }), 'note includes');
  assert.equal(uncovered.status, 'warning', 'a broad Markdown glob does not activate structured sources');
  assert.match(uncovered.message, /artifacts\/_assets\/_model\/features\.yaml, artifacts\/_assets\/engine-blocks\/rows\.json/);
  const models = 'notes:\n  includes: ["**/*.md", "artifacts/_assets/_model/*.yaml", "artifacts/_assets/engine-blocks/rows.json"]\n';
  assert.equal(named(fixture(t, { ...bound, '.rhizome/config.yml': models }), 'note includes').status, 'ok');
});

test('absent notes.includes is default all-Markdown discovery, and a first include list must keep it', t => {
  for (const config of ['code:\n  enabled: true\n', 'notes:\n  excludes:\n    - drafts/**\n']) {
    assert.equal(named(fixture(t, { '.rhizome/config.yml': config }), 'note includes').status, 'ok', config);
    const registry = named(fixture(t, { '.rhizome/config.yml': config, '.kb/curation/_conflicts.md': '# Conflicts\n' }), 'note includes');
    assert.equal(registry.status, 'warning');
    assert.match(registry.message, /\.kb\/curation\/_conflicts\.md/);
    assert.match(registry.message, /\*\*\/\*\.md/, 'a first include list that omits **/*.md would stop indexing the other notes');
  }
  const manifest = JSON.stringify({ sources: [{ path: 'artifacts/_assets/_model/features.yaml' }] });
  const models = named(fixture(t, { '.rhizome/config.yml': 'code:\n  enabled: true\n', '.rhizome/modules/ao-kb/bindings/manifest.json': manifest }), 'note includes');
  assert.equal(models.status, 'warning');
  assert.match(models.message, /artifacts\/_assets\/_model\/features\.yaml/);
  assert.match(models.message, /\*\*\/\*\.md/);
});

test('product tags AO would read differently from admission are named', t => {
  const vault = '---\nschemaVersion: 8\nproductMode: multi-nested\nproducts: [alpha, beta]\n---\n';
  // Admission compares frontmatter products exactly, as AO does: inline fields and value
  // variants agree. Only a differently cased key still reads differently.
  const clean = named(fixture(t, { '.kb/config/vault.md': vault, 'assertions/a.md': '---\nproducts: [alpha, "*"]\n---\nBody\n',
    'assertions/inline.md': '---\nproducts: [beta]\n---\nproducts:: alpha\n',
    'assertions/case.md': '---\nproducts: [Alpha]\n---\nBody\n' }), 'product tags');
  assert.equal(clean.status, 'ok');
  const root = fixture(t, { '.kb/config/vault.md': vault,
    'assertions/key.md': '---\nProducts: [beta]\n---\nBody\n',
    'assertions/fine.md': '---\nproducts: [beta]\n---\nBody\n' });
  const loose = named(root, 'product tags');
  assert.equal(loose.status, 'unsupported');
  assert.match(loose.message, /assertions\/key\.md/);
  assert.doesNotMatch(loose.message, /fine\.md/);
});

test('source ownership is ok only when the AO knowledge paths are declared', t => {
  const full = "sources:\n  - owner: AO KB\n    reason: AO KB writes these.\n    paths:\n      - .kb/**\n      - assertions/**\n      - sources/**\n      - concepts/**\n      - artifacts/**\n";
  assert.equal(named(fixture(t, { '.rhizome/source-ownership.yml': full }), 'source ownership').status, 'ok');
  const partial = full.replace('      - concepts/**\n', '');
  const result = named(fixture(t, { '.rhizome/source-ownership.yml': partial }), 'source ownership');
  assert.equal(result.status, 'warning');
  assert.match(result.message, /concepts\/\*\*/);
});

test('names assertions whose frontmatter connections are sequences', t => {
  const root = fixture(t, {
    'assertions/map.md': '---\nconnections: {supports: [a-1]}\n---\nClaim.\n',
    'assertions/typed.md': '---\nconnections:\n  - type: refines\n    target: a-1\n---\nClaim.\n',
    'assertions/bare.md': '---\nconnections: [a-1]\n---\nClaim.\n',
  });
  const result = named(root, 'frontmatter connections');
  assert.equal(result.status, 'unsupported');
  assert.match(result.message, /assertions\/typed\.md/);
  assert.match(result.message, /assertions\/bare\.md/);
  assert.doesNotMatch(result.message, /map\.md/);
});

test('reports binding freshness beside the knowledge checks without trusting the saved manifest', t => {
  const root = fixture(t);
  let result = check(root, { runCompiler: unselected });
  assert.equal(result.starterInstalled, true);
  assert.equal(result.bindingsReady, false);
  assert.equal(result.indexReady, false);
  assert.equal(named(root, 'model compiler dependencies').status, 'ok');
  assert.equal(named(root, 'model bindings').status, 'pending');
  assert.match(named(root, 'model bindings').message, /--product <slug>/);
  assert.equal(named(root, 'model index').status, 'pending');
  assert.equal(named(root, 'model source ownership').status, 'pending');

  const missingUv = () => ({ ...unselected(), dependencies: { uv: { ready: false, message: 'spawn uv ENOENT' }, rzm: { ready: true } },
    diagnostics: [{ code: 'ao_dependency_missing', severity: 'error', message: 'ao_dependency_missing: install the reported dependencies' }] });
  assert.equal(named(root, 'model compiler dependencies', missingUv).status, 'error');
  assert.match(named(root, 'model compiler dependencies', missingUv).message, /uv/);
  assert.equal(named(root, 'model bindings', missingUv).status, 'pending');

  const stale = () => ({ bindingsReady: false, indexReady: false, status: 'stale', dependencies: {},
    diagnostics: [{ code: 'ao_bindings_stale', severity: 'error', message: 'ao_bindings_stale: refresh-bindings is required' }] });
  assert.equal(named(root, 'model bindings', stale).status, 'warning');

  const ready = () => ({ bindingsReady: true, indexReady: true, status: 'ready', dependencies: {}, diagnostics: [] });
  result = check(root, { runCompiler: ready });
  assert.equal(result.bindingsReady, true);
  assert.equal(result.indexReady, true);
  assert.equal(named(root, 'model bindings', ready).status, 'ok');
  assert.equal(named(root, 'model index', ready).status, 'ok');
});

test('requires every compiled AO source to be declared AO-owned', t => {
  const manifest = JSON.stringify({ sources: [{ path: 'artifacts/_assets/_model/lane.yaml' }, { path: 'products/alpha/artifacts/_assets/engine-blocks/rows.json' }, { path: 'model/extra.yaml' }],
    knowledgeSources: [{ path: 'assertions/shared.md' }] });
  const ownership = "sources:\n  - owner: AO KB\n    reason: AO KB writes these.\n    paths:\n      - .kb/**\n      - assertions/**\n      - sources/**\n      - concepts/**\n      - artifacts/**\n      - products/*/artifacts/**\n";
  const root = fixture(t, { '.rhizome/modules/ao-kb/bindings/manifest.json': manifest, '.rhizome/source-ownership.yml': ownership });
  const row = named(root, 'model source ownership');
  assert.equal(row.status, 'error');
  assert.match(row.message, /model\/extra\.yaml/);
  assert.doesNotMatch(row.message, /lane\.yaml|rows\.json|shared\.md/);
  const covered = fixture(t, { '.rhizome/modules/ao-kb/bindings/manifest.json': manifest, '.rhizome/source-ownership.yml': ownership + "      - model/**\n" });
  assert.equal(named(covered, 'model source ownership').status, 'ok');
});

test('ownership patterns match like the declaration', () => {
  assert.ok(globMatch('.kb/**', '.kb/curation/_conflicts.md'));
  assert.ok(globMatch('products/*/artifacts/**', 'products/alpha/artifacts/_assets/_model/lane.yaml'));
  assert.ok(!globMatch('products/*/artifacts/**', 'products/alpha/beta/artifacts/x.yaml'));
  assert.ok(globMatch('decisions.md', 'DECISIONS.md'), 'case-insensitive like the server');
  assert.ok(!globMatch('artifacts/**', 'notes/artifacts.md'));
  assert.ok(!globMatch('**.md', 'assertions/note.md'), 'globstar is recursive only as a whole path segment');
});

test('a missing compiler is an error, not a ready result', t => {
  const report = runCompilerCheck(fixture(t));
  assert.equal(report.bindingsReady, false);
  assert.equal(report.diagnostics[0].code, 'ao_compiler_missing');
});

test('public check preserves independent knowledge and model readiness', t => {
  const root = fixture(t);
  const runCompiler = () => ({...unselected(), knowledgeReady: true, modelReady: false,
    bindings: {knowledge: {ready: true, status: 'ready'}, model: {ready: false, status: 'incomplete'}}});
  const report = check(root, {runCompiler});
  assert.equal(report.knowledgeReady, true);
  assert.equal(report.modelReady, false);
  assert.equal(report.bindingsReady, false);
  assert.equal(report.bindings.knowledge.status, 'ready');
  assert.equal(report.bindings.model.status, 'incomplete');
});

test('local schema types whose query roots collide with the starter knowledge types are named', t => {
  const local = 'type assertion @node(paths: ["assertions/*.md"]) { id: ID }\ntype hub @node(paths: ["MOCs/*.md"]) { x: String }\n';
  const row = named(fixture(t, { '.rhizome/ontology/core-knowledge.graphql': local }), 'knowledge bindings');
  assert.equal(row.status, 'error');
  assert.match(row.message, /\.rhizome\/ontology\/core-knowledge\.graphql declares assertion/);
  assert.doesNotMatch(row.message, /hub/);
  const module = named(fixture(t, { '.rhizome/modules/other/ontology/x.graphql': 'type Assertion @node(paths: ["a/*.md"]) { id: ID }\n' }), 'knowledge bindings');
  assert.equal(module.status, 'error');
  assert.equal(named(fixture(t, { '.rhizome/ontology/other.graphql': 'type hub { x: String }\n' }), 'knowledge bindings').status, 'ok');
});
