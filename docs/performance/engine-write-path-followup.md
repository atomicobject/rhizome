---
summary: "Ownership batching evidence and disposition of remaining write-path performance candidates."
reference-kind: guide
---

# Ownership batching and write-path follow-up

Delivery effort EFF-2026-09-05-14-10 / SPEC-0089, 2026-09-05. The initial research report records historical observations before the selected changes; its original recommendations are not the final selection. Selected batches are B02 atomic code persistence, B06 bulk alias construction, B07 shared SQLite transaction retry, B09 authoritative explicit code freshness, and B10 ownership lookup and cleanup batching.

## Ownership batching delivery

B10 replaces two per-note ownership lookups with one transaction-local query per bounded chunk (400 paths, two parameters per path). It retains the existing three equality predicates, indexed-note requirement, projection defaults, and code-path normalization. Reverse-index source rows are retired for the complete effective path set in bounded chunks, then raw symbol/external targets are pruned once. Cleanup remains unconditional when root owner rows are absent; all work stays in the existing ownership transaction.

Regressions cover 901-path lookup and effective-deletion boundaries, mixed owners, coexisting code ownership, missing owner rows, orphan targets, a shared target retained by an untransitioned source, and rollback with no published reconciliation generation. Existing affected-source/anchor and pending-generation coverage remains. Independent and coordinator production reviews found no actionable issue.

Controlled baseline `e4224dda6fad3fb8003ea5d18ada0ccd816ab3e7`, Apple M4 Pro / Darwin arm64 / `GOMAXPROCS=4`. Three operations per sample, three samples. Matched prebuilt binaries use the identical retained `pkg/anchors/sqlite/ownership_transition_bench_test.go` source. Setup, database open/migrations, seed writes, and collector rendering are outside timing. The no-op case seeds 1,000 current note sources. The retirement case seeds 1,000 reverse-index source/target pairs without root owner rows and transitions those paths to unowned; it is not a complete cold full-index fixture.

| 1,000-path operation | Before median ms/op | After median ms/op | Before → after median allocated bytes/op | Before → after cumulative writer hold per 3 operations |
| --- | ---: | ---: | ---: | ---: |
| Current note no-op | 20.560 | 2.684 | 3,526,341 → 1,086,298 | 61 → 7 ms |
| Populated reverse-index retirement | 254.491 | 148.736 | 16,348,429 → 14,618,797 | 763 → 445 ms |

Writer holds use the collector's native millisecond precision; setup writes are excluded. Earlier one-operation exploratory samples included database bootstrap in the retirement timer and are superseded by this corrected controlled comparison. No aggregate full-index speedup is inferred from these component results.

Compile current and a clean baseline package with the same retained benchmark (`go test -c -tags=fts5 ./pkg/anchors/sqlite`); the baseline can use a disposable Go overlay to add that benchmark source. Run both binaries sequentially:

```sh
GOMAXPROCS=4 /absolute/path/to/sqlite.test -test.run '^$' -test.bench '^BenchmarkApplyOwnershipTransitions1000$' -test.benchtime=3x -test.count=3
```

Session logs: `/tmp/b10-before-bench.txt`, `/tmp/b10-after-bench.txt`. Full repository verification is recorded in the delivery PR.

## Deferred candidates

| Candidate | Evidence from initial research | Disposition and next useful proof |
| --- | --- | --- |
| C08 full-vault freshness and dirty scans | At 1,000 synthetic notes, `MetadataStateCurrent` read 2,000 sources over two enumerations (3.275 ms); `DiscoverDirtyPaths` read/projected all 1,000 sources (14.746 ms and about 40.6 MB allocated). New-path deltas intentionally rederive all links. | Deferred after B06 removes the measured quadratic alias work and duplicate alias query. Removing source verification requires a trustworthy freshness contract; new/deleted paths and alias changes still need broad link rederivation because unresolved inbound links are not persisted. A narrower future candidate is returning the already-computed snapshot from `MetadataStateCurrent`; measure actual caller frequency and preserve provider diagnostics and freshness parity before selecting it. |
| Per-file rationale DDL | The 512-path empty-rationale experiment took a median 17.523 ms and executed nine schema statements per path. | Deferred behind reproduced data-integrity/freshness failures and the larger measured ownership/alias costs. Moving DDL to migration ownership is plausible but needs first-open/upgrade and rationale/FTS replacement parity. Re-measure representative populated rationale after B02, whose transaction boundary changed, before claiming an end-to-end gain. |
| Watcher maximum batch age | A 100 ms debounce receiving 30 events every 20 ms delivered no callback during 625.9 ms of activity, then one callback after 150 ms quiet. | Deferred as a scheduling policy change, not accepted as solved. A maximum age would trade bounded delivery latency against more overlapping/batched indexing work. Establish a latency target and test sustained multi-path events, cancellation, overflow recovery, and eventual convergence before changing the hub. |
| No-op configuration and maintenance | Three unchanged full indexes each rewrote configuration; `ANALYZE` took about 3 ms in the fixture. Downstream invalidation amplification was not measured. | Deferred because the measured benefit is smaller than the selected hot paths. Compare serialized configuration before atomic replacement and separately assess maintenance due-gating; preserve unknown fields, scope metadata, migration behavior, and checkpoint requirements. Do not attribute watcher/cache amplification without measuring it. |

