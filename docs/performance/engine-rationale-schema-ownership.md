---
summary: "B18 schema-ownership proof and actual atomic-persistence measurements for rationale FTS."
---

# Rationale FTS schema ownership

Rationale search, replacement and deletion now use the schema established by store initialization. They no longer repeat permanent-schema creation statements during ordinary operations. The schema builder remains in migration/reset assembly, and schema validation now checks the exact FTS5 definition, rowid-map columns and required index.

## Scope and proof

The operation-path change removes three calls to `ensureRationaleFTSSchema` from `rationale_store.go`. Review also exposed missing concrete validation for the rationale FTS virtual table; the correction strengthens initialization validation and invalidates older schema proofs. It retains query sanitization, SQL, rowid-map maintenance, deletion order, migrations and transaction boundaries. Missing schema after a successful open surfaces an ordinary SQL error; an operation no longer attempts repair. Supported initialization and recovery remain authoritative. The stronger validator compares the code-owned FTS5 definition, including column order, `rationale_id UNINDEXED` and the porter tokenizer, and requires the rowid-map index. The schema validation fingerprint changes while Intel schema version remains 62.

Before the change, each nonblank rationale batch path reached the helper three times: initial FTS deletion, replacement and its nested deletion. The helper issues three schema statements, giving nine attempts per path even with empty rationale. This is source-derived work count, not a traced syscall count. The redundant nested deletion itself remains outside this change.

Tests verify schema presence before the first rationale operation after fresh, warm, external-handle and obsolete-v55 reset opens. A SQLite authorizer rejects main-schema CREATE statements while permitting temporary batch tables. All five tested runtime paths failed before the fix: search, direct replacement, populated batch, empty batch and deletion. They pass after the fix. Read-only search returns the seeded records without changing `schema_version`.

Atomic persistence snapshots now include rationale FTS and rowid mappings, extending existing injected-failure and cancellation tests beyond primary rationale rows. There is no schema version bump because the persisted schema is unchanged. The new validation fingerprint invalidates older proofs. The normal `OpenReadOnly` path refuses an old proof until a writable open revalidates it. A separate snapshot opener that performs full structural validation instead of trusting the proof can establish compatibility without rewriting a sealed database; that cross-batch behavior requires its own integration evidence. Healthy schema retains its data; concrete drift follows the existing derived-Intel reset/recreation path. Schema damage after an already-successful open remains an operation-time SQL error.

The new regression first reproduced four accepted-but-invalid current databases: missing FTS, a plain-table lookalike, wrong FTS shape/tokenizer and a missing rowid-map index. Each fixture has an old proof matching the observed SQLite schema version. After correction, read-only opens refuse the stale proof; writable opens detect drift, reset/recreate the derived Intel domain, and establish a proof that a subsequent read-only open accepts. This is concrete open-path evidence, replacing the earlier mistaken inference from broader table lists.

## Actual-method measurements

Base source: `4d1963a7490df74ff9d3ec7039a3739c4cdae678`. The measured candidate changes only the three runtime calls in production. The subsequent validation correction affects store open and proof validation, while these steady-state operation bodies and authoritative schema DDL remain unchanged. Its extra full-validation query and recovery behavior are outside the timed loop; no store-open speedup or unchanged open cost is claimed. The final matched binaries share benchmark/tests; a Go overlay restores the original rationale source for the baseline. Five alternating pairs run under the exclusive heavy-work lease, with five replacements per case and `GOMAXPROCS=2` on Darwin arm64 / Apple M4 Pro.

The fixture uses the real complete `ApplyCodePersistenceBatch`, including symbols, references, external evidence, anchors and FTS. Rationale batches remain present in both variants, with either empty slices or one rationale per path. Stores are opened, seeded and inputs constructed outside timing. After timing, checks compare canonical tuples, table counts, FTS/map ID and field joins, and actual search results. No triggers or normal SQLite behavior are disabled.

| Rationale | Paths | Baseline median | Candidate median | Change |
| --- | ---: | ---: | ---: | ---: |
| Empty | 1 | 1.003 ms | 0.966 ms | -3.7% |
| Empty | 128 | 18.002 ms | 16.509 ms | -8.3% |
| Empty | 512 | 102.098 ms | 96.638 ms | -5.3% |
| Populated | 1 | 1.055 ms | 1.109 ms | +5.2% |
| Populated | 128 | 20.320 ms | 18.942 ms | -6.8% |
| Populated | 512 | 112.920 ms | 106.010 ms | -6.1% |

The small one-path result is mixed and does not establish an improvement. At 128–512 paths, complete persistence is about 5–8% faster in this fixture. At 512 paths, allocated bytes fall from 8.394 to 7.988 MB for empty rationale and from 9.351 to 8.947 MB for populated rationale. These are synthetic store-level measurements, not end-to-end indexing or browser latency.

Final raw logs, hashes and order: `/tmp/b18-final-paired/manifest.json`; reproducer `/tmp/run-b18-final-pairs.py`. Earlier `/tmp/b18-paired` samples preceded the additional FTS-to-map join assertion and are retained as preliminary evidence. The historical pre-B02 17 ms empty-rationale observation is not a current estimate and is not used here.

## Verification

The focused rationale, schema-ownership, read-only and atomic persistence tests pass. Initial independent Standards and Spec reviews found zero issues, but hosted review later found that the concrete validator omitted the FTS table. Fresh independent Standards and Spec reviews of the concrete validator, fingerprint and supported open/recovery paths found zero issues. All ten final paired runs pass the strengthened parity checks. The initial implementation passed full `make check`, including race-enabled unit/integration tests and web checks, followed by `make build`; documentation validation reported zero issues. The corrected validator and compatibility tests also passed full `make check`, `make build` and documentation validation with zero issues.

Hosted Windows CI at `d6513cfb` exposed an unchanged queue-test scheduling gap: the test released backpressure after a delay without proving the submit goroutine had entered its measured call. The SQLite package passed. The fixture now waits for the existing submit-attempt barrier before its original backpressure check; production queue behavior and the metric assertion are unchanged. The focused test passes 50 repetitions and 10 race-enabled repetitions, and independent review found zero issues.
