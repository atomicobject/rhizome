import assert from 'node:assert/strict';
import { test } from 'node:test';
import { reader, locator } from '../../templates/starters/agentic-engineering/module/views/delivery/reads.ts';
import { identity, selectionFor, promptFor } from '../../templates/starters/agentic-engineering/module/views/delivery/lib/index.ts';
const ref = (notePath, typeName = 'EffortNote', fragment = '') => ({ notePath, typeName, fragment, kind: fragment ? 'section' : 'note', nodeId: fragment || notePath });
const field = (name, sectionNodes = [], values = null, links = []) => ({ name, present: true, values, sectionNodes, links, issues: [] });
const node = (r, fields = [], format = 'markdown') => ({ ref: r, title: r.notePath, resolvedType: r.typeName, content: 'Full authored prose', format,
  locator: { sourceLocator: r.notePath, exists: true, requiresFix: false, linkTarget: { ref: r, exists: true, requiresFix: false } },
  workspace: { sourceRevision: { contentFingerprint: 'r1' }, fields } });
const link = r => ({ target: r.notePath, authoredTarget: `authored ${r.fragment}`, text: 'alias', kind: 'wiki', anchor: r.fragment,
  embed: false, targetKind: 'note', resolved: true, resolvedRef: r, title: 'Alias', preview: null });
function harness(nodes = [], source = new Map(), alter = () => {}, inventory = {}) {
  const facts = new WeakMap();
  const calls = [];
  const api = {
    markdownChecklist(source) {
      assert.equal(source, '- [x] Verified\n- [x] Backported');
      return { checked: 2, total: 2 };
    },
    generation: data => facts.get(data)?.generation,
    roots: data => facts.get(data)?.roots,
    async readNodes(targets, selection) {
      calls.push({ targets, selection });
      const wanted = targets.map(text => JSON.parse(text));
      const found = wanted.flatMap(r => nodes.filter(n => identity(r) === identity(n.ref)));
      const unresolved = wanted.filter(r => !found.some(n => identity(r) === identity(n.ref))).map(r => ({ requestedRef: JSON.stringify(r), message: 'unresolved' }));
      const data = { nodes: found, complete: !unresolved.length, errors: [], unresolved, truncated: [] };
      facts.set(data, { generation: '7' });
      return data;
    },
    async graphql(query, variables) {
      calls.push({ query, variables });
      if (query.includes('DeliveryInventory')) {
        const root = query.includes('specLike(') ? 'specLike' : 'effort';
        const all = inventory[root] ?? [];
        const page = all.slice(variables.offset, variables.offset + 100);
        const data = { [root]: page };
        facts.set(data, { generation: '7', roots: { [root]: { status: variables.offset + page.length >= all.length ? 'complete' : 'capped',
          hasMore: variables.offset + page.length < all.length, nextOffset: variables.offset + page.length, offset: variables.offset, returnedCount: page.length } } });
        alter(data, variables, facts);
        return data;
      }
      const r = JSON.parse(variables.ref);
      const all = source.get(identity(r)) ?? [];
      const data = { node: { ref: r, workspace: { sourceRevision: { contentFingerprint: 'r1' }, sourceLinkCount: all.length,
        sourceLinks: all.slice(variables.offset, variables.offset + 100) } } };
      facts.set(data, { generation: '7' });
      alter(data, variables, facts);
      return data;
    },
  };
  return { read: reader(api), calls, api };
}

test('focused occurrence traversal preserves duplicates and unresolved occurrences at page boundaries', async () => {
  for (const count of [0, 99, 100, 101, 200, 201]) {
    const own = node(ref('scope.md'));
    const links = Array.from({ length: count }, () => link(ref('spec.md', 'UserStory', 'a')));
    if (count) links[count - 1] = { ...links[count - 1], resolved: false, resolvedRef: null };
    const h = harness([own], new Map([[identity(own.ref), links]]));
    const result = await h.read.occurrences(own);
    assert.equal(result.coverage.complete, true);
    assert.equal(result.occurrences.length, count);
    assert.deepEqual(result.occurrences.map(x => x.ordinal), links.map((_, i) => i));
    if (count) assert.equal(result.occurrences.at(-1).targetKind, 'unresolved');
  }
});

test('missing generation, source edits, target-only edits, short pages and failed later pages stay partial', async () => {
  const own = node(ref('scope.md'));
  const links = Array(201).fill(link(ref('spec.md', 'UserStory', 'a')));
  for (const change of ['generation', 'revision', 'missing', 'short', 'error']) {
    const h = harness([own], new Map([[identity(own.ref), links]]), (data, vars, facts) => {
      if (!vars.offset) return;
      if (change === 'generation') facts.set(data, { generation: '8' });
      if (change === 'revision') data.node.workspace.sourceRevision.contentFingerprint = 'r2';
      if (change === 'missing') facts.set(data, {});
      if (change === 'short') data.node.workspace.sourceLinks.pop();
      if (change === 'error') throw new Error('read failed');
    });
    const result = await h.read.occurrences(own);
    assert.equal(result.coverage.complete, false, change);
    assert.ok(result.occurrences.length >= 100, change);
    assert.equal(h.calls.length, 2, change);
  }
});