The initial fixtures use real SQLite but synthetic note content and disabled embedding providers. Their absolute timings are historical, host-specific evidence, not current release performance. Complete source for the metadata, rationale, watcher, and full-index reproducers is retained in the write-path research report; temporary output paths are supplementary evidence only.

## Reproducibility of selected batches

Each accepted timing comparison must identify its source revision, retained benchmark/fixture, sample count, concurrency setting, and setup exclusions. B02 retains `BenchmarkCodePersistenceBatch`; B06 retains alias construction and path-delta benchmarks; B09 retains `BenchmarkWarmNoopCodeIndex` with read counts/bytes and parser/persistence counters; B10 retains `BenchmarkApplyOwnershipTransitions1000` with cumulative writer-hold metrics. B07 uses deterministic retry behavior and real two-handle contention coverage rather than claiming a throughput improvement.

B09 removes an obsolete boolean argument from internal Go indexing orchestration and migrates its callers. Reverse-index reset context and readiness checks remain at callers; this is not a CLI flag removal.

## Combined indexing comparison

`scripts/perf/engine_write_fixture.py` preserves the original 1,000-note/100-code generator and runs three independent fixtures per binary. Each has a fresh-database index, three unchanged runs, and one note-body edit. Setup and binary hashing are outside timed subprocesses. It records binary hash/version, exit codes, wall times, complete collector output, and stable persisted note/file/symbol/anchor/link/graph counts; it rejects existing output directories and stops on failure. The runner uses `GOMAXPROCS=4` with note and code embeddings disabled. “Cold” means a fresh database, not flushed operating-system caches. Equal persisted counts establish cardinality, not row-content or semantic parity. The separate graph-revision harness uses `GOMAXPROCS=2` and local test embedding providers; its samples are a different workload and must not be pooled with this comparison.

Run the same retained script with verified base and final binaries during a controlled slot, alternating binaries on fresh fixtures:

```sh
for pair in 0 1 2; do
  python3 scripts/perf/engine_write_fixture.py --binary /absolute/path/to/base-rzm --output "/tmp/engine-write/base-$pair" --samples 1
  python3 scripts/perf/engine_write_fixture.py --binary /absolute/path/to/final-rzm --output "/tmp/engine-write/final-$pair" --samples 1
done
```

Record source revisions and build commands alongside both binary hashes. Compare complete phase wall time and collector writer hold; do not sum isolated component speedups. The completed combined comparison below measures the selected changes together.


### Pre-B12 combined evidence

Controlled comparison on macOS 27.0 / Apple M4 Pro arm64 / Go 1.24.2, with other heavy work paused. Original source `af100a9059fe1e34a88a59d70af26cf75e5e1368` was compared with clean `7d9550de7b07e855b7521699866eebf186f22ff1`, containing all selected production changes including B11. The original baseline was built before frontend assets were generated. The combined binary retained generated frontend assets: `NO_WEB=1` skips rebuilding them but does not exclude them from Go embedding. These historical runs did not control asset state; the post-B12 matched full-UI comparison supersedes them for final combined claims. Recorded final build command: `GOMAXPROCS=2 GOFLAGS=-p=2 NO_WEB=1 make build`. Index subprocesses use the runner's `GOMAXPROCS=4`. Baseline SHA256: `a0f950bc5a5874317de92ec3704ed816c250feb124c09827d9d282a8ae2d5316`; final SHA256: `7ba0b4c24669dfec5bc9b330171b07effc628b9b690625ccdfd8d5101591a396`. Hashes matched again after execution.

Three baseline/final pairs ran sequentially on six fresh fixtures. All 30 phase invocations exited successfully.

| Phase | Samples per binary | Baseline median ms | Combined median ms | Change | Baseline range ms | Combined range ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Fresh-database index | 3 | 1090.753 | 999.310 | -8.4% | 1085.505–1103.418 | 990.269–1001.646 |
| Unchanged index | 9, three per fixture | 227.356 | 193.437 | -14.9% | 223.871–230.699 | 188.581–195.746 |
| One note-body edit | 3 | 340.369 | 305.762 | -10.2% | 338.178–345.900 | 304.340–308.921 |

