---
summary: "B16 actual-method profiling, paired score replacement measurements, and atomicity evidence."
---

# Graph score replacement batching

Document and anchor score replacements now issue bounded multirow statements inside their existing transactions. The change shortens measured score persistence while keeping graph revision triggers, input ordering and atomic publication intact.

## Why this path

`runUnifiedPostIndex` calls both replacement methods after note/code changes; `refreshGraphScores` also calls them during live graph refresh. The previous implementation deleted the old table and executed one prepared insertion per accepted row. Ordinary unchanged indexing skips graph recomputation, so this is a refresh-write improvement, not another unchanged-index optimization.

The actual-method baseline used normal file-backed stores with production revision triggers. A separate 10,000-row CPU profile sampled 4.93 seconds: 86.41% cumulative through prepared-statement `ExecContext` and 96.75% flat in `runtime.cgocall`. The profile cannot distinguish SQLite internals from other CGO work; it supports examining statement execution shape, not attributing all cost to crossing the Go/C boundary.

## Preserved behavior

Both methods prepare arguments, filter blank keys, validate document types and check cancellation before acquiring the writer transaction. The transaction retains `withWriteTx`, the initial delete, input order and `INSERT OR REPLACE`. The existing helper limits statements to 900 parameters: 112 document scores or 300 anchor scores. All chunks commit together. Failed and canceled operations preserve prior complete rows and their graph fingerprint; committed replacements remain visible through another store handle.

There is no schema, trigger, public API or semantic no-op identity change. Preparing typed arguments before the transaction and SQL may change which error wins when multiple different invalid rows are supplied; isolated diagnostics and rollback remain. No contract promised first-error ordering for multiply invalid input.

## Paired measurements

Base source: `4d1963a7490df74ff9d3ec7039a3739c4cdae678`. Candidate adds only the two score-method changes to production. Benchmarks call the actual replacement methods with populated tables and production triggers at 100, 1,000 and 10,000 synthetic rows. They exclude setup, full-tuple verification and cleanup from timing; graph fingerprint advancement is checked outside timing. Normal store/SQLite settings and checkpoint behavior remain.

Five pairs alternate execution order, ten replacements per case, `GOMAXPROCS=2`, Apple M4 Pro / Darwin arm64. Both test binaries were built before the exclusive heavy-work lease. Baseline uses a Go overlay of the two original score source files, with the identical current instrumented benchmark source; candidate uses the corrected production methods. The exclusive lease covers all five pairs and excludes coordinated builds/tests/indexing. These fixture sizes are synthetic, not measured user inventories. Raw logs and binary hashes are retained in `/tmp/b16-corrected-paired/manifest.json`; reproducer `/tmp/run-b16-corrected-pairs.py`. Baseline profile artifacts are `/tmp/b16-baseline.cpu` and `/tmp/b16-profile-top.txt`.

| Replacement | Rows | Baseline median | Candidate median | Change |
| --- | ---: | ---: | ---: | ---: |
| Document scores | 100 | 0.326 ms | 0.293 ms | -10.2% |
| Document scores | 1,000 | 16.983 ms | 4.042 ms | -76.2% |
| Document scores | 10,000 | 183.055 ms | 53.353 ms | -70.9% |
| Anchor scores | 100 | 0.212 ms | 0.152 ms | -28.2% |
| Anchor scores | 1,000 | 11.725 ms | 2.130 ms | -81.8% |
| Anchor scores | 10,000 | 124.127 ms | 24.567 ms | -80.2% |

At 10,000 rows, measured writer hold falls from 183.052 to 52.241 ms for documents and from 124.124 to 24.140 ms for anchors. Total call time includes argument conversion; writer hold excludes it. These counters measure the existing lock interval, not inferred occupancy.

Allocation is the tradeoff. At 10,000 rows, document allocation rises from 4.72 to 7.90 MB per replacement and anchor allocation from 2.40 to 3.19 MB, while allocation counts fall from 119,573 to 71,007 and 89,842 to 40,414 respectively. These are allocated bytes, not peak RSS. The existing helper materializes arguments for the input; SQL chunks are bounded but argument construction is proportional to the full slice. The measured reduction in persistence time justifies this bounded-scope tradeoff. No end-to-end indexing, browser latency or production memory saving is claimed.

## Verification

The contract suite passed on both the baseline and candidate. It checks complete tuples, changed and empty replacements, mixed document types, blank rows, duplicate last-write behavior across chunk boundaries, invalid type and later-chunk timestamp failure rollback, pre-canceled calls, and cross-handle fingerprint changes. Boundary fixtures include 112/113 accepted document rows and 300/301 accepted anchor rows; skipped blank rows are additional inputs. A new regression holds the shared writer mutex and proves canceled empty/nonempty calls and invalid document input return before acquiring it. It failed against the original implementation and passes after correction. Per-row context checks cover conversion; the regression directly proves the pre-transaction failure boundary, without claiming a timed mid-conversion cancellation observation.

The original `8f4d3c45` measurements under `/tmp/b16-paired` are historical: review found argument preparation inside the writer lock. The accepted measurements above repeat all five pairs after moving preparation outside the transaction and adding cancellation checks. All ten corrected benchmark runs passed complete row and fingerprint assertions. Independent Standards and Spec reviews found zero issues. The corrected implementation passed full `make check`, including race-enabled unit/integration and web checks, then `make build`. Documentation validation reported zero issues.
