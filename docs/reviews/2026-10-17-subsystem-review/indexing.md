---
summary: "Lane report from the 2026-10-17 subsystem quality review: Indexing subsystem review. Findings are a snapshot of commit 5251923f."
---

# Indexing subsystem review

Worktree: `/Users/colthorp/.t3/worktrees/rhizome/t3-e36f9b1a` (branch `t3/critical-subsystem-audit`, HEAD `5251923f`). Read-only review. Scope: `pkg/app/indexing`, `pkg/app/indexingpipe`, `pkg/app/indexcore`, `pkg/app/indexwriter`, `pkg/vault/indexlock`, `pkg/indexingperf`, `pkg/vault/watchhub`, plus their callers in `cmd/` and `pkg/app/bootstrap` where the contract matters.

Size: production / test lines are indexing 6543 / 7570, indexwriter 2558 / 1903, indexingperf 3565 / 1603, watchhub 2209 / 1521, indexlock 1080 / 1768, indexcore 827 / 227, indexingpipe 540 / 643.

## Summary

The core design is sound, and the parts built most recently show it: the `indexcore` Discover/Publish split, typed ordered writer controls, and the `indexlock` ownership rules. The test suite is mostly behavior-level, including valuable delta-equals-rebuild tests (`TestLiveWorkloadMatchesFreshBatchAfterRenameDeleteSchemaAndIgnore`, `TestRunUnifiedCore_IncrementalScenariosMatchFreshRebuild`). The problems are mostly layers of older code that newer designs replaced without deleting them.

The three biggest problems:

1. **The call-edge and scope streaming machinery is disabled in production but still runs.** The batch streaming coordinator's call-edge and scope machinery (about 700 production lines, 15 tests) is switched off by the only production caller, which sets its rebuild hooks to nil (`unified.go:539-542`). It still runs planning DB queries, sends batches that get thrown away, and adds a way to fail the whole index run.
2. **Queue-full means run-failed.** The post-write hand-off chain (writer callback → dispatcher → submitter → code channel) uses non-blocking sends that return "queue full" errors. A slow consumer therefore aborts `rzm index` instead of applying backpressure.
3. **A second indexing pipeline.** `rzm code index` and `rzm code index-anchors` bypass the queued writer, ownership transitions, and derived obligations. They carry a third copy of call-edge orchestration. Their walker matches vault-relative ignore rules against code-root-relative paths.

`indexwriter` also has severe copy-paste: one kind touches 6 or more places, and the copies have already drifted. `RunUnifiedCore` is a roughly 540-line function wired together through six pointer-to-function out-params.

**The refactor I would do first** is to delete the disabled streaming call-edge and scope path (IDX-1) together with its non-blocking scope hand-off. It is mostly deletion and has low risk. It removes a failure mode and shrinks `unified_streaming.go` by about half, which makes the writer-callback simplification (IDX-2) easy.

## Findings — production code

### IDX-1 Disabled call-edge/scope streaming still runs its planning work and keeps a failure path
- **Severity:** P1
- **Confidence:** High that the hooks are disabled and that planning still runs. Medium on how much it costs.
- **Location:** `pkg/app/indexing/unified.go:529-542`; `pkg/app/indexing/unified_streaming.go:412-964` (`runCode`, 554 lines), `:355-376`; `pkg/app/indexing/call_edge_ready.go` (200), `call_edge_membership.go` (139), `scope_ready.go` (296); edge/scope parts of `file_progress.go`.
- **Problem:**
  - `newUnifiedStreamingCoordinator` wires `rebuildCallEdges`, `rebuildGoPackages`, `rebuildScopeIDs`, and `recomputeScopes`. Its only production caller then sets them all to nil. Structural call-edge and scope work now lives in `indexcore`'s `ApplyStructuralFinalize` (`pkg/app/indexcore/writer_handlers.go:41-82`).
  - Even so, `flushCallEdges` still polls `reverseIndexReady`, which is a DB read.
  - It calls `planRebuild` → `Service.PlanCallEdgeRebuildIncremental`, which reads the DB through `ReferrerPathsBySymbolRefs` (`pkg/anchors/service_intel.go:950-1010`), for every batch with def deltas.
  - It maintains the ready/membership/residual indexes and shows a "Planning residual call edges" stage. Then it hits `if len(rebuildPaths) == 0 || c.rebuildCallEdges == nil { return true }` and discards the result.
  - `flushScopes` returns immediately, but `TrySubmitScopeNotePaths` still sends a note-scope batch for every note index flush. Those sends are non-blocking and fail the run when `codeCh` is full (see IDX-2).
- **Evidence:**
  - `streaming.rebuildCallEdges = nil` … `streaming.recomputeScopes = nil` (`unified.go:539-542`).
  - `_ = codeStore` (`unified_streaming.go:191`) is an unused constructor parameter.
  - `AddEdgeWork` and `AddScopeWork` are called only after the nil checks (`unified_streaming.go:592,681,764`).
  - `calledge.flush.*` and `calledge.incremental_recompute_batches` metrics, which the renderers in `indexingperf/report_window.go:139-144` expect, are never emitted in production.
