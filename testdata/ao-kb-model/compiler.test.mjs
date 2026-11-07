import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile, writeFile, mkdir, mkdtemp, cp, rm, rename, realpath} from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {render} from '../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/render.mjs';
import {refresh, check} from '../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/publish.mjs';
import {hash, readJSON} from '../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/runtime.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const compiler = path.resolve(here, '../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings');
const recorded = JSON.parse(await readFile(path.join(here, 'intermediate.json')));
const expected = await readFile(path.join(here, 'expected.graphql'), 'utf8');
const selection = {formatVersion: 1, product: 'alpha', toolkit: null};
async function fixture(t) {
  const root = await realpath(await mkdtemp(path.join(os.tmpdir(), 'aokb-laneM2-publish-')));
  t.after(() => rm(root, {recursive: true, force: true}));
  await cp(path.join(here, 'account'), root, {recursive: true});
  const directory = path.join(root, '.rhizome/modules/ao-kb/bindings');
  await cp(compiler, directory, {recursive: true});
  await cp(path.join(compiler, '../scripts'), path.join(directory, '../scripts'), {recursive: true});
  const exportModel = async () => {
    const ir = structuredClone(recorded);
    for (const input of ir.inputs) {
      try { input.hash = hash(await readFile(path.join(root, input.path))); }
      catch (error) { if (error.code === 'ENOENT') input.hash = null; else throw error; }
    }
    return ir;
  };
  return {root, directory, selection, exportModel, validate: async () => {}};
}
const deps = async () => ({uv: {ready: true}, rzm: {ready: true}});

test('renderer is deterministic and matches the SDL compiled by Go', () => {
  assert.equal(render(recorded).sdl, expected);
  assert.deepEqual(render(JSON.parse(JSON.stringify(recorded))), render(recorded));
  assert.ok(!expected.includes('products/beta'));
  assert.match(expected, /membership: \{path: "products", includes: "alpha", includeMissing: false, wildcard: "\*"\}/);
  assert.match(expected, /sourceOrder: 0/);
  assert.ok(recorded.oracle.rows.service.SHARED);
  assert.ok(!recorded.oracle.rows.user.HIDDEN);
  assert.deepEqual(Object.keys(recorded.oracle.rows.position), ['1', '2']);
});

test('authored edits stay ready; config edits need refresh; index readiness stays separate', async t => {
  const options = await fixture(t);
  let report = await check({...options, checkDependencies: deps});
  assert.equal(report.bindingsReady, false);
  await refresh(options);
  report = await check({...options, checkDependencies: deps});
  assert.equal(report.bindingsReady, true);
  assert.equal(report.indexReady, false);
  const model = path.join(options.root, 'products/alpha/artifacts/_assets/_model/lane.yaml');
  await writeFile(model, (await readFile(model, 'utf8')) + '\n# input changed\n');
  report = await check({...options, checkDependencies: deps});
  assert.equal(report.bindingsReady, true);
  // rzm index re-serializes .rhizome/config.yml; the bindings do not depend on its bytes.
  await mkdir(path.join(options.root, '.rhizome'), {recursive: true});
  await writeFile(path.join(options.root, '.rhizome/config.yml'), 'notes:\n  includes:\n    - "**/*.md"\n');
  assert.equal((await check({...options, checkDependencies: deps})).bindingsReady, true, 'config rewrite must not stale bindings');
  const config = path.join(options.root, 'products/alpha/.kb/config/metamodel.yaml');
  await writeFile(config, (await readFile(config, 'utf8')) + '\n# configuration changed\n');
  report = await check({...options, checkDependencies: deps});
  assert.equal(report.bindingsReady, false);
  assert.equal(report.status, 'stale');
  await refresh(options);
  assert.equal((await check({...options, checkDependencies: deps})).bindingsReady, true);
});

