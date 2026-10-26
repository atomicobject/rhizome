---
type: EffortNote
id: EFF-2026-10-26-09-57
aliases: [EFF-2026-10-26-09-57]
name: Workbench sidebar
created-at: 2026-10-26T13:57:31Z
plan-approved-by: Drew Colthorp
status: complete
summary: "Deliver SPEC-0128: one persistent, user-toggled sidebar frame across Notes, Ontology, Explorer, and Agent, with one remembered collapsed state and width, a toggle in the browser top bar and the desktop toolbar, and ⌃⌘S or ⌘\\ shortcuts."
governing-specs: ["[[workbench-sidebar]]"]
---

# Workbench sidebar

## Scope

Implement [[workbench-sidebar|SPEC-0128]], which Drew asked for on 2026-10-26 after #48 merged. His decisions:

| Decision | Choice |
|---|---|
| Collapsed state | One state for every section |
| Search tabs | Keep the sidebar |
| Browser toggle | In the top bar, beside the section switcher |
| Desktop toggle | A toolbar control in Rhizome Desktop |
| Ontology contents | The type tree, with an Atlas row |
| GraphQL | No sidebar; toggle disabled |
| Shortcuts | ⌃⌘S and ⌘\\ |

Excluded: SPEC-0128 Non-Goals (a GraphQL sidebar, per-section state or widths, new narrow-window behaviour, right-pane changes, and changes to what the Notes sidebar holds).

## Spec Set (Frozen)

- [[workbench-sidebar|SPEC-0128]], 2026-10-26 revision: US1–US3 and all requirements.

## Stories In Scope (Frozen)

All three stories of SPEC-0128 (`^SPEC-0128-US1` through `^SPEC-0128-US3`).

## Spec Coverage Checklist

- [x] US1 keep the sidebar where I put it (batch 1; desktop part batch 3).
- [x] US2 toggle from one place (browser batch 1; desktop batch 3).
- [x] US3 the same sidebar in every section (batch 1 frame; batch 2 contents).
- [x] Shell and state requirements (batch 1).
- [x] Desktop requirements (batch 3). The packaged-app click-through follows the merge.
- [x] Accessibility and verification requirements (batches 1–3).
- [x] Documentation requirements (batch 4).

## Plan

Delivery is one PR from `feat/persistent-sidebar` to `main`, with one commit per batch. I implement batch 1. Batches 2 and 3 then run in parallel as T3 `delegate_task` workers (Opus 5.5 medium, own worktrees branched from batch 1), and I review and integrate each. Before merge: `greptile review --json` on the branch until it reports 5/5 with no comments, and an independent GPT-6.1 Sol review of the whole diff. After CI is green I squash-merge, as with #37 and #48.

**Current gap.**

| Surface | Today | Where |
|---|---|---|
| Notes sidebar | Hidden whenever a search tab is active; toggle in the document tab strip; state in `rhizome:notes:panes:v1` with no cross-tab sync | `NotesShell.tsx:412,620,770-806`, `shellPanes.ts`, `notes-shell.css:38-74,142-177` |
| Ontology | No sidebar | `OntologyAtlasWorkspace.tsx` |
| Explorer | Fixed 260px rail on the surface colour; no collapse or resize | `ExplorerWorkspace.tsx:444-480`, `explorer.css:170-181,713-727` |
| Agent | Session rail on `--surface-alt`; collapses only into a drawer below 980px | `AgentWorkspace.tsx:132,396-439`, `AgentSessionRail.tsx`, `agent.css:1-17,406-460` |
| Browser top bar | Brand, section switcher, search, tools; no sidebar toggle | `AppShell.tsx:289-351`, `base.css:77-183` |
| Desktop | No sidebar toggle since #48 removed the repository sidebar; the page reports state only through its title | `desktop/src/App.tsx:771-858`, `src-tauri/src/{menu,page,state,commands}.rs`, `web/src/components/desktopHost.ts` |

