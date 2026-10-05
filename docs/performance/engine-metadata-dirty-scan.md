---
summary: "Single-inventory metadata verification and source-only dirty discovery evidence for the two no-op whole-vault metadata checks."
reference-kind: guide
---

# No-op metadata checks: verification and dirty discovery

C08. Two whole-vault checks in `pkg/notemeta/indexer_operations.go` did redundant work on every invocation.

`Indexer.MetadataStateCurrent` enumerated and read every selected note twice: once inside `metadataStateCurrentWithSelectedProviders` and again through a second `computeNotesHash` call whose only purpose was recovering the path list that the first pass had already computed and discarded. That helper now returns its inventory.

`Indexer.DiscoverDirtyPaths` loaded every note through the full provider projection, but the dirty comparison consumes only authored source identity: path, content hash, mtime, size, plus the persisted row's projection status. Discovery now loads authored sources (`loadAuthoredSources`) and never calls `Runtime.Project`. The bounded loader is shared as a generic `loadBounded[T]`, so ordering, first-error, and cancellation semantics are unchanged.

The docket's original framing — reuse the verification snapshot inside the dirty scan — does not apply: the two entry points are never executed in the same command path, so there was nothing to share. Both changes delete work instead of adding a cache. The legacy `discoverDirtyPaths`/`metadataStateHashForPaths` pair, which existed only for isolated parity tests, is deleted.

## Callers

| Path | `MetadataStateCurrent` | `DiscoverDirtyPaths` |
| --- | --- | --- |
| `rzm index` no-op re-run (`RunUnifiedCore`) | No | No |
| Watcher-driven reindex (`pkg/app/bootstrap/schedulers.go`) | No | No |
| Agent session start (`ontology.EnsureIndexed` → `EnsureIndexed`) | No | No |
| `rzm validate` / `rzm agent validate` without exact paths | No | Yes, once per run |
| `rzm graph stats/clusters/dead-ends`, `rzm orphans`, health dead-ends, `context_text` graph summary (non-managed) | Yes, once per command | No |

MCP tools take the `LoadReadyPersistedGraphSnapshot` path and reach neither entry point.

## Controlled comparison

Apple M4 Pro, Darwin arm64, 14 logical CPUs. `BenchmarkNoopMetadataChecks` (`pkg/notemeta/indexer_benchmark_test.go`): 1,000 generated notes with a title, a tag, a unique alias, and a wikilink to `n0000`; real SQLite in a temp dir; in-memory note reader with call counters. Baseline is `dd34d5d4` with only the benchmark file added. Prebuilt test binaries, `-count 5`, medians below. The host was shared with other agent work, so timings are indicative; the counter and allocation columns are exact.

| Sub-benchmark | Before ms/op | After ms/op | Before B/op | After B/op | Before allocs/op | After allocs/op | reads/op | lists/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `metadata_state_current` | 2.995 | 2.679 | 2,624,267 | 2,286,307 | 39,014 | 34,009 | 2,000 → 1,000 | 2 → 1 |
| `discover_dirty_noop` | 14.393 | 6.352 | 37,617,803 | 3,936,010 | 284,582 | 43,104 | 1,000 → 1,000 | 1 → 1 |

Dirty discovery drops about 90% of its allocated bytes and 85% of its allocations; its wall time roughly halves. Verification halves reader traffic; its wall time falls about 11% because the persisted-row fetch and hashing, not the in-memory reads, dominate that benchmark.

`reads/op` stays at 1,000 for discovery on purpose. Every selected note's bytes are still read and hashed on every call; timestamp or size equality never short-circuits the content check (B09).

This is a component measurement. It is not a claim about end-to-end `rzm validate` wall time, which is dominated by the validation product runner's own work.

## Behavior notes

Because discovery no longer projects, projection-time Go errors (`ErrProjectorUnavailable`, `ErrInvalidProjection`) no longer abort a dirty scan; they still surface from `BuildPathDelta` for changed paths. This aligns discovery with `EnsureIndexed`'s existing descriptor-only handling. Diagnostics remain produced and persisted at projection and publication time only.

## Related docs

- [[docs/reference/subsystems/notemeta|Notemeta subsystem guidance]]
- [[docs/performance/engine-follow-up-handoff|Engine follow-up handoff]]
- [[docs/performance/engine-code-freshness|Code freshness]]
