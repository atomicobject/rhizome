---
type: ReferenceDoc
summary: "Operational notes for the unified `.rhizome/db.sqlite` store: what lives there, how rebuilds split, and which locking and throughput rules matter."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Code Index - Unified SQLite DB.md
  - docs/reference/analysis/subsystem-backport-wave-1-review.md
last-verified: 2026-04-12
status: active
---

# Code Index - Unified SQLite DB

## Summary

Rhizome keeps semantic embeddings, code index data, and code-intel tables in one SQLite file so joins stay cheap and rebuilds stay coherent.

## Current layout

- `emb_*`: note and code embedding tables
- `code_*`: code embedding tables and FTS
- `intel_*`: code-intel anchors, sections, edges, and chunks

## Operational constraints

- SQLite WAL allows many readers but only one writer commit at a time.
- All openers and the WAL sidecars must remain in one host/VM on a local filesystem; host/container split access through a Docker bind or passthrough mount is unsupported.
- Heavy write paths must serialize through one writer lane.
- Per-open txlock configuration is preferred over runtime mutation.
- In-place domain rebuilds remain supported. Full, corrupt, or incompatible replacement must fail closed and preserve DB/WAL/SHM until an operator stops every Rhizome process and moves the files aside; the current index lock is not a lifecycle lock held by every opener.

## Throughput notes

- Batch writes should stay grouped in larger transactions.
- Queue-backed indexing beats per-item write fanout.
- Explicit checkpointing matters after heavy maintenance.

## Why this note exists

This is the current-state architecture note for the unified store. The stable state model lives in the domain notes; this note captures the practical DB behavior that operators and implementers need.

## References

- [[Code Intel - Data model (anchors, sections, edges)]]
- [[semantic-code-index-spine]]
- [[Indexing pipeline - Concurrency + batching requirements]]