- **Subtle live dependency:** `noteRuntimeCode` (`service.NoteRuntimeCodeIndexWorks`) is the one side effect still in use. It fills `Service.runtimeCodeSources`, which `call_edge_rebuild.go:1083` reads during `ApplyStructuralFinalize`. The streaming goroutine fills it asynchronously on a 75 ms idle timer. Whether structural finalize sees in-memory or DB sources therefore depends on timing. Low-to-medium confidence that results can differ; high confidence that it is timing-dependent.
- **Recommendation:**
  - Delete `flushCallEdges`, `flushCallEdgePending`, `flushGoPackages`, `flushScopes`, `callEdgeReadyIndex`, `callEdgeMembershipIndex`, and `scopeReadyIndex`.
  - Delete `planRebuild`, `reverseIndexReady`, `runtimeCodeVersions`, `filterRuntimeRebuildPaths`, `SubmitScopeNotePaths`, `TrySubmitScopeNotePaths`, `EnqueueNoteIndexBatch` and the `afterNoteIndexBatch` hook, and the edge/scope progress counters.
  - Shrink `codeStreamingBatch` to the semantic-prep fields (`indexedPaths`, `prepared`, `prePersistWorks`, `forceFullRebuild`).
  - Either populate the runtime code-source cache synchronously in the writer's `afterCodeIndexBatch` (before `FlushAndWait` returns, so structural finalize always sees it), or drop the cache. Measure first.
  - `runCode` should end up under about 150 lines.
- **Effort:** M
- **Dependencies:** Enables IDX-2 (removes the scope hand-off) and the IDX-T-1 deletions. Shrinks IDX-9.

### IDX-2 Non-blocking "queue full" sends turn backpressure into run failure
- **Severity:** P1
- **Confidence:** High on the mechanics. Medium-low on how often it happens; it needs the stream consumer to fall about 128 batches behind.
- **Location:** `pkg/app/indexing/post_write_dispatcher.go:111-137`, `post_code_submitter.go:66-91`, `unified_streaming.go:1061-1072`; wired at `unified.go:569-578`.
- **Problem:**
  - The writer goroutine calls `afterCodeIndexBatch` and `afterNoteIndexBatch`, which are `postWriteDispatcher.Enqueue*`.
  - The dispatcher goroutine calls `asyncCodeBatchSubmitter.Enqueue` and `streaming.TrySubmitScopeNotePaths`.
  - Every hop uses `select { … default: return errFull }`. The error goes to `q.fail` or `streaming.fail`, which aborts `RunUnifiedCore`.
  - `runCode` drains `codeCh` only between synchronous flushes. Each flush includes DB-backed `prepareCodePaths` and planning, so a long flush on a large repo can fill the 128-slot channel.
  - This contradicts the "Bounded backpressure, no hidden queues" constraint. Here a full queue is neither backpressure nor hidden: it is a crash.
  - Tests codify the behavior: `TestPostWriteDispatcherDecouplesWriterFromFullPostCodeQueue` asserts `errPostCodeSubmitterFull`.
- **Evidence:**
  - `default: return fmt.Errorf("%w (depth=%d capacity=%d)", errPostWriteDispatcherFull, …)` at `post_write_dispatcher.go:134`.
  - The same pattern appears at `post_code_submitter.go:88` and `unified_streaming.go:1069-1070`.
  - Three goroutine hops and three bounded channels (128, 64, 128) sit between one writer callback and `runCode`.
- **Recommendation:**
  - After IDX-1, replace the dispatcher, the async submitter, and `codeCh` with one coalescing pending set owned by the coordinator: `func (c *coordinator) Persisted(works []CodeIndexWork)`.
  - It appends paths and prepared works under a mutex and signals a 1-buffered `chan struct{}`.
  - Paths are bounded by the file count, so memory is bounded, the writer never blocks, and nothing can fail on "full".
  - This deletes `post_write_dispatcher.go` (171 lines) and `post_code_submitter.go` (146 lines) and most of the `unifiedBatchFinalizer` once-pairs.
- **Effort:** M
- **Dependencies:** After IDX-1. Simplifies IDX-7.

### IDX-3 `rzm code index` / `code index-anchors` is a second, non-conforming indexing pipeline
- **Severity:** P1
- **Confidence:** High on the bypasses. Medium-high on the ignore-path bug: the code is clear, but no test covers it.
- **Location:** `pkg/app/indexing/commands.go:63-283` (called from `cmd/code.go:46,67`); `pkg/app/codeintel/ingest.go:627-664` (`IndexRoot`), `:388-405` and `:750-770` (direct-write fallback when no queue is in the context); `pkg/app/indexingpipe/files.go:82-158` (`CountFiles`), `:163-339` (`ProcessFiles`).
- **Problem:**
  - The commands open a store and call `codeintel.IngestNotesForVault` and `codeintel.IndexRoot` without `codeintel.WithWriteQueue`. In that case codeintel writes straight through `service.ApplyCodeIndexBatch` and `service.ApplyNoteIndexBatch`. This is the side write path the note forbids ("Route every new SQLite-backed write through `indexwriter.Writer`").
  - They skip ownership discovery and transitions, `SubmitDerivedDirty`, and activation. They run their own call-edge rebuild logic, which is the third copy (the others are `indexcore/writer_handlers.go` and the disabled streaming path).
  - Rebuild failures are printed as warnings, then `SetIndexerVersion` stamps the index as current (`commands.go:171-180,216`).
  - **Path bug:** `ProcessFiles` computes `rel` with `paths.NewVaultPaths(opts.Root)` (`files.go:203`, and `:108` for `CountFiles`). `IndexRoot` passes the *code root* as `Root` and a *vault-level* matcher (`ignore.LoadUnifiedMatcher(opts.VaultPath, …)`, `commands.go:113,123`). As a result, anchored vault ignore rules (for example `src/generated/`) are tested against code-root-relative paths (`generated/…`) and do not match. It only works when the code root is the vault root. Every `IndexRoot` test passes `ignore.NewMatcher(nil)` or `nil`, so the bug is untested.
  - `CountFiles` has no production caller. Only five indexingpipe tests call it.
- **Recommendation:** Pick one.
  - (a) Delete both commands and point users at `rzm index`, which already supports code-only configurations.
  - (b) Reimplement them as `indexcore.Discover` + `indexcore.Publish` with a code-only `Request` and the standard writer. The anchors variant would use the validation post-apply anchor path.
  - Either way, then delete `ProcessFiles`, `CountFiles`, `codeintel.IndexRoot`, `IngestNotesForVault`, `IngestNotesWithMatcher`, the `writeQueue == nil` direct-write branches in `codeintel/ingest.go`, and `withCommandIndexLockUsing`.
