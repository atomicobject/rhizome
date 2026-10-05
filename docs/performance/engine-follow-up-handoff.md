---
summary: "Resume guide for unstarted engine performance and cleanup work after the 2026-09-05 integration effort."
last-verified: 2026-09-06
---

# Engine follow-up handoff

Drew requested this record so another AI can resume after the current changes merge. PR #206 is merged to main and the effort is closed. This note records future candidates; picking one up requires a new effort.

## Current stabilization status

PR #231 was rebase-merged into main on 2026-09-06 as `3557337f`, after all current-head checks passed and Greptile's findings were resolved. The original follow-up head `fc4aebe4` was rewritten by the rebase merge; compare accepted content rather than requiring the old commit ID to be an ancestor.

The remaining navigation, cancellation, and citation-guidance work is tracked in `2026-09-06-09-05-navigation-stabilization`. Unlinked criteria do not need ambient block IDs: `requires_fix` on a requested locator means an anchor is needed before persisting that particular citation. It is not a default validation defect. The current stabilization work uses content-derived transient identity and on-demand durable anchors; it does not reopen the unmeasured performance candidates below.

The dated sections below retain the earlier investigation and handoff history. Their branch-local delivery and pending-PR statements describe that earlier state.

Staging `codex/note-click-loading` now includes reviewed PRs #237–#240: cancellation reporting, cache-test synchronization, applied-before-cited link guidance, and content-derived node navigation. Its integrated binary builds successfully. Ontology materialization version 5 causes one derived-state rebuild on first use. Final PR verification is tracked in the effort; the user's server on port 61353 has not been restarted by this stabilization task.

## Establish the baseline before resuming

Fetch the repository and inspect whether PR #206 actually merged. Use its accepted commits and current main, not the original research checkout or a leftover local binary. The [docket](engine-docket.md) records accepted child heads, squash commits, decisions and evidence; the effort owns delivery status. Older reports deliberately preserve historical findings, including items subsequently fixed.

B01–B12 and B14–B18 are integrated. B14 query inventory reuse landed through PR #208 as `6c3302e5`; its [final evidence](query-selector-inventory.md) distinguishes the multi-root gain from an unchanged single-root control. B16 graph-score batching landed through PR #209 as `75d6b939`; its [corrected evidence](engine-graph-score-batching.md) includes shorter writer occupancy, preserved atomicity and the allocation tradeoff. B18 rationale-schema cleanup landed through PR #210 as `04be6542`; B17 durable link-edge repair landed through PR #211 as `b1a75a82`, including the publication-ordering barrier that fixed a real full-index foreign-key race and its measured indexing cost ([evidence](ensure-link-edge-convergence.md)). B13 published reads is **not delivered**: it never reached a child PR, and nothing on main implements usable published reads during indexing. Its approved formative direction is D22/D31 in the docket, and its unmerged work sits on `codex/engine-published-reads` with a stash in the coordinator worktree; treat both as research, re-derive from current main, and do not assume the sealed-generation contract exists in shipped code.

Start new substantive work in an isolated checkout. Read `AGENTS.md`, the relevant subsystem note under `docs/reference/subsystems/`, and `docs/engineering/`. Preserve unrelated changes. Use plain `rzm index`; the old `--code` and `--semantic` switches are removed. Read configuration before indexing a disposable fixture: enabled embedding providers can perform external work. Prefer prepared copies and explicit provider settings rather than touching the user's live index.

Drew accepts marginal indexing overhead for faster reads and says embeddings dominate his indexing time. That preference does not justify stale source checks or incomplete publication. Keep immutable published generations coherent, mutations under current writer authority, complete artifact batches atomic, canonical identities and ambiguity behavior intact, and cancellation/errors observable.

## Reproduced correctness follow-up: embedded pane shows no content

A retained B11 browser probe opened `docs/specs/technical/linkable-embedded-node-identifiers.md`, selected User Stories, then `SPEC-0023-US1`. The GraphQL response contained valid EMBEDDED content/workspace/bodies and the sidebar selected the expected story, but the pane showed “Choose a note.” This was not counted as a successful embedded rendering check. The same source guard exists on original `af100a90`; it is a pre-existing defect, not a claimed B11 regression.

