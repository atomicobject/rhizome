---
type: ReferenceDoc
summary: "Describes WatchHub event handling, stale/resync fallback, and degraded-mode behavior when watcher fidelity drops."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Watcher design + degraded mode.md
  - docs/reference-notes/Code Intel - Indexing + watcher integration.md
last-verified: 2026-09-17
status: active
---

# Watcher design + degraded mode

## Summary

Rhizome treats watcher infrastructure as a best-effort signal source. WatchHub produces dirty and stale hints, but the cache remains the source of truth. When watcher fidelity drops, Rhizome degrades toward deterministic resync instead of trying to preserve a false sense of incrementality.

## WatchHub contract

- WatchHub emits create, write, remove, and rename events plus stale signals
- cache subscribers translate those signals into `DirtyKind` values or `stale=true`
- ignore-file changes, directory renames, overflow, and backend failures can all force stale mode

## Backend and process model

- macOS prefers FSEvents
- fsnotify fallback is allowed only when watch-count pressure is still acceptable
- only the vault runtime that won `.rhizome/runtime.lock` watches the filesystem; other Rhizome processes for the same vault are one-shot readers or exit at election ([[vault-runtime]])

## Degraded-mode rules

- backend reliability failures coalesce into bounded stale reasons to avoid hot loops
- correctness-critical reasons such as ignore changes and directory renames bypass the coalescing shortcuts
- once stale, the next `Refresh()` performs full resync instead of incremental patch-up

## Code-intel and live update implications

- note saves rebuild doc-section and mention-edge state for that path
- code saves rebuild code-anchor and edge state for that path
- embeddings and graph-score maintenance are scheduled asynchronously on top of the same watcher-driven dirtiness model

## Practical takeaway

The watcher path is optimized for near-instant local updates when signals are trustworthy, but the design prefers visible degradation and full correction over quiet drift.
