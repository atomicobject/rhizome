---
type: ReferenceDoc
summary: "Before, midpoint, and after evidence for indexed seed resolution, persisted graph loading, bounded materialization I/O, and sqlite-vec KNN in agent semantic-query."
reference-kind: analysis
last-verified: 2026-08-05
status: current
---

# Semantic-query deep performance, 2026-08-05

## Conclusion

The four EFF-0071 changes reduce the representative Rhizome query from 1,000.190 ms to 393.321 ms median, a 60.7% improvement on top of EFF-0076, with exact ordered-result/lane/warning/content parity. The filesystem-sensitive midpoint accounts for 489.523 ms; bounded sqlite-vec KNN removes another 117.346 ms.

The same filesystem work is substantial on the Markdown-heavy comparison: 1,043.720 ms falls to 551.016 ms at midpoint. KNN is neutral there because the vector span is almost entirely the roughly 194 ms remote provider call and contains little local scoring work.

## Method

- Every checkpoint runs 20 fresh `rzm` processes with repository delegation disabled and `--timings` enabled.
- Rhizome query: path `pkg/app/mcp`, query `semantic query transaction invariants identifier mappings`, limit 20.
- Rhizome corpus: a frozen temporary copy of this worktree containing 1,505 Markdown files and a 450,412,544-byte unified index. `graph_doc_scores` was cleared in the copy to preserve the confirmed production precondition that triggered live graph fallback; raw note metadata/edges and all vectors remained intact.
- Markdown-heavy query: path `Clippings`, query `agentic coding software architecture`, limit 20.
- Markdown-heavy corpus: a refreshed temporary copy of a personal Markdown vault containing 2,772 Markdown files and a 284,164,096-byte unified index. The source vault was not mutated.
- `before` is EFF-0076 behavior built from branch HEAD before EFF-0071 changes. `midpoint` adds indexed directory seeds, persisted raw-graph loading, truthful live-walk diagnostics, an eight-worker I/O pool, and request-scoped physical-read dedupe. `after` adds prefiltered sqlite-vec KNN with deterministic tie handling.
- Normalized parity compares mode, target status, ordered type/path/symbol/start-line/role identities, lanes, and warnings. A post-run replay also hashes candidate counts and user-visible titles/previews/content fields. All 120 measured outputs were internally stable, and both parity hashes match across the three checkpoints within each corpus.
- The artifact records base revision `74e755fd29103d9da6c02de35787b6888dd39ba3`, checkpoint source-state labels, binary SHA-256 values, and SHA-256 fingerprints of both frozen SQLite indexes.
- Wall time is observational and machine-sensitive. Operation counts, query-plan tests, scalar/KNN parity, and normalized results are the deterministic gates.

Raw samples, binary manifests, corpus fingerprints, diagnostics, and normalized hashes are in [semantic-query-deep-performance-2026-08-05-data.json](semantic-query-deep-performance-2026-08-05-data.json).

## Before / midpoint / after

### Rhizome

| Checkpoint | Median | p95 | Seed | Search | Vector | Provider inside vector | Graph | Shape | Normalized hash |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Before | 1,000.190 ms | 1,298.197 ms | 190.0 ms | 762.5 ms | 395.0 ms | 196.5 ms | 306.0 ms | 4.0 ms | `e846972b…` |
| Midpoint | 510.667 ms | 557.913 ms | 0.0 ms | 467.5 ms | 397.0 ms | 197.0 ms | 13.0 ms | 4.0 ms | `e846972b…` |
| After | 393.321 ms | 466.101 ms | 0.0 ms | 344.5 ms | 270.5 ms | 198.0 ms | 14.0 ms | 5.0 ms | `e846972b…` |

The after run includes a 1,310 ms first sample; the other 19 samples range from 372 to 466 ms. The table's p95 follows the harness's repeated-sample percentile contract, while the raw list preserves the first-process evidence.

### Markdown-heavy comparison

