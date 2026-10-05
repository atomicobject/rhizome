---
type: ReferenceDoc
summary: "Evidence, causal analysis, storage guidance, and measurement plan for the reported 22–30 second rzm agent start latency under Lima and virtiofs."
reference-kind: analysis
derived-from:
  - docs/specs/technical/agent-start-performance.md
  - docs/efforts/2026-07-22-14-14-agent-start-contained-optimization.md
  - docs/efforts/2026-07-22-14-15-agent-start-capability-fast-path.md
  - docs/efforts/2026-07-22-21-30-agent-start-indexed-enrichment.md
last-verified: 2026-07-23
status: active
aliases:
  - agent-start-performance-2026-07-22
---

# Agent start performance analysis — 2026-07-22

## Executive summary

An early user's original representative command was:

```sh
rzm agent start \
  --profile code \
  --ontology \
  --file docs/specs \
  --submodule-depth 1 \
  --intent "Test out some rhizome stuff"
```

The native baseline for that exact rich command was five current-head samples from 1.770 to 1.962 seconds, with a 1.887-second median. After the contained and capability/schema slices, seven exact-command native runs measured 280.872 ms median and 284.319 ms p95 with equivalent normalized output—about 85.1% lower median latency at that checkpoint. Two early users reported roughly 22–30 seconds in Lima with checkout and SQLite on virtiofs, but Lima was unavailable here: no Lima median, p95, phase profile, syscall profile, or storage-placement matrix is claimed.

The final product contract no longer makes that ontology-rich command the default code bootstrap. The EFF-0066 local benchmark measured first bare start at 57.342 ms; repeated bare at 20.430 ms median / 21.252 ms p95; directory-targeted at 20.365 ms median / 22.009 ms p95; and the then-live-discovery explicit rich path at 5,586.667 ms median / 7,714.388 ms p95. Bare and directory-targeted samples had zero recorded note passes, repository walks, note/code reads, schema statements, integrity checks, SQLite transactions, and session operations.

EFF-0067 now routes explicit rich start through bounded read-only unified-index queries. It scopes file and directory candidates in SQL before provider-free intent ranking, enforces stable item and byte budgets, and returns structured fail-soft remediation when required indexed projections are unavailable. Tests and diagnostics prove that this path does not start the process-local note/coderef cache, embedding providers, live ontology projection, repair, or index writes. Its exact immediately-before/final-after local timing has not yet been run, so the EFF-0066 explicit-rich number above remains historical baseline evidence rather than a current-performance claim.

The seven optimized native samples were 283.544, 280.662, 276.277, 280.872, 284.319, 280.595, and 284.111 ms, measured from implementation commit `203ad918` after one discarded warm-up. The host environment was not pinned as a reproducible benchmark cell; these raw values support the native before/after result only.

The current implementation performs enough small filesystem and SQLite operations that a shared mount can plausibly turn a roughly two-second native command into a much slower VM command. The likely mechanism is round-trip amplification, not one isolated expensive algorithm: complete note discovery and reads, coderef candidate discovery and reads, a second repository-document inventory, repeated ontology freshness work, store-open schema checks, and per-item session writes all cross the host/guest boundary. SQLite WAL on virtiofs adds lock, shared-memory, journal, sync, and metadata traffic to the same slow path and is also an unsupported topology.

The immediate supported mitigation is to put the SQLite index on the Lima guest's local filesystem by setting an absolute guest-local `indexPath`. A VM-local checkout is preferable when practical because it removes both source traversal and database traffic from virtiofs. `RHIZOME_SQLITE_ALLOW_UNSAFE_FS=1` is not a performance or correctness fix: it bypasses Rhizome's refusal only.

This analysis supports [[../../specs/technical/agent-start-performance|SPEC-0082]], the contained measured optimization in EFF-2026-07-22-14-14, and the measurement-gated architectural work in EFF-2026-07-22-14-15.

## Evidence ledger

