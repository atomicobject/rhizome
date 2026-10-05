---
summary: "Defines Go code anchors for watcher/eventing behavior (WatchHub + fsnotify backend) and embeds the key watcher docs."
tags: [type/reference, subsystem/codeanchor, subsystem/cache]
code-anchors:
  go:
    - label: fsnotify-watcher
      symbol: github.com/atomicobject/rhizome/pkg/vault/watchhub.newFSNotifyBackend
    - label: codeanchor-new-watcher
      symbol: github.com/atomicobject/rhizome/pkg/anchors.NewWatcher
---

# Go anchor - Watchers (WatchHub + fsnotify)

Use this note as the **single source of truth** for Go code anchors related to filesystem watching and incremental updates.

## Read these first

![[indexing]]
![[Watcher design + degraded mode]]
![[Dirty tracking + Refresh semantics]]
![[code-anchors-watcher-incremental-updates]]
![[code-anchors-matching-scopes]]
