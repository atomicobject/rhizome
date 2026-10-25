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
- [ ] Agent guidance updated so agents know how to review performance from diagnostics.

## Plan

1. Specify: revise SPEC-0116 with the new requirements, including the client evidence privacy bounds and how frontend measurement works in production WebKit builds (Tauri on macOS) where Long Tasks and Long Animation Frames may be unavailable and React Profiler needs a profiling build.
2. Retention fix (smallest, independent).
3. Aggregate perf view and anomaly flags over existing evidence.
4. Frontend client evidence: browser aggregation, endpoint, recorder events.
5. Code-mode timings.
6. Agent guidance, quality gates, independent review, pull request.

Detailed design follows in the planning phase.

## Plan Approval

On 2026-10-25 Drew approved opening this effort with the recommended plan ("Yes" to "Should I open one with this plan?"). That approves the scope and the outline above; the detailed plan from the planning phase still needs approval. `plan-approved-by` is omitted until that approval.

## Original Intended Delivery

Persistent diagnostics that keep their full retention window, summarize performance and stability for agents, and include frontend render and code-mode evidence.

## Actual Delivered

Not yet delivered.

## Execution Notes

- 2026-10-25T13:43:47Z: Opened from a review of the primary checkout's `.rhizome/diagnostics`. 4,012 retained reports against the 4,096-artifact cap; 3,542 were watcher-batch jobs, 3,502 of them under 50 ms, so history covered about three hours. The same data showed 79 headless `rzm serve` starts within one minute at 12:08, and `runtime-output.log` showed an `intel_fts` FTS5 integrity failure that appeared nowhere else. The web UI has no performance instrumentation.

- 2026-10-25T14:05:00Z: Revised SPEC-0116 (plan step 1): routine-work report suppression, structured integrity-failure WARN, code-mode host-call metrics, web UI evidence, the `rzm diagnostics perf` view with anomaly flags, and matching verification. One open decision is marked `[TODO: Confirm with Drew]`: whether production web builds ship React's profiling build. `frozen-scope-drift` reports no SPEC-0116 drift; its three findings belong to other efforts.

## Deviations

None.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

None yet.

## Status

Active. SPEC-0116 revision drafted; waiting on Drew's profiling-build decision, then the planning phase.
