---
summary: "Bounded source audit and reproducible baseline measurements for ontology catalog, node hydration, graph assembly, and typed queries."
reference-kind: guide
---

# Engine read-path research

Research baseline: `af100a9059fe1e34a88a59d70af26cf75e5e1368`, 2026-09-05.
Initial research lane: `codex/engine-read-path-performance`. The initial audit made no production edits; the B01/B04 implementation sections and subsequent batch reports record the approved changes.
Coordinator task: `01a072bf-e520-7603-bb3d-468148d9a63b`.

## Recommendation

Start with graph assembly and bounded node hydration. These affect interactive reads, have measured scaling failures, and can be improved inside the existing request scope without a persistent cache or SQLite migration. Follow with catalog resolver reuse and indexed sort scoping. Coordinate any store edits with the integration coordinator.

| Rank | Opportunity | Measured evidence | Estimated cost |
| --- | --- | --- | --- |
| 1 | Index graph precedence by endpoint pair | 10k typed + 10k doc edges: 1.92 seconds, despite output limits of 10 | Small to medium; preserve endpoint and direction semantics |
| 2 | Keep path-qualified hydration and fallback bounded; fix embedded profile upgrade | 1,000-note missing GraphQL path: 131 ms, 2,000 content reads; upgrade loses content | Medium; provider freshness, overlays, aliases and ambiguity need regressions |
| 3 | Reuse catalog projection resolver and precompute sibling identity work | 100 → 500 descendants: 3.78 → 97.60 ms; 6.16 → 172.37 MB | Medium; identity and output order are strict invariants |
| 4 | Scope SQL field-sort aggregation to candidate types/rows | Fixed ten candidates, 100 → 10k total rows: 0.067 → 2.83 ms | Small to medium; store ownership coordination required |
| 5 | Batch inbound neighborhood source types before filtering | 1,000 candidate edges with output limit one: 1,000 singleton type requests | Small; retain relation/type filtering before final limits |
| 6 | Avoid repeated schema/recipe work | Schema load 2.52 ms; executable schema 1.18 ms; recipe view recompiles whole registry | Medium; runtime freshness/invalidation needs explicit ownership |

Costs in the initial ranking are implementation estimates. The implementation sections below record accepted before/after measurements and supersede the original research-only status.

## Method and limits

Apple M4 Pro, darwin/arm64, Go benchmark reports 14 logical CPUs. Fixtures are synthetic and disposable. Store fixtures use real SQLite; graph assembly and cold snapshot microbenchmarks use prebuilt fake stores to isolate CPU and request size. Filesystem caches are warm; each query, graph, or hydration measurement creates a fresh request scope. Setup, schema compilation, and indexing are outside query timing. Other workers may contend for the host, so absolute numbers are indicative; repeated scaling ratios and exact request/file counts are stronger evidence.

Experiments are opt-in with the `research` build tag. Initial observations below describe the research base. The B04 profile, requested-host, and inbound-query probes now assert the intended behavior; ordinary tests also cover these contracts.

Reproduce from this worktree:

```sh
export GOCACHE="$PWD/.gocache"
export GOMODCACHE="$PWD/.gomodcache"
export GOTMPDIR="$PWD/.gotmp"
make build NO_WEB=1
go test -tags 'fts5 research' ./pkg/ontology -run '^$' -bench '^BenchmarkResearchCatalogScaling$' -benchmem -benchtime=200ms -count=3
go test -tags 'fts5 research' ./pkg/ontology/query -run '^TestResearchQueryScaling$' -bench '^BenchmarkResearchRepositorySchema$' -benchmem -benchtime=200ms -count=1 -v
go test -tags 'fts5 research' ./pkg/ontology/noderead -run '^TestResearch' -v
go test -tags 'fts5 research' ./pkg/ontology/noderead -run '^$' -bench '^BenchmarkResearch(ColdSnapshot|GraphFacts)$' -benchmem -benchtime=3x -count=2
go test -tags 'fts5 research' ./pkg/anchors/sqlite -run '^TestOntologyNodesByTypePlan_(UsesIndexedFieldPredicateAtScale|TypedPredicatesUsePartialIndexes)$' -bench '^BenchmarkResearchTypedSortUnrelatedRows$' -benchmem -benchtime=200ms -count=3 -v
```