- **Effort:** M for (b), S for (a).
- **Dependencies:** Prerequisite for IDX-6 (removing the direct-write fallback). Removes the IDX-T-5 CountFiles tests.

### IDX-4 `indexwriter` kinds are copy-pasted across six places and have already drifted; `Handlers` is 26 nullable funcs
- **Severity:** P1
- **Confidence:** High
- **Location:** `pkg/app/indexwriter/run_command.go:16-311` (`handleCommand`, one 280-line type switch), `run_flush.go:50-353` (`flushAll`, `flushExpired`), `run.go:22-38` (`queueState` maps), `run.go:205-220` (`waitingForInput`), `writer.go:119-146` (`Handlers`), `flush.go`, `submit.go`.
- **Problem:**
  - Adding a write kind means editing `Submit*`, the payload type, the `queueState` map, the `run()` map literal, the `handleCommand` arm, `flushAll`, `flushExpired`, and `waitingForInput`.
  - About 13 `handleCommand` arms repeat the same sequence: ensure batch, add, compute codeHot, `shouldFlushNow`, defer metric, flush, delete. Each repeats the op-label string 3 or 4 times.
  - Drift already exists: `waitingForInput` omits `s.ontologyNodes` and `s.ontologyStates`, so the `node.write.starved` metric is wrong while those batches are pending.
  - Nil handling is inconsistent. Some kinds return "… queue not initialized" (`ApplyOntologyNodeStates`, `ApplyValidationState`, `ApplyIntentEmbeddingSnapshot`). Others call a nil func and **panic the writer goroutine**, which crashes the process: `ApplyIntelEmbeddings`, `ApplyCodeItemEmbeddingBatch`, `ApplyNoteMetaBatch`, `ApplyCodeIndexBatch`, `ApplyNoteIndexBatch`, `ApplyOntologyDelta`.
  - Three writer compositions exist (`unified.go:418`, `validation_projection.go:197`, `bootstrap/structural_writer.go:11`). The structural writer has only `BindWriterHandlers`. If anything ever submits a semantic kind to it, the runtime panics. Today that is prevented only by convention.
  - `unified.go:419-424` assigns seven handlers that `BindWriterHandlers` overwrites. They are dead assignments.
- **Dead paths:**
  - `SubmitNoteChunks` has zero callers anywhere. With it go `noteChunkWrite`, `s.noteChunkJobs`, `flushNoteChunkBatch`, `ApplyNoteChunkBatch`, `ApplyNoteChunks`, and `estimateNoteChunkWrite`, plus the slot in the `SemanticWriteQueue` interface.
  - The per-item fallbacks `ApplyCodeItemChunks` and `ApplyNoteChunkSync` are unreachable in production, because `unified.go` always sets the `*Batch` variants and no other composition submits those kinds.
- **Ordering:** Batches are keyed by phase and flushed by ranging over a map (`for phase, batch := range s.ontologyOps`). Order within a kind across phases is therefore random. It is safe today only because callers put a `FlushAndWait` between phases. Medium confidence that this is purely theoretical today.
- **Recommendation:**
  - Introduce an ordered kind table, roughly:
    - `type kind struct { name, op string; policy func(Config) FlushPolicy; deferrable, idleFlush, flushDuringHold, flushDuringDrain bool; flush func(context.Context, *Writer, *batch) error }`
    - `var kinds = []kind{codeIndex, noteIndex, noteMeta, …, noteMetadata, ontologyDelta, ontologyNodes, …}`
  - Slice order *is* the flushAll ordering, which makes the metadata → ontology-delta → node-read-model rule explicit in one place.
  - Replace `queueState`'s 15 maps with `map[kindID]map[phase]*batch`.
  - Have `New` validate that every kind's handler is set, or take only the handler groups a composition declares, and return an error rather than panic.
  - Delete `SubmitNoteChunks` and the per-item fallbacks.
  - Expected result: `run_command.go` and `run_flush.go` drop from about 660 lines to about 200.
- **Effort:** M
- **Dependencies:** Write characterization tests first (IDX-T-9b, e). Pairs with IDX-5.

### IDX-5 Ordered-control boilerplate is repeated 7 times with inconsistent completion handling
- **Severity:** P2
- **Confidence:** High
- **Location:** `submit.go:251-324` (`SubmitOwnershipTransitions`, `AcknowledgeOwnershipReconciliation`), `submit.go:326-349` (`FlushAndWait`), `graph.go:19-41`, `structural.go:29-54`, `derived.go:22-87`; `run.go:41-58,108-198` (`ackControl`, `pendingControl` union); `batches.go:57-68`.
- **Problem:**
  - Each control repeats "clone input, capture barrier, sendControl, select ack/ctx/doneCh".
  - The graph, structural, and derived variants re-check `ack` after `doneCh` closes. The ownership, reconciliation, and flush variants do not. When the writer exits right after acking (for example because its context was cancelled), a committed ownership transition can therefore come back as "queue stopped before ownership transition completed", because Go's `select` picks randomly. The cost is a spurious failure, not data loss: the pending generation forces recovery on the next run.
  - `pendingControl` is a struct of seven optional pointers dispatched by a switch.
  - `queueDerived` uses `activate` and `acknowledge` bools to choose among three operations, i.e. a flag-driven struct.
- **Recommendation:**
  - Define `type control interface{ barrier() uint64; apply(*Writer) error; fail(error) }`.
  - Use one generic `func awaitControl[T any](ctx, q, cmd, ack <-chan result[T]) (T, error)` that always prefers a ready ack.
  - Split `queueDerived` into three types.
  - Expected saving is about 150 lines.
