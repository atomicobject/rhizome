---
summary: "Transactional graph freshness revision, measured read gains, and indexing costs for engine batch B03b."
last-verified: 2026-09-05
---

# Graph revision performance and correctness

B03b replaces the aggregate graph fingerprint with a persisted incarnation and revision. Intel schema v62 adds one singleton and transactional row triggers covering the tables consumed by the global web graph, including ontology type attachments. A same-count edge permutation through another connection now changes the cache key. SQL transaction rollbacks restore the prior revision; resetting/recreating the domain changes its incarnation. External evidence remains outside the global graph.

## Measurements

Controlled local comparison on macOS arm64 / Apple M4 Pro, Go 1.24.2, vendored dependencies, `fts5`, `GOMAXPROCS=2`. Base production code is integration commit `76d41fd3` (v61); comparison code is this v62 change. Other worker timing and full gates were paused. These are local measurements, not production latency claims.

Maintained `BenchmarkGraphWebFingerprint`: 50 calls per sample, three samples per size, fixtures seeded before `ResetTimer`; `StopTimer` excludes database close/checkpoint. At 100,000 persisted edges the old read took 25.98–26.29 ms/call, versus 4.42–4.95 microseconds/call with the singleton. Across zero through 100,000 edges the new read took 3.13–4.95 microseconds. This measures only the cache-key read, not graph construction, HTTP serialization, or browser rendering. Initial short measurements included Close and were discarded.

The maintained `BenchmarkGraphRevisionTriggerWriteOverhead` inserts 1,000 distinct edges per transaction with a prepared single-row statement, 50 transactions/sample, three samples. Median transaction time is 3.36 ms without the three edge-table triggers and 32.14 ms with them (about 9.6x). Both cases use the same v62 database/schema otherwise. This is a real adverse result for this SQL execution pattern.

Disposable attribution probes, one sample of 50 transactions each, found an empty `SELECT 1` trigger still costs 28.3 ms/transaction; replacing the singleton with an unconstrained table costs 31.2 ms. A set-based recursive INSERT of 1,000 rows takes 3.84 ms without triggers and 4.10 ms with the production trigger (+6.8%). The vendored SQLite source shows trigger frames are allocated/released per statement execution and reused within one execution; that is a plausible explanation for the execution-shape difference, not a native CPU profile. No driver, SQL batching, or trigger suppression optimization is included.

Actual `index --code` comparisons use disposable copies of `testdata/integration/python-app/vault` and alternate binary order. Fresh indexing starts without a database; incremental indexing rewrites 50 generated notes after the cold run. The scaled fixture adds 500 notes with 20 links each: 523 admitted notes and 20,060 persisted graph-edge rows (wikilinks also have typed note-link rows). Incremental changes redirect 1,000 authored links. Note and code embeddings are enabled with the fixture's local `test` provider (256 dimensions). These runs include local test-provider work but no real embedding-service latency; embeddings are not disabled. This differs from the embeddings-disabled combined write fixture and its `GOMAXPROCS=4` setting.

| Workload | Base median | Revision median | Median delta | Base range | Revision range |
| --- | --- | --- | --- | --- | --- |
| Small existing fixture, 5 pairs | 300.94 ms | 305.65 ms | +4.71 ms / 1.6% | 299.61–918.94 ms | 299.92–749.98 ms |
| Scaled cold index, 5 pairs | 676.99 ms | 707.84 ms | +30.85 ms / 4.6% | 669.47–730.34 ms | 698.15–780.84 ms |
| Scaled incremental index, 5 pairs | 381.95 ms | 405.66 ms | +23.71 ms / 6.2% | 373.42–519.48 ms | 400.56–578.53 ms |

The small-fixture first binary invocations are high outliers. An earlier three-pair scaled run measured cold 678.49→714.36 ms (+5.3%) and incremental 380.67→433.18 ms (+13.8%; revision range 414.78–571.20 ms). Reported medians do not establish tail-latency improvements; outliers occur on both binaries.

