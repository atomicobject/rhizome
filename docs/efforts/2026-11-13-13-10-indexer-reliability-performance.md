---
type: EffortNote
id: EFF-2026-11-13-13-10
aliases: [EFF-2026-11-13-13-10]
name: Indexer reliability and performance
created-at: 2026-11-13T18:15:20Z
plan-approved-by: Drew Colthorp
status: complete
summary: Make live and batch indexing consistent, retain work through interruption, and group embedding requests within provider limits.
governing-specs: []
---

# Indexer reliability and performance

## Scope

Make live and batch indexing agree, retain changes and reusable vectors through interruption, and drain embedding work in provider-sized requests. Preserve the owner's rule that text already embedded with the same provider, model, and dimensions is reused.

The implementation spans `d581e3487..54d86c054` on integration branch `t3/indexer-reliability-performance`. This closure note is created retrospectively on 2026-11-13 from the approved plan, delivered commits, and worker evidence.

## Spec Set (Frozen)

No governing spec exists for this work. The frozen scope is [plan revision 4](2026-11-13-13-10-indexer-reliability-performance/materials/plan-r4.md), with finding IDs from the [original investigation](2026-11-13-13-10-indexer-reliability-performance/materials/findings.md). The checked-in plan is unchanged, with SHA-256 `8e1fb3cc5c1ddceecf34009db5b969d93ed7112ae0e4d68f2b4e09ce1db0a717`.

The council reviewed the plan over three rounds. Round 3 scores were Opus 8.5, Fable 9, and Sol 9. Revision 4 incorporates the required corrections. The owner, Drew Colthorp, approved revision 4 and its scope on 2026-11-13 with "Let's just get started". The coordinator supplied this approval evidence for closure. The approval's exact time was not supplied.

## Stories In Scope (Frozen)

There are no governing story or acceptance-criterion nodes. The plan's batches 1 through 5, conditional batch 6, and decisions D1 through D4 are the selected obligations.

## Spec Coverage Checklist

- [x] Batch 1 covers C1, C3 through C7, current selection/configuration, parser reconfiguration, graph retirement, and deterministic live/batch equivalence.
- [x] Batch 2 covers R2, R3/D2, R6, and S4 ordering and deduplication.
- [x] Batch 3 covers S1, S2/S5, combined witness/planning, member-safe publication, recovery, and count-based throughput tests.
- [x] Batch 4 covers R1a, R4, R5, retained diagnostics, and D1's coarse post-mark recovery. Its week-long measurement is carried forward below, as the plan permits.
- [x] Batch 5 covers C2, batch-local linking, version repair, and link equivalence.
- [x] Batch 6/D3 remains conditional. No narrowing was built without the required measurement.
- [x] D4 preserves pending configuration edits during runtime-delegated indexing.
- [x] Original model evidence and the permanent replacements for all four scratch replay tests are recorded in the [materials README](2026-11-13-13-10-indexer-reliability-performance/materials/README.md).

## Plan

The complete [approved revision 4](2026-11-13-13-10-indexer-reliability-performance/materials/plan-r4.md) retains the concrete tasks, affected modules, exit criteria, test strategy, and documentation changes. Its decisions are frozen here.

| Decision | Approved behavior |
| --- | --- |
| D1 | Recovery after an interruption past `mark()` stays coarse. A week of real-vault diagnostics decides whether to open a follow-up. |
| D2 | External writer priority leaves lock-free provider computation running. Explicit indexing may still preempt it. Interruption does not count as a provider failure. |
| D3 | Skip a note edit's global code ticket only if batch 6's measurement justifies it. One complete input set must govern both ticket creation and the note side of the code witness. |
| D4 | Runtime-delegated explicit indexing uses the runtime's applied configuration snapshot and never rewrites configuration. Standalone indexing retains persistence. |

The plan originally assigned one PR per batch. Batches 1 and 2 were independent, batch 3 needed 2, batch 4 was independent, batch 5 needed 1, and conditional batch 6 needed 2 and 5. Deterministic rows, provider calls, and job counts are test evidence. Wall times are measurements, not CI assertions.

## Original Intended Delivery