- **Effort:** S
- **Dependencies:** IDX-4.

### IDX-6 The single-writer invariant is enforced by convention: optional queues, nil fallbacks, and a silently write-dropping nil `*Writer`
- **Severity:** P2
- **Confidence:** High on the code. Low that it is currently exploited.
- **Location:**
  - `pkg/app/indexwriter/submit.go:16-28,363-371`: nil-receiver methods return `nil`. A nil writer **reports success for every write.**
  - `pkg/app/indexing/ontology_node_embeddings.go:72-75`: a nil `writeQueue` means direct `intelStore` writes. Production always passes the writer; only `unified_html_ontology_semantic_test.go:61,72,123` passes nil.
  - `pkg/app/codeintel/write_queue.go:31-51` and `ingest.go:392,754`: the queue is fetched from the context, with a direct-write fallback.
  - `pkg/search/semantic/*` has about 20 `if s.WriteQueue != nil { … } else { direct }` branches. That code is in the search lane; flagged for cross-lane follow-up.
  - `indexwriter.SemanticWriteQueue` (`estimates.go:219-236`) is a 16-method interface with one implementation. Its consumers use 5 methods (`ontology_body_writer.go`).
- **Recommendation:**
  - Make the writer a required, non-nil dependency everywhere in indexing. Delete the nil-receiver guards so misuse panics at the call site.
  - Pass the writer explicitly instead of through `context.Value`.
  - For tests and one-shot tools that genuinely want direct writes, provide one `indexwriter.Direct(handlers)` adapter that implements the same submit interface, so producers keep a single code path.
  - Narrow `SemanticWriteQueue` to the five methods its consumer uses, or use `*indexwriter.Writer` directly.
- **Effort:** M (crosses into the search lane for `pkg/search/semantic`).
- **Dependencies:** After IDX-3.

### IDX-7 `RunUnifiedCore` is a roughly 540-line orchestrator with circular construction and ad-hoc output
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/app/indexing/unified.go:325-829`; `unified_semantic.go:108-125` (six `*func` out-params); `unified.go:70-284` (`unifiedBatchFinalizer`: nine `sync.Once` pairs plus `writerClosed`); `unified_semantic.go:88-101,128-149`.
- **Problem:**
  - The writer is built before the semantic stores, so `prepareUnifiedSemanticResources` back-fills handler closures through six pointer-to-function out-params. Writer handlers capture variables that are assigned later. This is correct only because producers are created afterwards, so the race-freedom is an unstated ordering.
  - The finalizer is a hand-rolled shutdown graph.
  - Output goes three ways:
    - `progress.Println`
    - `fmt.Fprintf(os.Stderr, "[intent] …")` (`unified_semantic.go:91,101`)
    - `log.Printf` for validation failures (`unified.go:815,821,823`)
  - Progress filtering matches another package's format strings by prefix (`"Embedding "`, `"Sync complete"`, … at `unified_semantic.go:128-149`), so renaming a log line silently changes CLI output.
  - `localCfg, _ := obsidian.LoadLocalConfig(...)` (`unified.go:382`) swallows the error. The scope hash then becomes empty, so the evidence contract ("persists authoritative ScopeConfigHash") silently degrades to "blocked" rather than reporting why.
  - The file is 878 lines and `unified_streaming.go` is 1386 lines. Both exceed the 500-line architecture guideline.
- **Recommendation:**
  - Open the embedding stores first; they do not need the writer. Build the writer with complete handlers. Then build the syncers with the writer. This deletes the out-params.
  - Make the run a struct (`type unifiedRun struct{…}`) with phase methods: `setup`, `publishStructure`, `drainSemantic`, `postIndex`, `acknowledgeDerived`, `publishValidation`.
  - Replace the finalizer with an ordered `[]func(context.Context) error` cleanup stack.
  - Route every message through `ProgressSink`.
  - Have syncers emit typed progress events instead of format strings.
  - Return or surface the `LoadLocalConfig` error.
- **Effort:** M
- **Dependencies:** Easier after IDX-1 and IDX-2.

### IDX-8 The cancellation contract in the note does not match writer behavior
- **Severity:** P2
- **Confidence:** Medium-high
- **Location:** `pkg/app/indexwriter/writer.go:178-202` (`NewWithConfig` derives the writer context from the caller's context); `run.go:271-280`; `pkg/app/indexing/unified.go:98-103,163-174` (`nonCanceledQueueContext`, `DrainStreamingWrites`); note `indexing.md:69`.
- **Problem:**
  - The note says `DrainStreamingWrites` "flushes their queued writes, then reports caller cancellation … Cancellation must not short-circuit the durability barrier."
  - But `RunUnifiedCore` builds the writer from the job context. On cancellation the writer loop takes `case <-q.ctx.Done(): q.fail(…); return`. In-memory batches never flush, and in-flight handlers run with a cancelled context.
  - `nonCanceledQueueContext` only keeps the *submitter's* wait alive.
  - Recovery is still correct, because durable dirty obligations predate the work. But the stated guarantee is not true.
  - `TestDrainStreamingWritesPropagatesCancellation` checks only `ErrorIs(err, context.Canceled)`, so it cannot detect this.
- **Recommendation:** Decide which contract you want.
  - (a) Give the writer `context.WithoutCancel(ctx)`, and on job cancel call `CloseWriter` and then return the cancellation, making "accepted writes are durable" true.
  - (b) Change the note to "cancellation abandons undrained derived writes; obligations make the run recoverable" and delete `nonCanceledQueueContext`.

  I recommend (b): it is simpler and matches the yield-promptly requirement for priority preemption.
- **Effort:** S
- **Dependencies:** IDX-T-6

### IDX-9 The metrics contract is stringly typed across about 113 files; renderers hard-code about 275 names
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/indexingperf/report_window.go` (150 quoted metric names), `report_phase.go` (125), `writeback_metrics.go` (the only descriptor table); producers in `indexwriter/*.go`, `indexing/*.go`, `pkg/anchors`, and `pkg/search/semantic`.
- **Problem:**
  - Producers pass ad-hoc strings, and renderers look the same strings up again. Renaming either side silently zeroes a report column.
  - Some names are built dynamically (`"calledge.fallback."+reason`, `"queue.flush.outcome."+metricOutcome(err)`).
  - Columns that depend on IDX-1's disabled path are permanently empty.
  - The note claims presentation is "descriptor-driven", but only the writeback family is.
  - The package also has two collector modes, `New()` (unbounded, one production use in `pkg/app/agentstart/start.go:57`) and `NewBounded()`, plus a test proving they render identical bytes (`TestBoundedCompleteRenderingMatchesLegacyBytes`).
