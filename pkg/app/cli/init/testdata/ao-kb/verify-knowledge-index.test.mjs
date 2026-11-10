import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdir, mkdtemp, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {verifyKnowledgeIndex} from '../../templates/starters/ao-kb/module/bindings/verify-index.mjs';

const roots = ['assertion', 'source', 'concept', 'artifact', 'mapOfContent', 'submission', 'curationDocument', 'conflictRegistry'];
const complete = {data: Object.fromEntries(roots.map(name => [name, []])), extensions: {typedRoots: Object.fromEntries(roots.map(name => [name, {status: 'complete'}]))}};
async function fixture(t, count) {
  const root = await mkdtemp(path.join(tmpdir(), 'ao-knowledge-index-'));
  t.after(() => rm(root, {recursive: true, force: true}));
  await mkdir(path.join(root, 'assertions'));
  for (let i = 0; i < count; i++) await writeFile(path.join(root, `assertions/${i}.md`), '# Assertion\n');
  return root;
}

test('knowledge verification reads every item in batches larger than the default page', async t => {
  const root = await fixture(t, 101);
  const sizes = [];
  await verifyKnowledgeIndex(root, async query => {
    if (!query.includes('nodes(refs:')) return complete;
    assert.match(query, /items \{ requestedRef ref \{ notePath \} node/);
    const files = JSON.parse(query.match(/refs: (\[[^\]]*\])/)[1]);
    assert.match(query, new RegExp(`first: ${files.length}\\)`));
    sizes.push(files.length);
    return {data: {nodes: {items: files.map(notePath => ({requestedRef: notePath, ref: {notePath}, node: {__typename: 'Note'}})), pageInfo: {truncated: false}}}};
  });
  assert.deepEqual(sizes, [100, 1]);
});

test('knowledge verification rejects unresolved items and truncated batches', async t => {
  const root = await fixture(t, 1);
  await assert.rejects(verifyKnowledgeIndex(root, async () => ({data: {nodes: {
    items: [{requestedRef: 'assertions/0.md', error: {code: 'not_found', message: 'not indexed'}}], pageInfo: {truncated: false},
  }}})), /assertions\/0.md: not indexed/);
  await assert.rejects(verifyKnowledgeIndex(root, async () => ({data: {nodes: {items: [], pageInfo: {truncated: true}}}})), /complete node batch/);
});
