---
summary: "Defines Go code anchors for the cache subsystem so callers (via call-sites) automatically pull the right docs in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/cache]
code-anchors:
  go:
    - label: cache-service-api
      symbol: github.com/atomicobject/rhizome/pkg/vault/cache.Service
    - label: cache-new-service
      symbol: github.com/atomicobject/rhizome/pkg/vault/cache.NewService
    - label: cache-note-adapter
      symbol: github.com/atomicobject/rhizome/pkg/vault/cache.NewNoteAdapter
---

# Go anchor - Cache subsystem

Use this note as the **single source of truth** for Go code anchors related to the cache subsystem.

## Read these first

![[vault-core]]
![[Cache (Hub)]]
![[Vault cache service (Service)]]
![[Dirty tracking + Refresh semantics]]
![[Watcher design + degraded mode]]
![[AnalysisCache (backlinks + graph memoization)]]

## Related

![[Code reference scanning in cache]]
![[Ignore + exclude rules]]
![[Debugging cache staleness + watcher issues]]