- **Recommendation:**
  - Extend the descriptor approach to every family: one `metrics.go` with exported typed names (`type Metric string; const QueueFlush Metric = "queue.flush"` …) used by producers and renderers alike.
  - Make `New` an alias of `NewBounded` and delete the legacy-equivalence test.
- **Effort:** M
- **Dependencies:** IDX-1 first, which deletes dead call-edge metrics. Enables IDX-T-4.

### IDX-10 "Lock held" is a precondition by convention on `RunUnifiedCore` and `indexcore.Publish`
- **Severity:** P2
- **Confidence:** High that no check exists. Medium on the value of fixing it.
- **Location:** `pkg/app/indexing/unified.go:325` (no lock reference in the function); `pkg/app/indexcore/publish.go:24`; callers `cmd/index_unified.go:23`, `cmd/runtime_helpers.go:38`, `cmd/serve_index_job_hooks.go:52`, `pkg/app/bootstrap/live_ownership.go:253`.
- **Problem:**
  - The validation post-apply path already requires an opaque `validate.IndexLockLease` and checks the vault root.
  - The main entry points accept nothing. A new caller can run a full index without the lock and nothing catches it.
  - `TryAcquireIndexLock(ctx, path, requestPriority bool, debug bool, out)` uses positional booleans.
- **Recommendation:**
  - Introduce `indexlock.Lease` (the token plus canonical vault root), returned by `TryAcquire*` and by the runtime lane. Require it as the first parameter of `RunUnifiedCore`, `indexcore.Publish`, and the validation projection held core, and verify the root.
  - Replace the boolean parameters with `LockWaitOptions` everywhere (it already exists).
- **Effort:** M
- **Dependencies:** None

### IDX-11 Dead and redundant code (batch delete)
- **Severity:** P3
- **Confidence:** High (every item grep-verified)
- **Location and items:**
  - `pkg/app/indexing/reverse_index.go`:
    - `MarkReverseIndexBackfillComplete` and `ShouldMarkReverseIndexComplete` have no production callers; they are tested only in `cmd/index_reverse_test.go`.
    - The private wrappers `markReverseIndexBackfillComplete` and `shouldMarkReverseIndexComplete` are unused.
    - `ensureReverseIndexReadiness` is a one-line alias.
    - `type ReverseIndexStore = reverseIndexStore` is an exported alias of an unexported interface.
    - The backfill flag is actually set by `ApplyStructuralFinalize`.
  - `unified.go:299-306`: `ontologyNodeEmbeddingsRequireRebuild` is used only by `unified_test.go`.
  - `semantic_core.go:10`: the `changedPathCount` parameter is always `0` (`unified.go:849`), so the `note_raw.compat_paths` metric is always zero.
  - `unified.go:575`: `if writeQueue != nil` can never be false.
  - `unified_streaming.go:191`: `_ = codeStore`.
  - `pkg/vault/indexlock/lock.go:44-47,96`: `LockData.ProcessLifetime` is written but never read by any decision; its doc comment describes behavior that is now universal.
  - `lock.go:137-143`: the `age < 2*time.Second` branch returns the same result as the following `age < 10*time.Minute` branch.
  - `pkg/vault/watchhub/hub.go:71-125`: `Filter.Extensions` and `Filter.OpMask` are unused by every production subscriber.
  - `pkg/app/indexwriter`: `NewWithConfig` and the exported `Config`/`FlushPolicy` knobs are test-only (four test callers, no production caller).
  - `pkg/app/indexcore/metrics.go:11` and `pkg/app/indexing/perf_helpers.go:12`: duplicate `runPhase` and event helpers. Move one copy to `indexingperf.RunPhase`.
  - `pkg/app/indexing/validation_projection.go:196`: `store.SetWriteMu(&sync.Mutex{})` on a freshly opened private store is a no-op.
- **Recommendation:** Delete the items above, along with the tests that only pin them (IDX-T-5).
- **Effort:** S
- **Dependencies:** None

### IDX-12 Batch and live adapters both repeat Publish → flush → acknowledge
- **Severity:** P3
- **Confidence:** High
- **Location:** `pkg/app/indexing/unified.go:583-604`; `pkg/app/bootstrap/live_ownership.go:246-266`.
- **Problem:**
  - Both adapters call `indexcore.Publish`, then `if result.Reconciliation.Required() { AcknowledgeOwnershipReconciliation(result.StructuralGeneration) }`.
  - The note's ordering rule (acknowledge after finalization, ontology publication, and derived activation) is already satisfied at the end of `Publish`.
  - Both also wrap the context with `codeintel.WithWriteQueue`, which `Publish` already does internally (`publish.go:35`).
- **Recommendation:** Move the acknowledgement into `Publish`, or into `indexcore.PublishAndAcknowledge`, so there is exactly one way to finish structural work.
- **Effort:** S
- **Dependencies:** None

