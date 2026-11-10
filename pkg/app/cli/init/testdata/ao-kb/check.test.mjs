import test from 'node:test';
import assert from 'node:assert/strict';
import { cpSync, mkdtempSync, mkdirSync, writeFileSync, rmSync, readdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { check } from '../../templates/starters/ao-kb/module/scripts/check.mjs';
import { globMatch, liftStarterOwnership, ownedPatterns, runCompilerCheck, STARTER_PATHS } from '../../templates/starters/ao-kb/module/scripts/lib/bindings-check.mjs';

// The declaration the starter installed before 2026-11-10, as an existing installation still carries it.
const FORMER = "# Installed by the ao-kb starter. AO KB writes these files; Rhizome reads them and\n# refuses native edits to them. Paths follow AO KB's standard layout for a single-product\n# KB at the vault root and for every product root of a multi-product account\n# (products/<slug>/). Add any other AO-authored location your KB uses. Removing this\n# file (or removing the ao-kb add-on) removes the protection.\nsources:\n  - owner: AO KB\n    reason: Managed by AO KB. Change it with the AO KB skills (/ao-kb:process, /ao-kb:curate, /ao-kb:suggest).\n    paths:\n      - .kb/**\n      - assertions/**\n      - sources/**\n      - concepts/**\n      - artifacts/**\n      - decisions.md\n      - _moc.md\n      - MOCs/**\n      - _improvements/**\n      - products/*/.kb/**\n      - products/*/assertions/**\n      - products/*/sources/**\n      - products/*/concepts/**\n      - products/*/artifacts/**\n      - products/*/decisions.md\n      - products/*/_moc.md\n      - products/*/MOCs/**\n      - products/*/_improvements/**\n";

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
  assert.equal(named(root, 'source ownership').status, 'ok', 'the starter declares no ownership; notes are editable');
  assert.equal(named(root, 'product-model bindings'), undefined, 'the knowledge check leaves model bindings to their own check');
  assert.match(named(root, 'saved analysis').message, /unavailable/);
  assert.equal(result.checks.length, 15);
  for (const item of result.checks) assert.ok(item.path && item.message);
  assert.deepEqual(snapshot(), before);
});

