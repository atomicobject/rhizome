---
type: TechnicalSpec
id: SPEC-0116
aliases: [SPEC-0116, Persistent diagnostics]
summary: "Bounded local diagnostic events and operation reports, useful indexing, runtime, code-mode, and web UI measurements, and an offline reader that summarizes performance and flags instability for humans and agents."
spec-status: active
last-updated: 2026-10-25
---

# Persistent diagnostics

## Summary

Persist operational evidence under the vault's ignored `.rhizome/diagnostics` directory. Versioned JSONL events and atomic JSON operation reports are authoritative. The reader works without a running runtime or a healthy index. This extends [[indexing-observability-and-maintenance-policy|SPEC-0047]] with durable evidence; existing progress, timing labels, writer ownership, and maintenance policy still apply.

The 2026-10-25 revision makes that evidence useful for performance and stability review. Routine background work no longer crowds older history out of retention. The web UI and code mode record bounded measurements through the same recorder. An offline performance view summarizes latency and flags known instability patterns, so an agent can find a performance bug without writing its own queries over thousands of files.

## Goals

- Diagnose the latest indexing attempt after its process exits, including failure or cancellation.
- Explain indexing scope, full-scan reasons, queue and lock waits, expensive phases, embedding reuse, and physical provider requests.
- Correlate CLI, runtime, MCP, HTTP, and background work with useful, safe structured context.
- Support bounded offline history queries and comparisons that disclose incomplete evidence.
- Keep the configured retention window meaningful under continuous watcher activity.
- Let an agent answer "what is slow" and "what is unstable" from one offline command, including web UI render churn, frame stalls, and code-mode host-call cost.

## Non-Goals

- Changing full-scan fallback, reconciliation, embedding reuse, or rebuild behavior.
- Adding SQLite storage for diagnostics, an external tracing service, or telemetry upload.
- Persisting note bodies, queries, request payloads, credentials, or provider response bodies.
- Promising that interrupted work completed or that differently sized workloads are comparable.
- Recording one event per React render or commit, or explaining why a component rendered (prop-level diffing).
- Fixing performance or stability problems that the evidence reveals. Those are separate work.

## Requirements

### Persistence and lifecycle

- Events and reports MUST carry a schema version, timestamp, process identity, operation identity when available, subsystem, severity or outcome, and stable event or reason names.
- Events MAY carry a bounded `metrics` JSON object alongside their flat attributes. Attributes hold identity and filter values; `metrics` holds measurements. Event metrics MUST pass the same sanitization as report metrics (byte bound, nesting limit, credential redaction) with a cap that keeps the whole record within the per-event bound. The field is additive within schema version 1; readers ignore it when absent.
- Each process MUST own its event segments. Complete reports MUST publish atomically; readers MUST tolerate partial records, corrupt files, concurrent rotation, and unknown schema versions with explicit coverage warnings.
- Retention MUST default to seven days and a bounded disk budget. The latest completed full-index attempt MUST remain available within a bounded exception. Unknown files and symlink targets MUST not be deleted by retention.
- Raw headless startup and crash output MAY use a separate rotated fallback under the diagnostics directory. Its rotation budget and any pre-capture or native-descriptor limits MUST be documented separately from structured retention.
- Diagnostics MUST remain separate from the index database and MUST not trigger indexing or watcher feedback loops.
- Recording failures MUST fail open, emit a bounded nonrecursive warning, and preserve the underlying operation outcome. CLI JSON and MCP stdout MUST remain protocol-clean.
- A start event MUST precede work. A missing terminal record MUST remain unknown or interrupted evidence, never an inferred success.

### Routine work evidence

- A background indexing job of kind `watcher-batch`, `embed-cycle`, `graph-cycle`, or `validation-refresh` that succeeds in under one second, with no fallback, truncation, or error, MUST NOT publish a standalone report. Its `job.finished` event remains its record, so the report is skipped only when the recorder retains INFO events.
- Any job that fails, is canceled or preempted, records a fallback or truncation, or runs for at least one second MUST still publish a report. Explicit indexing and boot catch-up MUST always publish reports.
- `job.finished` events MUST carry the job kind, trigger, status, duration, queue wait, lock wait, and execution time, so aggregate views can account for jobs without reports. Jobs without a report SHOULD carry their bounded indexing metrics snapshot in the event's `metrics`.
- On a vault with continuous watcher activity, routine jobs MUST NOT be the reason history younger than the retention window is evicted.

