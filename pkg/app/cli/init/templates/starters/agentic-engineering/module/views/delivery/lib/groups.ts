import { inventoryComplete, isComplete } from './coverage.ts';
import { identity, type Effort, type Portfolio, type RecordSummary, type Spec, type Story } from './model.ts';

export type Stage = 'shaping' | 'ready' | 'planned' | 'active' | 'history' | 'contracts' | 'review';
export type Row =
  | { kind: 'spec'; record: Spec }
  | { kind: 'story'; record: Story; parent: Spec }
  | { kind: 'effort'; record: Effort }
  | { kind: 'review'; record: Effort; observation: 'closure-checklist' | 'selected-contract'; selected: readonly RecordSummary[] };
export type Group = {
  stage: Stage;
  title: string;
  activity: 'Shape' | 'Build' | 'Settle';
  primary: boolean;
  unit: 'specs' | 'stories' | 'efforts' | 'observations';
  rule: string;
  rows(input: Portfolio): Row[];
};

function effortRows(input: Portfolio, status: Effort['status'][]): Row[] {
  return input.efforts.filter(effort => status.includes(effort.status))
    .map(record => ({ kind: 'effort', record }));
}

function observations(input: Portfolio): Row[] {
  const rows: Row[] = [];
  for (const effort of input.efforts) {
    if (effort.status === 'active' && effort.checklist.state === 'recorded'
      && effort.checklist.total > 0 && effort.checklist.checked === effort.checklist.total) {
      rows.push({ kind: 'review', record: effort, observation: 'closure-checklist', selected: [] });
    }
    if (effort.status !== 'complete' || effort.scope.state !== 'known'
      || !isComplete(effort.scope.coverage)) continue;
    const pending = new Map<string, RecordSummary>();
    for (const occurrence of effort.scope.selections) {
      if (!occurrence.target) continue;
      const targetKey = identity(occurrence.target);
      for (const spec of input.specs) {
        for (const story of spec.stories) {
          if (story.status !== 'draft' && story.status !== 'ready') continue;
          if (!inventoryComplete([effort.coverage, effort.scope.coverage, spec.coverage,
            spec.storiesCoverage, story.coverage])) continue;
          if (occurrence.targetKind === 'story' && identity(story.ref) === targetKey) {
            pending.set(targetKey, story);
          }
          if (occurrence.targetKind === 'criterion' && inventoryComplete([story.criteriaCoverage])) {
            for (const criterion of story.criteria) {
              if (identity(criterion.ref) === targetKey && inventoryComplete([
                effort.scope.coverage, story.criteriaCoverage, criterion.coverage])) pending.set(targetKey, criterion);
            }
          }
        }
      }
    }
    if (pending.size) rows.push({ kind: 'review', record: effort,
      observation: 'selected-contract', selected: [...pending.values()] });
  }
  return rows;
}

export const groups: readonly Group[] = [
  { stage: 'shaping', title: 'Shaping specs', activity: 'Shape', primary: true, unit: 'specs',
    rule: 'Proposed specs and active specs with at least one draft story.',
    rows: input => input.specs.filter(spec => spec.status === 'proposed'
      || spec.status === 'active' && spec.stories.some(story => story.status === 'draft'))
      .map(record => ({ kind: 'spec', record })) },
  { stage: 'ready', title: 'Ready stories', activity: 'Shape', primary: true, unit: 'stories',
    rule: 'Ready stories under proposed or active specs. Proposed contracts need specification before freezing.',
    rows: input => input.specs.filter(spec => spec.status === 'proposed' || spec.status === 'active')
      .flatMap(parent => parent.stories.filter(story => story.status === 'ready')
        .map(record => ({ kind: 'story', record, parent }))) },
  { stage: 'planned', title: 'Planned efforts', activity: 'Build', primary: true, unit: 'efforts',
    rule: 'Efforts recorded as planned.', rows: input => effortRows(input, ['planned']) },
  { stage: 'active', title: 'Active efforts', activity: 'Build', primary: true, unit: 'efforts',
    rule: 'Efforts recorded as active, regardless of checklist state.', rows: input => effortRows(input, ['active']) },
  { stage: 'history', title: 'Delivery history', activity: 'Settle', primary: true, unit: 'efforts',
    rule: 'Efforts recorded as complete or archived, retaining their distinct statuses.',
    rows: input => effortRows(input, ['complete', 'archived']) },
  { stage: 'contracts', title: 'Current contracts', activity: 'Shape', primary: false, unit: 'specs',
    rule: 'Active specs are normative contracts in force. This does not establish delivery.',
    rows: input => input.specs.filter(spec => spec.status === 'active').map(record => ({ kind: 'spec', record })) },
  { stage: 'review', title: 'Record review', activity: 'Settle', primary: false, unit: 'observations',
    rule: 'Inspect active efforts with recorded fully checked checklists, or complete efforts with exact selected pending contract.',
    rows: observations },
];

