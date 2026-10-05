---
type: EffortNote
id: EFF-2026-10-04-09-52
aliases: [EFF-2026-10-04-09-52]
name: Shared batch and live indexing
created-at: 2026-10-04T13:52:34Z
status: complete
summary: Share indexing operations and recovery while keeping batch throughput and making live structural publication independent of embedding latency.
governing-specs:
  - "[[indexing-pipeline-architecture]]"
  - "[[semantic-runtime-lane-policy]]"
  - "[[navigation-read-availability-during-indexing]]"
---

# Shared batch and live indexing

## Scope

Consolidate ownership and dependency reconciliation, structural publication, and derived-work execution and recovery across batch and live indexing. Keep discovery, buffering, scheduling, and completion policy specific to each driver. Ordinary live edits publish metadata and ontology without waiting for an embedding provider. Derived work survives failures and restarts, retries without another filesystem event, and cannot overwrite a newer structural result. Preserve warm browser navigation and existing batch overlap and throughput.

Out of scope are new public GraphQL fields, embedding-provider changes, deployment, and changes to note authoring semantics. Topology and schema changes may use a complete metadata rebuild when existing dependency facts cannot prove a scoped update safe.

## Spec Set (Frozen)

- [[indexing-pipeline-architecture]] at `0839691`, including its shared writer, bounded pipeline, and batch/live requirements.
- [[semantic-runtime-lane-policy]] at `0839691`, including compatible shared nodes, latency policy, and successful-drain semantics.
- [[navigation-read-availability-during-indexing]] at `0839691`, including warm navigation and truthful readiness.

## Stories In Scope (Frozen)

The selected scope is the requirements of the three frozen specs as applied to the indexing refactor and latency work described above. No unrelated feature scope is selected.

## Spec Coverage Checklist

- [x] Shared operations replace duplicated processing and recovery code.
- [x] Structural publication progresses while provider work is stalled.
- [x] Durable recovery handles failure, cancellation, restart, and obsolete results.
- [x] Batch/live logical outputs agree after edits, deletion, renames, schema and ownership changes.
- [x] Comparable measurements protect live latency and batch throughput.
- [x] Required repository checks and public read verification pass.

## Plan

1. Capture baseline behavior with deterministic delayed/failing embedding providers and reusable batch/live workloads.
2. Extract the existing queued writer into a cycle-free application package. Share structural reconciliation and metadata/ontology publication, including explicit results and barriers. Use scoped sealed metadata for ordinary edits and explicit full fallback where required.
3. Persist coalesced derived-work debt before structural completion. Run physical provider work outside the vault writer lease, publish through short generation-checked writer jobs, and reuse the execution code for batch and live. Batch waits for requested work; live publishes structural readiness and retries derived work independently.
4. Integrate the two drivers and delete superseded processing helpers. Review ownership, cancellation, stale results, deletion, startup, and shutdown before broader verification.
5. Compare baseline and final performance, prove logical equivalence and browser/read availability, run `make check-full`, and update the owning subsystem notes.

GPT-6.1 Sol agents do implementation and verification. The coordinating agent independently reviews the design, integrated diff, and evidence. Each writer uses a separate worktree. Foundation review occurs before dependent runtime integration, with routine corrections covered by the existing approval.

### Approval

Drew approved the preceding four-part recommendation in chat on 2026-10-04 with "Okay, let's do this" and requested GPT-6.1 Sol subagents with independent oversight. This plan makes that approved scope executable. The configured Rhizome current user is absent, so `plan-approved-by` is omitted. Publication and merging are not part of this approval.

### Throughput checkpoint

Three initial lanes cover structural design, derived-work design, and baseline measurement. Production ownership splits into the shared core and runtime/derived execution, with baseline tests in a separate worktree. The main dependency is the shared writer/publication interface. Avoid parallel edits to that interface until both implementers agree on it.

## Original Intended Delivery

One implementation of each indexing operation with thin batch and live drivers. Warm application reads remain usable, structural updates become visible before embeddings finish, and semantic recovery proceeds without a new file edit.

## Actual Delivered

Batch and live indexing use shared ownership discovery, structural publication, and the extracted queued writer. Semantic computation is shared; live provider calls release the vault writer lease and publish only against current work and source fingerprints. Durable obligations recover after failures, cancellation, and restart, including unchanged explicit batch runs. Scoped metadata preserves incoming links. Committed path events refresh open panes so types, properties, and embedded content converge together. Superseded watcher processing, throttling, and schedulers were removed.

## Measurements

Synthetic workloads use identical source corpora and deterministic embeddings. Public timing uses filesystem writes, the actual runtime, HTTP reads, and SSE events with race detection.