test('validation failure preserves old SDL, selection, and ownership while marking stale', async t => {
  const options = await fixture(t);
  await refresh(options);
  const sdl = path.join(options.directory, '../ontology/ao-model.generated.graphql');
  const before = await readFile(sdl);
  const owner = await readFile(path.join(options.root, '.rhizome/source-ownership.yml'));
  let validations = 0;
  await assert.rejects(refresh({...options, validate: async () => { if (++validations === 2) throw new Error('bad schema'); }}), /bad schema/);
  assert.deepEqual(await readFile(sdl), before);
  assert.equal((await readJSON(path.join(options.directory, 'manifest.json'))).status, 'stale');
  assert.deepEqual(await readFile(path.join(options.root, '.rhizome/source-ownership.yml')), owner);
  assert.deepEqual(await readJSON(path.join(options.directory, 'selection.json')), selection);
});

test('a failure at either live rename rolls the pair back', async t => {
  for (const failTarget of ['ao-model.generated.graphql', 'bindings/manifest.json']) {
    const options = await fixture(t);
    await refresh(options);
    const old = await readJSON(path.join(options.directory, 'manifest.json'));
    const file = path.join(options.directory, '../ontology/ao-model.generated.graphql');
    const before = await readFile(file);
    await assert.rejects(refresh({...options, renameFile: async (...args) => {
      if (args[1].endsWith(failTarget)) throw new Error('injected disk failure');
      await rename(...args);
    }}), /injected disk failure/);
    assert.deepEqual(await readFile(file), before);
    const current = await readJSON(path.join(options.directory, 'manifest.json'));
    assert.equal(current.sdlHash, old.sdlHash);
    assert.equal(current.status, 'stale');
  }
});

test('mid-compile input change, concurrent refresh, and initial failure never publish ready', async t => {
  const options = await fixture(t);
  const change = path.join(options.root, 'products/alpha/.kb/config/metamodel.yaml');
  await assert.rejects(refresh({...options, beforePublish: async () => writeFile(change, 'changed')}), /ao_input_changed/);
  assert.equal((await readJSON(path.join(options.directory, 'manifest.json'))).status, 'error');
  assert.equal((await readJSON(path.join(options.directory, 'knowledge-manifest.json'))).status, 'ready');
  await mkdir(path.join(options.directory, 'refresh.lock'));
  await assert.rejects(refresh(options), /ao_refresh_locked/);
});

test('mixed origin and incompatible intermediate declarations fail before rendering', () => {
  const bad = structuredClone(recorded);
  bad.diagnostics.push({code: 'ao_mixed_origin_record', severity: 'error', kind: 'map', key: 'overlap',
    paths: ['artifacts/_assets/_model/lane.yaml', 'products/alpha/artifacts/_assets/_model/lane.yaml']});
  assert.throws(() => render(bad), error => error.diagnostics[1].code === 'ao_mixed_origin_record');
  bad.formatVersion = 2;
  assert.throws(() => render(bad), /ao_intermediate_version/);
});

test('source and compiler tampering, missing dependency, and stale manifest fail the check', async t => {
  const options = await fixture(t);
  await refresh(options);
  await writeFile(path.join(options.directory, 'render.mjs'), '// modified compiler');
  assert.equal((await check({...options, checkDependencies: deps})).bindingsReady, false);
  const result = await check({...options, checkDependencies: async () => ({uv: {ready: false}})});
  assert.equal(result.diagnostics[0].code, 'ao_dependency_missing');
  assert.equal(result.status, 'dependency missing', 'previous bindings exist, but a missing dependency is not staleness');
});

