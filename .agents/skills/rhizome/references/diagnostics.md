# Operational diagnostics

Use this route when Rhizome indexing is slow or fails, the runtime will not start, a query degrades, or a mutation returns an operational error. These are retained operational records, distinct from content validation and ontology health reports.

Read evidence before rerunning expensive work:

```bash
rzm diagnostics index --last --json
rzm diagnostics index --since 7d --json
rzm diagnostics logs --level warn --since 2d --json
```

These commands read plain files under `.rhizome/diagnostics/`. They do not start the runtime, open the main index, or record their own activity. They need no agent session or code-mode server. When configuration, registration, or the index is broken, pass an existing directory explicitly:

```bash
rzm diagnostics index --last --vault /absolute/repository/path --json
```

Use the returned operation ID to join its full report and events. `--operation-id` matches one operation exactly; use `--trace-id` to include related parent/worker operations in the same trace. History includes kinds beyond index; discover actual recorded kinds and statuses rather than assuming all operations were captured:

```bash
rzm diagnostics reports --since 7d --json
rzm diagnostics report <operation-id> --json
rzm diagnostics logs --operation-id <operation-id> --since 7d --level debug --json
```

`reports` accepts exact `--kind`, `--status`, `--operation-id`, and `--trace-id` filters. `logs` accepts an exact `--subsystem` filter and a minimum severity. History queries support `--since`, `--until`, `--limit`, and `--max-bytes`; times accept durations such as `7d` or `2h`, or RFC3339 timestamps. Consult the command's `--help` for current defaults.

## Read the evidence accurately

- `index --last` selects the latest completed index attempt, including error and cancellation, and excludes individual watcher updates. It is independent of the history cutoff. Check its timestamps before treating it as recent.
- `coverage.warnings` and `coverage.truncated` describe missing, malformed, partial, unsupported, or bounded input. No matching records does not establish that an operation never happened. Logging may have been disabled, the operation may predate instrumentation, or retention may have removed it.
- A stored report's `truncated` flag describes bounded or incomplete recorded detail. It is separate from `coverage.truncated`, which describes the read. Index summaries preserve this distinction.
- `index_summaries` ranks measured phase spans for `index` and `indexing-job` reports and reports observed reason counts. The existing `fallback_counts` field includes search fallback suffixes and full producer names for `calledge.fallback.*`, `scope.full_recompute.*`, and `watcher.full_discovery.*`. These represent distinct work: call-edge fallback, scope recomputation, and watcher discovery. None alone establishes a destructive index rebuild. Nested or overlapping spans cannot be added into a new total. A fallback count means the branch ran; it does not establish successful recovery.
- Reports preserve raw `metrics`, operation/parent/trace IDs, version, status, timestamps, and queue, lock, and execution waits. Metrics use nanoseconds unless a field says otherwise; report duration and wait fields use milliseconds. Use explicit values instead of deriving a timing from a counter.
- Retained-prefix quantiles and interval unions describe retained detail. Dropped sample/interval counts and rejected observations are evidence gaps. Do not call them complete distributions.

For a performance comparison, keep repository content, file/chunk counts, index mode, cache warmth, providers/fingerprints, configured concurrency, and versions comparable. Report what changed and the measured before/after values. Use both cumulative measurements and latency distributions when available. Identify unavailable coverage or mismatched workloads before attributing a difference to a change. These summaries do not detect anomalies or establish causation automatically.

Read the failing operation's warning/error events and measured phases, then inspect the implicated subsystem or configuration. Preserve returned evidence; rerun only the smallest authorized workload needed to test a concrete hypothesis. Content problems still use `references/validation-and-repair.md`; indexing scope/freshness uses `references/indexing-and-freshness.md`.

## Raw files

Plain JSON/JSONL files remain authoritative and can be read without Rhizome:

```bash
jq '{operation_id, kind, status, duration_ms, metrics}' .rhizome/diagnostics/latest-index.json
jq -c 'select(.level == "WARN" or .level == "ERROR")' .rhizome/diagnostics/events/*.jsonl
```

An actively written JSONL tail may be incomplete; the bounded CLI skips damaged records and returns warnings. A direct `jq` read can fail at such a tail. Inspect retained messages before including them in a bug report.
