import test from 'node:test';
import assert from 'node:assert/strict';
import { traverse } from '../../templates/starters/ao-kb/module/views/knowledge/trace/traverse.ts';
import { columns, edgeRole, hiddenTypes } from '../../templates/starters/ao-kb/module/views/knowledge/trace/layout.ts';
import { depth, queryBudget } from '../../templates/starters/ao-kb/module/views/knowledge/trace/model.ts';

const record = (path, type = 'Assertion') => ({ path, aoId: path, title: path, __typename: type, ref: { notePath: path, kind: 'NOTE' } });
const edge = (from, rel, to) => ({ from, to, rel, declaredBy: [], locations: [{path: from, section: 'Sources'}] });
function graph(records, edges) {
  const calls = [];
  const read = async (paths, direction, hop, remaining) => {
    calls.push({ paths, direction, hop, remaining });
    return { records, edges: edges.filter(e => paths.includes(e.from) || paths.includes(e.to)), diagnostics: [] };
  };
  return { read, calls };
}
const defaults = { up: 2, down: 2, mentions: true, budget: 200 };

test('follows reference direction, retains second paths, and excludes boundary-only edges', async () => {
  const records = ['focus', 'source', 'artifact', 'next', 'outside'].map(path => record(path));
  const edges = [edge('focus', 'cites', 'source'), edge('artifact', 'references', 'focus'), edge('next', 'references', 'artifact'), edge('next', 'references', 'focus'), edge('artifact', 'references', 'source'), edge('outside', 'references', 'source')];
  const {read, calls} = graph(records, edges);
  const trace = await traverse(records[0], defaults, read);
  assert.deepEqual([...trace.nodes.keys()], ['focus', 'source', 'artifact', 'next']);
  assert.equal(trace.nodes.get('source').lane, 'up');
  assert.equal(trace.nodes.get('artifact').lane, 'down');
  assert.equal(trace.edges.length, 4);
  assert.equal(trace.edges.some(e => e.from === 'outside'), false);
  assert.deepEqual(calls.filter(c => c.direction === 'down').map(c => c.paths), [['focus'], ['artifact', 'next']]);
  assert.equal(edgeRole(edges[2], trace, new Set(trace.nodes.keys())), 'Second path');
});

test('lateral relations stay beside the focus, unclassified stays list-only, extraction stays out', async () => {
  const records = ['focus', 'old', 'older', 'supported', 'extracted', 'concept'].map(path => record(path));
  const {read, calls} = graph(records, [edge('focus', 'supersedes', 'old'), edge('old', 'supersedes', 'older'), edge('focus', 'supports', 'supported'), edge('extracted', 'extracted', 'focus'), edge('focus', 'mentions', 'concept')]);
  const trace = await traverse(records[0], defaults, read);
  assert.deepEqual([...trace.nodes.keys()], ['focus', 'old', 'concept']);
  assert.equal(trace.nodes.get('old').lane, 'related');
  assert.equal(trace.edges.length, 2);
  assert.deepEqual(trace.listOnly.map(e => e.rel), ['supports']);
  assert.equal(calls.some(c => c.paths.includes('old')), false);
  const hidden = await traverse(records[0], {...defaults, mentions: false}, read);
  assert.deepEqual([...hidden.nodes.keys()], ['focus', 'old']);
});

test('cycles terminate, the same edge counts once, and each direction reads only its frontier', async () => {
  const records = ['a', 'b', 'c'].map(path => record(path));
  const {read, calls} = graph(records, [edge('a', 'references', 'b'), edge('b', 'references', 'c'), edge('c', 'references', 'a')]);
  const trace = await traverse(records[0], {...defaults, up: 3, down: 3}, read);
  assert.equal(trace.nodes.size, 3);
  assert.equal(trace.edges.length, 3);
  assert.ok(calls.every(c => c.paths.length === 1));
  assert.ok(calls.length <= 7);
});

test('query budget is distinct from display cap and names the stopped direction and hop', async () => {
  const records = ['focus', ...Array.from({length: 12}, (_, i) => `claim${i}`)].map(path => record(path));
  const {read} = graph(records, records.slice(1).map(r => edge(r.path, 'cites', 'focus')));
  const trace = await traverse(records[0], {...defaults, budget: 8}, read);
  assert.equal(trace.nodes.size, 8);
  assert.equal(trace.edges.length, 7);
  assert.deepEqual(trace.stop, {direction: 'downstream', hop: 1, reason: 'budget'});
  const layout = columns(trace, 2, 2, new Set());
  assert.equal(layout.columns.find(c => c.key === 'd1').hidden.length, 1);
  assert.equal(trace.edges.length, 7, 'layout does not mutate the counted query scope');
});

test('column cap selects round-robin across parents then displays in parent order; hidden parents remain counted', async () => {
  const names = ['focus', 'p1', 'p2', ...Array.from({length: 8}, (_, i) => `a${i}`), 'b0', 'b1', 'grandchild'];
  const records = names.map(path => record(path));
  const edges = [edge('p1', 'references', 'focus'), edge('p2', 'references', 'focus'), ...Array.from({length: 8}, (_, i) => edge(`a${i}`, 'references', 'p1')), edge('b0', 'references', 'p2'), edge('b1', 'references', 'p2'), edge('grandchild', 'references', 'a7')];
  const trace = await traverse(records[0], {...defaults, down: 3}, graph(records, edges).read);
  const layout = columns(trace, 2, 3, new Set());
  const second = layout.columns.find(c => c.key === 'd2');
  assert.deepEqual(second.shown.map(n => n.record.path), ['a0', 'a1', 'a2', 'a3', 'b0', 'b1']);
  assert.equal(hiddenTypes(second.hidden), '4 assertions');
  assert.equal(layout.columns.find(c => c.key === 'd3').orphan, 1);
  assert.equal(edgeRole(edges.at(-1), trace, layout.shown), 'Record behind Show more');
  const expanded = columns(trace, 2, 3, new Set(['d2']));
  assert.equal(expanded.columns.find(c => c.key === 'd3').orphan, 0);
  assert.equal(expanded.shown.size, trace.nodes.size);
});

test('invalid URL depths and authored budgets stay bounded', () => {
  assert.deepEqual([0, 1, 2, 3, 4, '3'].map(depth), [2, 1, 2, 3, 2, 2]);
  assert.deepEqual([undefined, 1, 12, 500, NaN, 2.5].map(queryBudget), [200, 200, 12, 200, 200, 200]);
});
