# AE starter implementation quality and correctness review

Goal restated. Drew wants two fresh Fable 5.2 reads of the complete Agentic Engineering starter before one consolidated cleanup pass. This leaf owns implementation quality and correctness. PR #123 stays open and unmerged.

**Verdict: CHANGES.** Two small required fixes (IQ-1, and the failure-isolation half of IQ-3), one required decision on a shared public surface (IQ-2), and three recommended or optional cleanups. Nothing here needs a redesign. The Go query changes, the paging and coverage model, and the G1 to G9 fixes hold up under source reading.

## Run facts

- Provider and model: Anthropic, `claude-fable-5-2`, through the Claude Code harness. No other model, no subagents, no delegates.
- Head reviewed: `aa830a2b44544a3374ea33909973280df31da8ec`, clean tree. Base `019a59ea128b900b57dfbcf565481f10ea4e9553`.
- Method: read-only source review of `git diff base...head` plus reachable callers. No tests, builds, gates, browser, pushes, or repository edits. The only file written is this report.
- Evidence labels: "verified" means read in source at the cited lines. "Measured" means a number read from an existing artifact or counted from files. "Inferred" means derived from source without running it.

## Findings, in priority order

### IQ-1. Record review computes the exact pending records, then no consumer shows them
- Class: spec/correctness (SPEC-0137 US6). Severity: medium. **Required.**
- Where: `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/lib/groups.ts:34-56` builds `selected` for each `selected-contract` row. `presentation.tsx:216-222` renders only the generic fact "Selected contract still records pending stories or criteria". `lib/prompts.ts:38-48` adds only the effort locator and plan/log sources. `stage.tsx:116-139` renders the row without `row.selected`.
- Verified scenario: a complete effort whose frozen selection includes ready story A and satisfied story B appears in Record review. The row and the copied prompt name the effort only. The user cannot tell that A is the pending record without opening the effort and comparing by hand. `grep` finds `Row.selected` read only in `testdata/agentic-engineering/delivery.test.mjs:83`. The installed E2E asserts only the negative case (`web/tests/e2e-agentic-engineering/delivery.spec.ts:379-382`).
- Why it matters: US6 asks for observations "with rules, exact sources, scope" and prompts that compare "the exact selected contract". The model has that evidence and drops it at the view boundary.
- Minimal fix: in the review row, list `row.selected` with `RecordLink` and its locator. In `promptFor`, for `row.kind === 'review'`, `add(item.locator, 'selected pending story or criterion')` for each `row.selected` item.
- Validation: extend the pure test at `delivery.test.mjs:75` to assert the prompt text contains `spec.md#ac-1`. Add one positive installed assertion on `stage("review")` that the pending story title is visible in the observation row.

### IQ-2. The kit entry now loads the Markdown and unified stack in every custom view
- Class: standards/architecture (shared public contract). Severity: medium. **Required decision before merge.**
- Where: `web/kit/index.tsx:25` (`Markdown`), `:31` (`markdownChecklist`), `web/kit/markdown-checklist.ts:1-6` (static `unified`, `remark-parse`, `remark-gfm`), `web/src/components/markdown/Markdown.tsx:2-3` (static `react-markdown`, `remark-gfm`). `pkg/viewscript/importmap.json` maps `@rhizome/kit` to the single `kit.js` entry.
- Verified: at base, nothing under `web/kit` imported the Markdown renderer or remark (`git grep` at base returns nothing). These are static re-exports from the one kit entry, so every frame that imports `@rhizome/kit` evaluates them, including Knowledge pages, panels, bundled views and team views that render no Markdown.
- Measured, from existing build artifacts I did not produce: this worktree's `pkg/app/web/assets/dist/kit/v1/kit.js` is 313,018 bytes (82,930 gzip) and contains the micromark/mdast grammar inline. The primary checkout's `main` build from 2026-11-06 is 83,533 bytes (24,941 gzip) with none. Limit: that older build predates about 1.9k lines of other kit changes, so the exact attributable delta needs one kit build at base. The inlining itself is certain.
- Why it matters: `docs/engineering/architecture.md` asks that a new public contract be surfaced as a decision with its tension. The exports are documented in `kit-api.md`, but the cost to every view is recorded nowhere in the effort or evidence. Moving a public kit export after merge is a breaking change, so this is cheapest to settle now.
- Minimal fix (recommended): give the Markdown surface its own import-map entry, for example `@rhizome/kit/markdown` built from `web/kit/entries/`, exporting `Markdown`, `convertWikilinks` and `markdownChecklist`. Leave `responseGeneration`, `resolveRenderedLinkTarget` and `getNodePreview` in the main entry, since they add no new dependency. Update `evidence.tsx`, `data.tsx`, `kit-api.md`, and the two identity tests at `web/kit/index.test.tsx:516-530`, which only mirror the implementation. Alternative: keep it as is and record the accepted cost in the effort's Deviations and `docs/reference/subsystems/web.md`.
- Validation: build the kit at base and at the fix, compare `kit.js` bytes, and rerun the three browser suites. Limit: I did not trace how `pkg/viewscript` and strict template TypeScript consume a new import-map key.

