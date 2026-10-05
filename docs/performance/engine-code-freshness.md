---
summary: "Correctness and measured read cost of authoritative explicit code indexing."
reference-kind: guide
---

# Authoritative explicit code freshness

B09, EFF-2026-09-05-14-10 / SPEC-0089. Explicit code indexing previously skipped reads when persisted anchor timestamps were at least the selected file's timestamp. Same-second edits, preserved timestamps, and backdated content could therefore leave old symbols after successful indexing.

Full and code-only indexing now read every selected code file. The existing content hash, indexer version, and trusted parse-status gate decides whether parsing and persistence are needed. The global anchor timestamp preload and unchanged-path timestamp touches are removed. Watchers already read their event-selected files before hash checking; their scheduling is unchanged.

The obsolete boolean argument is removed from internal Go orchestration functions, with all callers migrated. Reverse-index reset context and readiness checks remain at callers. Exported anchor timestamp observation methods remain available, with corrected comments. This is not a CLI flag removal.

## Behavior evidence

Regression cases cover different nanoseconds within one second, exactly preserved timestamps, and backdated content. The counting store embeds the real SQLite store so the old optional timestamp interface remains visible; these regressions fail against the original implementation. A subsequent unchanged index asserts zero additional parser calls and persistence batches. Actual CLI tests run both full indexing and `index --code`, then verify stored symbols replace `Before` with `After` despite an unchanged timestamp. Independent source review and coordinator review found no actionable production issue.

## Controlled cost comparison

Baseline `e4224dda6fad3fb8003ea5d18ada0ccd816ab3e7`, Apple M4 Pro, Darwin arm64, `GOMAXPROCS=4`. Each real Go file is exactly 4,096 bytes. Three operations per sample, three samples; baseline and current prebuilt binaries run sequentially. Files and database are seeded before timing, so these are warm-cache no-op measurements. Embeddings are absent from this code-ingest benchmark.

| Entry point / files | Before median ms/op | After median ms/op | Before → after median bytes allocated/op |
| --- | ---: | ---: | ---: |
| Candidates / 1,000 | 13.204 | 29.540 | 5,373,480 → 21,329,450 |
| Root walk / 1,000 | 26.355 | 37.857 | 10,520,034 → 26,366,786 |
| Candidates / 5,000 | 66.303 | 146.673 | 25,042,290 → 104,923,072 |
| Root walk / 5,000 | 129.313 | 186.200 | 50,784,712 → 130,086,229 |

Before: zero source reads. After: 1,000 reads / 4,096,000 bytes or 5,000 reads / 20,480,000 bytes per operation. Both versions perform zero parser calls and zero persistence batches for these unchanged files. File-read counts come from the collector; byte totals use the fixed payload size. The benchmark fails if no-op result counts, parser calls, or persistence batches differ from their expectations.

This is a correctness tradeoff, not a performance improvement. File reads, hashing, and per-file guard work increase warm no-op latency and allocation. Operating-system cache misses, other storage, larger source files, and embedding providers are not represented by these samples. Combined full-index measurements must assess the cost alongside the other batches; isolated improvements must not be summed.

Retained source: `pkg/app/codeintel/ingest_freshness_benchmark_test.go`. Compile the test binary with `go test -c -tags=fts5 ./pkg/app/codeintel`. The baseline uses a disposable Go overlay restoring the original ingest and affected original tests at the baseline revision; disposable copies of the new benchmark and regression add the former boolean argument. Checked-in code uses current signatures directly. Run both binaries sequentially:

```sh
GOMAXPROCS=4 /absolute/path/to/codeintel.test -test.run '^$' -test.bench '^BenchmarkWarmNoopCodeIndex$' -test.benchtime=3x -test.count=3
```

Session logs: `/tmp/b09-before-bench.txt`, `/tmp/b09-after-bench.txt`. Full repository verification is recorded with the delivery PR.
