import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp, realpath, mkdir, writeFile, rm, cp} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {assess} from '../../templates/starters/ao-kb/module/scripts/readiness.mjs';

async function fixture(t, products = []) {
  const root = await realpath(await mkdtemp(path.join(os.tmpdir(), 'ao-ready-')));
  t.after(() => rm(root, {recursive: true, force: true}));
  await cp(new URL('../../templates/starters/ao-kb/module', import.meta.url), path.join(root, '.rhizome/modules/ao-kb'), {recursive: true});
  const write = async (name, text) => { await mkdir(path.dirname(path.join(root, name)), {recursive: true}); await writeFile(path.join(root, name), text); };
  await write('.rhizome/config.yml', '{}\n');
  await write('.kb/config/vault.md', `---\nschemaVersion: 8\nproductMode: ${products.length ? 'multi' : 'single'}\nproducts: [${products.join(', ')}]\n---\n`);
  await write('.rhizome/source-ownership.yml', 'sources:\n  - owner: AO\n    reason: AO owns these files\n    paths:\n      - .kb/**\n      - assertions/**\n      - sources/**\n      - concepts/**\n      - artifacts/**\n      - products/**\n');
  const pin = JSON.parse(await (await import('node:fs/promises')).readFile(path.join(root, '.rhizome/modules/ao-kb/bindings/toolkit-pin.json')));
  if (products.length) await write(".kb/config/manifest.json", JSON.stringify({siteEngine: {version: pin.version, source: pin.source}}));
  for (const product of products) {
    await write(`products/${product}/.kb/config/product.yaml`, `slug: ${product}\naccount: ../..\n`);
    await write(`products/${product}/.kb/config/manifest.json`, JSON.stringify({siteEngine: {version: pin.version, source: pin.source}}));
  }
  return {root, write};
}
test('unambiguous single-product setup is automatic', async t => {
  const {root} = await fixture(t);
  assert.deepEqual((await assess(root)).input, {single: true});
});
test('one eligible product is automatic and multiple products require an explicit choice', async t => {
  const one = await fixture(t, ['alpha']);
  const resultOne = await assess(one.root);
  assert.deepEqual(resultOne.input, {product: 'alpha'}, JSON.stringify(resultOne));
  const many = await fixture(t, ['alpha', 'beta']);
  const result = await assess(many.root);
  assert.equal(result.next, 'needs-input');
  assert.deepEqual(result.recovery[0].choices, ['alpha', 'beta']);
});
test('empty verified KB can be ready and missing dependencies have an actionable reason', async t => {
  const {root, write} = await fixture(t);
  await write('.rhizome/modules/ao-kb/bindings/selection.json', '{"formatVersion":1,"product":null}');
  const preflight = () => ({checks: [{name: 'note includes', status: 'ok'}]});
  assert.equal((await assess(root, {preflight, verify: async () => {}, check: async () => ({bindingsReady: true, indexReady: true})})).next, 'ready');
  const failure = await assess(root, {preflight, verify: async () => {}, check: async () => ({bindingsReady: false, dependencies: {uv: {ready: false, message: 'uv is missing'}}, diagnostics: [{severity: 'error', code: 'ao_dependency_missing', message: 'Install uv'}]})});
  assert.equal(failure.next, 'blocked');
  assert.match(failure.reason, /uv is missing/);
  assert.equal(failure.recovery[0].kind, 'retry');
});

test('readiness delegates source admission without rejecting a valid brace include', async t => {
  const {root, write} = await fixture(t);
  const source = 'artifacts/_assets/_model/items.yaml';
  await write('.rhizome/modules/ao-kb/bindings/selection.json', '{"formatVersion":1,"product":null}');
  await write('.rhizome/modules/ao-kb/bindings/manifest.json', JSON.stringify({sources: [{path: source}]}));
  await write('.rhizome/config.yml', 'notes:\n  includes:\n    - "artifacts/_assets/_model/*.{yaml,yml}"\n');
  const result = await assess(root, {
    preflight: () => ({checks: []}), verify: async () => {},
    check: async () => ({bindingsReady: true, indexReady: true}),
  });
  assert.equal(result.next, 'ready');
  assert.deepEqual(result.includes, [source]);
});
