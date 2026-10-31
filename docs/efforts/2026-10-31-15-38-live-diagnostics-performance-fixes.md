---
type: EffortNote
id: EFF-2026-10-31-15-38
aliases: [EFF-2026-10-31-15-38]
name: Live diagnostics performance fixes
created-at: 2026-10-31T19:39:10Z
status: complete
summary: "Fix the slowdowns and diagnostic noise found in two days of live runtime diagnostics: idle watcher ticks, health-probe logging, double resyncs, unbounded embed-cycle fan-out, false perf flags, and desktop sidecar polling."
governing-specs:
  - "[[persistent-diagnostics]]"
---

# Live diagnostics performance fixes

## Scope

On 2026-10-31, Drew asked for a review of the performance data from running Rhizome, then for a council-refined plan and fixes. The evidence came from `.rhizome/diagnostics` in the main checkout, `drews-vault`, and older worktrees (v0.50.5 runtimes that had been up for 55 hours).

In scope:

1. **Idle watcher ticks.** A periodic tick with no pending input submits no lane job. On the main checkout, 14,975 of 14,987 watcher batches did nothing, but their events filled about 47 MB of the 64 MiB diagnostics budget. Retained history dropped to about 12 hours, which violates SPEC-0116's requirement that routine jobs not evict history.
2. **Health-probe logging.** Fast successful `GET /api/v1/runtime` probes record no events. Failed, slow, canceled, and panicking probes still record.
3. **Truthful perf flags.** `rzm diagnostics perf` flags a missing terminal event only where it read that process's events contiguously. Before this change, 6 of 6 and 10 of 10 flagged operations had in fact finished.
4. **One discovery per resync.** A watcher stale event followed by its cache recrawl runs one full structural discovery, not two. FSEvents `MustScanSubDirs` events in dropped or ignored trees no longer mark the vault stale.
5. **Bounded derived publication.** Code and note embedding finalization is bounded, and publication is batched under fewer lane jobs, so the source-fingerprint witness runs once per batch. Before this change, one cold code pass admitted about 46,000 embed-cycle jobs in a minute, with queue waits up to 15.5 minutes.
6. **Desktop polling backoff.** The desktop presence loop polls at its short interval only while something is changing. Otherwise it uses a longer interval and refreshes on focus. Before this change it spawned an `rzm` sidecar every 3 seconds per app.
7. **Startup re-embedding investigation.** Determine with a counting fake provider whether a same-build restart re-embeds unchanged ontology bodies or code. Fix it only if it does.

Excluded: Voyage base64 response encoding (measure later), relocating diagnostics out of worktrees, UI route latency, `tasks run-due` reports (21 KB in total), an in-process Rust health probe, runtime pprof, and narrowing full-scope derived work after a no-change resync. That last item changes an indexing invariant and needs Drew's decision.

## Spec Set (Frozen)

- [[persistent-diagnostics|SPEC-0116]] at its 2026-10-25 revision. This effort amends it with a routine-probe exception to HTTP event recording and with the perf reader's coverage rule for unfinished-operation flags.

## Stories In Scope (Frozen)

Requirements-only change to SPEC-0116 and the indexing and vault-runtime subsystem notes. No stories are selected.

## Spec Coverage Checklist

- [x] SPEC-0116 amended for the routine health-probe exception and the unfinished-flag coverage rule.
- [x] Idle watcher ticks admit no lane jobs; every recovery path still converges.
- [x] Health probes that are healthy and fast record nothing; failures and slow probes still record.
- [x] `diagnostics perf` raises no unfinished flag for a terminal event outside the contiguously read range.
- [x] One overflow produces one `discovery.full`; ignored-tree rescans produce none.
- [x] Bounded embed-cycle admissions during a cold code pass.
- [ ] Desktop sidecar launches per minute drop while idle. Unit-tested only; see Deviations.
- [x] Same-build restart behavior established with evidence.

## Plan

Council: Claude Opus 5.5 (high) and GPT-6.1 Sol (high), both read-only. No third provider was available. Their findings merged into these work packages, each with exclusive file ownership:

