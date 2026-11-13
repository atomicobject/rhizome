import assert from 'node:assert/strict';
import { test } from 'node:test';
import { reader, locator } from '../../templates/starters/agentic-engineering/module/views/delivery/reads.ts';
import { identity, promptFor } from '../../templates/starters/agentic-engineering/module/views/delivery/lib/index.ts';
import { NODE_DETAIL } from '../../templates/starters/agentic-engineering/module/views/delivery/queries.ts';
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
  assert.equal(result.record.scope.selections.some(item => identity(item.target) === identity(b)), false);
  assert.equal(result.entryMentions.occurrences[0].target.fragment, 'b');
  assert.equal(result.workLogSnapshot.stories.occurrences[0].target.fragment, 'c');
  assert.equal(result.node.content, 'Full authored prose');
  for (const source of nodes.filter(item => ![b, c].some(target => identity(target) === identity(item.ref)))) {
    assert.equal(h.calls.filter(call => call.targets?.includes(JSON.stringify(source.ref))).length, 1, source.ref.fragment || source.ref.notePath);
  }
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
  const originalRead = h.api.readNodes;
  h.api.readNodes = async (...args) => { const result = await originalRead(...args); result.nodes.reverse(); return result; };
  const result = await h.read.record(own.ref);
  assert.deepEqual(result.record.stories.map(s => s.ref.fragment), ['b', 'a']);
  assert.equal(result.record.stories[1].criteria[0].source.content, 'Full authored prose');
  own.workspace.fields.find(item => item.name === 'userStories').sectionNodes.push(wrapper.ref);
  const duplicate = await h.read.record(own.ref);
  assert.equal(duplicate.record.storiesCoverage.complete, false);
  assert.deepEqual(duplicate.record.stories.map(story => story.ref.fragment), ['b', 'a']);
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

test('child hydration faults preserve healthy criterion neighbors and disclose shared failures', async () => {
  for (const fault of ['unresolved', 'owned-field', 'global-field', 'foreign-owner', 'transport', 'truncated', 'opaque', 'unreported', 'generation', 'missing-generation']) {
    const specs = ['spec.md', 'clean.md'].map(path => node(ref(path, 'ExperienceSpec'), [field('specStatus', [], ['active'])]));
    const wrappers = specs.map(spec => node(ref(spec.ref.notePath, 'UserStoriesSection', 'stories')));
    specs.forEach((spec, i) => spec.workspace.fields.push(field('userStories', [wrappers[i].ref])));
    const criteria = ['a', 'b', 'c'].map((id, i) => node(ref(i === 2 ? 'clean.md' : 'spec.md', 'AcceptanceCriterion', id)));
    const packs = criteria.map(criterion => node(ref(criterion.ref.notePath, 'AcceptanceCriteriaSection', `criteria-${criterion.ref.fragment}`)));
    packs.forEach((pack, i) => pack.workspace.collections = [{ name: 'criteria', items: [{ ref: criteria[i].ref }] }]);
    const stories = packs.map((pack, i) => node(ref(pack.ref.notePath, 'UserStory', `story-${i}`), [field('status', [], ['ready']), field('acceptanceCriteria', [pack.ref])]));
    wrappers[0].workspace.collections = [{ name: 'stories', items: stories.slice(0, 2).map(story => ({ ref: story.ref })) }];
    wrappers[1].workspace.collections = [{ name: 'stories', items: [{ ref: stories[2].ref }] }];
    const h = harness([...specs, ...wrappers, ...stories, ...packs, ...criteria], new Map(), () => {}, { specLike: specs });
    const readNodes = h.api.readNodes;
    const generation = h.api.generation;
    let altered;
    h.api.generation = data => data === altered && fault === 'generation' ? '8'
      : data === altered && fault === 'missing-generation' ? undefined : generation(data);
    h.api.readNodes = async (...args) => {
      const result = await readNodes(...args);
      if (!args[0].includes(JSON.stringify(criteria[0].ref))) return result;
      altered = result;
      result.complete = fault === 'generation' || fault === 'missing-generation';
      if (fault === 'unresolved') {
        result.nodes = result.nodes.filter(item => identity(item.ref) !== identity(criteria[0].ref));
        result.unresolved.push({ requestedRef: JSON.stringify(criteria[0].ref), message: 'Criterion unavailable' });
      }
      if (fault === 'unreported') result.nodes = result.nodes.filter(item => identity(item.ref) !== identity(criteria[0].ref));
      if (fault.endsWith('field') || fault === 'foreign-owner') result.errors.push({ message: 'Criterion field unavailable',
        extensions: fault === 'owned-field' ? { requestedRef: JSON.stringify(criteria[0].ref) }
          : fault === 'foreign-owner' ? { requestedRef: 'not-requested' } : {} });
      if (fault === 'transport') throw new Error('Batch unavailable');
      if (fault === 'truncated') result.truncated.push({ path: ['nodes'], returned: 2, limit: 2 });
      return result;
    };
    const result = await h.read.portfolio();
    assert.equal(result.coverage.complete, false, fault);
    assert.equal(promptFor('ready', result.portfolio, [identity(stories[0].ref)]).copyable, false, `affected: ${fault}`);
    for (const story of stories.slice(1)) assert.equal(promptFor('ready', result.portfolio, [identity(story.ref)]).copyable,
      fault === 'unresolved' || fault === 'owned-field', `healthy ${story.ref.notePath}: ${fault}`);
    assert.equal(result.portfolio.specs[0].stories[1].source.content, 'Full authored prose');
    assert.equal(h.calls.filter(call => call.targets?.includes(JSON.stringify(criteria[0].ref))).length, 1, `batched: ${fault}`);
  }
});

