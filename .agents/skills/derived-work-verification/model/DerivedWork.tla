---------------------------- MODULE DerivedWork ----------------------------
(* The live indexer's derived-work protocol for ONE derived kind.

   A structural publication marks durable tickets, commits structural rows,
   then activates the tickets. The derived scheduler groups ready tickets,
   plans and witnesses their inputs in one lane job, computes outside the
   lane, publishes in lane jobs that re-check every member, and acknowledges.
   Direct saves, explicit indexing, interruption, provider failure, and
   restart act on the same durable rows.

   Kinds never share ticket, epoch, or revision rows, and every lane job is
   serialized, so each kind is checked on its own:
     Style = "notes"    note and code tickets: one result per path, published
                        in batches (derived_publication.go computeAndPublish),
                        then a final acknowledgement job.
     Style = "ontology" ontology tickets: RECORD source sets, one indivisible
                        result published with the acknowledgement, and a
                        replan of the survivors when a member drops.

   One step of this spec is one lane job or one SQL transaction in the code.
   A lane job is atomic with respect to every other lane job, because
   pkg/app/bootstrap/lane runs one job at a time under the vault lease.
   The structural batch is the exception: its mark and its completion are
   separate steps so that interruption between them is visible.

   Each action names the code it stands for. Each regression switch below is
   TRUE for the code as it is, and a variant check in ../check sets one to
   FALSE. MaxLostEvents breaks an assumption and is 0 in every code check. *)
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS
  Paths,          \* note paths
  RecordHosts,    \* paths that share one RECORD source set (ontology only)
  Style,          \* "notes" or "ontology"
  MaxEdits,       \* external edits (editor, git, agent) that change a file
  MaxSaves,       \* direct saves: structural rows change without a ticket
  MaxExplicit,    \* explicit `rzm index` jobs on the runtime lane
  MaxRestarts,    \* runtime stops and starts
  MaxInterrupts,  \* structural batches cancelled after mark (yield, preemption)
  MaxFailures,    \* derived passes that end early (provider error, preemption)
  MaxLostEvents,  \* ASSUMPTION CHECK (0 = assumed away): file events that never
                  \* reach the watcher and raise no resync
  \* Regression switches (TRUE = current code)
  CheckTicket,      \* currentMembers calls store.CurrentDerivedWork
  RecordWitness,    \* captureWitness records every path in inputs[member]
  RecordExpand,     \* ontology inputs come from ontology.RecordSourceSetPaths
  AckFilter,        \* AckDerivedWorkMatched deletes only the exact generation/epoch/revision
  PublishOnlyLive,  \* publication skips results of members that dropped
  PublishSurvivors, \* a group continues with its survivors when a member drops
  AckOnlyPublished  \* the final job acknowledges only members whose results were all
                    \* published (computeAndPublish returns them)

ASSUME Style \in {"notes", "ontology"} /\ RecordHosts \subseteq Paths

Values == {0, 1}           \* file contents; a revert brings a value back
Global == ""               \* derived_work.path of a global ticket
Scopes == Paths \cup {Global}
NoTicket == [present |-> FALSE, gen |-> 0, ready |-> FALSE]

\* The paths whose structural rows an output for p is built from.
\* ontology.RecordSourceSetPaths: a RECORD host reads the whole source set.
Src(p) == IF p \in RecordHosts THEN RecordHosts ELSE {p}

VARIABLES
  disk,     \* file content on disk
  idx,      \* committed structural rows (notes, projection, ontology_nodes)
  out,      \* published derived output: for each path, the structural value
            \* of each path in Src(p) it was built from
  tix,      \* derived_work rows: [present, gen, ready]
  seq,      \* derived_work_sequence.generation per scope
  epoch,    \* derived_work_epoch.generation (bumped by a global mark)
  rev,      \* derived_work_epoch.revision (bumped by every mark)
  pend,     \* paths whose file events the watcher has not yet published
  resync,   \* the watcher owes a full discovery (startup, restored resync)
  batch,    \* the watcher's structural lane job between mark and completion
  sch,      \* the derived scheduler's in-memory run
  used,     \* environment budgets spent
  staleWrite, unpublishedAck, ackedNewer, crossWrite, \* ghost: rule breaks seen
  cover     \* ghost: scenarios reached, for the cover checks

