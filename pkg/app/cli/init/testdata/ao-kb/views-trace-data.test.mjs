import test from 'node:test';
import assert from 'node:assert/strict';
import { createReader, directory } from '../../templates/starters/ao-kb/module/views/knowledge/trace/data.ts';
import { traverse } from '../../templates/starters/ao-kb/module/views/knowledge/trace/traverse.ts';

const record = (path, type, aoId) => ({ path, title: path, aoId, __typename: type, ref: {notePath: path, kind: 'NOTE', typeName: type} });
const neighborhood = edges => ({edges: edges.map(([relation, notePath]) => ({relation, target: {notePath}})), nodes: [], truncated: false});
const claim = record('assertions/claim.md', 'Assertion', 'a-1');
const source = record('sources/source.md', 'Source', 's-1');
const other = record('assertions/other.md', 'Assertion', 'a-2');
const registry = [claim, source, other];
const details = {
  [claim.path]: {...claim, provenanceSource: 's-1', concepts: [], conceptsCount: 0,
    sourcesSection: {ref: {...claim.ref, kind: 'EMBEDDED', fragment: 'sources'}, citations: [
      {recordedLink: '[[source]]', sources: [{path:source.path}], sourcesCount: 1},
      {recordedLink: '[[source]]', sources: [{path:source.path}], sourcesCount: 1},
    ]},
    connectionsSection: {ref: {...claim.ref, kind: 'EMBEDDED', fragment: 'connections'}, neighborhood: neighborhood([['supportedBy', other.path]])},
    frontmatterSupportedBy: ['a-2']},
  [other.path]: {...other, provenanceSource: null, concepts: [], conceptsCount: 0, sourcesSection: null, connectionsSection: null},
  [source.path]: {...source, extractedAssertionIds: ['a-1', 'a-2', 'a-missing']},
};
function fixtureQuery(calls, snapshot = details) {
  return async document => {
    calls.push(document);
    const out = {};
    for (const match of document.matchAll(/(n\d+): node\(ref: ("[^"]+")\)/g)) {
      const path = JSON.parse(match[2]);
      out[match[1]] = document.includes('__typename path title') ? snapshot[path] : {neighborhood: {...neighborhood([]), nodes: path === source.path ? [{ref: claim.ref}] : []}};
    }
    for (const match of document.matchAll(/(r\d+_\d+): assertion/g)) out[match[1]] = [];
    return out;
  };
}

test('native repeated citations keep occurrences and origin, and deduplicate inverse declarations', async () => {
  const reader = createReader(fixtureQuery([]), registry, 200);
  const batch = await reader.read([claim.path], 'related', 1, 199);
  const citation = batch.edges.find(e => e.rel === 'cites');
  assert.equal(citation.to, source.path);
  assert.deepEqual(citation.locations.map(l => l.section), ['Sources #1 (origin)', 'Sources #2']);
  assert.equal(citation.locations[0].ref.fragment, 'sources');
  const support = batch.edges.find(e => e.rel === 'supports');
  assert.equal(support.from, other.path);
  assert.equal(support.to, claim.path);
  assert.deepEqual(support.declaredBy.map(d => d.origin), ['body', 'frontmatter']);
});

test('source extraction remains separate from citations and names unresolved ids', async () => {
  const reader = createReader(fixtureQuery([]), registry, 200);
  const trace = await traverse(source, {up: 2, down: 2, mentions: true, budget: 200}, reader.read);
  const history = await reader.extraction(source.path);
  assert.deepEqual(history.map(row => [row.id, row.cites]), [['a-1', true], ['a-2', false], ['a-missing', null]]);
  assert.equal(history[2].record, undefined);
  assert.equal(trace.edges.some(e => e.rel === 'extracted'), false);
  assert.equal(trace.nodes.has(other.path), false, 'an extracted assertion is not silently a supporting assertion');
});

test('incoming queries target only frontier owners with explicit caps and filtered raw ids', async () => {
  const calls = [];
  const reader = createReader(fixtureQuery(calls), registry, 200);
  await reader.read([source.path], 'down', 1, 3);
  const incoming = calls.find(query => query.includes('direction: INBOUND'));
  assert.match(incoming, /node\(ref: "sources\/source.md"\)/);
  assert.match(incoming, /first: 4/);
  assert.match(incoming, /field: "provenanceSource", op: in/);
  assert.ok(calls.every(query => !/assertion\(first: \d+\)\s*\{/.test(query)), 'no unfiltered graph scan');
});

test('the documented identity ceiling does not silently claim a complete picker', () => {
  const records = Array.from({length:201}, (_, i) => record(`assertions/${i}.md`, 'Assertion', `a-${i}`));
  const result = directory({assertion: records, source: [], concept: [], artifact: []});
  assert.equal(result.records.length, 200);
  assert.equal(result.capped, true);
});

test('relation counts name targets beyond a bounded read separately from the display cap', async () => {
  const snapshot = structuredClone(details);
  snapshot[claim.path].concepts = [{path: 'concepts/one.md'}, {path: 'concepts/two.md'}];
  snapshot[claim.path].conceptsCount = 7;
  const reader = createReader(fixtureQuery([], snapshot), registry, 200);
  const batch = await reader.read([claim.path], 'up', 1, 1);
  assert.equal(batch.stop.reason, 'field');
  assert.equal(batch.stop.direction, 'upstream');
  assert.ok(batch.diagnostics.includes('assertions/claim.md: 5 more concept targets beyond the read cap.'));
});

test('an exactly full relation page is complete when its count proves exhaustion', async () => {
  const snapshot = structuredClone(details);
  snapshot[claim.path].concepts = [{path: 'concepts/one.md'}, {path: 'concepts/two.md'}];
  snapshot[claim.path].conceptsCount = 2;
  const reader = createReader(fixtureQuery([], snapshot), registry, 200);
  const batch = await reader.read([claim.path], 'up', 1, 1);
  assert.equal(batch.stop, undefined);
});

test('explicit related concepts use the section field rather than guessing an ambient edge name', async () => {
  const concept = record('concepts/self-service.md', 'Concept', 'cpt-1');
  const related = record('concepts/reminders.md', 'Concept', 'cpt-2');
  const snapshot = {...details, [concept.path]: {...concept, relatedConceptsSection: {
    ref: {...concept.ref, kind: 'EMBEDDED', fragment: 'related-concepts'},
    neighborhood: neighborhood([['related', related.path]]),
    relatedConcepts: [{path: related.path}], relatedConceptsCount: 1,
  }}};
  const reader = createReader(fixtureQuery([], snapshot), [...registry, concept, related], 200);
  const trace = await traverse(concept, {up: 1, down: 1, mentions: true, budget: 200}, reader.read);
  assert.equal(trace.edges.find(e => e.rel === 'related-concepts').to, related.path);
  assert.equal(trace.nodes.get(related.path).lane, 'related');
});

test('all typed source targets in a citation occurrence remain navigable', async () => {
  const second = record('sources/second.md', 'Source', 's-2');
  const snapshot = structuredClone(details);
  snapshot[claim.path].sourcesSection.citations = [{recordedLink:'[[source]] and [[second]]', sources:[{path:source.path},{path:second.path}], sourcesCount:2}];
  const reader = createReader(fixtureQuery([], snapshot), [...registry, second], 200);
  const batch = await reader.read([claim.path], 'up', 1, 199);
  const cites = batch.edges.filter(e => e.rel === 'cites');
  assert.deepEqual(cites.map(e=>e.to), [source.path,second.path]);
  assert.equal(cites[1].locations[0].section, 'Sources #1');
});

// Product-model records: neighborhoods name RECORD endpoints by nodeId without a fragment,
// and omit them from `nodes`. The reader derives keyed refs, keeps rows distinct, and names
// the end that authored each edge.
const host = (file) => `artifacts/_assets/_model/${file}.yaml`;
const value = (entries) => ({ kind: 'object', entries: Object.entries(entries).map(([key, v]) => ({ key, value: Array.isArray(v) ? { kind: 'array', items: v.map((s) => ({ kind: 'string', scalar: s })) } : { kind: 'string', scalar: v } })) });
const endpoint = (file, digest) => ({ notePath: host(file), kind: 'EMBEDDED', fragment: null, nodeId: `record-v1-${digest}`, typeName: null });
const modelRow = (file, digest, aoValue, type, entries, out = [], inbound = []) => ({
  __typename: type, path: `${host(file)}#record:v1:${digest}`, title: aoValue, aoValue,
  ref: { notePath: host(file), kind: 'EMBEDDED', fragment: `record:v1:${digest}`, nodeId: `record-v1-${digest}`, typeName: type },
  workspace: { source: { value: value(entries) } },
  modelOut: { truncated: false, edges: out.map(([relation, target]) => ({ relation, target })) },
  modelIn: { truncated: false, edges: inbound.map(([relation, target]) => ({ relation, target })) },
});
const featBook = modelRow('features', 'aa', 'FEAT-book', 'AOKBModel_feature_single', { id: 'FEAT-book', flows: ['FLOW-book'], supported_by: ['a-1'] },
  [['evidenced-by', { notePath: claim.path, kind: 'NOTE', fragment: null, nodeId: null, typeName: 'Assertion' }], ['serves', endpoint('flows', 'cc')]]);
const featCancel = modelRow('features', 'bb', 'FEAT-cancel', 'AOKBModel_feature_single', { id: 'FEAT-cancel', supported_by: ['a-1'] },
  [['evidenced-by', { notePath: claim.path, kind: 'NOTE', fragment: null, nodeId: null, typeName: 'Assertion' }]]);
const flowBook = modelRow('flows', 'cc', 'FLOW-book', 'AOKBModel_flow_single', { id: 'FLOW-book', screens: ['SCR-book'] }, [],
  [['serves', endpoint('features', 'aa')], ['serves', endpoint('screens', 'dd')], ['governs', { ...endpoint('requirements', 'x'), nodeId: 'record-pos-v1-ee' }]]);
const scrBook = modelRow('screens', 'dd', 'SCR-book', 'AOKBModel_screen_single', { id: 'SCR-book' }, [['serves', endpoint('flows', 'cc')]]);
const rows = [featBook, featCancel, flowBook, scrBook];
function modelQuery(calls) {
  return async (document) => {
    calls.push(document);
    const out = {};
    for (const match of document.matchAll(/(n\d+): node\(ref: ("[^"]+")\)/g)) {
      const path = JSON.parse(match[2]);
      if (document.includes('__typename path title')) out[match[1]] = rows.find((r) => r.path === path) ?? details[path] ?? null;
      else out[match[1]] = { neighborhood: { truncated: false, edges: path === claim.path ? [featBook, featCancel].map((r) => ({ relation: 'evidenced-by', target: endpoint('features', r.ref.fragment.slice(10)) })) : [] } };
    }
    for (const match of document.matchAll(/(r\d+_\d+): assertion/g)) out[match[1]] = [];
    return out;
  };
}

test('model records reached through edges stay distinct, keyed by native identity', async () => {
  const calls = [];
  const reader = createReader(modelQuery(calls), registry, 200, true);
  const trace = await traverse(claim, { up: 1, down: 1, mentions: true, budget: 200 }, reader.read);
  assert.ok(trace.nodes.has(featBook.path));
  assert.ok(trace.nodes.has(featCancel.path), 'two rows of features.yaml are separate trace records');
  assert.equal(trace.nodes.has(host('features')), false, 'no record collapses to its host file');
  assert.equal(trace.nodes.get(featBook.path).record.aoId, 'FEAT-book');
  assert.ok(calls.some((q) => q.includes(`node(ref: "${featBook.path}")`)), 'reads the derived keyed ref');
});

test('a declaration stored the other way round names the record that authored it', async () => {
  const reader = createReader(modelQuery([]), [...registry, flowBook], 200, true);
  const trace = await traverse(flowBook, { up: 1, down: 1, mentions: true, budget: 200 }, reader.read);
  const screens = trace.edges.find((e) => e.from === scrBook.path);
  assert.deepEqual([screens.rel, screens.to, screens.declaredBy[0].record], ['serves', flowBook.path, flowBook.path]);
  assert.deepEqual(screens.locations[0], { path: host('flows'), section: 'FLOW-book › screens', ref: flowBook.ref });
  assert.equal(trace.nodes.get(scrBook.path).lane, 'down');
  const feature = trace.edges.find((e) => e.from === featBook.path);
  assert.equal(feature.declaredBy[0].record, featBook.path);
  assert.equal(trace.nodes.get(featBook.path).lane, 'up');
  assert.ok(trace.diagnostics.some((d) => d.includes('positional record in artifacts/_assets/_model/requirements.yaml')));
});
