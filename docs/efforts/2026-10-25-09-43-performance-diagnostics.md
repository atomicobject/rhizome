---
type: EffortNote
id: EFF-2026-10-25-09-43
aliases: [EFF-2026-10-25-09-43]
name: Performance diagnostics
created-at: 2026-10-25T13:43:47Z
status: active
summary: "Extend persistent diagnostics so agents can find Rhizome performance and stability problems: real retention, an aggregate perf view with anomaly flags, frontend render evidence, and code-mode timings."
governing-specs:
  - "[[persistent-diagnostics]]"
---

# Performance diagnostics

## Scope

Extend the existing file-based diagnostics from [[persistent-diagnostics|SPEC-0116]] so an agent can review Rhizome performance and surface instability without writing ad hoc queries over thousands of files. Keep plain files as the authoritative store; do not add a diagnostics database.

In scope:

1. **Retention that holds its window.** Successful watcher-batch indexing jobs below a time threshold record their existing `job.finished` event without a standalone report, so the seven-day retention window covers seven days.
2. **Aggregate performance view.** An offline `rzm diagnostics` mode (working name `perf`) reports p50, p95, and max duration by operation kind, HTTP route, and MCP tool over a window, with JSON output and coverage disclosure. It flags anomalies, including bursts of runtime starts, error spikes, and WARN events such as index integrity failures.
3. **Frontend evidence.** The web UI aggregates render and responsiveness measurements in the browser and flushes them periodically to a loopback, control-authorized endpoint that records `web.client` events through the existing recorder. Candidate measurements: React Profiler commit counts and durations per component, frame stalls from `requestAnimationFrame` gaps, rerenders driven by background refreshes, and route-transition timing. No per-render events.
4. **Code-mode timings.** `rzm agent code execute` reports sandbox startup and per-host-call names and durations in its command report metrics.

Excluded: a diagnostics SQLite store, telemetry upload, and fixes to issues the new evidence reveals. The 2026-10-25 runtime spawn storm and `intel_fts` corruption are investigated in separate threads.

## Spec Set (Frozen)

- [[persistent-diagnostics|SPEC-0116]], extended by this effort. The current revision lists SQLite storage as a non-goal (kept) and does not cover client-side evidence, aggregate views, or report suppression for routine work. The first planned step revises SPEC-0116 to state those requirements before implementation.

## Stories In Scope (Frozen)

Requirements-only extension of SPEC-0116. No stories are selected.

## Spec Coverage Checklist

- [ ] SPEC-0116 revised for retention behavior, aggregate view, client evidence, and code-mode timings.
- [ ] Routine watcher batches no longer evict retained history.
- [ ] Aggregate perf view with anomaly flags, human and JSON output.
- [ ] Frontend render and responsiveness evidence in persistent diagnostics.
- [ ] Code-mode execution breakdown in command reports.
- [ ] Integrity check failures recorded as structured WARN events.
- [ ] Agent guidance updated so agents know how to review performance from diagnostics.

## Plan

SPEC-0116 was revised on 2026-10-25 (commits `4a2cbad3` and later) and is the contract for this plan. The current gap: every watcher batch publishes a report, so retention covers about three hours; the web UI records nothing; code-mode command reports carry no metrics; there is no aggregate view; and integrity failures reach only raw stderr.

### Decisions

- **D1, command report metrics.** Commands need a way to add metrics to their `command` report. Recommendation: a context-carried metrics setter next to the existing finalizer key in `cmd/diagnostics_wiring.go`, so `agent code execute` sets metrics before finishing. This stays inside `cmd`, not a new shared contract.
- **D2, event metrics and client event shape.** Settled in the spec: events gain an optional bounded `metrics` object, sanitized like report metrics, additive within schema version 1. One `web.client` event per batch carries its measurements there. Batches with all-zero windows are not sent, which keeps an idle tab from filling the event budget.
- **D3, profiling-build fallback threshold.** Drew chose the profiling build in production. Recommendation: fall back to development-only render regions if the profiling build raises scripted-scenario scripting time on a heavy type view by more than 10 percent (median of five Playwright runs).

None needs a foundation review: each is local to one module and cheap to change.

