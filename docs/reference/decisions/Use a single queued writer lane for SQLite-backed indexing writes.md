---
type: ReferenceDoc
reference-kind: architecture
summary: "Route high-volume indexing writes through one queued writer lane so SQLite batching, write serialization, and instrumentation live in one place."
status: active
decision-domain: architecture
tags: [type/decision, subsystem/indexing, subsystem/intel, subsystem/embeddings, subsystem/performance]
derived-from:
  - docs/decisions/Use a single queued writer lane for SQLite-backed indexing writes.md
last-verified: 2026-04-12
---

# Use a single queued writer lane for SQLite-backed indexing writes

## Context

Rhizome stores indexing state in one WAL-mode SQLite database. Readers scale well; writers do not. Earlier or simpler designs let more write paths talk to stores directly, which made small writes, mutex contention, and noisy lock waits easy to introduce.

Indexing throughput depends less on how many goroutines can write than on how effectively hot write traffic gets turned into larger, predictable transactions.

## Decision

Use one indexing-scoped queued writer lane for high-volume indexing writes.

That queue owns batching and flush policy for:

- code ingest writes
- note ingest writes
- semantic item and chunk writes
- intel embedding writes
- other indexing-stage batch deltas that target the same SQLite durability path

Direct store writes are still fine for cold or isolated operations, but hot indexing loops should not invent side-channel write paths.

## Consequences

- SQLite serialization becomes an explicit architectural choice instead of an incidental runtime effect.
- Batching policy can be tuned per write class without changing every caller.
- Queue wait and writer busy time become meaningful system-level metrics.
- New indexing features need to integrate with the queue rather than writing opportunistically from worker goroutines.

## Follow-ups

- [Indexing pipeline architecture](../technical/indexing-pipeline-architecture.md)
- [Semantic code index spine](../technical/semantic-code-index-spine.md)
- [Indexing pipeline - Concurrency + batching requirements](../../reference-notes/Indexing pipeline - Concurrency + batching requirements.md)
- [Code Index - Unified SQLite DB](../analysis/Code Index - Unified SQLite DB.md)