Initial catalog run used `GOCACHE=/private/tmp/rhizome-engine-read-go-cache`; initial query/SQL runs preceded addition of the opt-in tag and used `-tags fts5`. Those harnesses are otherwise unchanged.

## Measurements

### Graph assembly

`BenchmarkResearchGraphFacts`, two repeats of three iterations. Fresh scope, requested `NodeLimit=10`, `EdgeLimit=10`, fake store supplies all candidate edges. These are assembly measurements excluding SQL.

| Typed edges | Doc edges | Time/op range | Bytes/op, approximately |
| ---: | ---: | ---: | ---: |
| 1,000 | 0 | 4.80–4.82 ms | 3.17 MB |
| 5,000 | 0 | 101.6–101.8 ms | 14.96 MB |
| 10,000 | 0 | 400.0–400.9 ms | 29.84 MB |
| 1,000 | 1,000 | 19.11–19.24 ms | 4.67 MB |
| 5,000 | 5,000 | 476.0–476.3 ms | 21.83 MB |
| 10,000 | 10,000 | 1.918–1.921 s | 43.59 MB |

Source: `pkg/ontology/noderead/graph.go:261` calls `removeDuplicateGraphDocEdges` for each typed edge; the helper at line 1105 scans the accumulated map. Typed edges are inserted before document edges at line 284, so this pass scans a growing typed-only map in the normal assembly order. Each document edge calls `hasOntologyGraphEdge` at line 318, scanning the map again. `GraphFacts` at line 375 deliberately requests unbounded graph candidates before ranking. A local endpoint-pair lookup can retain that ranking contract while removing quadratic precedence checks.

### GraphQL path-qualified reads

`TestResearchQueryScaling` executes `{ note(path: ...) { path title } }` with indexed Markdown fixtures and the ordinary file reader.

| Vault notes | Request | Time/op | Bytes/op | Content reads | List calls |
| ---: | --- | ---: | ---: | ---: | ---: |
| 10 | Existing exact path | 0.370 ms | 144,187 | 0 | 1 |
| 1,000 | Existing exact path | 13.691 ms | 4,591,370 | 0 | 1 |
| 10 | Missing exact path | 1.663 ms | 745,809 | 20 | 3 |
| 1,000 | Missing exact path | 131.148 ms | 56,399,968 | 2,000 | 3 |

Content/list counts are per first execution, not cumulative benchmark counts. `rootNoteFromArgs` (`query/execute.go:404`) resolves the path before fetching it. Scope path-cache construction (`noderead/resolve.go:669`) lists all notes and loads aliases. Missing metadata enters `noderead/records.go:45` → whole-vault `snapshot`, then `query/loaders.go:178` → another whole-vault `snapshot`. Both call sites describe fallback as bounded, but both implementations materialize all current Markdown notes before selecting the requested path. This is verified behavior, not merely a misleading comment.

### Node hydration and inbound counts

The embedded-node experiment first hydrates summary, then content in the same scope, then content in a fresh scope. Summary contains zero body bytes; same-scope upgrade contains zero; fresh content contains 62 bytes including the fixture body. Upgrade performs zero file reads; fresh content performs two. `scope.go:36` keys embedded record hits by identity without checking the requested profile. Note-root content has separate upgrade logic. This is a reproducible correctness defect.

