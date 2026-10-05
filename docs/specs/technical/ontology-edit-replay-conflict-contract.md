---
type: TechnicalSpec
summary: "Defines ontology edit replay invariants: canonical locator capture, safe rebase boundaries, conflict taxonomy, span ownership, restored-session behavior, and regression surfaces for source-preserving markdown edits."
id: SPEC-0046
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0046
  - ontology-edit-replay-conflict-contract
---

# Ontology edit replay conflict contract

## Summary

Ontology edit replay is the Markdown provider's source-preserving structural write path for typed Markdown. It accepts user- or validation-authored operations, resolves author-facing locators into canonical `NodeRef` identity, stages semantic operations rather than raw patches, and replays those operations against either the captured base snapshot or the current on-disk snapshot.

This contract sits below the browser workspace and above the Markdown parser. The authored Markdown file remains authoritative for operations governed by this contract. Edit replay may safely rebase through unrelated file drift, but it must fail closed when a targeted node, field, collection membership/order, locator, or owned source span can no longer be proven to represent the same authored object.

Related governing specs, listed as paths instead of wikilinks so this spec does not accidentally author `successor` relations through ambient body-link discovery:

- `docs/specs/technical/structural-node-model-and-ontology-read-path.md`
- `docs/specs/technical/node-workspace-capability-pipeline.md`
- `docs/specs/technical/linkable-embedded-node-identifiers.md`
- `docs/specs/technical/ontology-indexed-read-model-contract.md`
- `docs/specs/technical/format-aware-note-maintenance-mutations.md`

## Implementation Surface

- `pkg/ontology/edit_session.go` owns staged operation replay, per-file base/current/updated fingerprints, conflict conversion, atomic multi-file writes, collection drift detection, narrative block resolution, block-id insertion, and field/span mutation.
- `pkg/ontology/projection.go` owns `NodeRef`, snapshot parsing, locator resolution, structural fingerprints, byte ranges, field bindings, and collection bindings.
- `pkg/ontology/body.go` owns node body block projection: narrative, inline field, child section, and collection spans.
- `pkg/ontology/fixes.go` and `pkg/ontology/node_link.go` emit validation/linkability operations that must replay through the same edit-session path.
- `pkg/app/web/ontology.go`, `pkg/app/web/ontology_sessions.go`, `pkg/app/web/ontology_diff.go`, and `pkg/app/web/types.go` own the HTTP edit-session contract, canonicalized op snapshots, restored-session behavior, diff payloads, and workspace refresh after commit.
- Regression anchors are `pkg/ontology/projection_edit_test.go` and the ontology edit-session tests in `pkg/app/web/server_integration_test.go`.

## Goals

- preserve authored markdown formatting while changing only the source spans owned by staged ontology operations
- make canonical node identity the write contract after accepting author-facing path, heading, or block locators
- define which file drift can be rebased and which drift must become a conflict
- define source-span ownership for field, narrative, collection, delete, add, block-id, and whole-file operations
- keep browser edit sessions recoverable after server restart without treating client snapshots as authority
- give future implementation agents a single contract for conflict behavior and regression tests

## Non-Goals

- changing runtime behavior or introducing a new edit API
- replacing markdown source files with a generated normalized document format
- guaranteeing conflict-free merges for arbitrary simultaneous edits to the same node or collection
- making `NodeRef.String()` a complete cache, graph, or write identity
- making read-only GraphQL, search, semantic-retrieval, or indexing paths mutate files implicitly
- specifying the browser interaction design for resolving conflicts
- routing HTML root-metadata, authored-link, or move mutations through the Markdown parser/edit replay path; those use the shared validation transaction contract and format-aware maintenance contract

## Requirements

### Operation Model And Replay Boundary

- Edit sessions MUST stage semantic operations, not precomputed text patches, for ontology-aware edits.
- Staged operations MUST be grouped and replayed per note path in deterministic path order.
- `Preview` MUST replay against the captured base content and MUST NOT touch disk.
- `PreviewCurrent` MUST replay against current disk content when the current fingerprint differs from the captured base fingerprint.
- `Commit` MUST use the same current-disk replay path as `PreviewCurrent` and MUST write only when every touched file replays without conflicts.
- Every operation MUST have a stable identity so recovery, compaction, retry, and commit are idempotent.
- Equivalent user paths MUST canonicalize before operations are grouped, locked, replayed, or published.
- Multi-file commit MUST coordinate through a vault-wide canonical-path writer, preserve existing file mode, verify the expected current fingerprint immediately before replacement, and publish all touched files or report a typed failure.
- Publication MUST use collision-safe temporary and backup names, report rollback failures, clean prepared artifacts after every exit, and maintain enough journal state to recover or finish an interrupted transaction deterministically.
- A commit request's durable receipt MUST publish inside the same recovery journal as its note replacements. Recovery treats missing receipt publication as an incomplete prepared transaction and cannot expose new note bytes without the matching retry identity.
- The receipt MUST bind its request ID to the complete immutable submission: session, accepted revision, canonical operations, and verified base documents. A retry that changes any of those inputs MUST fail.
- Long-running server startup MUST recover edit journals before cache, watcher, or index workers can observe vault files. Direct server construction MUST run the same barrier unless its caller proves recovery already completed.
- Whole-file rewrite/transform operations MAY exist as escape hatches, but callers using them own the broader span and must not pretend they preserve node-level conflict semantics.

