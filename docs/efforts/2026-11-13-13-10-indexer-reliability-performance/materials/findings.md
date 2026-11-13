# Indexing findings, 2026-11-13

Source: a TLA+ model of the live indexer, four replay tests on the real runtime, a batch-versus-live code read, and a throughput measurement with the real Voyage client pointed at a local fake server. Repository: Rhizome (Go), checkout `/Users/colthorp/.t3/worktrees/rhizome/t3-6de0a005-2c77ffec97a32eee`, commit `d581e3487`.

Evidence grades: **replayed** (a test on the real runtime reproduced it), **measured** (numbers from a harness), **read** (both code paths traced, not executed), **plausible** (depends on something not fully traced).

The owner's goals: batch (`rzm index`) and live (the vault runtime's watcher) are consistent; the live indexer is very reliable and robust; both are fast up to the limit of embedding API calls. A standing owner rule: text already embedded with the same provider, model and dimensions is never sent to the provider again.

## How the live path works (for orientation)

- `pkg/app/bootstrap/live_ownership.go`: the watcher loop drains pending file events into one "watcher batch" lane job. The batch calls the shared `indexcore.Discover` and `indexcore.Publish` (`pkg/app/indexcore/`), the same structural publisher batch uses.
- `indexcore.Publish` writes durable "derived work" tickets (`pkg/store/derived_work.go`; kinds notes, ontology, code, graph; path-scoped or global) before it mutates structure, and activates them after.
- `pkg/app/bootstrap/derived_scheduler.go`: a scheduler loads up to 32 ready tickets every 250 ms and executes them one after another. Each ticket runs several separate lane jobs (fingerprint, plan, one or more publication batches, acknowledgement) and computes at the provider outside the index lock.
- `pkg/app/bootstrap/lane/`: one serialized executor; each job acquires `.rhizome/index.lock`. Background jobs yield to explicit `rzm index` jobs and to external priority requests from interactive writers (saves, repairs, CLI mutations).
- Batch (`pkg/app/indexing/unified.go` `RunUnifiedCore`) does the derived work inline with its own orchestration and acknowledges the tickets at the end.

## What holds

In the model (5,879 states, with restarts and `rzm index`): a quiet live indexer has an index that matches the file and embeddings that match the index; embeddings are never published for text the index does not hold. No modelled path loses a change. That depends on every restart and every interrupted batch running a whole-vault pass.

## Consistency findings

