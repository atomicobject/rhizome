---
type: EffortNote
id: EFF-2026-10-02-12-13
name: View manual ordering
created-at: 2026-10-02T16:13:02Z
status: active
summary: Drag rows, cards, and kanban cards into order in views sorted by an editable number, enum, or boolean field, staging ordinary field updates.
aliases:
  - EFF-2026-10-02-12-13
---

# View manual ordering

## Scope

Deliver [[view-manual-ordering|SPEC-0108]] in the web workspace: pointer and keyboard reordering in table, card, and kanban variants of configured views whose sort leads with an editable `int`, `real`, `enum`, or `bool` field. Writes go through the existing edit session. No server, view YAML, ontology schema, or persisted data changes. Reorder APIs for agents and custom view apps are outside this slice.

## Spec Set (Frozen)

- [[view-manual-ordering|SPEC-0108]], as drafted in this branch from `3e2a5d3302b30237ccd68b42378ad7b29c9f771e`.
- [[configured-view-engine-and-repo-config|SPEC-0058]] as context only. Its kanban rules are unchanged: a column move still stages one field update; placement updates are additional ops defined by SPEC-0108.

## Stories In Scope (Frozen)

SPEC-0108 is requirements-only. All of its requirements are in scope.

## Spec Coverage Checklist

- [x] Orderable detection, drag handle, and truncated read-only state.
- [x] Gap anchors, cross-group drops, and no-op drops.
- [x] Placement for number, enum, and boolean keys, missing values, and renumbering.
- [x] One `stageOps` call per drop, transient placement, failure alert, and announcement.
- [x] Alt+Up and Alt+Down.
- [x] Unit, component, and end-to-end tests.

## Plan

### Batch 1: placement

- Add a pure module `web/src/components/viewOrdering.ts`: `orderableKeys(sort, capabilities)` and `placeItem({ rows, from, gap, keys })` returning `setField` ops built with the existing `editOpForCell`.
- Unit tests for every case SPEC-0108 lists under Verification.
- Exit: `npm test` for the module passes; `make check-fast` passes.

### Batch 2: table and cards

- Drag handle, gap indicator, and drop handling in `ConfiguredViewTableRow` and the card group renderer, scoped per leaf group, with cross-group drops when the group field is editable.
- Alt+Up and Alt+Down on focused rows and cards.
- Transient placement bridge while re-executing, reusing the board's `placeStagedCards` and `compareRowsBySort` approach; read staged values through `src/staging/`.
- Read-only states for non-orderable and truncated results.
- Component tests beside `ConfiguredView.board.test.tsx`.

### Batch 3: kanban

- Positional gaps inside columns; a cross-column drop stages the column op plus placement ops in one call. The existing move menu keeps working.
- Extend `ConfiguredView.board.test.tsx`.

### Batch 4: documentation, verification, dogfood

- Update `web/CONTEXT.md` (renderers may stage placement ops), `pkg/app/views/CONTEXT.md` if its renderer rule changes, and the changelog.
- Extend `web/tests/e2e/view-variants.spec.ts` with a reorder.
- Gates: `make check`, `make web-e2e`, `./scripts/rzm validate`, `./scripts/rzm validate frozen-scope-drift`.
- Dogfood the build in a disposable copy of Drew's vault on the IP opportunities view; then open a PR.

### Authorization

Drew asked on 2026-10-02 for drag-and-drop ordering that works for views ordered by an enum or a number, plus other sensible cases. After reviewing the plan, Drew approved it in chat at 2026-10-02T16:15:53Z: "OK. Build it and run it in the vault so I can try it out interactively." No current user is configured in this checkout, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

Drew drags IP opportunities into order within their stage groups and moves them between priorities, and Save writes the new `rank` and `priority` values.

## Actual Delivered

