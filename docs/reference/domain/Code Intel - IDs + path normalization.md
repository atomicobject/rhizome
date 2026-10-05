---
type: ReferenceDoc
summary: "Stable path and ID rules for code-intel: vault-root-relative paths, PathRef boundaries, and move-safe hashing inputs."
reference-kind: architecture
derived-from:
  - docs/reference-notes/Code Intel - IDs + path normalization.md
  - docs/reference/analysis/subsystem-backport-wave-1-review.md
last-verified: 2026-04-12
status: active
---

# Code Intel - IDs + path normalization

## Summary

Code-intel IDs stay stable only when every persisted path is normalized the same way at every boundary.

## Contract

- Persisted paths must be vault-root-relative, slash-normalized, and cleaned.
- Absolute paths belong only to filesystem I/O.
- `pkg/paths.PathRef` is the canonical boundary type.
- Only `PathRef.Rel` may flow into storage, IDs, FQNs, or cache keys.

## Practical rules

- Use `VaultPaths.RelStrict`, `RelNoteStrict`, or `RelCodeStrict` for storage-side validation.
- Use `ResolveNoteRef` and `ResolveCodeRef` at subsystem entrypoints.
- Keep root context separate from the base ref when deriving FQNs.
- Treat IDs as stable-ish across formatting changes, not immutable across large renames.

## ID inputs

- `anchor_id`: `{lang, kind, relPath, stableKey}`
- `section_id`: `{relPath, breadcrumbSlug, start_byte}`
- `chunk_id`: `{owner_id, ord, granularity}`

## References

- [[Code Intel - Data model (anchors, sections, edges)]]
- [[Code Index - Unified SQLite DB]]
- [[PathRef contract]]
