---
type: ExperienceSpec
id: SPEC-0126
aliases: [SPEC-0126, Workbench redesign]
summary: "Redesigns the Rhizome workbench in the browser and in Rhizome Desktop: selectable chrome themes with light and dark appearance, an Inter type scale and neutral surfaces, a calmer view header with progressive disclosure, a table column policy that keeps titles readable, resizable side panels with one right pane, and fuzzy finders for notes and files (⌘P), repositories (⌘O), and worktrees (⇧⌘O)."
spec-status: active
last-updated: 2026-10-26
---

# Workbench redesign

## Summary

After [[design-system-parity|SPEC-0125]] the workbench was consistent but still read as cramped, beige, and noisy. On 2026-10-25 an Impeccable critique scored it 24/40 (`.impeccable/critique/2026-10-25T17-51-30Z__web-src.md`). The worst defect it found was the title column of a type table shrinking to 10px at a 1100px window while ID, status, and date columns kept their widths. Drew reviewed three rounds of comps (`.impeccable/mocks/workbench-comps/`, current round `comps3.html`) and chose a partial redesign. It keeps Rhizome's AO identity (warm-black ink, AO red for focus and risk, teal for connection, JetBrains Mono for evidence, Merriweather for document titles), keeps expert density, and replaces the neutrals, the UI typeface, the type and spacing scale, and the layout rules.

The direction follows Slack's model:
- **Chrome:** one soft, tinted colour for the top bar and sidebar, chosen from preset themes, each with light and dark appearance.
- **Content:** a white content panel inset with a rounded corner.
- **Disclosure:** quiet headers that keep rarely used controls one click away.
- **Desktop:** Rhizome Desktop's web-drawn title bar takes the same theme.
- **Finders:** fuzzy finders replace the left-rail note list and the desktop repository sidebar.

This spec governs the Notes workspace shell, configured views, bundled group views, the note reader and its edit mode, the desktop shell chrome, and the finders. It amends [[notes-workspace-shell|SPEC-0090]] (rail note list and fixed rail widths), [[rhizome-desktop|SPEC-0113]] (repository sidebar), [[desktop-unified-toolbar|SPEC-0119]] (toolbar chrome), and [[design-system-parity|SPEC-0125]] (its "no in-app appearance override" non-goal and Barlow as the UI face).

## Goals

- At 1100, 1440, and 1920px wide the workbench looks composed and calm while showing at least as much information per screen as today.
- A table's title column is always readable. Supporting columns never squeeze it below its minimum, and columns that don't fit move to the right pane instead of truncating the title.
- Every screen uses visibly separated surfaces: white work surfaces, one tinted chrome layer, and hairlines. Adjacent surfaces differ by a clear lightness step, never one or two points of L\*.
- A user picks a theme and an appearance (Match system, Light, Dark) once, and the browser UI, custom-view frames, and Rhizome Desktop's chrome all follow it.
- Controls appear when they are needed. A view header shows the title, presentations, find, Filter, and View, and everything else sits one click away.
- Getting to any note, file, repository, or worktree takes one shortcut and a few typed characters.
- Text and focus meet WCAG 2.1 AA in every theme and appearance.

## Non-Goals

- Phone or touch layouts.
- User-defined theme colours. Themes are presets.
- A multi-scope command palette (commands, branches, commits). The finders are separate and scoped.
- Restructuring Board and Cards, Ontology, Explorer, Agent, or GraphQL beyond the new tokens, type scale, and shared header pattern.
- Changes to the GraphQL body-block projection (`workspace.bodies.blocks.markdown`) or the edit-session op contract.
- New identity or logo work, and commercial fonts.
- Changes to view YAML, preference storage contracts, or GraphQL beyond the additive finder endpoint named below.

## User Stories

### US1 - Choose a theme and an appearance

- id:: ^SPEC-0126-US1
- summary:: Pick a chrome theme and Match system, Light, or Dark from an Appearance control, and see every Rhizome surface follow the choice.
- status:: ready

#### Acceptance Criteria

