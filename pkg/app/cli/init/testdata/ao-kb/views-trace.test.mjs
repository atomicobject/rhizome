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

// Product-model hops (SPEC-0133 R7). Two features share features.yaml; the flow declares its
// screens, so their `serves` edge is stored screen to flow while the flow authored it.
const row = (file, id, type) => ({ path: `artifacts/_assets/_model/${file}.yaml#record:v1:${id}`, aoId: id, title: id, __typename: type, ref: { notePath: `artifacts/_assets/_model/${file}.yaml`, kind: 'EMBEDDED', fragment: `record:v1:${id}` } });
const declared = (from, rel, to, by = from) => ({ ...edge(from, rel, to), declaredBy: [{ record: by, as: rel, origin: 'body' }] });
const claim = record('assertions/claim.md');
const book = row('features', 'feat-book', 'AOKBModel_feature_single');
const cancel = row('features', 'feat-cancel', 'AOKBModel_feature_single');
const flow = row('flows', 'flow-book', 'AOKBModel_flow_single');
const screen = row('screens', 'scr-book', 'AOKBModel_screen_single');
const form = row('components', 'booking-form', 'AOKBModel_component_single');
const rule = row('requirements', 'fr-book', 'AOKBModel_requirement_single');
const model = [claim, book, cancel, flow, screen, form, rule];
const modelEdges = [
  declared(book.path, 'evidenced-by', claim.path),
  declared(cancel.path, 'evidenced-by', claim.path),
  declared(flow.path, 'evidenced-by', claim.path),
  declared(book.path, 'serves', flow.path),
  declared(screen.path, 'serves', flow.path, flow.path),
  declared(screen.path, 'made-of', form.path),
  declared(rule.path, 'governs', flow.path),
];

test('model forward: an assertion leads downstream to the records it evidences, then their declared structure', async () => {
  const trace = await traverse(claim, { ...defaults, up: 1, down: 3 }, graph(model, modelEdges).read);
  const at = (path) => [trace.nodes.get(path)?.lane, trace.nodes.get(path)?.hop];
  assert.deepEqual(at(book.path), ['down', 1]);
  assert.deepEqual(at(cancel.path), ['down', 1], 'two rows of one YAML file stay distinct records');
  assert.deepEqual(at(flow.path), ['down', 1]);
  assert.deepEqual(at(screen.path), ['down', 2], 'the flow declared its screens, so they are downstream of it');
  assert.deepEqual(at(form.path), ['down', 3]);
  assert.equal(trace.nodes.has(rule.path), false, 'a requirement governing the flow is upstream of it, not downstream');
  const serves = trace.edges.find((e) => e.rel === 'serves' && e.from === screen.path);
  assert.deepEqual([serves.from, serves.to], [screen.path, flow.path], 'arrows keep authored direction');
});

test('model reverse: a component leads upstream through declaring records to the claims they rest on', async () => {
  const trace = await traverse(form, { ...defaults, up: 3, down: 1 }, graph(model, modelEdges).read);
  const at = (path) => [trace.nodes.get(path)?.lane, trace.nodes.get(path)?.hop];
  assert.deepEqual(at(screen.path), ['up', 1]);
  assert.deepEqual(at(flow.path), ['up', 2]);
  assert.deepEqual(at(book.path), ['up', 3]);
  assert.deepEqual(at(rule.path), ['up', 3]);
  assert.deepEqual(at(claim.path), ['up', 3], 'the flow rests on the claim: a reference leads upstream');
  assert.equal(trace.nodes.has(cancel.path), false);
});

test('model relations are never lateral; an unclassified model relation stays list-only', async () => {
  const custom = { ...declared(book.path, 'mitigates', cancel.path) };
  const trace = await traverse(book, { ...defaults, up: 1, down: 1 }, graph(model, [...modelEdges, custom]).read);
  assert.equal([...trace.nodes.values()].some((n) => n.lane === 'related'), false);
  assert.deepEqual(trace.listOnly.map((e) => e.rel), ['mitigates']);
  assert.equal(trace.nodes.has(cancel.path), false);
  assert.deepEqual(trace.nodes.get(flow.path).lane, 'down');
  assert.deepEqual(trace.nodes.get(claim.path).lane, 'up');
});

test('model cycles terminate and count each edge once', async () => {
  const part = row('components', 'slot-picker', 'AOKBModel_component_single');
  const cycle = [declared(form.path, 'made-of', part.path), declared(part.path, 'made-of', form.path)];
  const trace = await traverse(form, { ...defaults, up: 3, down: 3 }, graph([form, part], cycle).read);
  assert.equal(trace.nodes.size, 2);
  assert.equal(trace.edges.length, 2);
});

test('the query budget stops model hops explicitly', async () => {
  const trace = await traverse(claim, { ...defaults, down: 3, budget: 3 }, graph(model, modelEdges).read);
  assert.equal(trace.nodes.size, 3);
  assert.deepEqual(trace.stop, { direction: 'downstream', hop: 1, reason: 'budget' });
});