| Claim | Status | Evidence and limitation |
|---|---|---|
| Exact native command takes 1.770–1.962 seconds, median 1.887 seconds | Measured fact | Five current-head native samples. The run did not include stable phase diagnostics, a pinned machine manifest, or p95-quality sample volume. |
| Lima execution takes roughly 22–30 seconds | User-reported observation | Reported by two early users for source and SQLite on Lima virtiofs. It has not been reproduced in this worktree and must not be treated as a measured median or p95. |
| Current vault bootstrap sees 469 notes, 271 typed notes, and 18 ontology types | Measured fact | A 2026-07-22 native `rzm agent start` bootstrap in this worktree returned those vault-context counts. The concurrently produced ontology summary reported 470 total notes, demonstrating that live worktree counts can move and are not a frozen benchmark corpus. |
| The repository contains 4,256 tracked files and 985 filesystem-visible Markdown files at inspection time | Measured inventory fact | Direct current-worktree counts. These are corpus-size context, not the exact number of files opened by the representative command. Generated, ignored, and untracked files affect filesystem-visible totals. |
| Virtiofs round trips and WAL traffic explain the slowdown | Causal inference | Strongly consistent with the implementation's repeated passes and SQLite access pattern, but attribution between source reads and database traffic requires the 2×2 placement matrix and phase/operation diagnostics. |
| Moving only the index to guest-local storage will materially help | Expected result, not yet measured here | It removes unsupported WAL-on-virtiofs traffic. The remaining source-on-virtiofs cost may still be significant, so the result must be measured rather than promised. |
| Optimized exact native command completes in 280.872 ms median and 284.319 ms p95 | Measured fact | Seven post-warm-up samples with normalized output equivalence. This is about 85.1% faster than the 1.887-second native baseline; it is not a Lima measurement. |
| Final first bare start completes in 57.342 ms | Measured fact | One deliberately retained first invocation under the `agent-start-v2` minimal output contract. |
| Final repeated bare starts complete in 20.430 ms median and 21.252 ms p95 | Measured fact | Seven local samples; all minimal operation counters were zero and normalized output was stable. |
| Final directory-targeted starts complete in 20.365 ms median and 22.009 ms p95 | Measured fact | Seven local samples with bounded root/ancestor/target/submodule guidance and zero repository-wide operation counters. |
| EFF-0066 explicit-rich starts completed in 5,586.667 ms median and 7,714.388 ms p95 | Historical measured fact | Seven local samples of the former live-discovery rich path. EFF-0067 replaces that path with bounded indexed reads; its exact immediately-before/final-after timing is still pending. |

Operation counts are absent from the original baseline because the pre-instrumentation response exposed only total `durationMs`. The final exact-command runs report zero code-content reads, zero coderef work, zero broad repository-document ranking, one repository walk, two schema-management statements, and five instrumented startup transactions. The transaction counter covers the schema probe plus session reserve/commit work; it is not a count of every SQLite read, autocommit, or pragma performed by the process.

## End-to-end startup path

The command is a composite bootstrap, and several phases overlap. The useful model is an ownership and I/O sequence rather than a promise that every phase executes serially:

1. **Resolve configuration and construct the live runtime.** The CLI builds the shared runtime used by agent handlers rather than a lightweight command-specific reader.
2. **Reach Search readiness.** Runtime startup opens the SQLite-backed stores, applies filesystem safety checks, performs integrity/migration/schema work, and makes the note/cache layer available.
3. **Reach Semantic and Code readiness.** The existing readiness chain can construct semantic-provider and embedding-related capabilities even when this exact start request does not perform semantic retrieval. Code readiness currently follows Semantic partly because store-handle ownership is shared through that path.
4. **Build the note cache.** `EnsureReady` discovers Markdown notes, stats and reads changed entries, parses them serially, publishes them into shared indexes, loads ignore inputs, and discovers/reads coderef candidates.
5. **Build vault context.** The shared `vault_context` handler constructs profile context, root guidance, relevant note material, session-aware context candidates, and the explicit `docs/specs` target context.
6. **Inventory repository documents.** The historical code-profile path walks repository documentation again to rank broad guidance, even though the explicit target already defines a strong context boundary.
7. **Ensure ontology freshness and summarize it.** Targeted file context and the explicit `--ontology` summary historically use separate freshness paths; the explicit summary can instantiate a raw note reader after the cache is ready, repeating note stat/read work.
8. **Apply session dedupe and pack the response.** Historical `Allow` and `MarkSent` calls write individual session items around packing. Admission can mark an item seen even if the budget later omits it.
9. **Serialize the response.** The CLI returns the session id, total duration, packed vault context, and stable ontology fields.

The phase list introduced by [[../../specs/technical/agent-start-performance|SPEC-0082]] makes these costs visible as total, runtime Search/Semantic/Code readiness, store warm-open, note crawl, coderef discovery/read, repository-document inventory, vault context, targeted file context, ontology freshness/summary, and session reserve/commit/cleanup. Some spans run concurrently, so their durations are not expected to sum to total wall time.

## Why virtiofs amplifies this workload