`snapshotForPathLocked` (`records.go:691`) calls `ensureNoteState` (`scope.go:275`), loading all metadata, types, and assessments to gate one source snapshot. The count experiment observes 10, 1,000, and 10,000 paths passed to type and assessment readers for one requested file. Fake-store timing: 6.7–10.8 µs / 228–271 µs / 2.220–2.228 ms; at 10k rows it allocates 7.45 MB. Real SQLite and assessment JSON decoding are excluded.

Inbound `Neighborhood` invokes `noteTypeForPathLocked` per distinct source (`neighborhood.go:86,371`). The experiment observes 10 and 1,000 singleton type calls for 10 and 1,000 candidate edges, even when `FirstTotal=1`. Batch types for candidate source paths, then apply existing filtering and limits.

### Catalog materialization

`BenchmarkResearchCatalogScaling` builds the catalog for one Markdown snapshot with globally typed checkbox descendants. It checks every descendant is present before timing. Median of three runs:

| Descendants | Time/op | Bytes/op | Allocations/op |
| ---: | ---: | ---: | ---: |
| 10 | 91,066 ns | 168,622 | 1,532 |
| 100 | 3,779,375 ns | 6,159,214 | 35,296 |
| 500 | 97,602,569 ns | 172,374,413 | 589,647 |

Five times as many descendants costs 25.8 times the CPU and 28 times the memory. `node_catalog.go:99,158` and `node_edges.go:47,71` call `ProjectNodeFromSnapshot` for each child. `projection.go:263,389` reconstructs the resolver, inline properties, assessment, and section/global-type indices each time. `ProjectNodesFromSnapshot` already demonstrates resolver reuse.

Residual quadratic work needs attention even after resolver reuse: section fingerprints recursively recalculate ancestor identities (`projection.go:807`), sibling ordinals scan sibling sets (`:851`), source identifier derivation rebuilds the used-ID inventory (`:934`), and source sibling ordinals scan all spans (`:2160`). Benchmark profiles should decide the smallest useful batch. Preserve authored identifier reservation precedence, structural refs, deterministic row ordering, fallback policy, and semantic parents.

### SQL pushdown

Existing `TestOntologyNodesByTypePlan_UsesIndexedFieldPredicateAtScale` and `TypedPredicatesUsePartialIndexes` pass. They cover 1,200-row predicate behavior and EXPLAIN evidence for normalized/typed partial indexes. Their EXPLAIN statements are handwritten approximations of production SQL, so they cannot prove every generated query retains its intended plan.

Field sorting has an independently measured issue. `store.go:7842` aggregates all rows with the requested field name, grouped by node ID, without the outer candidate type restriction. Fixed ten `Wanted` nodes, return five rows, increase unrelated `Other` nodes:

| Total field rows | Median time/op | Bytes/op | Allocations/op |
| ---: | ---: | ---: | ---: |
| 100 | 66,826 ns | 11,477 | 169 |
| 10,000 | 2,829,656 ns | 11,519 | 169 |

This is 42.3 times slower for an unchanged result set. Scope aggregation to candidate type/node rows or use a candidate-correlated aggregate; compare actual SQL plans before choosing. Preserve duplicate-field MIN/MAX semantics, null ordering, and stable tie breaks. No migration or generic sqliteutil change is currently indicated.

### Schema and recipes

Repository schema load: 2.519 ms / 1.81 MB / 21,534 allocations. Executable GraphQL schema build: 1.179 ms / 1.08 MB / 11,873 allocations. Preparing the known-path query: 4.414 µs / 6,473 bytes / 147 allocations. These are single benchmark samples; rank below larger measured hotspots.

Source-only opportunities, not yet benchmarked end to end:

