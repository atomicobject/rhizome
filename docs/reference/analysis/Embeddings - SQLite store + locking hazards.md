---
type: ReferenceDoc
summary: "Operational constraints for the SQLite-backed embeddings store: metadata validation, vec mirrors, WAL safety, and write contention."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Embeddings - SQLite store + locking hazards.md
last-verified: 2026-04-12
status: active
---

# Embeddings - SQLite store + locking hazards

## Summary

The embeddings store is SQLite-backed, WAL-based, and safe only when write paths stay serialized and rebuild flows avoid corrupting active sidecars.

## Contracts

- provider/model/dimension mismatches require safe rebuild rather than mixed-state reuse
- vec mirror tables are query-time search inputs; relational embedding blobs are transitional storage
- long-running write paths must serialize through store write locks
- tx-lock policy should be injected at open time, not mutated globally at runtime
- rebuild flows must never remove WAL/SHM sidecars while another process still has the DB open

## Recovery notes

- embeddings-domain repair should be attempted before full DB removal
- expensive embeddings should be preserved when the store can self-heal safely

## Related

- [[Embeddings - indexing pipeline]]
- [[Indexing pipeline - Concurrency + batching requirements]]
