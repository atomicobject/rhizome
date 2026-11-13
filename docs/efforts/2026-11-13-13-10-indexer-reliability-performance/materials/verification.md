# Delivery verification evidence

The closure pass inspected `git log --oneline d581e3487..54d86c054`, the implementation diffs, the supplied worker reports, and the named real tests. The reports describe checks on their own source revisions. They are supplied evidence, not commands rerun by the documentation worker. Their original temporary logs are not durable repository artifacts.

## Worker and integration commits

The integration history preserves the changes with different commit IDs for most worker slices.

| Report | Worker commit(s) | Integration commit(s) |
| --- | --- | --- |
| `batch4-worker-report.md` | `fe99c22c7`, `8f2931f6f` | Same commits. |
| `rhizome-batch1-worker-report.md` | `e765a9620`, `8603a865c`, `7211d16f3`, `5cd2a3550` | `b44af6663`, `ec57191e9`, `5c0c82f68`, `b9ad84080`. |
| `rhizome-c7-c4-worker-report.md` | `39bb05655` | `ae2e14b72`. |
| `rhizome-scheduler-worker-report.md` | `5d1bf0926`, `b22ab6b4e` | `8bfa18d17`, `4a6c3afe6`. |
| `indexer-linker-worker-report.md` | `03f365039`, `ac89ebd21`, `ba6d29012` | `85abb2883`, `b450d26f2`, `ab21887d9`. |
| `index-stop-diagnosis.md` | `23f1eec06` | `0fc28ce4f`. |
| `rzm-batch3-report.md` | `e58daaa68`, `91d6dc6d2` | `b422bc785`, `54d86c054`. |
| `rzm-batch3-measurement.md` | Baseline `e58daaa68` and a pre-cleanup batch 3 implementation. | [Measurement details](measurement.md). |

The batch 3 worker started from `4a6c3afe6`. Its complete gate does not cover all other changes subsequently combined in `54d86c054`. The coordinator must verify the integrated revision before merge.

## Supplied behavioral evidence

- Batch 1 reproduced selection divergence in both directions and graph scores that survived code retirement. Its corrected tests passed. The suspected C6 early version stamp did not reproduce, so that item added a guard without a production change. Validation and repair use the applied selection. The initial equivalence skip for C4 was removed by `85abb2883`.
- C7/C4 tests reproduced unchanged Python code retaining old fully qualified names and an absent live scope stamp. Live and batch tests then passed. Failure/recovery tests preserve old configuration hashes until convergence succeeds. The zero-text test changes a parse limit while embedding inputs remain unchanged.
- Batch 2 original-source overlay tests reproduced missing supersession delay, failure attempts on interruption, cancellation during external priority, no-schema debt, code re-sends, code-first ordering, and lost in-flight vectors. The corrected tests passed. The code retry sent zero repeated texts. The 500-code-file test sends the note's text before the global code ticket runs.
- Batch 3 tests cover typed and untyped 50-note groups, independent survivors, direct saves, overlapping RECORD inputs, partial provider failures, failed backlogs, input-specific isolation, restart recovery, survivor-only cleanup, provider-sized packing, path normalization, and exact acknowledgement counts. A 1,500-text plan produces requests of 1,000 and 500 while retaining byte limits.
- Batch 4 cancellation tests failed before R1a for the pre-mark stale mark. Deterministic retry tests failed before backoff and exposed a late-wake input-version race, which was fixed. The final focused race suite passed. Save/restart and retained diagnostic events were tested without a real embedding service.
- Batch 5 reproduced stale links and missing upgrade repair. All ten final equivalence sequences and version-repair assertions passed in the affected-package race tests. Unchanged repair text sent zero provider texts and retained vectors. The extra disabled-language test failed on the unsupported Python candidate before the fix and passed after it.
- Stop/index diagnosis reproduced EOF and connection refusal on base `d581e3487`. The final recovery stress passed 10 invocations, containing 30 races, in 52.915 seconds. No racing interruption occurred in that final sample. `TestRuntimeIndexStreamReportsInterruptionDuringShutdown` deterministically exercised the missing-result branch and passed 20 race-enabled repetitions. The precise cleanup delay remains unisolated.

## Supplied gates and limitations

The batch 3 worker reported these final successful commands at `91d6dc6d2169e869e51d132177e5ae7765eb661f`.

```sh
make build
make build NO_WEB=1
GOCACHE=$PWD/.gocache go vet -tags fts5 ./pkg/app/bootstrap/... ./pkg/embedindex/... ./pkg/store/... ./pkg/embeddings/...
RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm validate
make check-full GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4 CHECK_JOBS=2
GOCACHE=$PWD/.gocache go test -mod=vendor -tags 'fts5 integration' -race -p 4 -parallel 4 -count=1 ./tests/integration/...
GOCACHE=$PWD/.gocache go test -mod=vendor -tags 'fts5 integration' -race -p 4 -parallel 4 -count=1 -run '^TestIntegration' ./pkg/vault/cache ./pkg/anchors ./pkg/validate
```

Documentation validation reported zero issues. Some unchanged Go package results in the full gate were cached. The two explicit `-count=1` integration commands ran freshly. Web tests passed 2,224 tests across 272 files and all ten supervisor tests. These source inputs were not identical to the complete integrated revision.

Earlier failed gates remain part of the record.

| Report | Failed or incomplete gate evidence |
| --- | --- |
| Batch 4 | `make check-full` exposed the fixed late-wake retry defect, then stopped after two serve discovery-port shutdown failures. Full integration was not reached. Final focused bootstrap, lane, and cache race tests and `make check-fast` passed. |
| Batch 1 | The broad race gate failed the unchanged validation-product FIFO test's two-second bound under load. A separate integration stage failed `TestIndexRacingStopStillSucceeds`. Later isolated passes were diagnostic only. Web and mixed integration passed separately. |
| C7/C4 | Full unit/race and static checks passed, but full integration failed the stop/index test with an EOF. Separate mixed integration passed. |
| Batch 2 | The full gate failed the stop/index test after affected-package race suites, static checks, web checks, and other integrations passed. Separate mixed integration passed. Baseline isolated runs did not establish the cause. |
| Batch 5 | Full-gate command, agent-code, and serve tests failed. All affected-package race suites passed. Fresh integration passed every package except progressive code discovery, which failed during runtime shutdown. Web and mixed integration passed separately. |
| Stop/index diagnosis | Its full gate failed two serve shutdown tests and its `make check` failed the watcher-before-boot guard. Focused tests, web tests, and separately completed integration phases passed. Those broad failures were not concealed or fixed by this test-contract change. |
| Batch 3 | An earlier `make check` timed out in `TestServeCommandPrintsURLAndRegistersInstance`. A later full gate passed on that worker tree. Its shutdown cause remains unexplained. |

No passing rerun establishes a fix for an unexplained failure. No retry, increased timeout, or skip was added to hide these failures. Windows, real providers, and live web catalog behavior were not verified by these reports. The week-long D1 verdict and batch 6 largest-vault measurement remain unperformed.

## Checks performed for documentation closure

The owning [effort record](../../2026-11-13-13-10-indexer-reliability-performance.md) records this pass's build, identifier allocation, documentation validation, frozen-scope check, and `make check-fast` result. This pass changes no Go or web source and does not supply a new integrated full-gate or independent-review verdict.
