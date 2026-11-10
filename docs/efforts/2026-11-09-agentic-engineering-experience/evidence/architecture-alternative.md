# Agentic Engineering experience, independent architecture alternative

Design input for the complete starter experience used by designers and leads shaping contracts, and developers and agents delivering bounded work. Recommend one Delivery experience with a shared read model and the existing Shape / Build / Settle visual map. Treat the map as named work views, not a state machine that moves a spec to completion.

This is a source-grounded design sketch, not an implementation or approved plan. Inspected checkout HEAD equals supplied base `019a59ea128b900b57dfbcf565481f10ea4e9553`. The pre-existing modified `pkg/app/web/assets/dist/index.html` was neither used as design authority nor changed. A concurrent author later created `docs/efforts/2026-11-09-agentic-engineering-experience/`; it was not read or changed. No separately authored uncommitted spec or effort was consulted. Repository files were not edited. No agents were spawned. Work ran in-thread on codex/gpt-6.1-sol high.

Compounding lookup ran before unfamiliar inspection. It reported `Atomic Object server unavailable: unlock the system keychain or run rzm auth login`, followed by no matching shards. This is degraded retrieval, not proof that no constraints exist. No further explicit Rhizome operations were attempted. Static source inspection establishes the contracts below; live ontology queries, browser behavior, generated installation, and tests remain unverified.

Throughput checkpoint is n/a for this read-only investigation.

## Caller usage comes first

### A lead shapes a spec

The Delivery group opens its home page. The lead sees the quiet three-part map, compact excerpts of the work views, and a link to every recorded lifecycle state. Ready means that an active contract contains ready stories. It does not mean a person approved the entire spec. The lead opens a spec, sees all its stories in authored order, opens criteria in place, and sees exact selected scope separately from other effort references. Copying a shaping or planning prompt names canonical targets and asks the existing workflow skill to do the work. The site does not change status or approve a plan.

### A developer resumes an effort

The developer finds the effort in Build or in the panel. Its own title, summary, raw status, creation date, and recorded approver appear before broader relationship reads finish. The brief shows source Status, the last authored execution entries, the closure checklist, and frozen selections. An EffortNote uses its inline sections. An EffortWorkspace opens its explicitly linked implementation plan, work log, and materials. A neighboring file does not become a member. Unresolved members stay visible.

The developer can open the exact selected story or criterion, read the current contract, and distinguish it from preserved frozen prose. Copy resume includes the effort target and the workflow skill. Missing approval metadata is reported as unrecorded, never converted to a prohibition or permission.

### A lead settles delivered work

The lead opens To reconcile and sees unsatisfied stories or criteria explicitly selected by complete efforts, with each effort and frozen-section source beside the signal. This means recorded states warrant review. It does not prove delivery satisfied the criterion or the spec is wrong. A governing-spec link alone produces a related completed-effort fact, not a reconciliation finding. The reconciliation prompt asks the workflow to compare actual delivery, deviations, and current contract before making changes.

Example call sites, proposed starter-local API:

```tsx
function DeliveryHome() {
  const delivery = useDelivery();
  return <WorkflowMap lanes={delivery.workViews} coverage={delivery.coverage} />;
}

function SpecPage() {
  const subject = useViewContext();
  const spec = useSpec(subject); // own record first, scope facts independently
  const nav = usePageNavigation();
  return <SpecRecord read={spec} onOpen={ref => nav.openNode(ref)} />;
}

function EffortPage() {
  const subject = useViewContext();
  const effort = useEffort(subject); // both effort forms share one public result
  return <EffortRecord read={effort} />;
}
```

The hook signatures below accept the kit's context rather than trusting a path split or a display ID. Wrong-context calls return a useful invalid-subject error. Presentation props here are schematic; they are not additional kit APIs.

## Recommended grouping and page model

Keep the comps' spacing, density, three colored groups, story cells, two-column brief, rule disclosure, panel, first-day prompt, and paired themes. Do not reopen the visual direction. Correct semantic wording and counts at the shared model.