### Locator Capture And Canonical Identity

- Browser and validation callers MAY submit author-facing locators such as `note.md`, `note.md#Heading`, `note.md#^block-id`, or op-specific `nodeId` values.
- Before an operation is persisted in a web edit session, the server MUST resolve non-root node locators and copy the resolved `NodeID` and `Structural` fingerprint onto the operation.
- Stored operations MUST keep enough identity to distinguish note roots, structural sections, and embedded nodes after headings or byte offsets drift.
- `NodeRef.String()` MUST remain an author-facing locator only; conflict detection and session dirty state must use canonical identity that includes `NodeID`, `Kind`, and structural fingerprint when available.
- Replaying a structural ref MUST reject a projection whose resolved structural fingerprint no longer matches the requested fingerprint.
- Replaying a node-id ref without structural identity MUST reject a projection whose resolved node id changes.
- Duplicate heading fragments MUST remain unsupported as precise edit targets unless a stronger node id, block id, or structural ref disambiguates them.

### Conflict Taxonomy

- Replay MUST report structured conflicts rather than partially writing files when a staged op cannot be applied.
- Missing target nodes MUST report `MISSING_NODE`.
- Missing target fields or collections MUST report `MISSING_FIELD`.
- Collection membership drift, same-member order drift, missing ordered fragments, and invalid collection/member spans MUST report `COLLECTION_DRIFT`.
- Unsupported targets, invalid block IDs, duplicate block IDs, invalid insertion offsets, and unsupported op kinds MUST report `UNSUPPORTED_TARGET`.
- A targeted field whose current value differs from the operation's captured expected value MUST report `FIELD_CHANGED` with the expected, current, and proposed values.
- A syntactically valid operation that produces no source change MUST report a successful no-op outcome rather than a conflict.
- Conflict reports MUST include note path, node ref when available, field/collection when available, and a message preserving the lower-level reason.
- A file may be marked rebased without being conflicted when current disk drift does not invalidate any staged operation.

### Safe Rebase Rules

- Note-root Markdown YAML-frontmatter edits may rebase over unrelated body changes when the target frontmatter can still be parsed or synthesized.
- Inline field edits may rebase over unrelated content when the target node and field binding still resolve inside the current node-owned range.
- Field edits MUST NOT overwrite external drift to the same targeted field. They may rebase only when the captured expected field value still matches current source.
- Add-embedded-node operations may rebase over unrelated insertions when the parent collection/section still resolves and its insertion offset is valid.
- Narrative edits may rebase when the originally targeted narrative block still resolves by exact range and prior text, or by a unique/nearest matching prior narrative block according to the current resolver policy.
- Reorder operations MUST NOT rebase over collection membership changes, and MUST NOT rebase over same-member order drift that occurred outside the staged operation.
- Reorder operations MUST prove the requested order is an exact permutation of current members, including multiplicity, before moving source spans.
- Delete operations MUST NOT rebase when the resolved node span is invalid, missing, or no longer matches the requested identity.
- Block-id operations MUST NOT silently overwrite another node's existing block ID.
- Add-node and block-id operations MUST reject a requested block ID already owned anywhere in the note.

### Span Ownership

- `NodeBodyBlock` ranges are the browser-visible ownership model for node body editing.
- Narrative operations MUST only replace blocks whose kind is `narrative`; inline fields, child sections, and collection ranges are not narrative-owned text.
- Narrative replacement MUST preserve the original trailing whitespace/newline suffix of the owned raw block.
- Inline field operations MUST replace the first matching inline field line and remove trailing duplicate inline spans for that same field from bottom to top.
- New inline fields MUST be inserted inside the target node before an embedded block-id line when present, or at the node end when no block-id line exists.
- Collection reorder owns the projected collection range and moves each member block from its item start through the next item start, preserving interstitial content attached to that member.
- Delete-node owns the resolved node's full byte range and is intentionally out of scope for full-note deletion.
- Add-embedded-node owns only the insertion at the collection end or parent node end and must preserve existing collection members.
- Ensure/set-block-id operations own only the node's block-id line when one exists, or the safe block-id insertion point for the embedded node.