The delivered integration still has the source cause: `renderedFromPublicNode` in `web/src/api/nodeWorkspaceAdapter.ts` accepts NOTE and SECTION only. `OntologyNotePane.tsx` then lacks the rendered content its body requires. B13's owner explicitly reports that the publication work does not claim to repair this adapter exclusion.

**Next proof:** reproduce that exact click flow on the final built app and check current type-adapter behavior before editing. A bounded candidate is admitting supported note-backed EMBEDDED content while retaining code-kind separation, backed by a real payload adapter regression, rendered title/body test and actual browser click. Preserve canonical identity and edit/section semantics. Prefer investigating this reproduced user-facing defect before speculative caches.

**Status:** fixed on `codex/engine-embedded-pane` — `renderedFromPublicNode` now admits note-backed EMBEDDED alongside NOTE and SECTION while still excluding code kinds, proven by an adapter regression test, a rendered pane test and a Playwright deep-link check against the fixture vault.

Supplementary retained evidence under `/tmp/rhizome-b11-fixtures/evidence/`: `embedded-pane-followup.md`, `embedded-detail-payload.json`, `embedded-final.png`, `embedded-buttons.txt`, `embedded-probe.cjs`. Early script assumptions about title/acceptance text timed out; those are not successful assertions. The steps and source cause above are durable even if the temporary artifacts disappear.

## First performance candidate: B19 embedded validation projection reuse

**Evidence:** source-confirmed repeated setup plus a fresh exploratory profile at `884d68cd`. `validateEmbeddedProjectionFields` in `pkg/ontology/index.go` calls `ProjectNodeFromSnapshot` for field, collection and global-source children; that function in `pkg/ontology/projection.go` creates a new resolver each time. The traversal's seen check happens after projection, so duplicate visits can still pay setup. B05 addressed catalog resolver reuse, not this validation traversal.

Two existing view tests passed under `-race`. CPU samples attributed 4.77s cumulatively to embedded validation, 4.45s to resolver construction and 3.92s to `indexGlobalSourceTypes`. The path-filter test took 22.30s locally. Race instrumentation and shared-host execution prevent treating these as production latency or accepted before/after measurements.

**Next proof:** measure the real validation/index path without race instrumentation at increasing embedded-node counts, compare complete validation issues and identities, then assess one resolver per immutable document/schema traversal. Preserve recursion, cycles, global sources, partial projection errors and issue ordering. Do not introduce a cross-request cache. Recheck whether final inflight changes already affect this path.

The existing tests reproduce the workload from a clean checkout:

```sh
GOMAXPROCS=4 go test -race -tags fts5 -count=1 ./pkg/app/views \
  -run '^TestOntologySource(FiltersAndSortsIndexedRowsBeforePagination|PushesNotePathFilterBeforeSourceCap)$'
```

Local supplementary artifacts: `/tmp/engine-views-audit.jsonl`, `/tmp/engine-views-audit.cpu`, `/tmp/engine-views-audit.test`. They are not needed to recreate the workload and may not survive to the next session. B19 was queued but never started before Drew froze work.

**Delivered:** see [embedded validation resolver reuse](embedded-validation-resolver-reuse.md) for the change, parity evidence, and before/after measurements.

## Other remaining candidates

### Graph-cache follower cancellation

**Evidence:** source finding; user-visible frequency and latency unmeasured. `graphResponseCache.getOrBuild` in `pkg/app/web/graph_cache.go` waits on an existing call's done channel without the follower's own cancellation. The builder closure uses its initiating request's context, which can also fail healthy followers. See [browser/runtime research](engine-browser-runtime-research.md).

**Delivered.** The shared build now runs on its own goroutine under the cache lifetime context; every waiter (initiator included) returns on its own `ctx.Done()`; results are stored only on success; `Server.Close` cancels the lifetime context and drains builds before store cleanup.

### Duplicate cold workspace projections