- An Appearance control offers Match system, Light, and Dark, plus the preset themes Graphite (default), Slate, Spruce, Oxblood, and Indigo, each shown as a swatch.
- Choosing a theme recolours the top bar and sidebar immediately, without reloading the page.
- Choosing an appearance switches the content panel, chrome, custom-view frames, GraphiQL, and canvas renderers (graphs, diagrams) between light and dark immediately. Match system follows the operating system and tracks later OS changes.
- In Rhizome Desktop the choice applies to every window and every repository, and survives restarts. In the browser (`rzm serve` without Desktop) it is remembered per browser origin.
- Each theme in each appearance meets AA for chrome text, muted chrome text, the active item, and focus indicators.

### US2 - See the desktop window take the theme naturally

- id:: ^SPEC-0126-US2
- summary:: In Rhizome Desktop, see the title bar behind the window controls, the sidebar, and the window background in the theme colour, with no white flash.
- status:: ready

#### Acceptance Criteria

- The toolbar under the overlay title bar, including the strip behind the traffic lights, uses the theme's chrome colour. The section tabs stay centred.
- Opening, restoring, and resizing a window show the theme's chrome or panel colour, never a white flash, in both appearances.
- Native menus, scrollbars, and window controls follow the chosen appearance.
- The repository content inside the window shows the same theme as the chrome around it, including when a different repository or worktree opens.

### US3 - Scan a collection without header clutter

- id:: ^SPEC-0126-US3
- summary:: Open a configured view or type collection and see a title row and presentation tabs first, with grouping, sorting, row density, and columns behind one View control.
- status:: ready

#### Acceptance Criteria

- The header shows the collection dot and title, its count, a short description that truncates, Find in view (⌘F), Filter, View, and the pane toggle on one row. Presentations (Table, Board, Cards, Briefing, and saved views) appear as text tabs on the row below.
- View opens a popover holding Group by, Sort, Rows (a visible "1 line | 2 lines" segmented control), Columns, Reset to shared, and Save view.
- The separate stats band and preset pill band no longer appear. Counts appear on group headers and in the title row, and active filters appear as removable chips under the header only while a filter is active.
- On a 900px-tall window the first data row of an ungrouped table starts no more than about 166px below the top of the content panel (the 36px tab strip, the 56px title row, the 40px presentation tabs, and the 34px column heads); a grouped table adds its 44px group header.

### US4 - Keep titles readable in tables at any width

- id:: ^SPEC-0126-US4
- summary:: Read every row's title in a table at 1100px or wider, with supporting columns giving way before the title does.
- status:: ready

#### Acceptance Criteria

- The title column keeps at least 240px in one-line rows and 320px in two-line rows, and takes all remaining width.
- ID, status and enum, date, number, flag, and relation columns have fixed widths and never take a share of extra width. Short-text columns are sized like enums.
- When the table can't fit all columns at those minimums, the lowest-priority columns leave the table and show in the right pane for the selected row, with a footer note that names them. The order is relation, then ID, then date.
- At 1920px no supporting column is wider than its fixed width plus its user-resized width.
- The density toggle switches between one-line rows (about 36px) and two-line rows (about 56px) showing the summary.

### US5 - Resize and collapse side panels

- id:: ^SPEC-0126-US5
- summary:: Drag the sidebar and right pane to the widths I want, collapse them, and find them the same next time.
- status:: ready

#### Acceptance Criteria

- The sidebar and right pane each have a drag handle on their inner edge. The sidebar ranges from 200 to 400px and the right pane from 280 to 560px.
- Each handle is keyboard operable (`role=separator`, arrow keys in 16px steps, Home and End for minimum and maximum) and shows a visible focus state.
- Widths and collapsed state persist across reloads.
- Below 1200px wide, the right pane starts collapsed and opens over the content when toggled, so the table keeps its width.
- Amended by [[workbench-sidebar|SPEC-0128]] on 2026-10-26: the sidebar's width and collapsed state are shared by every section, and its toggle is in the top bar (Rhizome Desktop: the toolbar), with ⌃⌘S and ⌘\\.

### US6 - Use one right pane for the selected thing

- id:: ^SPEC-0126-US6
- summary:: See the selected row's record or the active note's context in one right pane that closes when there is nothing to show.
- status:: ready

#### Acceptance Criteria

