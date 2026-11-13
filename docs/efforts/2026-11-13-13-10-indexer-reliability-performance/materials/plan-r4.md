# Plan, revision 4: make live and batch indexing consistent, robust, and provider-bound

Finding IDs refer to `findings.md` in this folder. Scores so far: round 1 Opus 6.5, Fable 6, Sol 7; round 2 Opus 8, Fable 8, Sol 9; round 3 Opus 8.5, Fable 9, Sol 9. "Changes from revision 3" at the end maps each round 3 required change to where it landed.

## Triage

| ID | Verdict | Council | Note |
|---|---|---|---|
| C1 | Keep | 3/3 | |
| C2 | Keep | 3/3 | Affects search refs, graph scores and the code witness. It does not affect embedding text: two reviewers read that `DocLinksForAnchors` uses only `dst_kind='anchor'` rows (`doc_links.go:225`). |
| C3 | Keep | 3/3 | |
| C4 | Keep, with C7 | 3/3 | The scope hash covers code settings, so it is stamped only for what was fully applied. |
| C5 | Keep, recompute | 3/3 | |
| C6 | Keep as a regression test first | 3/3 | Full discovery already reparses old-version rows; only the early version stamp is in doubt. |
| C7 | Keep | 3/3 | Apply the parser configuration and reparse the code it affects. |
| C8 | Defer | 2 defer, 1 keep | Orphan chunks are filtered after the nearest-neighbour query, so they are hidden but take candidate slots. Trigger: a measured recall loss, a batch that already touches ontology node retirement, or the batch 1 equivalence test failing on orphan chunks in a delete or rename sequence. |
| C9 | Defer | 2 drop, 1 defer | Trigger: a fixture reproduces divergent TypeScript import resolution. |
| C10 | Drop | 3/3 | |
| C11 | Drop | 3/3 | |
| C12 | Defer | 3/3 | Trigger: a user report, or credential entry that does not restart the runtime. |
| R1 | Keep the pre-`mark()` part | 3/3 | After `mark()`: keep coarse recovery in this effort (consensus decision D1). |
| R2 | Keep, before S1 | 3/3 | |
| R3 | Keep, before S1 | 3/3 | |
| R4 | Keep as a guard | 3/3 | |
| R5 | Keep | 3/3 | |
| R6 | Keep, widened | 3/3 | |
| R7 | Defer | 3/3 | Trigger: 429s in diagnostics, or a 429 harness exists. |
| R8 | Defer | 2 drop, 1 defer | Trigger: a user on an endpoint without rate-limit headers reports slow indexing. |
| R9 | Drop | 3/3 | |
| S1 | Keep | 3/3 | |
| S2, S5 | Keep, one mechanism | 3/3 | |
| S3 | Not its own item | 3/3 | See below. |
| S4 | Keep ordering and deduplication; the rest is conditional | 3/3 | See batch 6. |
| S6 | Drop | 3/3 | The broad code witness is deliberate. Batch 6, if it runs, defines a narrower input set and says so. |
| S7 | Drop | 3/3 | |
| S8 | Defer | 3/3 | Trigger: a measured slow restart on a large vault, and only once saves have durable tickets (R4). |

### The 6 ms lane job (S3)

All three reviewers checked the arithmetic in both rounds and agree it does not earn its own item.

- A 50-note burst today: 312 jobs, 1.6–2.2 s of the 6.6 s zero-latency drain. Grouped: about 12–15 jobs, 60–90 ms, next to a provider round trip of 100 ms or more.
- A single edit keeps one ticket per kind: about 12–18 jobs, 70–110 ms of background work behind a provider call. Jobs are short, so writers interleave.
- One change is free and is in batch 3: take the witness and build the plan under one lease. It removes a job per ticket and makes the plan match the witness by construction.
- Revisit only if a grouped-run measurement shows lane jobs as the remaining bottleneck, or a job costs more than 20 ms on Windows.

## Batches

Each batch is one pull request. Order: 1 and 2 are independent of each other; 3 needs 2; 4 is independent; 5 needs 1; 6 is conditional and needs 2 and 5. Every batch passes the repository's required gate (`docs/engineering/quality-gates.md`).