1. Batch 1 makes explicit indexing and validation use the applied selection, accepts absent ontology, recomputes retired-code graph scores, guards indexer upgrades, applies parser settings and watch roots, and certifies the applied hashes. Live and batch snapshots agree across edits, deletions, renames, selection changes, and parser changes.
2. Batch 2 delays superseded work without failed attempts, preserves provider computation across external priority, retains interrupted vectors, prioritizes notes and ontology, and deduplicates global code paths.
3. Batch 3 groups path tickets, verifies each member and all consumed sources inside publication leases, preserves independent members, packs complete plans to provider limits, and proves recovery after changes, failures, and restarts. Fifty-note bursts use at most two calls and 20 lane jobs per tested kind.
4. Batch 4 preserves scope when cancellation restores inputs before mutation, backs off structural errors, protects save/restart embedding recovery, and proves diagnostics survive a restart. Post-mark recovery remains coarse under D1.
5. Batch 5 links live code to the complete Markdown note set using a batch-local parser and repairs old rows through an indexer-version bump without resending unchanged text.
6. Batch 6 runs only if a note edit on the largest vault costs more than two seconds of global code background work or delays another ticket's provider call. It then uses one proven complete input set for ticket marking and witness checks.

C10, C11, R9, S6, and S7 were dropped. S3 was dropped as a separate item, while its combined witness/planning lease entered batch 3. C8, C9, C12, R7, R8, and S8 were deferred with explicit triggers.

## Actual Delivered

Batches 1 through 5 are implemented through `54d86c054`. Commit IDs below are the integration-branch commits. [Delivery evidence](2026-11-13-13-10-indexer-reliability-performance/materials/verification.md) maps them to the worker reports and distinguishes supplied checks from this documentation pass.

| Batch | Delivered behavior and integration commits |
| --- | --- |
| 1 | `b44af6663` uses one existing `LiveRuntime.Snapshot()` for explicit indexing, validation, queries, and projection readers and prevents runtime config writes. `ec57191e9` refreshes graph scores after code retirement. `5c0c82f68` guards watcher-before-boot version repair. `b9ad84080` adds deterministic sequence comparisons. `ae2e14b72` rebuilds both parser-service holders, updates watch roots, reparses code on parser-hash changes, and stamps applied hashes after successful convergence. C3 arrived with batch 2. |
| 2 | `8bfa18d17` adds interruption and ordering regressions. `4a6c3afe6` implements supersession delays, no-schema drain, priority-preserving computation, vector reuse, kind ordering before LIMIT, and global path deduplication. `b422bc785` removes duplicate successful code-cache writes and bounds in-flight cancellation to five seconds once all submitters cancel. Active shared peers retain their call. |
| 3 | `54d86c054` groups up to 64 ready path tickets per kind, including failed backlogs. Global tickets stay single. It captures per-member consumed inputs with the plan, checks generation/epoch and fingerprints on publication, limits cleanup and acknowledgement to survivors, preserves overlapping RECORD sets, and replans independent ontology members from cached vectors. Complete-plan packing reaches the physical shared node. Grouped events count matched acknowledgements. |
| 4 | `fe99c22c7` retains restored watcher scope before marking, preserves coarse post-mark recovery, and backs structural failures off from one tick to one minute. Fresh input resets the delay. `8f2931f6f` guards save/restart re-embedding and diagnostic retention across restart. |
| 5 | `85abb2883` removes the temporary C4 equivalence skip. `ab21887d9` builds the Markdown linker after dependency expansion, carries authoritative link replacement with parsed work, retains writer cache invalidation, bumps `IndexerVersion` to v1.18.0, and adds doc-link equivalence and zero-resend repair tests. |
| 6 | Not built by design. The required largest-vault measurement has not been performed. D3 remains a conditional follow-up. |

`b450d26f2` additionally filters disabled languages in automatic-scope discovery. `0fc28ce4f` corrects the pre-existing stop/index race's test contract and adds a deterministic stream-cancellation regression.

Subsystem notes for indexing, vault runtime, code intelligence, and stores and the affected package context notes were reconciled on the implementation branch. `CHANGELOG.md` records grouped live embedding and bounded cancellation.

### Measured results

The batch 3 worker's single successful runs used 50 typed-note edits in one watcher batch and a synthetic provider with 100 ms per physical call. Both produced 50 current persisted embedding paths, sent exactly 50 texts, and recorded zero failures.