### IDX-13 `watchhub.Hub` is a 13-primitive god object; dispatch is not serialized
- **Severity:** P3
- **Confidence:** High on structure. Low that the ordering hazard causes harm today.
- **Location:** `pkg/vault/watchhub/hub.go:149-191` (struct), `:587-670` (`enqueue`/`flush`/`dispatch`).
- **Problem:**
  - The struct holds 8 mutexes, a WaitGroup, `sync.Once`, atomics, and channels; the file is 1010 lines.
  - `flush` runs on `time.AfterFunc` goroutines. If a dispatch is still running when the re-armed timer fires, two dispatches overlap, so subscriber callbacks can run concurrently and out of order.
  - The current subscribers (`bootstrap/watcher_pending.go:17`, `vault/cache/watchhub.go:22`, `bootstrap/live.go:1249`) only record into mutex-guarded maps, and deletions are re-checked on disk. The risk is therefore latent, but the "Watcher batches yield and fail without losing reconciliation" constraint assumes ordered delivery.
- **Recommendation:**
  - Use one event-loop goroutine that owns `pending`, the debounce timer, and dispatch.
  - Split the file into filter, lifecycle, and dispatch parts.
  - Delete the unused `Filter` knobs (IDX-11).
- **Effort:** M
- **Dependencies:** None

### IDX-14 Subsystem note drift
- **Severity:** P3
- **Confidence:** High
- **Location:** `docs/reference/subsystems/indexing.md`
- **Problem:**
  - Lines 85 and 87 are the same "Validation projection is scoped indexing" bullet, with divergent wording ("fragment targets" vs "Markdown targets").
  - Line 120 says "Slot incremental/watcher behavior into … `RunUnifiedCore` with dirty paths". There is no such option; live goes through `indexcore.Publish`.
  - Line 155 calls `runBackgroundIndex` "yielding lock". The code comment at `cmd/runtime_helpers.go:26` says it "acquires no lock".
  - Line 75 states invariants for `CountFiles`, which is dead (IDX-3).
  - Line 69 describes a durability guarantee the writer does not provide (IDX-8).
  - Line 122 points readers to `unified_test.go` patterns; that file contains one trivial test.
  - Over the 500-line guideline: `unified_streaming.go` 1386, `watchhub/hub.go` 1010, `unified.go` 878, `validation_projection.go` 680, `indexingperf/report_window.go` 547, `report_phase.go` 540, `indexwriter/write_queue_test.go` 1738.
- **Recommendation:** Fix the note in the same change sets as IDX-1, IDX-3, and IDX-8, and bump `last-verified`.
- **Effort:** S
- **Dependencies:** IDX-1, IDX-3, IDX-8

## Findings — tests

### IDX-T-1 Delete with IDX-1 (tests pin machinery production disables)
- **Verdict:** delete
- **Tests:**
  - `pkg/app/indexing/unified_streaming_test.go`: `TestRunCodeReadiesResidualBacklogFromDurableFootprints`, `TestRunCodeFinalDrainResolvesResidualAfterBackfillCompletes`, `TestRunCodeThrottlesReverseIndexReadyChecks`, `TestRunCodeDefersCallEdgeRebuildUntilFinalDrain`, `TestFilterRuntimeRebuildPathsSkipsOnlyUnchangedFreshPaths`
  - `call_edge_ready_test.go`: all 4 tests
  - `call_edge_membership_test.go`: both tests
  - `scope_ready_test.go`: both tests
  - `file_progress_test.go`: `TestFixedTotalFileProgressCountsEdgeWorkDynamically`, `TestFixedTotalFileProgressCountsScopeWorkDynamically`
  - `unified_shutdown_test.go`: `TestUnifiedStreamingTrySubmitScopeNotePathsReturnsBackpressureWhenCodeQueueFull`
- **Why:** Each builds a coordinator with non-nil `rebuildCallEdges`, `rebuildScopeIDs`, or `planRebuild` hooks, which production never has (`unified.go:539-542`). They pass while the production path behaves differently.
- **Replacement:** None beyond the existing `RunUnifiedCore` integration tests, which already cover call edges via `ApplyStructuralFinalize` (for example `TestRunUnifiedCoreBuildsGoPackageRelationshipsWithEmbeddingsDisabled`).

### IDX-T-2 Move ownership tests to the package they test
- **Verdict:** move
- **Tests:** `pkg/app/indexing/ownership_run_test.go` (8 tests, `TestBuildOwnershipRun_*`) and `ownership_reconciliation_test.go` (5 tests, `TestOwnershipReconciliationTracker*`).
- **Why:** They call only `noteownership.Discover` and `noteownership.NewReconciliationTracker`, with zero `indexing.` symbols. That contradicts `pkg/app/indexing/AGENTS.md` ("Discovery and transition contracts belong to `noteownership`"). `pkg/app/noteownership` has no reconciliation test of its own, so these are its only coverage.
- **Replacement:** Move them into `pkg/app/noteownership` with their helpers (`writeOwnershipFile`, `ownershipCandidateShapes`, …). Keep `ownership_transition_test_helpers_test.go` only if `unified_ownership_integration_test.go` still needs it.

