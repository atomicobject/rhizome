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
  const coverage = material || input.form === 'EffortNote' ? composeCoverage(reads)
    : { ...input.selector.coverage, complete: false, sourceRevision: null };
  const selectorReadMatchesGeneration = input.form !== 'EffortWorkspace'
    || isComplete(input.selector.coverage) && (!material || input.selector.coverage.graphGeneration === coverage.graphGeneration);
  if (!selectorReadMatchesGeneration) {
    reasons.push('Selector read incomplete or graph generation changed');
  }
  if (material && !isComplete(coverage)) reasons.push('Frozen source read incomplete');
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

export function deferredScope(): FrozenScope {
  const reason = 'Frozen scope not read';
  return { state: 'deferred', source: null, authored: [], specs: [], selections: [], reasons: [reason],
    coverage: { complete: false, errors: [reason], truncated: false, sourceRevision: null, graphGeneration: null } };
}

/** Navigation lookup only. Ordered occurrences remain the authority for provenance. */
export function distinctTargets(occurrences: readonly Occurrence[]): Map<string, Ref> {
  const targets = new Map<string, Ref>();
  for (const occurrence of occurrences) {
    if (occurrence.target) targets.set(identity(occurrence.target), occurrence.target);
  }
  return targets;
}