Tables, cards, and boards reorder by pointer drag and Alt+Arrow keys whenever the effective sort leads with an editable `int`, `real`, `enum`, or `bool` field, including a sort chosen by clicking a column header. Placement lives in `web/src/components/viewOrdering.ts`; drag, drop, keyboard, staging, and the transient re-sort live in `useViewReorder.ts`, shared by all three renderers. A drop stages one deduplicated `stageOps` call of witnessed `setField` ops. Tables and card grids accept drops into other groups when the group field has a safe enum, boolean, or single-node edit; dropping on a table group header places the item first in that group. Boards place cards inside a column and across columns; Alt+Arrow keys stay within a column. Truncated, paged, or locally filtered results are read-only, with an explanation on the table handle. No server, view YAML, or schema changes.

## Execution Notes

- Batch 1: `npx vitest run src/components/viewOrdering.test.ts` passed 12 placement tests.
- Batches 2 and 3: `src/components/ConfiguredView.reorder.test.tsx` covers table drop and keyboard, truncated and non-orderable tables, card drop and keyboard, and board drop and keyboard; all 681 component tests pass under `npx vitest run src/components`.
- Batch 4: `npx playwright test tests/e2e/view-variants.spec.ts` passed both tests, including a new keyboard reorder after sorting the fixture by its checkbox.
- `npm run lint` and `npm run typecheck` pass. `make check` passes everything except `TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates`, the pre-existing identifier-reconciliation failure recorded in EFF-2026-09-29-14-46; it is unrelated to this web-only change.
- `rzm validate` and `rzm validate frozen-scope-drift` report zero issues.
- Dogfood: the branch build served a disposable copy of Drew's vault. In the IP opportunities view, a pointer drop renumbered two unranked rows, Alt+Down moved a row from Captured into Pursuing (staging stage and rank), a card drop reordered the card grid, and a board drop placed a card at a position in another column. Drew then tried it in the same copy.
- Independent review (Fable advisor) confirmed the placement math and found a header drop that placed items last while showing first, Alt+Left/Right also moving grid focus, possible duplicate op ids when the group field is a sort key, and a missing `total` truncation check. All four were fixed with tests where observable.
- PR review ([atomicobject/rhizome#1](https://github.com/atomicobject/rhizome/pull/1)), Greptile round 1 and Drew's trial: the client re-sort now compares values as the server's `sortRows` does (indexed enums by normalized text, booleans unindexed with missing first ascending) instead of schema order; gaps whose drop stages nothing are not offered, and Alt+Arrow skips past them; Int ranks beyond the safe integer range and Real steps that would not change a value renumber; a staged cross-group move shows in its new table or card group; a board drop on another column's background places the card first there; and a card dropped on itself no longer jumps to the end of its group. The CI e2e failure came from `app.spec.ts` saving the fixture's second item as done, which left both checkbox values equal; the reorder e2e now sorts by status and reads the values it starts from. Greptile round 2 found that placement still compared raw enum text, so values the server sorts as equal (`high`, ` High `) could offer a gap; placement now uses the server-order comparison for equality too, including when renumbering decides which rows share the earlier keys (round 3). Gates: `npx vitest run src/components` (691 tests), `npm run lint`, `npm run typecheck`, and the full `npx playwright test` suite.
- Worktree created with the T3 Code setup script at `~/.t3/scripts/rhizome-worktree-setup.sh`; binaries and index copied from the main checkout.

## Deviations

- PR review refined SPEC-0108 rather than its scope: `int` and `real` sort keys must be indexed to be orderable, enums compare as the server sorts them rather than in schema order, gaps that would stage nothing are not offered, board column-background drops place the card first, and staged cross-group moves show in their new group before re-execution.
- 2026-10-03 (frozen-scope-drift acknowledged via [[2026-10-03-07-07-type-view-presentation|EFF-2026-10-03-07-07]]) SPEC-0058 gained `mount.replaceGenerated` for native type and interface mounts. This effort holds SPEC-0058 as context only, and its kanban rules are unchanged, so the scope is unaffected.

## Closure Checklist

- [x] Plan approved.
- [x] Batches 1 to 4 complete with recorded evidence.
- [x] Dogfooded on the IP opportunities view.
- [x] PR opened and linked: [atomicobject/rhizome#1](https://github.com/atomicobject/rhizome/pull/1).

## Compounding Follow-ups

None yet.

## Status

Active. Implementation, checks, review, and dogfood are complete on `drag-reorder`; waiting on Drew's trial feedback and the PR merge.