| ID | Finding | Grade | Evidence |
|---|---|---|---|
| C1 | `rzm index` delegated to a live runtime uses the note selection the runtime started with. After a live `notes.includes`/`excludes` change the watcher is right, then `rzm index` re-adds excluded notes (and embeds them) or drops newly included ones; the watcher does not repair it. | replayed, both directions | `cmd/serve_index_job_hooks.go:59` passes `VaultDef: rt.VaultDef`; `rt.currentOwnershipVaultDefinition()` holds the reloaded one (`pkg/app/bootstrap/live_ownership_policy.go:32`). |
| C2 | Live never writes code-to-note `doc_links`. | read, spot-checked | Batch builds a linker (`pkg/app/indexing/unified.go:167-181`); the runtime anchor service has none (`pkg/app/bootstrap/live_code.go:156-163`); links are computed only with a linker (`pkg/anchors/batch.go:168-170`, `:445-458`). `doc_links` feed code-embedding context (`pkg/embedindex/doc_context.go:63`), the code derived witness, rename reconciliation and graph endpoints. A later incremental `rzm index` probably does not repair it because the file hash already matches (not traced). |
| C3 | In a vault with note embeddings on and no ontology schema, the live ontology ticket fails forever. | measured (9 attempts in 60 s, never drains) | `derived_scheduler.go:409` returns the `ErrNoOntologyFiles` error from `LoadSchema`; batch treats it as "no schema" (`unified.go:219-220`). Every path-scoped ontology ticket in that vault fails the same way. Retries back off to 16 s; the leftover ticket also makes every later `Discover` report ontology recovery. |
| C4 | Only batch stamps the scope-config hash. After a live selection change, indexed-context freshness says "run rzm index" until someone does. | read | `pkg/app/indexing/unified_post.go:130-133`; consumers `pkg/app/operations/context_file_freshness.go:43-68`, `cmd/runtime_helpers.go:263`. |
| C5 | Incremental batch skips the graph recompute after a pure code-file deletion but still acknowledges the graph ticket, leaving a stale `graph_doc_scores` row and anchor PageRank. Live is correct here. | read | `unified_post.go:51-53` (recompute only if notes changed, `total.Indexed > 0`, or `ForceGraph`); `unified.go:426-427` acks unconditionally. |
| C6 | Live stamps indexer versions on every publish without batch's mismatch check. After an upgrade that bumps `IndexerVersion`, a watcher full discovery that wins the lane before boot catch-up can stamp new versions over old-indexer rows, hiding the need to reindex. | plausible (startup lane order not traced) | `pkg/app/indexcore/writer_handlers.go:72-81`; batch check `unified.go:202-210`, `reverse_index.go:29-41`. |
| C7 | A live code-config change (enable code, change roots or disabled languages) updates discovery but not the parsers; the anchor service keeps the indexers it was built with. | read for the mismatch; plausible that it outlives a later `rzm index` | `live_ownership_policy.go:233-235` swaps `w.codeCfg`; service built once at `live_code.go:150-163`; reload rejects only index-path and provider changes (`live_config_reload.go:10-21`). |
| C8 | Validation projection deletes node rows but keeps the node's chunks and embedding state, and marks no derived ticket; chunks for a removed or renamed node survive until a schema rebuild. | plausible (did not check whether search joins the orphans away) | `pkg/app/validationproduct/validation_projection.go:205-209`, `pkg/store/ontology_node_retirement.go:57-73`. Shared by live and incremental batch. |
| C9 | The TypeScript import tail index is fresh per batch run but lives for the whole runtime; explicit index jobs never feed it and retirement never prunes it, so the same edit can resolve an import differently. | plausible | `unified.go:172`, `pkg/anchors/batch.go:98`, `indexer_ts.go:1411-1420`. |
| C10 | Call-edge stale catch-up runs once per anchor-service lifetime; batch gets a fresh service per run, live keeps one. | plausible (leftovers not confirmed to occur) | `service_scopes.go:103`, `call_edge_catchup.go:14-53`. |
| C11 | Plain-note freshness markers (`MarkLastSync`, source high-water, `CommitSyncGeneration`) are batch-only. Search is unaffected; a live full note `Plan` never takes its fast path. | read, low impact | `pkg/embedindex/derived_pipeline.go:93-101`, `note_plan.go:76-82`. |
| C12 | With a provider enabled but unavailable at start, live never rebuilds its syncers when a credential appears; tickets retry until a restart. | plausible | `derived_scheduler.go:86`, `live_embeddings.go`. |

## Robustness findings

| ID | Finding | Grade | Evidence |
|---|---|---|---|
| R1 | A watcher batch that yields (or fails) marks the cache stale, so the next batch is a whole-vault discovery with global tickets for every kind. If it yielded after `mark()`, the unready tickets also force every file to be re-read and re-published. | replayed: one edited note that yields is followed by a discovery that re-publishes all 17 files in a 16-note fixture; an uninterrupted edit publishes 2 | `live_ownership.go:90-96`, `:160-166` (`MarkStale`); `indexcore/discover.go:92-94`, `:131-137` (recovery makes every present candidate "affected", which sets `ForceRead`); `indexcore/publish.go:51-71`. The subsystem note says "Structural recovery is intentionally coarse". |
| R2 | The derived scheduler spins when a ready ontology or code ticket's file no longer hashes to its indexed row (an edit the watcher has not seen yet): "superseded" skips the retry delay. Same shape when the schema hash on disk differs from the published one. A global ontology ticket rereads and hashes every note on each spin. | replayed: 32 lock-acquiring lane jobs in 4 s (8/s), 0 provider calls, attempt count unchanged | `derived_scheduler.go:84-90` (no `RetryDerivedWork` for `errDerivedSuperseded`), `derived_sources.go:59-61`, `:83-85`, `derived_scheduler.go:418-424`. |
| R3 | An external priority request cancels provider computation that holds no lock, and each cancel counts as a failed attempt: the retry delay doubles per request. | replayed: 250 ms, 500 ms, 1 s, 2 s, 4 s over five requests; cap 16 s | `derived_scheduler.go:274-295` (`computeContext`, polled every 100 ms), `:84-88`. Intended per `docs/reference/subsystems/vault-runtime.md` ("Explicit indexing and external priority cancel computation") and tested by `TestDerivedPriorityCancelsProviderWithoutHoldingWriterLease`. |
| R4 | After a save through Rhizome, embeddings are stale with no durable ticket; only the in-flight file event (or the whole-vault pass at restart) creates one. | read; not a live bug | Only `indexcore.Publish` writes tickets; saves do not call it. Any change that makes restart cheaper would turn save-then-crash into permanently stale embeddings. |
| R5 | A failed structural batch is retried on every 3 s tick with a cache recrawl and no backoff. | read | `live_ownership.go:23-52`, `:155-167`. |
| R6 | The live code pipeline drops provider vectors that were in flight when a pass is cancelled or superseded, so the retry re-sends that text. Notes and ontology cache them. This breaks the owner's "never re-embed" rule. | read | `derived_scheduler.go:383` passes a nil cache for code; `pkg/embedindex/derived_pipeline.go:111-125` sets no `abandon`; `embed_pipeline_core.go:227`. |
| R7 | On HTTP 429 each of up to 32 in-flight requests backs off on its own; concurrency never shrinks. | read, not measured | `pkg/embeddings/provider_voyage.go:429-457`, `pkg/embedindex/shared_embedding_node.go:115`, `adaptive_dispatch.go:22-80`. |
| R8 | An OpenAI-compatible endpoint that never sends rate-limit headers stays at 3 requests/s and 80k tokens/min forever. | read | `pkg/embeddings/openai_ratelimit.go:12-17`. |
| R9 | Publication backpressure: a full 128-result buffer blocks provider dispatch while a publication waits for the lane. | read; this is the documented bounded-backpressure design | `derived_scheduler.go:186`, `:244`. |

