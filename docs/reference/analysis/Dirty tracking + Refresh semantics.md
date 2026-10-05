---
type: ReferenceDoc
summary: "Explains the dirty-set state machine and how Refresh and resync turn noisy watcher signals into deterministic cache updates."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Dirty tracking + Refresh semantics.md
last-verified: 2026-04-12
status: active
---

# Dirty tracking + Refresh semantics

## Summary

Rhizome treats file-system events as hints, not truth. The cache uses a dirty map from `path -> DirtyKind` as the boundary between asynchronous watcher noise and synchronous reads. `Refresh()` drains that map and applies deterministic updates so callers do not need to reason about raw fs event ordering.

## Core state machine

- `created`: new file or directory appeared
- `modified`: existing path changed
- `removed`: deleted path
- `renamed`: old path after rename; the new path is discovered by parent rescan
- `recreated`: delete-plus-create burst collapsed into one state

Coalescing matters:

- `removed -> created/modified` becomes `recreated`
- `recreated -> removed` collapses back to `removed`
- `removed` stays sticky until recreation is detected

## Refresh contract

- if the cache is not ready, `Refresh()` delegates to readiness/bootstrap
- dirty state is snapshotted under lock, then cleared
- if `stale == true`, Rhizome performs a full resync instead of trying incremental repair
- otherwise each dirty path is handled with path-local replace semantics

Path-local behavior:

- removed or renamed paths remove stale rows and may rescan the parent
- recreated paths remove stale state, then re-enter the created path flow
- created or modified directories rescan the directory
- created or modified markdown files re-read and re-index that file
- created or modified code files refresh code refs and related per-file state when enabled

## Invariants

- `Service.Version()` is the invalidation key for derived caches
- version bumps happen on initial crawl completion, applied dirty changes, and full resync completion
- incremental refresh should stay per-path and deterministic; correctness-critical ambiguity escalates to stale/resync