test('the check names the actual model state instead of calling every failure stale', async t => {
  const options = await fixture(t);
  await refresh(options);
  const unavailable = async () => { throw Object.assign(new Error('ao_dependency_unavailable: uv could not prepare packages'),
    {diagnostics: [{code: 'ao_dependency_unavailable', severity: 'error', message: 'ao_dependency_unavailable: uv could not prepare packages'}]}); };
  let report = await check({...options, exportModel: unavailable, checkDependencies: deps});
  assert.equal(report.status, 'dependency missing');
  assert.equal(report.bindingsReady, false);
  const pinned = async () => { throw new Error('ao_toolkit_pin: manifest siteEngine.version is not the supported baseline'); };
  report = await check({...options, exportModel: pinned, checkDependencies: deps});
  assert.equal(report.status, 'error');
  // An unsupported schema is an error even though the same edit also stales the model.
  await writeFile(path.join(options.root, '.kb/config/vault.md'), '---\nschemaVersion: 7\nproductMode: multi-nested\nproducts: [alpha, beta]\n---\n');
  report = await check({...options, checkDependencies: deps});
  assert.ok(report.diagnostics.some(d => d.code === 'ao_bindings_stale'));
  assert.equal(report.status, 'error');
});

test('a uv environment failure is reported as a missing dependency', {skip: process.platform === 'win32'}, async t => {
  const {adapter} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/runtime.mjs');
  const bin = await mkdtemp(path.join(os.tmpdir(), 'aokb-fake-uv-'));
  t.after(() => rm(bin, {recursive: true, force: true}));
  const uv = path.join(bin, 'uv');
  await writeFile(uv, '#!/bin/sh\necho "error: No solution found; the network was disabled" >&2\nexit 1\n', {mode: 0o755});
  const saved = process.env.PATH;
  process.env.PATH = `${bin}${path.delimiter}${saved}`;
  t.after(() => { process.env.PATH = saved; });
  await assert.rejects(adapter(bin, compiler, selection, {offline: true}),
    error => error.diagnostics?.[0]?.code === 'ao_dependency_unavailable' && /network was disabled/.test(error.message));
  // A crash inside the adapter is the adapter's error, not a missing dependency.
  await writeFile(uv, '#!/bin/sh\necho "Traceback (most recent call last):" >&2\nexit 1\n', {mode: 0o755});
  await assert.rejects(adapter(bin, compiler, selection, {offline: true}), error => !error.diagnostics && /Traceback/.test(error.message));
});

test('index certification rejects an index that silently omitted native sources', async () => {
  const {verifyIndex} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/verify-index.mjs');
  const ir = {sources: [{kind: 'row', compositionOrigin: 'product', path: 'rows.yaml'}], records: [{kind: 'row', path: 'rows.yaml'}]};
  const complete = rows => ({data: {model0: rows}, extensions: {typedRoots: {model0: {status: 'complete'}}}});
  await assert.rejects(verifyIndex('.', ir, async () => complete([])), /ao_index_projection_incomplete/);
  await verifyIndex('.', ir, async () => complete([{nodeId: 'one', notePath: 'rows.yaml'}]));
  await assert.rejects(verifyIndex('.', ir, async () => ({data: {model0: []}})), /ao_index_coverage_missing/);
});

test('index certification follows typed-root pagination and rejects repeated nodes', async () => {
  const {verifyIndex} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/verify-index.mjs');
  const ir = {sources: [{kind: 'row', compositionOrigin: 'product', path: 'rows.yaml'}], records: [{kind: 'row', path: 'rows.yaml'}, {kind: 'row', path: 'rows.yaml'}]};
  let calls = 0;
  await verifyIndex('.', ir, async text => {
    const page = calls++;
    assert.match(text, new RegExp(`offset: ${page}`));
    return {data: {model0: [{nodeId: String(page), notePath: 'rows.yaml'}]}, extensions: {typedRoots: {model0: page ? {status: 'complete'} : {status: 'capped', nextOffset: 1}}}};
  });
  assert.equal(calls, 2);
});