### Indexing evidence

- Indexing MUST collect bounded operation-scoped metrics without requiring `--timings`. The flag MUST continue to render useful human timing output.
- Lane evidence MUST distinguish admission, coalescing, queue wait, writer-lock wait, execution, completion, failure, and cancellation without counting joined clients as separate jobs.
- Collectors MUST bound metric cardinality, raw samples, and intervals; snapshots MUST disclose truncation and preserve aggregate counts where possible.
- Reports MUST distinguish full discovery, incremental work, and destructive rebuild. Watcher reconciliation MUST retain known trigger reasons instead of reducing every cause to a boolean.
- Metrics MUST distinguish logical embedding demand, reuse and misses, physical provider attempts, retries, backoff, and observed HTTP status where available. No cache or fallback optimization is authorized by this spec.
- A failed index integrity check MUST record a structured WARN `integrity.failed` event with a reason code and, when SQLite names one, the affected table, in addition to any console output.

### Runtime evidence

- One process-level recorder MUST serve the vault's participating boundaries. Request and job context MUST preserve correlation while retaining existing cancellation and ownership behavior.
- Runtime lifecycle and readiness, MCP tool outcome, HTTP route/status/latency, and background job outcomes MUST have stable structured events with useful safe attributes.
- Legacy logging bridges MUST avoid recursive logging and unsafe payload capture. Redacted or unavailable detail MUST be recognizable as incomplete evidence.

### Code-mode evidence

- The `rzm agent code execute` command report MUST include metrics for Node subprocess startup until the runner is ready, script execution time, and the host-call total.
- Host-call metrics MUST be aggregated per catalog operation name: call count, error count, total duration, and maximum duration. They MUST record whether calls were served in-process or forwarded to the runtime. Cardinality is bounded by the operation catalog; inputs and outputs MUST NOT be recorded.

### Web UI evidence

- The web UI MUST aggregate measurements in the browser and send at most one batch every ten seconds while the page is visible, plus a final batch when the page is hidden or unloaded. It MUST skip windows in which every measurement is zero. It MUST NOT send per-render or per-frame records.
- Measurement MUST NOT cause renders. Counters live outside React state, and a failed or disabled send MUST be dropped silently without retry storms.
- Each batch MUST carry a per-page-load client session identity, the web build version, the surface (desktop or browser), and the window start and end. It MAY carry these measurements:
  - **Render regions.** For each named `<Profiler>` region: commit counts by phase (mount, update, nested update), and the total and maximum `actualDuration` and `baseDuration`. Region names MUST be fixed identifiers defined in source, such as `app-shell`, `sidebar`, `view-table`, or `note-view`.
  - **Frame stalls.** Gaps between `requestAnimationFrame` callbacks while the page is visible: counts of gaps over 50 ms and over 200 ms, the total stalled time, and the longest gap. This works in WebKit, which lacks the Long Tasks and Long Animation Frames APIs.
  - **Background refresh churn.** From the single invalidation point, `VaultInvalidationBridge`: refresh cycles, queries refetched, refetches whose data was structurally unchanged, the commits per render region in the second after each refresh, and the refreshes judged as churn. A refresh is churn when regions committed in its second and every refetch it started returned unchanged data. The browser decides this per refresh once that second has passed and its refetches have resolved, so a batch boundary cannot split the evidence. A refresh that starts inside an earlier one's second joins it and is judged with it, and a refresh with a failed, replaced, or still-outstanding refetch after ten seconds is not judged.
  - **Stability.** Event stream reconnects, uncaught errors, and unhandled promise rejections, counted by error name only.
