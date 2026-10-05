---
summary: "Code anchors for the notemeta raw-note fact indexer (full/incremental sync, persisted graph snapshots, note source snapshots) so consumers pull the subsystem guidance in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/notemeta]
code-anchors:
  go:
    - label: notemeta-ensure-indexed
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.Indexer.EnsureIndexed
    - label: notemeta-sync-paths
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.Indexer.SyncPaths
    - label: notemeta-load-persisted-graph-snapshot
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.Indexer.LoadPersistedGraphSnapshot
    - label: notemeta-load-note-source-snapshots
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.Indexer.LoadNoteSourceSnapshots
    - label: notemeta-build-note-source-snapshots
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.Indexer.BuildNoteSourceSnapshots
    - label: notemeta-source-snapshot
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.NoteSourceSnapshot
    - label: notemeta-content-only-snapshot
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.NewContentOnlyNoteSourceSnapshot
    - label: notemeta-load-alias-map
      symbol: github.com/atomicobject/rhizome/pkg/notemeta.LoadAliasMap
---

# Go anchor - Notemeta index

Code anchors for `pkg/notemeta`, the canonical raw-note fact indexer: provider-projected authored sources into durable SQLite rows, freshness gates, and the `NoteSourceSnapshot` source contract for downstream domains.

## Read these first

![[notemeta]]
