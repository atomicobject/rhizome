---
name: stores-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/sqliteutil, or adding any new SQLite reads/writes elsewhere. Loads single-writer, batching, and migration constraints with review checklist.
---

# Stores subsystem

## Goal

Change or review SQLite store code without violating the layered single-writer discipline, batching policy, or migration contract that keeps the unified `.rhizome/db.sqlite` index correct under concurrency.

## First move

Read `docs/reference/subsystems/stores.md` first; its constraints are normative for this skill. Use its "Review checklist — problems to catch" section verbatim when reviewing diffs.

## Rules

1. **All durable writes go through the writer lane.** Inside indexing, submit through `queuedWriter` (`pkg/app/indexing/write_queue.go`, via `codeintel.WithWriteQueue`); inside a store, wrap writes in `withWrite`/`withWriteTx`. Never `db.Exec` a write from a new side path.
2. **One shared write mutex per DB file.** A new store handle on an existing DB must get `SetWriteMu(&sharedWriteMu)` wired (pattern: `pkg/app/bootstrap/live.go`, `pkg/app/indexing/unified.go`).
3. **Batch by default.** Store write APIs take slices and persist them in one transaction; new queue write kinds need a rows/bytes/idle flush policy and entries in both `flushAll` and `flushExpired`.
4. **Keep write transactions short.** No provider calls, file I/O, or network inside `withWriteTx` — the shared mutex blocks every other store while held.
5. **Schema changes are migration steps.** Add a `Step` to the right domain plan in `pkg/sqliteutil/migration/domains/`, bump `Target`, extend `Validate`. No DDL outside `migration.EnsureDomain` for migrated tables.
6. **Open connections only via `sqliteutil.OpenDSN(DSNWithOptions(...))`** (or `sqlstore.OpenDB`) so WAL/pragmas/mmap/sqlite-vec stay uniform. Never import `mattn/go-sqlite3` directly in new code.
7. **FTS5 needs the build tag and the sanitizer.** Build/test with `-tags "fts5"`; pass user text to `MATCH` only through `sqliteutil.FTS5QueryFromText`.
8. **Vault-root-relative keys only.** Normalize through `pkg/paths` strict helpers before any value becomes a SQLite key.
9. **Write-capable opens hold the schema init lock.** A new open path that creates or migrates schema (own pool or shared pool) calls `sqliteutil.LockSchemaInit`/`LockSchemaInitForDB` before its first connection and releases after validation; two processes open a fresh vault database at once whenever a command auto-starts the runtime.

## Pre-handoff checklist

- [ ] New writes routed through the writer lane / `withWrite*`; no direct write paths added
- [ ] Schema changes shipped as migration steps with `Target` bump and `Validate` coverage
- [ ] Batching in place for any loop that writes rows
- [ ] `go test -tags "fts5" ./pkg/sqliteutil/...` green
- [ ] `go test -tags "fts5" -race ./pkg/sqliteutil/...` green (plus affected store packages, e.g. `./pkg/anchors/sqlite/...`)
- [ ] Full gate before commit per AGENTS.md (`make check`)
- [ ] `docs/reference/subsystems/stores.md` updated if constraints changed (refresh `last-verified`)

## Output

- summary of what changed and which writer-lane/batching/migration rules it touches
- test commands run and results
- any flagged deviation from the constraints above, with rationale