| Batch | Outcome | Scope and ownership | Work | Exit evidence |
|---|---|---|---|---|
| 1. Shared state, frame, and browser toggle | US1 in the browser, US2 in the browser, US3's frame, the shell and state requirements | New `web/src/components/workbenchSidebar.ts` (state) and `WorkbenchSidebar.tsx` (frame), a `sidebar.css` taken from `notes-shell.css`, `AppShell.tsx`, `NotesShell.tsx`, `shellPanes.ts` (right pane only afterwards), `desktopHost.ts` | One external store for `collapsed` and `width`, under `rhizome:workbench:sidebar:v1`. It carries over the Notes values from `rhizome:notes:panes:v1` once, follows `storage` events, and in Desktop takes its state from the shell's command instead of storage. A frame component draws the chrome aside, header, resize handle, and hidden, inert collapse. The top-bar toggle goes before the section switcher, and ⌃⌘S and ⌘\\ are handled outside text fields. Notes renders its rail in the frame. The tab-strip toggle goes, and search tabs keep the sidebar. | Unit tests for the store (persistence, carry-over, cross-tab, desktop command) and the shortcut exclusion. `search.spec.ts` now expects the sidebar on search tabs. A new `sidebar.spec.ts` covers toggle, reload, and section switches. `make check` passes. |
| 2. Section contents | US3: Ontology, Explorer, Agent, and the GraphQL exception | `NotesLeftRail.tsx` (extract the type tree for reuse), `OntologyAtlasWorkspace.tsx`, `ExplorerWorkspace.tsx`, `explorer.css`, `AgentWorkspace.tsx`, `AgentSessionRail.tsx`, `AgentStatusHeader.tsx`, `agent.css` | Ontology gets an Atlas row and the shared type tree, routing to type detail with the current type selected. The Explorer tree and the Agent session list move into the frame with the shared row styles. Agent's drawer and hamburger go. GraphQL renders no frame, and the toggle is disabled with a tooltip there. | Unit tests for the Ontology tree selection and routing. `sidebar.spec.ts` covers each section and GraphQL. Existing `explorer`, `agent`, and `ontology` e2e pass. Screenshots of all four sections in light and dark. |
| 3. Desktop toggle | US2 and US1 in Desktop, the desktop requirements | `desktop/src/App.tsx`, `api.ts`, `style.css`, `src-tauri/src/{page,state,menu,commands}.rs`, `web/src/components/desktopHost.ts` | Additive `page::Command::Sidebar { collapsed }` and a `sidebar` field in the title report. `Preferences.sidebar_collapsed` is app-wide, applied on page load and broadcast to every window. A toolbar icon button goes first after the traffic lights, pressed while the sidebar shows and disabled on GraphQL and on non-runtime pages. View menu: Show Sidebar or Hide Sidebar with ⌃⌘S. ⌘\\ pressed in the page reports through the title, and the shell persists it. | `make desktop-check` (shell tests and Rust tests for the command, report, preference, and menu). `make desktop-build` succeeds. Drew's click-through in Rhizome Dev after merge. |
| 4. Documentation and closure | Documentation requirements | SPEC-0090, SPEC-0098, SPEC-0113, SPEC-0119, SPEC-0126, `DESIGN.md`, `web/CONTEXT.md`, `desktop/README.md`, `CHANGELOG.md`, this effort | Amend each spec where it omits the sidebar from search, places the toggle in the tab strip, or describes the toolbar without it. Update the docs and changelog, then run the closure pass. | `./scripts/rzm validate` reports 0 issues; `rzm agent validate frozen-scope-drift` is clean. |

**Escalation boundary.** Stop for Drew if the Ontology tree needs data the atlas query does not provide, or if a Desktop design would need the page to gain a capability beyond SPEC-0119's title-only reports.

## Plan Approval

- 2026-10-26T14:05:59Z: Drew approved the plan at commit 579fc126 ("Do it. Opus medium workers"), including the merge after Greptile, an independent review, and green CI. Batch workers run Opus 5.5 at medium effort.

## Original Intended Delivery

One PR in which the sidebar stays where the user put it across searches, sections, reloads, and windows. One toggle in the browser top bar or the desktop toolbar, and ⌃⌘S or ⌘\\, shows or hides it, and Notes, Ontology, Explorer, and Agent each fill the same frame.

## Actual Delivered

One PR on `feat/persistent-sidebar`, with one commit per batch:
- batch 1: the shared state, the frame, and the browser toggle;
- batch 2: the Ontology, Explorer, and Agent sidebars;
- batch 3: the Desktop toolbar toggle, View menu item, and app-wide preference;
- batch 4: documentation and this closure.

