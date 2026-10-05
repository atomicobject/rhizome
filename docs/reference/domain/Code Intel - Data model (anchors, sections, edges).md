---
type: ReferenceDoc
summary: "Canonical handle model for code-intel: anchors, doc sections, chunks, and the typed edge set that connects them."
reference-kind: architecture
derived-from:
  - docs/reference-notes/Code Intel - Data model (anchors, sections, edges).md
  - docs/reference/analysis/subsystem-backport-wave-1-review.md
last-verified: 2026-04-12
status: active
---

# Code Intel - Data model (anchors, sections, edges)

## Summary

The code-intel spine exposes a small set of stable handles and typed edges so retrieval can stay deterministic.

## Canonical handles

- `anchor_id`: a code entity with a span in a file
- `section_id`: a heading-based doc section, or whole-file fallback
- `chunk_id`: a content chunk with provenance and ordering

## Stored shape

- Internal row ids are join accelerators only.
- Read paths synthesize external handles at the boundary.
- Unresolved edges are rebuilt, not persisted as partial state.

## Edge kinds

- `defines`
- `mentions`
- `calls`
- `tests`

`mentions` is the primary docs-to-code attachment signal. Coderefs and code anchors are inputs or fallbacks that should produce `mentions` edges, not replace them.

## Reverse refs

Reverse symbol refs are a derived index, not part of the public handle spine. They should preserve authoritative owner identity even when parse or delta churn leaves a symbol row missing.

## References

- [[semantic-code-index-spine]]
- [[Code Index - Unified SQLite DB]]
- [[Code Intel - IDs + path normalization]]