test('mapping key scope restarts at a declared container and group fallback stays terminal', () => {
  const ir = structuredClone(recorded);
  const parent = ir.kinds.find(k => k.name === 'service');
  parent.selector = 'groups{}';
  const child = ir.kinds.find(k => k.name === 'job');
  child.selector = 'groups{}.jobs';
  child.declaration.key = 'id';
  const grouped = ir.kinds.find(k => k.name === 'group');
  grouped.selector = 'outer{}[].items';
  const sdl = render(ir).sdl;
  const job = sdl.slice(sdl.indexOf('type AOKBModel_job_product'), sdl.indexOf('type AOKBModel_link_product'));
  assert.match(job, /key: \{field: "id"\}/);
  const group = sdl.slice(sdl.indexOf('type AOKBModel_group_product'), sdl.indexOf('interface AOKBModel_job'));
  assert.match(group, /key: \{mappingKey: true\}/);
  assert.ok(!group.includes('fallback: MAPPING_KEY'));
});

test('standard model contracts expose native evidence, file sync, labels, and exact node mounts', async () => {
  const ir = JSON.parse(await readFile(path.resolve(here, '../ao-kb/model-intermediate.json')));
  const result = render(ir);
  assert.match(result.sdl, /targetKey: \["\$fileStem", "aoId"\], mode: STRICT, values: ELEMENTS, allowWildcard: false, edge: \{name: "evidenced-by", direction: OUTBOUND\}/);
  assert.match(result.sdl, /implements AOKBModel_feature & AOKBModelRecord @node\(locator: EMBEDDED, label: "Feature"\)/);
  assert.match(result.sdl, /type AOKBModelFile[\s\S]*select: ""[\s\S]*synced: String @field\(path: "synced"\)/);
  assert.match(result.views['AOKBModel_feature_single.yaml'], /entry: model.tsx[\s\S]*kind: node, type: AOKBModel_feature_single, default: true/);
  const unsupported = structuredClone(ir);
  unsupported.kinds.find(k => k.name === 'assertion').declaration.file = 'kb:concepts';
  assert.throws(() => render(unsupported), /ao_knowledge_target_unsupported/);
});

test('a failed product switch keeps the previous product complete instead of mixing contexts', async t => {
  const options = await fixture(t);
  await refresh(options);
  const scope = path.join(options.directory, '../ontology/ao-knowledge.generated.graphql');
  const before = await readFile(scope, 'utf8');
  const beta = {...selection, product: 'beta'};
  await assert.rejects(refresh({...options, selection: beta, exportModel: async () => { throw new Error('uv unavailable'); }}), /product switch not applied/);
  assert.equal(await readFile(scope, 'utf8'), before, 'alpha admission retained');
  assert.equal((await readJSON(path.join(options.directory, 'selection.json'))).product, 'alpha');
  assert.equal((await readJSON(path.join(options.directory, 'knowledge-manifest.json'))).context.product, 'alpha');
  assert.equal((await readJSON(path.join(options.directory, 'manifest.json'))).status, 'stale');
});

test('knowledge admission is generated without Python and survives a model dependency failure', async t => {
  const options = await fixture(t);
  await refresh(options);
  const model = await readFile(path.join(options.directory, '../ontology/ao-model.generated.graphql'));
  await assert.rejects(refresh({...options, exportModel: async () => { throw new Error('uv unavailable'); }}), error => error.knowledgeReady && /uv unavailable/.test(error.message));
  const knowledge = await readFile(path.join(options.directory, '../ontology/ao-knowledge.generated.graphql'), 'utf8');
  assert.match(knowledge, /extend type Assertion @admit/);
  assert.match(knowledge, /has:products/);
  assert.deepEqual(await readFile(path.join(options.directory, '../ontology/ao-model.generated.graphql')), model);
  const report = await check({...options, checkDependencies: async () => ({uv: {ready: false}})});
  assert.equal(report.knowledgeReady, true);
  assert.equal(report.modelReady, false);
});

