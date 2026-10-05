---
type: TechnicalSpec
id: SPEC-0082
summary: "Defines a measured performance contract for one-shot rzm agent start: minimal code bootstrap by default, bounded index-backed rich context, batched session dedupe, capability-scoped runtime initialization, and latency-safe SQLite open behavior."
spec-status: active
last-updated: 2026-07-23
aliases:
  - SPEC-0082
  - agent-start-performance
---

# Agent start performance and bootstrap efficiency

## Summary

`rzm agent start` is the latency-sensitive handshake for an agent conversation. Its default code-repository path creates a session and returns only root plus explicitly targeted repository guidance. It does not need a fresh vault-wide note snapshot, graph analysis, ontology projection, coderef discovery, broad repository-document ranking, or code-index integrity scan. Those capabilities belong to explicit vault/ontology modes or later specialized commands.

The earlier contained and capability-scoped slices made startup cost observable and removed redundant work from minimal bootstrap. Pre-follow-on evidence showed that an explicit `--ontology` plus directory target rebuilt process-local note and coderef state: 470 note reads and 1,645 code reads consumed roughly 5.3 seconds even though the unified index already held the durable note, ontology, `doc_links`, and `intel_edges` projections. Rich start now reads bounded target-relevant state from the existing index, or degrades to minimal guidance with a structured indexing warning. It never repairs or refreshes the index synchronously. The exact immediately-before/final-after local timing remains the acceptance evidence needed to move US10-US11 from `ready`.

## Goals

- Make startup latency attributable through stable phase timings and environment-independent operation counters.
- Make default code bootstrap proportional to the small guidance packet it returns, with no vault-wide note, graph, ontology, coderef, code-index, or broad repository-document work.
- Keep vault-wide and ontology-rich onboarding available only when the caller explicitly requests those semantics.
- Exclude full-database integrity checking from the latency-sensitive start open while retaining it on existing indexing and maintenance opens.
- Reduce filesystem round trips by sharing note and ontology freshness work and eliminating redundant repository walks.
- Preserve concurrent session-dedupe correctness while committing only context items that were actually emitted and batching writes into bounded transactions.
- Let one-shot agent commands request only the runtime capabilities they need while preserving one shared bootstrap architecture for CLI, MCP, and serve mode.
- Make warm store paths fast and fail-safe, and avoid starting repository discovery capabilities that the requested operation will not consume.
- Measure the actual first and repeated local start experience without hiding the first invocation behind a discarded warm-up.
- Make explicit rich startup a read-only consumer of the unified index rather than an owner of note crawling, coderef discovery, or ontology refresh.
- Return only bounded target- and intent-relevant indexed enrichment, with deterministic behavior when the index is missing or stale.

## Non-Goals

- Supporting or performance-certifying SQLite WAL databases on virtiofs, FUSE, NFS, SMB/CIFS, 9p, or other rejected filesystems.
- Preserving vault graph statistics, authority ranking, communities, orphan lists, broad repository-document ranking, or ontology summaries in default code bootstrap.
- Adding a persistent parsed-note cache solely to keep global note behavior in the default start path.
- Adding a replacement integrity scheduler or new maintenance subsystem; existing `rzm index` and other normal store opens retain the integrity check.
- Weakening `synchronous`, WAL, locking, migration validation, future-schema rejection, unsafe-filesystem refusal, or integrity checking outside the explicit start-path exception.
- Weakening freshness for vault, ontology, coderef, or schema behavior that a caller explicitly requests.
- Removing stable ontology response fields or replacing target-relevant guidance with an empty bootstrap solely to improve a benchmark.
- Making a daemon, `rzm serve`, or cross-process IPC a prerequisite for fast one-shot commands.
- Caching final rendered context blobs across invocations.
- Automatically relocating a user's index without explicit configuration.
- Optimizing ontology graph construction that is not on the measured summary path; ontology type counts already use the indexed read model.
- Adding another note-ingest concurrency path; `rzm index` already uses parallel file processing and a serialized adaptive batch writer.
- Making `agent start` repair, refresh, or opportunistically write index state.

## User Stories

### US1 - Attribute agent-start cost with stable timings and operation counters

- id:: ^SPEC-0082-US1
- summary:: A maintainer can identify which startup phase and which class of filesystem or SQLite operation explains a slow agent start.
- status:: satisfied
- increment:: 1

#### Acceptance Criteria

