---
type: EffortNote
id: EFF-2026-11-06-07-26
aliases: [EFF-2026-11-06-07-26]
name: "Ontology and Explorer performance"
created-at: "2026-11-06T12:26:05Z"
status: complete
summary: "Reduce Atlas layout work and Explorer graph loading while preserving layout, counts, graph meaning, and freshness."
governing-specs:
  - "[[docs/specs/product/ontology-browser-workspace]]"
  - "[[docs/specs/technical/frontend-data-lifecycle]]"
  - "[[docs/specs/technical/ontology-indexed-read-model-contract]]"
---

# Ontology and Explorer performance

## Scope

Speed up ontology and Explorer initial loading and repeated navigation. Preserve diagram geometry, count semantics, graph profiles, folder expansion and search, canonical navigation, and freshness. Use existing indexed read owners. No new persistent tables, approximate counts, renderer replacement, or changes to the user's vault content.

## Spec Set (Frozen)

Frozen against `a738a260`:

- [[docs/specs/product/ontology-browser-workspace|SPEC-0014]]: schema families, counts, starting refs and coherent navigation.
- [[docs/specs/technical/frontend-data-lifecycle|SPEC-0074]]: complete query identities, freshness, abortable requests and late-response protection.
- [[docs/specs/technical/ontology-indexed-read-model-contract|SPEC-0040]]: indexed graph ownership, endpoint identity, profile semantics and edge precedence.

## Stories In Scope (Frozen)

No separate story migration. Only the performance-preserving implementation of the selected contracts is in scope; their broader capabilities remain unchanged.

## Spec Coverage Checklist

- [x] Atlas keeps the same node positions and connector routes; repeat navigation reuses completed layout.
- [x] Ontology summaries preserve exact counts, issue-bearing host semantics and bounded starting refs.
- [x] Explorer preserves folder membership, counts, type colors, edge multiplicity, expansion, search and file navigation with a smaller response.
- [x] Graph reads preserve profile filtering, edge precedence, cancellation and current-index admission.
- [x] Focused tests, performance replay, real browser checks, repository gates and independent review cover the final change.

## Plan

### 1. Preserve and accelerate Atlas geometry

Keep both ELK passes and current placement rules. Remove per-candidate temporary rectangle allocations in component packing. Cache only layout geometry with a bounded, in-flight-deduplicating cache keyed by topology, dimensions and layout-relevant fields. Merge current count/issue/description data into cached geometry; changed geometry recomputes. Validate exact geometry against the old algorithm on disconnected synthetic graphs and the local replay, plus cache hit, failure and invalidation cases. A worker is deferred unless the measured cold path still warrants it.

### 2. Remove duplicate graph reads and unnecessary count materialization

Keep graph evidence acquisition under the existing store/noderead owners. Avoid loading all code edges merely to enumerate graph endpoints and then reading them again. Add a summary aggregate operation that obtains exact counts and bounded examples without hydrating every catalog row. Preserve interface unions, issue distinctness, source/provider eligibility, deterministic ordering, cancellation and staged overlays. Keep the full overlay path when aggregate equivalence is unavailable. Compare old and new results on existing fixtures and measure the same workload.

### 3. Give Explorer a compact overview contract

Resolve the public response shape through foundation review before dependent integration. Prefer a server projection of current indexed graph facts that avoids sending unused node metadata and repeated embedded edges; preserve current folder and edge aggregation semantics through differential tests. Keep folder placement and personal expansion state in the browser. Route all HTTP data through shared graph identity/readiness owners and preserve the full graph API for other consumers. Measure response bytes and browser preparation against the same vault before and after. Do not silently truncate contributing nodes or edges to make it faster.

### 4. Verify and deliver

Run focused Go/web tests, `make check`, `make web-e2e`, required documentation validation and final CI. Compare representative screenshots and local performance replays. Run Sol review, available council review, and Greptile with required clean score; fix relevant findings. Record the final evidence and deviations, open a real PR, update its description with council results and proof, and leave it open and unmerged with auto-merge disabled.

## Plan Approval

The user requested "$council craft a plan, then $full-stop", authorizing the bounded performance work, verification and PR preparation. No merge is authorized. The repository has no configured current user; approval is recorded here without inventing a `plan-approved-by` identity. The public Explorer contract remains subject to the planned foundation review.

## Original Intended Delivery

A verified performance improvement for both surfaces, with measured before/after evidence and a PR ready for the user to merge.

## Actual Delivered

Atlas has a bounded geometry cache and worker-backed component packing; packing preserves geometry with fewer temporary allocations. Summary reads stream canonical identities and retain bounded examples. Explorer consumes a compact file-pair overview with preserved nested-type counts. Graph endpoint discovery no longer computes weighted edges.

## Execution Notes

- 2026-11-06: Initial investigation measured an approximately 1.1-second Atlas packing step for 32 nodes, while the API completed in tens of milliseconds. A temporary allocation-reduction replay preserved exact geometry and halved packing time. The Explorer requested the detailed graph to draw a small folder overview. Backend inspection found repeated edge aggregation and full catalog hydration for summaries. Private input and exact vault examples remain outside the repository.
- 2026-11-06: Rebased the clean task branch onto `a738a260`. Council abstract planning completed with Claude Opus 5.5 and GPT-6.1 Sol at high effort. Both prioritize geometry equivalence, cache identity, graph/count correctness and measured proof. No third provider was available.
- 2026-11-06: Source-level foundation reviews used relevant code and synthetic fixtures, excluding private vault content and credentials.