Repeated store Open with 1,000 edges, 30 samples, excludes Close: base median 1.623 ms / p95 3.014 ms; revision median 1.619 ms / p95 2.167 ms. Single fresh-open samples were 31.65 and 31.54 ms. No median open regression was observed; a single fresh-open sample is not a distribution.

The tradeoff is exact cross-handle freshness and a constant-size cache-key read at the cost of trigger work during writes. Whole-index measurements contextualize, but do not erase, the prepared-row microbenchmark regression. More extensive bulk-writer optimization requires its own scope and evidence.

## Reproduction and validation

```sh
go test -mod=vendor -tags fts5 ./pkg/anchors/sqlite -run '^$' -bench '^BenchmarkGraph(WebFingerprint|RevisionTriggerWriteOverhead|RevisionStoreOpen)$' -benchtime=50x -count=3
```

The durable scaled indexing harness is `scripts/perf/graph_revision_index_benchmark.py`. After B12 it runs plain `index` on both binaries and reproduces the final workload below. Historical code-only measurements require the pre-B12 runner (retained hash below):

```sh
python3 scripts/perf/graph_revision_index_benchmark.py /absolute/path/to/v61-rzm /absolute/path/to/v62-rzm --pairs 5
```

Build the two binaries from their respective worktrees with the same Go flags. Run the harness serially on a quiet host; it prints all samples and retains disposable databases/logs. The maintained benchmark file is `pkg/anchors/sqlite/graph_revision_benchmark_test.go`, including warm store-open measurement. To reproduce the old fingerprint read, run the same fingerprint benchmark against v61.

Behavior coverage includes same-count rewires through another handle, unchanged reads, transaction rollback, deletes, eligible/ineligible edge-kind transitions, external-target isolation, singleton/schema repair checks, reopen/reset incarnation, and an actual cached HTTP graph response after a cross-handle rewire followed by an isolated code-source link insertion. The latter checks admission of a code source even when its link destination is not a note.

The v62 migration is forward-only. The original `af100a90` binary supports Intel v61 and rejects normal opening of a v62 database; its future-schema test verifies rejection without schema mutation. The transaction-rollback test above does not test downgrading a database or returning to an older binary. For the original binary, unscoped `index --rebuild` explicitly discards the index and its sidecars before opening the database, then regenerates from sources; it does not migrate v62 back to v61. Returning to an older binary therefore requires a compatible restored index or explicit regeneration with no other process using that disposable/recovery index. The final disposable CLI check confirmed both paths: normal old-binary `index` exited 1 with the future-schema error and an unchanged logical database; on a separate v62 copy, old-binary `index --rebuild` exited 0, regenerated Intel v61, and preserved the compared source/artifact counts. The original v62 fixture and preserved binary were unchanged. This verifies the bounded generated fixture, not recovery beneath active readers. No transparent downgrade or safe replacement beneath live readers is claimed.

Raw local results: `/tmp/engine-graph-benchmark-before-corrected.txt`, `/tmp/engine-graph-benchmark-after-corrected.txt`, `/tmp/engine-graph-index-results.json`, `/tmp/engine-graph-index-scaled-results.json`, `/tmp/engine-graph-index-scaled-five-results.json`, and `/tmp/engine-store-open-{base,revision}.txt`. The reproducible index harness is `/tmp/engine-graph-index-scaled-five.py`; these disposable paths are evidence from this run, not repository dependencies. Full-gate results are recorded in the child PR and coordinator-owned effort.

## Review correction: preserve trigger literal case

Codex review identified that lowercasing entire trigger definitions could accept a malformed predicate such as `src_type = 'CODE'` in place of `'code'`. Validation now compares the generated and stored DDL without changing case, normalizing whitespace, the trailing semicolon, and SQLite's omission of the generated `IF NOT EXISTS` clause. This is a strict comparison of owned generated definitions, not a general SQL equivalence parser.

