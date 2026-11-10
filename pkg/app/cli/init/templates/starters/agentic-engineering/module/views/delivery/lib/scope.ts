import { composeCoverage, isComplete } from './coverage.ts';
import { identity, type FrozenScope, type Occurrence, type Ref,
  type ScopeMaterial, type ScopeSelector } from './model.ts';

function targetsKnown(items: readonly Occurrence[]): boolean {
  return items.every(item => item.target && item.targetKind !== 'unresolved' && item.targetKind !== 'unsupported');
}

export function frozenScope(input:
  | { form: 'EffortNote'; inline: ScopeMaterial | null }
  | { form: 'EffortWorkspace'; selector: ScopeSelector }
): FrozenScope {
  const material = input.form === 'EffortNote' ? input.inline
    : input.selector.state === 'missing' ? null : input.selector.material;
  const reasons: string[] = [];
  if (input.form === 'EffortWorkspace' && input.selector.state !== 'selected') {
    reasons.push(`Frozen scope selector ${input.selector.state}`);
  }
  if (!material) reasons.push('Authoritative material unavailable');
  if (material && !material.supported) reasons.push('Selected material unsupported');
  for (const section of material ? [material.specs, material.stories] : []) {
    if (section.state !== 'present') reasons.push(`Frozen section ${section.state}`);
    if (!targetsKnown(section.occurrences)) reasons.push('Frozen target unresolved or unsupported');
  }
  const reads = material ? [material.coverage, material.specs.coverage, material.stories.coverage] : [];
  const coverage = composeCoverage(reads);
  // A selector belongs to the entry, whose revision differs from the material's.
  if (input.form === 'EffortWorkspace' && (!isComplete(input.selector.coverage)
    || input.selector.coverage.graphGeneration !== coverage.graphGeneration)) {
    reasons.push('Selector read incomplete or graph generation changed');
  }
  if (!isComplete(coverage)) reasons.push('Frozen source read incomplete');
  const authoritative = input.form === 'EffortNote' || input.selector.state === 'selected';
  return {
    state: input.form === 'EffortWorkspace' && input.selector.state === 'missing' ? 'unknown'
      : reasons.length === 0 ? 'known' : 'incomplete',
    source: material?.ref ?? null,
    authored: input.form === 'EffortWorkspace' ? input.selector.authored : [],
    specs: authoritative ? material?.specs.occurrences ?? [] : [],
    selections: authoritative ? material?.stories.occurrences ?? [] : [],
    coverage, reasons,
  };
}

/** Navigation lookup only. Ordered occurrences remain the authority for provenance. */
export function distinctTargets(occurrences: readonly Occurrence[]): Map<string, Ref> {
  const targets = new Map<string, Ref>();
  for (const occurrence of occurrences) {
    if (occurrence.target) targets.set(identity(occurrence.target), occurrence.target);
  }
  return targets;
}

export type SourceRole = 'governing' | 'frozen' | 'entry-mention' | 'work-log';
export function sourceRoles(sources: readonly { ref: Ref; role: SourceRole }[]):
  { ref: Ref; roles: SourceRole[] }[] {
  const result = new Map<string, { ref: Ref; roles: SourceRole[] }>();
  for (const { ref, role } of sources) {
    const key = identity(ref);
    const entry = result.get(key) ?? { ref, roles: [] };
    if (!entry.roles.includes(role)) entry.roles.push(role);
    result.set(key, entry);
  }
  return [...result.values()];
}

/** Exact matching never promotes a criterion selection to its containing story. */
export function selectionFor(scope: FrozenScope, target: Ref) {
  const key = identity(target);
  return { known: scope.state === 'known' && isComplete(scope.coverage),
    occurrences: scope.selections.filter(item => item.target && identity(item.target) === key) };
}

export type EffortEvidence = {
  governing: { coverage: FrozenScope['coverage']; occurrences: readonly Occurrence[] };
  entryMentions: { coverage: FrozenScope['coverage']; occurrences: readonly Occurrence[] };
  frozen: FrozenScope;
  workLogSnapshot: ScopeMaterial | null;
};

export function attributionFor(evidence: EffortEvidence, target: Ref) {
  const key = identity(target);
  const matches = (items: readonly Occurrence[]) => items.filter(item =>
    item.target && identity(item.target) === key);
  const log = evidence.workLogSnapshot;
  const sharedSource = !!log && !!evidence.frozen.source
    && identity(log.ref) === identity(evidence.frozen.source);
  return {
    governing: { known: isComplete(evidence.governing.coverage) && targetsKnown(evidence.governing.occurrences), occurrences: matches(evidence.governing.occurrences) },
    frozenSpecs: { known: evidence.frozen.state === 'known', occurrences: matches(evidence.frozen.specs) },
    selected: selectionFor(evidence.frozen, target),
    entryMentions: { known: isComplete(evidence.entryMentions.coverage) && targetsKnown(evidence.entryMentions.occurrences), occurrences: matches(evidence.entryMentions.occurrences) },
    sharedSource,
    workLogSnapshot: log && !sharedSource ? {
      known: log.supported && targetsKnown(log.specs.occurrences) && targetsKnown(log.stories.occurrences)
        && log.specs.state === 'present' && log.stories.state === 'present'
        && isComplete(composeCoverage([log.coverage, log.specs.coverage, log.stories.coverage])),
      specs: matches(log.specs.occurrences), selections: matches(log.stories.occurrences),
    } : null,
  };
}