- **WP-A, watcher recovery** (`pkg/app/bootstrap/live_ownership.go`, `watcher_pending.go`, `pkg/vault/cache/service.go`, `refresh_result.go`):
  - On ticker wakes only, skip `Submit` when there are no pending paths or resync, no cache pending work (not ready, stale, recrawl in flight, unconsumed completion, dirty), no ownership reconciliation debt, and no unready derived work. A read error submits.
  - Replace the shared, consumable `Resynced` bit with a recrawl-completion sequence the watcher owns. A watcher stale obligation waits for the recrawl it caused and then runs one discovery.
- **WP-B, watchhub rescans** (`pkg/vault/watchhub/sources.go`): apply dropped-path and ignore filtering to `MustScanSubDirs` paths before marking stale. A rescan of the root or of an ancestor of a watched root still marks stale.
- **WP-C, health probes** (`pkg/app/web/diagnostics.go`): skip events for `HealthPath` responses below 400 that finish under a slow threshold.
- **WP-D, perf reader** (`pkg/diagnostics/readers.go`, `pkg/app/cli/diagnostics/perf.go`): record, per process, the time from which events were read contiguously, and flag unfinished starts only after it.
- **WP-E, derived publication** (`pkg/app/bootstrap/derived_scheduler.go`, `pkg/embedindex/embed_pipeline_core.go`, `derived_pipeline.go`): bound completion finalization, and give code and notes one publisher that drains bounded batches into one `withLease` each. Publication remains gated on the current work and witness. Acknowledge only after a full drain. Preserve preemption and shutdown joins.
- **WP-F, desktop presence** (`desktop/src-tauri/src/presence.rs`): use the short interval while starting, opening, or rebuilding, and a longer idle interval otherwise, with a refresh on focus or visibility change.
- **WP-G, re-embed investigation**: a test with a counting fake provider runs an index, a restart, and a catch-up. A fix follows only if that test proves unnecessary provider calls.

The lead integrates every package onto one branch and owns the SPEC-0116 amendment, the `indexing.md` and `vault-runtime.md` updates, the diagnostics guide, and the CHANGELOG. Gates: `make check-full`, `./scripts/rzm validate`, and desktop verification for WP-F. Before and after, the lead also measures event bytes per hour and idle CPU on a live runtime.

## Original Intended Delivery

The seven scope items above, in one pull request.

## Actual Delivered

All seven scope items, plus a start-intent poll backoff found during verification (see Deviations):

- **WP-A:** periodic watcher ticks with nothing pending admit no lane job. Cache recrawl completion is a monotonic count each reader compares with its own last value. A watcher resync waits for the recrawl it coincides with, then runs one discovery. Agent resyncs wait for a recrawl that started after their own stale mark (`MarkStaleForResync`, `ResyncCovered`).
- **WP-B:** FSEvents `MustScanSubDirs` rescans inside dropped or ignored trees no longer mark the vault stale.
- **WP-C:** healthy runtime health probes that finish in under one second record no events.
- **WP-D:** `rzm diagnostics perf` flags unfinished operations only within each process's contiguously read range.
- **WP-E:** embedding finalizers are bounded by provider concurrency. Code and note results publish in batches of up to 128 results, 16 MiB, or 250 ms, each under one lane job and one witness check. A failed batch cancels the compute pass.
- **WP-F:** the desktop presence loop polls every 20 s while idle and every 3 s while a runtime is starting, a pane is settling, or a rebuild is pending.
- **WP-G:** a same-build restart makes zero provider calls (`pkg/app/indexing/unified_restart_reuse_test.go`). The 10-29 full re-embed came from the `markdown-projection-v5` bump in `530703fd`, which deletes each note's ontology embeddings even when its bytes are unchanged.

Live measurement: the old build (`main` at `dbc04f3a`) and the new build ran side by side on copies of `testdata/integration/python-app/vault`, with the `test` embedding provider and a health probe every 3 s, for 10 idle minutes after boot. The old build wrote 804,173 event bytes and admitted 201 jobs. The new build wrote 0 event bytes and admitted 0 jobs. Idle CPU over 5 minutes: 1.29 CPU-s/min before the start-intent backoff, 0.18 CPU-s/min after.

