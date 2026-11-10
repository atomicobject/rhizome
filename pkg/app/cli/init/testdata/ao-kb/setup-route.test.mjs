// The knowledge-only setup route for legacy KBs and the site source signature (SPEC-0135 R2b).
import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmod, cp, mkdir, mkdtemp, readFile, readdir, realpath, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { knowledge, indexSignature, siteInventory } from '../../templates/starters/ao-kb/module/bindings/knowledge.mjs';
import { refresh, check } from '../../templates/starters/ao-kb/module/bindings/publish.mjs';
import { hash, readJSON } from '../../templates/starters/ao-kb/module/bindings/runtime.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '../../../../../..');
const starter = path.resolve(here, '../../templates/starters/ao-kb/module');
const legacy = path.join(repo, 'testdata/ao-kb-legacy');
const unix = { skip: process.platform === 'win32' && 'the fake rzm is a shell script' };

async function install(t, source) {
  const root = await realpath(await mkdtemp(path.join(tmpdir(), 'ao-kb-route-')));
  t.after(() => rm(root, { recursive: true, force: true }));
  await cp(source, root, { recursive: true });
  for (const dir of ['bindings', 'scripts', 'ontology']) await cp(path.join(starter, dir), path.join(root, '.rhizome/modules/ao-kb', dir), { recursive: true });
  await mkdir(path.join(root, '.rhizome/modules/ao-kb/actions'), { recursive: true });
  await writeFile(path.join(root, '.rhizome/modules/ao-kb/actions/check.yaml'), 'run: {}\n');
  return { root, directory: path.join(root, '.rhizome/modules/ao-kb/bindings') };
}

// A PATH holding node and a fake rzm only: uv is absent, and rzm records each call.
async function bin(root) {
  const dir = path.join(root, '.test-bin');
  await mkdir(dir);
  await symlink(process.execPath, path.join(dir, 'node'));
  const rzm = path.join(dir, 'rzm');
  await writeFile(rzm, `#!/bin/sh\necho "$*" >> "${dir}/rzm.log"\n[ "$1" = "--version" ] && echo "rzm 0.0.0-test"\nif [ "$1 $2" = "ontology query" ]; then\nnode -e 'const names=["assertion","source","concept","artifact","mapOfContent","submission","curationDocument","conflictRegistry"]; const text=process.argv[1]||"";const nodes=text.includes("nodes(refs:")?JSON.parse(text.slice(text.indexOf("["),text.indexOf("]")+1)).map(notePath=>({requestedRef:notePath,ref:{notePath},node:{__typename:"Note"}})):[];console.log(JSON.stringify({data:{...Object.fromEntries(names.map(n=>[n,[]])),nodes:{items:nodes,pageInfo:{truncated:false}}},extensions:{typedRoots:Object.fromEntries(names.map(n=>[n,{status:"complete"}]))}}));' "$4"\nfi\nexit 0\n`);
  await chmod(rzm, 0o755);
  return { env: { PATH: dir, HOME: root }, log: () => readFile(path.join(dir, 'rzm.log'), 'utf8').catch(() => '') };
}
function node(root, env, ...args) {
  const result = spawnSync(process.execPath, args, { cwd: root, env, input: '{}', encoding: 'utf8' });
  try { return { ...JSON.parse(result.stdout), exit: result.status }; }
  catch { throw new Error(`${args.join(' ')} printed no JSON: ${result.stdout}${result.stderr}`); }
}
const cli = (root, env, ...args) => node(root, env, '.rhizome/modules/ao-kb/bindings/cli.mjs', ...args);
const rows = report => Object.fromEntries(report.checks.map(c => [c.name, c]));
async function authored(root) {
  const files = {};
  for (const name of await readdir(root, { recursive: true, withFileTypes: true })) {
    const full = path.join(name.parentPath, name.name);
    const rel = path.relative(root, full).split(path.sep).join('/');
    if (name.isFile() && !rel.startsWith('.rhizome/modules/') && !rel.startsWith('.test-bin/') && rel !== '.rhizome/config.yml') files[rel] = hash(await readFile(full));
  }
  return files;
}

