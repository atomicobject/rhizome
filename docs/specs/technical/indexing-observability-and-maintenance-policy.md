---
type: TechnicalSpec
summary: "Defines the indexing observability and SQLite maintenance contract for progress labels, --timings metrics, DB-write diagnostics, and vacuum/analyze/optimize/checkpoint policy."
id: SPEC-0047
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0047
  - indexing-observability-maintenance-policy
  - indexing-observability-and-maintenance-policy
---

# Indexing observability and maintenance policy

## Summary

Indexing observability has two audiences: users need calm progress and enough `--timings` detail to explain slow or stale runs, while maintainers need phase, queue, provider, and SQLite diagnostics that map to real pipeline ownership. Post-index maintenance is part of the same contract: cheap health work runs routinely after writes, but full `VACUUM` is reserved for explicit repair or proven reclaimable space.

This spec narrows [[indexing-workflow]] and [[indexing-pipeline-architecture]] for timing/progress/metrics/maintenance behavior. Use [[Indexing pipeline - Phase ownership matrix]] for phase ownership, [[Indexing pipeline - Performance tradeoffs + guardrails]] for performance tuning guardrails, and [[Go anchor - Indexing pipeline (rzm index)]] for the code anchors that should surface this contract from indexing implementation files.

## Goals

- keep normal progress output stable, low-noise, and user-facing
- make `rzm index --timings` diagnostic enough to localize slow phases, queue waits, provider underfill, and DB-write hotspots
- preserve phase labels that correspond to real pipeline barriers rather than broad wrappers
- make unlabeled SQLite writes visible as defects in instrumentation coverage
- keep routine SQLite maintenance cheap and predictable
- gate full `VACUUM` on explicit user intent or meaningful freelist evidence

## Non-Goals

- making every internal metric a stable product API
- printing high-cardinality metric streams during normal indexing
- replacing profiling tools for deep performance investigations
- exposing SQLite administration as a general user-facing command set
- using elapsed time, WAL size, or historical vacuum breadcrumbs as automatic full-vacuum triggers

## User Stories

### US1 - See progress labels that explain current work without exposing every internal queue, worker, or metric
- id:: ^SPEC-0047-US1
- summary:: See progress labels that explain current work without exposing every internal queue, worker, or metric.
- status:: ready

#### Acceptance Criteria

- Progress labels use user-facing domains, not implementation-only metric names.
  verification:: Run `rzm index` and inspect progress labels for code ingest, note ingest, ontology primary-chunk sync, graph, saving state, optimizing, and optional vacuum.
- Non-verbose progress suppresses routine diagnostic chatter while still surfacing warnings.
  verification:: Run a clean index without `--timings` and confirm routine phase chatter is hidden while warnings still print through the progress writer.
- Progress must remain monotonic even when later finalization discovers dynamic call-edge or scope work.
  verification:: Run `go test ./pkg/app/indexing -run 'TestIntegratedProgress|TestFixedTotalFileProgress|TestFinalDrainProgress'`.

### US2 - Use `--timings` to locate whether a slow run is discovery, planning, provider calls, queue wait, DB lock hold, graph, or maintenance
- id:: ^SPEC-0047-US2
- summary:: Use `--timings` to locate whether a slow run is discovery, planning, provider calls, queue wait, DB lock hold, graph, or maintenance.
- status:: ready

#### Acceptance Criteria

- Unified timing summaries list named phases for real work and correctness barriers.
  verification:: Run default unified `rzm index --timings`; confirm labels include open/config/store work, code/note ingest, ontology planning/write phases, semantic planning/embed phases, graph, routine maintenance, optional vacuum, and total.
- Provider diagnostics separate logical requested texts, provider calls/texts, batch fill, bytes fill, mixed batches, gate wait, queue age, and future wait where the shared embedding node observes them.
  verification:: Inspect `pkg/search/semantic/shared_embedding_node.go` metrics and run `go test ./pkg/indexingperf`.
- Writer diagnostics separate enqueue wait, code-vs-other queue wait, deferred polling, writer busy/idle time, queue depth, batch rows/bytes, DB wait, and DB hold.
  verification:: Inspect `pkg/app/indexwriter/writer.go` and `pkg/anchors/sqlite/store.go`; run `go test ./pkg/app/indexing ./pkg/indexingperf ./pkg/anchors/sqlite`.
- Unlabeled SQLite writes are still reported as `auto.*` diagnostic operations so missing `indexingperf.WithOp` coverage is visible.
  verification:: Run `go test ./pkg/indexingperf -run TestCollectorRenderSummary` and inspect `ObserveDBWrite` behavior.

### US3 - Keep SQLite maintenance cheap by default and make expensive full vacuuming evidence-driven
- id:: ^SPEC-0047-US3
- summary:: Keep SQLite maintenance cheap by default and make expensive full vacuuming evidence-driven.
- status:: ready

#### Acceptance Criteria

- Routine post-index maintenance always attempts `ANALYZE`, `PRAGMA optimize`, and a truncating WAL checkpoint after writes have flushed.
  verification:: Inspect `runUnifiedPostIndex`; it attempts routine maintenance after writer flush/config persistence and before successful handoff.