- One right pane hosts Record for the selected table or board item, and Info, Outline, and Graph for the active note. It replaces the separate Record rail and note context rail, with one width, one toggle, and one collapsed state.
- With nothing selected on a collection, the pane is closed. Selecting a row opens it unless the user closed it explicitly, and ] toggles it.
- The pane title, field labels, and values use the new type scale: 15–17px semibold title, 13px labels, no 700 weights.

### US7 - Read wide-screen pages that stay composed

- id:: ^SPEC-0126-US7
- summary:: Read notes on a wide monitor without prose stretching across the whole window, while collection presentations such as Briefing and Overview use the full panel under their tabs.
- status:: ready

#### Acceptance Criteria

- The note body and its header controls cap their width at a 65–75 character measure for prose and centre in the content panel. Briefing and Overview fill the content panel and align with the presentation tabs above them.
- Data surfaces (Table, Board) fill the width. Board columns range from 260 to 360px, so a wider window shows more columns rather than wider ones.

### US8 - Jump to any note or file with ⌘P

- id:: ^SPEC-0126-US8
- summary:: Press ⌘P (Ctrl+P off macOS), type a few characters of a title or path, and open the note or file.
- status:: ready

#### Acceptance Criteria

- ⌘P opens a finder over the current page in the browser and in Desktop. With no query, it lists recently opened notes.
- Matching is fuzzy over display title and vault-relative path. It tolerates dropped and swapped characters ("shcema" finds "schema"), is smart-case, highlights the matched characters, and ranks exact and prefix title matches first. Frecency (how often and how recently the user opened an item) breaks close scores.
- Results show title, type, and middle-truncated path. Enter opens a note as a tab (or a code file in Explorer), and ⌘Enter opens beside.
- Results update within 50ms of a keystroke at the 95th percentile on a 20,000-note vault served locally, and never block typing.
- The left-rail note list is removed. The sidebar holds Views, scopes (All notes, Recently changed, Problems), and the type tree.

### US9 - Switch repositories with ⌘O

- id:: ^SPEC-0126-US9
- summary:: In Rhizome Desktop, press ⌘O or click the repository name in the toolbar to fuzzy-find a known repository and open its primary checkout.
- status:: ready

#### Acceptance Criteria

- The desktop toolbar, where the shell already draws its worktree selector, shows the current repository and worktree as two separate click targets whose tooltips name ⌘O and ⇧⌘O. In a browser the sidebar header names the vault.
- The repository finder lists known repositories by most recent use, works without typing, and fuzzy-filters by name and path. ⌘1–⌘9 open the first nine.
- Enter opens the repository's primary worktree in the current window, and ⌘Enter opens it in a new window. "Add repository…" sits at the bottom.
- Each row shows whether a Rhizome runtime is running for that repository. Per-repository actions (Open in Browser, Reveal in Finder, What Gets Indexed…, Stop/Restart Rhizome, Primary worktree, Rhizome executable, Remove from Library) are available from the row and from the repository name's menu.

### US10 - Switch worktrees with ⇧⌘O

- id:: ^SPEC-0126-US10
- summary:: In Rhizome Desktop, press ⇧⌘O or click the worktree name to fuzzy-find a worktree of the current repository, with the primary worktree one keystroke away.
- status:: ready

#### Acceptance Criteria

- The worktree finder lists only the current repository's worktrees, with the primary worktree pinned first, and fuzzy-filters by branch and path.
- Rows show branch, path, the running indicator, and the new-worktree marker. Enter opens in this window and ⌘Enter opens a new window.
- On the primary worktree the toolbar's worktree label is quiet. On any other worktree it is visibly distinct.

### US11 - Read and edit a note comfortably

- id:: ^SPEC-0126-US11
- summary:: Read a typed note with a calm document rhythm, and switch to Edit without stray blank lines, stacked boxes, or the text moving.
- status:: ready

#### Acceptance Criteria

- Sections separate by space, not by a rule under every heading. There is more space above a heading (about 28px) than below it (about 8px), and H2 and H3 sizes differ clearly on the type scale.
- Prose uses Inter at about 15px with a 65–75 character measure. Spacing between paragraphs is visibly smaller than spacing between sections.
- A collapsible section's disclosure control appears on hover and keyboard focus rather than permanently in the margin. A collapsed section still shows its control.
- In read mode, properties without values collapse behind a "Show N empty fields" toggle. The remaining rows use muted labels without a rule on every row.
- The note's home or breadcrumb line reads as quiet metadata above the body.
- In edit mode, each section body shows its prose without the leading blank line that follows its heading, and without trailing blank lines from CRLF files. It uses the same typography as read mode. An editable region shows a border or tint only on hover and focus, and toggling Edit does not move the text.
- Saving an edited section writes the file with its original surrounding blank lines and line endings.

