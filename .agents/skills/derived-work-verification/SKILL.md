---
name: derived-work-verification
description: Use when a change touches derived-work tickets or the scheduler that executes them (pkg/store/derived_work.go, derived_witness.go, pkg/app/bootstrap/derived_*.go, pkg/app/indexcore/publish.go or candidates.go, the derived acknowledgement in pkg/app/indexing/unified.go), or when embeddings look stale or lost after live indexing. Model-checks the ticket protocol with TLA+ in about 35 seconds.
---

# Derived-work verification

`model/DerivedWork.tla` is a TLA+ model of the live indexer's derived-work protocol: durable tickets that turn committed structural changes into embedding output, and the scheduler that groups, witnesses, publishes and acknowledges them. TLC checks every interleaving within the model's bounds, so it finds orders of events that tests rarely reach. The model checks one derived kind at a time. Kinds share no ticket, epoch or revision rows, and lane jobs run one at a time.

The protocol's promise, as TLC rules:

| Promise | Rule | Harm it prevents |
| --- | --- | --- |
| P1. No stale publication | `NoStalePublication` | Results, cleanup and acknowledgements are written only while the ticket is current (generation, kind epoch, and the kind revision for a global ticket) and every input the plan read is unchanged. Otherwise search serves output built from text that is no longer indexed. |
| P2. No lost work | `QuietMeansCurrent`, `NoAckWithoutPublication`, `AckNeverCoversNewerChange`, `EventuallyQuiet` | When the indexer goes quiet, every output was built from the current rows of every path it depends on. No ticket is acknowledged for results that were not published, and no run's acknowledgement covers a change made while the run was in flight. Otherwise embeddings stay stale with no ticket left to repair them. |
| P3. Grouping is safe | `GroupPublishesOnlyCurrentMembers`, cover `CoverSurvivorsContinue` | A grouped run writes outputs only for members that are current at that moment. When one member drops, the survivors still publish (notes and code) or are replanned (ontology). Otherwise one note's edit either corrupts or stalls its group. |

`QuietMeansIndexed` checks the model itself: it says the structural rows match the files whenever the indexer is quiet. `CoverSurvivorsContinue` is meant to break. Its counterexample shows that the survivor path is reachable.

Sync commit: `a9c493c9c`. This is the commit at which the model was last confirmed to describe the covered code. `model/sync.txt` records each covered file's blob at that commit, so the record still works when a squash merge leaves the commit out of your history.

## When your change touches the covered code