- Automatic full `VACUUM` only runs when SQLite freelist stats show at least 64 MiB reclaimable, or at least 16 MiB reclaimable with at least 20 percent freelist ratio.
  verification:: Run `go test ./pkg/app/indexing -run TestVacuumStatsNeedReclaim`.
- Manual `rzm index --vacuum` remains an unconditional full-vacuum override.
  verification:: Inspect CLI plumbing from `cmd/index.go` into unified indexing options and confirm `opts.Vacuum` bypasses the auto-vacuum predicate.
- Failed maintenance operations warn and do not hide the indexing work that already completed.
  verification:: Inspect `runUnifiedPostIndex`; `VACUUM`, `ANALYZE`, `PRAGMA optimize`, and checkpoint errors print warnings instead of replacing successful index results.

## Requirements

### Progress

- Progress labels MUST describe user-facing domains: indexing code, ingesting notes, ontology refresh, semantic/provider work, graph, saving index state, optimizing, optional vacuum, and completion.
- Progress updates MUST be monotonic across integrated progress segments.
- Finalization progress MAY add dynamic work units for call-edge and scope recomputation, but it MUST NOT move the visible bar backward.
- Non-verbose progress MUST suppress routine detail and preserve warnings.
- Verbose progress MAY surface additional lines through the same progress writer used by `--timings`.

### Timings and Metrics

- `rzm index --timings` MUST install one `indexingperf.Collector` on the command context and close it with a final `total` span.
- Rolling timing windows MAY be emitted while indexing is running; final summaries MUST include the cumulative phase summary.
- Repeated stable metric families and their public ordering/prefix metadata MUST come from one immutable descriptor catalog consumed by both rolling-window and cumulative-summary rendering. An integrated byte-exact regression MUST pin the combined output contract.
- Stable phase labels MUST map to actual pipeline phases or barriers. Do not rename them casually; downstream docs and diagnostics rely on them.
- New high-cost work MUST either live under an existing truthful phase or add a named phase with `runPhase` / `indexingperf.WithPhase`.
- Provider-backed work MUST report provider calls/texts/latency and should report capacity/fill/gate/wait diagnostics when using the shared embedding node.
- High-volume queued writeback MUST report queue wait/depth, writer busy/starved/deferred, batch rows/bytes, and DB wait/hold where applicable.
- SQLite write helpers SHOULD set explicit `indexingperf.WithOp` labels before acquiring write locks.
- Auto-generated `auto.*` DB-write labels are diagnostic fallback only. A repeated `auto.*` line in `--timings` SHOULD be treated as missing instrumentation and replaced with a stable op label.
- Unified timing output MUST separate routine maintenance (`analyze_optimize_checkpoint`) from full reclaim (`vacuum`).
- Code-only timing output MUST keep maintenance operations visible at least as explicit write-locked DB ops (`intel.analyze`, `intel.optimize`, `intel.vacuum`). If code-only indexing gains phase spans for maintenance, it SHOULD reuse the unified phase names rather than invent route-specific labels.

### SQLite Maintenance

- Routine post-index maintenance MUST run after indexing writes and queued writer flushes complete.
- Routine maintenance MUST attempt `ANALYZE`, `PRAGMA optimize`, and a truncating WAL checkpoint.
- Routine maintenance failures SHOULD warn rather than fail the completed indexing run.
- Unified indexing owns progress-stage labels for "Optimizing index" and optional "Vacuuming index"; code-only indexing owns plain `[code]` maintenance lines unless/until it adopts the integrated progress bar.
- Full `VACUUM` MUST NOT run on every index.
- Manual `--vacuum` MUST run full `VACUUM` regardless of freelist stats.
- Automatic full `VACUUM` MUST be gated by `Store.VacuumStats` using SQLite `page_count`, `freelist_count`, and `page_size`.
- Automatic full `VACUUM` MUST require at least 64 MiB reclaimable, or at least 16 MiB reclaimable with at least 20 percent freelist ratio.
- WAL size, elapsed time, and `.vacuum_state` breadcrumbs MUST NOT be automatic full-vacuum triggers.
- `Store.VacuumStats` MUST compute `ReclaimableBytes` from `freelist_count * page_size`; callers should not guess file size from the filesystem.
- `VACUUM` warnings MUST be visible because it can need exclusive access and can be expensive.

### Code Binding

- Timing setup code MUST link to this spec because it defines the stable meaning of `--timings`.
- Maintenance policy code MUST link to this spec and retain the threshold rationale near `vacuumStatsNeedReclaim`.
- SQLite maintenance helpers SHOULD document the cheap-vs-expensive distinction locally, with this spec carrying the durable policy.

## Open Questions

- whether a future `rzm index explain` command should turn `--timings` into specific next-step recommendations
- whether maintenance should expose reclaimable bytes/ratio in `--timings` before deciding to skip automatic `VACUUM`
- Top-level code/semantic modes are retired; `rzm index --timings` uses the unified phase spans for all configured work.
- whether recurring `auto.*` DB-write labels should become an explicit validation warning
