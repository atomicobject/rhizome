------------------------------ MODULE LiveIndexer ------------------------------
(* One file's journey through Rhizome's live indexer.

   File on disk -> watcher input -> structural batch on the indexing lane ->
   durable derived ticket -> provider computation outside the lease ->
   witnessed publication and acknowledgement.

   Sources: pkg/app/bootstrap/{live_ownership,watcher_pending,derived_scheduler,
   derived_sources}.go, pkg/app/bootstrap/lane, pkg/app/indexcore/publish.go,
   pkg/store/derived_work.go. *)
EXTENDS Naturals

CONSTANTS
  MaxWrites,   \* how many times the file may be written
  Structural,  \* TRUE: model the watcher batch step by step
  Derived,     \* TRUE: model the embedding scheduler
  Restarts,    \* TRUE: the runtime may stop and start
  Explicit     \* TRUE: someone may run `rzm index`

VARIABLES
  up,      \* the vault runtime is running
  disk,    \* version of the file on disk
  evt,     \* a filesystem event for the last write has not reached the watcher
  input,   \* watcher input held in memory: "none" | "path" | "full"
  why,     \* why a whole-vault discovery is owed
  idx,     \* version held by the structural index (durable)
  emb,     \* version the published embeddings were built from (durable)
  ticket,  \* durable derived-work row: "none" | "unready" | "ready"
  lane,    \* who holds the index lock: "idle" | "batch"
  bstep,   \* watcher batch progress: "taken" | "marked" | "committed"
  bscope,  \* what the batch discovers: "path" | "full"
  comp,    \* provider computation: "idle" | "running" | "done"
  cver,    \* text version being embedded
  cwit,    \* index version witnessed when the computation was prepared
  cstale,  \* the ticket was marked again after the computation was prepared
  backoff, \* why the ticket's retry is delayed: "none" | "provider" | "priority"
  hold,    \* an interactive writer has asked for priority on the index lock
  spin     \* the scheduler retried a ticket whose source it cannot read yet

vars == <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep, bscope,
          comp, cver, cwit, cstale, backoff, hold, spin>>

TypeOK ==
  /\ up \in BOOLEAN /\ evt \in BOOLEAN /\ hold \in BOOLEAN
  /\ cstale \in BOOLEAN /\ spin \in BOOLEAN
  /\ disk \in 0..MaxWrites /\ idx \in 0..MaxWrites /\ emb \in 0..MaxWrites
  /\ cver \in 0..MaxWrites /\ cwit \in 0..MaxWrites
  /\ input \in {"none", "path", "full"}
  /\ why \in {"none", "yield", "error", "restart", "overflow"}
  /\ ticket \in {"none", "unready", "ready"}
  /\ lane \in {"idle", "batch"}
  /\ bstep \in {"taken", "marked", "committed"}
  /\ bscope \in {"path", "full"}
  /\ comp \in {"idle", "running", "done"}
  /\ backoff \in {"none", "provider", "priority"}

Init ==
  /\ up = TRUE /\ disk = 0 /\ evt = FALSE /\ input = "none" /\ why = "none"
  /\ idx = 0 /\ emb = 0 /\ ticket = "none" /\ lane = "idle"
  /\ bstep = "taken" /\ bscope = "path" /\ comp = "idle" /\ cver = 0 /\ cwit = 0
  /\ cstale = FALSE /\ backoff = "none" /\ hold = FALSE /\ spin = FALSE

OweFull(cause) == /\ input' = "full"
                  /\ why' = IF why = "none" THEN cause ELSE why

------------------------------------------------------------------------------
(* The world outside the indexer *)

\* An editor, git, or an agent writes the file directly.
EditOutside ==
  /\ disk < MaxWrites
  /\ disk' = disk + 1 /\ evt' = up
  /\ UNCHANGED <<up, input, why, idx, emb, ticket, lane, bstep, bscope, comp,
                 cver, cwit, cstale, backoff, hold, spin>>