test('hydration honors canonical resolution across enriched refs and refuses omitted canonical replies', async () => {
  const requested = { notePath: 'effort.md', kind: 'NOTE', typeName: 'EffortNote', ref: 'effort.md', structural: 'source-fingerprint' };
  const own = node({ ...requested, structural: null });
  const h = harness();
  h.api.generation = () => '7';
  h.api.readNodes = async () => ({ nodes: [own], complete: true, errors: [], unresolved: [], truncated: [] });
  for (const target of [requested, { notePath: 'effort.md', kind: 'note' }]) {
    const result = await reader(h.api).hydrate([target], NODE_DETAIL);
    assert.equal(result.coverage.complete, true);
    assert.equal(result.nodes[0].ref, own.ref);
  }
  h.api.readNodes = async () => ({ nodes: [], complete: true, errors: [], unresolved: [], truncated: [] });
  await assert.rejects(reader(h.api).record(requested), /absent from batch response/);
});

test('API hydration consumes the canonical public ref and keeps the returned ref object', async () => {
  const own = node({ ...ref('spec.md', 'ExperienceSpec'), ref: 'canonical:spec', structural: 'raw', structuralFingerprint: 'raw' });
  const h = harness();
  let requested;
  h.api.readNodes = async targets => {
    requested = targets;
    return { nodes: [own], complete: true, errors: [], unresolved: [], truncated: [] };
  };
  const result = await reader(h.api).hydrate([own.ref], NODE_DETAIL);
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
  assert.equal(result.coverage.complete, true, result.coverage.errors.join('\n'));
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
  h.api.generation = data => data.nodes?.includes(closure) ? '8' : generation(data);
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
  assert.equal(result.coverage.complete, true);
  assert.ok(h.calls.every(call => !call.query?.includes('DeliveryInventory')));
  assert.ok(h.calls.every(call => !call.targets?.some(target => target.includes('other.md'))));
});


function inlineFixture(path) {
  const r = ref(path);
  const specs = node(ref(path, 'NarrativeSection', 'specs'));
  const stories = node(ref(path, 'NarrativeSection', 'stories'));
  const closure = { ...node(ref(path, 'NarrativeSection', 'closure')), content: '- [x] Verified\n- [x] Backported' };
  const own = node(r, [field('status', [], ['active']), field('specSetFrozen', [specs.ref]),
    field('storiesInScopeFrozen', [stories.ref]), field('closureChecklist', [closure.ref])]);
  return { own, nodes: [own, specs, stories, closure] };
}


