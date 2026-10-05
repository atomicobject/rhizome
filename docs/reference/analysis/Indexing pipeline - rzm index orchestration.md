---
type: ReferenceDoc
summary: "Current orchestration rules for `rzm index`, including unified core phases, semanticruntime lanes, primary chunk sync, rebuild semantics, lock coordination, and scope invalidation."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Indexing pipeline - rzm index orchestration.md
last-verified: 2026-09-05
status: active
---

# Indexing pipeline - rzm index orchestration

## Summary

`rzm index` is the authoritative batch builder for notes, code anchors/intel, source-owned primary semantic chunks, persisted graph signals, and cheap SQLite maintenance. The CLI surface routes into `pkg/app/indexing`; `RunUnifiedCore` owns the batch path and selects work from configured scope, enabled providers, and content freshness.

## Contracts

- plain `rzm index` uses the unified pipeline; top-level code/semantic mode flags and their force overrides are retired
- note embedding work consumes published note metadata and ontology projection state, then drains primary chunk writeback before marking semantic freshness
- file discovery goes through `pkg/app/indexingpipe.ProcessFiles`, so producer walk-ahead is bounded by worker queue capacity
- high-volume code/note/semantic writes go through the indexing queued writer, which keeps SQLite write serialization observable
- provider work should use `pkg/app/semanticruntime` compatible lanes; indexing paths should not build one-off provider batchers for code, note, ontology body, or intent exemplar work
- ontology-ready vaults use ontology body chunks as the primary note semantic surface; raw authored-section chunks are compatibility-only when ontology is unavailable
- ontology primary chunks are generated after fresh projection and use source-owned `node_body` granularity
- ancestor/context or primary-chunk format changes expand affected ontology owners explicitly; schema/format rebuilds may force full sync
- scope-config hash changes invalidate discovery caches automatically
- explicit `--rebuild` clobbers the unified DB and WAL/SHM sidecars before recreating every domain; mode-scoped rebuilds are retired
- incremental indexing reuses compatible embeddings; explicit full rebuilds discard the derived database and its caches
- `--timings` should keep planning/rendering, provider embedding, writeback, prune, graph, and maintenance phases distinct
- index lock acquisition happens before competing batch/background indexers write shared state

## Convergence boundary

Batch indexing is the authoritative convergence path. A successful run means discovered note/code source rows, ontology/catalog rows, source-owned semantic chunks/embeddings, graph signals, and sync freshness marks have all crossed their final writer barriers. Error cleanup must stop semantic producers before the writer closes, then drain or fail the remaining tail work deterministically.

Ontology body/intel chunk writes cross the queued writer barrier before source high-water marks advance. `last_sync` is diagnostic wall-clock state only; skip decisions use source generations and high-water marks.

Lazy pruning may preserve old rows for branch-switch reuse. Preserved rows are storage/cache artifacts, not query-visible active index state; search and lookup surfaces should filter to the current valid generation while still allowing old vectors to seed reuse.

## Related

- [[indexing-workflow]]
- [[indexing-pipeline-architecture]]
- [[Embeddings - indexing pipeline]]
- [[Indexing pipeline - End-to-end walkthrough]]
- [[Indexing pipeline - Concurrency + batching requirements]]