Test evidence is counts and persisted rows, not wall-clock time. The throughput figures in `findings.md` are targets to re-measure and record, not CI assertions.

### Batch 1: consistency fixes

- **C1.** The explicit index job, and every other runtime caller that captured the startup definition, uses the watcher's current definition. Known callers: `cmd/serve_index_job_hooks.go:59` and the validation coordinator at `pkg/app/cli/serve/readiness.go:398`; audit the remaining `rt.VaultDef` and `live.VaultDef` readers. The current-definition getter is private to `bootstrap`, so export one. The explicit job also hashes disk config for its scope stamp (`unified.go:159`); definition, configuration and hash must come from one snapshot, the configuration the runtime has applied, passed as `opts.Config`.
  - *No config rewrite from inside the runtime (decision D4).* Today the explicit job writes its resolved code and embedding settings back to `config.yml` (`unified_post.go:73-83`). Running on the applied snapshot would then overwrite a newer edit the runtime has refused until restart, such as a provider change (`live_config_reload.go:17`). The runtime's explicit job sets `SkipConfigPersistence`, as boot catch-up does (`cmd/runtime_helpers.go:53`). An in-process `rzm index` with no runtime keeps persisting.
- **C3.** The live ontology ticket treats "no ontology files" as "no schema" and acknowledges, as batch does.
- **C5.** Batch recomputes graph scores when code files were retired.
- **C6.** Add a regression test: start a runtime on an index written by an older indexer version, let a watcher full discovery win the lane before boot catch-up, and assert old-version rows are reparsed and no version proof is stamped early. Fix in the shared publisher only if it fails.
- **C7, in three parts.**
  1. *Apply the configuration.* A parser-affecting code-config change rebuilds the anchor service inside `publishOwnershipConfiguration`, under the batch's lease. The rebuild must reach both holders: the atomic `rt.codeAnchorSvc` and the watcher's own `w.noteSvc` (so the callback returns the new service). Other readers load the service per call. It respects `rt.requirements` (indexers only with the warm-up capability). The rebuild is triggered by comparing the resolved code configuration directly, not a hash.
  2. *Watch the new roots.* Code watch roots are registered once at leader start (`live_leader.go:46-70`). The same publication registers added roots and drops removed ones.
  3. *Reparse what the change affects.* Rebuilding the service is not enough: a file whose content and indexer version are unchanged is skipped (`pkg/anchors/batch.go:120-122`), yet changed roots can change its module names (`indexer_python.go:162`). Persist a hash of the complete resolved parser configuration: per-language resolved roots (so an added empty language block, which resolves to root `"."`, changes it), code enablement, disabled languages, and parse limits such as the TypeScript timeout. The existing scope hash is not enough; it omits those (`pkg/vault/repoconfig/defaults.go:162`). When the stored parser hash differs from the applied one, the shared publisher marks every code candidate `ForceRead` in that full discovery (`codeintel/ingest.go:522`), which reparses code and leaves notes alone, and stamps the new hash only after the reparse succeeds, so a cancel or crash repeats it. This is in the shared publisher, so batch behaves the same. `indexcore.Request` gains the applied hash. An index with no stored parser hash is stamped without a reparse; batch 5's indexer-version bump reparses all code once anyway.
  - *Fallback.* If part 1 or 2 cannot be done safely for some holder, a parser-affecting change shuts the runtime down cleanly through the path a replaced database uses (`pkg/app/cli/serve/database_guard.go:43`), and the next client starts a fresh one. Part 3 is required either way.
- **C4.** After a full structural convergence that included part 3's reparse, live stamps the scope hash of the exact configuration snapshot it applied, through the writer lane.
- **Equivalence test (new, owned by this batch).** Extend the existing batch snapshot (`pkg/app/indexing/unified_snapshot_test.go`) into a comparison of a quiet live runtime's database with a fresh batch index of the same files, over deterministic columns only (no timestamps): structural rows, chunks and embeddings, graph scores, and the scope stamp. Sequences: edit, delete, rename, selection change, and code-config change followed by an edit under the new root, with an unchanged file under changed roots, and with an added empty language block. Until batch 5 lands, the fixture has no code-to-note mentions, because graph scores read links live does not write yet. A counting provider asserts that a forced code reparse sends zero texts. Later batches add rows to it.
- **Exit:** the selection replay passes in both directions; the equivalence test passes for the sequences above.

