---
type: ReferenceDoc
summary: "Authority note for `cache.Service`: lifecycle, stored shape, refresh semantics, and safe-change boundaries."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Vault cache service (Service).md
last-verified: 2026-04-12
status: active
---

# Vault cache service (Service)

## Summary

`cache.Service` maintains the in-memory note view: content, derived metadata, dirtiness, ignore rules, and refresh-driven reconciliation with disk.

## Contracts

- `EnsureReady` performs the initial crawl before steady-state refresh use
- watch events become coalesced dirty markers, not immediate full reindexes
- stale mode triggers full resync on next refresh
- `EntriesSnapshot` is the safe refresh-aware read boundary
- derived-cache invalidation flows through the service version counter

## Related

- [[Dirty tracking + Refresh semantics]]
- [[Watcher design + degraded mode]]
- [[Ignore + exclude rules]]