## Requirements

### Themes, appearance, and tokens

- MUST keep `web/src/tokens.css` as the single token source for the host, the kit, bundled views, and the desktop shell.
- MUST express a theme as chrome tokens only (chrome background, top-bar background, chrome text, muted chrome text, active pill and its text, selection tint). Content tokens are shared by all themes and vary only by appearance.
- MUST apply appearance through one attribute on `<html>` with `prefers-color-scheme` as the Match system default, and keep the public kit token names locked by SPEC-0125 working.
- MUST apply the theme and appearance before first paint in the host and in every custom-view frame.
- MUST invalidate canvas colour caches (`web/src/lib/themeColors.ts`) and switch GraphiQL's theme on an appearance change, not only on the OS media query.
- MUST use at most white plus two tinted neutral surfaces in the content area per appearance, separated by clear lightness steps. Selection uses a chrome-tinted neutral, not red-soft. AO red stays for brand, focus, and real risk.
- MUST keep `@media (prefers-reduced-motion)` respected for theme transitions.

### Typography and spacing

- MUST replace Barlow with Inter as the UI face, self-hosted as variable woff2 (Latin and Latin Extended) under SIL OFL, with license notices generated through `scripts/licenses/generate.py`. Merriweather stays for document titles and JetBrains Mono for evidence.
- MUST apply tabular figures only to number-bearing elements (counts, dates, numeric cells), because Inter widens the hyphen under `tnum`.
- MUST use a 14px-class UI base, 13px secondary text and column heads, 12px labels, and an 18px view title, with weights 400, 500, and 600 in chrome and 650–700 only for titles.
- MUST replace hard-coded Barlow references (graph labels, mermaid, docs, skill copies) and hard-coded font sizes in bundled group views with tokens.

### Desktop