### Batch 2: scheduler behaviour under change and interruption

- **R2.** A superseded ticket gets a short, capped retry delay instead of running again on the next 250 ms pass. The delay does not count as a failed attempt (it must not grow the provider backoff or take the ticket out of grouping). It is written through `inLane`, because `withLease`'s witness check would reject it; the write is a no-op for a ticket that is no longer current, and `MarkDerivedDirty` clears it when the path is published again. The ordinary failure retry (`derived_scheduler.go:87`) moves to `inLane` for the same reason.
- **R3 and consensus decision D2.** An external priority request no longer cancels provider computation, which holds no lock; publication already waits for the lane. A preemption by an explicit index still cancels. No preemption or yield counts as a failed attempt, whether it interrupts computation or a prepare or publication lane job. Update `docs/reference/subsystems/vault-runtime.md` (the "Derived computation releases the writer lease" constraint) and `TestDerivedPriorityCancelsProviderWithoutHoldingWriterLease`.
- **R6.** The code pipeline writes the vectors of results it could not publish to its cache and keeps owners a failed run left incomplete, as notes and ontology do; the code planner already reads that cache (`syncer_reuse.go:203-222`). For every kind, a provider call already in flight when a pass is cancelled finishes and its vectors are cached.
- **S4, ordering and deduplication.** Notes and ontology run before code and graph, applied before the load limit so a page of code tickets cannot starve notes. The global code ticket's path list is deduplicated.
- **Exit:** the spin replay shows at most one lane job per delay interval and no attempt increment; the priority replay shows no cancelled provider call and no attempt increment; a cancelled or superseded code pass re-sends zero texts on retry (counting provider); one note edit on 500 code files reaches its own embedding before the global code ticket runs.

### Batch 3: grouped tickets (provider-bound live embedding)

- **S1.** The scheduler groups ready path-scoped tickets of one kind into one run: one prepared plan, one computation, one publication stream. Global tickets stay single.
- **Invariant.** Every destination write, cleanup, freshness stamp and acknowledgement for a path happens inside a lease in which that path's own ticket is still current (generation and kind epoch) and the source fingerprint of every input its plan consumed is unchanged. Each member has its own fingerprint, captured in the same lease that builds the plan; the scheduler's single witness string becomes one per member. For ontology the consumed inputs include the expanded RECORD source set (`derived_sources.go:18-29`, `pkg/ontology/sync_record_plan.go:31`), not only the ticket's path. Set membership itself is not fingerprinted; that is safe because a ticket on a path that joins a set plans the whole set again.
- **Member handling.** `withLease` takes the member tickets and returns the survivors; only survivors are published and acknowledged. A stale member is dropped, keeps its ticket, gets the R2 delay, and has any returned vectors cached; it never costs another member a re-send. The note and code source loaders report a changed file per path instead of failing the whole list. `FinishDerived` cleanup is restricted to survivors. The table-wide part of a code fingerprint is computed once per lease, not once per member.
- **Dependent members.** Members whose consumed inputs overlap wait together: a path that is ahead on disk drops every member whose consumed set contains it. RECORD hosts in one source set therefore succeed or wait as a unit. Because an ontology result is one object, a stale member means cache the vectors, drop the dependent members, and plan the independent rest again; the cache makes the second plan free at the provider.
- **Bounds.** Tickets group whatever their attempt count, so a backlog that failed while offline or rate-limited still drains in full requests when the provider returns. A failure that is not input-specific (transport, timeout, 5xx, 429) delays the whole group and keeps it groupable. After a failure that could be specific to one member's input, the scheduler runs that group's members singly on their next attempt; it tracks this in memory, so no schema change is needed and a restart simply tries the group once more. Load every ready path ticket of a kind up to a stated group size, not the first 32 rows across kinds. Keep a grouped prepare short enough to finish inside the lane's one-second priority check.
- **Single lease for witness and plan** (from S3).
- **S2 and S5, one mechanism.** A derived run knows all its texts before it dispatches, so it packs to the provider's request limits and flushes when the plan is submitted, for path groups and global tickets alike. Verify the setting reaches the physical shared node.
- **Events.** A grouped acknowledgement emits one index-changed event carrying its paths and counts only rows that matched.
- **Exit, by counts, with the harness pinning all 50 edits into one watcher batch:** 50 changed typed notes drain with at most 2 provider calls for the ontology kind and at most 20 lane jobs; the same for 50 untyped notes and the notes kind; an edit to one member during computation publishes every member whose inputs are independent of it and re-sends nothing; an edit to one RECORD host holds back its source set and nothing else; a provider failure on part of a group publishes nothing stale and caches what returned; after a simulated outage that failed every ticket, the backlog drains in grouped requests, not one call per path; a direct save to a member is rejected at publication for that member only; a restart mid-group resumes from the tickets. Re-measure the 100 ms-latency drain and record it in the effort.