- `runtime.go:85,104` loads/discards schema, then `index.go:231` loads it again. Fresh runtime also ensures metadata before a later ensure path repeats it.
- Recipe views (`pkg/app/views/source_query_recipe.go:18,32,53`) load and validate the entire registry and compile the selected recipe again. `queryrecipe/load.go:34` walks all roots, including Markdown-rich roots. Registry caching requires explicit file/schema freshness; selected-recipe validation must retain global duplicate-ID checks.
- `findTypedNotes` (`query/execute.go:2109`) scans all metadata and sorts all matches. Interface roots at line 1008 similarly start from all notes. These are broader selectors than exact paths; measure real workloads before introducing another index boundary.
- Web `ontologyDefinitions` caches schema/executable schema for the Server lifetime (`pkg/app/web/ontology.go:35`). No invalidation assignment was found in the web package. Whether hot SDL changes are supported needs confirmation before treating this as a defect.
- `ontology.Service.Assessment` caches misses even when the store returned an error (`service.go:65`). A reused service can turn a transient read error into a permanent miss for that service lifetime. Caller lifecycle and a behavioral reproduction remain to be checked.

## Proposed first implementation batch

1. Replace graph precedence full-map scans with request-local endpoint-pair bookkeeping. Preserve undirected document-edge suppression, multiple typed relations, exact embedded endpoints, code opt-ins, stable output, and GraphFacts ranking/limits.
2. Fix embedded summary-to-content/workspace upgrades using profile-aware cache state. Keep summary catalog reads free of source I/O and cache negative results only for the authority/profile they prove.
3. Replace single-path snapshot freshness checks with batched metadata/type/assessment reads for requested hosts; batch inbound source type reads. Keep provider-current gates and overlay merge semantics.
4. Bound both missing-path fallback layers and avoid path-cache enumeration for authoritative exact paths. Preserve alias/path precedence and preferred-identifier cross-kind ambiguity; do not broaden misses.

Each step gets behavior-focused regressions and before/after benchmarks. Catalog and SQL sort are follow-on batches unless the coordinator prefers broader scope. The coordinator subsequently approved B01, B04, B05, and B08; implementation and verification status appears below. PR target is only `codex/engine-performance-integration`; never main or release.

## Audit coverage and test quality

Read owning ontology, noderead, GraphQL/query, and stores notes plus engineering policy. Inspected schema/runtime/catalog/projection, loader and root execution paths, pushdown planning, recipe load/validation, hydration/resolve/neighborhood/graph, and node-plan SQL. No generic sqliteutil or indexing changes are owned by this lane.

Useful existing coverage includes catalog identity/fallback tests, noderead store contracts, note-root hydration upgrades, overlay behavior, query batching and ambiguity tests, and SQL typed partial-index tests. Gaps exposed here are embedded profile transitions, query miss I/O volume, one-host metadata scope, graph candidate scaling under small output limits, and unrelated-type sort scaling. No ontology benchmarks existed before these experiments. Prefer observable counts/output regressions and opt-in benchmarks over fragile timing assertions.

Large implementation files impede focused review: `query/execute.go` 5,750 lines, `query/query_test.go` 6,165, `anchors/sqlite/store.go` 10,146. Extract only touched behavior into focused files when implementing; wholesale reorganization is outside this batch.

Local binary built with `make build NO_WEB=1`. Minimal Rhizome agent start succeeded with session `KQZrt5diHRJU0cLo`. No semantic index freshness is claimed by this source audit. Focused measurement commands passed; full `make check` intentionally not run during research to limit host contention. `./scripts/rzm validate` passed with zero issues/errors. After measurements, the lane fast-forwarded to shared contract commit `76d41fd3` (SPEC-0089, EFF-2026-09-05-14-10); that update changes only shared planning documents.

## B01: graph assembly implementation

Approved by the coordinator on 2026-09-05. Replaced whole-edge-map precedence scans with a request-local set of canonical endpoint pairs. Typed edges are still assembled before document edges; direction-independent document suppression does not collapse directed typed relations or their weights. Candidate loading, sorting, filters, limits, diagnostics, and scope caching are unchanged. No SQLite changes or persistent cache.

Controlled host slot, Apple M4 Pro, same `BenchmarkGraphFactsPrecedence` fixture and command, median of three samples of three iterations:

| Typed edges | Additional doc edges | Before | After | Speedup | Before bytes/op | After bytes/op |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1,000 | 0 | 5.003 ms | 1.357 ms | 3.7× | 3,175,048 | 3,365,605 |
| 5,000 | 0 | 104.116 ms | 6.511 ms | 16.0× | 14,955,880 | 15,734,938 |
| 10,000 | 0 | 407.731 ms | 13.158 ms | 31.0× | 29,843,741 | 31,411,717 |
| 1,000 | 1,000 | 19.920 ms | 2.023 ms | 9.8× | 4,672,344 | 4,862,880 |
| 5,000 | 5,000 | 485.332 ms | 10.437 ms | 46.5× | 21,829,869 | 22,610,784 |
| 10,000 | 10,000 | 1,919.702 ms | 22.102 ms | 86.9× | 43,594,365 | 45,162,304 |

The pair set trades roughly 1.57 MB extra allocations at 10k typed edges for removal of quadratic work. Output requests remain ten nodes/ten edges; the benchmark asserts ten nodes plus nine typed-only edges or ten mixed edges. It does not reduce candidates to improve timing. SQL is excluded from this CPU benchmark.

Durable benchmark: `pkg/ontology/noderead/graph_precedence_test.go`. Run:

```sh
go test -tags fts5 ./pkg/ontology/noderead -run '^$' -bench '^BenchmarkGraphFactsPrecedence$' -benchmem -benchtime=3x -count=3
```

Baseline used Go's `-overlay` to substitute `graph.go` from `2070ffa6`, while retaining the identical benchmark and parity test. The graph file at that revision is unchanged from the original research base. A first baseline overlapped a catalog profile and was discarded; the table uses the subsequent isolated before/after runs.

Parity: the new test checks both directions of all three fallback link kinds, multiple typed relations, duplicate weights, diagnostic order/content, cached output equality, and GraphFacts filtering/limits. It and all existing `TestScopeGraph*` tests pass against both the original graph implementation (overlay) and the updated implementation. Focused noderead/readmodel tests pass. Independent read-only review and coordinator diff review found no actionable issues. `GOMAXPROCS=4 make check CHECK_JOBS=2 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4` exited 0, including vet, race-enabled unit/integration tests, web checks, and Greptile configuration validation. `./scripts/rzm validate` passed with zero issues/errors.

B01 has an ordinary maintained benchmark. Read-path and query research fixtures accompany B04. B05 and B08 now retain their maintained fixtures and reconstruction commands in [catalog resolver reuse](catalog-resolver-reuse.md) and [typed sort scope](typed-sort-scope.md).


## B04: bounded hydration implementation

Approved by the coordinator on 2026-09-05. Host snapshots load requested metadata, types, and assessments into the request scope. Missing metadata is cached as a miss. Property/tag read failures may use snapshots only for requested sources already proven to be current Markdown. Descriptor-only, stale, fatal, and unknown sources retain the parser eligibility gate.

Embedded record caches distinguish summary and content profiles. A content request after a summary projects the host and returns the body; repeat requests reuse the content record. Partial references are associated with canonical records through a bounded set of discriminator indexes. Repeated canonical rows do not create false ambiguity, and all supplied identity discriminators participate in matching. Link-target apply clears affected host state and dependent embedded caches before resolving again.

Inbound neighborhoods batch candidate source-type reads. Path-qualified GraphQL note resolution uses the existing catalog resolution and ambiguity behavior with locator hydration omitted; loaders no longer repeat a vault-wide fallback after the shared scope has answered. The internal `OmitLocators` option omits locator fields and link-fix diagnostics; explicit plan/apply requests override it, including the resolution after applying a fix. Default resolution remains unchanged.