**Closed: measured, join rejected.** `nodeProjectionCache.Projection` (`pkg/app/web/node_projection_cache.go`) still unlocks before load, so N concurrent cold misses for one ref do N loads (verified 1/2/4 → 1/2/4 with a gated reader; the 2026-09-24 test audit retired the test that pinned this count, since the load count is not a caller contract). It does not matter: the GraphQL note-detail payload uses request-scoped `noderead.Scope`, not this cache, and a browser note open sends exactly one request through it (the SSE subscribe in `usePaneStack.ts`), so N is 1 in practice; edit-session and watch-bridge callers are sequential. One duplicated miss costs 0.3–1.2 ms load+parse plus 10–90 µs projection (12–48 KB notes, order-of-magnitude on a shared host). A singleflight join would need an inflight map, a detached load context and generation checks to save nothing measurable, so the lock-release-load design stays. The one real freshness edge — an obsolete load overwriting a fresher entry with an older-stamped snapshot, self-healed by an extra reload — is closed by a guard in `storeProjection` that refuses same-schema results older than the cached entry (`TestNodeProjectionCacheObsoleteLoadDoesNotReplaceFresherEntry`: 3 → 2 loads). If a multi-pane "open all" feature ever produces real overlap, reuse the `graphResponseCache` detached-build shape rather than inventing another.

### Full-vault metadata verification and dirty scans

**Evidence:** historical synthetic 1,000-note fixture: `MetadataStateCurrent` read 2,000 sources across two inventories; `DiscoverDirtyPaths` projected 1,000 sources and allocated about 40.6MB. See [write follow-up](engine-write-path-followup.md) and its retained reproduction in [write research](engine-write-path-research.md). Owning entry points are in `pkg/notemeta/indexer_operations.go`.

**Next proof:** profile actual post-integration callers and assess reuse of the snapshot already computed during verification. Preserve provider diagnostics, eligibility and content freshness. New/deleted paths and alias changes may require broad link rederivation because unresolved inbound links are not persisted. Timestamp equality alone cannot justify skipping content checks; B09 explicitly corrected that bug. Resolved as two local simplifications — single inventory in verification, source-only discovery — with before/after evidence in [metadata dirty scans](engine-metadata-dirty-scan.md).

