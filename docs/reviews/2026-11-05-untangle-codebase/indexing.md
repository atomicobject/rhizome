# Indexing structural audit

Read-only audit of `pkg/app/indexing`, `pkg/app/indexcore`, `pkg/app/indexwriter`, `pkg/embedindex`, `pkg/graph/index`, `pkg/vault/watchhub`, and `pkg/indexingperf`, against HEAD `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`.

Read the owning indexing skill, subsystem note, package context, and indexing test-ownership guidance. Compounding lookup returned no matching current shards. Production files were not changed. The strongest improvements are three bounded refactors below. None requires changing persistence formats or supported user behavior.

## IDX-1: Give code-priority scheduling an explicit identity

**CONFIRMED hidden links; medium refactor value.**

- `pkg/app/indexwriter/payload_kinds.go:77` determines whether to update the code hot-window by comparing the metrics operation string with `"intel.replace_code_file"`.
- `pkg/app/indexwriter/payload_kinds.go:83`, `pkg/app/indexwriter/run.go:101`, and `pkg/app/indexwriter/run_flush.go:37` all assume `s.kinds[0]` is code work.
- `newPayloadKinds` declares the actual code kind at `payload_kinds.go:127` and puts it first at approximately line 157. The ordered list has a separate purpose: durable flush order. A metrics-label rename or list maintenance can therefore silently change runtime scheduling.

**Smallest coherent fix:** Construct the code payload kind once, retain a named reference in queue state, and also insert that object into the ordered flush list. Recognize code work using that identity rather than the metrics string. Keep durable list order unchanged. A small constructor returning queue state (or named code kind plus the ordered list) is sufficient; do not introduce a general registry or scheduling framework.

**Reach:** `payload_kinds.go`, `run.go`, `run_flush.go` and queue construction in `run`. Existing external writer callers and public `Submit*` methods remain unchanged. Update the indexing writer CONTEXT line about kind identity if documenting this invariant; preserve the single writer and ordering contracts.

**Verification:** Existing `TestQueuedWriterDefersSemanticFlushWhileCodeIndexIsHot`, `TestQueuedWriterAllowsUrgentSemanticFlushWhileCodeIndexIsHot`, `TestQueuedWriterPrefersCodeLaneWhileCodeIsQueued`, phase-order tests, and control barrier tests. Add one narrow test proving scheduling does not depend on the code operation's display name or position in a test-local list, or directly exercise the named code-kind state. Run focused indexwriter tests and the required race/integration gates through root.

**Risk:** Miscounting pending code work could starve semantic writes or erase priority. Preserve the distinction between queued channel commands and pending phase segments, and preserve whether the channel is counted at each existing call site.

## IDX-2: Let ontology orchestration own batching once

**CONFIRMED surplus and needless coupling; high refactor value.**

`pkg/embedindex/provider_embed.go:14-150` implements another batching loop, semaphore, goroutine-per-batch dispatch, result channel, cancellation fanout, vector ordering, and wait group. Its only production caller is `OntologyNodeSyncer.ComputeDerived` at `pkg/embedindex/ontology_derived.go:67`. That caller already:

1. Sets `sliceTexts = batchSize` at lines 63-64.
2. Bounds in-flight slices by the effective provider concurrency at line 65.
3. Passes each at-most-one-batch slice to the helper with `maxConcurrent = 1` at line 67.
4. Collects vectors by slice, stops starting work after failure, and preserves successful slices at lines 70-119.

The helper's multi-batch and parallel behavior is reachable only from its unit tests. The two schedulers change together, but the relationship is implicit.

**Smallest coherent fix:** Replace the private helper with a single physical provider-call helper taking the already-selected batch, shared gate, and needed metrics context. Keep the gate acquisition/cancellation behavior, vector-count check, provider counters/samples, and time measurements. Keep all slice scheduling and partial-result preservation in `ComputeDerived`. Do not replace the shared embedding-node path or add a new batching abstraction.

**Reach:** `provider_embed.go`, `ontology_derived.go`, and `provider_embed_test.go`. The helper is unexported; repository search found one production call and three tests. Existing high-level tests already exercise real requirements at their owner: `TestOntologyNodeSyncer_EmbedsChangedChunksConcurrentlyWithGate`, `TestOntologyNodeSyncer_UsesProviderDefaultBatchSize`, and ontology freshness tests for succeeded slices, cancellation, and failed publication.

**Tests cleanup:** Move or rewrite `TestEmbedTextsWithProviderSplitsByBatchSize`, `TestEmbedTextsWithProviderUsesConcurrency`, and `TestEmbedTextsWithProviderCancelsRemainingBatchesOnError` to verify `ComputeDerived` behavior where batching actually belongs; avoid preserving unused helper flexibility only to keep these tests. Retain tests for a malformed provider vector count, cancellation while waiting for the shared gate, exact vector order, and partial success/retry.

**Risk:** Retain vectors returned by in-flight provider calls after cancellation, and do not accidentally cancel other already-started slices on a sibling failure. Keep existing metric meanings and phase attribution. This is a structural simplification, not a measured performance optimization.

## IDX-3: Delete impossible queued-write branches and narrow the producer port

**CONFIRMED surplus; medium refactor value.**