test('ownership is enforced before publication and source compatibility is distinct from staleness', async t => {
  const options = await fixture(t);
  await refresh(options);
  const before = await readFile(path.join(options.directory, '../ontology/ao-model.generated.graphql'));
  const incompatible = async () => {
    const ir = await options.exportModel();
    ir.diagnostics.push({code: 'ao_duplicate_record', severity: 'error', kind: 'service', key: 'S1', message: 'Duplicate source records'});
    return ir;
  };
  const report = await check({...options, exportModel: incompatible, checkDependencies: deps});
  assert.equal(report.bindings.model.status, 'incomplete');
  assert.equal(report.status, 'duplicate IDs');
  assert.ok(report.diagnostics.some(d => d.code === 'ao_duplicate_record'));
  await assert.rejects(refresh({...options, exportModel: async () => {
    const ir = await options.exportModel();
    ir.sources[0].path = 'outside/model.yaml';
    return ir;
  }}), /ao_ownership_missing:.*outside\/model.yaml/);
  assert.deepEqual(await readFile(path.join(options.directory, '../ontology/ao-model.generated.graphql')), before);
});

test('model publication removes disappeared kind views and keeps authored presentation entries', async t => {
  const options = await fixture(t);
  await refresh(options);
  const entry = path.join(options.directory, '../views/knowledge/model.tsx');
  await writeFile(entry, '// authored presentation');
  const generated = path.join(options.directory, '../views/knowledge/AOKBModel_group_product.yaml');
  assert.ok(await readFile(generated));
  await refresh({...options, exportModel: async () => {
    const ir = await options.exportModel();
    ir.sources = ir.sources.filter(s => s.kind !== 'group');
    ir.kinds = ir.kinds.filter(k => k.name !== 'group');
    ir.records = ir.records.filter(r => r.kind !== 'group');
    return ir;
  }});
  await assert.rejects(readFile(generated), {code: 'ENOENT'});
  assert.equal(await readFile(entry, 'utf8'), '// authored presentation');
});

test('product preflight and checked admission match AO registry and folder selection', async () => {
  const {knowledge} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/knowledge.mjs');
  const root = await realpath(path.resolve(here, '../ao-kb-product/account'));
  const selected = {formatVersion: 1, product: 'alpha'};
  await assert.rejects(knowledge(root, compiler, null), /ao_selection_required:.*--product <slug>/);
  await assert.rejects(knowledge(root, compiler, {formatVersion: 1, product: null}), /ao_product_required/);
  const result = await knowledge(root, compiler, selected);
  assert.equal(result.sdl, await readFile(path.resolve(here, '../ao-kb-product/expected-knowledge.graphql'), 'utf8'));
  assert.match(result.sdl, /artifacts\/01_define\/alpha/);
  assert.match(result.sdl, /NOT assertions\/beta/);
  const single = await knowledge(await realpath(path.resolve(here, '../ao-kb/booking')), compiler, {formatVersion: 1, product: null});
  assert.doesNotMatch(single.sdl, /extend type/);
});

test('an authored edit expires the index receipt without expiring bindings', async t => {
  const options = await fixture(t);
  await refresh(options);
  const {indexSignature} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/runtime.mjs');
  const manifest = await readJSON(path.join(options.directory, 'manifest.json'));
  const receipt = {fingerprint: manifest.fingerprint, sdlHash: manifest.sdlHash,
    knowledgeFingerprint: (await readJSON(path.join(options.directory, 'knowledge-manifest.json'))).fingerprint,
    sourceSignature: await indexSignature(options.root, await options.exportModel())};
  await writeFile(path.join(options.directory, 'index-receipt.json'), JSON.stringify(receipt));
  assert.equal((await check({...options, checkDependencies: deps})).indexReady, true);
  const source = path.join(options.root, 'products/alpha/artifacts/_assets/_model/lane.yaml');
  await writeFile(source, (await readFile(source, 'utf8')) + '\n# an ordinary AO edit\n');
  const report = await check({...options, checkDependencies: deps});
  assert.equal(report.bindingsReady, true);
  assert.equal(report.indexReady, false);
});

