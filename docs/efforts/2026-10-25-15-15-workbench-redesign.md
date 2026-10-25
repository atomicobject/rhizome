---
type: EffortNote
id: EFF-2026-10-25-15-15
aliases: [EFF-2026-10-25-15-15]
name: Workbench redesign
created-at: 2026-10-25T19:15:41Z
status: active
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

- [ ] US1 themes and appearance; theme and token requirements.
- [ ] US2 desktop window chrome; desktop requirements.
- [ ] US3 view header and View popover.
- [ ] US4 table column policy and density.
- [ ] US5 resizable, persisted panels.
- [ ] US6 one right pane.
- [ ] US7 reading-surface width caps and board columns.
- [ ] US8 ⌘P finder; finder requirements and latency target.
- [ ] US9 ⌘O repository finder.
- [ ] US10 ⇧⌘O worktree finder.
- [ ] US11 note reader rhythm and edit mode; note editing fidelity requirement.
- [ ] Typography (Inter, scale, tabular figures) and contrast test.
- [ ] Documentation: DESIGN.md and design.json, PRODUCT.md, SPEC-0090/0113/0119/0125 amendments, desktop README, custom-views skill templates, changelog.

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

Not yet delivered.

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

## Deviations

- 2026-10-25T20:23:19Z: Scope change approved by Drew in chat ("Yes, please do"). He reported, with screenshots, that edit mode shows blank lines and stacked boxes in note sections, and that read mode looks cramped and inelegant.
  - **Spec change:** added US11 (read and edit a note comfortably) and a note editing fidelity requirement to SPEC-0126. Removed "the note reader" from the restructuring non-goal, and added a non-goal that rules out changing the GraphQL body-block projection or the edit-session op contract.
  - **Cause of the blank lines:** the server's narrative block keeps the blank line after a heading as a leading `\n` (`pkg/ontology/body.go` `emitNarrative`), which tests pin as part of the public GraphQL contract, and `NarrativeEditor` shows it unchanged. The fix trims only the display.
  - **Plan change:** delivered as new Batch 2b. frozen-scope-drift acknowledged for SPEC-0126: the spec set is refrozen at this revision.

## Closure Checklist

- [ ] Spec coverage checklist complete or deviations recorded.
- [ ] Quality gates recorded with exact commands and results.
- [ ] SPEC-0126 reconciled with delivery; amended specs, DESIGN.md, and PRODUCT.md updated.
- [ ] Parent PR open; Greptile 5/5 per batch; CI green on the final head.
- [ ] Follow-ups recorded.

## Compounding Follow-ups

None yet.

## Status

Active. Batch 1 on the parent branch; batches 2–5 in progress.