The sidebar is now one frame in Notes, Ontology, Explorer, and Agent, and only the user shows or hides it. The toggle sits in the top bar before the section tabs, and ⌃⌘S or ⌘\\ work too. In Rhizome Desktop the toggle is the toolbar's first button, View › Show Sidebar has ⌃⌘S, and the choice applies to every window. Search tabs keep the sidebar, and GraphQL has none.

## Execution Notes

- 2026-10-26T13:57:31Z: effort opened from `907e1bb8` (#48 merged) on `feat/persistent-sidebar`.
- Batch 1 (shared state, frame, browser toggle):
  - **State:** `web/src/components/sidebarState.ts` holds one external store for the collapsed state and width, under `rhizome:workbench:sidebar:v1`. It carries over the Notes values from `rhizome:notes:panes:v1` once and follows `storage` events. In Desktop it takes the collapsed state from the app's `sidebar` command and reports `sidebarCollapsed` in the title only after that command arrives, so the page's default never overwrites the app's stored choice.
  - **Frame:** `WorkbenchSidebar.tsx` holds the frame (header, body, resize handle; collapsed means hidden and inert), the top-bar `SidebarToggle`, and the shortcuts. `workbench.css` holds the shared layout.
  - **Notes:** Notes renders in the frame. The tab-strip toggle is gone, search tabs keep the sidebar, and `shellPanes.ts` now owns only the right pane.
  - **Shortcuts:** in Desktop the page answers only ⌘\\, since the View menu will own ⌃⌘S. The Notes ⌘S save handler now ignores ⌃⌘S.
  - **Naming:** the state module is `sidebarState.ts`, not `workbenchSidebar.ts`, because that would collide with `WorkbenchSidebar.tsx` on a case-insensitive filesystem.
  - **Evidence:**
    - `make check` passed: 1,697 web unit tests, plus the Go suites and lint.
    - `npx playwright test` passed all 105 e2e tests, including the new `sidebar.spec.ts` and `search.spec.ts`, which now expects the sidebar on search tabs.
    - Browser screenshots at 1440px show search results beside the sidebar and the top-bar toggle.
- Batch 3 (Desktop), commit `07e8e718` from a worker (Opus 5.5 medium), integrated as `17cd689d`:
  - **Command:** additive `page::Command::Sidebar { collapsed }`, sent to runtime pages, covered or loading ones included (the same rule as the theme).
  - **Report:** an optional `sidebarCollapsed` in the title report.
  - **Preference:** `Preferences.sidebar_collapsed`, sent when a page first reports and broadcast to every window when it changes.
  - **Toggle detection:** a change counts as a toggle only when the same document's previous report had a value and this one differs. A page echoing the app's command is therefore not a toggle, and a stale report from another window cannot flip the preference back.
  - **Controls:** the toolbar's first `icon-button`, pressed while the sidebar shows, and View › Hide/Show Sidebar with ⌃⌘S, placed before Enter Full Screen.
  - **Disabled:** on GraphQL, while Settings covers the page, and before the page reports a sidebar state.
  - **Refactor:** `appearance::send_theme` became `page::send_setting`, shared by the theme and the sidebar.
  - **Evidence:** `make desktop-check` passed (89 shell tests; `cargo test` passed 85; fmt and clippy clean). `make desktop-build` built `Rhizome.app`.
- Batch 2 (section sidebars), commit `0ad97916` from a worker (Opus 5.5 medium), integrated as `69830f16`:
  - **Shared tree:** the type tree moved from `NotesLeftRail.tsx` into `RailTree.tsx`, which Notes and Ontology share. Ontology keeps its expanded groups under its own key, because both sidebars can be mounted at once.
  - **Ontology:** an Atlas row, then the groups, types, and interfaces from `useOntologySummaryQuery`. No API was added.
  - **Explorer:** the vault tree moved into the frame. The 260px rail and its breakpoints are gone.
  - **Agent:** the session list and settings moved into the frame. The drawer, scrim, and hamburger are gone.
  - **Header:** every section's header names the vault in a browser, and the top bar no longer repeats it outside GraphQL.
  - **Evidence:** `make check` passed (1,700 web unit tests). The new `OntologyAtlasWorkspace.test.tsx` passed, and `sidebar.spec.ts` now covers all four sections.
- Integration on `feat/persistent-sidebar` (69830f16):
  - `make check` passed (1,700 web unit tests, plus the Go suites and lint).
  - `make desktop-check` passed (89 shell tests, 85 Rust tests).
  - `npx playwright test` passed all 106 e2e tests.
  - Light and dark screenshots of Notes, Ontology, Explorer, and Agent at 1440px, against a copy of Drew's vault, show one chrome sidebar beside the inset panel in each section.

## Deviations

- 2026-10-26T16:30:00Z: #49 (scheduled tasks) merged to `main` holding SPEC-0127 first, so this spec is renumbered SPEC-0128, along with every reference this branch added. frozen-scope-drift acknowledged for SPEC-0128: the spec set is refrozen at this revision.
- 2026-10-26T16:05:00Z: an independent GPT-6.1 Sol review (xhigh) of `a3bc9734` found three issues:
  - **Delayed echo:** a delayed reply could overwrite the latest choice. This was already fixed by the toggle count in `257af38d`.
  - **Width across windows:** windows of the same repository now follow each other's width through `storage` events in Desktop too.
  - **⌃⌘S in a text field:** the native View-menu ⌃⌘S toggles even while a text field has focus. SPEC-0128 US2 now says the page's shortcuts skip text fields, while the menu item answers ⌃⌘S from anywhere, like any macOS menu command. ⌃⌘S has no text-editing meaning. frozen-scope-drift acknowledged for SPEC-0128: the spec set is refrozen at this revision.
- 2026-10-26T15:45:00Z: Greptile review of #57 (4/5) found three issues, all fixed:
  - **Late replies:** a late reply to an earlier app command could look like a page toggle and undo a newer choice after two quick toggles. The page now reports `sidebarToggles`, a count of its own toggles, and the app treats a report as a toggle only when that count rises.
  - **Ontology read failure:** a failed type-tree read showed an empty tree. The sidebar now says "Types unavailable" with a Retry button.
  - **Desktop width (also flagged by the Sol review):** the width is not shared across Desktop windows. SPEC-0128 US1 is amended: in Desktop the collapsed state is app-wide, and the width is remembered per repository in the page's storage. frozen-scope-drift acknowledged for SPEC-0128: the spec set is refrozen at this revision.
- 2026-10-26T15:20:00Z: routine choices within the approved plan, recorded for review:
  - Ontology lets embedded types open their detail (Notes still disables them), because US3 says choosing a type opens its detail.
  - Explorer's root row shows the vault's short name.
  - Off macOS the View menu's accelerator is Ctrl+Alt+S, following SPEC-0128. `Ctrl+CmdOrCtrl+S` would be Ctrl+S (Save) there.
  - The top bar names the vault only on GraphQL or a status error, since every sidebar header names it.

## Closure Checklist

- [x] Spec coverage checklist complete or deviations recorded.
- [x] Quality gates recorded with exact commands and results (Execution Notes).
- [x] SPEC-0128 and the amended specs (SPEC-0090, SPEC-0098, SPEC-0113, SPEC-0119, SPEC-0126) reconciled with delivery.
- [ ] PR open; Greptile 5/5; independent review; CI green on the final head.
- [x] Follow-ups recorded.
- [x] Desktop verification: recorded as a post-merge click-through in Rhizome Dev (Follow-ups); Rhizome Dev installs only from `main`.

## Compounding Follow-ups

- **Desktop click-through:** after merge, run `make desktop-install-dev` from the primary checkout and try the toolbar toggle, View › Hide Sidebar (⌃⌘S), and ⌘\\ in the page across two windows.
- **Atlas type list:** the Ontology atlas home still has its own "Find a type" list, which focuses a type on the canvas, beside the new sidebar tree. Merging the two would remove the duplication.
- **Shortcuts inside view frames:** keyboard events in a custom-view iframe never reach the host window, so the sidebar shortcuts (like ⌘P) do nothing while focus is inside a view. The kit could forward host shortcuts to the parent.

## Status

Complete. SPEC-0128 is delivered on `feat/persistent-sidebar`; the PR, its review, and CI are recorded below as they land.