test('inventory pages summaries without whole-vault prose and discloses missing coverage', async () => {
  const efforts = Array.from({ length: 201 }, (_, i) => node(ref(`effort-${i}.md`), [field('status', [], ['active'])]));
  const h = harness(efforts, new Map(), () => {}, { effort: efforts });
  const result = await h.read.portfolio();
  assert.equal(result.portfolio.efforts.length, 201);
  assert.equal(result.coverage.complete, true);
  assert.equal(result.reviewEvidence, 'deferred');
  assert.equal(result.portfolio.efforts[0].checklist.state, 'deferred');
  assert.ok(h.calls.every(call => !call.query?.includes(' content ')));
  const broken = harness([], new Map(), (data, vars, facts) => facts.set(data, { generation: '7' }));
  assert.equal((await broken.read.portfolio()).coverage.complete, false);
});

test('workspace A selection, B mention and C log remain distinct including criterion-only selection', async () => {
  const a = ref('spec.md', 'AcceptanceCriterion', 'criterion-a');
  const b = ref('spec.md', 'UserStory', 'b');
  const c = ref('spec.md', 'UserStory', 'c');
  const scopeRef = ref('scope.md', 'EffortMaterial');
  const logRef = ref('log.md', 'EffortMaterial');
  const frozenSpecs = ref('scope.md', 'NarrativeSection', 'specs');
  const frozenStories = ref('scope.md', 'NarrativeSection', 'stories');
  const logSpecs = ref('log.md', 'NarrativeSection', 'specs');
  const logStories = ref('log.md', 'NarrativeSection', 'stories');
  const relation = (name, r) => field(name, [], [r.notePath], [{ value: r.notePath, resolved: true, ref: r, title: r.notePath }]);
  const own = node(ref('entry.html', 'EffortWorkspace'), [field('status', [], ['active']), relation('frozenScope', scopeRef), relation('workLog', logRef), field('governingSpecs')], 'html');
  const selected = node(scopeRef, [field('specSetFrozen', [frozenSpecs]), field('storiesInScopeFrozen', [frozenStories])]);
  const log = node(logRef, [field('specSetFrozen', [logSpecs]), field('storiesInScopeFrozen', [logStories])]);
  const nodes = [own, selected, log, ...[frozenSpecs, frozenStories, logSpecs, logStories, a, b, c].map(r => node(r))];
  const source = new Map([[identity(own.ref), [link(b)]], [identity(frozenStories), [link(a), link(a)]], [identity(logStories), [link(c)]]]);
  const h = harness(nodes, source);
  const result = await h.read.record(own.ref);
  assert.equal(JSON.parse(h.calls[0].targets[0]).notePath, 'entry.html');
  assert.equal(result.record.scope.state, 'known');
  assert.equal(result.record.scope.selections.length, 2);
  assert.equal(result.record.scope.selections[0].targetKind, 'criterion');
  assert.equal(selectionFor(result.record.scope, b).occurrences.length, 0);
  assert.equal(result.entryMentions.occurrences[0].target.fragment, 'b');
  assert.equal(result.workLogSnapshot.stories.occurrences[0].target.fragment, 'c');
  assert.equal(result.node.content, 'Full authored prose');
  selected.workspace.fields[1].issues = [{ code: 'cardinality', message: 'Duplicate section' }];
  assert.equal((await h.read.record(own.ref)).record.scope.state, 'incomplete');
  selected.workspace.fields[1].issues = [];
  own.workspace.fields = own.workspace.fields.filter(f => f.name !== 'frozenScope');
  const legacy = await h.read.record(own.ref);
  assert.equal(legacy.record.scope.state, 'incomplete');
  assert.equal(legacy.record.scope.selections.length, 0);
});

test('canonical locators refuse targets needing repair and cancelled reads never publish', async () => {
  const own = node(ref('spec.md', 'UserStory', 'a'));
  own.locator.linkTarget.requiresFix = true;
  assert.equal(locator(own), null);
  const abort = new AbortController();
  abort.abort();
  const h = harness([own]);
  await assert.rejects(reader(h.api, abort.signal).record(own.ref), { name: 'AbortError' });
  assert.equal(h.calls.length, 0);
});

test('structural public refs retain canonical identity and agree with host selector aliases', () => {
  const raw = { notePath: 'spec.md', kind: 'SECTION', structural: 'first', ref: 'canonical:first' };
  const host = { notePath: 'spec.md', kind: 'SECTION', structuralFingerprint: 'first' };
  assert.equal(identity(raw), identity(host));
  assert.notEqual(identity(raw), identity({ ...raw, structural: 'second' }));
  assert.equal(raw.ref, 'canonical:first');
});