test('selected efforts with unrecorded or incomplete frozen scope stay copyable with one note each', async () => {
  const known = inlineFixture('known.md');
  const workspace = node(ref('legacy/entry.html', 'EffortWorkspace'), [field('status', [], ['active']),
    { ...field('frozenScope'), present: false }], 'html');
  const bare = node(ref('bare.md'), [field('status', [], ['active'])]);
  const efforts = [known.own, workspace, bare];
  const h = harness([...known.nodes, workspace, bare], new Map(), () => {}, { specLike: [], effort: efforts });
  const { portfolio } = await h.read.portfolio();
  const selection = await h.read.selectedEfforts(efforts.map(item => item.ref));
  assert.deepEqual(selection.efforts.map(item => item.scope.state), ['known', 'unknown', 'incomplete']);
  assert.equal(selection.coverage.complete, true, selection.coverage.errors.join('\n'));
  assert.equal(selection.coverage.graphGeneration, portfolio.effortCoverage.graphGeneration);
  const loaded = new Map(selection.efforts.map(item => [identity(item.ref), item]));
  const current = { ...portfolio, efforts: portfolio.efforts.map(item => loaded.get(identity(item.ref)) ?? item) };
  const prompt = promptFor('active', current, efforts.map(item => identity(item.ref)));
  assert.equal(prompt.copyable, true, prompt.reasons.join('\n'));
  assert.match(prompt.text, /^- known\.md \(active\)$/m);
  assert.match(prompt.text, /^- legacy\/entry\.html \(active; frozen scope not recorded, inspect the effort\)$/m);
  assert.match(prompt.text, /^- bare\.md \(active; frozen scope incomplete in this read, inspect the effort\)$/m);
  assert.equal(prompt.text.match(/frozen scope/g).length, 2);
});

test('selected effort failures retain healthy siblings and bounded work', async () => {
  const fixtures = Array.from({ length: 7 }, (_, i) => inlineFixture(`selected-${i}.md`));
  const h = harness(fixtures.flatMap(fixture => fixture.nodes));
  const original = h.api.readNodes;
  let active = 0;
  let peak = 0;
  h.api.readNodes = async (targets, selection) => {
    active++;
    peak = Math.max(peak, active);
    await new Promise(resolve => setImmediate(resolve));
    try {
      if (targets.length === 1 && JSON.parse(targets[0]).notePath === 'selected-1.md') throw new Error('Selected subject unavailable');
      return await original(targets, selection);
    } finally { active--; }
  };
  const result = await h.read.selectedEfforts(fixtures.map(fixture => fixture.own.ref));
  assert.deepEqual(result.efforts.map(effort => effort.ref.notePath), fixtures.filter((_, i) => i !== 1).map(fixture => fixture.own.ref.notePath));
  assert.equal(result.coverage.complete, false);
  assert.match(result.coverage.errors.join(' '), /Selected subject unavailable/);
  assert.ok(result.efforts.every(effort => effort.scope.state === 'known'));
  assert.ok(peak > 1 && peak <= 4, `in-flight reads: ${peak}`);
});

test('inline effort reuses hydrated narrative sections for scope and checklist', async () => {
  const fixture = inlineFixture('bounded.md');
  const h = harness(fixture.nodes);
  const result = await h.read.record(fixture.own.ref);
  assert.equal(result.record.scope.state, 'known');
  assert.deepEqual(result.record.checklist, { state: 'recorded', checked: 2, total: 2 });
  assert.equal(h.calls.length, 5, `requests: ${h.calls.length}`);
  for (const source of fixture.nodes) assert.equal(h.calls.filter(call => call.targets?.includes(JSON.stringify(source.ref))).length, 1, source.ref.fragment);
});

test('a failed selected material read is unreadable rather than a broken authored selector', async () => {
  const scope = ref('scope.md', 'EffortMaterial');
  const own = node(ref('entry.html', 'EffortWorkspace'), [field('status', [], ['active']),
    field('frozenScope', [], ['scope.md'], [{ value: 'scope.md', resolved: true, ref: scope }])], 'html');
  const h = harness([own]);
  const original = h.api.readNodes;
  h.api.readNodes = (targets, selection) => targets.includes(JSON.stringify(scope))
    ? Promise.reject(new Error('Material unavailable')) : original(targets, selection);
  const result = await h.read.record(own.ref);
  assert.ok(result.record.scope.reasons.includes('Frozen scope selector unreadable'));
  assert.equal(result.node.content, own.content);
});
