import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { composeCoverage, composePages, identity, frozenScope, deferredScope, distinctTargets, groups, groupResult, eligibility, promptFor, firstDayPrompt }
  from '../../templates/starters/agentic-engineering/module/views/delivery/lib/index.ts';

const coverage = (extra = {}) => ({ complete: true, truncated: false, errors: [],
  sourceRevision: 'r1', graphGeneration: 7, ...extra });
const ref = (notePath, fragment = '') => ({ notePath, fragment, kind: fragment ? 'section' : 'note',
  nodeId: fragment || notePath, typeName: fragment ? 'UserStory' : 'Spec' });
const record = (path, fragment = '') => ({ ref: ref(path, fragment), title: path,
  locator: path + (fragment ? `#${fragment}` : ''), coverage: coverage() });
const story = (fragment, status = 'ready') => ({ ...record('spec.md', fragment), status,
  criteria: [], criteriaCoverage: coverage() });
const spec = (status = 'active', stories = [story('a'), story('b', 'draft')]) => ({
  ...record('spec.md'), kind: 'spec', status, stories, storiesCoverage: coverage() });
const occurrence = (target, ordinal = 0, targetKind = 'story') => ({ source: ref('scope.md', 'frozen'),
  link: { target: target?.notePath ?? 'missing', authoredTarget: target?.notePath ?? 'missing',
    text: 'authored alias', kind: 'wiki', anchor: target?.fragment ?? null, embed: false,
    targetKind: 'note', resolved: !!target, resolvedRef: target, title: null, preview: null },
  ordinal, authored: target ? `[[${target.notePath}#${target.fragment}]]` : '[[missing]]', target, targetKind });
const section = (occurrences = [], extra = {}) => ({ state: 'present', coverage: coverage(), occurrences, ...extra });
const material = (selections = []) => ({ ref: ref('scope.md'), supported: true, coverage: coverage(),
  specs: section([occurrence(ref('spec.md'), 0, 'spec')]), stories: section(selections) });
const scope = (selections = []) => frozenScope({ form: 'EffortNote', inline: material(selections),
  selector: { state: 'missing', authored: [], material: null, coverage: coverage() } });
const effort = (status = 'active', selections = []) => ({ ...record('effort.md'), kind: 'effort',
  form: 'EffortNote', status, checklist: { state: 'recorded', checked: 0, total: 1 }, scope: scope(selections) });
const portfolio = (specs = [spec()], efforts = [effort()]) => ({ specs, efforts,
  specCoverage: coverage(), effortCoverage: coverage() });
const keys = result => result.rows.map(row => identity(row.record.ref));

test('note evidence joins canonical roots without weakening section or criterion identity', () => {
  const canonical = Object.freeze({ notePath: 'spec.md', ref: 'spec.md', kind: 'NOTE', nodeId: 'spec.md', structural: null, typeName: 'ExperienceSpec' });
  const linked = Object.freeze({ ...canonical, structural: 'catalog-fingerprint', structuralFingerprint: 'catalog-fingerprint' });
  assert.equal(distinctTargets([occurrence(linked)]).has(identity(canonical)), true);
  assert.equal(distinctTargets([occurrence(linked), occurrence(canonical)]).size, 1);
  assert.equal(identity(linked), identity({ notePath: 'spec.md', kind: 'note' }));
  assert.notEqual(identity(canonical), identity({ ...canonical, notePath: 'other.md', ref: 'other.md' }));
  const selected = Object.freeze({ ...canonical, kind: 'SECTION', fragment: 'selection', nodeId: 'spec.md#selection', structural: 'first' });
  assert.notEqual(identity(selected), identity({ ...selected, structural: 'second' }));
  assert.notEqual(identity(selected), identity({ ...selected, fragment: 'other' }));
  assert.notEqual(identity(selected), identity({ ...selected, kind: 'EMBEDDED', typeName: 'AcceptanceCriterion' }));
  assert.notEqual(identity(canonical), identity(selected));
  assert.equal(linked.structural, 'catalog-fingerprint');
});

