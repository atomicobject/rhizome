---
type: ReferenceDoc
summary: "Write-serialization, batching, and cross-process coordination rules for indexing and embeddings."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Indexing pipeline - Concurrency + batching requirements.md
last-verified: 2026-04-12
status: active
---

# Indexing pipeline - Concurrency + batching requirements

## Summary

Rhizome uses parallel parse and embed work, but durable writes still flow through explicitly serialized lanes because SQLite WAL still commits one writer at a time.

## Contracts

- keep per-path write transactions small
- use real bulk transactions for high-cardinality metadata/index rows; batching at the queue is not enough if store internals still execute one autocommit per row
- share write mutexes across stores that target the same DB
- coordinate cross-process writers with `.rhizome/index.lock`
- let interactive `rzm index` preempt best-effort background work through priority requests
- prefer bounded worker pools and bounded write queues over unbounded fanout
- checkpoint after heavy write phases rather than letting WAL sidecars grow indefinitely

## Related

- [[Embeddings - SQLite store + locking hazards]]
- [[Indexing pipeline - Performance tradeoffs + guardrails]]