vars == <<disk, idx, out, tix, seq, epoch, rev, pend, resync, batch, sch,
          used, staleWrite, unpublishedAck, ackedNewer, crossWrite, cover>>
ghosts == <<staleWrite, unpublishedAck, ackedNewer, crossWrite, cover>>

EmptyFn == [x \in {} |-> 0]
IdleBatch == [phase |-> "idle", full |-> FALSE, affected |-> {}, work |-> {}]
IdleSch == [phase |-> "idle", members |-> {}, queue |-> <<>>, plan |-> EmptyFn,
            wit |-> EmptyFn, results |-> {}, published |-> {}]

CurrentOutput(p) == [r \in Src(p) |-> idx[r]]

Init ==
  \* Every path was just edited and committed; its ticket is ready.
  /\ disk = [p \in Paths |-> 1]
  /\ idx = [p \in Paths |-> 1]
  /\ out = [p \in Paths |-> [r \in Src(p) |-> 0]]
  /\ tix = [s \in Scopes |-> IF s = Global THEN NoTicket
                            ELSE [present |-> TRUE, gen |-> 1, ready |-> TRUE]]
  /\ seq = [s \in Scopes |-> IF s = Global THEN 0 ELSE 1]
  /\ epoch = 0
  /\ rev = 1
  /\ pend = {}
  /\ resync = FALSE
  /\ batch = IdleBatch
  /\ sch = IdleSch
  /\ used = [edit |-> 0, save |-> 0, explicit |-> 0, restart |-> 0,
             interrupt |-> 0, failure |-> 0, lost |-> 0]
  /\ staleWrite = FALSE /\ unpublishedAck = FALSE /\ ackedNewer = FALSE
  /\ crossWrite = FALSE /\ cover = {}

Spend(k) == used' = [used EXCEPT ![k] = @ + 1]

-----------------------------------------------------------------------------
(* Ticket store: pkg/store/derived_work.go *)

Unready == \E s \in Scopes : tix[s].present /\ ~tix[s].ready

\* CurrentDerivedWork: exact generation, ready, kind epoch, and for a global
\* ticket the kind revision.
Current(m) == /\ tix[m.p].present /\ tix[m.p].gen = m.gen /\ tix[m.p].ready
              /\ epoch = m.epoch
              /\ (m.p = Global => rev = m.rev)

\* MarkDerivedDirty, one transaction: each scope gets the next generation with
\* ready=0; every mark bumps the revision; a global mark bumps the epoch.
Mark(scopes) ==
  /\ seq' = [s \in Scopes |-> IF s \in scopes THEN seq[s] + 1 ELSE seq[s]]
  /\ tix' = [s \in Scopes |-> IF s \in scopes
                              THEN [present |-> TRUE, gen |-> seq[s] + 1, ready |-> FALSE]
                              ELSE tix[s]]
  /\ epoch' = IF Global \in scopes THEN epoch + 1 ELSE epoch
  /\ rev' = rev + 1

\* The tickets MarkDerivedDirty returns.
MarkedWork(scopes) ==
  {[p |-> s, gen |-> seq[s] + 1, epoch |-> IF Global \in scopes THEN epoch + 1 ELSE epoch,
    rev |-> rev + 1] : s \in scopes}

\* ActivateDerivedWork: a global ticket first retires the kind's path tickets
\* when no mark followed it, then each exact generation becomes ready.
Activate(work) ==
  LET clearPaths == \E w \in work : w.p = Global /\ epoch = w.epoch /\ rev = w.rev
  IN [s \in Scopes |->
        IF s # Global /\ clearPaths THEN NoTicket
        ELSE IF \E w \in work : w.p = s /\ tix[s].present /\ tix[s].gen = w.gen
             THEN [tix[s] EXCEPT !.ready = TRUE]
             ELSE tix[s]]

\* PendingDerivedWorkForKind: ready rows with the epoch and revision read now,
\* and nothing while any ticket of the kind is unready.
Pending ==
  IF Unready THEN {}
  ELSE {[p |-> s, gen |-> tix[s].gen, epoch |-> epoch, rev |-> rev] :
          s \in {x \in Scopes : tix[x].present /\ tix[x].ready}}