| Checkpoint | Median | p95 | Seed | Search | Vector | Provider inside vector | Graph | Shape | Normalized hash |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Before | 1,043.720 ms | 1,200.526 ms | 30.0 ms | 977.5 ms | 194.5 ms | 193.5 ms | 690.0 ms | 3.0 ms | `dadfe2d3…` |
| Midpoint | 551.016 ms | 623.804 ms | 0.0 ms | 514.0 ms | 197.0 ms | 196.0 ms | 224.5 ms | 3.0 ms | `dadfe2d3…` |
| After | 551.443 ms | 670.268 ms | 0.0 ms | 513.5 ms | 197.0 ms | 196.0 ms | 225.5 ms | 3.0 ms | `dadfe2d3…` |

## Attribution

### Directory seed discovery

The former seed resolver obtained a default `obsidian.Note` and called `GetNotesList` for a directory. That path executes Markdown discovery over the entire vault. Its old counter reported zero because instrumentation did not wrap this call site. The replacement asks the managed store for at most 60 ordered note paths under the normalized prefix. Indexed-only semantic-query never falls back to the live reader; ordinary live callers retain their previous fallback and now increment the repository-walk counter truthfully.

### Graph expansion

The Rhizome and comparison indexes had no usable `graph_doc_scores`, so `GraphRetriever` fell back to `ComputeGraphAnalysis`, which rebuilt a graph by discovering and reading every note. Indexed-only semantic-query now loads the ready persisted raw note rows, tags, and link edges and feeds the same canonical `GraphSnapshot` into the same `ComputeGraphAnalysisFromSnapshot` algorithm. This bounded contract intentionally trusts persisted readiness rather than proving live note freshness. Rhizome graph cost falls from 306 to 13 ms. The 2,772-note comparison still spends about 225 ms in the graph algorithm, but no longer pays the live filesystem traversal.

### Body and preview reads

Materialization was already selection-driven and concurrent, but its CPU-derived worker count becomes one on a two-vCPU guest. It now uses up to eight I/O workers based on selected task count. A synchronized request cache ensures body, excerpt, and preview consumers perform one physical read per canonical file path; diagnostics count physical cache misses. Native shaping remains 3-4 ms, so this is principally a Lima/slow-filesystem resilience improvement rather than a native benchmark win.

### Vector KNN

The previous query invoked `vec_distance_cosine` for every dimension-compatible row and asked SQLite to sort the full scored relation. The replacement uses the existing vec0 table's exact `embedding MATCH ? AND k = ?` plan. Filtered searches materialize eligible chunk IDs using the existing owner/path/type joins and push that set into vec0 through `chunk_id IN (...)`; this avoids lossy post-filter overfetch.

KNN probes beyond the requested limit to resolve equal-distance boundaries deterministically, doubling up to vec0's 4,096-row cap. It uses the scalar reference only for a request at/above the cap or an unresolved tie at the cap; arbitrary KNN errors propagate. Tests compare KNN with the scalar reference across every filter family, combined filters, an adversarial globally-nearest-but-ineligible corpus, and a 48-way tie. `EXPLAIN QUERY PLAN` proves the vec virtual-table KNN plan is selected.

On Rhizome, the vector median drops 126.5 ms while the provider portion is effectively unchanged, implying the local vector SQL portion falls from roughly 198.5 ms to 72.5 ms. The final diagnostic reports two KNN queries per request, zero tie retries, and zero scalar fallbacks. On the Markdown-heavy query, provider time already accounts for essentially the entire vector span, so KNN has no material isolated benefit.

## Residual costs and decisions

- The remote query embedding is now the largest stable Rhizome component at about 200 ms. A durable cross-process query cache would add invalidation, privacy, and storage policy and is not a micro-release follow-up by default.
- The persisted graph algorithm remains about 225 ms on the 2,772-note comparison. Precomputed graph scores/communities or an algorithmic optimization could be material, but it needs parity and index-freshness design beyond this bounded query-path change.
- First-process/cold variance remains visible. A Lima run with `--timings` can now distinguish seed discovery, indexed snapshot load/analysis, KNN vector work, physical result reads, and any retained live filesystem fallback.
- No index schema migration is required. Existing vec0 tables and raw note graph projections are reused, keeping this suitable for a micro release after ordinary review.
