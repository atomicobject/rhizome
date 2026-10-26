---
type: EffortNote
id: EFF-2026-10-25-15-15
aliases: [EFF-2026-10-25-15-15]
name: Workbench redesign
created-at: 2026-10-25T19:15:41Z
status: complete
summary: "Deliver SPEC-0126: chrome themes with light and dark appearance in the browser and Rhizome Desktop, Inter and a new type scale, a calmer view header, a table column policy, resizable panels with one right pane, and fuzzy finders for notes and files, repositories, and worktrees."
governing-specs: ["[[workbench-redesign]]"]
---

# Workbench redesign

## Scope

Implement [[workbench-redesign|SPEC-0126]], the partial redesign Drew chose on 2026-10-25 after the Impeccable critique (`.impeccable/critique/2026-10-25T17-51-30Z__web-src.md`, 24/40) and three rounds of comps (`.impeccable/mocks/workbench-comps/`, current `comps3.html`). In Drew's decisions, "the switcher" means the separate ⌘O repository and ⇧⌘O worktree finders:

| Decision | Choice |
|---|---|
| UI typeface | Inter |
| Default theme | Graphite |
| Section navigation | Top tabs |
| Repository and worktree switching | ⌘O repository and ⇧⌘O worktree finders, replacing the sidebar |
| Desktop finder rendering | An overlay webview |
| Left-rail note list | Removed in favour of ⌘P |
| ⌘P scope | Notes and code files |
| Theme storage in Desktop | Owned by Desktop |
| ⌘P matching | fzf's algorithm over an in-memory catalog; FFF rejected |

Excluded: SPEC-0126 Non-Goals (touch layouts, custom theme colours, a multi-scope command palette, restructuring Board/Cards and the other workspaces beyond tokens and the shared header, identity work, commercial fonts, view YAML and preference contract changes beyond the additive finder endpoint).

## Spec Set (Frozen)

- [[workbench-redesign|SPEC-0126]], 2026-10-25 revision: US1–US11 and all requirements. It was refrozen on 2026-10-25 to add US11 and the note editing fidelity requirement (see Deviations).

## Stories In Scope (Frozen)

All eleven stories of SPEC-0126 (`^SPEC-0126-US1` through `^SPEC-0126-US11`).

## Spec Coverage Checklist