\* watcher_pending.go retainHubEvents: the path joins the pending set and wakes
\* the watcher.
EventArrives ==
  /\ up /\ evt
  /\ evt' = FALSE /\ input' = IF input = "full" THEN "full" ELSE "path"
  /\ UNCHANGED <<up, disk, why, idx, emb, ticket, lane, bstep, bscope, comp,
                 cver, cwit, cstale, backoff, hold, spin>>

\* Backend overflow, directory create or rename, file rename: a resync notice.
EventOverflows ==
  /\ up /\ evt
  /\ evt' = FALSE /\ OweFull("overflow")
  /\ UNCHANGED <<up, disk, idx, emb, ticket, lane, bstep, bscope, comp, cver,
                 cwit, cstale, backoff, hold, spin>>

\* A save, repair, or CLI mutation requests priority (indexlock.RequestPriority).
WriterArrives ==
  /\ ~hold /\ hold' = TRUE
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, comp, cver, cwit, cstale, backoff, spin>>

\* The writer saves this file under its own lease: source and structural rows
\* commit together; no derived ticket is written.
WriterSaves ==
  /\ hold /\ lane = "idle" /\ disk < MaxWrites
  /\ disk' = disk + 1 /\ idx' = disk + 1 /\ evt' = up
  /\ hold' = FALSE /\ spin' = FALSE
  /\ UNCHANGED <<up, input, why, emb, ticket, lane, bstep, bscope, comp, cver,
                 cwit, cstale, backoff>>

\* The writer changed something else and releases the lock.
WriterLeaves ==
  /\ hold /\ lane = "idle" /\ hold' = FALSE
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, comp, cver, cwit, cstale, backoff, spin>>

\* `rzm index` inside the runtime (lane kind explicit-index). It holds the lock
\* for its whole run, embeds inline, and acknowledges its own tickets.
ExplicitIndex ==
  /\ Explicit /\ up /\ lane = "idle" /\ ~hold
  /\ idx' = disk /\ emb' = disk /\ ticket' = "none"
  /\ cstale' = TRUE /\ spin' = FALSE /\ backoff' = "none"
  /\ comp' = IF comp = "running" THEN "idle" ELSE comp
  /\ UNCHANGED <<up, disk, evt, input, why, lane, bstep, bscope, cver, cwit, hold>>

\* The same run with the embedding provider unavailable: the ticket stays.
ExplicitIndexWithoutProvider ==
  /\ Explicit /\ up /\ lane = "idle" /\ ~hold
  /\ idx' = disk /\ ticket' = "ready"
  /\ cstale' = TRUE /\ spin' = FALSE /\ backoff' = "none"
  /\ comp' = IF comp = "running" THEN "idle" ELSE comp
  /\ UNCHANGED <<up, disk, evt, input, why, emb, lane, bstep, bscope, cver, cwit, hold>>

\* Crash, kill, or idle exit: memory is lost, the database is kept.
RuntimeStops ==
  /\ Restarts /\ up
  /\ up' = FALSE /\ evt' = FALSE /\ input' = "none" /\ why' = "none"
  /\ lane' = "idle" /\ comp' = "idle" /\ spin' = FALSE
  /\ UNCHANGED <<disk, idx, emb, ticket, bstep, bscope, cver, cwit, cstale,
                 backoff, hold>>

\* live_indexing.go: the watcher starts with the cache marked stale.
RuntimeStarts ==
  /\ Restarts /\ ~up
  /\ up' = TRUE /\ OweFull("restart")
  /\ UNCHANGED <<disk, evt, idx, emb, ticket, lane, bstep, bscope, comp, cver,
                 cwit, cstale, backoff, hold, spin>>

------------------------------------------------------------------------------
(* The watcher batch, a background job on the indexing lane *)

