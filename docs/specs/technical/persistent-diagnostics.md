---
type: TechnicalSpec
id: SPEC-0116
aliases: [SPEC-0116, Persistent diagnostics]
summary: "Bounded local diagnostic events and operation reports, useful indexing and runtime measurements, and an offline reader for humans and agents."
spec-status: active
last-updated: 2026-10-04
---

# Persistent diagnostics

## Summary

Persist operational evidence under the vault's ignored `.rhizome/diagnostics` directory. Versioned JSONL events and atomic JSON operation reports are authoritative. The reader works without a running runtime or a healthy index. This extends [[indexing-observability-and-maintenance-policy|SPEC-0047]] with durable evidence; existing progress, timing labels, writer ownership, and maintenance policy still apply.

## Goals

- Diagnose the latest indexing attempt after its process exits, including failure or cancellation.
- Explain indexing scope, full-scan reasons, queue and lock waits, expensive phases, embedding reuse, and physical provider requests.
- Correlate CLI, runtime, MCP, HTTP, and background work with useful, safe structured context.
- Support bounded offline history queries and comparisons that disclose incomplete evidence.

## Non-Goals

- Changing full-scan fallback, reconciliation, embedding reuse, or rebuild behavior.
- Adding SQLite storage for diagnostics, an external tracing service, or telemetry upload.
- Persisting note bodies, queries, request payloads, credentials, or provider response bodies.
- Promising that interrupted work completed or that differently sized workloads are comparable.

## Requirements

### Persistence and lifecycle

- Events and reports MUST carry a schema version, timestamp, process identity, operation identity when available, subsystem, severity or outcome, and stable event or reason names.
- Each process MUST own its event segments. Complete reports MUST publish atomically; readers MUST tolerate partial records, corrupt files, concurrent rotation, and unknown schema versions with explicit coverage warnings.
- Retention MUST default to seven days and a bounded disk budget. The latest completed full-index attempt MUST remain available within a bounded exception. Unknown files and symlink targets MUST not be deleted by retention.
- Raw headless startup and crash output MAY use a separate rotated fallback under the diagnostics directory. Its rotation budget and any pre-capture or native-descriptor limits MUST be documented separately from structured retention.
- Diagnostics MUST remain separate from the index database and MUST not trigger indexing or watcher feedback loops.
- Recording failures MUST fail open, emit a bounded nonrecursive warning, and preserve the underlying operation outcome. CLI JSON and MCP stdout MUST remain protocol-clean.
- A start event MUST precede work. A missing terminal record MUST remain unknown or interrupted evidence, never an inferred success.

### Indexing evidence

- Indexing MUST collect bounded operation-scoped metrics without requiring `--timings`. The flag MUST continue to render useful human timing output.
- Lane evidence MUST distinguish admission, coalescing, queue wait, writer-lock wait, execution, completion, failure, and cancellation without counting joined clients as separate jobs.
- Collectors MUST bound metric cardinality, raw samples, and intervals; snapshots MUST disclose truncation and preserve aggregate counts where possible.
- Reports MUST distinguish full discovery, incremental work, and destructive rebuild. Watcher reconciliation MUST retain known trigger reasons instead of reducing every cause to a boolean.
- Metrics MUST distinguish logical embedding demand, reuse and misses, physical provider attempts, retries, backoff, and observed HTTP status where available. No cache or fallback optimization is authorized by this spec.

### Runtime evidence

- One process-level recorder MUST serve the vault's participating boundaries. Request and job context MUST preserve correlation while retaining existing cancellation and ownership behavior.
- Runtime lifecycle and readiness, MCP tool outcome, HTTP route/status/latency, and background job outcomes MUST have stable structured events with useful safe attributes.
- Legacy logging bridges MUST avoid recursive logging and unsafe payload capture. Redacted or unavailable detail MUST be recognizable as incomplete evidence.

### Offline use

- `rzm diagnostics` MUST read persisted evidence without starting a runtime, opening an index, or recording a new operation. Explicit vault selection MUST support diagnosis when normal configuration is broken.
- Commands MUST expose the latest index report, bounded operation history, and severity/time/operation-filtered logs in human and JSON forms.
- Output MUST disclose scanned coverage, truncation, corruption, and absent history. Comparisons MUST identify workload/version differences and avoid claiming statistical certainty from sparse samples.
- The managed Rhizome skill source MUST teach agents how to inspect reports and distinguish observed bottlenecks from hypotheses.

### Verification

- Verification MUST cover concurrent writers/readers, retention, partial/corrupt records, filesystem failure, collector bounds, cancellation, correlation, and protocol output.
- A repeatable local workload MUST compare behavior and overhead before and after recording. Report measured evidence and its limitations; do not claim indexing speedups from instrumentation alone.