| Map group | Work view | Exact membership and limits |
| --- | --- | --- |
| Shape | Shaping | Proposed specs, plus active specs containing draft stories. Show draft/ready/satisfied counts together. |
| Shape | Ready | Active specs with at least one ready story. Show the ready stories and each one's known frozen selections. Existing efforts never hide the remaining ready work. This is not whole-spec approval. |
| Build | Planned | Efforts with recorded status planned. Zero is a valid visible result. The status does not establish that approval is missing. |
| Build | Building | Active efforts except those with a fully read, nonempty, all-checked closure checklist. Missing, unreadable, or empty checklists carry an explicit unknown/not-recorded badge here. |
| Build | Closing | Active efforts with a fully read, nonempty checklist whose authored items are all checked. Describe this as checklist checked, effort still active. It is not acceptance, verified gates, or merge readiness. |
| Settle | To reconcile | Specs containing draft/ready stories, or criteria under those stories, exactly targeted in the frozen story-scope section of a complete effort. Label as a review signal. Keep selected criteria distinct from selected whole stories. |
| Settle | In force | Every active spec. State explicitly that active means normative contract. This overlaps Shaping, Ready, and To reconcile. It is not a terminal delivery stage. |

Shape and Settle counts are spec counts and can overlap. Build counts are effort counts and partition planned/active work. Show units next to the numbers and one compact overlap explanation below the map. No sum across mixed units is a portfolio total. Raw lifecycle values remain on every row and record. A spec can have an active effort while still needing shaping or reconciliation of a different slice.

This is the deliberate structural difference from the prototype's exclusive `specStage` classifier. Exclusive stages conceal mixed increments, draft stories, and delivered slices. The alternative accepts overlap to preserve truth. Clicking a tile opens a work view with its rule, coverage notice, all matching rows, and bounded prompt selection.

Home contains the workflow map and short excerpts of Shaping, Ready, Planned, Building, Closing, and To reconcile. In force opens the normative-contract list. Keep policy/reference catalogues and broad issue lists off home. A compact `All recorded states` link beside the map reaches native collections, including proposed/active/superseded/archived specs, draft/ready/satisfied stories, and planned/active/complete/archived efforts. Unknown or invalid source state gets a visible raw-value/error fallback; it is not silently omitted. Panel links and native collections preserve access to all history, feature areas, and stories.

Pages and mount plan:

- Group home `agentic-engineering.home`, mounted on Delivery, default custom page.
- One hidden standalone work-view page `agentic-engineering.work`, with declared `lane` UI param validated against the registry. Deep links and Back restore it. No standalone rail entry is needed.
- Spec records use five concrete node mounts with unique IDs `agentic-engineering.spec.process`, `.product`, `.technical`, `.experience`, and `.operations`. Each uses `spec.tsx`, targeting ProcessSpec, ProductSpec, TechnicalSpec, ExperienceSpec, and OperationsSpec respectively. Node mounts match concrete type names; an interface mount is a collection presentation, not a substitute.
- Effort records use `agentic-engineering.effort.note` and `agentic-engineering.effort.workspace`, targeting EffortNote and EffortWorkspace, both using `effort.tsx`. Normal node navigation uses the registered default rather than reconstructing presentation IDs in every caller.
- Story and criterion links open exact native nodes. The owning note's panel keeps Delivery orientation; there is no new story lifecycle editor.
- Existing native collections `spec-driven.specs`, `spec-driven.efforts`, `spec-driven.user-stories`, and `spec-driven.feature-areas` retain their IDs, definitions, and edit behavior. The experience may include their definitions in its folder without replacing their identity.
- `agentic-engineering.panel` presents Delivery, SpecLike, Effort, UserStory, AcceptanceCriterion, FeatureArea, and EffortMaterial as needed. Avoid claiming NarrativeSection globally, which could take unrelated notes from other experiences.

Panel contains current location, work-view links and shared counts, Find, native collection/history links, and Recent. Use `useViewLocation` and `useViewContext`; keep search and transient selection in React. Use `useViewPreference` only for stable display choices. Recent is per window, vault, and experience as in Knowledge. The panel frame has a separate query client from the page frame, so do not promise cross-frame cache reuse. Both derive from the same pure domain module.

Record pages use RecordPage with own-read status and a separate partial notice. They retain useful own content when whole-scope reads fail. Each default-exported component uses page-scoped navigation and readiness hooks. Hidden pages do no custom polling. Kit-managed refresh handles generation changes. No new timers, event bus, router, raw storage protocol, or domain mutations.

## Read model and minimal API

