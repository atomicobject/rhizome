---
summary: "Defines Go code anchors for the unified index DB and code-intel spine so callers automatically pull the right docs in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/intel, subsystem/embeddings]
code-anchors:
  go:
    - label: codeanchor-service
      symbol: github.com/atomicobject/rhizome/pkg/anchors.Service
    - label: codeanchor-sqlite-store
      symbol: github.com/atomicobject/rhizome/pkg/anchors/sqlite.Store
    - label: embeddings-sqlite-store
      symbol: github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite.Store
    - label: embeddings-indexer
      symbol: github.com/atomicobject/rhizome/pkg/search/embeddings.NewIndexer
---

# Go anchor - Code Index (unified DB + intel)

Use this note as the **single source of truth** for Go code anchors related to:

- the unified `.rhizome/db.sqlite` index file
- code-intel tables (`intel_*`) and their lifecycle
- semantic embeddings tables (`emb_*`) inside the same DB

## Read these first

![[stores]]
![[Indexing pipeline (Hub)]]
![[Code Index (Hub)]]
![[Code Index - Unified SQLite DB]]
![[semantic-code-index-spine]]
![[Code Intel - Data model (anchors, sections, edges)]]
![[Intel Spine - Current State]]
![[Indexing pipeline - Live updating (watcher runtime)]]

## Related

![[Code anchors (Hub)]]
![[Embeddings (Hub)]]
![[Watcher design + degraded mode]]
![[Dirty tracking + Refresh semantics]]
