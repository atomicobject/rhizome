---
summary: "B14 evidence for sharing successful metadata inventory reads across broad GraphQL roots within one execution."
---

# Broad query selector inventory

B14 removes repeated metadata inventory reads when one GraphQL execution contains several `find` or interface-type roots. The inventory belongs to that execution: failed reads remain retryable, cancellation propagates, and the next execution reads the provider again. This is a shared metadata inventory, not a transaction snapshot across metadata and ontology type rows.

## Contract and scope

The original `61d9730b` executor called `CurrentNoteMetadataRows` separately for each broad selector. Find roots then scored and sorted candidate paths; interface roots sorted paths. Both loaded candidate types through the shared noderead scope before filtering and limiting results. A three-root query repeated metadata work even though its candidate type rows were already shared.

The change reuses only the successful inventory read at these two selector call sites. Ranking, lexical ties, type/interface eligibility, filtering, first/first-plus-one pagination, candidate type reads, hydration and provider fallback are unchanged. An error for a candidate outside the first returned page still propagates. Roots in one execution now see the same first successful metadata inventory; concurrent publication becomes visible through a new execution. No cross-request cache, new store API, schema migration or provider-selection rule is introduced.

Incremental type batches were considered but deferred: stopping after enough matches could hide an error that the previous all-candidate type read returned. Top-k ranking would retain that full type read while adding selection complexity. Neither is part of this batch.

## Measurement

The retained fixture is `pkg/ontology/query/engine_b14_query_selectors_test.go`, using a real SQLite store, prepared public queries, 1,000 and 10,000 notes, and `first: 10`. Fixture setup, indexing and schema compilation are outside timed execution. Each iteration creates a fresh query execution; file-system caches are warm. Store instrumentation records inventory/type calls, rows and time. No external providers participate.

For the final comparison, build a Go test executable from baseline `61d9730b` and one from B14, with the same strengthened benchmark fixture in both. Run the 10,000-note generic broad single-root control and three-root target five times per binary in baseline-then-changed pairs, with `GOMAXPROCS=4` and a 200 ms benchmark duration. No worker builds or tests overlap this final series. The earlier sequential series and an interrupted quiet series are excluded from the final timing claim.

The same command is used for each prepared binary in every pair:

```sh
GOMAXPROCS=4 "$BENCHMARK_BINARY" -test.run '^$' \
  -test.bench '^BenchmarkB14SelectorRoots$/^notes=10000$/(generic-find-broad-single-root|generic-find-broad-three-roots)$' \
  -test.benchtime=200ms -test.count=1
```

For three broad find roots over 10,000 notes, the median of five samples changed from **54.295 ms to 29.461 ms** (45.7% lower). Allocated bytes fell from **77,653,244 to 42,322,672 bytes per execution** (45.5% lower); allocated objects fell from **812,665 to 372,991** (54.1% lower). Metadata calls/rows fell from **3 / 30,000 to 1 / 10,000**; type loading remained **1 / 10,000 candidates**. Both binaries assert the same explicit ordered paths before timing.

The single-root broad control measured **25.892 ms to 26.116 ms** (+0.9%), with unchanged metadata and type work. No control timing gain is claimed. These short, coordinated local samples establish the repeated-work reduction, not tail latency or cold filesystem performance. The cache retains its one inventory until the query execution finishes; peak resident memory was not measured.

Single-root selective and interface cases also retain one inventory read and unchanged candidate work. Their earlier exploratory timing differences are not attributed to this optimization. This batch improves repeated roots; it does not eliminate the first inventory scan or the all-candidate type read.

## Verification boundary

Behavioral tests cover mixed typed-find/interface roots, ordering and type filtering, pagination, refreshed metadata after deletion in a new execution, same-execution inventory coherence, transient read failure recovery, cancellation after a cached success, and type errors beyond the returned page. The benchmark fixture also checks output and exact work counts before measuring. Required repository gates and accepted commit/workflow evidence belong in the integration docket.