The public extension is source-link pagination plus exporting the existing generation accessor described below. Everything else is private starter-local TypeScript. Import kit ref types rather than publish wire response types as domain types. Use canonical full refs as identity. Never key stories or criteria by owner path alone, label, US number, or display ID.

```ts
import type { ViewContext, ViewNodeRef } from '@rhizome/kit';

type Ref = ViewNodeRef;
type Key = string; // canonical full-node identity, made in one local helper

type Coverage =
  | { kind: 'complete'; generation: string }
  | { kind: 'partial'; reasons: string[] }
  | { kind: 'pending' }
  | { kind: 'error'; message: string };

type Read<T> = { value?: T; coverage: Coverage };
type Lane = 'shaping' | 'ready' | 'planned' | 'building'
  | 'closing' | 'reconcile' | 'inforce';

type Occurrence = {
  source: Ref; ordinal: number; authoredTarget: string;
  text?: string; embed: boolean; kind: string; targetKind: string;
  target: { kind: 'resolved'; ref: Ref; title?: string }
    | { kind: 'unresolved'; title?: string };
};
type FrozenSelection = {
  owner: Ref;
  section: Ref;
  occurrences: Occurrence[]; // includes repeated and unresolved occurrences
  targets: Map<Key, Ref>; // derived distinct targets for membership/counts only
  coverage: Coverage;
};
type Checklist =
  | { kind: 'read'; source: Ref; items: { text: string; checked: boolean }[] }
  | { kind: 'missing'; owner: Ref }
  | { kind: 'unavailable'; owner: Ref; reason: string };

type EffortRead = {
  own: Read<EffortSummary>;
  form: 'note' | 'workspace';
  governing: Read<Occurrence[]>; // metadata-bound values, never frozen-scope fallback
  frozenSpecs: Read<FrozenSelection>;
  frozenStories: Read<FrozenSelection>;
  sections: Read<SectionRead[]>; // source order, exact refs, authored text
  plan: Read<SourceRead>;
  workLog: Read<SourceRead>;
  materials: Read<SourceRead[]>; // explicitly linked, author order
  checklist: Read<Checklist>;
};

type Delivery = {
  specs: Map<Key, SpecSummary>;
  stories: Map<Key, StorySummary>;
  efforts: Map<Key, EffortSummary>;
  workViews: Record<Lane, { rows: WorkRow[]; coverage: Coverage }>;
  coverage: Coverage;
};

function useDelivery(): Read<Delivery> & {
  workViews: Delivery['workViews']; coverage: Coverage;
};
function useSpec(context: ViewContext): SpecRead;
function useEffort(context: ViewContext): EffortRead;
function workViews(facts: DeliveryFacts): Delivery['workViews'];
function promptFor(action: 'shape' | 'plan' | 'resume' | 'reconcile',
  selection: readonly Ref[]): string;
```

The remaining private shapes make the dominant access patterns explicit. The wire adapter preserves raw unexpected status values so malformed source stays visible. No approval-stage, merged, blocked, or verified booleans are introduced.

```ts
type Summary = { ref: Ref; title: string; id: string; summary: string };
type SpecSummary = Summary & {
  type: 'ProcessSpec' | 'ProductSpec' | 'TechnicalSpec'
    | 'ExperienceSpec' | 'OperationsSpec';
  specStatus: string; lastUpdated?: string;
};
type StorySummary = Summary & {
  owner: Ref; status: string; increment?: number;
};
type Criterion = { ref: Ref; owner: Ref; text: string };
type EffortSummary = Summary & {
  form: 'note' | 'workspace'; status: string;
  createdAt: string; planApprovedBy?: string;
};
type SourceRead = {
  ref: Ref; title: string; content: string;
  representation: string; // preserve native source, do not coerce HTML into Markdown
};
type SectionRead = SourceRead & { field?: string };
type SpecRead = {
  own: Read<SpecSummary>;
  stories: Read<{ story: StorySummary; criteria: Read<Criterion[]> }[]>;
  governingEfforts: Read<Ref[]>;
  frozenSelections: Read<FrozenSelection[]>;
  otherEffortReferences: Read<Ref[]>;
};
type WorkRow = {
  ref: Ref; title: string; rawStatus: string;
  reasons: { text: string; sources: Ref[] }[];
};
type DeliveryFacts = {
  specs: Read<SpecSummary[]>;
  storiesBySpec: Map<Key, Read<StorySummary[]>>;
  efforts: Read<EffortSummary[]>;
  closureByEffort: Map<Key, Read<Checklist>>;
  frozenByEffort: Map<Key, Read<FrozenSelection>>;
  criteriaByStory: Map<Key, Read<Criterion[]>>;
};
async function readFrozenSelection(section: Ref,
  signal?: AbortSignal): Promise<Read<FrozenSelection>>;
```