test('Node preflight refuses incompatible schema, registry, markers, stamp, and ownership', async t => {
  for (const [file, bytes, code] of [
    ['.kb/config/vault.md', '---\nschemaVersion: 7\n---\n', 'ao_schema_unsupported'],
    ['products/alpha/.kb/config/product.yaml', 'slug: beta\naccount: ../..\n', 'ao_product_invalid'],
    ['products/orphan/.kb/config/product.yaml', 'slug: orphan\naccount: ../..\n', 'ao_product_unregistered'],
    ['products/alpha/.kb/config/manifest.json', '{"siteEngine":{"version":"unsupported"}}', 'ao_toolkit_pin'],
    ['.rhizome/source-ownership.yml', 'sources:\n  - owner: AO KB\n    reason: AO\n    paths: ["artifacts/**"]\n', 'ao_ownership_missing'],
    ['.rhizome/source-ownership.yml', 'sources:\n  - owner: AO KB\n reason: invalid indentation\n    paths: ["artifacts/**", "assertions/**", "sources/**", "concepts/**", ".kb/**", "products/*/artifacts/**"]\n', 'ao_ownership_missing'],
  ]) {
    const options = await fixture(t);
    const full = path.join(options.root, file);
    await mkdir(path.dirname(full), {recursive: true});
    await writeFile(full, bytes);
    let called = false;
    await assert.rejects(refresh({...options, exportModel: async () => {called = true; return options.exportModel();}}), new RegExp(code));
    assert.equal(called, false, 'preflight runs before Python/model export');
    await assert.rejects(readFile(path.join(options.directory, '../ontology/ao-knowledge.generated.graphql')), {code: 'ENOENT'});
  }
});

test('a vault.md without schemaVersion is current, as AO reads it', async t => {
  const options = await fixture(t);
  const vault = path.join(options.root, '.kb/config/vault.md');
  await writeFile(vault, (await readFile(vault, 'utf8')).replace(/^schemaVersion:.*\n/m, ''));
  assert.doesNotMatch(await readFile(vault, 'utf8'), /schemaVersion/);
  await refresh(options);
  assert.equal((await check({...options, checkDependencies: deps})).bindingsReady, true);
});

test('a tampered generated-file inventory cannot delete authored files', async t => {
  const options = await fixture(t);
  await refresh(options);
  const authored = path.join(options.root, 'assertions/shared.md');
  const before = await readFile(authored);
  const file = path.join(options.directory, 'manifest.json');
  const manifest = await readJSON(file);
  manifest.generatedFiles.push('../../../../assertions/shared.md');
  await writeFile(file, JSON.stringify(manifest));
  await assert.rejects(refresh(options), /ao_generated_path_invalid/);
  assert.deepEqual(await readFile(authored), before);
});

test('missing executables have an actionable dependency diagnostic', async () => {
  const {run} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/runtime.mjs');
  assert.throws(() => run('/aokb-synthetic-missing-rzm', ['--version']), /ao_dependency_missing: aokb-synthetic-missing-rzm/);
});

test('missing Rhizome keeps knowledge readiness separate from model prerequisites', async t => {
  const options = await fixture(t);
  await refresh(options);
  const report = await check({...options, checkDependencies: async () => ({rzm: {ready: false}})});
  assert.equal(report.knowledgeReady, true);
  assert.equal(report.modelReady, false);
  assert.ok(report.diagnostics.some(d => d.code === 'ao_dependency_missing'));
});