- Batches MUST NOT contain note titles, paths, URLs beyond a fixed route kind, query text, error messages, or stack traces.
- The runtime MUST accept batches on a browser-facing `POST` route that applies the existing loopback-host and same-origin checks. It MUST bound the body size, validate the schema and identifier patterns, reject unknown fields, and record each accepted batch through the process recorder as one `web.client` event: identity and window in attributes, measurements (including per-region data) in `metrics`. When persistence is disabled, the route MUST accept and discard batches.
- Production web builds use React's profiling build (`react-dom/profiling`), which costs about 4.5% scripting time with no regions mounted. Render regions mount only when the page is loaded with `window.__RHIZOME_RENDER_PROFILING__` set, or in a development build, and the choice holds for the page load. Mounted regions cost about 47 to 51% scripting time on a 1,500-row table view. Rhizome Dev always sets the flag; release desktop builds set it only when the desktop setting is on, which defaults to off and applies to windows opened afterward. Plain browsers do not set it. Drew chose this on 2026-10-25 after the first measurement ruled out always-on regions.

### Offline use

- `rzm diagnostics` MUST read persisted evidence without starting a runtime, opening an index, or recording a new operation. Explicit vault selection MUST support diagnosis when normal configuration is broken.
- Commands MUST expose the latest index report, bounded operation history, and severity/time/operation-filtered logs in human and JSON forms.
- Output MUST disclose scanned coverage, truncation, corruption, and absent history. Comparisons MUST identify workload/version differences and avoid claiming statistical certainty from sparse samples.
- The managed Rhizome skill source MUST teach agents how to inspect reports and distinguish observed bottlenecks from hypotheses.

### Performance view

- `rzm diagnostics perf` MUST summarize a time window, defaulting to the last 24 hours, from terminal events, in human and JSON forms. Every reported operation also emits a terminal event, so events cover both reported and event-only work. It MUST share the existing reader bounds and coverage disclosure, and say when the window is only partly covered.
- It MUST group operations by kind and name (command path, HTTP route, MCP tool, code-mode operation, indexing job kind), reporting count, error and cancellation counts, median and maximum duration, and queue and lock waits where present. It MUST report a 95th percentile only for groups with at least 20 samples, and always show the sample count.
- It MUST summarize web UI evidence per render region and surface: commits, render time, frame stalls, refresh churn, and stability counts.
- It MUST flag these anomalies with the supporting operation or event identifiers:
  - more than three runtime starts for the vault within 60 seconds, timed by their start events;
  - started operations with no terminal record that are older than the newest evidence from their process, excluding runtime lifetimes and event-stream requests, which finish only when they stop;
  - WARN and ERROR events grouped by event name, including index integrity failures;
  - refreshes the browser judged as churn (only where render regions are enabled);
  - frame stalls over 200 ms;
  - two or more event stream reconnects in one batch window. Every reconnect also starts a refresh, so reconnects are not compared with refresh cycles.
- Flags MUST be worded as observations to investigate, not diagnoses.

### Verification

- Verification MUST cover concurrent writers/readers, retention, partial/corrupt records, filesystem failure, collector bounds, cancellation, correlation, and protocol output.
- A repeatable local workload MUST compare behavior and overhead before and after recording. Report measured evidence and its limitations; do not claim indexing speedups from instrumentation alone.
- A sustained watcher workload MUST show that routine jobs no longer consume the artifact budget, while failed and slow jobs still publish reports.
- Web UI verification MUST show, in a real browser, that an induced frame stall and a background refresh with unchanged data appear in `rzm diagnostics perf`, that measurement adds no renders, and that rejected batches (oversized, unknown fields, cross-origin) are not recorded. Verification MUST show that the production bundle mounts render regions only when the render-profiling flag is set. The post-refresh render churn flag is verified with synthetic batches.
- Code-mode verification MUST show per-operation host-call metrics in the command report for a script that makes in-process and forwarded calls.

## Open Questions

- Should `perf` cover a full seven-day window? Reader byte caps limit how much event history it can examine. If agents need week-scale trends, periodic summary reports written by the runtime are the proposed answer, not a database.
- Should the runtime spawn storm seen on 2026-10-25 add an anomaly flag beyond the runtime start burst? Its fix landed in #41; the `intel_fts` warning was a false report, fixed in #43.