### Batches

| Batch | Outcome and scope | Work | Exit evidence |
| --- | --- | --- | --- |
| B0 Event metrics | Events accept a bounded `metrics` object (D2). `pkg/diagnostics` (`types.go`, event write path, `reports.go` `sanitizeMetrics` reuse, readers), `pkg/diagnostics/CONTEXT.md`, a metrics-carrying event helper in `pkg/logging` if producers need one. | Add the field, an event-sized cap, sanitization, and reader round-trip. Update the package contract. | Package tests: round-trip, oversize and deep nesting rejected or truncated with `truncated` set, credential redaction, old records without the field still read. Focused tests, `make check-fast`. |
| B1 Retention and integrity | Routine jobs stop publishing reports; integrity failures become WARN events. `pkg/app/bootstrap/lane` (`diagnostics.go` `publishDiagnostics`), `pkg/app/indexing/unified_post.go`. | Suppression rule from the spec (kinds, under 1 s, success, no fallback or truncation); confirm `job.finished` carries kind, trigger, status, duration, and waits. Replace the integrity `fmt.Fprintf` with a structured WARN that still mirrors to stderr. Coordinate with the `intel_fts` investigation thread, which may touch the same code. Update the indexing subsystem note. | Lane tests for the suppression matrix; a test that many routine jobs publish no reports while slow and failed jobs do; an integrity-failure test that sees the WARN event. `make check-fast`, focused tests. |
| B2 Code-mode metrics | `agent code execute` reports carry startup, script, and per-operation host-call metrics. `pkg/app/agentcode` (`execute.go`, `server.go` dispatch), `cmd/agent_code_execute.go`, `cmd/diagnostics_wiring.go` (D1). | Time Node startup until the runner is ready and script execution; aggregate host calls per catalog operation at the single dispatch point, recording in-process or forwarded; attach through D1. | `cmd` test running a script with in-process and forwarded calls and asserting the report metrics; no inputs or outputs recorded. Focused tests. |
| B3 Web UI evidence | The browser sends bounded batches; the runtime records them. `web/src` (new `diagnostics/` module, `main.tsx`, `VaultInvalidationBridge.tsx`, region wrappers), `web/vite.config.ts`, `pkg/app/web` (new route and handler). Custom views and the kit bundle are excluded. | Client: counters outside React state; `<Profiler>` regions (`app-shell`, `sidebar`, `type-view-table`, `note-view`, refined during implementation); `requestAnimationFrame` gap detector while visible; refresh-cycle, refetch, and unchanged-data counts from the query cache at the invalidation bridge; reconnect and error counts; 10 s flush plus `pagehide`/`visibilitychange` with `keepalive`; surface from `window.__RHIZOME_DESKTOP__`. Build: alias `react-dom/client` to `react-dom/profiling` for production. Server: `POST /api/v1/diagnostics/client` under `registerRoutes`, existing host and origin checks, `MaxBytesReader`, strict decode, identifier patterns, D2 events. Run the D3 overhead experiment. Update the web subsystem note. | Go handler tests for accepted, oversized, unknown-field, and cross-origin batches; web unit tests that measurement adds no commits and all-zero windows are skipped; D3 measurement recorded in execution notes. `make check`. |
| B4 Performance view | `rzm diagnostics perf` summarizes and flags. `pkg/app/cli/diagnostics` (new `perf.go`, `read.go` dispatch, `text.go`), `cmd/diagnostics.go`. | Read reports and terminal events within existing bounds; group by kind and name with count, errors, cancellations, median, max, waits, and p95 only at 20 or more samples; web region and window summaries; the six spec anomaly flags with supporting IDs and observational wording; JSON and text output with coverage. | Package tests over synthetic diagnostics directories covering each flag, sparse-sample p95 omission, and partial coverage; run against the primary checkout's real diagnostics (read-only) and record the output. |
| B5 Integration and delivery | End-to-end evidence, guidance, review, pull request. | Playwright E2E: induce a 300 ms stall and a refresh whose data is unchanged but triggers region commits, then assert both appear in `rzm diagnostics perf --json`. Update the managed skill source (`pkg/app/cli/init/templates/skills/markdown/rhizome/references/diagnostics.md`, `SKILL.md`) to teach the perf view; regenerate with `make build`, `rzm init`, `rzm init --check`. Bump `last-verified` on touched subsystem notes. | `make verify` (or `make check-full` plus `make web-e2e`), `./scripts/rzm validate`, `frozen-scope-drift`; independent review with findings resolved; `greptile review --json`; pull request opened and linked. No merge or release. |

