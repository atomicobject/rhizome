---
summary: "Reproducible semantic-query latency baseline, phase and I/O evidence, and the bounded optimization boundary for EFF-0076."
reference-kind: analysis
last-verified: 2026-08-05
code-paths:
  - cmd/agent_context.go
  - cmd/runtime_helpers.go
  - pkg/app/mcp/semantic_query_unified.go
  - pkg/indexingperf/semantic_query.go
tags: [performance, semantic-query, subsystem/search, subsystem/agent-surface]
---

# Semantic-query performance, 2026-08-05

## Benchmark contract

The primary benchmark is a fresh one-shot `rzm` process against this repository's existing unified index. It uses an exact path seed, fixed query, limit 20, repository delegation disabled, and normalized ordered result identity. The Markdown-heavy a personal Markdown vault corpus is the comparison. The deterministic MCP provider fixture is the offline correctness and benchmark-smoke contract; it is not a production latency proxy.

The checked-in runner supports first, repeated no-session, repeated same-session, overview, and precision cells. Production comparisons use at least 20 repeated samples and record raw durations, median/p95, output bytes, normalized result hash, binary identity, corpus/index fingerprint, and opt-in diagnostics. It forces `RZM_SKIP_REPO_DELEGATE=1`; a regression test prevents a frozen binary from silently delegating to the worktree candidate. The final raw matrix is persisted in [semantic-query-performance-2026-08-05-data.json](semantic-query-performance-2026-08-05-data.json). Wall-time thresholds remain outside ordinary CI because machine and filesystem state vary.

Representative command:

```bash
scripts/perf/agent_start_benchmark.sh \
  --rzm ./bin/darwin/rzm \
  --source . \
  --semantic-query-only \
  --semantic-query-scenario semanticRepeatedNoSession \
  --semantic-query-path pkg/app/mcp \
  --semantic-query-text "semantic query transaction invariants identifier mappings" \
  --semantic-query-timings \
  --samples 20
```

## Frozen release baseline

Source/binary: `db055b359568ac402d1e2be3500b951afea1671d` (release, v0.50.3).

- Rhizome worktree: 1,504 Markdown files and a 449,736,704-byte SQLite index at the final frozen comparison. Twenty release samples produced a 3,175.460 ms median and 3,826.922 ms p95; all ordered result/lane/warning identities matched SHA-256 `e846972be319e76dc3f703a0f32049a90b7801a4a497e6a6e5721c0df1b0d70f`.
- Markdown-heavy comparison: 2,772 Markdown files and a 284,164,096-byte refreshed temporary-copy index. Twenty release samples produced an 875.311 ms median and 920.619 ms p95; all ordered result/lane/warning identities matched SHA-256 `cafffaaa27b933e94e921f5444a09f5a4701fb5ec1e5a2f725e3ddc490d2f18b`.
- A Rhizome resource sample was 2.88 seconds wall / 1.18 user / 1.93 system with zero block-input operations on the warm native host. High system time is consistent with metadata/page-cache work but is not a Lima cold-I/O measurement.

## Phase evidence

The first instrumented Rhizome run measured 4,145 ms inside the request boundary:

| Area | Duration / count | Evidence |
| --- | ---: | --- |
| Bootstrap | 2,593 ms | One-shot runtime construction and required readiness before handler dispatch |
| Note/cache readiness | 2,590 ms | 473 note reads, two repository walks, and 1,659 code reads |
| Handler | 1,551 ms | Shared MCP/CLI semantic-query handler |
| Search | 1,519 ms | 1,498 ms retrieval, 18 ms rank, 2 ms rollup |
| Vector lane | 1,253 ms | Note embedding 753 ms and code embedding 788 ms overlap |
| Shaping | 5 ms | Result selection/materialization after search |
| Schema statements | 2 | Query path reached existing schema/vec readiness work |

The spans overlap: vector note/code work is inside retrieval, search is inside handler, and note/cache readiness is inside bootstrap. They must not be summed. The dominant confirmed pre-handler cost is the full cache/discovery pass; the dominant handler cost is vector retrieval/embedding. This explains why a slow filesystem magnifies the command even when SQLite query work itself is bounded.

The generic collector initially caused an unacceptable observer effect (4,096.931 ms median and 9,324.624 ms p95 across 20 samples). Restricting semantic-query collection to the declared phase/counter set removed that distortion: seven instrumented candidate samples measured 2,909.479 ms median and 3,954.477 ms p95, retained the exact release result hash, and reported 6.9-7.4 seconds of search deadline margin. The no-timings path does not install either collector and its JSON omits `diagnostics`.

