---
type: ReferenceDoc
summary: "Read retained operational logs and measured indexing reports without starting Rhizome's runtime or opening its index."
reference-kind: guide
status: active
last-verified: 2026-10-04
---

# Operational diagnostics

Rhizome persists bounded structured logs and operation reports locally under `.rhizome/diagnostics/`. Read them when indexing is slow or fails, runtime startup fails, a query falls back, or a mutation returns an operational error:

```bash
rzm diagnostics index --last --json
rzm diagnostics index --since 7d --json
rzm diagnostics logs --level warn --since 2d --json
rzm diagnostics reports --since 7d --json
rzm diagnostics report <operation-id> --json
```

Omit `--json` for a readable summary. Each JSON result has `schema_version`, `vault_root`, `mode`, retained `reports` or `events`, and `coverage`. Index reports also include `index_summaries` with measured slow phases, observed fallback reason counts, and evidence gaps. Read failures produce a JSON result with `error` and exit status 1.

The reader never starts the runtime, opens the main index, creates a recorder, or writes diagnostic records. By default it uses the nearest ancestor containing a `.rhizome` directory, or the current directory. `--vault` accepts an existing vault directory or registered name. With broken configuration, use `--vault /absolute/repository/path` to read files directly.

History reads default to seven days and at most 100 records. Use `--since` and `--until` with durations (`7d`, `2h`) or RFC3339 timestamps. `--limit` and `--max-bytes` bound input and output. `reports` filters by exact `--kind`, `--status`, `--operation-id`, or `--trace-id`. `logs` filters by minimum `--level` and exact `--operation-id`, `--trace-id`, or `--subsystem`. Records have deterministic order; inspect `coverage` before assuming the results are complete.

`index --last` reads a protected copy of the latest completed index attempt, including errors and cancellations. It excludes individual watcher updates and remains readable outside the history retention window. Check its timestamps. `--last` and history bounds are separate queries. Supplying history bounds without `--last` selects history, with a seven-day default cutoff.

## Retention and configuration

Structured diagnostics default to seven days of history with a 64 MiB budget and a 4,096-file ceiling. Storage pressure can remove history earlier; the latest-index copy is protected. Active CLI event segments reserve up to 64 KiB each; runtime segments reserve up to 4 MiB. Closed segments count at their actual size. A busy storage guard is retried for at most two seconds before recording fails open. Headless Go output rotates `runtime-output.log` at 5 MiB and keeps one previous file. Native crash output and output before capture initializes can exceed that separate limit until the next log open trims it. Configure structured retention or disable persistence in `.rhizome/config.yml`:

```yaml
diagnostics:
  enabled: true
  retentionDays: 7
  maxBytes: 67108864
  level: info
```

Retention must be positive and `maxBytes` must be at least 4096. Operational warnings and errors remain visible on stderr. Boundaries whose CLI, JSON, or HTTP response already carries the result retain their diagnostic outcome quietly. Routine informational events go to retained files. Help, completion, version, and static agent metadata commands do not record diagnostics. An explicit `enabled: false` remains effective even if another diagnostics setting is invalid. Structured events correlate process, operation, parent operation, and trace IDs. No database or separate viewer is required.

## Performance evidence

Reports preserve queue, lock, execution, and total duration in milliseconds, plus typed metrics in nanoseconds where they represent duration. The summary ranks measured spans; nested or overlapping spans cannot be added into a new total. Fallback reason counts describe observed branches and do not prove recovery succeeded.

Every search retains correlated start and terminal log events with duration, outcome, and result count. Detailed search reports retain phase durations and fallback counts for calls taking at least 250 ms, failed or canceled calls, panics, and degraded or fallback calls, without requiring `--timings`. Healthy calls below that threshold retain events without writing a report. Read reports with `rzm diagnostics reports --kind search --json`. A report with `metrics_scope: context` shares its caller's collector, so its snapshot can include other work in that context. `metrics_scope: search_operation` identifies a collector owned by that search.

Watcher structural updates and background derived work retain separate reports. Read derived preparation, computation, and publication outcomes with `rzm diagnostics reports --kind derived-work --json`. A superseded input is skipped; explicit indexing and external writer priority are recorded as preemption causes. These reports cannot replace the protected full-index report.

Runtime embedding nodes may combine requests from several tickets into one physical batch. Their bounded physical provider, retry, batching, and dedupe metrics belong to a shared runtime lifetime report, `runtime.embedding-provider`, published after its nodes drain at shutdown. Per-ticket reports retain logical work measurements. An active or crashed runtime may have no completed shared physical summary; missing provider counters do not establish that no requests occurred.

Compare the same workload before and after a change. Check index mode, repository content, file/chunk counts, cache warmth, provider fingerprints, versions, and configured concurrency. Retained-prefix quantiles and unions of retained intervals may cover only part of the operation. The raw metric coverage and summary evidence gaps disclose dropped detail. No automatic anomaly or causation conclusion is produced.

Missing results, malformed or incomplete lines, unsupported records, and bounded reads appear in `coverage.warnings` or `coverage.truncated`. Absence can reflect disabled capture, an operation before instrumentation, retention, or a limit. It does not establish that nothing happened.

## Reading raw files

Reports are JSON and process event segments are JSONL. Plain files remain authoritative:

```bash
jq '{operation_id, kind, status, duration_ms, metrics}' .rhizome/diagnostics/latest-index.json
jq -c 'select(.level == "WARN" or .level == "ERROR")' .rhizome/diagnostics/events/*.jsonl
```

An actively written JSONL tail may be incomplete. The CLI skips damaged records with warnings; a direct `jq` read may fail at the tail. Review retained messages before sharing diagnostics outside the repository.