Focused noderead, query, and readmodel tests pass. New regressions verify requested metadata cardinality and cached misses, provider eligibility, bounded property/tag error fallback, embedded profile upgrades, partial/canonical identity matching, batched inbound type lookup, canonical query identity, basename ambiguity, explicit link-target plan/apply, and zero unrelated file/list/whole-metadata reads for found and missing path-qualified queries. Documentation validation passes with zero findings. Controlled measurements are recorded below. `GOMAXPROCS=4 make check CHECK_JOBS=2 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4` exited 0, including race-enabled unit/integration tests and web checks. Documentation validation passed with zero findings. Independent and coordinator review found no remaining actionable issues.


### B04 controlled measurements

Apple M4 Pro, `GOMAXPROCS=4`, exclusive coordinator slot, median of three samples. Original production from `e0821f14` is substituted through Go overlays; the updated fixtures are identical in both runs. Fixtures use warm filesystem caches and fresh request scopes. Query fixtures use real SQLite; snapshot fixtures use prebuilt fake metadata to isolate host-loading work.

| Path-qualified query | Vault notes | Before | After | Before bytes/op | After bytes/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| Existing | 10 | 0.320 ms | 0.110 ms | 144,327 | 44,196 |
| Existing | 1,000 | 11.297 ms | 0.114 ms | 4,556,251 | 44,347 |
| Missing | 10 | 1.521 ms | 0.062 ms | 744,426 | 22,456 |
| Missing | 1,000 | 116.468 ms | 0.062 ms | 55,833,563 | 22,475 |

At 1,000 notes the missing query's source reads fall from 2,000 to zero and list calls from three to zero. Existing-query list calls fall from one to zero. Both workloads now stay roughly flat as unrelated notes grow. Root-level basename resolution is excluded from this claim.

| Single-host snapshot | Before | After | Before bytes/op | After bytes/op |
| ---: | ---: | ---: | ---: | ---: |
| 10 metadata rows | 4.384 µs | 2.210 µs | 10,868 | 3,353 |
| 1,000 metadata rows | 257.278 µs | 2.308 µs | 865,966 | 3,374 |
| 10,000 metadata rows | 2,163.858 µs | 2.205 µs | 7,449,279 | 3,325 |

The requested host is fixed. The updated scope asks for one metadata path and at most one type/assessment path rather than every indexed path. The inbound-neighborhood regression separately verifies one batched type query for 1,000 candidates rather than 1,000 singleton queries.

The research fixtures `pkg/ontology/noderead/read_path_research_test.go` and `pkg/ontology/query/engine_read_research_test.go` were removed once ordinary tests covered their assertions; restore them from commit `8d01660f` before running the fixture commands below (the clean before/after reconstruction further down restores them the same way):

```sh
GOMAXPROCS=4 go test -tags 'fts5 research' ./pkg/ontology/noderead -run '^$' -bench '^BenchmarkResearchColdSnapshot$' -benchmem -benchtime=200ms -count=3
GOMAXPROCS=4 go test -tags 'fts5 research' ./pkg/ontology/query -run '^TestResearchQueryScaling$' -count=3 -v
```

A real-file metadata-miss regression returns no node on both original and updated code; source reads drop from two to zero. Indexed raw-content provider behavior is preserved. These measurements do not establish end-to-end HTTP/browser latency.


## Deferred and cross-lane findings