The regression creates the case-only malformed trigger, proves that inserting a normal lowercase code-source row fails to increment the revision, then requires schema validation to reject it. The test fails against the original normalizer through a Go overlay and passes with the correction; the focused fingerprint/validator/reset suite also passes. Independent source review found no remaining issue. The child PR records the subsequent full gate and exact published head; the earlier performance samples remain attributable to the original implementation before this validation-only correction.


## Pre-B12 combined indexing comparison

The unchanged retained graph harness was rerun for five pairs after the final main write fixture, with no competing heavy work. Original `af100a9059fe1e34a88a59d70af26cf75e5e1368` was compared with clean `7d9550de7b07e855b7521699866eebf186f22ff1`, containing all selected production changes including B11. These are combined results, not isolated trigger attribution. The original baseline was built before frontend assets were generated. The combined binary retained generated frontend assets: `NO_WEB=1` skips rebuilding them but does not exclude them from Go embedding. These historical runs did not control asset state; the post-B12 matched full-UI comparison supersedes them for final combined claims. Recorded final build used `GOMAXPROCS=2 GOFLAGS=-p=2 NO_WEB=1 make build`. The harness retains `GOMAXPROCS=2`, the linked-note fixture, local test providers, and alternating pair order.

| Workload | Baseline median ms | Combined median ms | Change | Baseline range ms | Combined range ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| Scaled cold index, 5 pairs | 662.176 | 696.733 | +5.2% | 660.762–667.778 | 682.013–698.019 |
| Scaled incremental index, 5 pairs | 368.327 | 397.218 | +7.8% | 365.032–373.757 | 393.957–570.058 |

All 20 index invocations exited successfully. One combined incremental sample took 570.058 ms; retain that outlier and do not infer improved tail latency. The read-priority tradeoff remains visible on this workload even though the separate full-index write fixture improved.

All ten final incremental databases matched counts: 523 notes, 19 files, 31 symbols, 50 Intel anchors, 14 document links, zero graph-score rows, and 20,060 graph edges. They also matched normalized note/file identities and graph/link path-level topology. Graph-degree tuple equality is vacuous here because this targeted command leaves graph-score rows empty. The harness retains only the post-incremental database, so these counts do not claim a separately captured cold-state snapshot. The source fixture manifest contained 101 files and no database/sidecar/index-lock artifacts before execution.

Evidence: `/tmp/rhizome-final-write-verification/graph-results.json` retains samples and disposable fixture root; `graph-persisted-comparison.json` retains final counts and normalized tuple hashes; `graph-fixture-manifest.json` records source fixture files. `manifest.json` records source and build provenance plus binary SHA256 values: baseline `a0f950bc5a5874317de92ec3704ed816c250feb124c09827d9d282a8ae2d5316`, combined `7ba0b4c24669dfec5bc9b330171b07effc628b9b690625ccdfd8d5101591a396`, both unchanged after execution. The retained runner SHA256 is `1e36c3e894a289f84d8e4157ce762071e8907690e3ef369b9fea105a4399e8b1`. Disposable downgrade/refusal and explicit-regeneration evidence is in `compatibility/evidence.json` with both command logs.


## Final matched plain-index comparison

The current retained graph runner uses plain `index --vault` on both original `af100a90` and final measured `35394d21`. This includes unified graph-score and post-index work; it is a different workload from historical `--code` runs above. Full-UI build provenance and the diagnostic-only delta through `9cd25887` and subsequent guidance-only head `50029c2d` are recorded in [[engine-write-path-followup]]. Both binary hashes and the runner were independently verified before accepting results.

Five pairs alternate binary order on independent disposable fixtures, using `GOMAXPROCS=2` and deterministic local test embedding providers. Cold means a fresh derived database. Incremental changes redirect links in 50 generated notes. All 20 invocations exited zero.

| Workload | Original median ms (range) | Final median ms (range) | Change |
| --- | ---: | ---: | ---: |
| Fresh-database index | 3623.524 (3577.222–3756.068) | 1430.670 (1283.002–1490.796) | -60.5% |
| Incremental link edit | 3200.359 (3161.695–3203.157) | 921.549 (912.264–957.971) | -71.2% |