test('ordinary knowledge edits stay ready while a new artifact phase requires admission refresh', async t => {
  const options = await fixture(t);
  await refresh(options);
  const {indexSignature} = await import('../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/runtime.mjs');
  const model = await readJSON(path.join(options.directory, 'manifest.json'));
  const scope = await readJSON(path.join(options.directory, 'knowledge-manifest.json'));
  const receipt = {fingerprint: model.fingerprint, knowledgeFingerprint: scope.fingerprint, sdlHash: model.sdlHash, sourceSignature: await indexSignature(options.root, await options.exportModel())};
  await writeFile(path.join(options.directory, 'index-receipt.json'), JSON.stringify(receipt));
  assert.equal((await check({...options, checkDependencies: deps})).indexReady, true);
  const note = path.join(options.root, 'assertions/shared.md');
  await writeFile(note, (await readFile(note, 'utf8')) + '\nOrdinary authored evidence edit.\n');
  assert.equal((await check({...options, checkDependencies: deps})).bindingsReady, true);
  receipt.sourceSignature = await indexSignature(options.root, await options.exportModel());
  await writeFile(path.join(options.directory, 'index-receipt.json'), JSON.stringify(receipt));
  assert.equal((await check({...options, checkDependencies: deps})).indexReady, true);
  await mkdir(path.join(options.root, 'artifacts/01_define/alpha'), {recursive: true});
  const stale = await check({...options, checkDependencies: deps});
  assert.equal(stale.knowledgeReady, false);
  assert.equal(stale.modelReady, true);
  assert.equal(stale.status, 'stale');
  assert.ok(stale.diagnostics.some(d => d.code === 'ao_artifact_phases_stale' && d.message.includes('01_define')));
  await refresh(options);
  const final = await check({...options, checkDependencies: deps});
  assert.equal(final.bindingsReady, true);
  assert.equal(final.indexReady, false, 'a new admission generation requires a new index receipt');
});

test('a knowledge-only single-product KB without a site engine pin publishes knowledge admission', async t => {
  const options = await fixture(t);
  await rm(path.join(options.root, 'products'), {recursive: true, force: true});
  await rm(path.join(options.root, '.kb/config/manifest.json'));
  await writeFile(path.join(options.root, '.kb/config/vault.md'), '---\nschemaVersion: 8\nproductMode: single\nproducts: ["solo"]\n---\n');
  const single = {formatVersion: 1, product: null, toolkit: null};
  const reached = async () => { throw new Error('model stage reached'); };
  await assert.rejects(refresh({...options, selection: single, exportModel: reached}), error => error.knowledgeReady && /model stage reached/.test(error.message));
  const saved = await readJSON(path.join(options.directory, 'knowledge-manifest.json'));
  assert.equal(saved.status, 'ready');
  assert.deepEqual(saved.inputs.find(input => input.path === '.kb/config/manifest.json'), {path: '.kb/config/manifest.json', hash: null});
  // Enabling AO's site engine later adds a pin, which stales the admission until refresh.
  await writeFile(path.join(options.root, '.kb/config/manifest.json'), '{"siteEngine": {"version": "3.137.0"}}');
  const stale = await check({...options, exportModel: reached, checkDependencies: deps});
  assert.equal(stale.knowledgeReady, false);
  assert.ok(stale.diagnostics.some(d => d.code === 'ao_knowledge_stale'));
  await writeFile(path.join(options.root, '.kb/config/manifest.json'), '{"siteEngine": {"version": "9.0.0"}}');
  await assert.rejects(refresh({...options, selection: single, exportModel: reached}), /ao_toolkit_pin/);
});

test('a selected product still requires its site engine pin', async t => {
  const options = await fixture(t);
  await rm(path.join(options.root, 'products/alpha/.kb/config/manifest.json'));
  await assert.rejects(refresh(options), /ao_toolkit_pin/);
});

test('a block-list product registry keeps its last product like AO does', async t => {
  const options = await fixture(t);
  await writeFile(path.join(options.root, '.kb/config/vault.md'), '---\nschemaVersion: 8\nproductMode: multi-nested\nproducts:\n  - alpha\n  - beta\n---\nSynthetic compiler fixture.\n');
  await refresh(options);
  assert.equal((await check({...options, checkDependencies: deps})).knowledgeReady, true);
});