These are measured subprocess walls, not sums of component gains. Collector phase medians show unchanged-code ingestion increasing from 1 to 5 ms with authoritative byte reads, while ownership transaction hold falls from 46 to 8 ms on unchanged runs. Fresh-run ownership hold falls from 696 to 612 ms, and note-edit hold from 49 to 10 ms. Fresh note ingestion increases from 82 to 92 ms; fresh graph computation falls from 52 to 31 ms. Collector values retain native millisecond resolution and do not sum to subprocess wall time; overlapping work, unnamed preparation, process overhead, and rounded totals are separate.

Every phase retained the same counts on both binaries: 1,000 notes, 100 files, 100 symbols, 200 Intel anchors, 100 document links, 1,100 graph-score rows, and 1,998 graph edges. Final note-edit databases also matched deterministic tuples for note path/title/format, file path/language, graph source/destination/kind, document-link source/destination paths/kind/language/label, and graph path/type/inbound/outbound. This checks source identity and path-level topology while excluding timestamps and replaceable IDs; it does not prove complete semantic or embedding parity. No tail-latency or production embedding-service claim follows from these small synthetic samples.

The separate pre-B12 five-pair code-only graph workload is recorded in [[engine-graph-revision]] and was slower on the combined binary. It differs from the plain-index workload below.

Retained evidence directory: `/tmp/rhizome-final-write-verification/`. `manifest.json` records source/build/binary/runner provenance; `main-summary.json` retains all wall samples, counts, topology hashes, and raw collector sections; `main-phase-summary.json` and `main-collector-summary.json` summarize printed phase/operation metrics. Each fixture's `evidence.json` and phase logs remain under `main/`. The final CLI binary is `final-cli-rzm`. These local outputs supplement the checked-in unchanged runner.


### Final matched plain-index evidence

B12 makes `rzm index` the single top-level indexing mode, selected by configuration and freshness. It removes `--code`, `--semantic`, force overrides and mode-scoped rebuilds; full `--rebuild` remains. Disabled code and embedding settings stay disabled. Active callers, templates, generated guidance and contracts use the plain command. Historical mode-specific measurements above retain their original meaning.

The accepted final comparison uses full-UI builds on both sides: original `af100a9059fe1e34a88a59d70af26cf75e5e1368` and integrated implementation `35394d210e468e4ed3d39d1734291f7a17d76507`. Baseline binary SHA256 is `78de3d45145179586073fbd9778826d16ac771e1e8c5d8b6e822ef1d1fbc548b`; final is `81ac096f1460e75a8ea0cfbf8f4e05dc6ce6cf3e54dfe7d317927aa8ec419085`. Both embed 33 built asset files from their respective source revisions. The final committed Go rebuild reused assets from the successful full build. `NO_WEB=1` skips building assets; it does not exclude existing assets from embedding. Independent audit rehashed both binaries, all 66 assets and both runners. Later head `9cd25887bca0e5ab2e08962656a642790ea6808d` changes exactly four diagnostic strings; measured indexing, runtime, frontend and build inputs are unchanged. Subsequent head `50029c2d4e87079b671cda349bcf860e7a43824c` only corrects eight guidance files; independent review verified that committed boundary.

On the same Apple M4 Pro / macOS 27.0 / Go 1.24.2 host, with competing heavy work paused, the unchanged main runner used plain `index --timings`, disabled embeddings and `GOMAXPROCS=4`. Three sequential original/final pairs used independent fresh fixtures. Every one of the 30 phase invocations exited zero.

| Phase | Samples per binary | Original median ms (range) | Final median ms (range) | Change |
| --- | ---: | ---: | ---: | ---: |
| Fresh-database index | 3 | 1073.842 (1072.935–1091.155) | 992.135 (988.268–1012.807) | -7.6% |
| Unchanged index | 9, pooled across 3 fixtures | 223.223 (218.876–229.867) | 188.997 (186.599–191.160) | -15.3% |
| One note-body edit | 3 | 330.196 (328.006–332.405) | 296.512 (295.043–312.652) | -10.2% |

All phases match counts: 1,000 notes, 100 files, 100 symbols, 200 Intel anchors, 100 document links, 1,100 graph-score rows and 1,998 graph edges. The six final databases also match normalized note/file identities, graph edges, path-level document links and graph degrees. These comparisons exclude replaceable IDs and timestamps; they do not establish complete row, weight, score, embedding or semantic parity. Only final edited topology was retained. Fresh databases do not imply flushed OS caches, and these samples do not establish tail latency or external embedding-service performance. Gains measure all integrated changes together, not the isolated effect of deleting flags.

Accepted artifacts are under `/tmp/rhizome-b12-final-accepted/`: `manifest.json`, `main-summary.json`, `main-phase-summary.json`, per-fixture evidence and logs, preserved binaries and summarizer source. The main runner SHA256 remains `00ce72c2daa2c4cc94ae006a9c3ef4c2e0375595bdca4b407729ddafb3b8b6ff`. The graph workload, now also plain index on both binaries, is recorded in [[engine-graph-revision]]. Preliminary mismatched-asset runs are excluded from final claims.