### Batch 4: watcher under interruption

Two facts bound this batch. An external priority request reaches a running background job at the lane's first priority check, one second after the job starts (`pkg/vault/indexlock/lease.go:115`, `heartbeat.go:63-74`), so a shorter batch does not yield to one. An explicit `rzm index` cancels a running batch immediately (`lane/executor.go:151-161`), so any batch can be interrupted that way.

- **R1a.** A cancelled batch that restored its inputs no longer marks the cache stale; the store decides the next scope, because each batch already reads unready tickets and pending reconciliation (`live_ownership.go:167-187`). In effect this removes the stale mark from the cancel path. Three cases keep it: any error; a batch that completed and was cancelled during its trailing cache refresh; and a cancel during the leading cache refresh, before the batch has taken its inputs, unless the implementation confirms the cache requeues its own drained set there.
- **Consensus decision D1: after `mark()`, recovery stays coarse in this effort.** All three reviewers chose this in round 2. One reason beyond risk: a note change marks an unready global code ticket inside the same publish, so recovery scoped to the paths that unready tickets name would usually fall back to whole-vault anyway.
- **Measurement, at effort closure, not a batch exit.** It only means something on a build that contains R1a, and Rhizome Dev installs from `main`, so it runs after this batch merges and its verdict is recorded when the effort closes (or as a follow-up action item if a week has not passed). From runtime diagnostics (lane yield and preemption events, full-discovery counts by reason including `unready_derived_work`, restored-batch events), on your real vaults over a representative week: count interruptions by external priority and by explicit index, the cost of the recovery pass that follows, and above all whether a whole-vault recovery pass is interrupted repeatedly during agent write bursts without finishing. Threshold, fixed now: open a follow-up if any recovery pass was interrupted three or more times in a row, or if recovery passes that follow an interruption cost more than five minutes of indexing in the week. This batch verifies that those events persist across runtime sessions; if the retained data is incomplete, the verdict is "insufficient evidence", not "below threshold".
- **R5.** Repeated structural failures back off with a cap; new watcher input resets the delay.
- **R4.** Record in `docs/reference/subsystems/indexing.md` that a save relies on its file event or the restart pass to create derived tickets, and add a test that a save followed by a restart re-embeds.
- **Exit:** a batch cancelled before `mark()` is followed by a batch of the same scope; a batch that fails three times in a row is retried at increasing intervals; the save-then-restart test passes; the diagnostics events the closure measurement needs are shown to persist across a runtime restart.

### Batch 5: code-to-note links in live (C2)

- Build the linker for a watcher batch from the complete Markdown note set: persisted note paths plus the snapshot's changes, the way `CompleteNotePaths` does (`indexcore/core.go:93`), filtered to Markdown by format ID (persisted paths carry no provider). A scoped batch's own discovery covers only the affected paths (`discover.go:143-152`), so it cannot be the source. Build it only when the batch contains code files, and pass it to that batch's parse work without mutating the long-lived service, whose linker field parse workers read. No long-lived linker.
- Repair existing vaults once through the indexer-version mechanism, which forces a reparse of every code file at the next boot catch-up or `rzm index` (an existing test, `pkg/app/codeintel/doclinker_upgrade_test.go:16`, covers relinking unchanged source). The bump also resets the reverse index once. Confirm with a counting provider that it sends no text.
- Known limit, unchanged and shared by both paths: unchanged code is not relinked when a note is added or renamed. A fresh batch index does relink it, so batch itself differs between incremental and fresh on those sequences.
- Equivalence test: add `doc_links`. For code-edit sequences compare live with a fresh batch index. For note add and rename sequences compare live with an incremental batch run on the same starting database, and exclude `doc_links` and graph scores from the fresh-batch comparison there. Keep alias mentions out of the fresh-batch comparison too: the linker loads aliases when it is built, before publication, so a fresh index has none to match.
- **Exit:** a code file edited to mention a note produces the same `doc_links` rows live as a fresh batch index, including when the watcher batch contains only that code file.

