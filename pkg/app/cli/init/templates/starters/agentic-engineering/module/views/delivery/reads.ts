import { composePages, composeCoverage, frozenScope, identity, inventoryComplete,
  type Coverage, type Ref, type ScopeMaterial, type ScopeSection, type ScopeSelector, type Occurrence,
  type Portfolio, type Checklist } from './lib/index.ts';
import { NODE_DETAIL, NODE_SUMMARY, SOURCE_PAGE, type LiveNode, type SourcePage,
  type FieldState, type LiveSpec, type LiveStory, type LiveEffort, type RecordDetail, type PortfolioRead } from './queries.ts';

export type ReadAPI = {
  markdownChecklist(source: string): { checked: number; total: number };
  graphql<T>(query: string, variables: Record<string, string | number>): Promise<T>;
  readNodes<T>(refs: readonly string[], selection: string, options: { signal?: AbortSignal }): Promise<{
    nodes: T[]; complete: boolean; errors: { message: string }[];
    unresolved: { requestedRef: string; message: string }[]; truncated: readonly unknown[];
  }>;
  generation(data: unknown): string | undefined;
  roots(data: unknown): Readonly<Record<string, { status: string; hasMore: boolean | null; nextOffset?: number | null; returnedCount: number; offset: number }>> | undefined;
};
const readRef = (ref: Ref): string => ref.ref ?? JSON.stringify(ref);
const absent = (reason: string): Coverage => ({ complete: false, errors: [reason], truncated: false,
  graphGeneration: null, sourceRevision: null });