### IQ-3. Per-effort detail reads are serial, repeat work, and one failure discards every other effort
- Class: correctness (failure isolation) and efficiency. Severity: medium. **Failure isolation is required. The rest is recommended.**
- Where: `reads.ts:314-331` (`selectedEfforts`), `reads.ts:186-195` (`portfolio(review)`), `reads.ts:230-313` (`record`), `relationships.tsx:12-15`, `data.tsx:42-51`, `stage.tsx:54-57`.
- Verified, failure isolation: `record()` throws at `reads.ts:234` when the subject read fails without resolving. `portfolio(review)` catches that per effort (`reads.ts:191-194`). `selectedEfforts` does not. One transient failure on one effort rejects the whole query, so "Load effort relationships" shows "Effort relationships could not be read" and drops every successful read, and a multi-select prompt shows "Selected effort read failed".
- Verified, request count: `record()` awaits each step in sequence. For an `EffortNote` that is at least 9 requests (subject, components, two frozen sections each with one hydrate and one source page, entry mentions, checklist, targets). Three of them re-read nodes the components batch at `reads.ts:251` already returned with the same selection (`reads.ts:209`, twice, and `reads.ts:224`). For a workspace it is at least 11, with the work log re-read at `reads.ts:285`.
- Measured scale: `docs/efforts/*.md` in this repository holds 69 effort notes with a recorded status (16 active, 53 complete), before counting workspaces. Record review therefore issues at least 69 x 9 = 621 serial requests before the list renders. "Load effort relationships" on any spec page reads every effort, so it issues at least the same number. Latency was not measured.
- Verified, repeat work: the selection query key is the whole selection (`data.tsx:45`), so each added checkbox rereads every already selected effort. Kit freshness invalidates view queries on vault events (`web/kit/freshness.ts:96`), and Copy is disabled while the portfolio is fetching (`stage.tsx:278`). Inferred, not observed: with an agent writing files, Record review can stay in "Refresh must finish successfully before copying" for long stretches on a repository this size.
- Why it matters: the effort's own throughput checkpoint says "avoid per-row detail calls". The fixture has about ten efforts, so the installed suite cannot see this.
- Minimal fix: wrap `record(target)` in `selectedEfforts` with the same catch used in `portfolio(review)` and push `absent(String(error))`. Run efforts through one small bounded-concurrency helper shared by both loops. Inside `record()`, take the frozen sections, closure checklist and work log from the `components` batch instead of re-hydrating, and start the independent reads together. Generation and revision guards compare values, so they are unaffected by ordering.
- Validation: the adapter harness already records `calls` (`delivery-adapter.test.mjs:14-52`). Add a case where one of three selected efforts throws and assert two efforts return with incomplete coverage. Assert an upper bound on calls for one `EffortNote` record.

### IQ-4. Story and criterion ownership is a quadratic join with JSON serialization in the inner loop
- Class: efficiency/maintainability. Severity: medium-low. Recommended, cheap.
- Where: `reads.ts:157` (`owned`), used at `reads.ts:159`, `:163`, `:164`, `:167`. `identity()` at `lib/model.ts:15-19` serializes on every call.
- Verified: for every parent, `owned` filters all candidates of that level, recomputes `refs(parent, name)` per candidate, and calls `identity()` twice per comparison.
- Measured scale: `docs/specs` in this repository has 430 acceptance-criteria sections and 1,975 criteria. The criterion level alone is about 430 x 1,975 x 4.6 x 2, roughly 7.8 million `JSON.stringify` calls per portfolio read. Inferred: on the order of seconds of main-thread time in each Delivery frame, since the page and the panel each read the portfolio and reread it after vault events. Not measured.
- Minimal fix:
  ```ts
  const index = (nodes: LiveNode[]) => new Map(nodes.map(node => [identity(node.ref), node]));
  const owned = (parent: LiveNode, name: string, byId: Map<string, LiveNode>) =>
    refs(parent, name).flatMap(ref => byId.get(identity(ref)) ?? []);
  ```
  Build each index once per level. Order becomes the parent's authored ref order, which equals today's order because `readNodes` returns nodes in first-named order.
- Validation: existing adapter tests for authored order and the 201-ref case must stay green. Add one case with two parents whose children interleave in the batch.

### IQ-5. Tested model functions are not the rules the views run
- Class: maintainability, and the SPEC-0137 requirement to "keep domain rules in one starter-owned model". Severity: low-medium. Optional cleanup, recommended in the same batch.
- Verified by grep across the shipped views:
  - `attributionFor`, `EffortEvidence`, `sourceRoles`, `SourceRole`, `distinctTargets` (`lib/scope.ts:46-102`) and `linkable` (`lib/groups.ts:135-137`) have no caller outside `testdata`. `selectionFor` is reached only through `attributionFor` and tests.
  - `relationships.tsx:16-40` reimplements governing, frozen and selected attribution inline, with different matching (member-set containment rather than exact target). `effort.tsx:153-156` recomputes the shared-source rule that `attributionFor` already returns.
  - `stage.tsx:30-40` overrides `eligibility()` for effort rows inside the view.
  - `mode: 'committed'` (`data.tsx:24`, `:38`) and `reviewEvidence` (`queries.ts:27`, `reads.ts:200`) are never read.
