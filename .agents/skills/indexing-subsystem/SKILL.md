---
name: indexing-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/app/indexing, pkg/app/indexingpipe, pkg/vault/indexlock, pkg/indexingperf, or pkg/vault/watchhub. Loads the indexing subsystem design constraints and review checklist before edits.
---

# Indexing subsystem

## Goal

Change or review indexing code without regressing end-to-end latency, write serialization, or cross-process coordination.

Read `docs/reference/subsystems/indexing.md` first and treat its constraints as normative. For deeper context follow its Related docs links ([[Indexing pipeline (Hub)]], [[indexing-pipeline-architecture]]).

## Load-bearing rules

1. **Optimize for minimum end-to-end latency.** Overlap parse/embed/provider work; never add a separate full pass when work can join an existing pipeline stage (`pkg/app/indexing/doc.go`).
2. **All durable SQLite writes go through the single queued writer lane** (`queuedWriter` in `pkg/app/indexing/write_queue.go`). New write kinds get a `Submit*` method, a batch type, and entries in both `flushAll` and `flushExpired`. No direct store writes from workers.
3. **Batch queries and writes.** Use the per-kind rows/bytes/idle flush policies; bulk transactions inside handlers, not autocommit-per-row.
4. **Keep correctness barriers explicit and ordered.** `FlushAndWait` before cross-file work; ontology deltas drain before ontology node read-model writes (FullRebuild truncate hazard). Do not add or remove barriers casually.
5. **Guard concurrent indexing with `.rhizome/index.lock`** (`pkg/vault/indexlock`): non-blocking `TryAcquire` + heartbeat, stale-PID reclaim, `RequestPriority` for interactive preemption — never delete a live lock. Background work (incl. MCP-boot full index in `cmd/runtime_helpers.go`) uses yielding heartbeats.
6. **Bounded backpressure only.** No unbounded channels, goroutine fanout, or walk-ahead beyond worker capacity (`pkg/app/indexingpipe`).
7. **Vault-root-relative paths only** in index rows and watch events; normalize through `pkg/paths`.
8. **Instrument honestly.** Attribute new work via `indexingperf` phase/op context; deferred writes report as deferred, not as enqueue blocking.

## Pre-handoff checklist

- [ ] No new write path bypasses the writer lane; no per-row autocommit loops.
- [ ] New work is inside a pipeline stage, lock-guarded, and backpressured.
- [ ] Barrier ordering unchanged or change is justified in the note/spec.
- [ ] Watcher/MCP handshake never blocked by new synchronous init.
- [ ] Tests added beside the change; run:
  `go test ./pkg/app/indexing/... ./pkg/app/indexingpipe/... ./pkg/vault/indexlock/... ./pkg/vault/watchhub/... ./pkg/indexingperf/...`
  and `go test -race -tags=integration ./...` for pipeline changes.
- [ ] `gofmt` + `go vet ./...` clean; `CONTEXT.md` and `docs/reference/subsystems/indexing.md` updated if invariants moved.