- **Schema and recipe caching:** deferred. The measured schema costs are smaller than the selected hotspots, and safe reuse needs an owner for file/schema freshness and global duplicate recipe-ID validation. No persistent cache is introduced by these batches.
- **Broad typed roots:** deferred pending representative selector measurements. Whole-vault candidate loading is expected for some broad roots; the path-qualified B04 workload does not establish a replacement boundary for them.
- **`ontology.Service.Assessment` error caching:** source-level suspicion only. Store-error reproduction and caller lifetime remain unverified; it is not an accepted defect or a claimed fix. The inspected web `ontologyContext` creates a service per call, so the source pattern alone does not establish cross-request impact.
- **Browser type/summary handlers:** deferred with a concrete source lead. `ontologySummary` in `pkg/app/web/ontology.go` loads all note metadata, types, and issue counts, then requests `TypeInstances` for each type/interface. `ontologyAtlas` likewise requests full type listings but retains only five starting refs per type. Counts and issue totals require broad evidence; a future count/top-ref API must preserve those semantics and be measured on the combined integration. No server handler speedup is inferred from B04 library microbenchmarks.
- **Root-level basename targets:** retained behavior. `README.md` may be ambiguous with `nested/README.md`; it still uses the basename/alias inventory. B04's bounded identity claim applies to path-qualified inputs containing a directory separator.
- **HTTP/browser validation:** still required on the combined integration for B04's real node request flow. Library query regressions and synthetic timings do not establish end-to-end latency.


## Known ensure-apply edge convergence gap

Greptile review identified a pre-existing write-convergence defect, reproduced against original `e0821f14` and B04 `0a542d87` with the same real indexed fixture. A ProductSpec contains an embedded UserStory with a `related: ProductSpec @link` field. Applying a missing block ID changes its catalog node from `specs/product.md#story-a-23` to `specs/product.md#^userstory-story-a-0cc547d8`; the persisted edge source remains the provisional ID in both the existing scope and a fresh scope. Original and B04 results are identical. This is not repaired by clearing an in-memory cache.

The coordinator explicitly deferred the durable fix from B04. Owner: ontology source publication and incremental convergence (`pkg/ontology/node_link.go`, `pkg/ontology/noderead/resolve.go`, `pkg/ontology/sync.go`). The required design must route source changes through the provider-aware publication boundary and `ontology.SyncPaths`, including affected dependent notes and atomic writer ownership, before claiming current edge identities. Noderead's catalog-only refresh lacks the indexer and full writer capabilities; adding a parallel edge-writing path would violate that ownership.

B04 now also clears its traversal cache when the catalog refresh succeeds, alongside graph/list/host caches. This removes local stale cache authority but does not claim durable edge convergence. Retained diagnostic source: [apply edge probe](fixtures/noderead-apply-edge-probe.go.txt). The clean-checkout recipe below runs the same diagnostic against the exact original and B04 revisions. The probe records a known gap, not the desired future contract.

The separate partial-metadata fallback review finding is fixed: every requested non-overlay host must have an established metadata result before fallback. Established misses stay absent; existing hosts must be eligible current Markdown, and every eligible snapshot must recover. A failed request does not cache partial misses; a later request retries successfully after the transient error clears. Its regression fails at published `0a542d87` with a missing expected error and passes after correction. The correction also removes an always-empty fallback merge and the unused missing-path return allocation.

The corrected B04 full `make check` passed, including the partial-fallback retry and traversal-cache invalidation regressions.


## Reconstruct the read comparisons

Run these commands from a checkout containing the recorded commits, with Go and the normal CGO toolchain installed. They create detached disposable worktrees; they do not modify the current checkout or a live vault/index. Synthetic fixtures create their own temporary data. Build both test binaries before the serialized timing window, then run the before and after binaries sequentially with no overlapping heavy work. Preserve the output directory as evidence. Each binary runs from its package directory, matching `go test` working-directory behavior. Use the same toolchain for both sides; the recorded host was Apple M4 Pro with Go 1.24.2. New results are fresh measurements, not replacements for the historical medians until reviewed.

### B01 graph assembly