- MUST keep SPEC-0119's protocol: title-only reports from the page and fixed-name DOM events with validated, serialized details, sent only to the verified runtime origin. The page gets no new capabilities and no URL parameters.
- MUST store theme and appearance in a desktop settings file that tolerates unknown and missing fields, separate from `library.json`, and broadcast changes to every window.
- MUST apply appearance natively (app-wide NSAppearance through Tauri's theme API) and set the window and webview background colours to the theme on creation and on change.
- MUST push the chrome theme to the content page with a validated page command, both on the page's first valid report and on every change, including while the page is covered or loading.
- MUST move Add Repository off ⌘O, bind ⌘O, ⇧⌘O, and ⌘P as menu items, and route ⌘P to the content page through a page command that focuses the content view.
- MUST remove the repository sidebar, its drag reorder, and the persisted `sidebarCollapsed`, and give the repository context-menu actions a new home (US9).

### Finders

- MUST serve ⌘P from a finder endpoint documented in `openapi.yaml`, with regenerated client types. It scores an in-memory catalog of every note and code file (display title, vault-relative path, type, kind) that is built from Rhizome's own index. That keeps it within the indexer's ignore rules, without a second filesystem walk or watcher, and it is refreshed on the index's change events.
- MUST score with fzf's matching algorithm (Go, MIT), with boundary and word-start bonuses and typo tolerance, rather than prefix candidates, so subsequence and transposed-character queries still match. The FFF toolkit was considered and rejected on 2026-10-25: it matches paths but not titles, it would keep a second index and watcher of the same tree, and it would add a Rust library to the Go build.
- MUST record opens for frecency per checkout, and apply frecency only to break close scores, so a strong match is never buried under a frequently opened weak one.
- MUST cancel superseded finder requests and never let a slow response replace a newer one.
- MUST make every finder keyboard-first: focus starts in the input, ↑ and ↓ move, Enter opens, Esc closes and returns focus to where it was, and the list is an ARIA listbox with an active descendant.

### Note editing fidelity

- MUST trim the leading blank lines (and a CRLF section's trailing carriage returns) only in what the section editor displays. The edit's `previousMarkdown` stays the raw block markdown, so the server's block matching and surrounding-whitespace restoration keep files byte-identical. The test is that an edit followed by a save leaves every other byte unchanged.

### Accessibility and verification

- MUST meet WCAG 2.1 AA for text, muted text, and focus in all five themes in both appearances. A checked-in contrast test covers every theme token pair.
- MUST keep F6 region cycling working across the renamed regions.
- SHOULD add screenshot goldens for the Effort notes table at 1100 and 1440 in Graphite light and dark, and for the desktop-mode shell.

### Documentation

- MUST rewrite `DESIGN.md` (palette, themes, type scale, layout rules, components) and regenerate `.impeccable/design.json`, and update the Barlow line in `PRODUCT.md`'s brand commitments.
- MUST amend SPEC-0090, SPEC-0113, SPEC-0119, and SPEC-0125 where this spec changes their contracts, and update `desktop/README.md` and the custom-views skill templates.

## Open Questions

- Resolved 2026-10-25: the ⌘O and ⇧⌘O finders draw in a dedicated finder webview layered above the content view. It is transparent outside the palette so it can show a scrim and shadow, and its capabilities and channel are scoped like the shell's. A spike proves it first; the fallback is a band that drops from the title bar and temporarily pushes the content view down. Rendering the finders inside the content page is ruled out, because the page and its same-origin custom views would receive the user's other repository names and paths.
- Resolved 2026-10-25: ⌘P finds notes and code files. Notes rank first, and a code file opens in Explorer.
- Resolved 2026-10-25: Add Repository gets no shortcut once ⌘O moves. It stays in the File menu and at the foot of the repository finder.
- Resolved 2026-10-25, delivery: the finder spike succeeded, so the overlay webview (`shell-finder-N`) shipped and the drop-down band fallback was not needed.
- Resolved 2026-10-25, delivery: the desktop settings file is the lenient `preferences.json`, which already holds the app's other preferences, rather than a new file.
- Resolved 2026-10-25, delivery: ⌘P scoring pins `github.com/junegunn/fzf` v0.57.0 and vendors only `src/algo` and `src/util`.
  - **When typo variants run:** only when the direct query matches fewer than 10 items and has 3 to 24 runes.
  - **What a variant is:** one adjacent pair swapped, or, from four runes, one rune dropped.
  - **What they match:** titles and file names, never full paths.
- Resolved 2026-10-25, delivery: the View popover's Sort shows and sets the first sort key only. For a view whose YAML sorts by several keys, reversing the direction keeps the later keys, and choosing another field replaces the sort with that one key.
- Resolved 2026-10-25, delivery: a reading measure.
  - **Width:** the note reads in a 680px column, where tables, code, and properties may use the full width. Prose (paragraphs, lists, block quotes) caps at 540px inside it, which holds 66–79 characters (72 on average) of 15px Inter. The section editor's lines wrap at the same width.
  - **Disclosure chevron:** it sits in the gutter beside a section's title.
  - **Home line:** a note's first body block reads as quiet metadata when it is a single line of a short label followed only by links.
- Resolved 2026-10-26, delivery: "toggling Edit does not move the text" (US11) holds within a section: a section's prose keeps its position relative to its heading. The note body can still shift, because Edit reveals every property, including the empty ones read mode hides, as form controls.
- Resolved 2026-10-26, delivery: below 1200px the narrow-window rule (US5) wins over opening on selection (US6). Selecting a row does not open the right pane there; `]` or the tab row's toggle opens it over the content.
- Resolved 2026-10-26, delivery: column heads ship at 13px and labels at 12px, the nearest steps on the type scale, instead of 12.5px.
- Resolved 2026-10-26, delivery: the screenshot requirement is met with reference captures in `.impeccable/references/workbench-2026-10/`, not asserted tests, because the repository has no pixel-diff layer and CI renders fonts differently. The desktop-mode shell has no capture: windows launched from this session never paint into captures.
- Resolved 2026-10-25, delivery: a page's first paint in Rhizome Desktop uses the theme baked into its window when it opened, unless that page's origin remembers a newer theme command (a reload after a change). A first load of another runtime origin in a window opened before a theme change can still show the older theme until the page reports its title.