B0 lands first. Then B1 and B2 (Go) and B3 (web plus its route) are independent and can run in parallel lanes, using T3 `delegate_task` for long work. B4 starts once B0, B1's event fields, and B3's event are in place. Escalate a material change to the spec contract, a needed SQLite or schema migration, or a D3 result that is ambiguous; everything else is a routine choice recorded in execution notes.

## Plan Approval

On 2026-10-25 Drew approved opening this effort with the recommended outline ("Yes" to "Should I open one with this plan?"), chose the production profiling build ("Your rec"), approved adding event `metrics` ("OK. Yes go"), and with that approved the detailed plan in commit `3d64b932` as amended by B0 and the D2 revision. The current-user configuration is not set in this worktree, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

Persistent diagnostics that keep their full retention window, summarize performance and stability for agents, and include frontend render and code-mode evidence.

## Actual Delivered

Not yet delivered.

## Execution Notes

- 2026-10-25T13:43:47Z: Opened from a review of the primary checkout's `.rhizome/diagnostics`. 4,012 retained reports against the 4,096-artifact cap; 3,542 were watcher-batch jobs, 3,502 of them under 50 ms, so history covered about three hours. The same data showed 79 headless `rzm serve` starts within one minute at 12:08, and `runtime-output.log` showed an `intel_fts` FTS5 integrity failure that appeared nowhere else. The web UI has no performance instrumentation.

- 2026-10-25T14:05:00Z: Revised SPEC-0116 (plan step 1): routine-work report suppression, structured integrity-failure WARN, code-mode host-call metrics, web UI evidence, the `rzm diagnostics perf` view with anomaly flags, and matching verification. One open decision is marked `[TODO: Confirm with Drew]`: whether production web builds ship React's profiling build. `frozen-scope-drift` reports no SPEC-0116 drift; its three findings belong to other efforts.
- 2026-10-25T14:20:00Z: Drew chose the production profiling build (recommendation accepted). Planning found that recorder events take flat attributes only, so the spec now records a batch as one window event plus one event per committing region, and skips all-zero windows to protect the event budget.
- 2026-10-25T14:13:00Z: B0 done in `c75e09ac`. `Recorder.EventMetrics` adds a bounded `metrics` object, sanitized by the existing `sanitizeMetrics`; invalid, oversized, or record-overflowing metrics are dropped with `truncated` set and the event kept. Evidence: `go test -tags fts5 ./pkg/diagnostics/...` passed (new `event_metrics_test.go`); `make check-fast` passed with 0 issues. B1, B2, and B3 started as parallel delegates in separate worktrees from `c75e09ac`.
- 2026-10-25T14:22:00Z: B1 merged (`d8db08df`, merge `fc8202e2`). Fast successful watcher-batch, embed-cycle, graph-cycle, and validation-refresh jobs publish no report; `job.finished` now carries trigger, duration (admission to finish, so queue wait counts), the report flag, and for skipped reports the metrics snapshot. A snapshot too large for an event still publishes a report. Fallback means a positive `search.fallback.*` or `calledge.source_fallback.*` counter; routine counters that merely contain "fallback" (untyped projections, hash reuse, `py_fallback`) do not count. Integrity failures record `index.integrity_check_failed` WARN events. Review fix `7b57bf29`: the table pattern missed SQLite's quoted form (`from table "intel_fts"`, the message seen on Drew's vault); added that case red-green. Delegate evidence: focused bootstrap, indexing, diagnostics, cli/diagnostics, and web package tests, race-enabled lane tests, and `make check-fast` all passed.
- 2026-10-25T14:24:00Z: B2 merged (`e181890a`). The runner emits one `executionReady` frame before running the script; the transport times startup (spawn to ready) and script (ready to result). Host calls are aggregated in the cmd invoker (`cmd/agent_code_serve.go`), not `server.go`, because only the invoker knows whether a call was forwarded; calls handled on the runtime path count as forwarded, including runtime-path failures. D1 landed as `setCommandDiagnosticMetrics` with `logging.CompleteQuietMetrics`. Delegate evidence: new `TestAgentCodeExecuteReportsHostCallMetrics` (real Node, fake runtime, checks the split and that no input or output markers reach diagnostics files), full `./cmd/...`, agentcode, logging, diagnostics tests, race runs, and `make check-fast` passed. After merging, `go build ./...` and the metrics and diagnostics cmd tests passed on the integrated branch.
- 2026-10-25T14:30:00Z: B4 server side. `diagnostics.ScanEvents` visits matching events within the byte budget without the 1,000-record cap; `ReadEvents` now wraps it. `rzm diagnostics perf` aggregates terminal events only (every report has one), groups by kind and trigger (job kind for indexing jobs), sorts by total time, and flags runtime start bursts, unfinished operations, and WARN/ERROR groups (integrity failures get their own flag). Spec wording updated to match. Tests in `perf_test.go` were checked for failure by breaking the p95 and burst thresholds on purpose. Run read-only against the primary checkout's diagnostics, it flagged the 79-runtime burst (12:08:25 to 12:09:00) and showed 80 canceled `/api/v1/runtime/shutdown` requests in the same period; old `job.finished` events lack `duration_ms`, so indexing jobs only appear in data written after B1. `make check-fast` passed. The web summary waits for B3's event shape.