- `pkg/embedindex/syncer_embed.go:46-55` initially defines an item writer that can call `s.WriteQueue.SubmitCodeItemEmbedding`, but lines 63-67 replace that writer whenever `s.WriteQueue != nil`. Its queue branch can never execute.
- Lines 58-61 adapt a nonnil queue as `intelTarget`, but lines 70-72 consider a queue only when `writebackBatcher == nil`.
- `newCodeWritebackBatcher` at `code_writeback_batcher.go:27-36` returns nonnil whenever its queue is nonnil, so line 72's direct queued-intel writer branch is also impossible.
- The unreachable item-write branch is the only production call to `SyncWriteQueue.SubmitCodeItemEmbedding` within embedindex. Removing it also removes that unnecessary method requirement at `syncer.go:108`; the real writer can retain its public single-item method for other callers.

**Smallest coherent fix:** Initialize the direct item writer once, replace it with batched queue submission only in the queue branch, and create the standalone intel batch writer only in the direct-store branch. Delete the redundant queue adapter and unreachable conditional. Remove only the no-longer-required method from `SyncWriteQueue`; do not delete the real writer method used by external callers.

**Reach:** `syncer_embed.go`, `syncer.go`, and optional cleanup of now-unneeded fake methods in `syncer_test_helpers_test.go` and `code_writeback_batcher_test.go`. `queueEmbeddingWriter` and `directIntelEmbeddingSubmitter` remain used elsewhere and must stay.

**Verification:** Existing syncer writeback tests, `TestCodeWritebackBatcherFlushesPendingKindsOnClose`, `TestCodeWritebackBatcherDrainsQueuedRowsAfterCancellation`, threshold/idle flush tests, and writer error propagation tests. No new production behavior or broad abstraction is needed.

**Risk:** Preserve deferred drain/close order, the direct-store batch writer path, and the distinction between queue-backed and standalone index calls.

## Selected Tests cleanup: make the gate-wait regression observe its precondition

**CONFIRMED weak synchronization; no flaky failure reproduced.**

`pkg/embedindex/shared_embedding_cancel_test.go:98-100` sleeps 50 ms before cancelling, assuming the submitted request left the pending queue and reached the occupied shared gate. On a slow scheduler, it can pass by cancelling a still-queued request and never test the claimed gate-wait behavior.

Use the existing collector, attached to the node's context, to wait for one `ProviderBatchTexts` sample. That sample is recorded in `shared_embedding_dispatch.go` after the batch is removed from pending and before gate acquisition. Then cancel and release the gate. This proves the distinct branch without adding a production test hook. Keep the empty provider-call assertion and bounded completion timeout. The neighboring first test already uses a collector to observe its queued-work precondition.

`TestOntologyComputeDerivedBoundsSliceGoroutines` also uses a sleep and whole-process `runtime.NumGoroutine`; do not replace it blindly with provider concurrency, because that would fail to detect goroutines waiting before the provider. A reliable rewrite needs a work-start observation tied specifically to this pipeline. I did not identify a sufficiently small existing seam to recommend altering that test in this pass.

Focused existing-test result: `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/embedindex -run 'TestNoteComputeFailureCachesEmbeddedSectionsOfIncompleteNotes|TestSharedEmbeddingNodeDropsBatchCancelledWhileWaitingForGate' -count=30` passed (`4.427s` package time). This proves current success only; it does not prove absence of scheduling-dependent false passes. No further tests were started after root requested exclusive verification ownership.

## Comments and document cleanup worth including with owned edits

- `pkg/embedindex/syncer.go:182` and `note_syncer.go:96` say setting the stale threshold to zero means immediate pruning. Both implementations explicitly replace zero with 0.3 (`syncer_plan.go:178-179`, `note_freshness.go:85-86`). Remove the false sentence; changing semantics is outside the behavior-preserving refactor.
- `pkg/app/indexwriter/CONTEXT.md:5` still names `search/embeddings` and `search/semantic`; the actual ports are in `pkg/embeddings` and `pkg/embedindex`. Correct the owning paths.
- `pkg/app/indexing/unified.go:34-35` has the `ProgressSink` comment sitting above the `repoconfig` import. Move the useful contract sentence to the actual type or remove it if only restating the type.
- Remove short comments that only paraphrase the selected branch/loop while editing these files. Keep explanations of durable barriers, source authority, cross-process lease ownership, provider lifetime, cancellation/partial-cache semantics, and OS backend constraints. Those describe external contracts that code alone cannot establish.
- Two exported default-threshold constants in `pkg/embeddings/sqlite/code_chunks.go:162` and `note_chunks.go:225` have no repository consumers. They misleadingly advertise store-owned defaults while embedindex supplies the real defaults. Potential deletion belongs to the embeddings/store owner; coordinate rather than changing another lane. The 0.3 cutoff used to select a note-planning strategy is a different policy and must not be merged with retention solely because its value matches.

## Deliberate boundaries retained

- Batch and live already share `indexcore.Discover`/`Publish`. Batch streaming and live-derived scheduling differ intentionally. Do not merge their orchestration or move code candidate workers into indexcore, which would form a dependency cycle.
- Keep one durable writer, its explicit flush order, phase segments, exact-generation controls, and cancellation recovery debt.
- Keep ontology/raw-note fallback, provider dimension discovery, stale generation/lazy pruning, and provider identity partitioning. These represent supported cases, not arbitrary exceptions.
- Keep platform-specific watch backends and Windows metadata-sharing rules. The watch readiness poll also covers OS retry deadlines; merely replacing it with an event is not a proven simplification.
- Graph algorithm/read-model separation and indexingperf's typed metric catalog are already coherent. No actionable structural change found there in this audit.

## Limits

This is a source-backed structural audit, not a complete behavioral or performance certification. No production files, persisted schemas, user configs, or fixture vaults were changed. No broad tests, integration gates, or benchmarks were run by this lane. Root owns final integration and required verification.