All ten final databases match counts: 523 notes, 19 files, 31 symbols, 50 Intel anchors, 14 document links, 542 graph-score rows and 20,060 graph edges. Normalized note/file identities, graph edges, path-level document links and all 542 graph-degree rows also match. The historical code-only workload had zero graph-score rows and cannot be pooled with this comparison or used as its baseline. Final edited databases do not establish separately retained cold-state topology or complete embedding/semantic parity. These small synthetic samples do not establish tail latency, production embedding latency or isolated trigger attribution; the prepared-row trigger cost above remains an accepted tradeoff.

Accepted evidence is `/tmp/rhizome-b12-final-accepted/{manifest.json,graph-results.json,graph-persisted-comparison.json}`. The results retain all samples and the fixture directory containing ten databases and logs. Current runner SHA256: `1968382a86a48e10828e5fd935f756d4807e86d60df17909e1a7f48e57ecac8d`. Rerun the command above with matched full-UI builds on a quiet host; no user index is required.


### Final explicit-rebuild verification

Fresh checks used four independent copies of a completed B12 main fixture and the accepted full-UI binaries above. Normal original-binary `index` exited 1 on Intel v62 with an identical before/after logical database dump. On a separate copy, original-binary `index --rebuild` exited 0 and regenerated v61. Final-binary `index --rebuild` exited 0 for both a healthy v62 copy and a copy whose Intel version was deliberately set to 999; each regenerated v62. All three rebuilds removed a copy-local sentinel table, proving full replacement, and retained the seven expected counts: 1,000 notes, 100 files, 100 symbols, 200 anchors, 100 document links, 1,100 graph scores and 1,998 edges.

The accepted source fixture's complete logical dump and both binary hashes remained unchanged. Commands used disabled providers, `GOMAXPROCS=4` and `RZM_SKIP_REPO_DELEGATE=1`. This is bounded disposable recovery evidence, not live-reader replacement or reverse migration. Exact commands, directories, schema/count/dump checks and logs are retained in `/tmp/rhizome-b12-compatibility/evidence.json`; runner `/tmp/b12-compatibility-check.py` reuses the earlier read-only logical-state inspector.

## v63: note-update trigger scoped to graph-relevant columns

Intel v63 narrows the `notes` update trigger to `AFTER UPDATE OF path, title`. No web-graph read path reads the `notes` table: node paths come from `intel_doc_sections` through `GraphDocPaths`, catalog rows and labels from `ontology_nodes`, types from `ontology_note_types`, and scores from the two score tables. The v62 trigger fired on every `UPDATE notes`, including the metadata-only writers `TouchNoteMtimes`, `TouchIntelNotePaths`, `UpsertNoteMeta` and `UpsertNoteMetadataBatch`.

Bump attribution on a disposable copy of `testdata/integration/python-app/vault` (24 notes), using counting triggers installed on each graph-input table and one full `rzm index` after `touch`-ing every Markdown file with unchanged content: 46 metadata-only `UPDATE notes` row executions occurred, none of which now advance the revision. A re-index with no filesystem change advanced the revision by 0.

The same touch-only re-index still advanced the revision by 327, because indexing rewrites genuine graph inputs for touched notes: `graph_doc_edges` 60 inserts and 60 deletes, `ontology_nodes` 46/46, `ontology_edges` 36/36, `ontology_note_types` 10/10 (304 attributed; 23 bumps are outside the probed table set). So this change removes 46 of roughly 373 pre-fix bumps on this fixture, and mtime-only re-indexing still invalidates cached graph responses through the ontology and edge rewrites. Single-sample counts on one small fixture; they are exact bump counts, not timings, and no latency claim is made.

Those remaining 327 bumps are eliminated by keying note state on content identity rather than mtime; see [[docs/performance/mtime-only-reindex|Mtime-only re-index cost]], which depends on this v63 narrowing.