## Speed findings

Harness: real `LiveRuntime`, watcher, syncers and lane; real Voyage client against a local fake server with fixed latency; 60-note typed vault unless stated.

| Changed notes | Live: drain time at 100 ms / 0 ms latency | Live calls, texts per call, peak in flight | Lane jobs | Batch: time at 100 ms, calls |
|---|---|---|---|---|
| 1 | 306 / 198 ms | 1, 1, 1 | 18 | 417 ms, 1 |
| 10 | 2.39 / 1.55 s | 10, 1, 1 | 72 | 402 ms, 1 call of 10 |
| 50 | 12.16 / 6.59 s | 50, 1, 1 | 312 | 463 ms, 1 call of 50 |

| ID | Finding | Grade | Evidence |
|---|---|---|---|
| S1 | One ticket per changed path, executed strictly in series: N changed notes become 2N+3 tickets and N one-text provider calls with peak concurrency 1. | measured (table above) | `derived_scheduler.go:74`; `pkg/app/indexcore/candidates.go:74-86`. `PlanPaths` and `AckDerivedWork` already take slices. |
| S2 | The live packer waits 75 ms (or for 8 texts) on every ticket; with one text per ticket it always waits the full timer. About 3.8 s of the 6.6 s that 50 notes take at zero latency. | measured | `pkg/embedindex/policy.go:96-108`, `pkg/app/bootstrap/live_embeddings.go:124`. |
| S3 | Each ticket costs about 3 lane jobs at 5.1–7.2 ms each (lock, two preflights, heartbeat, diagnostics). A notes ticket with no provider work costs 15–19 ms. | measured | `lane/executor_run.go`. For one edit: 18 jobs, about 110 ms of the 198 ms zero-latency drain. |
| S4 | Every note edit marks a global code ticket, and code sorts before notes and ontology. At 500 code files one note edit spends 1.0–1.4 s rereading and hashing 1,500 file entries (500 rows plus 1,000 item paths appended without deduplication) with zero provider calls; the note's own embedding waits behind it. | measured | `indexcore/publish.go:220-228`, `derived_scheduler.go:364-380`, `pkg/store/derived_work.go:117`, `derived_sources.go:70-90`. |
| S5 | Whole-vault live passes use the low-latency packer: 1,510 texts went out in 46 calls (median 9 per call); batch sent 2,500 texts in 5 calls of about 500. Against a requests-per-minute limit this bounds live by requests, not tokens. | measured | `policy.go:96-108`. |
| S6 | The source fingerprint is recomputed in every lane job: 40–110 µs for a note path, 0.8–1.0 ms for a code path, 2.3 ms global at 500 code files. The path-scoped code fingerprint scans whole tables because its filter is always true. | measured, small today, grows with vault size | `pkg/store/derived_witness.go:36-46`. |
| S7 | A mid-pass edit supersedes a global pass, which re-plans. At 500 notes: one restart, zero texts re-sent, no measurable slowdown. | measured; not a limiter at this size | `derived_work.go:139-143`. |
| S8 | Every runtime start runs boot catch-up (a full `RunUnifiedCore`) and a watcher whole-vault discovery with four global tickets. | read, not measured | `live_indexing.go:47`, `:103`. |

## Not measured

429s and retry storms; preemption of provider work by an explicit index; SQLite write contention; vaults above 500 code files or 500 notes; notes with many chunks; the cost of a whole-vault discovery on a large repository.