**Delivered (PR #219, #229).** `MetadataStateCurrent` performs one inventory and one read per note; `DiscoverDirtyPaths` compares authored source identity without projecting (37.6→3.9 MB/op at 1k notes, [evidence](engine-metadata-dirty-scan.md)). Separately, note state is now keyed on content identity rather than mtime, so a touch-only re-index writes only mtime/size and retains both generations ([evidence](mtime-only-reindex.md)). Snapshot reuse between the two entry points was not pursued: they never run in the same command path.

### Sustained watcher delivery latency

**Evidence:** historical watchhub experiment: a 100ms debounce received 30 events every 20ms and delivered no callback during 625.9ms of activity, then delivered after quiet. Owner: `pkg/vault/watchhub/hub.go`; [write follow-up](engine-write-path-followup.md).

**Next proof:** establish a desired maximum delivery age, then test sustained multi-path traffic, cancellation, overflow and eventual convergence. A maximum-age policy trades latency against more indexing batches; it is a behavior decision, not an automatic timer tweak. Respect B13 bundle exclusions and publication barriers. B15 eliminates identical-config churn but does not solve sustained real edits.

### Invalidation bursts and graph layout

**Evidence:** bootstrap schedulers can emit several invalidations for one real change; `web/src/query/VaultInvalidationBridge.tsx` invalidates broad query roots. Actual redundant HTTP work after all accepted batches remains unmeasured. `web/src/components/useSigmaGraph.ts` performs synchronous layout; large-graph main-thread stalls remain a hypothesis. See [browser/runtime research](engine-browser-runtime-research.md) and [node-opening evidence](node-open-latency.md).

**Next proof:** measure real QueryObserver/network counts per settled edit and capture browser long tasks on representative local/global graphs. B03a fixed subscription churn, B11 fixed renderer recreation and B15 removed false config-change events; do not claim those gains again. Preserve necessary invalidation and coherent B13 generation switches. Add coalescing or worker-based layout only if measured remaining work justifies it.

### Summary/atlas assembly and remaining broad selectors

**Evidence:** `ontologySummary` and `ontologyAtlas` in `pkg/app/web/ontology.go` request broad metadata/type/issue information; some full instance listings produce only counts or five starting refs. Historical 1,000-note handlers measured roughly 37–43ms before subsequent improvements. These are not current measurements. Broad find/interface selectors also scan and rank candidates; B14 addresses repeated metadata inventory within one execution, not every ranking or hydration cost.

**Next proof:** benchmark actual final handlers and prepared schema queries at 1k/10k notes, varying selectivity and number of roots. Record rows read, sorting/type work, complete outputs and ordering. Preserve counts, issue totals, eligibility, substring ranking, lexical ties and later-candidate errors. Shared store/planner capabilities own any count/top-ref optimization. Do not substitute FTS/semantic matching for the existing find contract. Evidence: [read research](engine-read-path-research.md).

**Partly resolved:** the summary/type handler-side duplicate inventory read is removed and measured in [summary/type inventory reuse](engine-summary-inventory-reuse.md), which also records the rejected schema/recipe reuse numbers and the broad find/interface selector numbers so those do not need re-measuring; the assessment-JSON decode in `Scope.ensureNoteState` is removed and measured in [materialized assessment flags](engine-assessment-flags.md), which persists `has_issues`/`type_ambiguous` on the assessment row; the remaining `ensureNoteState` cost is the `IN`-chunked `OntologyTypesByPaths` lookup, a candidate for a single-scan inventory reader.

### Schema/recipe reuse and derived identity costs

**Evidence:** initial single samples measured schema loading at 2.519ms and executable schema construction at 1.179ms. Recipe views load/validate the registry and compile the selected recipe. Derived-ID work and repeated runtime/schema setup were lower-priority source leads, not established regressions. Entry points: `pkg/ontology/runtime.go`, `pkg/ontology/index.go`, `pkg/app/views/source_query_recipe.go` and `pkg/ontology/queryrecipe/load.go`; relocate symbols if the final tree differs.

**Next proof:** measure actual caller frequency and remaining contribution after integration. A reuse policy needs ownership of schema/file freshness, duplicate recipe-ID validation and supported hot-schema changes. Preserve derived identities; do not optimize by changing their normalization or compatibility rules. Evidence: [read research](engine-read-path-research.md).

**Measured and rejected:** current numbers and the caller inventory are recorded in [summary/type inventory reuse](engine-summary-inventory-reuse.md); 3–6 ms per MCP tool call or recipe view is below the bar against the freshness-ownership policy the reuse would need.

### Assessment error-to-miss conversion

**Evidence:** suspicion only. `ontology.Service.Assessment` in `pkg/ontology/service.go` marks an assessment known after a store error as well as after an absent row; a later call on the same service can return nil without retry. Inspected web, views and MCP entry points generally construct fresh services, and normal query prefetch retries failed batches. No cross-request stale-assessment defect or performance benefit has been established.

**Next proof:** fault-inject a transient/canceled read followed by a healthy read on the same actual service/execution, demonstrate an observable result difference, and separately verify a new request recovers. If reproduced, keep the fix local to error ownership. Do not present this as a proven defect or justification for a general cache redesign.

**Reproduced at unit level and fixed (PR #222).** A cancelled read followed by a healthy read on the same service returned a miss. No production entry point observed it (each builds a fresh service and the query executor already flattens errors to misses); the two-line fix keeps error ownership local and is pinned by test.

### Repeated rationale FTS deletion after B18

**Evidence:** the B18 report explicitly retains a redundant nested deletion. `ReplaceRationaleForPath` in `pkg/anchors/sqlite/rationale_store.go` deletes existing rationale/FTS data, then `replaceRationaleFTSForPathTx` calls `deleteRationaleFTSByPathTx` again. B18 removes repeated schema creation, not this second deletion. No post-B18 cost or user-visible impact has been measured.

**Next proof:** count statements and measure populated/empty replacements after B18, including direct and atomic-batch callers. Remove repeated deletion only if the owning call paths establish equivalent FTS rows, rowid mappings, ordering, rollback and error behavior. Keep this below reproduced correctness issues and measured hotspots; do not infer a gain from the historical nine-DDL-statements count.

**Measured and rejected (PR #220 closed).** The nested deletion runs after the rationale rows are replaced, so it keys on the incoming IDs and removes FTS rows previously mapped to those IDs under another path. The store API accepts caller-supplied IDs, so that guard is load-bearing at the persistence boundary; removing it saved one DELETE per path replacement and would have orphaned FTS rows on cross-path ID reuse.

### Maintenance frequency and CI toolchain setup

**Maintenance evidence:** historical `ANALYZE` cost was about 3ms. Configuration churn is fixed by B15; rationale DDL is already inflight B18. The remaining maintenance candidate belongs to `pkg/app/indexing/maintenance.go`, `unified_post.go` and `pkg/anchors/sqlite/maintenance.go`. Measure real frequency/cost before adding due-gating; preserve migrations, checkpoint needs and error semantics.

**CI evidence:** completed workflow `33993794450`, Linux unit job `101380555371`, downloaded Go 1.24.2 during `go env` before cache restore. Tar then reported existing toolchain paths and restoration failed. `.github/workflows/ci.yml` requests Go 1.23 in six setup steps while `go.mod` declares Go 1.24.0 and toolchain 1.24.2. Check current versions before changing anything. Aligning setup with the repository toolchain is a bounded cleanup candidate, not a demonstrated explanation for the roughly 18-minute Windows job. Require successful hosted setup/cache restoration and unchanged checks before claiming improvement. Local supplementary log: `/tmp/engine-ci-linux-unit-baseline.log`.

**CI delivered (PR #212):** every `setup-go` step pins `go-version: "1.24.2"`; `go-version-file` was tried first and resolved the `go 1.24.0` directive, reproducing the toolchain download. Hosted logs now show 1.24.2 set up and the cache restored. Maintenance due-gating remains unmeasured.

## Verification discipline for resumed work

Use a current baseline and one bounded candidate per change. Match fixture content, provider configuration, embedded web assets and binary provenance. `NO_WEB` skips rebuilding assets; it does not omit existing embedded assets. Separate indexing costs, SQL/assembly costs, full HTTP latency and browser DOM/rendering observations. Do not add component speedups together or treat a fresh database as flushed OS caches.

Accepted timings require a quiet host; builds, indexing and browser runs in other tasks or subagents can invalidate comparisons. This session used a temporary shared/exclusive heavy-work lock, but a future machine must establish its own coordination rather than assume the `/tmp` helper exists. Test complete outputs, failure/cancellation and freshness parity; follow current repository gates and independent review policy. Preserve the user's live vault/index. Open a new effort for new work after this effort closes; do not rewrite frozen historical scope.

## Session 2026-09-05/06 summary

Effort: `2026-09-05-22-08-engine-follow-up`. Integration branch `codex/engine-follow-up` (from main `8d01660f`); PRs #212–#230 except #220 are squash-merged there, each with Greptile review, hosted checks and coordinator review; #230 (materialized assessment flags, Intel v64, materialization v4) trusts the columns only once the persisted materialization is ready and current. Not yet opened: the PR from `codex/engine-follow-up` to main. Remaining candidates with no new evidence: sustained watcher delivery latency (behavior decision), invalidation bursts and graph layout (browser measurement), `IN`-chunked `OntologyTypesByPaths` in `ensureNoteState` (largest remaining inventory cost after #230), and the not-current published rewrite that still rewrites every note row on a single edit ([evidence](mtime-only-reindex.md)). Confirmed pre-existing defect fixed in passing: percent-encoded fragment deep links never resolved (PR #228). A second pass diagnosed Drew's multi-second embedded-node opens: the detail read is 6–10 ms for every node kind; the seconds came from a watcher failure loop (code paths fed to the ontology-node embedding syncer, failing every ~11 s and forcing a stale recrawl that blocked readers for ~3.7 s on this tree) — fixed by PR #232, #233, #235, with the stale `item-N` ref null fixed by PR #236 and browser invalidation scoped by PR #234. Known flaky test: `TestDrainStreamingWritesPropagatesCancellation` in `pkg/app/indexing` (cancel can win before the drain observes the worker error).

## Suggested prompt for the next AI

> Read `docs/performance/engine-follow-up-handoff.md` and the final engine docket. Verify PR #206 and current main, identify which candidates still survive, and propose one bounded next batch using current source and a representative baseline. First verify the reproduced embedded-pane defect; prefer B19 for the next performance batch if its resolver cost remains. Preserve the documented read/write, freshness, identity and publication contracts. Do not restart delivered batches or rely on historical timings as current evidence.