### Web Edit Sessions

- `/api/ontology/edit-sessions` is the canonical browser write surface for provider capabilities currently supported by ontology edit replay; `/api/ontology/edit` is only a transitional short-lived shim. Formats without compatible structural edit capabilities MUST NOT be routed through this Markdown operation model.
- Create and stage responses may return canonical dirty state without revalidating against disk; GET, preview, diff, and commit MUST revalidate against disk.
- Session creation, recovery, and explicit review responses MUST include the canonical compact operation list and verified original bases needed for recovery. Ordinary stage acknowledgements MAY return only accepted operation IDs, revisions, affected identities, and status.
- Client-stored snapshots are backup state, not write authority. Restored sessions MUST replay server-side and re-run preview/rebase/conflict detection before committing.
- Restored sessions MUST preserve original base fingerprints so disk drift after staging is reported as rebased/stale rather than silently treated as the new clean base.
- Recoverable snapshots MUST preserve one verified original base snapshot per touched file plus stable operation identity and expected target state. Current disk content MUST never be substituted as the restored base.
- After a clean rebased preview, additional operations MUST capture their expected target state from that accepted current projection while preserving the session's verified original file base for recovery and final review.
- Successful commits MUST clear session ops and refresh touched node workspaces; conflicted commits MUST keep ops available for review and retry.
- Commit responses MUST use one `committed`, `unchanged`, `conflicted`, or `failed` outcome. Rebase remains an independent boolean and validation or publication details remain typed failure information; an empty change set MUST NOT imply failure.

### Typed Values And Source Preservation

- Changed values MUST be validated against the owning field's scalar, enum, list, nullability, identifier, and relation-target contract before source mutation.
- Changed relation assignments MUST be revalidated against the final session overlay and provider-current external targets during restored preview and again before commit. Missing, ambiguous, or incompatible targets retain the draft and return an operation-addressed conflict.
- Validation MUST be scoped to changed targets so unrelated pre-existing diagnostics do not prevent repair.
- Optional unset MUST remain distinct from false, zero, empty string, and an empty list.
- List operations MUST carry ordered values as a list and MUST NOT collapse them into the first value or comma-delimited scalar text.
- Frontmatter field operations MUST patch only the owned scalar or sequence span and preserve unrelated comments, quoting, multiline style, key order, indentation, anchors, and nested data.
- Source-wide operations MUST retain a full-file base fingerprint, expose their broad ownership in preview and diff results, and reject graph-sensitive heading, block-id, or link changes that require a graph-safe mutation.

### Validation, Linkability, And Fix Ops

- Validation fix suggestions that mutate ontology-backed notes MUST express their work as edit-session operations.
- Missing embedded block-id fixes MUST target the resolved node id or structural fingerprint plus the generated block id.
- Linkability reads MUST remain read-only unless the caller explicitly requests planning or applying fix operations.
- Block-id repair MUST compose with other staged operations in the same file through edit-session replay rather than ad hoc file writes.

### Regression Expectations

- Unit tests MUST cover simple rebase, narrative rebase, inline field preservation, duplicate inline span removal, collection membership drift, collection order drift, missing node conflicts, multi-file commits, path-derived section refs, and ambiguous heading rejection.
- Web integration tests MUST cover multi-node sessions, diff payloads, base fingerprint preservation, collection conflict status, rebase status, restored snapshots, restored rebased snapshots, and staging after rebase.
- Future changes to locator resolution, span ownership, or conflict classification MUST add or update tests before changing behavior.
- Publication tests MUST cover canonical path aliases, a final pre-replace drift check, mode preservation, unique backups, cleanup, rollback error reporting, same-path cross-session serialization, and interrupted-transaction recovery.

## User Stories

### US1 - Edit a typed node without losing authored markdown
- id:: ^SPEC-0046-US1
- summary:: As an ontology workspace user, I can edit fields and narrative blocks while Rhizome preserves comments, bullets, child sections, block IDs, and unrelated prose.
- status:: ready

#### Acceptance Criteria

- Field and narrative edits preserve unowned markdown.
  verification:: `go test ./pkg/ontology -run 'TestEditSession_SetInlineField_PreservesEmbeddedFormatting|TestEditSession_SetScalarAndLinkFields_CommitPreservesBody|TestEditSession_SetNarrative_CommitRebasesByMatchingNarrativeBlock'`
- Body-block ownership prevents narrative edits from consuming inline fields, child sections, or collection members.
  verification:: Inspect `BuildNodeBody` and `resolveNarrativeBlock`; targeted regression coverage belongs in `pkg/ontology/body_test.go` or `pkg/ontology/projection_edit_test.go`.