- The shared agent-start response exposes an additive diagnostics object when `--timings` is requested, with stable labels for total time, runtime Search/Semantic/Code readiness, note crawl, coderef discovery/read, repo-doc inventory, vault context, targeted file context, ontology freshness/summary, session reserve/commit/cleanup, and store warm-open work; existing response fields and packed context remain unchanged. ^SPEC-0082-US1-AC1
- Diagnostics include operation counts sufficient to assert complete note passes, repository walks, code-content reads, SQLite schema-management statements, and session write transactions without relying on wall-clock thresholds. ^SPEC-0082-US1-AC2
- The CLI-only start wrapper aggregates measurements from the same runtime, handler, cache, packing, and store implementations used by MCP; it does not fork those behaviors or introduce an MCP-only/start-only retrieval path, and timing names/order follow the stable descriptor conventions established by [[indexing-observability-and-maintenance-policy|SPEC-0047]]. ^SPEC-0082-US1-AC3

### US2 - Build explicitly requested vault context from one shared note and ontology snapshot

- id:: ^SPEC-0082-US2
- summary:: Vault-wide or ontology-rich startup pays for at most one authoritative note snapshot, while ordinary code startup does not construct one.
- status:: satisfied
- increment:: 1

#### Acceptance Criteria

- A start that explicitly requests vault-wide or ontology-rich context performs at most one complete note stat/read/parse pass, and all consumers share the resulting immutable snapshot. ^SPEC-0082-US2-AC1
- The explicit ontology path no longer instantiates a raw note reader after cache readiness, and concurrent consumers observe one freshness computation and one ontology store/schema result without leaking a concrete runtime into packing or rendering layers. ^SPEC-0082-US2-AC2
- Default code startup and explicit directory targets do not construct the note snapshot; explicit vault, ontology, or note-oriented requests retain the shared-snapshot behavior. ^SPEC-0082-US2-AC3
- If measurement justifies bounded parallel note loading, filesystem read/parse work may run concurrently only behind a deterministic serialized publication boundary; output order, cache versioning, dirty retries, cancellation, and race-test behavior remain equivalent to the serial implementation. ^SPEC-0082-US2-AC4

### US3 - Batch session dedupe and record only emitted context

- id:: ^SPEC-0082-US3
- summary:: Concurrent agent starts preserve exactly-one-winner dedupe while omitted candidates remain eligible for a later response.
- status:: satisfied
- increment:: 1

#### Acceptance Criteria

- Session dedupe uses a batched reserve/commit contract: one short transaction ensures the session and conditionally reserves candidate keys, and one short transaction commits emitted top-level and nested keys while releasing reservations owned by the invocation but omitted by packing. ^SPEC-0082-US3-AC1
- Only committed items count as seen; a candidate admitted before packing but omitted by budget or compression is eligible on the next invocation, and nested items are committed only when their emitted parent includes them. ^SPEC-0082-US3-AC2
- Concurrent reservation retains exactly one winner per unchanged key; abandoned uncommitted reservations can be reclaimed after a documented bounded TTL, so a losing invocation may suppress an ultimately un-emitted key only for that bounded interval. ^SPEC-0082-US3-AC3
- A normal start uses at most four session write transactions including cleanup when cleanup is due; cleanup is cross-process due-gated and does not run on every one-shot bootstrap. ^SPEC-0082-US3-AC4
- Existing session rows migrate as committed/seen, migration and validation follow the intel-domain store contract, and regression tests cover concurrency, omitted candidates, stale-reservation reclaim, nested items, and transaction counts. ^SPEC-0082-US3-AC5

### US4 - Initialize only capabilities required by the requested agent operation

- id:: ^SPEC-0082-US4
- summary:: A one-shot agent start does not wait for semantic providers or embedding stores that its requested context does not use.
- status:: satisfied
- increment:: 2

#### Acceptance Criteria

- Runtime capability selection is explicit and typed; it preserves established store ownership and shared context propagation rather than requiring a second start-only database lifecycle. ^SPEC-0082-US4-AC1
- The existing runtime accepts a small requested-capability plan for cache/metadata/session, semantic search, and code embeddings; serve mode requests its full long-lived set while one-shot tools request only their declared needs, without creating a parallel bootstrap implementation. ^SPEC-0082-US4-AC2
- Code/session readiness no longer depends on semantic-provider construction or note-embedding readiness, and optional code-embedding initialization is outside the ordinary agent-start critical path. ^SPEC-0082-US4-AC3
- For the representative code-profile command without semantic retrieval, diagnostics show no semantic-provider or embedding-store work on the response critical path while output remains semantically equivalent. ^SPEC-0082-US4-AC4

### US5 - Omit unused repository-wide discovery on targeted starts

- id:: ^SPEC-0082-US5
- summary:: A targeted one-shot start performs no repository-wide coderef discovery when no response consumer requires coderef state.
- status:: satisfied
- increment:: 2

#### Acceptance Criteria