-----------------------------------------------------------------------------
(* The world outside the indexer *)

\* An editor, git, or an agent writes a file; its event reaches the watcher
\* (pkg/app/bootstrap watcher_pending.go retainHubEvents).
Edit(p, v) ==
  /\ used.edit < MaxEdits /\ v # disk[p]
  /\ disk' = [disk EXCEPT ![p] = v]
  /\ pend' = pend \cup {p}
  /\ Spend("edit")
  /\ UNCHANGED <<idx, out, tix, seq, epoch, rev, resync, batch, sch, ghosts>>

\* A save publishes metadata and ontology projection for its file in a lane
\* job and creates no ticket; its file event reaches the watcher later
\* (indexing.md "Saves rely on watch or restart admission";
\* derived_direct_save_test.go publishDerivedTestSave).
DirectSave(p, v) ==
  /\ used.save < MaxSaves /\ batch.phase = "idle" /\ v # idx[p]
  /\ disk' = [disk EXCEPT ![p] = v]
  /\ idx' = [idx EXCEPT ![p] = v]
  /\ pend' = pend \cup {p}
  /\ Spend("save")
  /\ UNCHANGED <<out, tix, seq, epoch, rev, resync, batch, sch, ghosts>>

\* A file event that never reaches the watcher and raises no resync. Off in
\* every code check: indexing.md "Saves rely on watch or restart admission"
\* states this as an assumption, and the lost-event check shows what it buys.
EventLost(p) ==
  /\ used.lost < MaxLostEvents /\ p \in pend
  /\ pend' = pend \ {p}
  /\ Spend("lost")
  /\ UNCHANGED <<disk, idx, out, tix, seq, epoch, rev, resync, batch, sch, ghosts>>

\* The runtime stops and starts: memory is lost, the database is kept, and
\* the watcher starts with a stale cache, so it owes a full discovery.
\* A crash inside a structural batch is BatchInterrupted, then Restart.
Restart ==
  /\ used.restart < MaxRestarts /\ batch.phase = "idle"
  /\ sch' = IdleSch /\ pend' = {} /\ resync' = TRUE
  /\ Spend("restart")
  /\ UNCHANGED <<disk, idx, out, tix, seq, epoch, rev, batch, ghosts>>

-----------------------------------------------------------------------------
(* Structural publication by the watcher: one lane job.
   live_ownership.go reconcileBatchUnderLease -> indexcore.Discover ->
   indexcore.Publish (pkg/app/indexcore/publish.go). *)

\* hasPendingWork admits a batch for watcher input, a resync, or unready
\* tickets. Discover turns unready tickets into recovery, which forces full
\* discovery. Publish.mark() -> dirtyScopes (candidates.go) marks one path
\* ticket per affected path, or only the global ticket when full.
BatchMarks ==
  /\ batch.phase = "idle" /\ (pend # {} \/ resync \/ Unready)
  /\ LET full == resync \/ Unready
         scopes == IF full THEN {Global} ELSE pend
     IN /\ Mark(scopes)
        /\ batch' = [phase |-> "marked", full |-> full, affected |-> pend,
                     work |-> MarkedWork(scopes)]
  /\ pend' = {} /\ resync' = FALSE
  /\ UNCHANGED <<disk, idx, out, sch, used, ghosts>>

\* Publish commits the structural rows read from disk, drains the writer, then
\* SubmitActivateDerived (ActivateDerivedWork) makes the marked generations
\* ready; the watcher then wakes the scheduler.
BatchCompletes ==
  /\ batch.phase = "marked"
  /\ idx' = [p \in Paths |-> IF batch.full \/ p \in batch.affected THEN disk[p] ELSE idx[p]]
  /\ tix' = Activate(batch.work)
  /\ batch' = IdleBatch
  /\ UNCHANGED <<disk, out, seq, epoch, rev, pend, resync, sch, used, ghosts>>