- [x] US1 themes and appearance; theme and token requirements (batch 1, #48; Graphite placeholder contrast fixed in batch 6).
- [x] US2 desktop window chrome; desktop requirements (#53). The packaged-app click-through in Rhizome Dev follows the merge (Deviations).
- [x] US3 view header and View popover (#52). The first-row criterion was amended to the shipped geometry (Deviations).
- [x] US4 table column policy and density (#52).
- [x] US5 resizable, persisted panels (#51).
- [x] US6 one right pane (#51; below 1200px the narrow rule wins, per Deviations).
- [x] US7 reading-surface width caps and board columns (#52, #54). Briefing and Overview now fill the panel (Deviations).
- [x] US8 ⌘P finder; finder requirements and latency target (#50; p95 9.1ms on 20,000 notes).
- [x] US9 ⌘O repository finder (#53).
- [x] US10 ⇧⌘O worktree finder (#53).
- [x] US11 note reader rhythm and edit mode; note editing fidelity requirement (#54).
- [x] Typography (Inter, scale, tabular figures) and contrast test (batch 1; labels reconciled in SPEC-0126).
- [x] Documentation: DESIGN.md and design.json, PRODUCT.md, SPEC-0090/0099/0112/0113/0119/0125 and diagrams amendments, desktop README, custom-views skill templates, changelog (batch 6).

## Plan

Delivery uses a parent branch (pushed as `feat/workbench-redesign`) with one parent PR to `main`. Each batch is a child PR into the parent, squash-merged so the parent carries one commit per batch, and Greptile reviews each batch against the parent. Drew merges the parent PR.

**Current gap.** The surfaces this effort changes, and where each lives:

| Surface | Today | Where |
|---|---|---|
| Tokens | Five near-identical warm off-whites; dark mode only through the media query; no theme or appearance setting anywhere | `web/src/tokens.css` |
| UI typeface | Barlow, including outside the tokens | Graph labels, mermaid, and `base.css` font features |
| Shell rails | Left rail 220px with a note list loaded in full; right rail 300px, separate from the Record rail portal; no pane resizing | `web/src/notes-shell.css`, `NotesLeftRail.tsx`, `NotesRightRail.tsx`, `recordRail.tsx` |
| View header | Four stacked bands (header, facets, presets, toolbar); the primary column shrinks to 10px | `ConfiguredView.tsx:269-338`, `configured-view.css:424-637` |
| Desktop | Fixed sidebar of repositories; toolbar on `--bg-tint`; no background colour or theme on windows; ⌘O bound to Add Repository | `desktop/src/App.tsx`, `RepositoryList.tsx`, `style.css`, `src-tauri/src/windows.rs`, `menu.rs`, `state.rs`, `page.rs` |

| Batch | Outcome | Scope and ownership | Work | Exit evidence |
|---|---|---|---|---|
| 1. Tokens, themes, appearance, Inter | US1 in the browser, typography requirements, contrast test | `web/src/tokens.css`, `base.css`, `fonts.css`, `web/index.html`, `web/kit/` (`boot.js`, `theme.css`), `web/src/lib/themeColors.ts`, GraphiQL wiring, `scripts/licenses/generate.py`, `web/public/fonts` | Content neutrals per appearance and chrome tokens per theme, with five presets. Appearance through `html[data-appearance]` with `prefers-color-scheme` as Match system. Theme through `html[data-theme]`, applied before first paint in the host and kit frames. Canvas caches and GraphiQL follow appearance changes. Inter variable fonts with OFL notices replace Barlow. A new type scale and weights, with tabular figures scoped to numbers. A browser Appearance control. A contrast unit test over every theme in both appearances. | `make check`, `make web-e2e`, license `--check` clean, contrast test green, and light and dark screenshots of the current screens for Drew's look before layout work builds on the tokens. |
| 2. Shell layout | US5, US6, the sidebar half of US8 | Notes shell, rails, focus regions, `NotesLeftRail`, `NotesRightRail`, `recordRail`, `notes-shell.css`; amends SPEC-0090 | Themed sidebar and inset content panel. Remove the rail note list and its eager query. Rail widths as custom properties, with keyboard-operable resize handles persisted per browser. One right pane hosting Record, Links, Outline, and Graph, closed when empty, toggled with ], auto-collapsing below 1200px. F6 regions kept. | Unit tests for resize (pointer and keyboard) and pane state; `rails.spec.ts`, `tabs.spec.ts`, and `pane-lifecycle.spec.ts` updated and passing; screenshots at 1100/1440/1920. |
| 3. View header, table policy, reading widths | US3, US4, US7 | `ConfiguredView*.tsx`, `ConfiguredViewToolbar.tsx`, `ViewSelector.tsx`, `ConfiguredViewTableModel.ts`, `configured-view.css`, `web/bundled-views/group/` | Title row and presentation tabs. View popover holding group, sort, a "1 line \| 2 lines" segmented control, columns, reset, and save. Presets move into Filter, and active filters show as chips. Facets band removed. Column policy: title minimums of 240 and 320px; fixed evidence widths; overflow columns move to the right pane in priority order with a footer note; table minimum width computed from real widths. Reading caps for Briefing and Overview and centred note measure; board columns 260–360px. Bundled-view font sizes on tokens. | Unit tests for column allocation and overflow at 1100/1440/1920 widths; `type-views`, `view-variants`, `unified-views`, and `group-views` e2e passing; first data row at or below 140px measured. |
| 4. ⌘P finder | US8 | New finder catalog and handler in `pkg/app/web` (search-subsystem and indexing-subsystem constraints loaded first), `openapi.yaml` and generated client, new `web/src/components/Finder*`, frecency in per-checkout user state | Catalog built from the index (notes with display titles and types, code files), refreshed on index change events. fzf algorithm scoring with typo tolerance and smart-case. Frecency recorded on open, used only to break close scores. A shared keyboard-first palette component (listbox, active descendant, Esc restores focus) and ⌘P in the browser. | Go unit tests for scoring and catalog freshness; a benchmark showing p95 under 50ms per query on a synthetic 20,000-note vault; vitest for the palette; e2e for ⌘P open and search. |
| 5. Desktop chrome and finders | US2, US9, US10, desktop requirements | `desktop/src-tauri/src` (`windows.rs`, `menu.rs`, `page.rs`, `state.rs`, `pane.rs`, `security.rs`), `desktop/src` (`App.tsx`, `RepositoryList.tsx`, `Settings.tsx`, `style.css`, `api.ts`), `web/src/components/desktopHost.ts`; amends SPEC-0113 and SPEC-0119 | Lenient `settings.json` for theme and appearance, broadcast to windows. Native appearance via Tauri's theme API. Window and webview background colours. A validated `theme` page command pushed on first report and on change. Themed toolbar. Appearance in a native View › Appearance menu and in Settings. ⌘P menu item routed to the page. **Spike first:** a transparent finder webview above the content view (stopping rule below). Then the ⌘O and ⇧⌘O finders with ⌘1–⌘9, open in a new window, the repository actions relocated, Add Repository moved off ⌘O, and the sidebar removed. | `make desktop-deps`, `make desktop-check`, and `make desktop-build`; Rust tests for settings tolerance and command validation; vitest for finder filtering; the packaged app exercised against a fixture folder in both appearances, with screenshots via the CGWindow capture path. |
| 2b. Note reader and edit mode | US11, note editing fidelity | `web/src/ontology.css` (note body, section, and properties rules), `web/src/markdown-editor.css`, `web/src/components/body/` (`NarrativeEditor.tsx`, `ChildSection*.tsx`), `OntologyPropertyPanel.tsx`, `OntologyNotePane.tsx` (body wrapper only) | Section rhythm without per-heading rules, with more space above headings than below; distinct H2 and H3 sizes; 15px Inter prose at a 65–75ch measure. Disclosure controls on hover and focus. Empty properties behind "Show N empty fields", with quieter rows. A quiet home line. In edit mode, trim the displayed leading blank lines and CRLF tails in `NarrativeEditor` while `previousMarkdown` keeps the raw block; borderless regions that show on hover and focus; read-mode typography so toggling Edit doesn't move text. | A unit test that an edit and save of a section with a blank line after its heading (LF and CRLF) leaves every other byte unchanged; existing narrative edit tests and the GraphQL body-block tests unchanged and passing; screenshots of a typed note in read and edit mode at 1440, light and dark; `make check` and `make web-e2e`. |
| 6. Integration and closure | Documentation requirements, full verification | `DESIGN.md`, `.impeccable/design.json`, `PRODUCT.md`, SPEC-0090/0113/0119/0125, `desktop/README.md`, custom-views skill templates, `CHANGELOG.md` | Rewrite DESIGN.md from the shipped result (`/impeccable document`). Amend the specs. `make build`, `rzm init`, and `rzm init --check` for skill templates. Screenshot goldens for the Effort notes table (1100 and 1440, light and dark). Re-run the Impeccable critique. | `make verify`, desktop checks, `./scripts/rzm validate` and `frozen-scope-drift`, Greptile 5/5 per batch, CI green, and a critique score recorded against the 24/40 baseline. |

**Dependencies.** Batch 1 comes first, because every later batch reads its token names and type scale. Batch 2b starts once Batch 2 has merged into the parent, because both change the note reader's width and styles. Once Drew has looked at Batch 1, batches 2, 3, 4, and 5 run in parallel:
- **Batch 2** owns the shell and rails.
- **Batch 3** owns configured-view and bundled-view files. It writes overflow columns into the right pane through the existing record slot.
- **Batch 4** owns the Go finder, the API, and the palette component.
- **Batch 5** owns `desktop/`. It reuses Batch 4's palette only through the web page's ⌘P command.
- **Hand-offs:** where batches 2 and 3 meet at the right pane, Batch 2 lands its pane API before Batch 3 integrates into it.

**Spike (Batch 5).**
- **Question:** can a child webview above the content view be transparent outside its palette, receive focus, and close cleanly, with capabilities scoped like the shell's?
- **Smallest experiment:** a throwaway finder webview that shows a scrim and a centred box, opened by ⇧⌘O on a dev build.
- **Success:** the page stays visible under the scrim, the palette takes and returns focus, no white flash, and it works in both appearances.
- **Stop:** after two attempts, or about half a day, fall back to the drop-down band named in SPEC-0126 and record a deviation.

**Delegation.** I implement Batch 1 myself, because it fixes the token contract. Batches 2–5 go to Opus 5.5 High workers through T3 delegation, each in its own worktree branched from the parent after Batch 1, with the ownership split above. Each worker:
- runs `make check` and its batch's focused gates, with web e2e on a unique port;
- commits after every deliverable.

I review every diff, rebase onto the parent, and test the combined tree before squash-merging.

**Escalation boundary.** Routine choices are mine: token values within the AA rule, exact pixel values, component structure, test layout, and which repository actions go in a row menu versus the name menu. Anything else goes back to Drew:
- changes to the ten decisions in Scope;
- a public kit token rename;
- a persisted schema change beyond the new desktop settings file and the finder's frecency table;
- the Batch 5 fallback, which I record and report rather than silently take.

**Authority requested with this plan:**
- push the parent and child branches with explicit refs;
- open the parent and child PRs;
- squash-merge child PRs into the parent;
- run Greptile on each.

Merging to `main` stays with Drew.

## Original Intended Delivery

One parent PR that retires the beige, cramped workbench for Slack-like themed chrome with light and dark appearance, Inter, and a calmer layout. In that PR, a table never hides its titles, panels resize, and ⌘P, ⌘O, and ⇧⌘O reach any note, file, repository, or worktree, in the browser and in Rhizome Desktop.

## Actual Delivered

The parent PR #48 (`feat/workbench-redesign`) carries one commit per batch:
- batch 1 (themes, appearance, Inter);
- #50 (⌘P finder);
- #51 (shell, resizable panels, one right pane);
- #52 (view header, column rule, reading widths);
- #53 (desktop chrome and the ⌘O and ⇧⌘O finders);
- #54 (note reader and edit mode);
- #55 (Batch 6: documentation, critique, and closure).

The workbench has five chrome themes with Light, Dark, and Match System appearance in the browser and in Rhizome Desktop, and Inter on a 14px scale. A table's title column keeps at least 240px. The panels resize and persist. ⌘P, ⌘O, and ⇧⌘O reach notes, files, repositories, and worktrees. The note reader reads calmly and edits in place. The critique score rose from 24/40 to 29/40.

US3's first-row criterion was amended to the shipped 166px (210px grouped). The packaged-app click-through in Rhizome Dev follows the merge.

## Execution Notes

- 2026-10-25T19:15:41Z: Drew approved execution in chat ("Go ahead and update your spec and begin executing"). This came after reviewing the batch order presented earlier in the session (tokens, themes, and Inter; shell layout; view header and table; ⌘P; desktop with the finder spike; documentation and critique) and confirming every decision listed under Scope. This plan revision was written from that order. No current Person is configured in this checkout, so `plan-approved-by` is omitted; the chat approval stands. Push and PR authority is requested in the plan and confirmed with Drew before the first push.

- 2026-10-25 Batch 1 (the `feat(web): chrome themes, appearance, and Inter` commit, local).
  - **Token design:** every color token is a `light-dark()` pair resolved by the root `color-scheme`. Appearance is one attribute (`data-appearance`) with the OS as the default, so the separate dark block and its duplication are gone.
  - **Build output:** Vite's lightningcss lowers `light-dark()` to its `--lightningcss-light`/`--lightningcss-dark` fallback and rewrites the `color-scheme` rules for both attribute values and the media query. That lowering was checked in the built CSS.
  - **Kit:** frames get the raw token text through `boot.js` and mirror the host's attributes.
  - **Themes and type:** five themes; the neutral ramp is white, 96.5, and 92.7 L\* with 89.9 and 82.4 L\* lines. Inter (Google Fonts, 7 subset files) replaces Barlow's 14 files. The type scale moves to a 14px base with 600-weight labels. The global `ss01`/`tnum`/`cv11` features are removed, because Inter widens the hyphen under `tnum`.
  - **Browser controls and followers:** the browser header has an Appearance control. Canvas colors resolve through a probe element and refresh on appearance changes. GraphiQL is forced to the resolved appearance.
  - **Evidence:**
    - `make check` exit 0 (web unit tests 1,643 passed).
    - `RHIZOME_E2E_PORT=7881 make web-e2e` exit 0, 102 passed.
    - `python3 scripts/licenses/generate.py --check` current.
    - The contrast test (`web/src/lib/themes.test.ts`, 20 theme checks plus content checks) passes.
    - Screenshots of the Effort notes table, a note, All notes, GraphiQL, and the Appearance panel in Graphite, Spruce, Slate, and Indigo, light and dark.
  - **Fixed during the batch:** header button padding hid the Appearance icon; GraphiQL's own dark palette outranked ours.

- 2026-10-25T19:47:43Z: Drew reviewed the Batch 1 screenshots and approved continuing ("1 Yeah"), so batches 2–5 may start. He also granted the push and PR authority requested under Plan ("2. Yeah"): push the parent and batch branches with explicit refs, open the parent and batch PRs, squash-merge batch PRs into the parent, and run Greptile on each. Merging to `main` stays with Drew.
- 2026-10-25T19:47:43Z: Rebased onto `main` at `20113f3d` (#46, #47) before the first push. The rebase was clean; `make check` exit 0 (web unit tests 1,651 passed).

- 2026-10-25T20:23:19Z: Queued Batch 2b (note reader and edit mode) to start after Batch 2 merges.

- 2026-10-25T21:32:05Z Batch 4 (⌘P finder) merged into the parent as #50 (`0d840d70`).
  - **Review fixes:** superseded searches are canceled through the request context, a failed catalog rebuild retries with backoff, and Enter during an in-flight search opens the first fresh result. The palette also ignores IME composition keys and hides "Open beside" for code files.
  - **Reverted:** a `#`/`?` file-name heuristic in tab targets broke heading links, so it was reverted and recorded as a known limitation (see Compounding Follow-ups).
  - **Evidence:** Greptile 5/5 with no comments; `make check` and web e2e passed; CI green. `BenchmarkSearch20kNotes`, rerun on 2026-10-26 on the parent tree, gave p50 1.3ms, p95 9.1ms, and max 10.6ms against the 50ms target.
- 2026-10-25T22:02:32Z Batch 2 (shell) merged as #51 (`bf1e4d44`).
  - **Review fixes:** the frame fills the window when the sidebar is hidden, a table passes `selected` to the record pane, closing the pane from inside returns focus to its toggle, and the record pane opens 400ms after a selection settles.
  - **Evidence:** Greptile 5/5; `make check` and web e2e, including a new check that the frame fills the window; CI green.
- 2026-10-25T22:41:20Z Batch 3 (view header, column rule, reading widths) merged as #52 (`f643f4f1`).
  - **Review fixes:** an active preset's chip returns when its last filter row is removed, narrow headers shrink Find, and Sort shows a sort by a source key under its field.
  - **False positive:** Greptile's claim that the header's container query traps its menus under the sticky table headings was disproved with a probe in Chromium and WebKit. A control probe with `contain: layout` is covered, so the probe tells the two apart.
  - **Evidence:** Greptile 5/5; `make check` (1,682 web unit tests) and 104 e2e tests; CI green.
- 2026-10-26T00:25:47Z Batch 5 (desktop chrome and finders) merged as #53 (`078acb31`).
  - **Greptile:** it took about fifteen passes, each finding one real race or edge case. An independent Opus review of the whole diff then found four more at once.
  - **Fixes:** settings apply in save order on the main thread, and shells take settings only from the broadcast. Finder show, hide, and choice requests carry opening stamps and revisions so late ones are dropped. The finder takes focus only when an opening starts, and other menu commands close it first. → opens a row's actions, IME keys are ignored, and emoji and case-folded matches highlight correctly. The highlight follows row identity, and a new shell paints its saved theme from an initialization script.
  - **CI fix:** the desktop CI job installs only desktop packages, so the theme list moved to a dependency-free `web/src/lib/themes.ts`.
  - **README:** `desktop/README.md` was updated in this batch rather than Batch 6.
  - **Evidence:** Greptile 5/5 with no comments; `make check`; `make desktop-check` (88 shell and 81 Rust tests); 104 e2e tests; CI green.
- 2026-10-26T00:45:20Z Batch 2b (note reader and edit mode) merged as #54 (`17dc115f`).
  - **Worker delivery:** the display trim with raw `previousMarkdown`, the section rhythm, the gutter chevron, the empty-fields toggle, the quiet home line, and the in-place editor states.
  - **Integration fixes:**
    - Prose now holds the spec's 65–75 character measure: 540px inside the 680px column, measured in a browser at 66–79 characters per line with a 72 average.
    - The controls row truncates the path instead of the buttons.
    - A field with links or bound sections counts as filled.
    - A wikilink example in `web/CONTEXT.md` that failed content validation is now quoted.
  - **Evidence:**
    - `TestExecute_WorkspaceNarrativeTrimmedEditKeepsOtherBytes` covers LF and CRLF files.
    - A manual save through the UI kept every other byte, including all 23 CRLF line endings.
    - Greptile 5/5 with no comments on the first pass; `make check` and 104 e2e tests; CI green.

- 2026-10-26T01:12:17Z Batch 6 (documentation, critique, and closure) on `feat/workbench-redesign-b6-docs`, opened as #55.
  - **Design truth (worker):** `DESIGN.md` and `.impeccable/design.json` are rewritten from the shipped code, and `PRODUCT.md` names Inter. Reference screenshots of the Effort notes table (1100×800 and 1440×900, Graphite light and dark) are in `.impeccable/references/workbench-2026-10/`.
  - **Critique:** the fresh Impeccable critique (`.impeccable/critique/2026-10-26T00-54-53Z__web-src.md`) scores **29/40 against the 24/40 baseline**, with no P0 left. The title column that collapsed to 10px at 1100px is now 456px.
  - **Its P1s:** Edit mode grows the properties into form controls; the header is still 210px tall; "Active" still renders three ways (pre-existing).
  - **Fixed from the critique:** the global search placeholder in Graphite light was 4.47:1. Graphite's light `--chrome-muted` is now `#cbc7c1`, and the contrast test covers muted text on the raised field for every theme.
  - **Not reproduced:** the critique's report that the pane toggle claims an open pane at 1100px. A browser probe showed `aria-expanded` false until `]` opens the pane, then true.
  - **Specs:**
    - SPEC-0090, SPEC-0099, SPEC-0112, SPEC-0113, SPEC-0119, SPEC-0125, and the custom-view diagrams spec carry dated amendment notes.
    - SPEC-0126 records its delivery decisions. SPEC-0110 needed no change.
    - The web `CONTEXT.md` documents the finder routes, and the stores note the `finder_opens` table.
    - The custom-views skill names Inter and the current kit sizes; `rzm init --check` exits 0.
    - The changelog describes the final state.
  - **Gates on the docs branch:**
    - `RHIZOME_E2E_PORT=7887 make verify` exit 0: the full checks (1,694 web unit tests) and 104 e2e tests.
    - `make desktop-deps`, `make desktop-check` (88 shell and 81 Rust tests), and `make desktop-build` all exit 0.
    - `./scripts/rzm validate` exit 0. `./scripts/rzm validate frozen-scope-drift` reports only two drifts in other efforts (EFF-2026-10-01-10-06 and EFF-2026-10-03-18-33), both from specs changed on `main` before this branch.
  - **Packaged app:** it was built and launched against a temporary data directory, but its windows never painted into captures. A build from just before Batch 5 behaved the same, so this is the capture environment, not a regression. Temporary instrumentation confirmed the packaged shell loads `tauri://localhost` and renders "Add a repository to begin". Exercising it with a real repository is carried to Drew's click-through in Rhizome Dev.

## Deviations

- 2026-10-25T20:23:19Z: Scope change approved by Drew in chat ("Yes, please do"). He reported, with screenshots, that edit mode shows blank lines and stacked boxes in note sections, and that read mode looks cramped and inelegant.
  - **Spec change:** added US11 (read and edit a note comfortably) and a note editing fidelity requirement to SPEC-0126. Removed "the note reader" from the restructuring non-goal, and added a non-goal that rules out changing the GraphQL body-block projection or the edit-session op contract.
  - **Cause of the blank lines:** the server's narrative block keeps the blank line after a heading as a leading `\n` (`pkg/ontology/body.go` `emitNarrative`), which tests pin as part of the public GraphQL contract, and `NarrativeEditor` shows it unchanged. The fix trims only the display.
  - **Plan change:** delivered as new Batch 2b. frozen-scope-drift acknowledged for SPEC-0126: the spec set is refrozen at this revision.
- 2026-10-26T01:12:17Z: delivery reconciliation of SPEC-0126, with no scope change. frozen-scope-drift acknowledged for SPEC-0126 (`last-updated: 2026-10-26`).
  - **Resolved entries:** the spike result, the preferences file, the fzf pin and typo bounds, Sort's first key, the reading measure, and the desktop first-paint limit.
  - **US11:** "does not move the text" holds within a section.
  - **US5 and US6:** below 1200px the narrow-window rule wins over opening on selection.
  - **Labels and screenshots:** column heads ship at 13px and labels at 12px. The screenshot requirement is met with reference captures.
  - **Reset label:** the View popover's reset is "Reset to shared", matching the existing control.
- 2026-10-26T01:12:17Z: **awaiting Drew's decision.** US3's "first data row no more than about 140px below the top of the content panel" is not met.
  - **What ships:** the first data row sits 166px down in an ungrouped table and 210px in a grouped one: the 36px document tab strip, the 56px title row, the 40px presentation tabs, the 34px column heads, and a 44px group header.
  - **Why:** the header matches the approved comp (`comps3.html`) exactly. The 140px figure came from that comp, which had no document tab strip and no group header.
  - **Options:** amend the criterion to the shipped geometry (recommended), or compress the header rows to about 48, 32, and 30px to reach about 146px ungrouped.
- 2026-10-26T13:10:00Z: Drew reviewed the redesigned workbench ("that looks really good") and asked for #48's cleanup and merge. The US3 criterion is amended to the shipped geometry, as recommended: SPEC-0126 now allows about 166px ungrouped, with a grouped table adding its 44px group header.
- 2026-10-26T13:55:00Z: Drew asked for the desktop toolbar's ⌘O and ⇧⌘O hints to show only in the repository and worktree tooltips. US9's toolbar criterion now says so.
- 2026-10-26T13:40:00Z: Drew asked for Briefing and Overview to fill the content panel and hug the presentation tabs, instead of a centred 1120px block that floats apart from them. US7 now caps only note prose. frozen-scope-drift acknowledged for SPEC-0126: the spec set is refrozen at this revision.
- 2026-10-26T13:10:00Z: the packaged-app click-through moves after the merge. Rhizome Dev installs only from `main` (`make desktop-install-dev` refuses a feature branch), so Drew exercises it with a real repository once #48 lands.
- 2026-10-26T01:12:17Z: Batch 3's exit evidence ("first data row at or below 140px measured") was not checked before #52 merged; it was measured in Batch 6 (above).
- 2026-10-26T01:12:17Z: `desktop/README.md` was updated in Batch 5 instead of Batch 6. Batch 6 was split between a design-truth worker (DESIGN.md, design.json, PRODUCT.md, reference screenshots, critique) and the integrator (specs, changelog, context notes, skill templates, closure).

## Closure Checklist

- [x] Spec coverage checklist complete or deviations recorded. US3's criterion amended (Deviations).
- [x] Quality gates recorded with exact commands and results (Execution Notes, Batch 6).
- [x] SPEC-0126 reconciled with delivery; amended specs, DESIGN.md, and PRODUCT.md updated.
- [x] Parent PR open; Greptile 5/5 per batch; CI green on the final head. #48 carries the work; every child PR, through #55, reached Greptile 5/5 with green CI.
- [x] Follow-ups recorded.
- [x] Desktop verification: recorded as a post-merge click-through in Rhizome Dev (Deviations, Follow-ups).

## Compounding Follow-ups

- **Desktop click-through:** after #48 merges, run `make desktop-install-dev` from the primary checkout and exercise Rhizome Dev with a real repository: themes and appearance, ⌘O and ⇧⌘O, and the note reader.
- **Tab targets with `#` or `?` in a file name:** ⌘P opens such a code or note file through a tab target that treats `#` and `?` as a fragment or query. A heuristic fix broke heading links and was reverted, so these files stay a known limitation until tab targets carry the path and fragment separately.
- **E2E only in a fake-harness worktree:** web e2e run from a `/tmp` worktree failed 98 tests, because the server there was not the fake-harness test build. Workers had to run e2e in their own prepared worktree. The e2e setup should build or check the server it needs instead of trusting what the port serves.
- **Tracked build artifact:** `make build` rewrites the tracked `pkg/app/web/assets/dist/index.html`. That blocked rebases twice and had to be restored after every build. It should be generated, untracked, or written only when its content changes.
- **Desktop first paint:** a first load of another runtime origin, in a window opened before a theme change, still paints the older theme until the page reports its title (SPEC-0126 Open Questions). A window-scoped theme source that the content view reads at document start would close it.
- **Desktop CI and web imports:** the desktop CI job installs only desktop packages, so a desktop import of a web module with package dependencies fails only in CI. A lint rule or a check in `make desktop-check` that resolves `web/src` imports without `web/node_modules` would catch it locally.
- **Timing-sensitive desktop test:** "shows nothing for progress that finishes within the quiet period" (`desktop/src/App.test.tsx`) waits 2s for an 800ms quiet period and timed out once under load. It should use fake timers.
- **Section editor fidelity:**
  - Edit mode shows wikilinks by their resolved labels.
  - The editor's table widget adds space above tables.
  - Blank lines between paragraphs show as full source lines, so only a section's first paragraph sits exactly where read mode puts it.
- **No pixel-diff layer:** the repository has no visual regression tests. Reference screenshots under `.impeccable/references/` record the shipped look, but nothing fails when it drifts.
- **Review cost on concurrency-heavy batches:** Greptile found Batch 5's races one per pass. An early whole-diff review by an independent model found several at once, so run one before the Greptile loop on stateful or concurrent work.

## Status

Complete. SPEC-0126 is delivered by parent PR #48, with US3's first-row criterion amended and the desktop click-through following the merge.
