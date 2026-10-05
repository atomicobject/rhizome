---
summary: "Bulk alias construction and snapshot reuse evidence for incremental note indexing."
reference-kind: guide
---

# Incremental alias batching

B06, EFF-2026-09-05-14-10 / SPEC-0089. The old delta path populated aliases through repeated `AddOrUpdate` calls, each scanning existing alias claims. It also loaded the persisted alias map twice for an unchanged topology. The new path reuses the topology snapshot and bulk-builds the union of candidate paths and alias owners through the existing constructor.

Behavior coverage preserves duplicate alias claimants, ordinary candidate paths, alias owners outside the initial candidate set, and relative Markdown links. A source-only edit asserts a one-note delta and one alias query, then compares persisted graph links against a fresh full metadata snapshot. Existing topology-change handling still rederives links broadly when required.

## Controlled comparison

Apple M4 Pro, Darwin arm64, `GOMAXPROCS=4`, real SQLite for the path delta. Three operations per sample, three samples, sequential prebuilt baseline then current binaries. Baseline source is `76d41fd3`; it uses the identical retained benchmark with only the bulk-helper call replaced by the former `applyAliasesToCache` call. Database seeding is excluded. The component benchmark includes initial candidate-cache creation on both sides; the path-delta benchmark derives the same existing note without applying each delta.

| Benchmark | Before median ms/op | After median ms/op | Before → after median bytes/op |
| --- | ---: | ---: | ---: |
| Alias cache, 1,000 paths | 11.170 | 0.544 | 687,352 → 1,277,000 |
| Alias cache, 5,000 paths | 314.213 | 3.289 | 2,846,904 → 5,269,437 |
| Existing-path delta, 1,000 aliases | 16.058 | 4.345 | 4,489,592 → 4,093,672 |

The isolated constructor allocates more because it rebuilds the complete candidate/alias-owner union. The complete measured delta reduces both time and allocation after removing duplicate snapshot work. These are component and metadata-delta results, not a full indexing speedup claim.

Retained source: `pkg/notemeta/projection_incremental_benchmark_test.go`. Compile baseline and current test binaries with `go test -c -tags=fts5 ./pkg/notemeta`; use a disposable baseline checkout and the single benchmark-call adaptation above. Run each binary in sequence:

```sh
GOMAXPROCS=4 /absolute/path/to/notemeta.test -test.run '^$' -test.bench 'BenchmarkBuildDeltaAliasCache|BenchmarkBuildPathDeltaAliases1000' -test.benchtime=3x -test.count=3
```

Session logs: `/tmp/b06-before-bench.txt`, `/tmp/b06-after-bench.txt`. Focused metadata tests and independent source/behavior review passed. Full repository verification is recorded with the delivery PR.