### US2 - Rebase through unrelated drift but conflict on unsafe drift
- id:: ^SPEC-0046-US2
- summary:: As an editor, I can commit a staged change after unrelated file edits, but Rhizome stops me when another edit invalidates my target node or collection.
- status:: ready

#### Acceptance Criteria

- Unrelated disk drift marks the plan rebased and still commits safely.
  verification:: `go test ./pkg/ontology -run TestEditSession_Commit_AutoRebasesSimpleExternalChange`
- Collection membership and order drift return typed conflicts without writing files.
  verification:: `go test ./pkg/ontology -run 'TestEditSession_Commit_ReportsCollectionDriftConflict|TestEditSession_Commit_ReportsCollectionOrderDriftConflict'`
- External drift to a targeted field returns `FIELD_CHANGED` with expected, current, and proposed values, while unrelated field or body drift still rebases.
  verification:: Focused edit-session regression tests cover same-field conflict and unrelated-field rebase.
- Missing or structurally changed nodes return `MISSING_NODE`.
  verification:: `go test ./pkg/ontology -run TestEditSession_Commit_ReportsMissingNodeConflict`

### US3 - Recover browser edit sessions after restart
- id:: ^SPEC-0046-US3
- summary:: As a browser user, I can restore a client-saved edit session after the server forgets it, and the restored session still respects original base fingerprints and current disk drift.
- status:: ready

#### Acceptance Criteria

- Restored snapshots replay server-side before preview or commit.
  verification:: `go test ./pkg/app/web -run TestOntologyEditSessions_RestoresFromClientSnapshot`
- Restored snapshots report rebased/stale state when disk changed after staging.
  verification:: `go test ./pkg/app/web -run TestOntologyEditSessions_RestoresSnapshotAfterExternalRebase`
- Restored collection operations retain their original membership and order witness and conflict after external reorder.
  verification:: A web integration regression restores a session after collection-order drift and proves the file is unchanged.
- Staging after a rebased preview advances the live editor base to current disk.
  verification:: `go test ./pkg/app/web -run TestOntologyEditSessions_StageAfterRebasedPreviewUsesCurrentBase`

### US4 - Publish edit sessions without overwriting concurrent file changes
- id:: ^SPEC-0046-US4
- summary:: As an editor, I can trust a successful commit to preserve file metadata and all-or-nothing publication even when another writer or process acts concurrently.
- status:: ready

#### Acceptance Criteria

- Canonical path aliases participate in one replay and writer group, so two spellings cannot overwrite one another. ^SPEC-0046-US4-AC1
- Commit verifies the replayed fingerprint under a vault-wide canonical-path write lease immediately before replace. A changed file returns a typed conflict without publication. ^SPEC-0046-US4-AC2
- Publication preserves existing file mode and uses unique temporary and backup files without deleting unrelated artifacts. ^SPEC-0046-US4-AC3
- Multi-file rollback and interrupted-transaction recovery report failures and converge on either the full old set or the full new set. ^SPEC-0046-US4-AC4
- A later preparation failure cleans every earlier temporary artifact, and retries remain idempotent. ^SPEC-0046-US4-AC5

### US5 - Reject destructive collection, identifier, and typed-value operations
- id:: ^SPEC-0046-US5
- summary:: As an editor, I receive a typed rejection before an invalid operation can duplicate, delete, collapse, or reformat authored data.
- status:: ready

#### Acceptance Criteria

- Reorder accepts only an exact permutation of current collection members and cannot duplicate one member while deleting another. ^SPEC-0046-US5-AC1
- Add-node and block-id operations reject a duplicate block ID anywhere in the target note. ^SPEC-0046-US5-AC2
- Changed scalar, enum, list, relation, and identifier values are checked against the owning schema before mutation, while unrelated existing diagnostics remain repairable. ^SPEC-0046-US5-AC3
- Frontmatter edits preserve unrelated YAML comments, key order, quoting, multiline style, indentation, anchors, and nested structures. ^SPEC-0046-US5-AC4
- A source-wide edit carries explicit broad ownership, checks its original fingerprint, and rejects graph-sensitive changes that need safe link or locator mutation. ^SPEC-0046-US5-AC5

## Open Questions

- Should narrative duplicate matching eventually conflict when multiple candidate blocks have identical previous markdown, instead of choosing the nearest block to the original range?
- Should conflict reports distinguish invalid block IDs and duplicate block IDs with a dedicated `LOCATOR_CONFLICT` kind rather than `UNSUPPORTED_TARGET`?
- Should `RewriteFile` and `StageFileTransform` expose an explicit broad-span warning in web diff payloads so the UI can distinguish safe node edits from file-wide transformations?
