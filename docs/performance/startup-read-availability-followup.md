---
summary: "Deferred read-availability contract for ownership reconciliation, with source evidence and acceptance criteria."
last-verified: 2026-09-05
---

# Startup read availability during ownership reconciliation

Status: scoped follow-up; not fixed by the engine performance batches or B11 source-link optimization. Source-only assessment, 2026-09-05. Coordinator accepted deferral. No implementation or behavior tests were performed for this assessment.

## Observed problem and source evidence

The browser investigation observed valid note requests against a copied legacy Rhizome index, followed by successful null responses while background indexing reconciled ownership. Both original and combined implementations exhibit this behavior. This is separate from the measured warm note-detail latency addressed by B11.

Relevant source was inspected in the write worktree and compared with integration `dd0da04801a80d3210f6ae85d9572346b4481580`. The publication and readiness files were identical except for B09's removed `IndexCandidates` boolean argument in `unified.go`; that difference does not change this finding. Line numbers below describe that inspected source.

- `pkg/anchors/sqlite/ownership_transition.go`: an effective `ApplyOwnershipTransitions` transaction retires prior note/code/ontology/graph evidence, publishes replacement source provenance, invalidates metadata readiness, and advances the durable reconciliation generation atomically.
- `pkg/anchors/sqlite/ownership_transition_reconciliation.go:154`: `invalidateOwnershipMetadataStateTx` deletes `note_metadata_state`. `PendingOwnershipReconciliation` reads generation and acknowledgement in one SQLite statement. Exact-generation acknowledgement leaves newer work pending.
- `pkg/anchors/sqlite/note_metadata.go:993`: path-scoped metadata reads return no rows when global metadata state is absent/unready. This is deliberate eligibility behavior, not proof that the requested source does not exist.
- `pkg/app/indexing/unified.go:599`: initial ownership commit precedes destination work. Prepared provider outcomes cross another ownership fence around line 705. The complete metadata delta is built at line 716, but submitted and flushed around lines 809-815 after code ingestion (753-769), note ingestion (780-800), and the ingest writer barrier. Ontology follows; exact-generation acknowledgement remains after semantic, graph, validation, and final writer work around line 1009.
- `pkg/app/bootstrap/live_ownership.go:203`: the live path commits ownership to a fixed point, then ingests destinations at line 208, publishes metadata and ontology at line 215, completes semantic and graph destinations at line 248, and acknowledges the exact generation at line 253. Errors retain reconciliation debt.
- `pkg/app/bootstrap/live_ownership_publication.go:85`: the live publication helper combines metadata publication, ontology synchronization, and ontology body embedding work. Moving the whole helper before ingestion would violate its dependencies.
- `cmd/serve.go:120` and `:382`: a positive prior ontology-node count can open the boot gate. `pkg/app/web/gate.go:40` opens that gate once by closing a channel; it cannot revoke readiness after a later ownership transition, including one committed by another process.
- `pkg/app/web/graphql_public.go:32`: the public GraphQL handler computes a response before its final JSON write, providing a narrow place to reject a response if the durable generation changed during execution.

## Why early metadata Ready is not the local fix

The prepared metadata delta contains complete canonical note facts, provider provenance, fragment targets, and alias-resolved note graph edges. Its derivation does not require code ingestion, so an earlier raw-metadata publication stage is mechanically possible. It is not a complete note-detail publication boundary.

Ownership has already retired cross-domain evidence. Ontology consumes note and code-intel rows after ingestion. `pkg/ontology/runtime.go:53-59` requires metadata and ontology to agree on hashes and `LoadedAt`, as well as readiness and materializer identity. Publishing only metadata earlier would expose partial cross-domain results or still fail ontology readiness.

The metadata writer also updates `notes.content_hash` and `mtime`, which note-ingest freshness guards read. Any future reorder must preserve forced candidate paths from both ownership commits and preserve sealed-source identity; otherwise it risks incorrectly skipping destination work. It also changes failure semantics: errors that currently precede metadata publication would leave newly published raw metadata.

`docs/reference/subsystems/notemeta.md` explicitly requires ownership readiness not be republished merely to hide pending code, graph, link, ontology, or anchor work. The complete delta remains the source publication barrier; a state-only Ready write is not acceptable. Preserve the queued writer lane, both ownership fences, ingestion-before-ontology ordering, exact-generation acknowledgement, provider/fatal eligibility, full alias/link rederivation, and pending recovery after errors.

## Proposed bounded follow-up

Preserve the existing startup gate and add durable request-scoped availability checks for the selected persisted query endpoints. Before query execution, require no pending ownership reconciliation and usable metadata state. Check durable reconciliation state again before writing the buffered response. If either observation is pending, or the generation changed even if reconciliation already completed, discard the result and return `503 INDEX_INITIALIZING` with `Retry-After`. Store-check failures are service unavailability, not missing-note results.

The pre/post generation check closes the race where ownership replacement starts after the initial check. A resettable process-local channel alone cannot cover followers or external writers. The check must not acquire the index lock, initiate indexing, bypass metadata eligibility, or introduce arbitrary buffering around streaming handlers.

Choose the endpoint contract explicitly: the note-detail GraphQL response is an initial candidate. A narrow shared query availability helper should keep recipes and views that execute the same persisted query consistent. Other graph and ontology endpoints must be included deliberately or recorded as outside the initial scope. Inspect request execution and error mapping before selecting the helper's exact placement.

This proposal is conservative: reconciliation can remain pending through embedding and graph work. It promises truthful temporary unavailability and recovery, not uninterrupted browsing, shorter reconciliation, or a universally consistent snapshot for every unrelated writer. Existing non-ownership publication paths need their own review before expanding that claim.

Uninterrupted full-detail browsing through destructive ownership replacement requires preserving an immutable prior read snapshot or introducing a separately published read generation. An earlier raw-note surface would require a distinct readiness contract and explicit unavailable derived fields. Those are separate design choices and are not covered by moving the current Ready write.

## Acceptance and validation

1. A populated old index that enters ownership reconciliation never returns a successful missing-note result solely because metadata is temporarily unready.
2. With code ingestion held at a deterministic barrier, affected requests promptly return a structured retry response. Cancellation and failed indexing preserve pending state and the same truthful response.
3. After exact-generation convergence, the same note-detail request succeeds with complete expected data. A stable genuinely missing note still returns its normal missing result.
4. If a transition starts during query execution, reject the buffered response. Also cover a transition that starts and completes before the postcheck; a changed generation still invalidates the response.
5. A follower detects another process's transition without a local index callback. No positive ontology-row count overrides pending reconciliation or unusable metadata.
6. Valid empty or ontology-free vaults are handled according to endpoint capability, not a blanket nonzero-node-count requirement. Intentionally ungated embedded/test runtimes remain supported.
7. Existing ownership, fatal-source, sealed-source, alias/link, recovery, and writer-barrier behavior remains intact. Use deterministic concurrency barriers rather than timing sleeps.
8. Test the selected endpoint set and client retry/error presentation. Verify a copied-index browser session shows temporary availability status and recovers instead of displaying a missing note.
9. Measure request-check overhead only in an assigned quiet slot; do not mix it with B11 or final fixture timings. Follow the repository's required gates for the actual implementation scope.

## Disposition

Do not implement early metadata Ready or broad query generation gating within the current performance changes. Retain this as a scoped producer/readiness and request-contract follow-up. B11 continues warm source-link latency work independently.
