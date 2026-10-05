---
type: TechnicalSpec
summary: "Defines Rhizome's indexing pipeline contract: staged parallel extraction, one durable queued writer lane, shared semantic runtime lanes, explicit correctness barriers, and bounded backpressure across batch and live indexing."
id: SPEC-0012
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0012
  - Indexing pipeline architecture (Design)
---

# Rhizome indexing pipeline architecture

## Summary

Rhizome indexing is a staged pipeline optimized around two constraints: read/parse/extraction/provider preparation scale with parallelism, while durable state lands in one SQLite-backed persistence surface where write concurrency is a liability; semantic provider calls also need one shared policy surface so code, notes, ontology primary chunks, and intent exemplars do not invent competing batchers. The pipeline therefore favors parallel compute before commit, explicit correctness barriers where final cross-file state matters, shared semantic runtime lanes for provider work, and one queue-backed writer lane for durable writes.

This spec freezes the intended architecture for both `rzm index` and live watcher-driven indexing.

## Goals

- maximize throughput for large indexing runs without hiding correctness tradeoffs
- preserve low-latency incremental updates for live indexing
- keep write serialization, batching, and instrumentation in one deliberate place
- keep provider construction, compatible lanes, packer ceilings, and gates in one semantic runtime
- make pipeline bottlenecks legible enough to tune without guessing

## Non-Goals

- maximizing goroutine count as a goal by itself
- letting new indexing features invent independent hot write paths
- forcing every stage into a fully streamed model when cross-file correctness would regress
- specifying the full implementation details of every batch size or queue threshold

## User Stories

### US1 - Add a new indexing stage without bypassing the staged pipeline, single durable writer lane, shared semantic runtime lanes, or correctness barriers
- id:: ^SPEC-0012-US1
- summary:: Add a new indexing stage without bypassing the staged pipeline, single durable writer lane, shared semantic runtime lanes, or correctness barriers.
- status:: ready

#### Acceptance Criteria

- New high-volume durable writes use the indexing queued writer or document why the workload is outside the hot indexing path. ^SPEC-0012-US1-AC1
  verification:: Review changed indexing code for direct high-cardinality SQLite writes; tests cover queue-backed writeback where applicable.
- New provider-backed embedding work uses `semanticruntime` compatible lanes when a shared runtime is available. ^SPEC-0012-US1-AC2
  verification:: Inspect the new embedding path and confirm it receives a shared embedding node/lane instead of constructing an ad hoc provider-only batcher.
- Any stage that depends on final cross-file state stays behind an explicit barrier with a named timing/progress phase. ^SPEC-0012-US1-AC3
  verification:: Run targeted tests or `rzm index --timings` and confirm the dependent stage cannot observe partial ingest state.

### US2 - Improve throughput using bounded queues, truthful instrumentation, and explicit policies without weakening retrieval correctness
- id:: ^SPEC-0012-US2
- summary:: Improve throughput using bounded queues, truthful instrumentation, and explicit policies without weakening retrieval correctness.
- status:: ready

#### Acceptance Criteria

- Discovery, parse, and preparation stages backpressure on bounded worker/provider/write queues rather than accumulating unbounded hidden work.
  verification:: Unit tests or timing output expose queue depth/wait metrics for the tuned path.
- Timing labels separate planning, provider calls, queue wait/writeback, pruning, graph recompute, and maintenance for the tuned domain.
  verification:: `rzm index --timings` shows the tuned work under named phases that match the actual work performed.
- Throughput changes preserve ontology-ready chunk-family semantics: typed-note primary body evidence in `ontology_node`, and raw `doc_section` embeddings only for ontology-unavailable compatibility.
  verification:: Index a typed-note fixture and inspect chunk families after the tuned run.

## Requirements

### Must

- Indexing MUST remain a staged pipeline with distinct phases for discovery, parse/extract, overlap-safe downstream preparation, durable writeback, and finalization.
- Parse, extract, and other path-local compute-heavy stages MUST be allowed to run in parallel when they do not depend on final global state.
- High-volume durable indexing writes MUST flow through one indexing-scoped queued writer lane.
- The queued writer lane MUST own batching, flush policy, and write-serialization observability for hot indexing paths.
- Indexing-owned provider work MUST flow through `pkg/app/semanticruntime` compatible lanes when a shared runtime is available.
- Indexing paths MUST NOT create ad hoc provider-only lanes for code, note, ontology primary, or intent exemplar work.
- Later stages that depend on final cross-file state MUST stay behind explicit correctness barriers.
- Bounded queues MUST be used so overload shows up as visible backpressure instead of silent unbounded work accumulation.
- Path-local replacement semantics MUST remain transactional for note/code updates.
- Batch indexing and live indexing MUST preserve the same core invariants even when they tune coalescing and batch sizes differently.
- Performance and health instrumentation MUST continue to map to real pipeline stages such as queue wait, writer busy time, provider wall time, and stage wall time.
- Ontology-ready vaults MUST use ontology body chunks as the primary note semantic surface; raw authored-section embeddings are compatibility-only for ontology-unavailable runs.
- Ontology semantic indexing MUST emit source-owned `node_body` primary chunks after fresh ontology projection; no parallel generated-card chunk family may compete with them.
- Scoped ontology primary follow-up MUST derive owner work from changed/deleted note paths plus affected ancestor/descendant context; schema or primary format changes MAY force a full sync.
- Cross-process batch/background indexing MUST coordinate through the index lock and priority request files before competing for shared index writes.
- Full `VACUUM` MUST be explicit or freelist-gated; routine maintenance should keep using cheaper analyze/optimize/checkpoint work.
- Code comments and coderefs on key indexing orchestration/policy code MUST point to the smallest governing spec node when the rationale cannot be inferred from types or local control flow.

### Should

- Overlap-safe downstream stages should start early enough to keep CPUs and providers busy while ingest is still discovering work.
- Global or deferred recomputes should happen after the core durable write stages rather than leaking partial cross-file state into earlier phases.
- New indexing features should plug into an existing stage or define a new named stage with explicit barriers and instrumentation, not bypass the pipeline model.
- Watcher and rebuild flows should share as much of the same persistence and finalize logic as possible.
- `--timings` should separate primary ontology planning/rendering, provider embedding, writeback, and pruning so planning waits do not masquerade as provider cost.
- File discovery should backpressure on worker capacity, not build a hidden unbounded read backlog.

### May

- Queue policy, batch sizing, and flush windows may vary by workload as long as the durable single-writer contract remains intact.
- Some deferred or global maintenance work may move between finalize slices when profiling shows a better placement without violating correctness.

## Open Questions

- which current deferred/global recomputes should eventually become independently observable pipeline nodes
- whether any live-indexing slices need stricter published latency targets once the backlog migration is farther along