Native filesystem work often hides an inefficient number of small operations because metadata and file contents are served with low latency and aggressive host caching. Virtiofs makes the VM cross a virtualization boundary for source-mounted operations. Directory walks, `stat` calls, opens, small reads, and metadata lookups can each require host/guest coordination. Serial loops are especially sensitive: latency accumulates one round trip at a time instead of being hidden behind useful parallel work.

This command's source-side pattern is therefore unfavorable even when total bytes are modest:

- note discovery performs a complete pass before context is built;
- note refresh performs per-path metadata checks and reads;
- coderef discovery walks configured source candidates and reads their content;
- broad repository-document selection performs another walk;
- a raw ontology reader can repeat note inventory and content work after cache readiness.

SQLite WAL intensifies the problem when the database also resides on virtiofs. A correct WAL database depends on coherent locks and shared memory and produces many small, ordered operations: open and metadata probes, lock transitions, `-wal` appends, `-shm` coordination, transaction boundaries, and durability syncs. Schema ensure/validation at warm open adds SQL and metadata chatter, while historical per-item session dedupe creates a transaction storm. Virtualized latency is paid on ordering-sensitive operations that cannot all be collapsed by page cache or parallelized safely.

This is both a performance issue and a correctness boundary. Rhizome rejects known shared and network filesystem types for SQLite WAL, including virtiofs, FUSE, NFS, CIFS/SMB, and 9p. The analysis does not certify any of those filesystems merely because a benchmark cell can be run diagnostically.

## Immediate Lima configuration

Keep the checkout mounted from the host if that workflow is necessary, but place the index at an absolute path on the guest's local disk:

```yaml
indexPath: /home/<guest-user>/.local/share/rhizome/<project>/index.sqlite
```

The exact directory is user/environment specific. It must exist in the guest-local filesystem and be writable by the guest user. Do not point it back through `/mnt`, the Lima shared mount, or another virtiofs/FUSE path.

Prefer a VM-local checkout as well when practical. The four relevant storage cells are:

| Source | SQLite database | Interpretation |
|---|---|---|
| local | local | Native/reference cell |
| virtiofs | local | Supported practical Lima cell and primary optimization target |
| local | virtiofs | Diagnostic only; SQLite topology remains unsupported |
| virtiofs | virtiofs | The reported slow shape; diagnostic and unsupported |

`RHIZOME_SQLITE_ALLOW_UNSAFE_FS=1` suppresses only the startup refusal. It does not add coherent locking or shared-memory behavior, change SQLite durability pragmas, make WAL safe, improve performance, or convert an unsupported deployment into a supported one. It is suitable only as an expert diagnostic escape hatch with a disposable/recoverable index.

## Behaviors removed, preserved, and deferred

The contained slice deliberately removes low-value work only where the request already supplies a precise target:

- **Remove:** broad repository-document ranking for a start with `--file` or `--context-file`.
- **Preserve:** root agent guidance plus documentation for the target, its ancestors, and the requested submodule depth.
- **Preserve:** stable ontology response fields. The indexed type-count summary is part of the agent API and was not shown to be the dominant cost.
- **Preserve:** authoritative note, ontology, coderef, and schema freshness. The optimization shares or scopes work; it does not silently serve stale final context.
- **Preserve:** context ordering and content for a fixed fixture, except the deliberate targeted-context narrowing and the correctness fix that budget-omitted candidates are not committed as seen.
- **Preserve:** SQLite WAL durability, locking, integrity checks, migrations, future-schema rejection, and unsafe-filesystem refusal.
- **Keep serial:** final native note crawl measured 195–204 ms and no Lima attribution was available; bounded parallel reads were not justified. Publication remains deterministic and serialized.
- **Deliver:** typed capability-scoped runtime startup and validated warm-schema fingerprints. The representative start skips semantic/code embeddings, anchor warmup, leader syncers, and coderef discovery.
- **Drop:** persisted coderef inventory and unified-handle restructuring. Once unused coderef discovery was omitted, an inventory had no critical-path consumer; existing context-aware store ownership was sufficient for the schema proof.
- **Reject:** a mandatory daemon, cached rendered context blobs, pragma weakening, automatic index relocation, and speculative ontology graph optimization.

## Baseline limitations

The five native samples answer only “how long did this command take on one unpinned native environment?” They do not isolate cold versus warm behavior, establish a meaningful p95, record CPU/storage/background-load state, prove output equivalence, or attribute time by phase. The original command also lacked operation counters, so no exact baseline count exists for note reads, code reads, schema statements, or session transactions.

