# AE starter UI cleanup: change ledger

Goal. Drew asked for one coordinated cleanup of the Agentic Engineering starter from the two Fable 5.2 reviews. This leaf owns the Delivery view half. PR #123 stays open and unmerged. Nothing was pushed, installed, or merged.

- **Commit.** `c3b2d4c1c672352cab316ab1342ef585567898b7` on `t3/ae-starter-ui-cleanup`, one commit on base `aa830a2b44544a3374ea33909973280df31da8ec`.
- **Ran on.** Claude Code in T3 Code, `claude-fable-5-2`, no subagents.
- **Files.** `delivery/{delivery.css,presentation.tsx,stage.tsx,spec.tsx,effort.tsx,panel.tsx,relationships.tsx,evidence.tsx}` under `pkg/app/cli/init/templates/starters/agentic-engineering/module/views`, plus new `web/tests/e2e-agentic-engineering/fable-cleanup.spec.ts`. `home.tsx` needed no edit. No root-owned file, generated copy, or design sidecar was touched.

## Root must do before this batch is green

1. **Land the lib contracts.** `stage.tsx` imports `RowSort` and calls `groupResult(stage, portfolio, sort)`, `promptFor(stage, portfolio, keys, sort)`, and `eligibility(row, "select")`. Those are the only six type errors left in the template (measured, see Checks).
2. **Land `@rhizome/kit/markdown`** in the kit build and `pkg/viewscript/importmap.json`. `evidence.tsx` imports `Markdown` and `convertWikilinks` from it. Until then `TestAgenticEngineeringDeliveryFreshInstall`, `...UpdateManagedInstall`, and `...EjectPreservesLocalExperience` fail with `custom_view_import_unmapped` and nothing else (measured).
3. **Add the new spec to the Playwright project.** `web/playwright.agentic-engineering.config.ts` matches `/(?:delivery|constraints)\.spec\.ts/` for the `mixed` project, so `fable-cleanup.spec.ts` is collected by no project today. It needs the `mixed` fixture.
4. **Decide the panel stage channel** (next section).
5. **Run the installed suite and recapture.** No browser ran here. Every visual claim below is read from code.

## Finding: the panel cannot learn the stage from its location

VD-6 fix 5 assumed the panel's `useViewLocation()` reports the page's view id and params. Source says otherwise (verified by reading):

- `web/src/components/activePanel.ts:95-99` gives a panel on a stage page the context `{ kind: "standalone", view: "agentic-engineering.stage" }`.
- `web/src/components/ExperienceFrameLayer.tsx:268` reads a panel's params from an empty query, so the page's `stage` param never reaches it.
- `location.viewId` is the panel view's own id.

So the panel can know a stage page is showing, and not which stage. I shipped a default so the review finding is closed inside my files: the stage page writes its stage to `sessionStorage` while it is the active page and removes it when it leaves, and the panel reads it through a `storage` listener (`useAnnounceStage`, `useAnnouncedStage` in `presentation.tsx`, marked `ponytail:`). With storage unavailable the panel marks no stage, never a wrong one. This is unverified in a browser. The cleaner fix is a host change that passes the active page's params in the panel invocation. Tell me to drop the storage channel if you would rather make that change, or to leave the panel unmarked on stage pages.

## What changed