| Metric | Before | After |
| --- | ---: | ---: |
| Drain | 9.80 s | 573 ms |
| Provider calls | 50 | 1 |
| Lane jobs, including watcher publication | 255 | 9 |

These are single runs on a busy host with different observed loads. They show fewer serial requests and lane admissions, not a stable latency estimate. The [measurement record](2026-11-13-13-10-indexer-reliability-performance/materials/measurement.md) identifies the exact workload and source revisions. This closure pass did not repeat the benchmark.

## Execution Notes

- 2026-11-13T18:15:20Z [decision] Created this retrospective closure record under the owner's documentation brief. Approval of revision 4 was supplied as Drew's 2026-11-13 instruction, "Let's just get started". No approval time was invented.
- 2026-11-13T18:15:20Z [validation] Confirmed the starting HEAD with `git log --oneline -1`, which returned `54d86c054 perf(indexing): group derived embedding tickets`. Inspected the implementation range, worker reports, model, configurations, replay source, views, and both supplied PNGs. Permanent test names were checked in the current source.
- 2026-11-13T18:15:20Z [learning] `unready_derived_work` is retained as `trigger_unready_derived_work` on `batch.inputs`. Full-discovery reasons are `resync` or `pending_reconciliation`. D1's measurement must correlate them by operation ID rather than expect a dedicated full-discovery counter.
- 2026-11-13T18:15:20Z [validation] `make build NO_WEB=1` first built the CLI but failed at team API dependency downloads under sandbox DNS. The same command passed with host access. The combined code-mode authoring request timed out. After stopping the task-created runtime, the autostart-disabled one-shot allocator completed initial indexing and returned `EFF-2026-11-13-13-10`. The runtime authoring recipe also completed. No runtime or source fix was made.

- 2026-11-13T18:18:18Z [validation] `RZM_SKIP_REPO_DELEGATE=1 ./scripts/rzm validate` passed with zero issues and zero errors. The same launcher's `validate frozen-scope-drift` passed with no drift. `make check-fast` passed, including a fresh `fts5 repo_content` suite in 2.056 seconds, static analysis, vet, and web checks. The `closure-drift-pack` recipe resolved this note as `EffortNote`, status `complete`, with no query errors or workspace assessment issues. Evidence copies match the originals, view modules pass `node --check`, and all named replay replacements exist in the current source.

## Deviations

- The plan's separate batch PRs were consolidated on one integration branch. This record closes the combined delivery before its PR. The coordinator will insert the PR number and refresh closure if review changes the work.
- Batch 6 was deliberately not implemented without its measurement. The week-long D1 measurement is also carried forward, as revision 4 permits before a week has elapsed on a build containing R1a.
- Disabled-language filtering was an extra correctness fix. C7 exposed a disabled Python candidate reaching an absent parser under automatic scope. `b450d26f2` fixes both discovery callbacks and adds a failing-before-fix regression.
- C6's suspected early version stamp did not reproduce. Existing publication already reparsed old-version rows. The delivered change is a regression guard.
- C1 reused the existing coherent `Snapshot()` rather than adding a getter. C7 needed no shutdown fallback. Its zero-provider-text assertion uses a parse-limit change with unchanged embedding inputs. Root changes can alter fully qualified names in embedding text and legitimately require new texts.
- Batch 5 builds its linker after dependency expansion, since retiring a note can add referring code to the batch. Note additions alone still do not relink unchanged code. Note-add and rename tests compare with incremental batch output and exclude doc links and graph scores from fresh-batch comparison. Alias mentions are excluded from that fresh comparison.
- The stop/index flake predates this effort. The diagnosis reproduced EOF and connection-refused outcomes on `d581e3487`. Runtime shutdown may end the HTTP stream before the job result and before ownership is released within the existing grace. `0fc28ce4f` allows the racing index to exit with 1 or 130, still requires successful stop, and then requires ordinary indexing and a persisted symbol read to succeed. The deterministic HTTP test proves stream cancellation does not cancel the shared job. No product retry or timeout was added. The exact cleanup operation that exceeded the grace remains unknown.
- `TestIndexRacingStopStillSucceeds` now overstates its assertion. It verifies a normal interruption and later recovery, not unconditional success of the racing index. Its name is retained in this delivery and recorded for correction below.
- The C1 audit found a web `FileCatalog` constructed with the startup selection. Suggestions, classification, and note/viewer projection retain that selector. Correcting it requires cache invalidation and coordination with HTTP readers. It was left out of scope and was not exercised in a browser.
- Earlier worker gates had unresolved serve/shutdown and other test failures. A later batch 3 full gate passed on its worker tree, with some cached results. That pass does not explain earlier failures or prove the complete integrated HEAD passed. The coordinator retains integrated review and CI before merge. This documentation closure does not declare merge readiness.