test('missing root, unsupported versions and product modes are explicit', t => {
  assert.equal(named(fixture(t, { '.kb/config/vault.md': null }), 'AO KB root').status, 'error');
  // AO reads a vault.md that records no schemaVersion as current, and so does the check.
  const unrecorded = named(fixture(t, { '.kb/config/vault.md': '---\nproductMode: single\n---\n' }), 'schemaVersion');
  assert.equal(unrecorded.status, 'ok');
  assert.match(unrecorded.message, /no schemaVersion/i);
  assert.equal(named(fixture(t, { '.kb/config/vault.md': "---\nschemaVersion: '8'\n---\n" }), 'schemaVersion').status, 'ok');
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

test('source ownership reports the former starter declaration, the repository\'s own entries, and none', t => {
  const former = named(fixture(t, { '.rhizome/source-ownership.yml': FORMER }), 'source ownership');
  assert.equal(former.status, 'warning');
  assert.match(former.message, /former read-only declaration still covers .*assertions\/\*\*.*refresh-bindings/);
  const own = "sources:\n  - owner: Model tool\n    reason: Generated rows.\n    paths:\n      - model/**\n";
  const kept = named(fixture(t, { '.rhizome/source-ownership.yml': own }), 'source ownership');
  assert.equal(kept.status, 'ok');
  assert.match(kept.message, /model\/\*\* read-only.*starter adds no declaration/);
  const unreadable = named(fixture(t, { '.rhizome/source-ownership.yml': 'sources:\n  - owner: AO KB\n reason: bad indent\n    paths: [assertions/**]\n' }), 'source ownership');
  assert.equal(unreadable.status, 'warning');
  assert.match(unreadable.message, /cannot read the declaration/);
  assert.equal(named(fixture(t, { '.rhizome/source-ownership.yml': 'sources:\n' }), 'source ownership').status, 'ok');
  assert.match(named(fixture(t), 'source ownership').message, /No source ownership declaration/);
});

test('lifting removes exactly the former starter paths and keeps every other line', () => {
  // The untouched former declaration lifts to no file at all.
  assert.deepEqual(liftStarterOwnership(FORMER), { legacy: STARTER_PATHS, text: null });
  // Another owner's entry and a path someone added under AO KB stay byte for byte.
  const mixed = "# Local note\nsources:\n  - owner: Model tool\n    reason: Generated rows.\n    paths:\n      - model/**\n  - owner: AO KB\n    reason: Managed by AO KB.\n    paths:\n      - assertions/**\n      - 'extra/**' # mine\n      - products/*/sources/**\n  - owner: 'Docs team'\n    reason: \"Reviewed copy.\"\n    paths: [\"docs/**\"]\n";
  const lifted = liftStarterOwnership(mixed);
  assert.deepEqual(lifted.legacy, ['assertions/**', 'products/*/sources/**']);
  assert.equal(lifted.text, "# Local note\nsources:\n  - owner: Model tool\n    reason: Generated rows.\n    paths:\n      - model/**\n  - owner: AO KB\n    reason: Managed by AO KB.\n    paths:\n      - 'extra/**' # mine\n  - owner: 'Docs team'\n    reason: \"Reviewed copy.\"\n    paths: [\"docs/**\"]\n");
  assert.deepEqual(ownedPatterns(lifted.text), ['model/**', 'extra/**', 'docs/**']);
  // An AO KB entry with nothing else goes; the other owner remains.
  const two = "sources:\n  - owner: AO KB\n    reason: Managed by AO KB.\n    paths:\n      - concepts/**\n  - owner: Model tool\n    reason: Generated rows.\n    paths:\n      - model/**\n";
  assert.equal(liftStarterOwnership(two).text, "sources:\n  - owner: Model tool\n    reason: Generated rows.\n    paths:\n      - model/**\n");
  // Flow-style lists lift entry by entry, and CRLF stays CRLF.
  const flow = "sources:\r\n  - owner: AO KB\r\n    reason: AO.\r\n    paths: [\"artifacts/**\", \".kb/curation/**\", 'sources/**']\r\n";
  assert.deepEqual(liftStarterOwnership(flow), { legacy: ['artifacts/**', 'sources/**'], text: "sources:\r\n  - owner: AO KB\r\n    reason: AO.\r\n    paths: [\".kb/curation/**\"]\r\n" });
  // Nothing of the starter's, uncertain YAML, and no file all come back unchanged.
  for (const text of [two.replace('AO KB', 'Someone'), 'sources:\n  - owner: AO KB\n reason: bad indent\n    paths: [assertions/**]\n', 'sources:\n', null]) {
    assert.deepEqual(liftStarterOwnership(text), { legacy: [], text });
  }
  // Lifting converges: a lifted declaration has nothing left to lift.
  assert.deepEqual(liftStarterOwnership(lifted.text), { legacy: [], text: lifted.text });
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
  assert.equal(named(root, 'model source ownership'), undefined, 'ownership is no longer a model row');

  const missingUv = () => ({ ...unselected(), dependencies: { uv: { ready: false, message: 'spawn uv ENOENT' }, rzm: { ready: true } },
    diagnostics: [{ code: 'ao_dependency_missing', severity: 'error', message: 'ao_dependency_missing: install the reported dependencies' }] });
  assert.equal(named(root, 'model compiler dependencies', missingUv).status, 'dependency missing');
  assert.match(named(root, 'model compiler dependencies', missingUv).message, /uv/);
  assert.equal(named(root, 'model bindings', missingUv).status, 'dependency missing');

  // uv is on PATH but cannot prepare AO's pinned packages offline: a dependency, not stale bindings.
  const offline = () => ({ bindingsReady: false, indexReady: false, status: 'dependency missing',
    dependencies: { uv: { ready: true, version: 'uv 0.13.7' }, rzm: { ready: true, version: 'rzm 0.51.0' } },
    diagnostics: [{ code: 'ao_dependency_unavailable', severity: 'error', message: 'ao_dependency_unavailable: uv could not prepare AO\'s pinned kb-site package and PyYAML: the network was disabled' }] });
  for (const name of ['model compiler dependencies', 'model bindings']) {
    assert.equal(named(root, name, offline).status, 'dependency missing', name);
    assert.match(named(root, name, offline).message, /network was disabled/, name);
    assert.doesNotMatch(named(root, name, offline).message, /stale/, name);
  }

  const duplicate = () => ({ bindingsReady: false, indexReady: false, status: 'duplicate IDs', dependencies: {},
    diagnostics: [{ code: 'ao_duplicate_record', severity: 'error', kind: 'feature', key: 'FEAT-added', count: 2,
      message: '2 feature records use the ID FEAT-added: products/alpha/features.yaml#/features/0, products/alpha/features.yaml#/features/2' }] });
  assert.equal(named(root, 'model bindings', duplicate).status, 'duplicate IDs');
  assert.match(named(root, 'model bindings', duplicate).message, /FEAT-added.*features\/0.*features\/2/);
  assert.doesNotMatch(named(root, 'model bindings', duplicate).message, /stale/);

  const stale = () => ({ bindingsReady: false, indexReady: false, status: 'stale', dependencies: {},
    diagnostics: [{ code: 'ao_bindings_stale', severity: 'error', message: 'ao_bindings_stale: refresh-bindings is required' }] });
  assert.equal(named(root, 'model bindings', stale).status, 'stale');

  const failed = () => ({ bindingsReady: false, indexReady: false, status: 'error', dependencies: {},
    diagnostics: [{ code: 'ao_toolkit_pin', severity: 'error', message: 'ao_toolkit_pin: manifest siteEngine.version is not the supported baseline' }] });
  assert.equal(named(root, 'model bindings', failed).status, 'error');
  assert.match(named(root, 'model bindings', failed).message, /siteEngine/);

  const ready = () => ({ bindingsReady: true, indexReady: true, status: 'ready', dependencies: {}, diagnostics: [] });
  result = check(root, { runCompiler: ready });
  assert.equal(result.bindingsReady, true);
  assert.equal(result.indexReady, true);
  assert.equal(named(root, 'model bindings', ready).status, 'ok');
  assert.equal(named(root, 'model index', ready).status, 'ok');
});

test('ownership patterns match like the declaration', () => {
  assert.ok(globMatch('.kb/**', '.kb/curation/_conflicts.md'));
  assert.ok(globMatch('products/*/artifacts/**', 'products/alpha/artifacts/_assets/_model/lane.yaml'));
  assert.ok(!globMatch('products/*/artifacts/**', 'products/alpha/beta/artifacts/x.yaml'));
  assert.ok(globMatch('decisions.md', 'DECISIONS.md'), 'case-insensitive like the server');
  assert.ok(!globMatch('artifacts/**', 'notes/artifacts.md'));
  assert.ok(!globMatch('**.md', 'assertions/note.md'), 'globstar is recursive only as a whole path segment');
});

test('ownership reads a CRLF declaration like an LF one', () => {
  const lf = 'sources:\n  - owner: AO KB\n    reason: Managed by AO KB.\n    paths:\n      - assertions/**\n      - .kb/**\n';
  assert.deepEqual(ownedPatterns(lf), ['assertions/**', '.kb/**']);
  assert.deepEqual(ownedPatterns(lf.replace(/\n/g, '\r\n')), ownedPatterns(lf));
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

test('legacy single-product KBs report knowledge supported and every model row not applicable', t => {
  const reason = "Product-model bindings need AO schemaVersion 8; this KB records schemaVersion 5, so the model stage is not applicable. Upgrade the KB with AO's own migration to enable the model.";
  const legacy = (indexReady) => () => ({ starterInstalled: true, bindingsReady: true, knowledgeReady: true, modelReady: false, indexReady, status: 'ready', dependencies: {},
    bindings: { knowledge: { ready: true, status: 'ready' }, model: { ready: false, status: 'not applicable', reason } }, diagnostics: [] });
  for (const version of [4, 5, 7]) {
    const root = fixture(t, { '.kb/config/vault.md': `---\nschemaVersion: ${version}\nproductMode: single\n---\n` });
    const row = named(root, 'schemaVersion', legacy(false));
    assert.equal(row.status, 'ok', String(version));
    assert.match(row.message, /legacy spelling adapter.*not applicable below schemaVersion 8.*AO's own migration/);
  }
  const root = fixture(t, { '.kb/config/vault.md': '---\nschemaVersion: 5\nproductMode: single\n---\n' });
  for (const name of ['model compiler dependencies', 'model bindings']) {
    const row = named(root, name, legacy(false));
    assert.equal(row.status, 'not applicable', name);
    assert.equal(row.message, reason, name);
  }
  assert.equal(named(root, 'model index', legacy(false)).status, 'pending');
  assert.equal(named(root, 'model index', legacy(true)).status, 'ok');
  assert.equal(named(root, 'knowledge admission', legacy(true)).status, 'ok');
  const report = check(root, { runCompiler: legacy(true) });
  assert.equal(report.bindingsReady, true);
  assert.equal(report.modelReady, false);
  assert.equal(report.bindings.model.status, 'not applicable');
  for (const version of [3, 9]) assert.equal(named(fixture(t, { '.kb/config/vault.md': `---\nschemaVersion: ${version}\nproductMode: single\n---\n` }), 'schemaVersion').status, 'unsupported', String(version));
  const product = named(fixture(t, { '.kb/config/vault.md': '---\nschemaVersion: 5\nproductMode: multi-nested\n---\n' }), 'schemaVersion');
  assert.equal(product.status, 'unsupported');
  assert.match(product.message, /legacy product-mode accounts are not supported.*AO's own migration/);
});

test('curation documents beyond the conflict registry need the .kb/curation/*.md include', t => {
  const broad = 'notes:\n  includes: ["**/*.md", ".kb/curation/_conflicts.md"]\n';
  const docs = { '.kb/curation/_conflicts.md': '# Conflicts\n', '.kb/curation/review-log.md': '# Log\n' };
  const missing = named(fixture(t, { ...docs, '.rhizome/config.yml': broad }), 'note includes');
  assert.equal(missing.status, 'warning');
  assert.match(missing.message, /Add the include \.kb\/curation\/\*\.md/);
  assert.match(missing.message, /curation registries and reports/);
  const covered = 'notes:\n  includes: ["**/*.md", ".kb/curation/*.md"]\n';
  assert.equal(named(fixture(t, { ...docs, '.rhizome/config.yml': covered }), 'note includes').status, 'ok');
  assert.equal(named(fixture(t, { '.kb/curation/report.md': '# Report\n', '.rhizome/config.yml': covered }), 'note includes').status, 'ok');
});

test('selected product curation needs a literal hidden-path include', t => {
  const root = fixture(t);
  cpSync(new URL('../../../../../../testdata/ao-kb-product/account/', import.meta.url), root, { recursive: true });
  const write = (path, text) => { mkdirSync(join(root, path, '..'), { recursive: true }); writeFileSync(join(root, path), text); };
  write('.rhizome/modules/ao-kb/bindings/selection.json', JSON.stringify({ formatVersion: 1, product: 'alpha' }));
  write('products/alpha/.kb/curation/report.md', '# Synthetic product review\n');
  write('products/beta/.kb/curation/report.md', '# Unselected review\n');
  const config = 'notes:\n  includes: ["**/*.md", ".kb/curation/*.md"';
  write('.rhizome/config.yml', config + ']\n');
  const missing = named(root, 'note includes');
  assert.equal(missing.status, 'warning');
  assert.match(missing.message, /products\/alpha\/\.kb\/curation\/\*\.md/);
  write('.rhizome/config.yml', config + ', "products/alpha/.kb/curation/*.md"]\n');
  assert.equal(named(root, 'note includes').status, 'ok');
  write('.rhizome/config.yml', 'notes:\n  includes: ["**/*.md", ".kb/**", "products/alpha/.kb/**"]\n');
  assert.equal(named(root, 'note includes').status, 'ok', 'literal hidden-root includes still cover curation');
});