### IDX-T-3 Delete or merge tautological writer passthrough tests
- **Verdict:** delete / merge
- **Tests:**
  - **Delete** `pkg/app/indexwriter/write_queue_test.go:TestQueuedWriterFlushAllCarriesExternalEvidenceWithCodeWork` and `TestQueuedWriterFlushExpiredCarriesExternalEvidenceWithCodeWork`. The writer passes `CodeIndexWork` by value and never reads `ExternalEvidence`, so the tests assert that Go copies a struct. The close and interval flush triggers are already covered by `TestQueuedWriterFlushesOnClose` and `TestQueuedWriterFlushesOnInterval`. Evidence persistence belongs in the `ApplyCodeIndexBatch` store test.
  - **Rewrite** `TestQueuedWriterFlushesOntologyNodeBodyWrites`. It asserts field-by-field copies (`fieldRows[0].FieldName == "status"`, `linkDeps[0].ResolvedTargetNotePath`) plus a rendered summary substring. Reduce it to "node read model, states, and embeddings all reach their handlers in one close", or fold it into `TestQueuedWriterFlushesOntologyDeltaBeforeReadModel`.
  - **Merge** `TestQueuedWriterAcceptsCodeChunkBatchSubmission` into `TestQueuedWriterBatchesCodeChunkJobsWhenBatchHandlerIsAvailable`, and `TestQueuedWriterAcceptsCodeItemEmbeddingBatchSubmission` into `TestQueuedWriterUsesEmbedFlushPolicyForItemEmbeddings`. Each pair is the same scenario through the single and batch submit variants; use a table.
  - **Rename or merge** `TestQueuedWriterBatchesNoteChunkJobsWhenBatchHandlerIsAvailable`. It actually exercises `SubmitNoteChunkSync`, not note chunk jobs. Merge it with `TestQueuedWriterFlushesCleanupOnlyNoteChunkSyncBatch` and `TestQueuedWriterFlushesEmptyNoteChunkSyncBatch`.

### IDX-T-4 Replace render-string assertions with structured assertions
- **Verdict:** rewrite
- **Tests:**
  - `pkg/indexingperf/collector_test.go` (1067 lines, 479 `Contains` assertions on rendered output, for example `TestCollectorRenderWindowIncludesQueueLaneAndCallEdgeStageMetrics`, which feeds 60+ hand-typed metric names)
  - `pkg/app/indexwriter/intent_writer_test.go:TestIntentSnapshotUsesQueueBarrierAndOwnsVectors` (polls `RenderWindow` for `"intent_pending_rows=1"`)
  - `pkg/indexingperf/bounded_render_test.go:TestBoundedCompleteRenderingMatchesLegacyBytes` (pins two collector modes; delete with IDX-9)
- **Why:** These tests restate the renderer's name list, break on any format change, and still miss producer-side renames (IDX-9).
- **Replacement:**
  - Assert on `collector.Snapshot()` counters and spans; `TestWriterMetricsDistinguishAttemptedBatchesFromCommittedRows` already does this and is the model.
  - Keep one reviewed golden file per renderer (`testdata/window.golden`, `summary.golden`) with an `-update` flag.

### IDX-T-5 Delete tests of dead code
- **Verdict:** delete
- **Tests:**
  - `cmd/index_reverse_test.go`: `TestMarkReverseIndexBackfillComplete`, `TestShouldMarkReverseIndexComplete`, `TestShouldMarkReverseIndexComplete_RequiresForceWhenUnknown` (dead functions, IDX-11)
  - `pkg/app/indexing/unified_test.go:TestOntologyNodeEmbeddingsRequireRebuildPreservesEarlierSync` (test-only helper)
  - `pkg/app/indexingpipe/files_test.go:TestCountFilesMatchesCandidateClassification` and the `CountFiles` halves of `files_subtree_test.go` and `files_discovery_unix_test.go`, when `CountFiles` is deleted (IDX-3)
  - `pkg/vault/indexlock/lock_test.go:TestPriorityPath`. It asserts string concatenation, and every priority behavior test already depends on the path.

### IDX-T-6 The drain-cancellation test cannot detect the guarantee it names
- **Verdict:** rewrite
- **Test:** `pkg/app/indexing/ontology_publication_order_test.go:TestDrainStreamingWritesPropagatesCancellation`
- **Why:** It asserts only `ErrorIs(err, context.Canceled)`. The note promises that accepted queued writes become durable before cancellation is reported (IDX-8).
- **Replacement:** Submit a second payload while the first handler is blocked, cancel, release, and assert whether the second handler ran. It will currently show the second write dropped. Pick the IDX-8 contract and pin it.

### IDX-T-7 Self-skipping watcher test never asserts on the main dev platform
- **Verdict:** rewrite
- **Test:** `pkg/vault/watchhub/recursive_test.go:TestWatchHub_EmitsEventForNestedFile`
- **Why:** In this run it skipped after 1.55 s with "fsnotify backend did not emit probe event on darwin; watcher unavailable in this environment". Its skip condition is the failure it should detect, so it passes when the backend is broken.
- **Replacement:** On darwin, test the FSEvents backend (the production default) instead of `ForceFSNotify`. Gate the skip on an explicit environment variable for sandboxed runs, and otherwise fail.

### IDX-T-8 `pkg/app/indexing` runs effectively serially
- **Verdict:** rewrite (speed)
- **Evidence:** The sum of top-level test times is 29.0 s against a package wall time of 28.7 s. 32 of 50 test files do not call `t.Parallel()`. Only `provider_lifetime_test.go` uses `Setenv`.
- **Changes:**
  - `validation_postapply_eligibility_test.go:TestValidationProjectionPostApplyRefresherSelectsExactSources`: 14 subtests at about 0.43 s each, run serially (6.0 s). Each uses its own temp dir, so add `t.Parallel()` to the subtests.
  - Add `t.Parallel()` to the tempdir-isolated `RunUnifiedCore` and validation projection integration files.
  - Keep `TestLiveWorkloadMatchesFreshBatchAfterRenameDeleteSchemaAndIgnore` (12.3 s): it is the strongest delta-equals-rebuild evidence. Run it in parallel rather than shortening it.
- **Expected effect:** Package wall time drops toward about 13 s.
- **Caveat:** Confirm that no test relies on process-wide priority state (in-process priority requests count, per `indexlock`).