test('story and criterion collection order and full detail survive typed hydration', async () => {
  const own = node(ref('spec.md', 'ExperienceSpec'), [field('specStatus', [], ['active']), field('userStories', [ref('spec.md', 'UserStoriesSection', 'wrapper')])]);
  const wrapper = node(ref('spec.md', 'UserStoriesSection', 'wrapper'));
  const b = node(ref('spec.md', 'UserStory', 'b'), [field('status', [], ['ready'])]);
  const a = node(ref('spec.md', 'UserStory', 'a'), [field('status', [], ['draft']), field('acceptanceCriteria', [ref('spec.md', 'AcceptanceCriteriaSection', 'criteria')])]);
  const ac = node(ref('spec.md', 'AcceptanceCriteriaSection', 'criteria'));
  const criterion = node(ref('spec.md', 'AcceptanceCriterion', 'one'));
  wrapper.workspace.collections = [{ name: 'stories', items: [{ ref: b.ref }, { ref: a.ref }] }];
  ac.workspace.collections = [{ name: 'criteria', items: [{ ref: criterion.ref }] }];
  const h = harness([own, wrapper, a, b, ac, criterion]);
  const result = await h.read.record(own.ref);
  assert.deepEqual(result.record.stories.map(s => s.ref.fragment), ['b', 'a']);
  assert.equal(result.record.stories[1].criteria[0].source.content, 'Full authored prose');
});

test('ambiguous spec sections retain prose but cannot supply copied scope', async () => {
  for (const section of ['userStories', 'acceptanceCriteria']) {
    for (const fault of ['duplicate', 'issue']) {
      const wrapper = node(ref('spec.md', 'UserStoriesSection', 'stories'));
      const ac = node(ref('spec.md', 'AcceptanceCriteriaSection', 'criteria'));
      const criterion = node(ref('spec.md', 'AcceptanceCriterion', 'criterion'));
      const story = node(ref('spec.md', 'UserStory', 'story'), [field('status', [], ['ready']), field('acceptanceCriteria', [ac.ref])]);
      const own = node(ref('spec.md', 'ExperienceSpec'), [field('specStatus', [], ['active']), field('userStories', [wrapper.ref])]);
      const sibling = node(ref('spec.md', 'UserStory', 'sibling'), [field('status', [], ['ready'])]);
      wrapper.workspace.collections = [{ name: 'stories', items: [{ ref: story.ref }, { ref: sibling.ref }] }];
      ac.workspace.collections = [{ name: 'criteria', items: [{ ref: criterion.ref }] }];
      const nodes = [own, wrapper, story, sibling, ac, criterion];
      const cleanWrapper = node(ref('clean.md', 'UserStoriesSection', 'stories'));
      const cleanStory = node(ref('clean.md', 'UserStory', 'story'), [field('status', [], ['ready'])]);
      const cleanSpec = node(ref('clean.md', 'ExperienceSpec'), [field('specStatus', [], ['active']), field('userStories', [cleanWrapper.ref])]);
      cleanWrapper.workspace.collections = [{ name: 'stories', items: [{ ref: cleanStory.ref }] }];
      nodes.push(cleanSpec, cleanWrapper, cleanStory);
      const h = harness(nodes, new Map(), () => {}, { specLike: [own, cleanSpec] });
      const keys = [identity(story.ref)];
      assert.equal(promptFor('ready', (await h.read.portfolio()).portfolio, keys).copyable, true);
      const state = (section === 'userStories' ? own : story).workspace.fields.find(item => item.name === section);
      if (fault === 'duplicate') {
        const duplicate = node({ ...state.sectionNodes[0], fragment: 'duplicate', nodeId: 'duplicate' });
        state.sectionNodes.push(duplicate.ref);
        nodes.push(duplicate);
      } else {
        state.issues = [{ code: 'cardinality', message: 'Section must match a single heading' }];
      }
      const portfolio = await h.read.portfolio();
      assert.equal(portfolio.coverage.complete, false, `${section}/${fault}`);
      assert.equal(promptFor('ready', portfolio.portfolio, keys).copyable, false, `${section}/${fault}`);
      const detail = await h.read.record(own.ref);
      assert.equal(detail.coverage.complete, false, `${section}/${fault}`);
      assert.equal(detail.node.content, 'Full authored prose');
      assert.equal(detail.record.stories[0].criteria[0].source.content, 'Full authored prose');
      assert.equal(promptFor('ready', portfolio.portfolio, [identity(sibling.ref)]).copyable, section === 'acceptanceCriteria', `sibling: ${section}/${fault}`);
      assert.equal(promptFor('ready', portfolio.portfolio, [identity(cleanStory.ref)]).copyable, true, `clean neighbor: ${section}/${fault}`);
      own.workspace.fields.find(item => item.name === 'specStatus').values = ['archived'];
      assert.equal(promptFor('ready', (await h.read.portfolio()).portfolio, [identity(cleanStory.ref)]).copyable, true, `archived neighbor: ${section}/${fault}`);
    }
  }
});