| Workload | Baseline | Delivered |
| --- | --- | --- |
| First live edit to committed event | 3.082 s | 278 ms |
| Rapid repeat edit to committed event | 14.985 s | 243 ms |
| Cold 128 notes + 32 code files | 332–368 ms | 391–424 ms |
| Cold 512 notes + 32 code files | 1.133–1.208 s | 1.173–1.334 s |

Small cold runs add roughly 50–80 ms from scheduling and recovery controls. Larger runs remain around 1.2 seconds. Concurrent tests in another checkout made later batch repetitions noisy, so these ranges support a tradeoff report rather than a precise percentage claim. Final batches verify complete vectors and no outstanding derived debt.

## Execution Notes

- 2026-10-04T13:52:34Z [decision] Opened this effort against clean baseline `0839691`. Native GPT-6.1 Sol agents trace the shared core and derived work, with a third measuring baseline behavior in an isolated worktree.
- 2026-10-04T13:52:34Z [validation] Built the local binary with `make build`. Rhizome session `VNPShhVivlrTZmUC` started successfully. Initial concurrent recipe retrieval exceeded its deadline; a subsequent bounded code-evidence query succeeded with a truncated-context warning. Direct subsystem/spec reads cover the selected boundaries.
- 2026-10-04T13:58:27Z [validation] Independently reproduced the baseline quiet-retry defect. The recovered provider stayed at three calls, and all 16 pending ontology paths remained unretried. The new regression failed as expected.
- 2026-10-04T14:03:02Z [validation] Reviewed and integrated the shared writer extraction as `9899bc8`. Independent `go test -race -tags=fts5 ./pkg/app/indexwriter ./pkg/app/indexing` passed. Full verification remains pending. The worker's initial `make check` could not start its web checks because its isolated worktree had no installed web dependencies.
- 2026-10-04T14:22:44Z [validation] Independent public HTTP baseline measured filesystem-edit to readiness event at 3.08 seconds for a first edit and 14.98 seconds for a rapid repeat. Warm GraphQL reads took about 1 millisecond. Deterministic batch baselines were 349–357 milliseconds for 128 typed notes plus 32 code files and 1.197–1.228 seconds for 512 notes plus 32 code files, with vectors and inventories checked.
- 2026-10-04T14:22:44Z [review] Integrated shared structural publication, durable work fencing, and semantic compute stages. Independent review required global invalidation to fence path work, path edits to fence global work, direct-save source witnesses, and structural dependency finalization before activation. These corrections are included; runtime integration and final gates remain pending. The scoped metadata regression caught loss of link lookup behind the ownership readiness barrier; captured prior paths and aliases now supply that lookup.
- 2026-10-04T14:22:44Z [validation] `go test -race -tags=fts5 ./pkg/search/semantic -run 'Test.*Derived' -count=1` passed. This verifies the new ontology compute/publication separation, not the complete runtime. A new independent read-only review is running against the integrated shared code.
- 2026-10-04T14:32:22Z [validation] Integrated runtime checkpoint `0f146bc1`. Independent public HTTP tests passed with race detection for blocked and failing providers. Healthy filesystem writes announced `node.changed` in 330 and 262 milliseconds; final paired timing remains pending. Independent race tests for `indexcore`, `indexwriter`, `notemeta`, and `indexing` passed before this runtime checkpoint. `make build` passed.
- 2026-10-04T14:32:22Z [validation] Browser smoke test against a disposable synthetic vault showed a filesystem edit updating the open note's type, property, embedded section, outline anchor, and sidebar without a page reload. A repeat edit also appeared, and navigation through a warm unaffected tab remained usable. The collaborative browser required the fixture's LAN address and matching application origin.
- 2026-10-04T14:32:22Z [review] Independent review found schema-only refresh could no-op, provider-owned HTML could enter Markdown-only semantic preparation, and structural caller/scope changes needed additional code obligations. Repairs are underway. The exact batch/live equivalence test also found an incoming graph link lost after a scoped edit; it remains a blocking regression. Its timeout polling race was fixed without weakening comparisons. Fast checks exposed a generated Greptile guidance-map mismatch after adding the new packages; the map was regenerated.

- 2026-10-04T14:46:18Z [review] Fixed incoming-link preservation and verified exact batch/live equivalence through content edits, rename/delete, schema changes, and scope retirement with race detection. Integrated delayed graph recovery and final complete code-context planning after streaming drains. Review also found active note/ontology debt could be cleared by an unchanged batch; repair and regression coverage are in progress. Provider-aware semantic preparation now has a shared snapshot API.
- 2026-10-04T14:46:18Z [validation] Public HTTP race tests passed again: first and repeat filesystem edits announced `node.changed` in 212 and 268 milliseconds. The combined bootstrap command still failed to compile old helper-based tests that the runtime cleanup checkpoint replaces; it is not recorded as an overall pass.