1. **Find what changed.** Run `.agents/skills/derived-work-verification/check --coverage`. It lists every covered file whose content differs from the verified version. Read the diff of each changed file against the [coverage record](#coverage-record). This step is done when you can name every covered function the change touches, or have confirmed that it touches none.
2. **Make the model describe the new code.** Handle each touched function by its row in the coverage record:
   - The behaviour is unchanged (a rename, a refactor, logging): the model needs nothing.
   - The change removes or weakens a guard that has a [switch](#regression-switches): set that switch to its broken value in all four base configs `model/{notes,ontology}-{churn,lifecycle}.cfg`. This mirrors the regression exactly.
   - Any other behaviour change: edit the action. Keep one TLA+ step equal to one lane job or one SQL transaction in the code, and keep the `file function` reference in the comment above the action.

   This step is done when every touched function's action does what the new code does.
3. **Run the check.** Run `.agents/skills/derived-work-verification/check` from the repository root. It takes about 35 seconds. The first run downloads TLA+ tools 1.7.4 and verifies its SHA-256, and may install Java through mise. It exits 0 when every run gives its expected result. Each code configuration must hold its rules, and each variant must break the rule it guards. This step is done when it exits 0, or when step 4 explains every `UNEXPECTED` line.
4. **Read every unexpected result with `/statechart`.** Give it `model/DerivedWork.tla`, the `run.cfg` printed under the `UNEXPECTED` line, and `--view model/view.mjs`. It tells the shortest violating path as numbered steps. Then decide what the path is:
   - **A real bug** reproduces in the code. Replay the path one action at a time, using the coverage record to find each action's code. A Go test in `pkg/app/bootstrap` can do this. `groupedWatcher` in `derived_group_test.go` builds a ticket group, `groupedProvider.before` acts while the provider computes, and a `lane.Lane` wrapper acts between lane jobs (see `publicationLane` in `derived_publication_test.go`). Report the bug with the path told in the code's words. Fix the code only when you are asked to. A product fix is never made in the model.
   - **A modelling error** is a step the code cannot take. Examples are two steps the lane would serialize, or an input the code does not read. Correct the action and cite the code that rules the step out. Never weaken a rule or a bound to make a run pass.
5. **After any edit to the model or `check`, run `check --deep`.** It adds every budget at once and liveness across restarts, and takes about 2 minutes. If a check's expected result changes because the behaviour changed on purpose, edit its entry in the `CHECKS` table in `check` and write why in its description. Every regression switch must keep breaking its rule.
6. **Move the sync commit forward only after confirming.** Confirming means that steps 1 to 5 are done for every file `--coverage` lists, and the covered changes are committed. Then run `.agents/skills/derived-work-verification/check --resync`. It runs the default checks and, if they pass, records `HEAD` in `model/sync.txt` and on the `Sync commit` line above. A passing check alone does not confirm the model. Only reading the diff against the coverage record does.

## Coverage record

The model covers these functions as of the sync commit. Files marked "assumed" in `model/sync.txt` hold behaviour the model relies on without modelling it.

| Code | Model |
| --- | --- |
| `pkg/store/derived_work.go` `MarkDerivedDirty` | `Mark`, `MarkedWork`: new generation, `ready=0`, revision bump, epoch bump on a global mark |
| `derived_work.go` `ActivateDerivedWork` | `Activate`: a global ticket retires path tickets if no later mark; exact generations become ready |
| `derived_work.go` `PendingDerivedWorkForKind` | `Pending`, `SchedulerReads`: nothing while any ticket of the kind is unready |
| `derived_work.go` `CurrentDerivedWork` | `Current` (switch `CheckTicket`) |
| `derived_work.go` `AckDerivedWorkMatched` | `Deleted`, `Acknowledge` (switch `AckFilter`) |
| `derived_work.go` `RetryDerivedWorkBatch` | No state: retry delay and attempt count only |
| `derived_work.go` `HasUnreadyDerivedWork`, `HasDerivedWork` | `Unready` in `BatchMarks`; `recovery` in `ExplicitIndex` |
| `pkg/store/derived_witness.go` `DerivedSourceFingerprints` | `sch.wit`: each witnessed path's committed rows; a global ticket witnesses every path |
| `pkg/app/bootstrap/derived_scheduler.go` `run` | `SchedulerReads` (global tickets single, path tickets one group), `EndRun`, `PassFails` |
| `derived_scheduler.go` `computeContext`, `inLane` | `ExplicitIndex(_, cancel)`, `PassFails`; every lane job is one step |
| `pkg/app/bootstrap/derived_members.go` `currentMembers`, `independentMembers` | `Prepare` (ticket check only), `Live` (ticket, witness, disk ahead) |
| `derived_members.go` `captureWitness` | `Prepare`: `WitnessKeys` (switch `RecordWitness`) |
| `derived_members.go` `withLease` | One step per lane job, with acknowledgement in the final one |
| `pkg/app/bootstrap/derived_execute.go` `executeObserved` | `Prepare` inputs (`Rebuilds`, `Reads`, switch `RecordExpand`), `ComputeReturns`, `NotesFinal`, `OntologyFinal` replan (switch `PublishSurvivors`) |
| `pkg/app/bootstrap/derived_publication.go` `computeAndPublish` | `PublishBatch` (switch `PublishOnlyLive`), `PassFails` |
| `pkg/app/bootstrap/derived_sources.go` `publishedNoteReader`, `publishedCodeLoader` | `Ahead` |
| `pkg/app/indexcore/publish.go` `Publish` | `BatchMarks` (mark before structure), `BatchCompletes` (commit, then `SubmitActivateDerived`), `BatchInterrupted` |
| `pkg/app/indexcore/candidates.go` `dirtyScopes` | `BatchMarks` scopes: path tickets, or only the global ticket when full |
| `pkg/app/indexing/unified.go` `RunUnifiedCore` (`completedDerived`, `SubmitDerivedAcknowledgement`) | `ExplicitIndex` |
| Assumed: `live_ownership.go` `hasPendingWork`, `processOwnershipBatch`; `indexcore/discover.go` recovery; `lane/executor.go`; `ontology/sync_record_plan.go` `RecordSourceSetPaths`; `embedindex/derived_pipeline.go` `FinishDerived` | Batch admission and restored inputs; unready means full discovery; one lane job at a time with explicit preemption; `Src`; one result per path and cleanup only |
| Environment | `Edit`, `DirectSave` (rows change, no ticket, file event later), `Restart`, `EventLost` (0 except in assumption checks) |

`Style = "notes"` covers note and code tickets: one result per path, published in batches, and then a final acknowledgement job. `Style = "ontology"` covers RECORD source sets, with one indivisible result published together with the acknowledgement.

## Regression switches

Each switch is `TRUE` in the code configurations. Every variant check sets one switch and must break its rule. `check --list` shows the full table.

| Switch | Code it stands for | Variant check, rule it breaks |
| --- | --- | --- |
| `CheckTicket` | `currentMembers` calls `CurrentDerivedWork` | `no-ticket-check`, `NoStalePublication` |
| `RecordWitness` | `captureWitness` keeps the whole RECORD source set | `own-path-witness`, `NoStalePublication` |
| `RecordExpand` | ontology inputs come from `RecordSourceSetPaths` | `no-record-expansion`, `QuietMeansCurrent` |
| `AckFilter` | `AckDerivedWorkMatched` matches generation, epoch and revision | `ack-without-filter` holds on its own, because `currentMembers` checks the ticket in the same lane job. `no-ticket-check-or-ack-filter` breaks `AckNeverCoversNewerChange` and `QuietMeansCurrent`. |
| `PublishOnlyLive` | publication skips dropped members' results | `publish-dropped-{notes,ontology}`, `GroupPublishesOnlyCurrentMembers` |
| `PublishSurvivors` | a group continues with its survivors | `drop-whole-group-{notes,ontology}`: `CoverSurvivorsContinue` holds, so the code configurations' expected break disappears |
| `AckOnlyPublished` | the proposed fix for F1 (`FALSE` in the code) | `fix-ack-only-published` holds every rule |

## Finding F1: a member skipped by one batch can be acknowledged anyway

`NoAckWithoutPublication` breaks on the note and code style. The code configurations expect this break, and the runner marks it `finding F1`. `/statechart` reports the path as follows:

1. The scheduler reads ready note tickets for `a` and `c` and plans them as one group.
2. `a`'s file changes on disk while the provider computes.
3. The first `derived_publication` lane job finds `a` ahead of its rows. It publishes `c` and keeps `a`'s result unpublished.
4. `a`'s file changes back to the planned text before the next lane job. Editors that save by rename or by an undo can do this.
5. The final `withLease(ack=true)` checks only the members that survived *preparation*. `a` looks current again, so `AckDerivedWorkMatched` deletes `a`'s ticket. `a`'s output stays stale and no ticket remains.

The cause is that `executeObserved` passes its prepare-time `members` to the final acknowledgement, and `computeAndPublish` returns nil when it kept results for dropped members. A scratch Go test reproduced F1 on the sync commit. The test used `groupedWatcher` with two notes, wrote `a` ahead during the provider call, and restored it before the final lane job. `execute` returned nil, `a`'s ticket was gone, and `a` had no embedded chunks for its current sections. `c` had been published. The repair usually comes from the file events of steps 2 and 4, which mark `a` again. When those events are lost, the output stays stale: `finding-lost-event` shows this, and `fix-lost-event` shows that the fix closes it. When F1 is fixed in code, set `AckOnlyPublished = TRUE` in the base configs. In `CHECKS`, move `NoAckWithoutPublication` from the breaks to the holds of `notes-churn` and `notes-deep`, and drop the two `fix-*` checks.

## Bounds and what the check does not cover

- **Bounds.** Note configurations use paths `a` and `c`. Ontology configurations use `a`, `b` and `c`, with `{a, b}` as one RECORD source set. File content is 0 or 1, so a revert is possible. The churn configurations allow 2 edits, 1 direct save and 1 failed pass. The lifecycle configurations allow 1 edit, 1 save, 1 explicit index, 1 restart, 1 interrupted batch and 1 failed pass. `--deep` combines every budget with 2 edits. Every ready path ticket joins one group. In code the limit is 64. Result batches may split in any way. In code they hold up to 128 results, 16 MiB, or 250 ms.
- **Assumptions.** File events arrive or the runtime restarts (`lost-event` shows what this buys). The witness fingerprint is a deterministic function of each path's committed rows, so a revert restores it. The lane runs one job at a time.
- **Not modelled.** The graph kind. The code kind's table-wide witness inputs (doc links, anchor scopes, edges). Note creation, deletion and rename, including a new RECORD host joining a source set. Path batches that escalate to complete metadata, and the global code ticket that note changes mark. Schema changes and syncer availability. Retry timing, attempt counts and input-rejection singles. Vector caches. A crash between the ontology publication and its acknowledgement inside one lane job. Standalone `rzm index` without a runtime, which takes the same index lock. A pass covers the model within these bounds, not the whole system.
