---
type: ReferenceDoc
summary: "Evidence-backed review of the Notes editing subsystem across schema metadata, edit replay, persistence, performance, interaction design, accessibility, and code ownership."
reference-kind: analysis
review-status: reviewed
---

# Note editing subsystem review, 2026-09-11

## Scope

This review covered the complete Notes editing path on `origin/main` at `ce69a1fe`: public GraphQL workspace reads, the browser adapter, edit-session persistence and staging, ontology replay and file publication, modified-file review, structured field controls, narrative editing, Source mode, and the visible save and conflict experience.

Evidence came from source inspection, focused frontend and Go tests, browser interaction with a live server, intercepted request measurements, and temporary-vault probes of replay and publication behavior. The review found 23 actionable issues. The existing shell is strong enough to keep, but the editing path is still generic, request-heavy, and unsafe at several concurrency boundaries.

## Critical findings

1. Public GraphQL workspace fields omit authoritative type, enum, list, relation-target, nullability, and edit-capability metadata. The browser reconstructs metadata from assessment findings, which are empty for healthy notes, so most typed values fall back to plain text.
2. Pressing Escape in a property editor stages the canceled value, and pressing Enter can stage the same value twice.
3. Navigating away while the first stage request is pending can lose the local draft even when the server later accepts the operation.
4. A commit conflict collapses Edit mode and hides the conflict details and recovery actions.
5. Modified-file discard compares a bare file path with operation paths that may include fragments, so per-file discard can silently fail.
6. Narrative typing stages every keystroke, resends the full operation history, replays the session, and refreshes the workspace. A 60-character edit produced 120 requests; on a 4,100-character paragraph it moved about 49 MB and retained 61 operations.
7. An empty note body has no narrative insertion affordance, and Source mode remains read-only.

## Replay and publication findings

8. Reorder accepts a requested sequence with duplicate members, duplicating one member and deleting another. Reorder must prove an exact permutation.
9. Add-embedded-node accepts a block ID already owned by another node.
10. A file can change after replay planning and before rename; the final rename overwrites that change.
11. Session recovery preserves only a base hash, then treats current disk content as the restored base. Collection-order drift can therefore be overwritten after restart.
12. Equivalent paths such as `note.md` and `./note.md` are grouped separately and can overwrite one another.
13. File publication changes an existing file's mode to `0600`.
14. A valid no-op commit is mapped to an HTTP conflict because the result is neither applied nor explicitly successful.
15. Publication uses a fixed backup name, can remove a pre-existing backup, ignores rollback errors, lacks crash recovery, leaks prepared temp files after some failures, and coordinates only within one session.

## Schema and source-preservation findings

16. The property editor supports only enum, date, and text. It lacks Boolean, integer, float, date-time, list, relation, identifier, and read-only computed-field controls. List-valued enums can collapse to the first value.
17. Invalid enum, number, date, and Boolean values can be committed and discovered only by later validation. Editing must allow incomplete local input while blocking newly invalid changed values.
18. The current replay policy overwrites an external edit to the same field. The safer contract is automatic rebase for unrelated drift and a typed field conflict for targeted same-field drift.
19. A frontmatter field edit re-encodes the full YAML document and can reformat unrelated nested data, comments, quoting, and multiline content.

## Experience and code-quality findings

20. The editing toolbar and review surface duplicate Save/Discard actions with different vocabulary and a large review header.
21. Property controls wrap densely, identity fields repeat, and the center pane and Info rail duplicate the same property content.
22. Draft, staging, saving, conflict, error, and completion feedback are incomplete. Status text is not announced to assistive technology, and there is no edit undo/redo path.
23. Save contrast is below the desired control contrast, the edit indicator animates without a reduced-motion fallback, and enum colors are hard-coded.

The implementation also contains a registered but unreachable inline-field editor, duplicated locator handling, discarded async failures, and oversized orchestration modules. `OntologyNotePane.tsx`, `edit_session.go`, and `ontology_sessions.go` should be split along field rendering, replay, persistence, and publication responsibilities as those contracts are repaired.

## Product direction

- Make the public node workspace the authoritative source of field capabilities.
- Keep keystrokes in a route-independent local draft and coalesce them into compact semantic operations after an idle interval or explicit field completion.
- Use one shared field editor registry in Notes and configured tables.
- Preserve one original base snapshot per touched file and stable operation IDs across browser recovery.
- Detect same-target drift and present Keep mine, Keep current, and Review actions while rebasing unrelated drift automatically.
- Validate only changed values at the write boundary so pre-existing invalid data remains repairable.
- Publish through canonical paths, a vault-wide write coordinator, a final fingerprint check, unique backups, and a recoverable journal.
- Treat Source mode as a deliberate, fingerprint-checked edit-session escape hatch with graph-sensitive changes routed to safe operations.
- Consolidate editing into one compact action bar and one review surface with clear lifecycle states, accessible announcements, and keyboard support.

## Verification baseline

Before implementation, the current-main build passed, 42 focused frontend tests passed across eight files, and seven focused ontology replay tests passed. The browser and temporary-vault probes above provide the red behavior baseline. Phone layouts, IME composition, very large vaults, and process termination during publication were not exhaustively tested and belong in the implementation verification matrix.