The signatures are a design sketch, with implementations deliberately absent. The maps directly answer spec-to-stories, effort-to-checklist, and effort-to-exact-scope queries. A reverse target-to-effort index is derived once when assembling DeliveryFacts if needed by the record pages, never stored as a second source of truth. Return hooks through the project's ordinary query/error envelope; the Read union specifies their semantic coverage contract.

The laziness principle changes the design by keeping three consumer hooks and one pure grouping function, rather than seven lane loaders. Model the Domain changes it by making pending/partial/complete and unresolved occurrences explicit instead of guessing with empty arrays.

### Concrete reads and adaptation

1. Orientation selects typed `specLike(first: 5000)`, `userStory(first: 5000)`, and `effort(first: 5000)` summaries. Confirm roots against executable schema during implementation; source recipe naming supports these roots. Select complete canonical ref fields, status, title/name, IDs, summary, dates, and governing relations needed for orientation. No full body/workspace for every portfolio row. Use `typedRootCoverage` per selected root. Additional pages use existing root offset/pagination rather than treating first 5000 as exhaustive. Pending data is not zero.
2. A selected spec reads its own typed node and authored `userStories.stories` with acceptance criteria. Read containment without a positive relation cap, or disclose its coverage. Preserve that order. Direct story-root rows support cross-spec indexing, not reordering inside a spec. Current criteria remain current contract text, even if a frozen selection links them.
3. EffortNote reads its actual `scope`, `specSetFrozen`, `storiesInScopeFrozen`, `plan`, `specCoverageChecklist`, `originalIntendedDelivery`, `actualDelivered`, `executionNotes`, `deviations`, `closureChecklist`, `compoundingFollowUps`, and `statusSection` where present. Validate exact schema field spelling before coding each selection. Each section owns a source ref. Missing and empty are distinct.
4. EffortWorkspace selects actual `implementationPlan`, `workLog`, and `materials`. The UI may label implementationPlan as Plan, but must not query a nonexistent workspace `plan` field. For work-log section navigation use server-projected workspace structure and focus exact section refs. EffortMaterial is not an EffortNote. Do not fake a cast. Duplicated headings are ambiguous; display source choices rather than pick the first. Never infer members from folder or extension.
5. Read focused frozen-section `workspace.sourceLinks`, preserving ordinal, authored target, unresolved state, and canonical result. Hydrate targets with `readNodes(refs, selection)` for story and criterion inline fragments. It batches 200 and reports unresolved/errors/truncated/complete. Maintain occurrence-to-result correspondence separately because hydration deduplicates targets. Criterion links select that criterion, not its whole story. Owner ancestry is navigational provenance only.
6. `UserStory.efforts` is broad inbound subtree association despite its frozen-scope documentation. Label it other effort references if shown. Do not use it for frozen membership, readiness suppression, or reconciliation. Metadata governing links, frozen spec-section links, and frozen story/criterion links stay separate, including disagreements and missing metadata.
7. Load closure checklists for active efforts to settle Building/Closing counts. Load exact frozen selections from complete efforts to settle reconciliation counts. These can progressively enrich orientation. Until all required contributors are accounted for, show a lower-bound count such as `3 found, still reading` or an unavailable count. Never show `0` or `none` as complete during partial reads.

Checklist parsing is a bounded presentation read of one exact section, not a wikilink parser. Reuse a suitable existing Markdown/task-item utility if available; otherwise one pure parser with fixture proof. Count authored task items, including nested tasks, excluding code blocks. Preserve source order. Empty prose, missing section, and parse/read failure cannot satisfy all-checked. Execution entries remain authored order; show the last authored entries without guessing timestamps or interpreting a file mtime as activity.

### Public sourceLinks API decision

