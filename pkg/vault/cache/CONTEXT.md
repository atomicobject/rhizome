## pkg/vault/cache

Hot in-memory cache of provider-projected vault files with incremental freshness
via WatchHub events.

- **Entry points**: `NewService`, `EnsureReady`, `Refresh`, `Entry`
- **Selection invariant**: the cache is a provider projection, not note ownership authority. `SelectionPolicy` controls discovery, canonical vault-relative admission, and user excludes. A configured `noteformat.Runtime` projects selected sources; descriptor-only or non-current projections are omitted before syntax facts are read. The no-runtime path is a named Markdown-only compatibility adapter. `ReplaceSelectionPolicy` atomically installs all three and marks the cache stale so the next refresh evicts rejected paths and discovers selected paths. `RefreshWithResult` exposes raw drained watcher paths separately from cache-content changes.
- **Key invariant**: callers must call `Refresh()` before live reads; `Version()` bumps on changes for downstream memoization. Initial crawl and dirty refresh use the same visibility contract: exact `CONTEXT.md` bypasses ordinary note selection but never hard ignore/infrastructure boundaries.
- **Readers never wait on a stale-triggered recrawl.** Only the first crawl gates readers (`EnsureReady`). The `Refresh()` that observes `stale` starts one background `initialCrawl` (single-flight; `MarkStale` during a crawl re-arms `stale` so another follows) and returns the current index; dirty batches keep applying while it runs. The crawl reconciles the live index in place with `ready` left true: discovery is authoritative (entries it no longer returns are evicted first), every discovered file is re-read regardless of its cached size/mtime (a matching stat tuple is exactly what a missed event looks like), an entry a concurrent dirty refresh committed with a newer mtime wins over the crawl's read, and vanished code files are dropped by `scanCodeFiles`. Concurrent reads observe the last-good entry or the freshly upserted one, never an empty index. `RefreshResult.Resynced` reports the crawl's completion on the next `Refresh()`, not its start, so watchers force a full reconcile only once the index has caught up.

- **Shutdown drains owned recrawls.** `Close` cancels the service lifetime, prevents further background recrawl admission, and waits for admitted recrawls to finish their state updates. Callers still own synchronous `EnsureReady` and `Refresh` calls and must drain those before releasing their dependencies.

### Deep docs

- [[Cache (Hub)]]
- [[Watcher design + degraded mode]]