## Execution Notes

- 2026-10-31T19:39:10Z: Drew asked to "run a council and plan out how to fix all of the issues you found. Then fix them using a team of Opus High sub-agents", with the instruction "full stop" (no merge). That instruction approves this plan. No current user is configured, so `plan-approved-by` is absent.
- 2026-10-31T19:39:10Z: council corrections adopted:
  - The reader already reads newest files first.
  - A no-op batch must still emit its admission events, so the fix skips admission instead.
  - `MustScanSubDirs` is reclassified before ignore filtering, which is the likely source of the overflows.
  - Every completed owner gets an unbounded goroutine, and `DerivedSourceFingerprint` runs per job.
  - The desktop app backs off instead of porting the probe to Rust.

- 2026-10-31T21:40:00Z: council code review, with Claude Opus 5.5 (high) and GPT-6.1 Sol (high).
  - Round 1: Opus 8.5, Sol 5. Fixed a duplicate test type and the agent resync baseline race.
  - Round 2: Opus 8.5, Sol 9. Fixed compute continuing after a failed publish, the spec threshold, and a stray generated file.
  - Round 3: Opus 9, Sol 9. Restored the generated file again.

## Deviations

- 2026-10-31T21:00:00Z: the plan excluded idle-CPU work unless it remained after the other fixes. It did: about 1.1 CPU-s/min on the fixture with both builds. `sample` attributed it to `watchStartIntent`, which took a file lock and read the start intent every 50 ms for the runtime's lifetime. The poll now backs off from 50 ms to a 1 s cap, still within `StopGrace` (10 s). Added under the original authority because it is the root cause of finding F4, which was in the plan's measure-then-decide scope.
- Desktop verification ran `make desktop-check` (cargo fmt, clippy, and 87 Rust tests, including the new interval tests) and `make desktop-build`. The packaged app could not be exercised: the single-instance plugin handed each launch to Drew's already-running `Rhizome.app`, built in worktree `t3-e36f9b1a`, and quitting that app was not authorized. Watch sidecar launches per minute after `make desktop-install-dev`.
- WP-F accepts a slower relaunch: a runtime that crashes behind an open pane while the desktop is idle can take about 23 s to relaunch instead of about 6. No page-to-presence wake hook exists; recorded as a follow-up.
- SPEC-0116 was amended in this change set (idle-tick admission, the health-probe exception, and the contiguous-read rule for unfinished flags). The freeze above refers to the pre-amendment revision.

## Status

Complete. Delivered by [#62](https://github.com/atomicobject/rhizome/pull/62) on branch `t3/investigate-performance-bottlenecks`.

## Closure Checklist

- [x] Required quality gates pass: `make check-full` exit 0 at `8d317875`; `./scripts/rzm validate` 0 issues; `make desktop-check` and `make desktop-build` pass.
- [x] Alignment and actual outcomes are verified: council review rounds 1–3 and the live fixture measurement.
- [x] Specs and documentation are reconciled: SPEC-0116, `indexing.md`, `vault-runtime.md`, the diagnostics guide, and CHANGELOG.
- [x] Follow-ups are triaged (see Compounding Follow-ups).

## Compounding Follow-ups

- **Needs Drew's decision:** treat a projection or provider version bump for a note whose bytes are unchanged as a projection refresh, not an ownership transition, so embeddings are reused (WP-G report). Until then, each `ProjectionVersion` bump re-embeds every typed note body once (about 14k Voyage texts on drews-vault).
- **Needs Drew's decision:** narrow full-scope derived work after a resync that found no changes.
- Wake desktop presence when a page loses its runtime connection, to restore about 6 s crash recovery while idle.
- `~/.config/rhizome/instances` holds 7,907 leaked `*.intent.lock.guard.lock` files and 361 stale `*.pending.json` files. Find the leaking path, and prune guards for instances that no longer exist.
- Measure Voyage `encoding_format: base64`: responses were 7.5–10× the request bytes.
- Move diagnostics out of worktrees into per-worktree folders under one user-level root, with a cross-worktree reader.
- Skip the redundant `sync_ontology_body_final` re-plan when the final catalog sync wrote nothing.