```sh
set -eu
read_compare=$(mktemp -d /tmp/rzm-b01-compare.XXXXXX)
git worktree add --detach "$read_compare/before" 2070ffa6bfd6e0ef2dbd008e5235f3d31cfe0ff7
git worktree add --detach "$read_compare/after" e0821f14c699bfdf242acde16cc21b9d07a0c61e
git show e0821f14:pkg/ontology/noderead/graph_precedence_test.go > "$read_compare/before/pkg/ontology/noderead/graph_precedence_test.go"
for side in before after; do
  (cd "$read_compare/$side" && GOMAXPROCS=4 go test -mod=vendor -tags fts5 -c -o "$read_compare/$side.test" ./pkg/ontology/noderead)
done
# Enter the quiet timing window only after both builds complete.
for side in before after; do
  (cd "$read_compare/$side/pkg/ontology/noderead" && GOMAXPROCS=4 "$read_compare/$side.test" -test.run '^$' -test.bench '^BenchmarkGraphFactsPrecedence$' -test.benchmem -test.benchtime=3x -test.count=3) > "$read_compare/$side.txt" 2>&1
done
```

This carries the identical retained fixture into the original production checkout instead of replacing production with an overlay. The pre-B01 graph file at `2070ffa6` matches the historical baseline.

### B04 requested-host snapshot and query

```sh
set -eu
read_compare=$(mktemp -d /tmp/rzm-b04-compare.XXXXXX)
git worktree add --detach "$read_compare/before" e0821f14c699bfdf242acde16cc21b9d07a0c61e
git worktree add --detach "$read_compare/after" 0a542d873b12736ec9fb8406a5a30bd5b0cae254
for fixture in pkg/ontology/noderead/read_path_research_test.go pkg/ontology/query/engine_read_research_test.go; do
  git show "0a542d87:$fixture" > "$read_compare/before/$fixture"
done
for side in before after; do
  (cd "$read_compare/$side" && GOMAXPROCS=4 go test -mod=vendor -tags 'fts5 research' -c -o "$read_compare/$side-snapshot.test" ./pkg/ontology/noderead)
  (cd "$read_compare/$side" && GOMAXPROCS=4 go test -mod=vendor -tags 'fts5 research' -c -o "$read_compare/$side-query.test" ./pkg/ontology/query)
done
# Enter the quiet timing window only after all builds complete.
for side in before after; do
  (cd "$read_compare/$side/pkg/ontology/noderead" && GOMAXPROCS=4 "$read_compare/$side-snapshot.test" -test.run '^$' -test.bench '^BenchmarkResearchColdSnapshot$' -test.benchmem -test.benchtime=200ms -test.count=3) > "$read_compare/$side-snapshot.txt" 2>&1
  (cd "$read_compare/$side/pkg/ontology/query" && GOMAXPROCS=4 "$read_compare/$side-query.test" -test.run '^TestResearchQueryScaling$' -test.count=3 -test.v) > "$read_compare/$side-query.txt" 2>&1
done
```

`TestResearchQueryScaling` uses `testing.Benchmark` internally with the default benchtime and logs allocations plus explicit source/list counts; it is not a timing threshold test. Only the selected benchmark/probe runs. Other research regressions assert fixed behavior and are not expected to pass on original production. The table's after revision is `0a542d87`; the later correction `f8ce6416` passed its full gate and error-retry regressions, but the table is not a new timing claim for that correction or final integration.

For the deferred edge-identity diagnostic, reuse the two B04 worktrees above after timing ends:

```sh
for side in before after; do
  git show f8ce6416743b76548ad0498f54fc2e8f98a41ac3:docs/performance/fixtures/noderead-apply-edge-probe.go.txt > "$read_compare/$side/pkg/ontology/noderead/apply_edge_research_test.go"
  (cd "$read_compare/$side" && GOMAXPROCS=4 go test -mod=vendor -tags 'fts5 research' ./pkg/ontology/noderead -run '^TestResearchApplyEdgeIdentity$' -count=1 -v) > "$read_compare/$side-apply-edge.txt" 2>&1
done
```

Compare the logged catalog IDs and persisted edge endpoints for existing and fresh scopes. A passing diagnostic means the known mismatch was observed; it does not certify convergence. The reconstruction recipes were source-checked during closure alignment; they were not executed during the browser timing slot. The historical overlay runs and full gates described above remain the executed evidence.