| Item | Change |
| --- | --- |
| VD-1 | `.rz-facts` inside a row, a select row, or a record header drops its own rule and margin. |
| VD-2 | `.ae-page .rz-section:not(:first-child)` gets 22px. The checklist sentence moved into the Closure checklist section, word for word. "Clear selection" moved beside "Select all" in a list toolbar. |
| VD-3 | New `Notice` component: one tinted band with the house ring or risk disc as an inline SVG. Used for read failures, partial reads, changed selection, incomplete scope, relationship errors, field issues, and unresolved subjects. `ReadState` merges its lines into one notice. Existing `role="alert"` and `role="status"` elements keep their roles. `error.message` moved into an "Error detail" disclosure. |
| VD-4 | `.ae-path` mono evidence class on every path, id, locator, and authored selector. `TypeName` shows the kit type label with the identifier as `title`, and falls back to the mono identifier while the label loads or fails. Section headings are the kit's 15/600 again. Record titles are weight 500. Relationship rows are a two-column `dl` with the same label words and colons. 13px floor on map labels. |
| VD-5 | `RecordedStatus` renders the recorded word through kit `StatusMark` with `values={[]}`. Used in row facts, both record headers, and story summaries, where status and criteria count sit in fixed columns. |
| VD-6 | Panel flattened into sibling sections with 28px rows, a chrome-tinted find field, label left and count right in the map, a `--chrome-text` focus ring, and `aria-current="page"` on the current stage link. |
| VD-7 | `unit()` covers criteria, records, and visits. Plan labels are capitalised on screen, and their `aria-label` text is unchanged. |
| VD-8 | Read state shares the page column and left edge. `--sunk` is `--surface-sunk`. Every summary leads with `DisclosureIcon`, rotated by `details[open]`. |
| O1, O7 | Child stories indent 24px under their parent. Duplicate `.ae-panel .ae-link`, duplicate `.ae-criteria`, both sibling-margin rules, and the local focus rule that restated the kit's are gone. |
| O3, O5, O6 | Refresh sits inline after the saved-records line. Map cards stretch to one height. The current stage uses the `--selection` fill, not a focus-like outline. |
| IQ-1 view half | A review row with `row.selected` lists each record as a `RecordLink` plus its mono locator, under "Pending selected records found in this read". No count or coverage claim. |
| US2 sort | Kit `Segmented` labelled "Sort records": Source order (default), Title A to Z, Title Z to A, held in `useState`. The same `sort` goes to `groupResult` and `promptFor`. |
| Eligibility | The local `selectable` function is deleted. Rows call `eligibility(row, "select")`. |

Skipped as instructed: O2, O4, and host changes. `attributionFor`, `sourceRoles`, `selectionFor`, and `linkable` have no new callers.

## Behaviour changes to know about

- Stage "Select all" and "Clear selection" moved from the section heading and the aside into the list toolbar.
- `aria-current="page"` moved from the stage card `div` to its link.
- A source locator with no fragment now prints `path`, not `path#` (relationships rows).
- Two unreachable lines in `spec.tsx` were removed: the invalid-route and missing-node messages inside the `query.data` branch, which that branch can never show.
- `delivery/README.md` is root-owned and does not mention the stage channel.

## Checks

- **Formatter and lint.** `make web-lint` passed in the pre-commit hook on 897 files, including the new spec (measured). Template views sit outside the web lint scope.
- **Types.** `tsc --noEmit` over the template folder and the new spec with a temporary config: 12 errors, all from the pending contracts. With a throwaway `@rhizome/kit/markdown` alias, 6 remain, all in `stage.tsx` (measured). The config and alias were deleted.
- **Go.** `go test ./pkg/app/cli/init -run TestAgenticEngineeringDelivery`: the library test passes, and the three install tests fail only on the unmapped markdown import (measured).
- **Tooling note.** This worktree has no `web/node_modules`. I linked the primary checkout's for the checks and the hook, then removed the link. Lockfiles agree on `oxlint` 1.81.0, `oxfmt` 0.66.0, and `@oxlint/plugins` 1.81.0, and differ only in Tailwind packages.
- **Not run.** The installed Playwright suites, any build, any screenshot. The four new tests have never executed.

## New tests (unrun)

1. Sort reorders visible story rows and the composed prompt together, and Copy stays enabled.
2. Record review names the pending selected record: the mixed fixture's US4 is flipped to `ready` in GraphQL responses, the same way `constraints.spec.ts` flips US3.
3. The panel marks Ready, then Active after a panel click, and nothing on home.
4. A failed read keeps "Read failed." and Retry visible, with the raw error behind the disclosure.

Existing assertions I checked by reading still have their targets: exact `h1`, the `3 stories in 2 specs` text, `Governing-specs metadata:` and both frozen labels, `Open implementation plan:`, the Recent list, the one "Refresh records" button, and the `role="status"` partial line.