Choose minimal pagination plus count. It closes a factual gap used by exact frozen-scope navigation and can be shared by other section consumers. It requires no new lifecycle or ontology field and no second link resolver.

```graphql
# Proposed change to generated NodeWorkspaceProjection SDL.
sourceLinks(first: Int = 100, offset: Int = 0): [NodeWorkspaceSourceLink!]!
sourceLinkCount: Int!
```

Keep NodeWorkspaceSourceLink unchanged. `sourceLinkCount` counts every authored occurrence returned by the focused projection's `SourceLinks()` before slicing, including unresolved, unsupported, embeds, and duplicates. It does not count distinct targets or resolved notes. Scope is exactly the existing focused-node subtree/owner behavior. The current implementation already calls SourceLinks before slicing, so count does not require full-target hydration.

Page behavior:

- Offset is a zero-based occurrence index. Negative offset is a Prepare/argument-validation error. Out-of-range offset returns an empty list and unchanged count.
- Preserve existing first semantics. Omitted first is 100, nonpositive first returns empty, and values above 100 clamp to 100. Offset applies before first, with safe arithmetic and no integer-overflow slicing. Explicit null uses the same default handling as the existing argument helper.
- Preserve duplicates and source order across pages. Resolve/hydrate only the selected page, using the existing shared noderead scope. A count-only selection performs no target resolution or summary hydration.
- Same-file fragments, block IDs, heading fragments, aliases, ambiguous targets, unsupported syntax, embeds, and native provider ownership use the existing resolver unchanged. Count/list must use the same projected facts, including overlays.
- Resolver errors remain GraphQL errors. Do not return zero count on projection/cache failure as though scope were empty.
- Generated SDL, introspection, schema docs, and public query validation expose both additions. Old callers that omit offset still receive the same first page.

Expose the existing `responseGeneration(data): string | undefined` from `web/kit/coverage.ts` through `web/kit/index.tsx`; it is currently implemented but not exported there. This is a single export of an existing capability, not a new generation protocol. The starter's private `readFrozenSelection(sectionRef, signal)` hides paging. Each request selects `sourceLinkCount`, `sourceRevision { notePath contentFingerprint }`, and the page. Use the kit's generation-aware request mechanism. Aggregate only pages with the same source revision AND graph generation. Source content can stay unchanged while a linked target is renamed or becomes ambiguous, so content fingerprint alone is insufficient. Count changes, generation changes, or a short page before count is reached invalidate the aggregate. Expose stale/partial with explicit refresh, rather than combine versions or loop indefinitely. Superseded requests are aborted or ignored through the existing query lifecycle. Do not return complete while generation is unknown.

Use public `graphql` and the newly exported `responseGeneration` on every page response. Keep paging domain-local; no new generic kit pagination helper is required. Do not invent a parallel raw-fetch path that bypasses frame generation. Check hydrated target results with the same accessor; unknown generation prevents a complete aggregate.

| Alternative | Advantage | Material cost | Decision |
| --- | --- | --- | --- |
| Keep API, disclose cap | No server change. Safe when every 100-row result is labeled potentially incomplete. | At exactly 100, completeness is unknowable. A missing story cannot be labeled unselected, and affected global counts remain partial. | Valid fallback if server work is excluded, insufficient for a fully resolved starter experience. |
| Add offset and count | Complete occurrence traversal with existing resolver and small contract change. Exact 100 is distinguishable from overflow. | Requires generation-consistent aggregation, schema docs, and focused regression tests. | Recommended. |
| Add AE-specific frozen-scope root or client parser | Can return a tailored shape. | Duplicates source-resolution rules or couples general query infrastructure to one starter. | Reject. |

No universal full-portfolio snapshot API is proposed. Whole-root coverage and batched hydration remain separately disclosed. No negative claim is stronger than its contributing read coverage.

## Files and ownership seams

All source work belongs under the template tree, not generated installed copies. Suggested experience folder is `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/`.

