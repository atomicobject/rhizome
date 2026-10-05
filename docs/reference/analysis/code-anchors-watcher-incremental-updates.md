---
type: ReferenceDoc
summary: "How WatchHub-driven events keep code-anchor ingest, call edges, and scope recomputation current during live runs."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Code anchors - watcher + incremental updates.md
last-verified: 2026-04-12
status: active
aliases:
  - Code anchors - watcher + incremental updates
---

# Code anchors - watcher + incremental updates

## Summary

`codeanchor.Watcher` consumes WatchHub events for note roots and code roots, then schedules incremental ingest and deferred scope recompute.

## Live update flow

- WatchHub can source events from fsnotify or leader/follower hints.
- Watcher batches bursts with a debounce timer.
- Scope recompute is scheduled, not immediate, so rapid edits coalesce.
- When a code file changes, `IndexCodeFile` rebuilds call edges for that file only.
- During live watching, a full `RebuildAllCallEdges` is not required.

## Exclusions and failures

- Default excluded directories include `node_modules`, `vendor`, `dist`, `build`, and `__pycache__`.
- Unsupported language extensions are skipped.
- Failed files keep retry metadata so the watcher can revisit them later.

## Related

- [[Code anchors (Hub)]]
- [[code-anchors-matching-scopes]]
- [[code-anchors-frontmatter-syntax]]
