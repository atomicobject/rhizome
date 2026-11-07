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
  assert.match(expected, /membership: \{path: "products", includes: "alpha", includeMissing: false\}/);
  assert.match(expected, /sourceOrder: 0/);
  assert.ok(recorded.oracle.rows.service.SHARED);
  assert.ok(!recorded.oracle.rows.user.HIDDEN);
  assert.deepEqual(Object.keys(recorded.oracle.rows.position), ['1', '2']);
});

test('ready, stale input, and successful refresh remain distinct from index readiness', async t => {
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
  await assert.rejects(refresh({...options, validate: async () => { throw new Error('bad schema'); }}), /bad schema/);
  assert.deepEqual(await readFile(sdl), before);
  assert.equal((await readJSON(path.join(options.directory, 'manifest.json'))).status, 'stale');
  assert.deepEqual(await readFile(path.join(options.root, '.rhizome/source-ownership.yml')), owner);
  assert.deepEqual(await readJSON(path.join(options.directory, 'selection.json')), selection);
});

test('a failure at either live rename rolls the pair back', async t => {
  for (const failAt of [1, 2]) {
    const options = await fixture(t);
    await refresh(options);
    const old = await readJSON(path.join(options.directory, 'manifest.json'));
    const file = path.join(options.directory, '../ontology/ao-model.generated.graphql');
    const before = await readFile(file);
    let calls = 0;
    await assert.rejects(refresh({...options, renameFile: async (...args) => {
      if (++calls === failAt) throw new Error('injected disk failure');
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