- 2026-10-04T14:52:41Z [review] Independent follow-up review accepted `6ac3cdc5`: active Notes/Ontology recovery now replays unchanged sources before acknowledgement, with raw/typed delayed-debt regressions and provider-visible HTML evidence.
- 2026-10-04T14:52:41Z [performance] Interleaved precompiled benchmark runs isolated a small-corpus cold-index regression: about 500 milliseconds for 160 files versus 350 milliseconds baseline. The 544-file workload remains about 1.2 seconds in both versions. Profiling the added fixed delay is in progress; throughput verification is not complete.

- 2026-10-04T15:00:39Z [validation] The first `make check-full` run passed formatting, static analysis, web type/lint, credential scanning, benchmark contracts, and all Go packages except two regressions. Bootstrap rejected an absolute code root equal to the vault root; `f861e983` fixes canonicalization. The note-format architecture test correctly rejected parser selection in semantic code; projection is being routed through the existing ontology source seam. Web unit and integration stages did not run after that failure.
- 2026-10-04T15:00:39Z [performance] Phase profiling found early intent submission missed code/note preparation and caused a second 150-millisecond batching delay. `8bbf2e27` starts intent work at shared ingestion readiness; its provider wait falls to 43–53 milliseconds and all three sources share one batch. Preliminary 160-file runs fall from about 500 to 403–412 milliseconds; final quiet comparison remains pending.

- 2026-10-04T15:03:15Z [review] Integrated the neutral ontology projection seam and canonical-root regression tests. Runtime tests cover physical shared-node shutdown, quiet reopened-store retry, stale direct-save results, and retired code cleanup. Comment review removed 13 narration lines, retained public boundary contracts, and introduced no suppressions or unenforced constraints. No outstanding implementation finding remains; final gates and paired measurements are running.

- 2026-10-04T15:07:17Z [validation] `make check-full` passed at `6b8e0834` with the owned comment/doc changes and new verification fixtures present. This includes all Go packages with race detection, 146 web test files / 1,341 tests, integration packages, mixed integration tests, static checks, credential scanning, and benchmark contracts.

- 2026-10-04T15:10:19Z [validation] `make build` passed. `RHIZOME_E2E_PORT=51897 CI=1 make web-e2e` passed all 85 browser journeys against its disposable fixture. The final manual browser check nevertheless found retained tab metadata: an external type change updates the sidebar, fields, and embedded anchor, but the already-open header/context retains its former type. This observable gap is being repaired with focused regression coverage before closure.

- 2026-10-04T15:19:39Z [review] `86238d88` handles committed path events in open panes, including pending initial reads. The focused UI suite passed 56 tests, type checks, and lint; regressions cover old type, removed type, unaffected paths, and late old responses. The implementation preserves the existing staged-read and cancellation paths.
- 2026-10-04T15:19:39Z [performance] Final public HTTP race measurement: first filesystem edit to `node.changed` 278 milliseconds; rapid repeat 243 milliseconds, versus baseline 3.082 and 14.985 seconds. Warm reads remained usable. Batch comparisons use identical deterministic corpora and verify 192 / 576 durable vectors plus zero remaining debt. Quieter post-fix samples were 391–424 milliseconds for 160 files and 1.173–1.334 seconds for 544 files; corresponding baseline samples were 332–368 milliseconds and 1.133–1.208 seconds. Later paired repetitions overlapped another checkout’s test processes and varied widely, so no precise percentage throughput claim is made. Small cold runs retain roughly 50–80 milliseconds of extra scheduling/recovery cost; larger runs remain around 1.2 seconds.

- 2026-10-04T15:22:00Z [validation] Rebuilt after the pane fix. The actual browser now updates sidebar, open-pane header, context type, scalar fields, and embedded anchor after an external change from Before to After; removing the tag clears header/context to Untyped. Warm navigation to an unaffected tab remains usable. The fixture server was stopped after verification. A final `make check-full` rerun includes the pane change.

- 2026-10-04T15:23:34Z [validation] Final `make check-full` after `86238d88` passed, including Go race/integration tests and 146 web test files / 1,344 tests. Final browser-suite rerun and closure validation remain pending.

- 2026-10-04T15:25:47Z [validation] Final browser-suite rerun passed all 85 journeys with the pane fix included. The final build, full checks, public read tests, and manual browser checks are complete.

- 2026-10-04T15:26:19Z [validation] Closure validation passed with zero issues: ontology, identifiers, broken links, and frozen scope drift. All scope and closure checks are complete.

## Deviations

None.

## Compounding Follow-ups

None identified.

## Closure Checklist

- [x] Implementation and independent review complete.
- [x] Focused tests, performance comparison, and full checks pass.
- [x] Actual outcome matches the approved scope.
- [x] Owning docs and validation are reconciled.

## Status

Complete. Shared batch/live processing, durable recovery, and committed pane refresh are implemented and independently reviewed. Full checks, browser verification, logical equivalence, and performance comparisons passed. The measured small-corpus cold-index cost is recorded above.
