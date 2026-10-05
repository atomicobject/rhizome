---
summary: "Runbook for diagnosing stale cache data, WatchHub failures, ignore drift, and bursty filesystem events; includes quick repro and what to inspect."
tags: [type/runbook, subsystem/cache]
---

# Debugging cache staleness + watcher issues

### What this runbook is for

Use this when cache results feel stale, watch events appear to be missed, or resyncs are happening too often.

### Common symptoms → likely causes

- **Cache missing recent edits**
  - likely: watch events not reaching cache; check WatchHub subscriptions and `Refresh()` checkpoints
- **Frequent full resyncs**
  - likely: WatchHub stale signals (overflow, ignore changes, directory renames)
- **Leader/follower mismatch**
  - likely: follower hint log is not being tailed or leader lock is flapping

### What to inspect in code

- WatchHub wiring:
  - `cmd/serve.go` / `cmd/web.go` setup of `watchhub.NewHub`, roots, and subscriptions
  - `pkg/vault/cache/watchhub.go` event → dirty mapping
- Cache refresh logic:
  - `pkg/vault/cache/service.go` `Refresh()` / background `recrawl()` → `initialCrawl()` paths

### Quick repro checklist

- Create/modify/delete a markdown file and call `Refresh()` in a tight loop.
- Rename a directory to ensure a stale+resync path is triggered.
- Touch `.rhizome/ignore` to force ignore reload + stale.
- In leader/follower mode, verify hint log writes by the leader and tailing by the follower.

### Integration tests

- `go test -tags=integration ./pkg/vault/cache/...`
  - Uses WatchHub + real fsnotify events; timing-sensitive on CI.

### WatchHub → stale triggers

- watcher errors / channel closure
- overflow in pending add queue
- ignore-file events
- directory rename
- leader/follower resync hints

### Related

- [[Watcher design + degraded mode]]
- [[Dirty tracking + Refresh semantics]]
- [[Ignore + exclude rules]]