test('a schemaVersion 5 KB sets up knowledge-only, without uv, and its files never change', unix, async t => {
  const { root, directory } = await install(t, legacy);
  const { env, log } = await bin(root);
  const before = await authored(root);
  // The fixture is an installation from before editing was enabled: it still carries the
  // starter's former read-only declaration, which refresh lifts and nothing else touches.
  assert.ok(before['.rhizome/source-ownership.yml'], 'the fixture carries the former declaration');
  assert.equal(rows(node(root, env, '.rhizome/modules/ao-kb/scripts/check.mjs'))['source ownership'].status, 'warning');

  const refreshed = cli(root, env, 'refresh-bindings', '--single');
  assert.equal(refreshed.exit, 0, JSON.stringify(refreshed));
  assert.equal(refreshed.bindingsReady, true);
  assert.equal(refreshed.knowledgeReady, true);
  assert.equal(refreshed.modelReady, false, 'refresh reports the computed model state, not a fixed ready');
  assert.equal(refreshed.bindings.model.status, 'not applicable');
  assert.equal(refreshed.indexReady, false);
  assert.deepEqual(refreshed.dependencies, {}, 'no model dependency is looked up');
  const model = await readJSON(path.join(directory, 'manifest.json'));
  assert.equal(model.status, 'not applicable');
  assert.match(model.reason, /schemaVersion 5.*AO's own migration/);
  assert.deepEqual(model.generatedFiles, [], 'no model generation is published');
  await assert.rejects(readFile(path.join(root, '.rhizome/modules/ao-kb/ontology/ao-model.generated.graphql')), { code: 'ENOENT' });
  const scope = await readJSON(path.join(directory, 'knowledge-manifest.json'));
  assert.equal(scope.context.profile, 'product-discovery');
  assert.deepEqual(scope.diagnostics.map(d => d.code), ['ao_ownership_lifted']);
  await assert.rejects(readFile(path.join(root, '.rhizome/source-ownership.yml')), { code: 'ENOENT' }, 'the untouched former declaration lifts to no file');
  delete before['.rhizome/source-ownership.yml'];
  assert.deepEqual(scope.inputs.find(i => i.path === '.kb/config/manifest.json').hash, hash(await readFile(path.join(root, '.kb/config/manifest.json'))), 'a scaffold manifest without siteEngine stays a fingerprint input');

  await writeFile(path.join(root, '.rhizome/config.yml'), 'notes:\n  includes:\n    - "**/*.md"\n');
  let report = node(root, env, '.rhizome/modules/ao-kb/scripts/check.mjs');
  let named = rows(report);
  assert.equal(report.bindingsReady, true);
  assert.equal(report.indexReady, false);
  assert.equal(named.schemaVersion.status, 'ok');
  assert.match(named.schemaVersion.message, /legacy spelling adapter.*not applicable below schemaVersion 8.*AO's own migration/);
  assert.equal(named['source ownership'].status, 'ok');
  assert.match(named['source ownership'].message, /No source ownership declaration/);
  for (const name of ['model compiler dependencies', 'model bindings']) {
    assert.equal(named[name].status, 'not applicable', name);
    assert.match(named[name].message, /schemaVersion 5/, name);
  }
  assert.equal(named['model index'].status, 'pending');
  assert.match(named['model index'].message, /knowledge index receipt/);
  assert.equal(named['note includes'].status, 'warning', 'curation documents beyond the registry need their include');
  assert.match(named['note includes'].message, /\.kb\/curation\/\*\.md/);
  await writeFile(path.join(root, '.rhizome/config.yml'), 'notes:\n  includes:\n    - "**/*.md"\n    - ".kb/curation/*.md"\n');
  assert.equal(rows(node(root, env, '.rhizome/modules/ao-kb/scripts/check.mjs'))['note includes'].status, 'ok');

  const indexed = cli(root, env, 'index');
  assert.equal(indexed.exit, 0, JSON.stringify(indexed));
  assert.equal(indexed.indexReady, true);
  assert.match(await log(), /^index$/m, 'the index command runs real indexing');
  const receipt = await readJSON(path.join(directory, 'index-receipt.json'));
  assert.equal(receipt.model, 'not applicable');
  assert.equal(receipt.knowledgeFingerprint, scope.fingerprint);
  assert.equal(receipt.sourceSignature, await indexSignature(root, null, null));

  report = node(root, env, '.rhizome/modules/ao-kb/scripts/check.mjs');
  named = rows(report);
  assert.equal(report.bindingsReady, true);
  assert.equal(report.indexReady, true);
  assert.equal(named['model index'].status, 'ok');
  assert.doesNotMatch(await log(), /uv/);
  assert.deepEqual(await authored(root), before, 'no other file in the KB changes');
});

test('the site source signature stales on every bound document and an underscore-named artifact (schemaVersion 5)', unix, async t => {
  const { root, directory } = await install(t, legacy);
  const { env } = await bin(root);
  assert.equal(cli(root, env, 'refresh-bindings', '--single').exit, 0);
  const ready = () => node(root, env, '.rhizome/modules/ao-kb/scripts/check.mjs').indexReady;
  assert.equal(cli(root, env, 'index').indexReady, true);
  const file = name => path.join(root, name);
  // An ordinary note outside the site inventory never stales it.
  await mkdir(file('notes'), { recursive: true });
  await writeFile(file('notes/team.md'), '# Team\n');
  assert.equal(ready(), true);
  for (const [label, change] of [
    ['edit a map of content', () => writeFile(file('_moc.md'), '# Map of Content — edited\n')],
    ['add a map of content', () => writeFile(file('MOCs/ferries.md'), '# Ferries\n')],
    ['delete a submission', () => rm(file('_improvements/add-text-alerts.md'))],
    ['add a nested submission', () => mkdir(file('_improvements/new-idea'), { recursive: true }).then(() => writeFile(file('_improvements/new-idea/new-idea.md'), '---\nstatus: pending\n---\n'))],
    ['edit a curation document', () => writeFile(file('.kb/curation/review-log.md'), '---\nupdatedAt: "2026-06-01"\n---\n# Review log\n')],
    ['add a curation document', () => writeFile(file('.kb/curation/new-report.md'), '# New report\n')],
    ['edit an underscore-named artifact', () => writeFile(file('artifacts/plans/_working-notes.md'), '# Working notes, edited\n')],
  ]) {
    await change();
    assert.equal(ready(), false, label);
    assert.equal(cli(root, env, 'index').indexReady, true, `${label}: reindexing restores indexReady`);
    assert.equal(ready(), true, label);
  }
  // Archived submissions and raw sources are outside the inventory.
  await writeFile(file('_improvements/_archive/later.md'), '---\nstatus: applied\n---\n');
  await mkdir(file('sources/_raw/more'), { recursive: true });
  await writeFile(file('sources/_raw/more/raw.md'), 'raw\n');
  assert.equal(ready(), true);
  assert.equal((await readJSON(path.join(directory, 'manifest.json'))).status, 'not applicable');
});

test('the site source signature covers the model sources and the site inventory at schemaVersion 8', async t => {
  const model = path.join(repo, 'testdata/ao-kb-model');
  const { root, directory } = await install(t, path.join(model, 'account'));
  const recorded = JSON.parse(await readFile(path.join(model, 'intermediate.json'), 'utf8'));
  const exportModel = async () => {
    const ir = structuredClone(recorded);
    for (const input of ir.inputs) {
      try { input.hash = hash(await readFile(path.join(root, input.path))); }
      catch (error) { if (error.code === 'ENOENT') input.hash = null; else throw error; }
    }
    return ir;
  };
  const options = { root, directory, selection: { formatVersion: 1, product: 'alpha', toolkit: null }, exportModel, validate: async () => {} };
  const deps = async () => ({ uv: { ready: true }, rzm: { ready: true } });
  await refresh(options);
  const receipt = async () => {
    const m = await readJSON(path.join(directory, 'manifest.json'));
    const k = await readJSON(path.join(directory, 'knowledge-manifest.json'));
    await writeFile(path.join(directory, 'index-receipt.json'), JSON.stringify({ fingerprint: m.fingerprint, sdlHash: m.sdlHash, knowledgeFingerprint: k.fingerprint,
      sourceSignature: await indexSignature(root, await exportModel(), 'alpha') }));
  };
  const ready = async () => (await check({ ...options, checkDependencies: deps })).indexReady;
  await receipt();
  assert.equal(await ready(), true);
  // An old receipt that signed only the adapter's sources expires once.
  const old = await readJSON(path.join(directory, 'index-receipt.json'));
  const files = [...new Set([...recorded.sources, ...recorded.knowledgeSources].map(s => s.path))].sort();
  const revisions = [];
  for (const name of files) revisions.push([name, await readFile(path.join(root, name)).then(hash, () => null)]);
  await writeFile(path.join(directory, 'index-receipt.json'), JSON.stringify({ ...old, sourceSignature: hash(JSON.stringify(revisions, null, 2) + '\n') }));
  assert.equal(await ready(), false, 'a receipt signed before the site inventory existed asks for one reindex');
  await receipt();
  for (const [label, name, stales] of [
    ['a new account map of content', '_moc.md', true],
    ['a selected product submission', 'products/alpha/_improvements/idea/idea.md', true],
    ['a curation document', '.kb/curation/log.md', true],
    ['an underscore-named artifact', 'artifacts/_scratch.md', true],
    ['another product\'s map of content', 'products/beta/MOCs/beta.md', false],
  ]) {
    await mkdir(path.dirname(path.join(root, name)), { recursive: true });
    await writeFile(path.join(root, name), '---\nstatus: pending\n---\n# Added\n');
    assert.equal(await ready(), !stales, label);
    await receipt();
    assert.equal(await ready(), true, `${label}: reindexing restores indexReady`);
  }
});

test('the inventory follows the type and admission path rules, as a deliberate superset', async () => {
  const files = await siteInventory(legacy, null);
  for (const name of ['_moc.md', 'MOCs/routes.md', 'artifacts/plans/_working-notes.md', '_improvements/how-to-submit.md',
    '_improvements/split-timetable/attachments/draft-table.md', '.kb/curation/_conflicts.md', '.kb/curation/vocabulary-report.md']) {
    assert.ok(files.includes(name), name);
  }
  for (const name of ['_improvements/_archive/old-request.md', '_improvements/_pending-attachments/photo-notes.md', 'README.md', '.kb/config/vault.md']) {
    assert.ok(!files.includes(name), name);
  }
  assert.ok(!files.some(name => name.startsWith('sources/_raw/')));
  assert.deepEqual(files, [...files].sort());
});

test('legacy admission covers single-product schemaVersion 4 through 8 and refuses the rest', async t => {
  const { root, directory } = await install(t, legacy);
  const vault = path.join(root, '.kb/config/vault.md');
  const original = await readFile(vault, 'utf8');
  const single = { formatVersion: 1, product: null };
  for (const version of [4, 5, 7]) {
    await writeFile(vault, original.replace('schemaVersion: 5', `schemaVersion: ${version}`));
    assert.equal((await knowledge(root, directory, single)).schemaVersion, version);
  }
  for (const version of [3, 9]) {
    await writeFile(vault, original.replace('schemaVersion: 5', `schemaVersion: ${version}`));
    await assert.rejects(knowledge(root, directory, single), /ao_schema_unsupported/);
  }
  await writeFile(vault, original.replace(/^productMode: .*$/m, 'productMode: multi-nested'));
  await assert.rejects(knowledge(root, directory, single), /ao_schema_unsupported:.*legacy product-mode accounts are not supported.*AO's own migration/);
  await writeFile(vault, original);
  await assert.rejects(knowledge(root, directory, { formatVersion: 1, product: 'harbor' }), /ao_schema_unsupported/);
  // A scaffold manifest without siteEngine reads as absent, but a recorded engine must match the pin.
  await writeFile(path.join(root, '.kb/config/manifest.json'), JSON.stringify({ toolkit: { version: '4.2.0' }, siteEngine: { version: '9.0.0' } }));
  await assert.rejects(knowledge(root, directory, single), /ao_toolkit_pin/);
});

test('a profile the scalar reader refuses is omitted with a note and never fails refresh', async t => {
  const { root, directory } = await install(t, legacy);
  const vault = path.join(root, '.kb/config/vault.md');
  await writeFile(vault, (await readFile(vault, 'utf8')).replace('profile: "product-discovery"', 'profile: [a, b]'));
  const scope = await knowledge(root, directory, { formatVersion: 1, product: null });
  assert.equal(scope.context.profile, undefined);
  assert.equal(scope.notes[0].code, 'ao_profile_unreadable');
  const manifest = await refresh({ root, directory, selection: { formatVersion: 1, product: null, toolkit: null }, validate: async () => {} });
  assert.equal(manifest.status, 'not applicable');
  assert.equal((await readJSON(path.join(directory, 'knowledge-manifest.json'))).diagnostics[0].code, 'ao_profile_unreadable');
});