- 2026-10-25T14:40:00Z: B4 web summary in `c5151e06`. `perf` reads `web.client` metrics into per-surface summaries (frames, refresh, reconnects, error names) and per-region totals, and adds the three web flags: frame gaps over 200 ms, region commits after a refresh whose refetches were all unchanged, and more reconnects than refresh cycles. `perf_web_test.go` fails when the all-unchanged condition is removed. `make check-fast` passed.
- 2026-10-25T16:12:00Z: B3 merged (`aa065ea7`, `c71585eb`). D3 failed the 10% threshold: on a 1,500-note `Person` table (scroll to 1,200 rows, four sorts, Chromium CDP metrics, one warm-up and five interleaved runs per build) the profiling build added 47.1% median scripting time (2357.5 ms to 3467.6 ms), and 50.7% with the vault copies swapped as a control. Render regions are development-only; production ships frame stalls, refresh churn, and stability counts. The region `type-view-table` became `view-table` because the table renders for every configured view. Reconnects are counted in `VaultInvalidationBridge`, not `eventStream.ts`, to keep the kit bundle unchanged. Delegate evidence: handler tests for accepted, oversized, unknown-field, pattern, negative-count, trailing-value, and cross-origin batches; 29 vitest tests in `src/diagnostics` and `src/query` (no added commits, all-zero windows skipped, no private strings); `make check` passed; a real-browser smoke run recorded a `web.client` event with `over_200ms: 1, longest_ms: 300`. The final `pagehide` flush is covered by a unit test only. The `perf` reader matches the handler's metrics shape.
- 2026-10-25T16:17:00Z: Merged `origin/main` (`9b336978`). Main's FTS5 fix (#43) added its own integrity WARN, `integrity.failed` with `reason_code`, and fixed the false corruption reports behind the 2026-10-25 `intel_fts` warning. B1's `index.integrity_check_failed` was dropped in favor of main's event, keeping B1's `table` attribute; the `index_integrity` flag now matches `integrity.failed`. Main also carries the spawn-storm fix (#41). Integrity, diagnostics, web, and lane package tests passed after the merge.

## Deviations

- D3 fallback: React's profiling build cost 47 to 51% scripting time, so render regions are development-only instead of shipping in production. The `web_refresh_render_churn` flag therefore fires only for development sessions.
- The integrity WARN event is main's `integrity.failed` (#43), not the `index.integrity_check_failed` this plan introduced in B1.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

None yet.

## Status

Active. B0 to B4 merged with main; B5 (end-to-end test, agent guidance, full verification, review, pull request) remains.