- The representative explicit-directory start reports zero repository-wide code-content reads and zero coderef discovery work. ^SPEC-0082-US5-AC1
- Capability selection, not a start-only cache, determines whether coderef discovery runs; operations that consume coderefs continue to request the authoritative discovery capability. ^SPEC-0082-US5-AC2
- Omitting coderef discovery introduces no inventory schema, invalidation protocol, or best-effort write path. ^SPEC-0082-US5-AC3
- Long-lived and one-shot runtimes use the same typed capability contract, and omitted capability readiness is reported as unavailable rather than falsely ready. ^SPEC-0082-US5-AC4

### US6 - Fast-path current SQLite schemas without losing fail-closed schema behavior

- id:: ^SPEC-0082-US6
- summary:: A warm current-schema open avoids repetitive DDL and exhaustive structural validation while retaining migration and structural-drift recovery.
- status:: satisfied
- increment:: 2

#### Acceptance Criteria

- Each migrated domain persists and reads a versioned schema-plan fingerprint; an exact match performs no no-op DDL replay or exhaustive table/column/index shape scan during ordinary warm open. ^SPEC-0082-US6-AC1
- The fast probe transaction compares migration version, dirty state, schema-plan fingerprint, and SQLite `schema_version`; future-schema databases remain fail-closed. Default opens retain integrity checking, while the explicit start-path exception is governed by US9. ^SPEC-0082-US6-AC2
- Any missing probe metadata, dirty or mismatched state, changed SQLite `schema_version`, or structural error falls back to one full ensure-and-validate pass before open succeeds; only an exact current tuple may skip validation. ^SPEC-0082-US6-AC3
- Tests assert a stable warm-open schema-management statement budget, migration/future-schema behavior, drift fallback, corruption behavior, and shared-handle lifecycle. ^SPEC-0082-US6-AC4

### US7 - Reproduce first and repeated local startup performance

- id:: ^SPEC-0082-US7
- summary:: A maintainer can measure the first bare start, repeated bare starts, and explicit opt-in paths without discarding the user-visible first invocation.
- status:: satisfied
- increment:: 3

#### Acceptance Criteria

- The checked-in benchmark harness records the first bare code start separately, then reports at least seven repeated bare samples with median, p95, diagnostics, and normalized output identity. ^SPEC-0082-US7-AC1
- The harness separately measures an explicit directory target and each retained opt-in startup mode so default-path improvement cannot be purchased by silently regressing requested functionality. ^SPEC-0082-US7-AC2
- Performance acceptance uses operation-count invariants plus before/after local wall time from the same documented environment; it does not require or claim Lima, virtiofs, or other VM benchmark evidence. ^SPEC-0082-US7-AC3
- The benchmark asserts the deliberate default output contract rather than requiring equivalence with the removed graph statistics and broad repository-document packet. ^SPEC-0082-US7-AC4

### US8 - Bootstrap code agents without vault-wide work

- id:: ^SPEC-0082-US8
- summary:: A code agent receives a session and directly relevant repository guidance without paying for global context it did not request.
- status:: satisfied
- increment:: 3

#### Acceptance Criteria

- Bare and ordinary code-profile start return the session id, surface pointer, root guidance, and stable response envelope without note crawl, graph analysis, ontology projection, broad repository-document ranking, coderef discovery, or code-index readiness. ^SPEC-0082-US8-AC1
- An explicit directory target adds only root, ancestor, target, and requested submodule-depth guidance through bounded path lookup; it performs no repository-wide note, code, or documentation inventory. ^SPEC-0082-US8-AC2
- Explicit vault, ontology, or note-oriented requests retain their documented richer behavior and visibly request the capabilities they consume; default start does not silently infer those modes from repository contents. ^SPEC-0082-US8-AC3
- An explicit code-file target does not trigger repository-wide coderef discovery during start; callers use `file-context` or code-intelligence commands when they need that evidence. ^SPEC-0082-US8-AC4

### US9 - Keep full integrity checking off the start critical path

- id:: ^SPEC-0082-US9
- summary:: Agent start opens only the session state it needs without scanning the complete unified database for corruption.
- status:: satisfied
- increment:: 3

#### Acceptance Criteria

- The store-open request used by agent start explicitly skips `PRAGMA quick_check`; the default store-open contract continues to run it for `rzm index` and other existing integrity-checking callers. ^SPEC-0082-US9-AC1
- The start-specific open still enforces filesystem safety, SQLite durability pragmas, migration ordering, future-schema rejection, schema-plan validation, and normal SQL error propagation. ^SPEC-0082-US9-AC2
- No replacement integrity scheduler, timestamp, background job, or new maintenance state is introduced as part of this effort. ^SPEC-0082-US9-AC3
- Diagnostics and regression tests distinguish skipped integrity work from successful integrity work rather than reporting an unrun check as zero-cost validation. ^SPEC-0082-US9-AC4

### US10 - Read explicit rich startup context from the existing index

- id:: ^SPEC-0082-US10
- summary:: An agent can request ontology-rich startup context without triggering a live note crawl or repository-wide coderef scan.
- status:: satisfied
- increment:: 4