\* Explicit preemption, external priority, or an error cancels the batch after
\* mark. Payloads already drained may have committed (any subset); tickets stay
\* unready; processOwnershipBatch restores the drained inputs.
BatchInterrupted(committed) ==
  /\ batch.phase = "marked" /\ used.interrupt < MaxInterrupts
  /\ committed \subseteq (IF batch.full THEN Paths ELSE batch.affected)
  /\ idx' = [p \in Paths |-> IF p \in committed THEN disk[p] ELSE idx[p]]
  /\ pend' = pend \cup batch.affected
  /\ resync' = (resync \/ batch.full)
  /\ batch' = IdleBatch
  /\ Spend("interrupt")
  /\ UNCHANGED <<disk, out, tix, seq, epoch, rev, sch, ghosts>>

-----------------------------------------------------------------------------
(* The derived scheduler: pkg/app/bootstrap/derived_*.go *)

StartGroup(g, q) == [IdleSch EXCEPT !.phase = "prepare", !.members = g, !.queue = q]

\* derived_scheduler.go run: after a group ends (acknowledged, superseded,
\* failed, or preempted) the pass moves to its next group.
EndRun == sch' = IF sch.queue = <<>> THEN IdleSch ELSE StartGroup(Head(sch.queue), Tail(sch.queue))

\* What a member's plan rebuilds and reads. derived_execute.go executeObserved:
\* a global ticket covers every path; an ontology member covers
\* RecordSourceSetPaths; a note member covers its own path.
Rebuilds(mp) == IF mp = Global THEN Paths ELSE IF RecordExpand THEN Src(mp) ELSE {mp}
Reads(mp) == UNION {Src(q) : q \in Rebuilds(mp)}
\* derived_members.go captureWitness: the paths whose fingerprints are kept.
WitnessKeys(mp) == IF mp = Global THEN Paths ELSE IF RecordWitness THEN Rebuilds(mp) ELSE {mp}
\* inputs[member.Path] in currentMembers: the witness keys and the member itself.
InputKeys(mp) == WitnessKeys(mp) \cup {mp}
\* derived_sources.go publishedNoteReader: disk content ahead of its rows.
Ahead(keys) == {q \in keys \cap Paths : disk[q] # idx[q]}

\* derived_scheduler.go run: PendingDerivedWorkForKind (a read outside the
\* lane); each global ticket is its own group, all path tickets form one group.
SchedulerReads ==
  /\ sch.phase = "idle" /\ Pending # {}
  /\ LET gm == {m \in Pending : m.p = Global}
         pm == Pending \ gm
         groups == (IF gm # {} THEN <<gm>> ELSE <<>>) \o (IF pm # {} THEN <<pm>> ELSE <<>>)
     IN sch' = StartGroup(Head(groups), Tail(groups))
  /\ UNCHANGED <<disk, idx, out, tix, seq, epoch, rev, pend, resync, batch, used, ghosts>>

\* The "derived_prepare" lane job: withLease -> currentMembers without a
\* witness (ticket check only), then publishedNoteReader over every consumed
\* path, captureWitness (drops members whose inputs are ahead on disk), and
\* the plan, all from the same committed rows.
Prepare ==
  /\ sch.phase = "prepare" /\ batch.phase = "idle"
  /\ LET cur == {m \in sch.members : CheckTicket => Current(m)}
         ahead == Ahead(UNION {Rebuilds(m.p) \cup {m.p} : m \in cur})
         live == {m \in cur : (Rebuilds(m.p) \cup {m.p}) \cap ahead = {}}
         lp == {m.p : m \in live}
     IN IF live = {}
        THEN EndRun   \* errDerivedSuperseded; dropped members are delayed
        ELSE sch' = [sch EXCEPT !.phase = "compute", !.members = live,
                       !.plan = [mp \in lp |-> [r \in Reads(mp) |-> idx[r]]],
                       !.wit = [mp \in lp |-> [r \in WitnessKeys(mp) |-> idx[r]]],
                       !.results = {}, !.published = {}]
  /\ UNCHANGED <<disk, idx, out, tix, seq, epoch, rev, pend, resync, batch, used, ghosts>>

\* Provider computation runs outside the lane and writes nothing durable.
ComputeReturns ==
  /\ sch.phase = "compute"
  /\ sch' = [sch EXCEPT !.phase = IF Style = "notes" THEN "publish" ELSE "final",
                        !.results = IF Style = "notes"
                                    THEN UNION {Rebuilds(m.p) : m \in sch.members} ELSE {}]
  /\ UNCHANGED <<disk, idx, out, tix, seq, epoch, rev, pend, resync, batch, used, ghosts>>