| Owner | Exclusive files/responsibility | Contract consumed by others |
| --- | --- | --- |
| Query/platform | `pkg/ontology/query/schema_workspace.go`, `workspace.go`, `workspace_source_links.go`, focused source-link tests, argument validation owner, query CONTEXT and graphql-query subsystem note, relevant generated query documentation. `web/kit/index.tsx` export of existing responseGeneration, plus its public export contract test. | SourceLinks offset/count semantics and coherent generation/revision evidence. No AE knowledge. |
| AE domain/data | `delivery/lib/delivery.ts`, `delivery/data.tsx`, domain tests. Split only when approaching local size guidance. | Named hooks and types above, one work-view registry/rules implementation, canonical identity, coverage, prompts, source provenance. No visual components or registration YAML. |
| AE UI | `delivery/home.tsx`, `work.tsx`, `spec.tsx`, `effort.tsx`, `panel.tsx`, `parts.tsx`, `delivery.css`, focused UI tests. | Consumes domain hooks, public kit components, and shared rule results. No duplicate classifiers, body scanners, or raw GraphQL in panel. |
| Packaging/integration | `delivery/experience.yml`, custom definition YAML, moves of the four unchanged native definitions if needed, starter/template registration or include declarations if needed, `pkg/app/cli/init` contract tests, changelog, owning views/CLI docs if invariants move. | Stable IDs and mount/experience behavior, installed output. No authored schema/lifecycle changes. |

UI and domain worktrees can start from this interface agreement, using synthetic fixtures. Packaging owns registration moves, so UI/domain workers never race on YAML. The query extension is a base dependency for complete frozen-scope reads. Integrate domain and UI against its accepted API, then packaging, and run final installation/UI proof on one integrated head. Each owner inspects callers and carries its own focused tests; root inspects the combined result. No actual worktrees or branches were created for this design review.

The experience manifest declares title Delivery, group Delivery, SpecLike and Effort plus required concrete node families, and the panel ID. Keep `tailwind: false` only for kit-precompiled utilities and authored CSS, following Knowledge. No custom host-frame, iframe cache, global navigation module, new server, or schema status extensions.

## Proof matrix for implementation

These are required future proofs, not tests run during this review.

| Concern | Small discriminating proof |
| --- | --- |
| Lifecycle truth | Synthetic spec active with both draft and ready stories appears in Shaping, Ready, and In force. Raw states remain visible. Superseded/archived specs and archived/complete efforts remain reachable. Planned zero shows a real zero without an invented approval queue. |
| Multiple delivery slices | An active effort for one story does not hide another ready story. A complete effort plus an active effort can coexist with a reconciliation signal for the earlier selected slice. Counts state their units and overlap. |
| Frozen provenance | A story linked only in Execution Notes appears as other reference, never frozen scope. A criterion-only selection stays criterion-only. Governing metadata disagreement with frozen sections is displayed. Unresolved and repeated occurrences retain order. |
| Both effort forms | Equivalent EffortNote and EffortWorkspace fixtures produce comparable briefs while preserving source owners. Unlinked neighbor material is absent. Missing/broken explicit plan/log links remain visible. Duplicate matching headings require disambiguation. |
| Closure | Active plus nonempty all-checked checklist goes to Closing. Empty, missing, unreadable, or unchecked goes to Building with truthful annotation. Checked does not fabricate acceptance, approval, or passed gates. |
| Source-link pages | 0, 1, 99, 100, 101, and 201 occurrences; offsets 0/99/100/count/past-count; first 0/negative/101; negative offset; aliases with independent selections; duplicate targets; missing target; unsupported syntax; embed; same-file heading and block; focused section excludes sibling links. Count is occurrence count. |
| Query efficiency | Instrument count-only request for zero target-resolution calls. Page read hydrates at most selected page targets in a shared batch. Count and list use same projection under staged edits. No per-link file read. |
| Schema contract | Prepare validation and introspection expose offset/count. Schema documentation generation includes them. Existing browser sourceLinks caller still sees its original page. Invalid args fail with actionable paths. |
| Generation changes | Deterministic mutation between page requests renames/deletes a target with unchanged source, then edits the source itself. Neither case yields a complete mixed-generation result or cached absence. Earlier async response cannot replace newer data. No sleep-based race tests. |
| Root and batch coverage | More than 5000 root rows reports incomplete until paginated. More than 200 target refs uses readNodes. Inject unresolved refs, field error, truncation, network failure, unknown generation, and refresh. No false zero/unselected/complete result. |
| Page and panel lifetime | Navigate home, work view, two specs, both effort forms, and native collections. Back/Forward, restored tabs, deep link, hidden-page refresh, panel Find/Recent, and separate windows preserve the correct subject and state. Test keyboard access and visible focus. |
| Visual proof | Matched six-comp scenarios in both themes where applicable, inspected screenshots, plus real mounted navigation. Include fresh empty repository, dense mixed-status portfolio, narrow width, long titles, loading, partial reads, and error recovery. Static comps alone do not prove accessibility or working navigation. |
| Installation | Build this checkout, install into disposable fresh and existing AE fixtures, rerun init, then init --check exits 0. Validate views and inline GraphQL documents. Preserve four native IDs, installed choices, no Knowledge panel collision, and ejected-starter behavior. Do not install/test against the live vault. |