test('API hydration consumes the canonical public ref and keeps the returned ref object', async () => {
  const own = node({ ...ref('spec.md', 'ExperienceSpec'), ref: 'canonical:spec', structural: 'raw', structuralFingerprint: 'raw' });
  const h = harness();
  let requested;
  h.api.readNodes = async targets => {
    requested = targets;
    return { nodes: [own], complete: true, errors: [], unresolved: [], truncated: [] };
  };
  const result = await reader(h.api).hydrate([own.ref], true);
  assert.deepEqual(requested, ['canonical:spec']);
  assert.equal(result.nodes[0].ref, own.ref);
});

test('workspace checklist uses the exact log independently of unknown frozen authority', async () => {
  const logRef = ref('work-log.md', 'EffortMaterial');
  const closureRef = ref('work-log.md', 'NarrativeSection', 'closure');
  const statusRef = ref('work-log.md', 'NarrativeSection', 'status');
  const own = node(ref('entry.html', 'EffortWorkspace'), [field('status', [], ['active']),
    { ...field('frozenScope'), present: false },
    field('workLog', [], ['work-log.md'], [{ value: 'work-log.md', resolved: true, ref: logRef }])], 'html');
  const log = node(logRef, [field('closureChecklist', [closureRef]), field('statusSection', [statusRef])]);
  const closure = { ...node(closureRef), content: '- [x] Verified\n- [x] Backported' };
  const status = { ...node(statusRef), content: 'Resume the exact approved plan.' };
  const h = harness([own, log, closure, status]);
  let result = await h.read.record(own.ref);
  assert.deepEqual(result.record.checklist, { state: 'recorded', checked: 2, total: 2 });
  assert.equal(result.record.status, 'active');
  assert.equal(result.record.coverage.complete, true);
  assert.equal(result.coverage.complete, false);
  assert.equal(result.record.scope.state, 'unknown');
  assert.equal(result.components.find(item => identity(item.ref) === identity(statusRef)).content, status.content);
  assert.equal(new Set(result.components.map(item => identity(item.ref))).size, result.components.length);
  const closureField = log.workspace.fields[0];
  closureField.present = false;
  closureField.sectionNodes = [];
  result = await h.read.record(own.ref);
  assert.equal(result.record.checklist.state, 'missing');
  closureField.present = true;
  closureField.sectionNodes = [closureRef, closureRef];
  result = await h.read.record(own.ref);
  assert.equal(result.record.checklist.state, 'unreadable');
  closureField.sectionNodes = [ref('missing.md', 'NarrativeSection', 'closure')];
  result = await h.read.record(own.ref);
  assert.equal(result.record.checklist.state, 'unreadable');
  closureField.sectionNodes = [closureRef];
  const generation = h.api.generation;
  h.api.generation = data => data.nodes?.length === 1 && data.nodes[0] === closure ? '8' : generation(data);
  result = await h.read.record(own.ref);
  assert.equal(result.record.checklist.state, 'unreadable');
  assert.equal(result.record.coverage.complete, true);
  h.api.generation = generation;
  const originalRead = h.api.readNodes;
  h.api.readNodes = (targets, selection) => {
    if (targets.some(target => JSON.parse(target).notePath === 'work-log.md')) throw new Error('Log transport failed');
    return originalRead(targets, selection);
  };
  result = await h.read.record(own.ref);
  assert.equal(result.record.checklist.state, 'unreadable');
  assert.equal(result.node.content, 'Full authored prose');
  assert.equal(result.record.status, 'active');
  assert.equal(result.record.coverage.complete, true);
});

test('prompt scope hydration reads only the selected efforts and preserves unknown scope', async () => {
  const chosen = node(ref('chosen.md'), [field('status', [], ['planned'])]);
  const other = node(ref('other.md'), [field('status', [], ['active'])]);
  const h = harness([chosen, other], new Map(), () => {}, { effort: [chosen, other] });
  const result = await h.read.selectedEfforts([chosen.ref, chosen.ref]);
  assert.deepEqual(result.efforts.map(item => item.ref.notePath), ['chosen.md']);
  assert.equal(result.efforts[0].scope.state, 'incomplete');
  assert.equal(result.coverage.complete, false);
  assert.ok(h.calls.every(call => !call.query?.includes('DeliveryInventory')));
  assert.ok(h.calls.every(call => !call.targets?.some(target => target.includes('other.md'))));
});