test('mixed story states overlap factual groups and preserve historical states', () => {
  const input = portfolio([spec(), { ...spec('proposed'), ...record('proposal.md') }],
    [effort(), { ...effort('complete'), ...record('done.md') }, { ...effort('archived'), ...record('old.md') }]);
  assert.equal(groups.filter(group => group.primary).length, 5);
  assert.equal(groupResult('shaping', input).count, 2);
  assert.equal(groupResult('ready', input).count, 2);
  assert.equal(groupResult('ready', input).parentSpecCount, 2);
  assert.equal(groupResult('contracts', input).count, 1);
  assert.equal(groupResult('active', input).count, 1);
  assert.deepEqual(groupResult('history', input).rows.map(row => row.record.status), ['complete', 'archived']);
  assert.equal(groupResult('bogus', input).state, 'invalid-route');
});

test('checked active efforts remain active and observations do not decide acceptance', () => {
  const active = { ...effort(), checklist: { state: 'recorded', checked: 2, total: 2 } };
  const input = portfolio([], [active]);
  assert.equal(groupResult('active', input).count, 1);
  assert.equal(groupResult('review', input).rows[0].observation, 'closure-checklist');
  for (const checklist of [{ state: 'missing' }, { state: 'empty', checked: 0, total: 0 }, { state: 'unreadable' }]) {
    assert.equal(groupResult('review', portfolio([], [{ ...active, checklist }])).count, 0);
  }
});