Use the repository's gates, not invented substitutes. Focused query and init package tests, content suite for shipped contracts, `make check-fast` during code work, `make check` before commits, `make build` plus disposable init/init --check, and `make web-e2e` for the integrated UI. CI's `make check-full` is the full gate. Gate commands and approval process remain owned by docs/engineering. This leaf does not run them, open a PR, or assert implementation readiness. Eventual Full Stop delivery must leave its PR open and unmerged.

## Material risks and deliberate limits

1. The comps are a useful interaction reference, but their generator parses YAML, Markdown, links, and dates itself. Its counts and extracted plan/checklist heuristics are not authoritative production data. Reuse the layout and interaction intent, never the parser, the `len(plan) > 80` heuristic, file mtime ordering, or hardcoded snapshot totals.
2. Grouping overlaps are a deliberate usability tradeoff. They preserve mixed contract/work state but require visible units, rules, and overlap wording. An exclusive funnel would be simpler to explain and factually less faithful.
3. Reconciliation needs exact positive scope evidence. Legacy plain-text story IDs remain authored text with unresolved membership; the UI cannot repair them or claim there were no selected stories. Work-log schemas may offer only projected sections, so missing/ambiguous structure remains an evidence gap.
4. Frozen links resolve to today's nodes. They are not snapshots of old criteria. Show the frozen section text and provenance beside current target content, without claiming a historical diff or historical acceptance proof.
5. Root paging and occurrence paging span reads. Generation coherence and explicit partial state are necessary. No new snapshot service is justified for this starter. Large repositories may need subsequent measured query tuning; this review contains no performance measurement.
6. First-day copy should explain what a human supplies and copy one shaping prompt. A missing recorded approver cannot mean waiting for approval. A full checklist cannot mean ready to merge. No new policy/reference product features belong in this scope.
7. Custom-view TypeScript is transformed rather than fully checked by the runtime. Domain behavior tests, static query validation, and the real mounted fixture matter. Compilation alone is inadequate proof.
8. The existing generation accessor is not currently exported by the kit entry point. The platform owner adds that export and proves coherent reads across source-link pages and hydrated targets. Until that proof passes, report partial rather than manufacture completeness.

## Evidence consulted

- `AGENTS.md`; docs/engineering README, architecture, documentation, quality-gates, release, review-and-approval, testing-policy and credential policy.
- `docs/reference/subsystems/views.md`, `cli.md`, `graphql-query.md`; concrete query workspace SDL/resolvers and projection source-link scope implementation.
- `.agents/skills/rhizome/SKILL.md`, custom-views SKILL and `references/experiences.md`.
- `pkg/app/cli/init/templates/starters/ao-kb/module/views/knowledge/experience.yml`, `data.tsx`, `panel.tsx`, `lib/panel.ts` and existing read/coverage patterns.
- AE `module/ontology/spec-driven.graphql`, four existing native view YAML definitions, workflow-state reference, and recipe source.
- `web/kit/read-nodes.ts`, coverage export locations, and public source-link callers/tests located by search.
- All six images in supplied `site2/comps` were opened and inspected. Supplied `site2/build.py`, `pages.js`, and `lib.js.part` were read for generator/classifier behavior. No generator was run.
- pstack poteto-mode, architect, runner-prompt, design-red-flags, how, unslop, technical-writing guidance, Laziness Protocol, and Model the Domain were consulted. Leaf/no-edit instructions override delegation and implementation phases. No model fallback or panel was used.

Skipped implementation, live retrieval, tests, and browser acceptance. The main implementation risk is making incomplete scope reads look complete; pagination plus generation-aware coverage is the proposed dependency that addresses it.