The reported Lima range has larger gaps: no checked-in environment manifest, Lima version/configuration, guest CPU or memory allocation, mount options, source/database filesystem probes, raw samples, discarded warm-up convention, phase diagnostics, `strace` summary, or normalized output hashes. It is valid symptom evidence and sufficient to prioritize investigation, but not sufficient to claim a measured improvement.

Current corpus counts are not frozen. This worktree is active, the note inventory changed during inspection, and unrelated implementation is landing concurrently. Future performance comparisons must pin the commit, index state, configuration, and environment rather than compare against the counts in this dated analysis by assumption.

## Optimization and measurement plan

### Slice 1 — contained, correctness-preserving work

1. Add opt-in `--timings` output with stable phase labels and operation counters through the existing shared collector.
2. Check in a benchmark harness that runs at least one discarded warm-up followed by seven measured samples, reports median and p95, normalizes volatile output fields, and proves output equivalence.
3. Measure all available cells in the 2×2 source/database placement matrix. Mark unavailable cells honestly rather than substituting estimates.
4. Share one runtime-owned cache-backed ontology freshness result between targeted context and explicit ontology summary.
5. Suppress broad repository-document ranking for explicit targets while retaining root and target/ancestor/submodule guidance.
6. Replace per-item session writes with one reservation transaction and one emitted-only commit/release transaction. Keep committed history separate from temporary reservations and due-gate cleanup.
7. If the pinned Lima profile still attributes material residual time to note loading, split immutable read/parse from deterministic ordered publication and test bounded read workers. Otherwise record a no-change decision.
8. Re-run native and Lima cells immediately before and after the slice. Acceptance is at least a 25% median improvement in the pinned source-virtiofs/database-local cell with no more than a 10% p95 regression in the pinned native cell.

### Slice 2 — measurement-gated architecture

Proceed with EFF-2026-07-22-14-15 only when Slice 1 diagnostics leave material cost in the matching phase:

- capability-scoped runtime initialization when unused Semantic or embedding readiness remains on the critical path;
- one unified database owner/handle and shared write mutex when duplicated store initialization or ownership ordering remains material;
- a safely invalidated persisted coderef discovery inventory when repository-wide code reads remain material on warm start;
- versioned schema-plan fingerprints when no-op DDL and exhaustive structural validation remain material at warm open.

Each optimization needs an operation-count invariant in addition to elapsed time. Warm inventory should produce zero repository-wide code-content reads; ordinary session handling should use two write transactions and at most four when cleanup is due; current-schema warm open should stay within a stable schema-statement budget; and output-equivalence checks must stay green.

### Required benchmark record

For every published result, retain:

- exact built commit and representative command, including `--timings`;
- host and guest OS, architecture, Lima version/configuration, CPU and memory allocation;
- source and database absolute paths plus probed filesystem types and mount options;
- configuration and ignore/discovery inputs, index state, cold/warm classification, and background-load notes;
- one discarded warm-up and at least seven raw measured samples per available cell;
- median, p95, phase diagnostics, operation counters, and normalized output hash/equivalence;
- one `strace -c -f` run in Lima before optimization when the reporting user's environment is available.

The target at the end of Slice 2 is p95 at or below five seconds for the pinned source-virtiofs/database-local command. If evidence shows that target is infeasible without weakening correctness, the affected story returns to `ready`; the durability or freshness contract is not weakened to make a graph look green.

## Delivered result

The implementation first reduced the original exact rich command from a 1.887-second median to 280.872 ms at the capability/schema checkpoint. It then changed the ordinary code-start contract so unrequested global work is not performed at all. The final local result is 57.342 ms for the retained first bare invocation and 20.430 ms median / 21.252 ms p95 for repeated bare starts; directory-targeted starts are comparably bounded at 20.365 ms median / 22.009 ms p95. Minimal operation counters are all zero.

Explicit rich startup remains available, but its implementation now consumes bounded target-relevant projections from the existing unified index instead of rebuilding note and coderef state. On the exact requested command, the immediately preceding binary took 6,079.205 ms wall time (`durationMs: 6058`) and emitted 25,908 bytes. The review-fixed final build took 620 ms wall time (`durationMs: 28`) and emitted 6,533 bytes while returning indexed enrichment as `available` with 5 bounded reads and 12 results and ontology state as available/ready. This is an 89.8% wall-time reduction, or 9.8x faster. Exact-shape regression and harness diagnostics prove zero live note passes, note/code reads, repository walks, coderef discovery, index repair, and index writes. No Lima/virtiofs measurement was performed or required; the historical 22–30 second report remains unverified context, not a claimed benchmark result.