## Closure Checklist

- [x] Revision 4, its actual owner approval, and the absence of a governing spec are recorded without backdating creation.
- [x] Every batch and D1 through D4 is delivered or covered by an explicit deviation or conditional follow-up.
- [x] Integration commits, original evidence, count-based tests, measurements, and verification limits are recorded.
- [x] Owning subsystem documentation and the implementation changelog are reconciled.
- [x] The model, three configurations, two views, and two PNGs are checked in. Every scratch replay has a named permanent replacement. The scratch Go file is excluded.
- [x] Deferred work has an owner and a trigger. The week-long measurement is explicitly allowed to follow closure by the approved plan.
- [x] Documentation validation and frozen-scope drift report zero issues. `make check-fast`, including content checks, passes.
- [x] Required integrated review and CI remain with the coordinator before merge. This pass changes documentation only.

## Compounding Follow-ups

Drew owns access to real-vault diagnostics and the decision to start the conditional or deferred work. No follow-up below is evidence that its trigger has occurred.

- [ ] Measure D1 on real vaults for a representative week after installing a main build containing R1a. Count interruptions separately for external priority and explicit indexing, recovery-pass cost, and repeated interruptions without completion. Open a scoped-recovery follow-up if any recovery pass is interrupted three or more times in a row, or if recovery after interruptions costs more than five minutes of indexing during the week. Correlate retained lane yield/preemption events, restored batches, and full-discovery records across runtime sessions. If retained data is incomplete, record "insufficient evidence", not "below threshold". #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Measure the global code ticket after one note edit on the largest vault with batch 2 present. Start batch 6 only if background work exceeds two seconds or delays another ticket's provider call. If triggered, prove one complete set of note-written `IntelDocSections`, `IntelEdges`, and `doc_links` inputs and use it for both marking and the code witness. Keep marking when that set changes, then verify linked/unlinked edits and equivalence. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Triage the FileCatalog stale-selection finding in a separate web lifecycle change. Reproduce a live selection reload through suggestions, classification, and viewer projection, then coordinate selector refresh and file-cache invalidation with concurrent HTTP readers. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Revisit C8's orphan ontology chunks if measured search recall falls, another batch touches ontology node retirement, or live/batch equivalence fails on orphan chunks after delete or rename. Search hides the orphans after nearest-neighbor selection, so they can consume candidate slots. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Revisit C9's long-lived TypeScript import tail index when a fixture reproduces divergent import resolution between live and batch indexing. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Revisit C12's unavailable-at-start provider syncers after a user report or a credential-entry flow that does not restart the runtime. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Revisit R7's independent 429 backoff without concurrency reduction when diagnostics contain 429s or a 429 test harness exists. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Revisit R8's fixed throughput without rate-limit headers when a user of an endpoint without those headers reports slow indexing. The observed defaults are three requests per second and 80,000 tokens per minute. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Revisit S8's duplicate startup work only after a measured slow restart on a large vault and after saves have durable derived tickets under R4. Do not remove restart discovery while saves still rely on it. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Rename `TestIndexRacingStopStillSucceeds` to describe interruption and recovery when next editing that runtime test. Preserve its exit-status, stop, subsequent-index, and persisted-read checks. #action-item
  assignee:: [[Drew Colthorp]]

The deterministic cancellation, retry, member-publication, packing, and equivalence tests capture the reusable lessons. The six-millisecond lane cost remains deferred as a separate optimization. Revisit only if grouped measurements show lane jobs as the remaining bottleneck or a job exceeds 20 ms on Windows.

## Status

Complete on the branch under the owner's closure brief, with implementation recorded through `54d86c054`. Conditional batch 6, the D1 diagnostics week, and deferred findings remain explicit follow-ups. The coordinator owns integrated review/CI, inserts the PR number once it exists, and updates this record before merge if the delivered scope changes. Completion does not authorize publication.

PR: pending