### IDX-T-9 Missing behavior tests
- **Verdict:** add
- **a. Legacy code-command ignore semantics.** Vault with code root `src/`, `.rhizome/ignore` containing `src/generated/`, file `src/generated/x.go`. Assert that `RunCodeIndexCommand` does not index it. This fails today (IDX-3). If the command is deleted instead, skip this test.
- **b. Writer missing-handler safety.** Submit each payload kind to `indexwriter.New(ctx, Handlers{})` and assert an error from `FlushAndWait` rather than a goroutine panic. This fails today for `SubmitIntelEmbeddings`, `SubmitCodeItemEmbedding`, `SubmitNoteMeta`, and `SubmitOntologyDelta` (IDX-4).
- **c. Single-writer lane during a full run.** Wrap the store write mutex passed to `SetWriteMu` with one that records the calling goroutine (or a context marker installed by writer handlers). Run `RunUnifiedCore` on the fixture vault with embeddings enabled through a fake provider. Assert that every write came from the writer goroutine. No current test enforces "never open a side write path".
- **d. Publish fixed-point expansion.** No test asserts the `expanded → rediscover` loop in `indexcore.Publish` (`publish.go:60-117`); grep finds no reference to `ownership.transition_passes` or the expansion in any test. Add a case where an ownership transition reveals an additional affected source. Assert `transition_passes >= 2` and that the final state equals a fresh rebuild.
- **e. Writer intra-kind ordering across phases.** Submit two `OntologyDelta` values (or two `NoteMetadataDelta` values) for the same path under different phases without a barrier, close, and assert last-submitted-wins. This documents or forces a fix for the map-iteration order (IDX-4).
- **f. Queue saturation.** If IDX-2 is not done right away, add a regression test that a slow `runCode` consumer does not fail `RunUnifiedCore`.

## Suggested refactor sequence

Each step can ship on its own and is ordered so that later steps get simpler.

1. **Dead code and misplaced tests.** IDX-11 deletions, IDX-T-5 deletions, IDX-T-2 move, and the IDX-14 duplicate-bullet fix. Mechanical, size S.
2. **Remove the disabled streaming call-edge/scope machinery (IDX-1)** with IDX-T-1. Decide the runtime-code-source cache: populate it synchronously in the writer callback, or drop it after a timing comparison. Removes the note-scope hand-off half of IDX-2.
3. **Collapse the post-write hand-off chain (IDX-2)** into one coalescing pending set. Delete `post_write_dispatcher.go` and `post_code_submitter.go`. Rewrite the three queue-full tests as "slow consumer does not fail the run" (IDX-T-9f).
4. **Writer kind table and controls (IDX-4, IDX-5).** Land characterization tests IDX-T-9b and IDX-T-9e first, then the refactor, then IDX-T-3 cleanup.
5. **Retire or rewire the legacy code commands (IDX-3).** Then delete `ProcessFiles`, `CountFiles`, `IndexRoot`, `IngestNotesForVault`, and the codeintel direct-write fallbacks. Add IDX-T-9a if the commands stay.
6. **Make the writer mandatory (IDX-6)** inside indexing and codeintel. Coordinate the `pkg/search/semantic` `WriteQueue == nil` branches with the search-lane reviewer. Add IDX-T-9c.
7. **Restructure `RunUnifiedCore` (IDX-7)** and settle the cancellation contract (IDX-8, IDX-T-6). Fold the acknowledgement into indexcore (IDX-12).
8. **Typed lease (IDX-10).**
9. **Typed metric names and structured perf tests (IDX-9, IDX-T-4).**
10. **Watchhub event loop (IDX-13)** and the self-skipping test (IDX-T-7). Test parallelism (IDX-T-8) can land at any point.

## Slow-test table (measured)

Command: `go test -count=1 -json` over the seven packages, run once; all passed. Raw output is at `/tmp/rhizome-subsystem-review/indexing-test.json`.

| Package | Wall (s) |
| --- | ---: |
| pkg/app/indexing | 28.75 |
| pkg/vault/indexlock | 3.82 |
| pkg/vault/watchhub | 3.38 |
| pkg/app/indexcore | 0.61 |
| pkg/app/indexingpipe | 0.48 |
| pkg/indexingperf | 0.34 |
| pkg/app/indexwriter | 0.32 |

| Test | Package | Elapsed (s) | Note |
| --- | --- | ---: | --- |
| TestLiveWorkloadMatchesFreshBatchAfterRenameDeleteSchemaAndIgnore | indexing | 12.34 | Keep; strongest delta-equals-rebuild proof. Run in parallel. |
| TestValidationProjectionPostApplyRefresherSelectsExactSources | indexing | 5.98 | 14 serial subtests × about 0.43 s; parallelize (IDX-T-8). |
| TestWatchHub_EmitsEventForNestedFile | watchhub | 1.55 | Skipped after paying the cost (IDX-T-7). |
| TestAddWatchRoots_RecursiveDirectories | watchhub | 1.03 | |
| TestYieldingHeartbeatYieldsWithinItsPollInterval | indexlock | 1.00 | Real-time poll interval. |
| TestBatchRecoversDelayedNoteDebtWithoutSourceChanges | indexing | 0.91 | |
| TestRunUnifiedCore_IncrementalScenariosMatchFreshRebuild | indexing | 0.66 | |
| TestPriorityCheckReadsRecordsWhenTheGuardIsBusy | indexlock | 0.62 | |
| TestStaleTakeoverKeepsMetadataReadable | indexlock | 0.61 | |
| TestRunUnifiedCore_PendingOwnershipPrunesUndiscoverableNoteEmbeddings | indexing | 0.57 | |

Top-level test counts: indexing 163, indexlock 51, indexwriter 47, watchhub 36, indexingperf 33, indexingpipe 15, indexcore 5. Skips: `TestWatchHub_EmitsEventForNestedFile` and `TestTryAcquirePreservesUncertainPreviousBoot/same_boot_foreign_namespace` (the latter is Linux-only and expected).