\* live_ownership.go run / hasPendingWork / lane executor next():
\* background jobs do not start while priority is requested.
BatchStarts ==
  /\ Structural /\ up /\ lane = "idle" /\ ~hold
  /\ input # "none" \/ ticket = "unready"
  /\ lane' = "batch" /\ bstep' = "taken" /\ input' = "none"
  /\ bscope' = IF input = "full" \/ ticket = "unready" THEN "full" ELSE "path"
  /\ UNCHANGED <<up, disk, evt, why, idx, emb, ticket, comp, cver, cwit, cstale,
                 backoff, hold, spin>>

\* publish.go mark(): durable obligations commit before structure changes.
BatchMarks ==
  /\ Structural /\ lane = "batch" /\ bstep = "taken"
  /\ bstep' = "marked" /\ ticket' = "unready" /\ cstale' = TRUE
  /\ backoff' = "none"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, lane, bscope, comp, cver,
                 cwit, hold, spin>>

\* publish.go ingest, metadata and ontology publication.
BatchCommits ==
  /\ Structural /\ lane = "batch" /\ bstep = "marked"
  /\ bstep' = "committed" /\ idx' = disk /\ spin' = FALSE
  /\ UNCHANGED <<up, disk, evt, input, why, emb, ticket, lane, bscope, comp,
                 cver, cwit, cstale, backoff, hold>>

\* publish.go SubmitActivateDerived, then the lane releases the lock.
BatchCompletes ==
  /\ Structural /\ lane = "batch" /\ bstep = "committed"
  /\ lane' = "idle" /\ ticket' = "ready"
  /\ why' = IF bscope = "full" /\ input # "full" THEN "none" ELSE why
  /\ UNCHANGED <<up, disk, evt, input, idx, emb, bstep, bscope, comp, cver,
                 cwit, cstale, backoff, hold, spin>>

\* lane executor yielding heartbeat cancels the job; live_ownership.go restores
\* the inputs and marks the cache stale, whose recrawl forces a full discovery.
BatchYields ==
  /\ Structural /\ lane = "batch" /\ hold
  /\ lane' = "idle"
  /\ OweFull(IF bscope = "path" THEN "yield" ELSE "error")
  /\ UNCHANGED <<up, disk, evt, idx, emb, ticket, bstep, bscope, comp, cver,
                 cwit, cstale, backoff, hold, spin>>

\* Any error inside the batch takes the same restore path.
BatchFails ==
  /\ Structural /\ lane = "batch" /\ ~hold
  /\ lane' = "idle" /\ OweFull("error")
  /\ UNCHANGED <<up, disk, evt, idx, emb, ticket, bstep, bscope, comp, cver,
                 cwit, cstale, backoff, hold, spin>>

\* Structural = FALSE collapses the batch into one step for the embedding chart.
BatchRunsWhole ==
  /\ ~Structural /\ up /\ lane = "idle" /\ ~hold /\ input # "none"
  /\ input' = "none" /\ why' = "none" /\ idx' = disk /\ ticket' = "ready"
  /\ cstale' = TRUE /\ backoff' = "none" /\ spin' = FALSE
  /\ UNCHANGED <<up, disk, evt, emb, lane, bstep, bscope, comp, cver, cwit, hold>>

------------------------------------------------------------------------------
(* The derived scheduler: one ticket at a time, computing outside the lease *)

SchedulerMayRun == Derived /\ up /\ ticket = "ready" /\ backoff = "none" /\ comp = "idle"
                   /\ lane = "idle" /\ ~hold

\* derived_scheduler.go executeObserved: witness, then plan from the indexed source.
DerivedStarts ==
  /\ SchedulerMayRun /\ disk = idx
  /\ comp' = "running" /\ cver' = idx /\ cwit' = idx /\ cstale' = FALSE
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, backoff, hold, spin>>

