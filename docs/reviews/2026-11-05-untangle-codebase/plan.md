---
summary: "Proposed behavior-preserving refactor batches, scope decisions, and verification for the untangle audit."
reference-kind: analysis
---

# Untangle execution plan

**Approved:** the user approved this plan in the working conversation on November 5, 2026. The approval covers the batches below, both selected cleanup passes, verification and independent review.

This plan covers the [46 candidate groups](README.md) discovered at `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. It is a refactoring effort with a few explicit defect corrections, not a new product feature or a replacement architecture. Create the typed spec and frozen effort through the working Rhizome authoring surface before effort-driven implementation. Until that surface works, this file is the concrete reviewable plan and the reports are its evidence.

## Outcome and boundaries

Related policy should have one owner, ordinary callers should use that owner directly, and unused implementation should disappear. Preserve published response shapes, stored data, provider accounting, readiness/locking/cancellation, accessibility, and generated-source ownership.

Remove narration comments while touching their owners. Remove tests only when their scenario has no remaining production behavior or equivalent meaningful coverage replaces it. Replace the identified incidental sleeps and source-spelling assertions with observable synchronization and command results. Preserve safety explanations, public contract assertions, platform constraints, and real failure/recovery coverage.

Do not introduce generic frameworks, new dependencies, compatibility shims, schemas, data migrations, provider/model changes, or release/deployment work. Do not remove a documented extension merely because this repository has no caller. Preserve unrelated changes throughout the effort.

## Decisions included in this approval

1. **Graph and preview behavior remain distinct where they are distinct today.** Share pure mechanisms below CLI/HTTP, retaining explicit caller policy for module/language/order and missing preview values. Keep web readiness, enrichment and notes-only behavior at their existing boundary. Separate literal expectations must protect each public surface.
2. **Configured views keep the narrow read interface.** Q4 is reduced to using the actual view read store to gate indexed type execution. Preserve optional broader query capabilities and the current constructor signature. A public execution test must demonstrate filtering/sorting past a source cap when a view store exists without a query store.
3. **Markdown parser reuse starts with characterization.** T1 must first test unmatched backticks, false closing fences, indented code, multiline inline code, CRLF and existing templates. Share only genuinely common fence/protection behavior. Any changed accepted input needs a demonstrated defect and an explicit test; do not substitute all `ProtectedSpans` semantics indiscriminately.
4. **Retire the named unused Go APIs.** This includes the legacy embedding Indexer, runtime version registry, unused Store methods, progress hooks and command-era adapters identified in the reports. Repository reference absence does not prove external Go usage. Update current documentation and code-anchor bindings, and preserve the documented `RankRelated` extension and live watcher.
5. **Billing decisions may be shared, transactions may not.** Extract only identical pure receipt replay/settlement rules. Preserve adapter-owned lease checks, atomicity, error order and unknown-usage holds. Both SQLite and disposable DynamoDB Local contract suites must actually execute.

## Batches and ownership

Each batch includes its caller migrations, tests and owning documentation. Parallel workers receive disjoint files; shared files have one owner. Freeze workers before integrated gates. These are dependency groups, not ten separate approval requests.

| Batch | Candidate groups | Concrete change and exit evidence |
| --- | --- | --- |
| 1. Shared semantic facts | C1, C4; Q1, Q2, Q6, Q7; SC-2, SC-6 | Reuse canonical aliases; share the nonwildcard fuzzy loop, section matching, schema-doc projection, preview parser with caller fallback, property lookup, language registry and ranking tier calculation. Keep wildcard and ranking tie-break differences. Run the affected ontology/query/web, obsidian, search and code-intel behavioral suites. Reproduce alias drift before fixing it. |
| 2. Proven surplus | C2, C3, C7; SC-1, SC-3, SC-4, SC-5; S4; W2, W3; RUNTIME-4 | Remove the inventoried unused APIs, state, wrappers and impossible predicate terms; migrate every caller first. Preserve legacy scanner and DocLinker test scenarios through live entrypoints. Update the two embeddings documents and `docs/code-anchors/Go anchor - Code Index (unified DB + intel).md`. Compile affected consumers and validate bindings. |
| 3. Indexing ownership | IDX-1, IDX-2, IDX-3 | Give the code writer kind explicit identity; leave ontology batching with its current orchestration owner; remove unreachable queued write paths and narrow the producer interface. Preserve phase order, metrics, shared gates, successful partial slices and cancellation drain. Run writer, embedindex and indexing suites, with race and integration gates. |
| 4. Configured views | Q3, narrowed Q4 | Delete no-op pushdown adapters, use the existing link resolver directly, separate cohesive source-resolution code into bounded same-package files, and correct the indexed-store gate. Preserve caps, warnings, overlay ordering and link candidates. Run public view execution and filter/sort consistency tests plus CLI/web consumers. |
| 5. Application boundaries | S1, S2, S3 | Extract shared graph presentation below CLI and HTTP; move generic trust checks out of scheduling; construct each code-operation contract at one coherent location. Preserve wire schemas/order/validation and caller-specific graph behavior. Correct the stale continuation description. Verify literal public contracts, linked worktree trust, generated client/describe output and graph fixtures. |
| 6. Web contracts and navigation | W1, W4, W5 | Derive handwritten edit transport shapes from generated types while retaining stronger unions; share note target parsing/serialization; generate synchronous theme bootstrap from one source of theme data. Preserve HTML query/fragment semantics, first paint, desktop precedence and denied-storage fallback. Run typechecks, relevant UI tests, actual app interaction and web E2E. |
| 7. Runtime ownership and file layout | C5; RUNTIME-1, RUNTIME-2, RUNTIME-3 | Move intact cache/bootstrap method groups into cohesive sibling files; use one private Claude session constructor; reuse existing credential alias metadata in desktop mutation. Preserve locks, startup order, status noninteractivity, timeout defaults and key precedence. Verify runtime/cache/harness behavior, race/integration suites and packaged desktop fixture flow. |
| 8. Tooling and composition | T1, T2, T3, T4, T5, T6; Q5 | Apply the characterized Markdown consolidation; resolve the compounding executable once; project finding data from one read; share dev backend readiness/origin reconciliation; derive CI Go versions from modules; test release Make commands by invocation; inject the existing overlay checker at composition. Run init/overlay/compounding, fake backend restart and release tests. Prove overlay checks remain on command, index, live and fallback paths. |
| 9. Team service | SVC-1, SVC-2, SVC-3, SVC-4 | Inject the existing ledger into identity; remove deferred usage wiring and forwarding; share pure receipt decisions; reuse the wire input bound; delete unused helpers. Run stack, identity, traffic, pricing, SQLite and DynamoDB Local contracts, then `make team-api-check`. No real provider or deployed service calls. |
| 10. Selected test/comment cleanup and integration | C6 and the optional sections in every report | Remove only the identified restatements and obsolete test-only requirements; replace the documented cache/task/gate synchronization and brittle release assertions. Keep useful scenarios and do not declare passing tests flaky. Run integrated gates, inspect full diff, then repeat independent structural review and resolve worthwhile findings. |

Batches 1 and 2 establish shared owners and reduce obsolete callers. Batches 3 through 9 can run in parallel where files are disjoint; Q5 coordinates with indexing/runtime, S1 with web, and all changes to shared files have a single owner. Batch 10 follows those changes. If a later finding requires a materially different public behavior, schema, provider policy or authority boundary, record the evidence and seek a scoped decision before that change.

The optional SVC shared-runtime file move is included only if those functions are already touched and the move reduces reader load; otherwise retain it with that disposition. Proposed test rewrites without an observable replacement, including whole-process goroutine and provider coalescing observations, remain until a concrete equivalent test is available.

## Verification and completion

- Establish the starting build/check state and keep failures distinct from regressions. A baseline failure is not permission to remove its coverage.
- Run focused package or script tests after each batch; use literal expected behavior for changed public seams. Reproduce asserted behavior bugs before fixing them.
- Run `make check-fast` in the edit loop and `make check` before commits. Final integration requires `make verify` because the scope includes runtime, indexing and the web experience.
- For generated compounding/init changes, build, initialize a disposable fixture, and require `init --check` to exit 0. Do not rewrite maintained installed copies by hand.
- Run `make desktop-deps`, `make desktop-check` and `make desktop-build`; exercise the unmerged packaged app with a temporary absolute `RHIZOME_DESKTOP_DATA_DIR`. Never install it over Rhizome Dev from this branch.
- Run DynamoDB Local contracts explicitly; the default service gate may skip them. Record any unavailable gate as unavailable, never passed.
- Validate changed documentation and code anchors through Rhizome, including frozen-scope drift once the spec/effort exist. Fix only authorized findings; classify unrelated or historical findings.
- Fresh reviewers inspect integration boundaries and the remaining structural inventory. Record each candidate as fixed or retained with concrete reason. Continue when another worthwhile in-scope issue is found; a source audit cannot prove that no imaginable improvement exists.
- Record final files/commits, actual test results, remaining limits and delivery truth in the effort. No push, PR, merge or deployment is included in this request.

## Approval and current evidence

The user selected both optional cleanup passes and explicitly approved this plan in chat on November 5, 2026. This satisfies the local [review policy](../../engineering/review-and-approval.md). The approval remains valid for routine choices and review fixes within the recorded scope.

The unchanged baseline `make check` failed in `TestImmediateDecodeFailureHasInitializedProcessIdentity` during child close. Batch 10 must diagnose and resolve this gate failure while preserving process reaping and deadline behavior. A passing rerun, a larger timeout, or removal of the test is not a resolution. This is a verification blocker within the selected test-cleanup scope, not a confirmed new production bug. See [baseline evidence](baseline-evidence.md).

The independent [challenge review](inventory-review.md) accepts the inventory with the narrowed Q4, characterized T1, extra code-anchor update, separate graph/preview expectations and explicit DynamoDB execution required above. No application implementation has started. Baseline and authoring-integration results are recorded in the [review overview](README.md), with final verification still pending.