\* A provider error, computeContext's explicit-index preemption, or a failed
\* publication job ends the group without acknowledgement; run() delays the
\* members with RetryDerivedWorkBatch, which changes only attempt/retry_at.
PassFails ==
  /\ used.failure < MaxFailures /\ sch.phase \in {"compute", "publish"}
  /\ EndRun
  /\ Spend("failure")
  /\ UNCHANGED <<disk, idx, out, tix, seq, epoch, rev, pend, resync, batch, ghosts>>

\* derived_members.go currentMembers with the witness, inside a lane job:
\* ticket check, witness fingerprints, and disk ahead of the rows; a member is
\* dropped when any of its inputs is in the changed set (independentMembers).
Live ==
  LET ms == sch.members
      tdrop == IF CheckTicket THEN {m.p : m \in {x \in ms : ~Current(x)}} ELSE {}
      wchanged == {r \in Paths : \E mp \in DOMAIN sch.wit :
                     r \in DOMAIN sch.wit[mp] /\ idx[r] # sch.wit[mp][r]}
      changed == tdrop \cup wchanged \cup Ahead(UNION {WitnessKeys(m.p) : m \in ms})
  IN {m \in ms : InputKeys(m.p) \cap changed = {}}

MemberOf(mp) == CHOOSE m \in sch.members : m.p = mp

\* Ground truth for P1: the ticket is current and every input the plan read
\* still has the value it was planned from.
WriteOK(m) == Current(m) /\ \A r \in DOMAIN sch.plan[m.p] : idx[r] = sch.plan[m.p][r]

\* AckDerivedWorkMatched: the rows this acknowledgement deletes.
Deleted(ms) == {m \in ms : IF AckFilter THEN Current(m) ELSE tix[m.p].present}

Acknowledge(ms, published) ==
  /\ tix' = [s \in Scopes |-> IF s \in {m.p : m \in Deleted(ms)} THEN NoTicket ELSE tix[s]]
  /\ unpublishedAck' = (unpublishedAck \/ \E m \in Deleted(ms) : ~(Rebuilds(m.p) \subseteq published))
  /\ ackedNewer' = (ackedNewer \/ \E m \in Deleted(ms) : tix[m.p].gen # m.gen)

\* derived_publication.go computeAndPublish, one "derived_publication" lane
\* job per batch of results: withLease -> currentMembers, then publish each
\* result whose member is live and keep (cache) the others.
PublishBatch(S) ==
  /\ Style = "notes" /\ sch.phase = "publish" /\ batch.phase = "idle"
  /\ S \subseteq sch.results /\ S # {}
  /\ LET live == Live
         lp == {m.p : m \in live}
         global == Global \in {m.p : m \in sch.members}
         Owner(q) == IF global THEN Global ELSE q
         stop == live = {} \/ (~PublishSurvivors /\ live # sch.members)
         pubs == IF stop THEN {} ELSE {q \in S : ~PublishOnlyLive \/ Owner(q) \in lp}
     IN /\ out' = [q \in Paths |-> IF q \in pubs
                                   THEN [r \in Src(q) |-> sch.plan[Owner(q)][r]] ELSE out[q]]
        /\ staleWrite' = (staleWrite \/ \E q \in pubs : ~WriteOK(MemberOf(Owner(q))))
        /\ crossWrite' = (crossWrite \/ \E q \in pubs : q \notin UNION {Rebuilds(mp) : mp \in lp})
        /\ cover' = IF live # {} /\ live # sch.members /\ pubs # {}
                    THEN cover \cup {"survivors"} ELSE cover
        /\ IF stop
           THEN EndRun   \* errDerivedSuperseded: no live member
           ELSE sch' = [sch EXCEPT !.results = @ \ S, !.published = @ \cup pubs]
  /\ UNCHANGED <<disk, idx, tix, seq, epoch, rev, pend, resync, batch, used,
                 unpublishedAck, ackedNewer>>

\* executeObserved's final "derived_publication" withLease(ack=true) for notes:
\* currentMembers over the members computeAndPublish returned (those whose
\* results were all published), FinishDerived cleanup for the live paths, then
\* AckDerivedWorkMatched(live). Filtering Live afterwards is the same thing: a
\* path member's liveness depends only on its own inputs, and with no member
\* left, withLease returns errDerivedSuperseded without acknowledging.
NotesFinal ==
  /\ Style = "notes" /\ sch.phase = "publish" /\ sch.results = {} /\ batch.phase = "idle"
  /\ LET live == Live
         acked == IF AckOnlyPublished
                  THEN {m \in live : Rebuilds(m.p) \subseteq sch.published} ELSE live
     IN IF live = {} \/ (~PublishSurvivors /\ live # sch.members)
        THEN UNCHANGED <<tix, staleWrite, unpublishedAck, ackedNewer>>
        ELSE /\ Acknowledge(acked, sch.published)
             /\ staleWrite' = (staleWrite \/ \E m \in Deleted(acked) : ~WriteOK(m))
  /\ EndRun
  /\ UNCHANGED <<disk, idx, out, seq, epoch, rev, pend, resync, batch, used, crossWrite, cover>>

\* executeObserved's ontology withLease(ack=true): currentMembers; when a
\* member dropped, cache the indivisible result and replan the survivors
\* (errDerivedReplan); otherwise PublishDerived and acknowledge, one lane job.
OntologyFinal ==
  /\ Style = "ontology" /\ sch.phase = "final" /\ batch.phase = "idle"
  /\ LET live == Live
         lp == {m.p : m \in live}
         mps == {m.p : m \in sch.members}
         written == UNION {Rebuilds(mp) : mp \in mps}
         Owner(q) == CHOOSE mp \in mps : q \in Rebuilds(mp)
     IN CASE live = {} ->
               /\ EndRun
               /\ UNCHANGED <<out, tix, staleWrite, unpublishedAck, ackedNewer, crossWrite, cover>>
          [] live # sch.members /\ PublishOnlyLive ->
               /\ IF PublishSurvivors THEN sch' = StartGroup(live, sch.queue) ELSE EndRun
               /\ cover' = IF PublishSurvivors THEN cover \cup {"survivors"} ELSE cover
               /\ UNCHANGED <<out, tix, staleWrite, unpublishedAck, ackedNewer, crossWrite>>
          [] OTHER ->
               /\ out' = [q \in Paths |-> IF q \in written
                                          THEN [r \in Src(q) |-> sch.plan[Owner(q)][r]] ELSE out[q]]
               /\ Acknowledge(live, written)
               /\ staleWrite' = (staleWrite \/ \E m \in sch.members : ~WriteOK(m))
               /\ crossWrite' = (crossWrite \/ ~(written \subseteq UNION {Rebuilds(mp) : mp \in lp}))
               /\ EndRun
               /\ UNCHANGED cover
  /\ UNCHANGED <<disk, idx, seq, epoch, rev, pend, resync, batch, used>>

-----------------------------------------------------------------------------
(* Explicit indexing: pkg/app/indexing/unified.go RunUnifiedCore, one
   explicit-index lane job. It preempts background lane jobs (a cancelled
   structural batch is BatchInterrupted) and, through computeContext, may
   cancel a pass that is computing. Discovery is full: Publish marks the
   global ticket, commits every path, and activation retires path tickets.
   Embedding covers the paths whose rows changed (RECORD hosts expand to their
   source set), or every path when a ticket of the kind existed at discovery
   (NotesRecovery / OntologyRecovery). SubmitDerivedAcknowledgement then
   acknowledges the global ticket. When the provider fails the job returns
   before acknowledging, and the global ticket stays ready. *)
ExplicitIndex(ok, cancel) ==
  /\ used.explicit < MaxExplicit /\ batch.phase = "idle"
  /\ cancel => sch.phase = "compute"
  /\ LET changed == {p \in Paths : disk[p] # idx[p]}
         recovery == \E s \in Scopes : tix[s].present
         redo == IF recovery THEN Paths ELSE {q \in Paths : Src(q) \cap changed # {}}
         gen == seq[Global] + 1
     IN /\ idx' = disk
        /\ out' = IF ok THEN [q \in Paths |-> IF q \in redo THEN [r \in Src(q) |-> disk[r]]
                                                ELSE out[q]]
                  ELSE out
        /\ seq' = [seq EXCEPT ![Global] = gen]
        /\ tix' = [s \in Scopes |-> IF s = Global /\ ~ok
                                    THEN [present |-> TRUE, gen |-> gen, ready |-> TRUE]
                                    ELSE NoTicket]
  /\ epoch' = epoch + 1 /\ rev' = rev + 1
  /\ IF cancel THEN EndRun ELSE UNCHANGED sch
  /\ Spend("explicit")
  /\ UNCHANGED <<disk, pend, resync, batch, ghosts>>

-----------------------------------------------------------------------------
Quiet == /\ pend = {} /\ ~resync /\ batch.phase = "idle" /\ sch.phase = "idle"
         /\ \A s \in Scopes : ~tix[s].present

\* Nothing is left to do; this stops TLC reporting the end state as a deadlock.
\* Any other state without a step out is stuck work, and TLC reports it.
Done == Quiet /\ UNCHANGED vars

Next ==
  \/ \E p \in Paths, v \in Values : Edit(p, v) \/ DirectSave(p, v)
  \/ Restart \/ \E p \in Paths : EventLost(p)
  \/ BatchMarks \/ BatchCompletes
  \/ \E c \in SUBSET Paths : BatchInterrupted(c)
  \/ SchedulerReads \/ Prepare \/ ComputeReturns \/ PassFails
  \/ \E S \in SUBSET Paths : PublishBatch(S)
  \/ NotesFinal \/ OntologyFinal
  \/ \E ok, cancel \in BOOLEAN : ExplicitIndex(ok, cancel)
  \/ Done

Spec == Init /\ [][Next]_vars

\* The watcher and the scheduler keep taking their next step while it stays
\* possible: the lane serves every queued job, and the scheduler wakes on its
\* 250 ms ticker. Environment actions, failures and interruptions are unfair.
Fairness ==
  /\ WF_vars(BatchMarks) /\ WF_vars(BatchCompletes)
  /\ WF_vars(SchedulerReads) /\ WF_vars(Prepare) /\ WF_vars(ComputeReturns)
  /\ WF_vars(\E S \in SUBSET Paths : PublishBatch(S))
  /\ WF_vars(NotesFinal) /\ WF_vars(OntologyFinal)

FairSpec == Spec /\ Fairness

-----------------------------------------------------------------------------
(* Rules. P1-P3 are the protocol's promise; the cover checks are scenarios
   that must stay reachable, so the runner expects them to "fail". *)

\* P1. Results, cleanup and acknowledgements are written only while the
\* ticket is current and every input the plan read is unchanged.
NoStalePublication == ~staleWrite

\* P2a. When nothing is left to do, every output was built from the current
\* committed rows of every path it depends on.
QuietMeansCurrent == Quiet => \A p \in Paths : out[p] = CurrentOutput(p)

\* P2b. A ticket is acknowledged only for work whose results were published.
NoAckWithoutPublication == ~unpublishedAck

\* P2c. An acknowledgement never deletes a newer generation marked while the
\* run was in flight.
AckNeverCoversNewerChange == ~ackedNewer

\* P3. A grouped run writes only outputs that belong to members that are
\* current at that moment, never a dropped member's.
GroupPublishesOnlyCurrentMembers == ~crossWrite

\* Cover: after a member drops, a survivor still publishes (notes) or is
\* replanned (ontology) in the same run. Expected to be violated.
CoverSurvivorsContinue == "survivors" \notin cover

\* P2d. Every behavior ends quiet once the environment stops acting: no
\* ticket, watcher input or run is left behind for ever. Checked with FairSpec.
EventuallyQuiet == <>[]Quiet

\* Model sanity: structural rows match disk when nothing is left to do.
QuietMeansIndexed == Quiet => idx = disk
=============================================================================