## Measurements and verification

Same-workload replays compared `a738a260` with this implementation. Only aggregate measurements are recorded here; private graph contents and labels remain outside the repository.

| Workload | Before | After | Evidence |
| --- | ---: | ---: | --- |
| Atlas cold layout, 32-node replay | 1,246.5 ms | 670.6 ms | Median of three runs; every coordinate and connector identical. |
| Atlas cached geometry | Full layout | 0.06 ms | Median of three cache hits; current metadata is merged into cloned geometry. |
| Type summaries, 38 groups | 44.3 ms | 6.7 ms | Median of seven runs; complete results identical, including examples. |
| Explorer response | 6,149,946 bytes | 2,659,765 bytes | Same 3,541-node / 22,556-edge graph projected to 3,338 files and 10,223 pairs; 56.8% smaller. |
| Explorer projection | Not applicable | 3.2 ms | Single warm projection measurement, excluding graph construction. |

The cold layout replay measures CPU work outside the browser. In the app, component packing runs in a worker while the two unchanged ELK passes still run on the main thread. Cold layout still takes time; cache reuse removes that work on repeat navigation. The compact Explorer endpoint still builds and caches the complete global graph on the server. It reduces transfer and browser preparation, not the entire cold server graph cost.

SQLite query plans use the existing type index, note-path keys, assessment primary key, and indexed-note covering index. Canonical interface deduplication remains in Go rather than approximating it with `COUNT(DISTINCT node_id)`. The selected rows still need stable sorting; endpoint discovery still scans and deduplicates relevant edge evidence. No new persistent tables or indexes are required for this measured slice.

- `NODE_OPTIONS=--no-experimental-webstorage mise exec -- make check`: passed, including all Go packages and 243 web test files / 1,978 tests.
- `NODE_OPTIONS=--no-experimental-webstorage mise exec -- make build`: passed.
- `NODE_OPTIONS=--no-experimental-webstorage mise exec -- make check-full`: passed, including race-enabled Go tests, benchmark contracts, web tests, and both integration suites.
- `NODE_OPTIONS=--no-experimental-webstorage mise exec -- make web-e2e`: 121 passed.
- `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift`: passed with zero findings.
- Focused worker, cache, Atlas and AppShell tests: 41 passed.
- Built-app browser comparison on the synthetic fixture: all 11 node positions/dimensions and all connector paths match the baseline. The actual 2,254-byte packing worker returned successfully with no errors. Navigating away and back kept the worker count at one and preserved geometry.
- Explorer's built app requests `/api/v1/graphs/folders`; its file counts, legend and folder presentation were visually checked against the baseline. Differential fixture tests cover ownership, self-pairs, missing hosts, nested types, default weights, maximum category weights and first-seen order.
- The first full web run found three AppShell fixture failures because the mock server lacked the new folders route. The fixture was corrected and the complete run passed. The local Node runtime's experimental web-storage globals required the explicit `NODE_OPTIONS` override above; no product code was changed for that environment issue.

## Independent review

Two providers were available. Source-level foundation reviews accepted canonical streaming summaries and the compact file-pair projection before dependent integration.

| Reviewer | Round 1 | Round 2 |
| --- | --- | --- |
| Claude Opus 5.5, high effort | 5/10, changes | 9/10, approve |
| GPT-6.1 Sol, high effort | 9/10, approve | 9/10, approve |

Round 1 found that importing bundled ELK into a module worker crashes worker initialization (S1) and that worker failure needed a synchronous fallback (S2). Both were fixed, verified in the built browser, and accepted in round 2. The folders endpoint's error status was aligned with the global graph endpoint (S3). Unusual Unicode/case sorting differences (S4) do not alter user-visible behavior and were left alone. A hypothetical worker that neither responds nor errors (N1) has no identified trigger; adding a timeout is deferred.

Greptile reviewed implementation commit `bc6f5782237d956887561fe8a10e6a1aea5c6f0c`. Its initial 4/5 finding `39100944-0` requested provider-proof admission for web inventories. The unchanged base test `TestOntologySummary_CorruptAssessmentRowStaysInventoryReadable` explicitly requires inventory counts after an assessment update withdraws that proof. The existing provider-bound `IndexedOntologySummary` has a separate complete-proof contract. All three focused web readiness tests passed, and Greptile accepted that evidence on reassessment: 5/5, zero comments, with no behavior change. The noderead note now states this distinction explicitly.

## Deviations

- Council has two available providers rather than three. Abstract planning is not a source-level correctness review.

## Closure Checklist

- [x] Implementation and measurements complete.
- [x] Final local tests and documentation validation pass. CI on the PR remains the merge-readiness gate.
- [x] Independent reviews cover the final behavior diff and Greptile reports 5/5 with no unresolved findings.
- [x] Full implementation diff reviewed; delivery commit is `bc6f5782237d956887561fe8a10e6a1aea5c6f0c`. PR publication and final CI verification follow this closure commit; no merge is authorized.

## Compounding Follow-ups

None recorded.

- 2026-11-06: Source foundation review accepted the streaming-summary approach and a compact file-pair projection. Scoped folder summaries were rejected for this slice because preserving current expansion/search would duplicate browser presentation rules. A worker was added after the optimized cold Atlas replay still took hundreds of milliseconds; cache reuse remained below one millisecond.

## Status

Complete. Implementation, performance evidence, local full verification and independent reviews are complete. This closure ships with the reviewed implementation. The PR will remain open and unmerged; final CI and PR state are checked before handoff.
