---
type: ReferenceDoc
reference-kind: architecture
summary: "Use bounded queues and visible backpressure when indexing producers outrun SQLite, instead of adding more concurrent writer paths."
status: active
decision-domain: architecture
tags: [type/decision, subsystem/indexing, subsystem/performance, subsystem/intel, subsystem/embeddings]
derived-from:
  - docs/decisions/Use bounded backpressure in indexing instead of adding writer concurrency.md
last-verified: 2026-04-12
---

# Use bounded backpressure in indexing instead of adding writer concurrency

## Context

In large indexing runs, read, parse, and embed stages can outrun durable writes. At that point the system has two options:

- accept bounded waiting and let pressure propagate upstream
- hide the pressure with more writer concurrency, larger piles of in-flight state, or unbounded queue growth

Only the first option keeps the bottleneck honest.

## Decision

Use bounded queues and allow backpressure to slow upstream producers when the writer lane becomes the bottleneck.

This means:

- queue capacity stays finite
- urgent escape hatches are explicit, not accidental
- queue wait and writer-busy metrics are treated as design-level signals
- the preferred fix for regressions is better batching, overlap, or staging, not more concurrent writers

## Consequences

- Slow SQLite commits become visible quickly instead of being hidden behind growing memory use.
- The system can protect downstream consumers from uncontrolled write amplification.
- Some producer blocking is expected behavior during hot runs, not automatically a bug.
- Performance analysis stays grounded in real pressure points rather than synthetic concurrency gains.

## Follow-ups

- [Indexing pipeline architecture](../technical/indexing-pipeline-architecture.md)
- [Use a single queued writer lane for SQLite-backed indexing writes](Use a single queued writer lane for SQLite-backed indexing writes.md)
- [Indexing pipeline - Concurrency + batching requirements](../../reference-notes/Indexing pipeline - Concurrency + batching requirements.md)
- [Indexing pipeline - Performance tradeoffs + guardrails](../../reference-notes/Indexing pipeline - Performance tradeoffs + guardrails.md)
