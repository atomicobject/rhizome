# Indexer reliability and performance evidence

These materials support the [closed effort](../../2026-11-13-13-10-indexer-reliability-performance.md). The [approved plan](plan-r4.md) and [findings](findings.md) are unchanged copies of the original investigation. [Delivery evidence](verification.md) records implementation commits and supplied verification. [The measurement](measurement.md) records the workload and its limits.

## Model and charts

`LiveIndexer.tla` models one file in the original live indexer at `d581e3487`. It includes watcher publication, durable derived tickets, provider computation, interactive writers, runtime restarts, and explicit indexing. It does not model grouped tickets, multiple files, real provider limits, SQLite contention, or the delivered fixes. The model is supporting evidence, not an exit gate or a liveness proof.

The original full run explored 5,879 states. `TypeOK`, `QuietIndexMatchesFile`, `QuietEmbeddingsMatchIndex`, and `EmbeddingsNeverAhead` held in that bounded model. Four proposed invariants had counterexamples. A Rhizome save leaves stale embeddings without a ticket until its file event or restart discovery. A yield expands recovery to the whole vault. An unseen edit causes immediate retries. An interactive writer cancels provider work and delays its retry. The plan fixes or preserves these behaviors explicitly.

`structural.cfg` isolates the watcher. `embedding.cfg` isolates the scheduler. `full.cfg` includes restart and explicit-index actions. The two `view-*.mjs` files supply labels for the chart regions. `structural.png` and `embedding.png` are the supplied historical chart captures. They describe the original behavior and retain the original framing.

## Rerun the model

Use the t3-pstack `statechart` skill's `scripts/statechart` command. Its first run needs network access to install TLC and Java through mise. Run from this materials directory and write generated output to a new scratch directory outside the repository. Set `STATECHART` to that installed command and `OUTPUT` to the scratch directory.

```sh
"$STATECHART" LiveIndexer.tla --config full.cfg --out "$OUTPUT/full"
"$STATECHART" LiveIndexer.tla --config structural.cfg --view view-structural.mjs --out "$OUTPUT/structural"
"$STATECHART" LiveIndexer.tla --config embedding.cfg --view view-embedding.mjs --out "$OUTPUT/embedding"
```

The command writes `report.md`, TLC output, and `chart.mmd`. Counterexamples to the four proposed invariants are expected. Use the statechart skill's Pele rendering instructions to render new charts. The checked-in PNGs preserve the original captures. This closure pass did not rerun TLC or recreate the PNGs.

## Real tests replace the replay file

The temporary `zz_statechart_replay_test.go` is deliberately excluded. Its reproductions became behavioral regressions in the repository.

| Original replay | Real tests |
| --- | --- |
| `TestReplayYieldEscalatesToWholeVault` | `TestWatcherCancellationBeforeMarkPreservesScope`, `TestWatcherCancellationAfterMarkKeepsCoarseRecovery`, and `TestWatcherCancellationDuringTrailingRefreshMarksCacheStale` in `pkg/app/bootstrap/live_ownership_cancel_test.go` cover corrected pre-mark behavior and retained post-mark recovery. |
| `TestReplaySchedulerSpinsOnUnseenEdit` | `TestDerivedSchedulerDelaysUnseenEditWithoutFailedAttempt` in `pkg/app/bootstrap/derived_retry_test.go` checks the delay, unchanged attempt count, zero provider work, and reactivation after structural publication. |
| `TestReplayPriorityDelaysEmbedding` | `TestDerivedPriorityPreservesProviderWithoutHoldingWriterLease` in `pkg/app/bootstrap/derived_scheduler_test.go` checks five external priority requests without provider cancellation or added attempts. `TestDerivedRetryCountsFailuresButNotPrepareOrPublicationInterruptions` in `derived_retry_test.go` covers lane interruptions. |
| `TestReplayExplicitIndexUsesStartupSelection` | `TestExplicitIndexUsesAppliedSelectionAndPreservesPendingConfiguration` in `cmd/serve_index_job_hooks_test.go` checks both selection directions through the real registered hook and preserves a pending disk configuration edit. `TestValidationAndRepairProjectionFollowReloadedSelection` in `pkg/app/cli/serve/validation_selection_test.go` covers the other runtime consumer. |

`TestLiveSaveThenRestartReembedsWithoutFileEvent` in `pkg/app/bootstrap/live_save_restart_test.go` separately protects the save/restart dependency that the model exposed. Run the focused real tests from the repository root with the repository's `fts5` build tag. See [delivery evidence](verification.md) for supplied runs and the effort for checks performed during closure.