### Batch 6, conditional: skip the global code ticket when it has nothing to do (S4, remainder)

After batch 2 the note's own embedding no longer waits behind the code ticket, so what remains is background hashing with no provider calls. A missed mark would leave code embeddings stale permanently, so this runs only on evidence.

- **Condition, fixed now:** after batch 2, measure the global code ticket on your largest vault. Run this batch only if one note edit costs more than two seconds of background work, or delays another ticket's provider call.
- **Rule, if it runs:** define one input set, the note-written rows that code chunk text actually reads (trace `IntelDocSections`, `IntelEdges` and `doc_links`), and use it both to decide whether a note edit marks the global code ticket and as the note side of the code witness. This narrows the code witness that S6 left alone; the batch says so and proves the set complete. A witness can reject running work but cannot create a ticket, so the mark must stay whenever that set changes.
- **Exit:** an edit to a note with no code links creates no code ticket; an edit that adds, removes or retargets a code link does; the equivalence test still passes.

## Decisions

No decision blocks the plan. Four are recorded for your sign-off because each changes or confirms a documented behaviour:

- **D1 (3/3):** recovery after an interruption past `mark()` stays coarse in this effort; the batch 4 measurement decides whether a follow-up is opened.
- **D2 (3/3):** an external priority request no longer cancels provider computation (batch 2).
- **D3 (3/3, conditional):** a note edit may skip the global code ticket only under batch 6's rule and condition.
- **D4 (new in revision 4, raised by Opus):** an explicit index running inside the runtime uses the runtime's applied configuration and does not rewrite `config.yml`. This narrows the rule in `docs/reference/subsystems/vault-runtime.md` that an explicit index persists resolved settings to the in-process case; update that note in batch 1.

## Evidence kept with the effort

The TLA+ model, its two chart views and the four replay tests exist only in a scratch folder. Check them in as effort materials. The replay tests become real tests in the batches above. The model is supporting evidence, not an exit gate.

## Out of scope

Dropped: C10, C11, R9, S6, S7, and S3 as its own item. Deferred with the triggers above: C8, C9, C12, R7, R8, S8. Post-`mark()` scoped recovery (R1, option B) is deferred behind the batch 4 measurement.

## Changes from revision 3

Round 3 required changes and where each landed:

- Sol, the scope hash omits parser-affecting settings (an added empty `python: {}` block changes resolved roots but not the hash): batch 1, C7 part 3 now persists a hash of the complete resolved parser configuration, and the equivalence test gains the empty-block sequence.
- Opus 1, running the explicit job on the applied snapshot would overwrite a newer config edit: batch 1, C1 sets `SkipConfigPersistence` for the runtime's explicit job; recorded as decision D4.
- Opus 2, grouping only never-failed tickets stops grouping exactly when the backlog is largest: batch 3 groups whatever the attempt count and isolates only after a possibly input-specific failure, tracked in memory.
- Fable, batch 4's exit asked for a week of data before merge: the measurement and verdict move to effort closure on a build with R1a; the batch exit only proves the events persist.

Also adopted: the ontology query runtime is removed from C7's holders (it reads the service per query); the service rebuild compares resolved code configuration directly; the reparse uses per-candidate `ForceRead` so notes are not reparsed; a third stale-mark case for a cancel during the leading cache refresh; the batch 1 fixture avoids code-to-note mentions until batch 5; counting-provider assertion on the forced reparse; Markdown filtered by format ID; the batch linker is passed without mutating the shared service; alias mentions excluded from the fresh-batch link comparison; a third trigger for C8; "insufficient evidence" as a possible measurement verdict.