\* derived_sources.go publishedNoteReader / publishedCodeLoader: the file on
\* disk no longer hashes to its indexed row, so the ticket is "superseded" with
\* no retry delay and the next 250 ms pass tries again.
DerivedSpins ==
  /\ SchedulerMayRun /\ disk # idx /\ ~spin
  /\ spin' = TRUE
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, comp, cver, cwit, cstale, backoff, hold>>

ProviderReturns ==
  /\ Derived /\ comp = "running" /\ comp' = "done"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, cver, cwit, cstale, backoff, hold, spin>>

ProviderFails ==
  /\ Derived /\ comp = "running" /\ comp' = "idle" /\ backoff' = "provider"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, cver, cwit, cstale, hold, spin>>

\* derived_scheduler.go computeContext: an external priority request cancels
\* computation although computation holds no lock; the retry is delayed.
ComputeCancelledByWriter ==
  /\ Derived /\ comp = "running" /\ hold
  /\ comp' = "idle" /\ backoff' = "priority"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, cver, cwit, cstale, hold, spin>>

BackoffExpires ==
  /\ Derived /\ backoff # "none" /\ backoff' = "none"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, comp, cver, cwit, cstale, hold, spin>>

StillCurrent == ticket = "ready" /\ ~cstale /\ cwit = idx

\* withLease: CurrentDerivedWork and the source fingerprint are checked inside
\* the publication lease, then results are written and the ticket acknowledged.
DerivedPublishes ==
  /\ Derived /\ comp = "done" /\ lane = "idle" /\ ~hold /\ StillCurrent
  /\ emb' = cver /\ ticket' = "none" /\ comp' = "idle"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, lane, bstep, bscope, cver,
                 cwit, cstale, backoff, hold, spin>>

DerivedDiscards ==
  /\ Derived /\ comp = "done" /\ lane = "idle" /\ ~hold /\ ~StillCurrent
  /\ comp' = "idle"
  /\ UNCHANGED <<up, disk, evt, input, why, idx, emb, ticket, lane, bstep,
                 bscope, cver, cwit, cstale, backoff, hold, spin>>

------------------------------------------------------------------------------
Next ==
  \/ EditOutside \/ EventArrives \/ EventOverflows
  \/ WriterArrives \/ WriterSaves \/ WriterLeaves
  \/ ExplicitIndex \/ ExplicitIndexWithoutProvider
  \/ RuntimeStops \/ RuntimeStarts
  \/ BatchStarts \/ BatchMarks \/ BatchCommits \/ BatchCompletes
  \/ BatchYields \/ BatchFails \/ BatchRunsWhole
  \/ DerivedStarts \/ DerivedSpins \/ ProviderReturns \/ ProviderFails
  \/ ComputeCancelledByWriter \/ BackoffExpires
  \/ DerivedPublishes \/ DerivedDiscards

Spec == Init /\ [][Next]_vars

------------------------------------------------------------------------------
(* Rules *)

Quiet == /\ up /\ ~evt /\ ~hold /\ input = "none" /\ lane = "idle"
         /\ comp = "idle" /\ ticket = "none"

\* When the live indexer has nothing left to do, the index matches the file.
QuietIndexMatchesFile == Quiet => idx = disk

\* When the live indexer has nothing left to do, the embeddings match the index.
QuietEmbeddingsMatchIndex == (Derived /\ Quiet) => emb = idx

\* Embeddings are never published for text the index does not hold.
EmbeddingsNeverAhead == emb <= idx

\* Stale embeddings always have a durable ticket behind them.
StaleEmbeddingsHaveATicket == (Derived /\ emb # idx) => ticket # "none"

\* Yielding to an interactive writer costs only the work that was interrupted.
YieldStaysSmall == ~(why = "yield" /\ input = "full")

\* The scheduler never retries work it cannot do yet without waiting.
NoBusyRetry == ~spin

\* Only the embedding provider slows embedding down.
OnlyProviderDelays == backoff # "priority"
==============================================================================