export function groupFor(stage: string): Group | null {
  return groups.find(group => group.stage === stage) ?? null;
}

export function groupResult(stage: string, input: Portfolio) {
  const group = groupFor(stage);
  if (!group) return { state: 'invalid-route' as const };
  const rows = group.rows(input);
  const specReads = [input.specCoverage, ...input.specs.flatMap(spec =>
    [spec.coverage, spec.storiesCoverage, ...spec.stories.map(story => story.coverage)])];
  const effortReads = [input.effortCoverage, ...input.efforts.map(effort => effort.coverage)];
  const completed = input.efforts.filter(effort => effort.status === 'complete');
  const active = input.efforts.filter(effort => effort.status === 'active');
  const reads = stage === 'review' ? [...specReads, ...effortReads, ...completed.map(effort => effort.scope.coverage),
    ...input.specs.flatMap(spec => spec.stories.flatMap(story =>
      [story.criteriaCoverage, ...story.criteria.map(criterion => criterion.coverage)]))]
    : stage === 'contracts' ? [input.specCoverage, ...input.specs.map(spec => spec.coverage)]
      : ['shaping', 'ready'].includes(stage) ? specReads : effortReads;
  const specFactsKnown = input.specs.every(spec => spec.status !== null);
  const storyFactsKnown = input.specs.every(spec => spec.stories.every(story => story.status !== null));
  const effortFactsKnown = input.efforts.every(effort => effort.status !== null);
  const factsKnown = stage === 'contracts' ? specFactsKnown
    : stage === 'shaping' || stage === 'ready' ? specFactsKnown && storyFactsKnown
      : stage === 'review' ? specFactsKnown && storyFactsKnown && effortFactsKnown : effortFactsKnown;
  return { state: 'records' as const, group, rows, count: rows.length,
    complete: factsKnown && inventoryComplete(reads) && (stage !== 'review' ||
      completed.every(effort => effort.scope.state === 'known') && active.every(effort => effort.checklist.state !== 'unreadable' && effort.checklist.state !== 'deferred')),
    parentSpecCount: stage === 'ready' ? new Set(rows.flatMap(row =>
      row.kind === 'story' ? [identity(row.parent.ref)] : [])).size : null };
}

export function eligibility(row: Row): { eligible: boolean; reason: string | null } {
  if (!row.record.locator) return { eligible: false, reason: 'Durable canonical locator unavailable' };
  if (!inventoryComplete([row.record.coverage])) return { eligible: false, reason: 'Record read incomplete' };
  if (row.kind === 'spec' && !inventoryComplete([row.record.coverage, row.record.storiesCoverage])) {
    return { eligible: false, reason: 'Selected contract read incomplete' };
  }
  if (row.kind === 'story' && (!row.parent.locator || !inventoryComplete([
    row.parent.coverage, row.parent.storiesCoverage, row.record.coverage,
    row.record.criteriaCoverage, ...row.record.criteria.map(criterion => criterion.coverage)]))) {
    return { eligible: false, reason: 'Parent contract read incomplete' };
  }
  if ((row.kind === 'effort' || row.kind === 'review') && (row.record.scope.state !== 'known' || !inventoryComplete([row.record.coverage, row.record.scope.coverage]))) {
    return { eligible: false, reason: 'Selected frozen scope incomplete or unknown' };
  }
  return { eligible: true, reason: null };
}

export function linkable(record: RecordSummary): boolean {
  return !!record.locator;
}