export const field = (node: LiveNode, name: string): FieldState | undefined => node.workspace?.fields.find(item => item.name === name);
const refs = (node: LiveNode, name: string) => {
  const collection = node.workspace?.collections?.find(item => item.name === name);
  return collection ? collection.items.map(item => item.ref) : field(node, name)?.sectionNodes ?? [];
};
const value = (node: LiveNode, name: string) => field(node, name)?.values?.[0] ?? null;
const revision = (node: LiveNode) => node.workspace?.sourceRevision?.contentFingerprint ?? null;
export function locator(node: LiveNode): string | null {
  const target = node.locator.linkTarget;
  if (!target?.exists || target.requiresFix) return null;
  return target.ref.notePath + (target.ref.fragment ? `#${target.ref.fragment}` : '');
}
const summary = (node: LiveNode, coverage: Coverage) => ({ ref: node.ref, title: node.title, locator: locator(node), coverage });
const specStates = ['proposed', 'active', 'superseded', 'archived'] as const;
const effortStates = ['planned', 'active', 'complete', 'archived'] as const;
const storyStates = ['draft', 'ready', 'satisfied'] as const;
function status<T extends string>(node: LiveNode, name: string, allowed: readonly T[]): T | null {
  return allowed.find(item => item === value(node, name)) ?? null;
}
export function reader(api: ReadAPI, signal?: AbortSignal) {
  const check = () => signal?.throwIfAborted();
  const coverage = (data: unknown, errors: readonly string[] = [], complete = true, sourceRevision: string | null = null): Coverage => ({
    complete: complete && errors.length === 0 && api.generation(data) !== undefined,
    errors, truncated: false, graphGeneration: api.generation(data) ?? null, sourceRevision,
  });
  async function hydrate(targets: readonly Ref[], detail = false) {
    check();
    if (!targets.length) return { nodes: [], coverage: absent('No hydration requested'), missing: false };
    let result: Omit<Awaited<ReturnType<ReadAPI['readNodes']>>, 'nodes'> & { nodes: LiveNode[] };
    try {
      result = await api.readNodes<LiveNode>(targets.map(readRef), detail ? NODE_DETAIL : NODE_SUMMARY, { signal });
    } catch (error) {
      check();
      return { nodes: [], coverage: absent(String(error)), missing: false };
    }
    check();
    const read = coverage(result, [...result.errors.map(error => error.message),
      ...result.unresolved.map(item => `${item.requestedRef}: ${item.message}`)], result.complete);
    read.truncated = result.truncated.length > 0;
    read.complete &&= !read.truncated;
    return { nodes: result.nodes, coverage: read, missing: result.unresolved.length > 0 && !result.errors.length && !result.truncated.length };
  }
  async function occurrences(source: LiveNode) {
    const pages = [];
    let offset = 0;
    while (true) {
      check();
      try {
        const data = await api.graphql<SourcePage>(SOURCE_PAGE, { ref: readRef(source.ref), offset });
        check();
        const workspace = data.node?.workspace;
        if (!workspace) throw new Error('Source workspace unavailable');
        const read = coverage(data, [], true, workspace.sourceRevision?.contentFingerprint ?? null);
        pages.push({ ...read, offset, first: 100, count: workspace.sourceLinkCount, items: workspace.sourceLinks });
        const page = pages[pages.length - 1];
        if (!read.complete || read.sourceRevision !== revision(source)
          || pages.some(item => item.graphGeneration !== read.graphGeneration || item.sourceRevision !== read.sourceRevision)
          || page.items.length !== Math.min(100, page.count - offset) || offset + page.items.length >= page.count) break;
        offset += page.items.length;
      } catch (error) {
        check();
        const combined = composePages(pages);
        return { occurrences: toOccurrences(source.ref, combined.items), coverage: { ...combined.coverage,
          complete: false, errors: [...combined.coverage.errors, String(error)] } };
      }
    }
    const combined = composePages(pages);
    if (combined.coverage.sourceRevision !== revision(source)) {
      combined.coverage.complete = false;
      combined.coverage.errors = [...combined.coverage.errors, 'Source revision changed since subject read'];
    }
    return { occurrences: toOccurrences(source.ref, combined.items), coverage: combined.coverage };
  }
  async function inventory(root: 'specLike' | 'effort') {
    const nodes: LiveNode[] = [];
    const reads: Coverage[] = [];
    let offset = 0;
    while (true) {
      check();
      try {
        const data = await api.graphql<Record<string, LiveNode[]>>(`query DeliveryInventory($offset: Int!) { ${root}(first: 100, offset: $offset) { ... on Node { ${NODE_SUMMARY} } } }`, { offset });
        check();
        const page = data[root] ?? [];
        const facts = api.roots(data)?.[root];
        const errors = !facts || facts.offset !== offset || facts.returnedCount !== page.length ? ['Inventory coverage unavailable or inconsistent'] : [];
        reads.push(coverage(data, errors));
        nodes.push(...page);
        if (!facts || errors.length || !inventoryComplete(reads)) break;
        if (!facts.hasMore) {
          if (facts.status !== 'complete') reads.push(absent(`Inventory ${facts.status}`));
          break;
        }
        if (!page.length || facts.nextOffset !== offset + page.length) { reads.push(absent('Inventory paging did not advance')); break; }
        offset = facts.nextOffset;
      } catch (error) { check(); reads.push(absent(String(error))); break; }
    }
    return { nodes, coverage: combine(reads) };
  }
  async function children(parents: readonly LiveNode[], name: string, detail = false) {
    const targets = parents.flatMap(node => refs(node, name));
    return targets.length ? hydrate(targets, detail) : { nodes: [], coverage: null };
  }
  async function specs(nodes: LiveNode[], base: Coverage, detail = false): Promise<LiveSpec[]> {
    const wrappers = await children(nodes, 'userStories');
    const stories = await children(wrappers.nodes, 'stories', detail);
    const ac = await children(stories.nodes, 'acceptanceCriteria');
    const criteria = await children(ac.nodes, 'criteria', detail);
    const storyCoverage = combine([base, ...[wrappers.coverage, stories.coverage].filter(nonNull)]);
    const criteriaCoverage = combine([storyCoverage, ...[ac.coverage, criteria.coverage].filter(nonNull)]);
    const owned = (parent: LiveNode, name: string, candidates: LiveNode[]) => candidates.filter(node => refs(parent, name).some(ref => identity(ref) === identity(node.ref)));
    return nodes.map(node => ({ ...summary(node, base), source: node, kind: 'spec', status: status(node, 'specStatus', specStates),
      storiesCoverage: storyCoverage,
      stories: owned(node, 'userStories', wrappers.nodes).flatMap(wrapper => owned(wrapper, 'stories', stories.nodes)).map((story): LiveStory => ({
        ...summary(story, storyCoverage), source: story, status: status(story, 'status', storyStates), criteriaCoverage,
        criteria: owned(story, 'acceptanceCriteria', ac.nodes).flatMap(wrapper => owned(wrapper, 'criteria', criteria.nodes))
          .map(criterion => ({ ...summary(criterion, criteriaCoverage), source: criterion, story: story.ref })),
      })),
    }));
  }
  function effort(node: LiveNode, read: Coverage, scope = node.resolvedType === 'EffortWorkspace'
    ? frozenScope({ form: 'EffortWorkspace', selector: { state: 'unreadable', authored: [], material: null, coverage: absent('Frozen scope deferred') } })
    : frozenScope({ form: 'EffortNote', inline: null })): LiveEffort {
    return { ...summary(node, read), source: node, kind: 'effort', form: node.resolvedType === 'EffortWorkspace' ? 'EffortWorkspace' : 'EffortNote',
      status: status(node, 'status', effortStates), checklist: { state: 'deferred' }, scope };
  }
  async function portfolio(review = false): Promise<PortfolioRead> {
    const specRead = await inventory('specLike');
    const effortRead = await inventory('effort');
    const specRecords = await specs(specRead.nodes, specRead.coverage);
    const portfolio: Portfolio = { specs: specRecords, efforts: effortRead.nodes.map(node => effort(node, effortRead.coverage)),
      specCoverage: specRead.coverage, effortCoverage: effortRead.coverage };
    if (review) {
      const efforts: LiveEffort[] = [];
      for (const node of effortRead.nodes) {
        const candidate = effort(node, effortRead.coverage);
        if (candidate.status !== 'active' && candidate.status !== 'complete') { efforts.push(candidate); continue; }
        try {
          const detail = await record(node.ref);
          efforts.push(detail?.record?.kind === 'effort' ? detail.record : { ...candidate, coverage: absent('Review subject unavailable') });
        } catch (error) { check(); efforts.push({ ...candidate, coverage: absent(String(error)) }); }
      }
      portfolio.efforts = efforts;
    }
    return { portfolio, coverage: combine([specRead.coverage, effortRead.coverage,
      ...specRecords.flatMap(spec => [spec.storiesCoverage, ...spec.stories.map(story => story.criteriaCoverage)]),
      ...(review ? portfolio.efforts.filter(item => item.status === 'active' || item.status === 'complete').map(item => item.coverage) : [])]), reviewEvidence: review ? 'loaded' : 'deferred' };
  }
  async function material(node: LiveNode, read: Coverage): Promise<ScopeMaterial> {
    const supported = node.format === 'markdown';
    async function section(name: string): Promise<ScopeSection> {
      if (!supported) return { state: 'unsupported', coverage: read, occurrences: [] };
      const targets = refs(node, name);
      if (field(node, name)?.issues?.length) return { state: targets.length > 1 ? 'duplicate' : 'unreadable', coverage: read, occurrences: [] };
      if (targets.length !== 1) return { state: targets.length ? 'duplicate' : 'missing', coverage: read, occurrences: [] };
      const hydrated = await hydrate(targets, true);
      const child = hydrated.nodes[0];
      if (!child) return { state: 'unreadable', coverage: hydrated.coverage, occurrences: [] };
      const links = await occurrences(child);
      return { state: 'present', coverage: composeCoverage([read, { ...hydrated.coverage, sourceRevision: revision(child) }, links.coverage]), occurrences: links.occurrences };
    }
    return { ref: node.ref, supported, coverage: read, specs: await section('specSetFrozen'), stories: await section('storiesInScopeFrozen') };
  }
  async function checklist(owner: LiveNode, ownerCoverage: Coverage): Promise<{ checklist: Checklist; nodes: LiveNode[]; coverage: Coverage }> {
    const state = field(owner, 'closureChecklist');
    const targets = refs(owner, 'closureChecklist');
    if (!state || state.issues?.length || targets.length > 1) return {
      checklist: { state: 'unreadable' }, nodes: [], coverage: absent('Closure section unavailable or ambiguous') };
    if (!state.present && !targets.length) return { checklist: { state: 'missing' }, nodes: [], coverage: ownerCoverage };
    if (targets.length !== 1) return { checklist: { state: 'unreadable' }, nodes: [], coverage: absent('Closure section unresolved') };
    const section = await hydrate(targets, true);
    const child = section.nodes[0];
    const read = composeCoverage([ownerCoverage, { ...section.coverage, sourceRevision: child ? revision(child) : null }]);
    const counts = read.complete && child?.content !== undefined ? api.markdownChecklist(child.content) : null;
    return { checklist: counts ? { state: counts.total ? 'recorded' : 'empty', ...counts } : { state: 'unreadable' }, nodes: section.nodes, coverage: read };
  }
  async function record(ref: Ref): Promise<RecordDetail | null> {
    const own = await hydrate([ref], true);
    const node = own.nodes[0];
    if (!node) {
      if (!own.missing && !own.coverage.complete) throw new Error(own.coverage.errors.join('\n') || 'Subject read failed');
      return null;
    }
    const ownCoverage = { ...own.coverage, sourceRevision: revision(node) };
    const empty = { coverage: absent('Not applicable'), occurrences: [] };
    if (node.resolvedType?.endsWith('Spec')) {
      const records = await specs([node], ownCoverage, true);
      return { node, record: records[0] ?? null, components: [], selectedTargets: [], governing: empty,
        entryMentions: empty, workLogSnapshot: null, coverage: combine([ownCoverage, records[0].storiesCoverage,
          ...records[0].stories.map(story => story.criteriaCoverage)], revision(node)) };
    }
    if (node.resolvedType !== 'EffortNote' && node.resolvedType !== 'EffortWorkspace') return {
      node, record: null, components: [], selectedTargets: [], governing: empty, entryMentions: empty,
      workLogSnapshot: null, coverage: ownCoverage };
    const componentRefs = node.workspace?.fields.flatMap(item => [...(item.sectionNodes ?? []),
      ...(['implementationPlan', 'workLog', 'frozenScope', 'materials'].includes(item.name)
        ? (item.links ?? []).flatMap(link => link.ref ? [link.ref] : []) : [])]) ?? [];
    const components = componentRefs.length ? await hydrate(componentRefs, true) : { nodes: [], coverage: ownCoverage };
    const selectorField = field(node, 'frozenScope');
    const selectedLinks = selectorField?.links ?? [];
    const selectedNode = components.nodes.find(item => selectedLinks.some(link => link.ref && identity(link.ref) === identity(item.ref)));
    const selector: ScopeSelector = {
      state: !selectorField ? 'unreadable' : !selectorField.present ? 'missing'
        : (selectorField.values?.length ?? 0) > 1 ? 'duplicate'
          : !selectorField.values?.length ? 'broken'
          : selectorField.issues?.length || !selectedNode ? 'broken' : selectedNode.format !== 'markdown' ? 'unsupported' : 'selected',
      authored: selectorField?.values ?? [], coverage: ownCoverage,
      material: selectedNode ? await material(selectedNode, { ...components.coverage, sourceRevision: revision(selectedNode) }) : null,
    };
    const scope = node.resolvedType === 'EffortNote'
      ? frozenScope({ form: 'EffortNote', inline: await material(node, ownCoverage) })
      : frozenScope({ form: 'EffortWorkspace', selector });
    const result = effort(node, ownCoverage, scope);
    result.contextSources = (['implementationPlan', 'workLog'] as const).flatMap(name => {
      const state = field(node, name);
      return (state?.values ?? []).map((authored, index) => {
        const target = state?.links?.[index]?.ref;
        const component = target && components.nodes.find(item => identity(item.ref) === identity(target));
        return { role: name === 'implementationPlan' ? 'implementation plan' as const : 'work log' as const,
          authored, locator: component ? locator(component) : null };
      });
    });
    const entryMentions = await occurrences(node);
    const governingField = field(node, 'governingSpecs');
    const governing = { coverage: governingField ? ownCoverage : absent('Governing metadata unavailable'),
      occurrences: (governingField?.links ?? []).map((link, ordinal): Occurrence => ({ source: node.ref, ordinal, authored: link.value,
        target: link.ref, targetKind: link.resolved ? 'spec' : 'unresolved', link: { target: link.value, authoredTarget: link.value,
          text: link.title, kind: 'field', anchor: null, embed: false, targetKind: 'note', resolved: link.resolved,
          resolvedRef: link.ref, title: link.title, preview: null } })) };
    const logField = field(node, 'workLog');
    const logRefs = (logField?.links ?? []).flatMap(link => link.resolved && link.ref ? [link.ref] : []);
    const logRead = node.resolvedType === 'EffortWorkspace' && logRefs.length === 1 ? await hydrate(logRefs, true) : null;
    const log = logRead?.nodes[0];
    const workLogSnapshot = log && logRead
      ? selectedNode && identity(selectedNode.ref) === identity(log.ref)
        && revision(selectedNode) === revision(log)
        && components.coverage.graphGeneration === logRead.coverage.graphGeneration
        ? selector.material
        : await material(log, { ...logRead.coverage, sourceRevision: revision(log) }) : null;
    const logSectionRefs = log?.workspace?.fields.flatMap(item => item.name === 'closureChecklist' ? [] : item.sectionNodes ?? []) ?? [];
    const logSections = logSectionRefs.length ? await hydrate(logSectionRefs, true) : { nodes: [], coverage: ownCoverage };
    const checklistRead = node.resolvedType === 'EffortNote'
      ? await checklist(node, ownCoverage)
      : log && logRead ? await checklist(log, combine([ownCoverage, logRead.coverage], revision(log)))
        : { checklist: { state: 'unreadable' } satisfies Checklist, nodes: [], coverage: absent('Work log unavailable or ambiguous') };
    result.checklist = checklistRead.checklist;
    const targetRefs = [...scope.specs, ...scope.selections].flatMap(item => item.target ? [item.target] : []);
    const targets = targetRefs.length ? await hydrate(targetRefs, true) : { nodes: [], coverage: ownCoverage };
    scope.coverage = combine([scope.coverage, targets.coverage], scope.coverage.sourceRevision);
    if (!scope.coverage.complete && scope.state === 'known') {
      scope.state = 'incomplete';
      scope.reasons = [...scope.reasons, 'Selected target hydration incomplete or changed generation'];
    }
    const totalCoverage = combine([ownCoverage, components.coverage, scope.coverage, entryMentions.coverage, targets.coverage,
      checklistRead.coverage, logSections.coverage], revision(node));
    const uniqueComponents = [...new Map([...components.nodes, ...(log ? [log] : []), ...logSections.nodes,
      ...checklistRead.nodes].map(item => [identity(item.ref), item])).values()];
    return { node, record: result, components: uniqueComponents, selectedTargets: targets.nodes, governing, entryMentions,
      workLogSnapshot, coverage: totalCoverage };
  }
  async function selectedEfforts(targets: readonly Ref[]) {
    const efforts: LiveEffort[] = [];
    const reads: Coverage[] = [];
    const seen = new Set<string>();
    for (const target of targets) {
      if (seen.has(identity(target))) continue;
      seen.add(identity(target));
      check();
      const detail = await record(target);
      if (detail?.record?.kind === 'effort') {
        efforts.push(detail.record);
        reads.push(detail.record.coverage, detail.record.scope.coverage);
      } else reads.push(absent('Selected effort unavailable'));
    }
    const coverage = combine(reads);
    coverage.complete &&= efforts.every(item => item.scope.state === 'known');
    return { efforts, coverage };
  }
  return { portfolio, record, occurrences, hydrate, selectedEfforts };
}
function nonNull<T>(item: T | null): item is T { return item !== null; }
export function combine(reads: readonly Coverage[], sourceRevision: string | null = null): Coverage {
  return { complete: inventoryComplete(reads), errors: reads.flatMap(read => read.errors),
    truncated: reads.some(read => read.truncated), graphGeneration: reads[0]?.graphGeneration ?? null, sourceRevision };
}
function toOccurrences(source: Ref, links: readonly import('./lib/index.ts').SourceLink[]): Occurrence[] {
  return links.map((link, ordinal) => ({ source, link, ordinal, authored: link.authoredTarget, target: link.resolvedRef,
    targetKind: !link.resolved || !link.resolvedRef ? 'unresolved' : link.resolvedRef.typeName === 'UserStory' ? 'story'
      : link.resolvedRef.typeName === 'AcceptanceCriterion' ? 'criterion'
        : link.resolvedRef.typeName?.endsWith('Spec') ? 'spec' : 'unsupported' }));
}