test('complete effort only observes exact selected pending story or criterion', () => {
  const pending = story('a');
  const criterion = { ...record('spec.md', 'ac-1'), story: pending.ref };
  pending.criteria = [criterion];
  const input = portfolio([spec('active', [pending])], [effort('complete')]);
  assert.equal(groupResult('review', input).count, 0);
  input.efforts[0].scope = scope([occurrence(criterion.ref, 0, 'criterion')]);
  const result = groupResult('review', input);
  assert.deepEqual(result.rows[0].selected.map(item => item.locator), ['spec.md#ac-1']);
  const prompt = promptFor('review', input, keys(result));
  assert.equal(prompt.copyable, true);
  assert.match(prompt.text, /spec.md#ac-1 \(selected pending contract\)/);
  assert.doesNotMatch(prompt.text, /spec.md#a \(selected pending contract\)/);
  criterion.locator = null;
  assert.equal(promptFor('review', input, keys(result)).copyable, false);
  criterion.locator = 'spec.md#ac-1';
  assert.equal(input.efforts[0].scope.selections.some(item => identity(item.target) === identity(pending.ref)), false);
  pending.status = 'satisfied';
  assert.equal(groupResult('review', input).count, 0);
  pending.status = 'ready';
  pending.coverage.graphGeneration = 8;
  assert.equal(groupResult('review', input).count, 0);
  assert.equal(groupResult('ready', input).count, 1);
});

test('explicit workspace selection preserves A, entry B and separate log C roles', () => {
  const a = occurrence(ref('spec.md', 'a'));
  const b = occurrence(ref('spec.md', 'b'));
  const c = occurrence(ref('spec.md', 'c'));
  const selected = material([a, a, occurrence(null, 2, 'unresolved')]);
  const selector = { state: 'selected', authored: ['scope.md'], coverage: coverage(), material: selected };
  const frozen = frozenScope({ form: 'EffortWorkspace', inline: material([b, c]), selector });
  assert.deepEqual(frozen.selections, selected.stories.occurrences);
  assert.equal(frozen.state, 'incomplete');
  assert.equal(distinctTargets(frozen.selections).size, 1);
  assert.equal(frozen.selections[2].authored, '[[missing]]');
  for (const state of ['missing', 'broken', 'duplicate', 'unsupported', 'unreadable']) {
    const result = frozenScope({ form: 'EffortWorkspace', inline: material([c]),
      selector: { ...selector, state, material: null } });
    assert.equal(result.state, state === 'missing' ? 'unknown' : 'incomplete');
    assert.deepEqual(result.authored, ['scope.md']);
    assert.deepEqual(result.selections, []);
  }

});

test('empty scope is known only with present readable sections and coherent selector evidence', () => {
  assert.equal(scope().state, 'known');
  for (const state of ['missing', 'duplicate', 'unreadable', 'unsupported']) {
    const value = material();
    value.stories.state = state;
    assert.equal(frozenScope({ form: 'EffortNote', inline: value, selector: {} }).state, 'incomplete');
  }
  const value = material();
  assert.equal(frozenScope({ form: 'EffortWorkspace', inline: null, selector: {
    state: 'selected', material: value, authored: ['scope.md'], coverage: coverage({ graphGeneration: 8 })
  } }).state, 'incomplete');
});

test('paging and hydration require complete revision and generation evidence', () => {
  for (const count of [0, 99, 100, 101, 200, 201]) {
    const pages = [];
    for (let offset = 0; offset < Math.max(count, 1); offset += 100) pages.push({ ...coverage(), offset, first: 100, count,
      items: Array.from({ length: Math.min(100, count - offset) }, (_, i) => offset + i) });
    const result = composePages(pages);
    assert.equal(result.items.length, count);
    assert.equal(result.coverage.complete, true);
  }
  const page = { ...coverage(), offset: 0, first: 100, count: 101, items: Array(100).fill('same') };
  assert.equal(composePages([page]).coverage.complete, false);
  for (const change of [{ sourceRevision: 'r2' }, { graphGeneration: 8 }, { graphGeneration: null },
    { truncated: true }, { complete: false }, { errors: ['hydration failed'] }]) {
    assert.equal(composeCoverage([coverage(), coverage(change)]).complete, false);
  }
  assert.equal(composePages([page, { ...page, offset: 99, items: ['last'] }]).coverage.complete, false);
  assert.equal(composePages([]).coverage.complete, false);
  assert.equal(composePages([{ ...page, count: 100, items: Array(99).fill('same') },
    { ...page, offset: 99, count: 100, items: ['last'] }]).coverage.complete, false);
  const input = portfolio();
  input.specCoverage.complete = false;
  const result = groupResult('ready', input);
  assert.equal(result.count, 1);
  assert.equal(result.complete, false);
});

test('prompt contains canonical selection once in displayed order and proposed authority', () => {
  const input = portfolio([spec('proposed', [story('a'), story('b')])], []);
  const rows = groupResult('ready', input);
  const prompt = promptFor('ready', input, [...keys(rows).reverse(), keys(rows)[0]]);
  assert.equal(prompt.copyable, true);
  assert.equal(prompt.text.match(/spec.md#a/g).length, 1);
  assert.ok(prompt.text.indexOf('spec.md#a') < prompt.text.indexOf('spec.md#b'));
  assert.match(prompt.text, /proposed parent spec/);
  assert.match(prompt.text, /specify before freezing/);
  assert.match(prompt.text, /effort route, then plan/);
  assert.match(prompt.text, /Missing approver metadata does not establish missing approval/);
  assert.equal(promptFor('ready', input, []).copyable, false);
  assert.equal(promptFor('ready', input, ['stale-key']).copyable, false);
  input.specs[0].stories[0].locator = null;
  assert.equal(eligibility(groupResult('ready', input).rows[0]).eligible, false);
  assert.equal(promptFor('ready', input, keys(rows)).copyable, false);
  assert.match(firstDayPrompt().text, /<describe the outcome>/);
});



test('unknown statuses cannot establish an empty group and legacy logs never become selected sources', () => {
  const input = portfolio([spec(null, [])], [effort(null)]);
  assert.equal(groupResult('ready', input).count, 0);
  assert.equal(groupResult('ready', input).complete, false);
  assert.equal(groupResult('active', input).complete, false);
  const legacy = frozenScope({ form: 'EffortWorkspace', selector: { state: 'missing',
    authored: [], coverage: coverage(), material: material([occurrence(ref('spec.md', 'log-only'))]) } });
  assert.equal(legacy.source, null);
  assert.equal(legacy.state, 'unknown');
  assert.ok(legacy.reasons.every(reason => !reason.includes('generation')));
  assert.deepEqual(legacy.coverage.errors, []);
  assert.deepEqual(legacy.selections, []);
});

test('each work group uses its installed workflow route and incomplete selected criteria block copy', () => {
  const active = { ...effort(), checklist: { state: 'recorded', checked: 1, total: 1 } };
  const input = portfolio([spec('proposed'), { ...spec(), ...record('current.md') }],
    [active, { ...effort('planned'), ...record('plan.md') }, { ...effort('complete'), ...record('history.md') }]);
  for (const [stage, route] of [['shaping', /specify route/], ['planned', /classify route/],
    ['active', /implement route/], ['history', /finish or reconcile route/],
    ['contracts', /specify route/], ['review', /finish or reconcile route/]]) {
    const result = promptFor(stage, input, keys(groupResult(stage, input)));
    assert.equal(result.copyable, true, stage);
    assert.match(result.text, route);
    const skill = readFileSync(new URL('../../templates/starters/agentic-engineering/agents/skills/agentic-engineering/SKILL.md', import.meta.url), 'utf8');
    for (const phase of result.text.matchAll(/(?:agentic-engineering |or )(specify|classify|implement|finish|reconcile)(?: route)?/g)) {
      assert.ok(skill.includes('`' + phase[1] + '`'), `${stage}: installed phase ${phase[1]}`);
    }
  }
  const ready = groupResult('ready', input);
  input.specs[0].stories[0].criteriaCoverage.complete = false;
  assert.equal(promptFor('ready', input, [keys(ready)[0]]).copyable, false);
});


test('inventory generation is sufficient without inventing an inventory source revision', () => {
  const input = portfolio();
  input.specCoverage.sourceRevision = null;
  input.specs[0].coverage.sourceRevision = null;
  assert.equal(groupResult('ready', input).complete, true);
  input.specCoverage.graphGeneration = null;
  assert.equal(groupResult('ready', input).complete, false);
  assert.equal(composeCoverage([coverage({ sourceRevision: null })]).complete, false);
});


test('review coverage depends on active checklists and complete scope, not planned or archived scope', () => {
  const unknown = frozenScope({ form: 'EffortNote', inline: null });
  const input = portfolio([], [
    { ...effort('planned'), ...record('planned.md'), scope: unknown },
    { ...effort('archived'), ...record('archived.md'), scope: unknown },
    { ...effort('active'), ...record('active.md'), scope: unknown },
  ]);
  assert.equal(groupResult('review', input).complete, true);
  input.efforts[2].checklist = { state: 'unreadable' };
  assert.equal(groupResult('review', input).complete, false);
  input.efforts[2].checklist = { state: 'missing' };
  input.efforts.push({ ...effort('complete'), ...record('complete.md'), scope: unknown });
  assert.equal(groupResult('review', input).complete, false);
});

test('effort prompts retain explicit plan and log references without inventing a missing target', () => {
  const item = effort();
  item.contextSources = [
    { role: 'implementation plan', authored: 'plan.html', locator: 'docs/efforts/team/plan.html' },
    { role: 'work log', authored: '[[missing log]]', locator: null },
  ];
  const prompt = promptFor('active', portfolio([], [item]), [identity(item.ref)]);
  assert.equal(prompt.copyable, true);
  assert.match(prompt.text, /docs\/efforts\/team\/plan.html \(implementation plan\)/);
  assert.match(prompt.text, /Recorded work log: \[\[missing log\]\] \(reference not verified in this read\)/);
});

test('a composed multi-effort prompt requires one graph generation', () => {
  const first = effort('active');
  const second = { ...effort('active'), ...record('other-effort.md') };
  second.coverage = coverage({ graphGeneration: 8 });
  second.scope = { ...second.scope, coverage: coverage({ graphGeneration: 8 }) };
  const input = portfolio([], [first, second]);
  const result = promptFor('active', input, [identity(first.ref), identity(second.ref)]);
  assert.equal(result.copyable, false);
  assert.match(result.reasons.join(' '), /generation/i);
});


test('explicit sorting preserves parent groups and prompt order', () => {
  const z = { ...spec('active', [{ ...story('z'), title: 'Zebra' }, { ...story('a'), title: 'Apple' }]), title: 'Zulu' };
  const a = { ...spec('active', [{ ...story('b'), ...record('other.md', 'b'), title: 'Berry' }]), ...record('other.md'), title: 'Alpha' };
  const input = portfolio([z, a], []);
  const source = groupResult('ready', input);
  assert.deepEqual(source.rows.map(row => row.record.title), ['Zebra', 'Apple', 'Berry']);
  const sorted = groupResult('ready', input, 'title-asc');
  assert.deepEqual(sorted.rows.map(row => row.record.title), ['Berry', 'Apple', 'Zebra']);
  const prompt = promptFor('ready', input, keys(source), 'title-asc');
  assert.ok(prompt.text.indexOf('other.md#b') < prompt.text.indexOf('spec.md#a'));
  assert.ok(prompt.text.indexOf('spec.md#a') < prompt.text.indexOf('spec.md#z'));
});

test('summary efforts may be selected before their frozen scope loads, but not copied', () => {
  const row = { kind: 'effort', record: { ...effort(), scope: deferredScope() } };
  assert.equal(eligibility(row, 'select').eligible, true);
  assert.equal(eligibility(row).eligible, false);
  assert.equal(eligibility({ ...row, kind: 'review', observation: 'closure-checklist', selected: [] }, 'select').eligible, false);
  row.record.coverage.complete = false;
  assert.equal(eligibility(row, 'select').eligible, false);
});

test('unknown or incomplete frozen scope is noted once per effort and never blocks copy', () => {
  const unknown = { ...effort('active'), ...record('legacy.html'), scope: frozenScope({ form: 'EffortWorkspace',
    selector: { state: 'missing', authored: [], material: null, coverage: coverage() } }) };
  const incomplete = { ...effort('active'), ...record('bare.md'), scope: frozenScope({ form: 'EffortNote', inline: null }) };
  const input = portfolio([], [effort('active'), unknown, incomplete]);
  const prompt = promptFor('active', input, keys(groupResult('active', input)));
  assert.equal(prompt.copyable, true, prompt.reasons.join('\n'));
  assert.match(prompt.text, /^- effort\.md \(active\)$/m);
  assert.match(prompt.text, /^- legacy\.html \(active; frozen scope not recorded, inspect the effort\)$/m);
  assert.match(prompt.text, /^- bare\.md \(active; frozen scope incomplete in this read, inspect the effort\)$/m);
  unknown.locator = null;
  incomplete.locator = null;
  const blocked = promptFor('active', input, keys(groupResult('active', input)));
  assert.equal(blocked.copyable, false);
  assert.deepEqual(blocked.reasons, ['Durable canonical locator unavailable']);
});

test('an unrecorded workspace selector is unknown scope, not a failed read', () => {
  const selector = { state: 'missing', authored: [], material: null, coverage: coverage() };
  const unknown = frozenScope({ form: 'EffortWorkspace', selector });
  assert.equal(unknown.state, 'unknown');
  assert.equal(unknown.coverage.complete, true);
  const broken = frozenScope({ form: 'EffortWorkspace', selector: { ...selector, state: 'broken' } });
  assert.equal(broken.state, 'incomplete');
  assert.equal(broken.coverage.complete, false);
});