- Why it matters: `delivery.test.mjs:212-227` proves attribution behavior that no user reaches, while the inline version that users do reach has only browser coverage. The next change to attribution has two places to drift.
- Minimal fix: delete the unused exports and their assertions, or route `LoadedRelationships` and the effort page through one lib function and keep the existing test pointed at it. Move the deferred-scope effort eligibility into `lib/groups.ts` beside `eligibility`. Prefer deletion where no consumer is planned.
- Validation: strict template TypeScript, the pure suite, and the installed relationship and same-source cases.

### IQ-6. A failed component read is reported as a broken authored selector
- Class: correctness of state labelling (US7). Severity: low. Optional.
- Where: `reads.ts:251-262`. `hydrate` returns `nodes: []` on a thrown batch (`reads.ts:52-56`). The selector expression then reaches `!selectedNode ? 'broken'` (`reads.ts:259`).
- Verified scenario: the entry has a valid `frozen-scope` link and the components batch fails in transport. The brief shows "Frozen scope selector broken", which names an authoring defect, although `ScopeSelector` has an `unreadable` state for this. The page is still marked partial with the transport error, so nothing is silently wrong.
- Minimal fix: when the selector link resolved to a ref and `components.coverage` is incomplete, use `'unreadable'`.
- Validation: one adapter case with a rejecting `readNodes` on the components call asserting `scope.reasons` contains "unreadable" and not "broken".

## Required versus optional

| ID | Required | Notes |
| --- | --- | --- |
| IQ-1 | Yes | Small view and prompt change with two assertions. |
| IQ-2 | Decision required | Split the entry, or record explicit acceptance. |
| IQ-3 | Partly | Failure isolation yes. Concurrency and batch reuse recommended. |
| IQ-4 | No | Recommended. About ten lines. |
| IQ-5 | No | Cleanup. Deletion preferred. |
| IQ-6 | No | Optional. |

## Checked and found sound

- Go, `pkg/ontology/query`: `sourceLinks(first, offset)` slicing before resolution, the `first` clamp, empty page at or past the count, negative offset rejected in `prepare.go:210-212` and again at execution, and `sourceLinkCount` over the same focused projection. Batch error ownership in `execute.go:414-421` tags only errors appended during that item's node selection and copies the extension map. `values.go:224` keeps serialized refs off the explicit-path fast path. Callers in `workspace.go:118-131` pass both arguments.
- Go, `pkg/app/web/node_workspace_projection.go:91-93`: non-Markdown providers bypass the Markdown projection cache. A zero-value format runtime falls back to the cache path. All seven callers invoke it on a live `Server`.
- Paging and coverage: `occurrences()` (`reads.ts:83-113`) terminates on a short or empty page, and `composePages` (`lib/coverage.ts:31-49`) rejects offset gaps, count changes, and revision or generation changes. Inventory paging (`reads.ts:114-138`) requires typed-root facts on every page.
- G8 ownership (`reads.ts:58-81`), G4 selection refresh and acknowledgement (`stage.tsx:57-99`, `:226-235`), G9 note-root identity (`lib/model.ts:15-19`), G5 and G7 link serialization (`evidence.tsx:27-33`). The code matches the recorded fixes.
- Source-aware Markdown: the pass-through `urlTransform` matches the native reader's existing pattern, internal links resolve through the server preview with the source path, and `skipHtml` is set.
- Install, update and eject tests exercise real `init` runs, ownership records, check mode byte equality, and the served registry. The shared fixture edits (agentcode, SSE, updater, init JSON, AO KB) keep every prior assertion.

## Coverage and limits

- Read in full: the six engineering policy documents present (`testing.md` does not exist; the file is `testing-policy.md`), SPEC-0137, the effort note, every file under `module/views/delivery/`, the Go production diffs and their callers, the kit and web shared diffs, the ontology, template, skill and recipe diffs, the Go install tests, the E2E server script, config and helpers, and prior review evidence for G8, G9 and the integrated review.
- Read as diff hunks only: `docs/reference/subsystems/graphql-query.md`, `web.md`, `pkg/ontology/query/CONTEXT.md`, the two technical specs. The full subsystem notes and the Rhizome skill were not loaded. The compounding lookup was not run.
- Skimmed: the pure tests, the two large E2E specs, `delivery.css`, fixtures under `testdata/agentic-engineering`.
- Not covered: visual quality, screenshots, accessibility and layout, which belong to the other review. Runtime behavior of any kind.
- Not verifiable by reading: whether refs for the same embedded node are field-for-field identical across `sectionNodes`, `collections`, hydrated `node.ref` and `sourceLinks.resolvedRef` for unanchored targets. `identity()` depends on that outside note roots. The installed suite passes for block-anchored targets, and I found no counterexample in source, so this is a limit and not a finding.
- All latency statements are inferred from request and call counts. The bundle comparison uses builds from different commits.

Dispatch ledger. Review: in-thread on `claude-fable-5-2`, as the brief required a single leaf with no subagents.
