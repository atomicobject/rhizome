import { composePages, composeCoverage, deferredScope, frozenScope, distinctTargets, identity, inventoryComplete,
  type Coverage, type Ref, type ScopeMaterial, type ScopeSection, type ScopeSelector, type Occurrence,
  type Portfolio, type Checklist } from './lib/index.ts';
import { NODE_DETAIL, NODE_SUMMARY, REVISION, SECTION_CHILDREN, SOURCE_PAGE, SPEC_DETAIL_CHILDREN, SPEC_DETAIL_TREE,
  SPEC_TREE, SPEC_TREE_ALIAS, SPEC_TREE_CHILDREN, SPEC_PAGE_SIZE, SPEC_PAGE_WINDOW, type LiveNode, type SourcePage,
  type FieldState, type LiveSpec, type LiveStory, type LiveEffort, type RecordDetail, type PortfolioRead } from './queries.ts';

export type ReadAPI = {
  markdownChecklist(source: string): { checked: number; total: number };
  graphql<T>(query: string, variables: Record<string, string | number>): Promise<T>;
  readNodes<T>(refs: readonly string[], selection: string, options: { signal?: AbortSignal }): Promise<{
    nodes: T[]; complete: boolean; errors: { message: string; extensions?: { requestedRef?: unknown } }[];
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
function selectorState(state: FieldState | undefined, node: LiveNode | undefined, read: Coverage): ScopeSelector['state'] {
  if (!state) return 'unreadable';
  if (!state.present) return 'missing';
  if ((state.values?.length ?? 0) > 1) return 'duplicate';
  if (!state.values?.length || state.issues?.length || state.links?.length !== 1 || !state.links[0].resolved) return 'broken';
  if (!node) return read.complete ? 'broken' : 'unreadable';
  return node.format === 'markdown' ? 'selected' : 'unsupported';
}
export function reader(api: ReadAPI, signal?: AbortSignal) {
  const check = () => signal?.throwIfAborted();
  // Records the spec tree inventory already returned, by selection and ref, with the generation of their page.
  const prefetched = new Map<string, Map<string, { node: LiveNode; generation: string | undefined }>>();
  const merged = new WeakMap<object, string | undefined>();
  const generation = (data: unknown) => typeof data === 'object' && data !== null && merged.has(data) ? merged.get(data) : api.generation(data);
  const coverage = (data: unknown, errors: readonly string[] = [], complete = true, sourceRevision: string | null = null): Coverage => ({
    complete: complete && errors.length === 0 && generation(data) !== undefined,
    errors, truncated: false, graphGeneration: generation(data) ?? null, sourceRevision,
  });
  function remember(spec: LiveNode, pageGeneration: string | undefined, children: Record<string, string> = SPEC_TREE_CHILDREN) {
    const visit = (value: unknown, selection: string) => {
      for (const item of [value].flat()) {
        if (!item || typeof item !== 'object') continue;
        const node = item as LiveNode & Record<string, unknown>;
        for (const [key, childSelection] of Object.entries(children)) {
          if (key in node) { visit(node[key], childSelection); delete node[key]; }
        }
        if (node.ref?.ref) {
          const known = prefetched.get(selection) ?? new Map();
          prefetched.set(selection, known.set(node.ref.ref, { node, generation: pageGeneration }));
        }
      }
    };
    const record = spec as LiveNode & Record<string, unknown>;
    for (const key of Object.keys(record)) {
      if (key.startsWith(SPEC_TREE_ALIAS)) { visit(record[key], SECTION_CHILDREN); delete record[key]; }
    }
  }
  async function readNodes(refs: string[], selection: string, specTree = false): ReturnType<ReadAPI['readNodes']> {
    if (specTree) {
      // As with inventory pages, a tree read with any error is read again flat so each error stays with the record that owns it.
      const nested = await api.readNodes<LiveNode>(refs, `${selection} ${SPEC_DETAIL_TREE}`, { signal }).catch(() => null);
      check();
      if (nested && !nested.errors.length) {
        for (const node of nested.nodes) remember(node, api.generation(nested), SPEC_DETAIL_CHILDREN);
        return nested;
      }
    }
    const known = prefetched.get(selection);
    const unique = [...new Set(refs)];
    const hits = known ? unique.flatMap(ref => known.get(ref) ?? []) : [];
    if (!hits.length) return api.readNodes<LiveNode>(refs, selection, { signal });
    const missing = unique.filter(ref => !known?.has(ref));
    const rest = missing.length ? await api.readNodes<LiveNode>(missing, selection, { signal }) : null;
    const generations = new Set([...hits.map(hit => hit.generation), ...(rest ? [api.generation(rest)] : [])]);
    const seen = new Set<string>();
    const result = {
      nodes: [...hits.map(hit => hit.node), ...rest?.nodes ?? []].filter(node => !seen.has(identity(node.ref)) && !!seen.add(identity(node.ref))),
      complete: rest?.complete ?? true, errors: rest?.errors ?? [], unresolved: rest?.unresolved ?? [], truncated: rest?.truncated ?? [],
    };
    merged.set(result, generations.size === 1 ? [...generations][0] : undefined);
    return result;
  }
  /** With specTree, a spec target also brings its stories and criteria for later hydration. */
  async function hydrate(targets: readonly Ref[], selection: string, specTree = false) {
    check();
    if (!targets.length) return { nodes: [], coverage: absent('No hydration requested'), missing: false,
      coverageFor: (_owned: readonly Ref[]) => absent('No hydration requested') };
    let result: Omit<Awaited<ReturnType<ReadAPI['readNodes']>>, 'nodes'> & { nodes: LiveNode[] };
    try {
      result = await readNodes(targets.map(readRef), selection, specTree) as typeof result;
    } catch (error) {
      check();
      const failed = absent(String(error));
      return { nodes: [], coverage: failed, missing: false, coverageFor: (_owned: readonly Ref[]) => failed };
    }
    check();
    const requested = new Set(targets.map(readRef));
    const diagnostics = new Map<string, string[]>();
    const sharedErrors: string[] = [];
    function report(owner: unknown, message: string) {
      if (typeof owner !== 'string' || !requested.has(owner)) { sharedErrors.push(message); return; }
      diagnostics.set(owner, [...diagnostics.get(owner) ?? [], message]);
    }
    for (const error of result.errors) report(error.extensions?.requestedRef, error.message);
    for (const item of result.unresolved) report(item.requestedRef, `${item.requestedRef}: ${item.message}`);
    const returned = new Set(result.nodes.map(node => node.ref.ref));
    const unresolved = new Set(result.unresolved.map(item => item.requestedRef));
    const unanswered = targets.filter(target => typeof target.ref === 'string' && !returned.has(target.ref) && !unresolved.has(readRef(target)));
    if (unanswered.length) sharedErrors.push(`${unanswered.length} requested nodes absent from batch response`);
    if (!result.complete && !result.errors.length && !result.unresolved.length && !result.truncated.length) {
      sharedErrors.push('Hydration completeness unavailable');
    }
    function coverageFor(owned: readonly Ref[]) {
      const read = coverage(result, [...sharedErrors, ...owned.flatMap(target => diagnostics.get(readRef(target)) ?? [])]);
      read.truncated = result.truncated.length > 0;
      read.complete &&= !read.truncated;
      return read;
    }
    return { nodes: result.nodes, coverage: coverageFor(targets), coverageFor,
      missing: result.unresolved.length > 0 && !result.errors.length && !result.truncated.length && !sharedErrors.length };
  }
  type LoadedNodes = Pick<Awaited<ReturnType<typeof hydrate>>, 'nodes' | 'coverageFor'>;
  function selectedNodes(loaded: LoadedNodes, targets: readonly Ref[]) {
    const keys = new Set(targets.map(identity));
    return { nodes: loaded.nodes.filter(node => keys.has(identity(node.ref))), coverage: loaded.coverageFor(targets) };
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
  async function inventory(root: 'specLike' | 'effort', tree = '') {
    const size = tree ? SPEC_PAGE_SIZE : 100;
    const query = (nested: boolean) => `query DeliveryInventory($offset: Int!) { ${root}(first: ${size}, offset: $offset) { ... on Node { ${NODE_SUMMARY} } ${nested ? tree : ''} } }`;
    // The server fails a whole response for one bad nested field, so a failed tree page is read again flat.
    async function page(offset: number) {
      if (tree) {
        try { return { data: await api.graphql<Record<string, LiveNode[]>>(query(true), { offset }), nested: true }; } catch { check(); }
      }
      return { data: await api.graphql<Record<string, LiveNode[]>>(query(false), { offset }), nested: false };
    }
    const nodes: LiveNode[] = [];
    const reads: Coverage[] = [];
    for (let offset = 0; ; offset += size * (tree ? SPEC_PAGE_WINDOW : 1)) {
      check();
      const results = await Promise.all(Array.from({ length: tree ? SPEC_PAGE_WINDOW : 1 }, (_, index) =>
        page(offset + index * size).then(value => ({ value }), (error: unknown) => ({ error }))));
      check();
      for (const [index, result] of results.entries()) {
        const at = offset + index * size;
        if ('error' in result) { reads.push(absent(String(result.error))); return { nodes, coverage: combine(reads) }; }
        const { data, nested } = result.value;
        const list = data[root] ?? [];
        const facts = api.roots(data)?.[root];
        const errors = !facts || facts.offset !== at || facts.returnedCount !== list.length ? ['Inventory coverage unavailable or inconsistent'] : [];
        reads.push(coverage(data, errors));
        if (nested) for (const node of list) remember(node, api.generation(data));
        nodes.push(...list);
        if (!facts || errors.length || !inventoryComplete(reads)) return { nodes, coverage: combine(reads) };
        if (!facts.hasMore) {
          if (facts.status !== 'complete') reads.push(absent(`Inventory ${facts.status}`));
          return { nodes, coverage: combine(reads) };
        }
        if (!list.length || facts.nextOffset !== at + list.length) {
          reads.push(absent('Inventory paging did not advance'));
          return { nodes, coverage: combine(reads) };
        }
      }
    }
  }
  async function children(parents: readonly LiveNode[], name: string, selection: string) {
    const targets = parents.flatMap(node => refs(node, name));
    const result = targets.length ? await hydrate(targets, selection) : { nodes: [], coverageFor: (_owned: readonly Ref[]) => null };
    return { nodes: result.nodes, coverage(parent: LiveNode) {
      const errors = [
        ...(field(parent, name)?.issues ?? []).map(issue => `${parent.title}: ${name}: ${issue.message}`),
        ...(['userStories', 'acceptanceCriteria'].includes(name) && refs(parent, name).length > 1
          ? [`${parent.title}: ${name} matches multiple sections`] : []),
      ];
      const read = result.coverageFor(refs(parent, name));
      return errors.length ? combine([...[read].filter(nonNull), absent(errors.join('\n'))]) : read;
    } };
  }
  async function specs(nodes: LiveNode[], base: Coverage, detail = false): Promise<LiveSpec[]> {
    const tree = detail ? SPEC_DETAIL_CHILDREN : SPEC_TREE_CHILDREN;
    const wrappers = await children(nodes, 'userStories', SECTION_CHILDREN);
    const stories = await children(wrappers.nodes, 'stories', tree.stories);
    const ac = await children(stories.nodes, 'acceptanceCriteria', tree.acceptanceCriteria);
    const criteria = await children(ac.nodes, 'criteria', tree.criteria);
    const byIdentity = (items: LiveNode[]) => new Map(items.map(node => [identity(node.ref), node]));
    const wrapperNodes = byIdentity(wrappers.nodes);
    const storyNodes = byIdentity(stories.nodes);
    const acNodes = byIdentity(ac.nodes);
    const criterionNodes = byIdentity(criteria.nodes);
    const owned = (parent: LiveNode, name: string, candidates: Map<string, LiveNode>) => [...new Set(refs(parent, name).map(identity))]
      .flatMap(key => { const node = candidates.get(key); return node ? [node] : []; });
    return nodes.map(node => {
      const ownWrappers = owned(node, 'userStories', wrapperNodes);
      const storyCoverage = combine([base, ...[wrappers.coverage(node), ...ownWrappers.map(wrapper => stories.coverage(wrapper))].filter(nonNull)]);
      return { ...summary(node, base), source: node, kind: 'spec', status: status(node, 'specStatus', specStates),
        storiesCoverage: storyCoverage,
        stories: ownWrappers.flatMap(wrapper => owned(wrapper, 'stories', storyNodes)).map((story): LiveStory => {
          const ownCriteria = owned(story, 'acceptanceCriteria', acNodes);
          const criteriaCoverage = combine([storyCoverage, ...[ac.coverage(story), ...ownCriteria.map(wrapper => criteria.coverage(wrapper))].filter(nonNull)]);
          return { ...summary(story, storyCoverage), source: story, status: status(story, 'status', storyStates), criteriaCoverage,
            criteria: ownCriteria.flatMap(wrapper => owned(wrapper, 'criteria', criterionNodes))
              .map(criterion => ({ ...summary(criterion, criteriaCoverage), source: criterion, story: story.ref })),
          };
        }),
      };
    });
  }
  function effort(node: LiveNode, read: Coverage, scope = deferredScope()): LiveEffort {
    return { ...summary(node, read), source: node, kind: 'effort', form: node.resolvedType === 'EffortWorkspace' ? 'EffortWorkspace' : 'EffortNote',
      status: status(node, 'status', effortStates), checklist: { state: 'deferred' }, scope };
  }
  async function portfolio(review = false): Promise<PortfolioRead> {
    const [{ specRead, specRecords }, effortRead] = await Promise.all([
      inventory('specLike', SPEC_TREE).then(async specRead => ({ specRead, specRecords: await specs(specRead.nodes, specRead.coverage) })),
      inventory('effort')]);
    const portfolio: Portfolio = { specs: specRecords, efforts: effortRead.nodes.map(node => effort(node, effortRead.coverage)),
      specCoverage: specRead.coverage, effortCoverage: effortRead.coverage };
    if (review) {
      const candidates = effortRead.nodes.filter(node => ['active', 'complete'].includes(value(node, 'status') ?? ''));
      const details = new Map((await effortDetails(candidates.map(node => node.ref)))
        .map((result, index) => [identity(candidates[index].ref), result]));
      portfolio.efforts = portfolio.efforts.map(candidate => {
        const result = details.get(identity(candidate.ref));
        return !result ? candidate : result.detail?.record?.kind === 'effort' ? result.detail.record
          : { ...candidate, coverage: result.coverage };
      });
    }
    return { portfolio, coverage: combine([specRead.coverage, effortRead.coverage,
      ...specRecords.flatMap(spec => [spec.storiesCoverage, ...spec.stories.map(story => story.criteriaCoverage)]),
      ...(review ? portfolio.efforts.filter(item => item.status === 'active' || item.status === 'complete').map(item => item.coverage) : [])]), reviewEvidence: review ? 'loaded' : 'deferred' };
  }
  async function material(node: LiveNode, read: Coverage, loaded?: LoadedNodes): Promise<ScopeMaterial> {
    const supported = node.format === 'markdown';
    async function section(name: string): Promise<ScopeSection> {
      if (!supported) return { state: 'unsupported', coverage: read, occurrences: [] };
      const targets = refs(node, name);
      if (field(node, name)?.issues?.length) return { state: targets.length > 1 ? 'duplicate' : 'unreadable', coverage: read, occurrences: [] };
      if (targets.length !== 1) return { state: targets.length ? 'duplicate' : 'missing', coverage: read, occurrences: [] };
      const hydrated = loaded ? selectedNodes(loaded, targets) : await hydrate(targets, REVISION);
      const child = hydrated.nodes[0];
      if (!child) return { state: 'unreadable', coverage: hydrated.coverage, occurrences: [] };
      const links = await occurrences(child);
      return { state: 'present', coverage: composeCoverage([read, { ...hydrated.coverage, sourceRevision: revision(child) }, links.coverage]), occurrences: links.occurrences };
    }
    const [specs, stories] = await Promise.all([section('specSetFrozen'), section('storiesInScopeFrozen')]);
    return { ref: node.ref, supported, coverage: read, specs, stories };
  }
  async function checklist(owner: LiveNode, ownerCoverage: Coverage, loaded: LoadedNodes): Promise<{ checklist: Checklist; nodes: LiveNode[]; coverage: Coverage }> {
    const state = field(owner, 'closureChecklist');
    const targets = refs(owner, 'closureChecklist');
    if (!state || state.issues?.length || targets.length > 1) return {
      checklist: { state: 'unreadable' }, nodes: [], coverage: absent('Closure section unavailable or ambiguous') };
    if (!state.present && !targets.length) return { checklist: { state: 'missing' }, nodes: [], coverage: ownerCoverage };
    if (targets.length !== 1) return { checklist: { state: 'unreadable' }, nodes: [], coverage: absent('Closure section unresolved') };
    const section = selectedNodes(loaded, targets);
    const child = section.nodes[0];
    const read = composeCoverage([ownerCoverage, { ...section.coverage, sourceRevision: child ? revision(child) : null }]);
    const counts = read.complete && child?.content !== undefined ? api.markdownChecklist(child.content) : null;
    return { checklist: counts ? { state: counts.total ? 'recorded' : 'empty', ...counts } : { state: 'unreadable' }, nodes: section.nodes, coverage: read };
  }
  async function record(ref: Ref): Promise<RecordDetail | null> {
    const own = await hydrate([ref], NODE_DETAIL, true);
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
    const none: LoadedNodes & { coverage: Coverage } = { nodes: [], coverage: ownCoverage, coverageFor: () => ownCoverage };
    const [components, entryMentions] = await Promise.all([
      componentRefs.length ? hydrate(componentRefs, NODE_DETAIL) : none, occurrences(node)]);
    const selectorField = field(node, 'frozenScope');
    const selectedLinks = selectorField?.links ?? [];
    const selectedNode = components.nodes.find(item => selectedLinks.some(link => link.ref && identity(link.ref) === identity(item.ref)));
    const logRefs = (field(node, 'workLog')?.links ?? []).flatMap(link => link.resolved && link.ref ? [link.ref] : []);
    const logRead = node.resolvedType === 'EffortWorkspace' && logRefs.length === 1 ? selectedNodes(components, logRefs) : null;
    const log = logRead?.nodes[0];
    const selectedIsLog = !!log && !!selectedNode && identity(log.ref) === identity(selectedNode.ref);
    const logSectionRefs = log?.workspace?.fields.flatMap(item => item.sectionNodes ?? []) ?? [];
    const logSectionsRead: Promise<typeof none> = logSectionRefs.length ? hydrate(logSectionRefs, NODE_DETAIL) : Promise.resolve(none);
    // Only material read from the log's own sections waits for them.
    const [logSections, selectedMaterial, inline, logMaterial] = await Promise.all([
      logSectionsRead,
      selectedNode ? (async () => material(selectedNode, { ...components.coverageFor([selectedNode.ref]), sourceRevision: revision(selectedNode) },
        selectedIsLog ? await logSectionsRead : undefined))() : null,
      node.resolvedType === 'EffortNote' ? material(node, ownCoverage, components) : null,
      log && logRead && !selectedIsLog
        ? logSectionsRead.then(loaded => material(log, { ...logRead.coverage, sourceRevision: revision(log) }, loaded)) : null,
    ]);
    const selector: ScopeSelector = {
      state: selectorState(selectorField, selectedNode, components.coverageFor(selectedLinks.flatMap(link => link.ref ? [link.ref] : []))),
      authored: selectorField?.values ?? [], coverage: ownCoverage, material: selectedMaterial,
    };
    const scope = inline ? frozenScope({ form: 'EffortNote', inline }) : frozenScope({ form: 'EffortWorkspace', selector });
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
    const governingField = field(node, 'governingSpecs');
    const governing = { coverage: governingField ? ownCoverage : absent('Governing metadata unavailable'),
      occurrences: (governingField?.links ?? []).map((link, ordinal): Occurrence => ({ source: node.ref, ordinal, authored: link.value,
        target: link.ref, targetKind: link.resolved ? 'spec' : 'unresolved', link: { target: link.value, authoredTarget: link.value,
          text: link.title, kind: 'field', anchor: null, embed: false, targetKind: 'note', resolved: link.resolved,
          resolvedRef: link.ref, title: link.title, preview: null } })) };
    const workLogSnapshot = log && logRead ? selectedIsLog ? selector.material : logMaterial : null;
    const checklistRead = node.resolvedType === 'EffortNote'
      ? await checklist(node, ownCoverage, components)
      : log && logRead ? await checklist(log, combine([ownCoverage, logRead.coverage], revision(log)), logSections)
        : { checklist: { state: 'unreadable' } satisfies Checklist, nodes: [], coverage: absent('Work log unavailable or ambiguous') };
    result.checklist = checklistRead.checklist;
    const targetRefs = [...distinctTargets([...scope.specs, ...scope.selections]).values()];
    const targets = targetRefs.length ? await hydrate(targetRefs, NODE_DETAIL) : { nodes: [], coverage: ownCoverage };
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
  type Load = (ref: Ref) => Promise<RecordDetail | null>;
  async function effortDetails(targets: readonly Ref[], load: Load = record) {
    const results: { detail: RecordDetail | null; coverage: Coverage }[] = new Array(targets.length);
    let next = 0;
    async function work() {
      while (next < targets.length) {
        const index = next++;
        check();
        try {
          const detail = await load(targets[index]);
          results[index] = { detail, coverage: detail?.coverage ?? absent('Selected effort unavailable') };
        } catch (error) {
          check();
          results[index] = { detail: null, coverage: absent(String(error)) };
        }
      }
    }
    await Promise.all(Array.from({ length: Math.min(4, targets.length) }, work));
    return results;
  }
  /** Coverage is the effort record reads. Each effort's scope state reports its own frozen-scope evidence. */
  async function selectedEfforts(targets: readonly Ref[], load: Load = record) {
    const unique = [...new Map(targets.map(target => [identity(target), target])).values()];
    const details = await effortDetails(unique, load);
    const efforts: LiveEffort[] = [];
    const reads: Coverage[] = [];
    for (const result of details) {
      if (result.detail?.record?.kind === 'effort') {
        efforts.push(result.detail.record);
        reads.push(result.detail.record.coverage);
      } else reads.push(result.detail ? absent('Selected effort unavailable') : result.coverage);
    }
    return { efforts, coverage: combine(reads) };
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