#### Acceptance Criteria

- An explicit rich start reads note metadata, ontology summary data, code-to-note links, and code-intelligence edges from the existing unified SQLite index; it performs zero live complete note passes, note-content reads, repository walks, code-content reads, and coderef discovery work. ^SPEC-0082-US10-AC1
- `rzm index` remains the sole owner of discovery, parsing, coderef extraction, ontology projection, freshness updates, and integrity checking; `agent start` performs no synchronous repair, refresh, or best-effort index write. ^SPEC-0082-US10-AC2
- The indexed reader uses the existing store lifecycle and durable projections rather than introducing a second database, daemon requirement, process-local cache hydration, or persisted final-context blob. ^SPEC-0082-US10-AC3
- A code-file target can retain linked-note and rationale context through persisted indexed relationships with output-equivalence tests against representative current discovery results. ^SPEC-0082-US10-AC4

### US11 - Bound indexed enrichment to the requested activity

- id:: ^SPEC-0082-US11
- summary:: A rich start returns a small, relevant indexed packet for the requested path and intent, and remains usable when indexed enrichment is unavailable.
- status:: satisfied
- increment:: 4

#### Acceptance Criteria

- A directory target queries only persisted note/code relationships whose source or destination falls within the requested subtree, applies explicit result and byte budgets, and never materializes the repository-wide coderef graph. ^SPEC-0082-US11-AC1
- The request intent may rank already-indexed candidates through existing provider-free lexical and structural signals, but start does not initialize an embedding provider, create embeddings, or broaden beyond the target scope solely to satisfy intent ranking. ^SPEC-0082-US11-AC2
- Missing, incompatible, or stale required index state returns minimal root/ancestor/target guidance plus a structured warning that names the unavailable enrichment and recommends the appropriate `rzm index` command; startup does not fail solely because optional indexed enrichment is unavailable. ^SPEC-0082-US11-AC3
- Diagnostics distinguish bounded index reads, unavailable/stale indexed enrichment, and live discovery; the representative `--profile code --ontology --file docs/specs --submodule-depth 1` benchmark records the immediately preceding behavior and final behavior locally, with no Lima or VM benchmark requirement. ^SPEC-0082-US11-AC4

## Requirements

- Agent-start diagnostics MUST be additive, deterministic in label naming/order, non-interactive, and implemented through the shared agent handler so CLI and MCP cannot drift.
- Primary performance acceptance MUST use operation-count invariants and deliberate output-contract assertions; wall-clock comparisons MUST use an immediately preceding baseline on the same documented local environment.
- Session reservation and commit MUST use short batched transactions under the shared store mutex and MUST NOT perform packing, filesystem I/O, provider calls, or network work inside a transaction.
- Existing committed session rows MUST remain seen across migration; omitted-item eligibility is the only intentional dedupe behavior correction.
- Ontology freshness MUST remain authoritative when ontology or vault-wide context is explicitly requested; multiple consumers MUST share one result rather than independently refresh it.
- Capability planning MUST extend the existing live runtime and tool capability declarations rather than introduce a second one-shot runtime architecture.
- Default and directory-targeted code starts MUST omit note crawl, graph analysis, ontology projection, broad repository-document ranking, coderef discovery, and code-index readiness.
- Explicit code-file targets MUST NOT trigger repository-wide coderef discovery during start; specialized follow-up commands own code-reference retrieval.
- Current-schema fast paths MUST preserve migration ordering, future-schema rejection, filesystem safety, and one bounded full-validation retry on structural drift. The latency-sensitive start open MUST skip the full-database integrity check, while default/indexing opens retain it.
- The implementation MUST NOT change SQLite durability pragmas, support status for unsafe filesystems, or automatically move an index.
- The implementation MUST NOT require a daemon; opportunistic delegation to a compatible running process remains rejected until one-shot results demonstrate a need.
- Default context MUST intentionally contain only the stable response envelope, root guidance, and explicitly targeted ancestor/submodule guidance. Removed graph statistics, broad repo-document candidates, and inferred global context are not output-equivalence obligations.
- Explicit rich start MUST be a read-only consumer of existing unified-index projections and MUST NOT start the process-local note/coderef cache to satisfy its response.
- Indexed enrichment MUST be target-scoped and budgeted before rendering; repository-wide edge materialization is not an acceptable implementation of a bounded response.
- Missing, incompatible, or stale optional indexed enrichment MUST degrade to minimal guidance with a structured remediation warning rather than trigger synchronous indexing or make chat startup unusable.
- Index-maintenance concurrency MUST remain owned by the indexing pipeline. This follow-on MUST reuse its existing parallel file-processing and serialized-write boundaries rather than parallelize `pkg/vault/cache.Service.initialCrawl` for startup.

## Open Questions

None.