The deterministic in-process handler benchmark currently measures approximately 0.95 ms/op, 533 KB/op, and 1,648 allocations/op on an Apple M4 Pro (`-benchtime=5x`). Reopening the current test store measures approximately 1.33 ms/op, 16 KB/op, and 310 allocations/op. These isolate handler/store/retrieval/shaping work from process and runtime bootstrap; they are regression signals, not production corpus proxies.

## Historical implementation boundary

Before implementation, the safe boundary was one managed indexed-store owner and query-only vector readiness before removing cache/Search initialization. The approved combined Phase 2/3 slice then added indexed-only directory expansion and durable missing/stale/incompatible classification, allowing Search/cache removal without a live fallback.

Operation-count acceptance for the optimized current-index path is zero full note passes, zero repository walks, zero broad code reads, zero fallback store opens, zero integrity checks/schema DDL/index writes, and deterministic provider/session readiness. Native wall-time target is at least a 50% median improvement on Rhizome with no comparison-corpus regression.

## Implemented current-index path

The approved one-shot policy now requests Semantic + CodeIndex + CodeEmbeddings but omits Search/cache, watchers, indexers, syncers, and leader work. It opens one validated Intel reader with SQLite `mode=ro`/`query_only`, constructs provider-only query embedders, validates persisted provider/model/dimension metadata without creating or repairing embedding stores, and keeps session writes on the existing narrow session-dedupe handle. Compatible note/code provider configurations share one query-provider instance. The shared MCP handler no longer owns a second fallback store in this mode.

Vector readiness is read-first. A new store handle probes the dimension-specific vec table and caches an existing table without executing `CREATE VIRTUAL TABLE IF NOT EXISTS`; a query-only reader returns `rzm index` remediation when the table is genuinely absent. Explicit write-capable indexing retains idempotent creation ownership.

Final Rhizome diagnostics observed zero note passes, repository walks, fallback store opens, schema statements, integrity checks, and index writes. Bounded result materialization truthfully reported 18 note reads and 3 code reads in the representative run; these scale with selected results rather than corpus discovery. Search/runtime/bootstrap stayed on the same response path; provider-call diagnostics report one call when compatible note/code configurations share a provider.

## Final before/after evidence

All comparisons used fresh one-shot processes, the same host, fixed query/path/limit, and the frozen v0.50.3 binary versus the final candidate binary. The first sample is reported inside the raw sample set rather than called a true OS-cold measurement.

| Corpus | Release median / p95 | Candidate median / p95 | Median change | Parity |
|---|---:|---:|---:|---|
| Rhizome (1,504 Markdown files; 449,736,704-byte index) | 3,175.460 / 3,826.922 ms | 1,111.371 / 1,143.356 ms | 65.0% faster | 20/20 deterministic; exact release hash `e846972be319e76dc3f703a0f32049a90b7801a4a497e6a6e5721c0df1b0d70f` |
| personal-vault temporary refreshed copy (2,772 Markdown files; 284,164,096-byte index) | 875.311 / 920.619 ms | 280.537 / 291.878 ms | 68.0% faster | 20/20 deterministic; exact release hash `cafffaaa27b933e94e921f5444a09f5a4701fb5ec1e5a2f725e3ddc490d2f18b` |

The final candidate SHA-256 was `7e567f8b374a863d8e12bcfe923c2d892d08e1625deddce06f77838bd2d20f0b`; the frozen release SHA-256 was `7093c612795630056724882f2fe715dc73783f69fb9208ee74cb39c4fc893d24`. The Rhizome first sample was 1,483.632 ms versus a 1,111.371 ms repeated median. Seven same-session samples measured 1,131.869 ms median / 1,153.735 ms p95 and retained one normalized identity. Explicit overview and precision cells each reproduced the same two normalized hash variants seen in the frozen release, so the optimization introduced no new mode-specific identity set; that pre-existing two-variant behavior remains a search determinism risk.

The original personal-vault index lacked the new durable indexer/scope proof and correctly returned `indexed-context-missing`; it was not mutated. The comparison copied the vault to a disposable directory, ran `rzm index --code` there, and then used that identical refreshed index for both binaries.

Residual latency is now retrieval-shaped rather than bootstrap-shaped. A representative instrumented run spent 3 ms in bootstrap/readiness, 240 ms in seed resolution, 838 ms in search, and 5 ms in shaping. Graph retrieval and the provider call dominate observed search time. KNN/vector overfetch, graph-query design, and deadline-lane concurrency remain separate quality-sensitive work; they are not required for this micro-release-sized improvement.
