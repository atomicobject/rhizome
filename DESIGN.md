---
name: "Rhizome"
description: "Dense, calm professional workbench for structured code and markdown context."
colors:
  primary-red: "#fd4f57"
  primary-red-hover: "#e63e46"
  primary-red-deep: "#c8313a"
  primary-red-soft: "#ffeaec"
  primary-red-line: "#fccfd2"
  primary-red-ink: "#9c1d24"
  primary-red-dark: "#ff5a62"
  focus-ring-dark: "#ff6b72"
  connection-teal: "#16cbc4"
  connection-teal-soft: "#d8f7f5"
  connection-teal-ink: "#0b6a66"
  link-dark: "#75b9b5"
  success-green: "#2f8a50"
  success-green-ink: "#1e6b3b"
  success-green-soft: "#e4f3e8"
  success-green-dark: "#4cb873"
  success-green-ink-dark: "#7fd69c"
  info-blue: "#3295bd"
  warning-gold: "#dcad66"
  warning-soft: "#fbefd4"
  warning-ink: "#8a5a0f"
  ink: "#1f1e1d"
  ink-muted: "#5b5955"
  ink-faint: "#6b6965"
  surface: "#ffffff"
  surface-tint: "#f5f5f3"
  surface-strong: "#ebeae7"
  border: "#e3e2de"
  border-strong: "#cfcdc8"
  border-faint: "#edece9"
  ink-dark: "#ecebe9"
  ink-muted-dark: "#b8b5b0"
  ink-faint-dark: "#9c9994"
  bg-dark: "#151413"
  surface-dark: "#1c1b1a"
  surface-tint-dark: "#242321"
  surface-strong-dark: "#2d2c2a"
  border-dark: "#33312f"
  border-strong-dark: "#45433f"
  chrome-text: "#f4f3f1"
  graphite-chrome: "#45433f"
  graphite-top: "#3b3936"
  graphite-muted: "#cbc7c1"
  graphite-pill: "#f1efec"
  graphite-pill-text: "#2b2a28"
  graphite-chrome-dark: "#2b2a28"
  graphite-top-dark: "#232220"
  slate-chrome: "#3d4654"
  spruce-chrome: "#2f4a47"
  oxblood-chrome: "#573538"
  indigo-chrome: "#3c3d63"
  white: "#ffffff"
typography:
  display:
    fontFamily: "Merriweather, Georgia, Times New Roman, serif"
    fontSize: "24px"
    fontWeight: 900
    lineHeight: 1.18
    letterSpacing: "-0.015em"
  headline:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.01em"
  section:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "18px"
    fontWeight: 600
    lineHeight: 1.35
    letterSpacing: "-0.005em"
  section-heading:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "15px"
    fontWeight: 600
    lineHeight: 1.35
    letterSpacing: "0"
  title:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.005em"
  prose:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "0"
  body:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: "0"
  secondary:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "13px"
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: "0"
  label:
    fontFamily: "Inter, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "13px"
    fontWeight: 500
    lineHeight: 1.35
    letterSpacing: "0"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, Menlo, Consolas, monospace"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "0"
rounded:
  sm: "3px"
  base: "4px"
  md: "6px"
  lg: "10px"
  palette: "14px"
spacing:
  xxs: "4px"
  xs: "6px"
  sm: "8px"
  md: "10px"
  lg: "14px"
  xl: "18px"
  xxl: "24px"
  xxxl: "32px"
components:
  button-primary:
    backgroundColor: "{colors.primary-red-deep}"
    textColor: "{colors.white}"
    typography: "{typography.secondary}"
    rounded: "{rounded.base}"
    padding: "4px 10px"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.secondary}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "28px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink-muted}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0 8px"
    height: "30px"
  input-find:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0 10px"
    height: "30px"
  chip-type:
    backgroundColor: "{colors.surface-tint}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
    padding: "1px 6px"
  sidebar-row:
    backgroundColor: "{colors.graphite-chrome}"
    textColor: "{colors.graphite-muted}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0 10px 0 8px"
    height: "30px"
  sidebar-row-active:
    backgroundColor: "{colors.graphite-pill}"
    textColor: "{colors.graphite-pill-text}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    height: "30px"
  table-row:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    padding: "0 10px"
    height: "36px"
  finder-palette:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.palette}"
    width: "640px"
  segmented-track:
    backgroundColor: "{colors.surface-tint}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.secondary}"
    rounded: "{rounded.md}"
    padding: "2px"
    height: "28px"
  segmented-selected:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.secondary}"
    rounded: "{rounded.base}"
    padding: "0 9px"
    height: "22px"
  link-quiet:
    textColor: "{colors.connection-teal-ink}"
    typography: "{typography.body}"
---

# Design System: Rhizome

## 1. Overview

**Creative North Star: "The Structured Workbench"**

Rhizome is a precise professional workbench for structured markdown, code context, ontology, and agent collaboration. It is not a consumer notes app and not an AI chat wrapper. Developers and technical leads use it mid-task, so its surfaces stay dense while exposing provenance, relations, status, and next actions.

The shipped workbench follows a Slack-like model. One soft, tinted chrome colour carries the top bar and the sidebar; the work sits on a white content panel inset from the window with a rounded corner; headers stay quiet and keep rarely used controls one click away; and fuzzy finders (⌘P, and in Rhizome Desktop ⌘O and ⇧⌘O) replace long navigation lists. Five chrome themes, each with light and dark appearance, change the chrome only. Content tokens are shared by every theme and vary only by appearance.

The identity stays Atomic Object's: warm near-black ink, AO red for focus and risk, teal for connection, Merriweather for document titles, and JetBrains Mono for identifiers, paths, and code. Inter carries the working UI. The compact red diamond in the top bar remains a temporary app-shell marker; the runner-r mark in `docs/brand/` is the reviewed identity.

The supported product surface is desktop web and Rhizome Desktop. Optimize for 1100 to 1920px working widths; narrower windows reflow where layouts naturally allow it.

**Key Characteristics:**

- Dense but calm: a 14px UI base, 30–36px rows, and progressive disclosure instead of stacked control bands.
- One tinted chrome layer, white work surfaces, one tinted neutral, and hairlines.
- Typography that separates roles: Merriweather for document titles, Inter for UI, prose, and every number, JetBrains Mono for identifiers, paths, and code.
- Red is rare; selection is a neutral chrome tint, never red.
- Status always pairs colour with shape, text, or position.

## 2. Colors

Neutral surfaces carry the interface, the theme's chrome frames it, red appears only for focus, the one commit action, and real risk, and teal marks relationships and provenance.

Every colour in `web/src/tokens.css` is a `light-dark()` pair resolved by the root `color-scheme`. Appearance follows the operating system unless the root carries `data-appearance="light"` or `"dark"`; Rhizome Desktop sets the app appearance natively. The frontmatter lists light values with `-dark` keys for the dark counterparts that matter. Components read semantic tokens (`--text`, `--surface`, `--border`, `--accent`, `--link`, `--chrome-*`), never the `--ao-*` brand values, or they would not switch appearance.

### Primary

- **AO Red** (`primary-red`; `primary-red-dark` in dark): the `--accent` token. Marks, risk dots, problem counts' shapes, and the brand diamond. White on it is only 3.3:1, so it never fills a labelled control.
- **Deep Red** (`primary-red-deep`): `--accent-fill` and the light `--focus-ring`. Fills the one commit action (Save, Run, the GraphiQL execute button) at 5.3:1 with white, and draws every keyboard focus ring. Dark focus uses `focus-ring-dark`.
- **Red Ink** (`primary-red-ink`): red text such as problem counts on light surfaces and `--syntax-tag`.
- **Red Soft and Line** (`primary-red-soft`, `primary-red-line`): risk backgrounds and borders (error boundaries, refused changes) and text selection highlight. `primary-red-line` is also `--chrome-risk`, the problem count on the chrome.

### Secondary

- **Connection Teal** (`connection-teal`): relation lines and the linked chart series.
- **Link Teal** (`connection-teal-ink`; `link-dark` in dark): `--link`, the colour for prose links and explicit action or navigation links, with a 45% teal underline (`--link-line`) that goes solid on hover.
- Dark link teal is `#75b9b5`: WCAG sRGB relative-luminance contrast is **7.67:1** on `--surface` (`#1c1b1a`) and **7.00:1** on `--surface-sunk` (`#242321`), above AA 4.5:1. The quieter tone also supplies the dark `--link-line` tint.
- **Teal Soft** (`connection-teal-soft`): provenance backgrounds.
- **Success Green** (`success-green`, `--success`; `success-green-dark` in dark): the success status mark (the filled dot, the ✓ ring), 3:1 on every surface. `--success-ink` (`success-green-ink`, `success-green-ink-dark`) is success text and clears 4.5:1; `--success-soft` (`success-green-soft`) is a success background. Success is never teal, so a passing check never reads as a link.

### Tertiary

- **Info Blue** (`info-blue`): the in-progress status mark and informational tints.
- **Warning Gold** (`warning-gold`, `warning-soft`, `warning-ink`): staged (unsaved) edits everywhere, the dirty tab dot, warnings, and the connection banner. Gold means "changed, not yet saved", never "problem".
- **Type palette** (`--type-1` … `--type-13`, each with an `-ink` text variant): twelve categorical hues for ontology types in graphs, Atlas cards, and type dots, plus `--type-8`, the warm-gray neutral that interfaces and essential edges use. Every hue clears 3:1 against every surface in its theme, and neighbouring slots differ in hue and lightness; never red or teal. Types take hues in name order, types with records first, so a vault's types collide as late as possible; a name the ontology does not list falls back to a hash. Colour is always paired with a label, legend, or shape.
- **Series palette** (`--series-1` … `--series-8`, `--series-link`): chart colours, neutral first, no red, teal only for linked amounts.

### Neutral

The content area uses white plus one tinted neutral per appearance, separated by clear lightness steps.

- **Ink** (`ink`; `ink-dark`): primary text and structural strokes. A warm near-black, not pure black.
- **Muted and Faint Ink** (`ink-muted`, `ink-faint`; dark counterparts): metadata, column heads, labels, counts, and placeholders. `ink-faint` is the lightest text allowed; it still clears 4.5:1 on the tint.
- **Surface** (`surface`; `surface-dark`): the content panel, tables, the right pane, popovers, and the finder.
- **Tint** (`surface-tint`; `surface-tint-dark`): one value behind `--bg`, `--bg-tint`, `--surface-alt`, and `--surface-sunk`. Row hover, board columns, filter bands, code backgrounds, the editor's hover state, and loading placeholders.
- **Strong Tint** (`surface-strong`; `surface-strong-dark`): count badges on tool buttons.
- **Borders** (`border`, `border-strong`, `border-faint`; dark counterparts): `border` separates regions and outlines controls, `border-strong` outlines inputs, segmented controls, and overlays, and `border-faint` separates table rows and the properties block.

### Chrome themes

A theme sets only the chrome tokens on `html[data-theme]`: `--chrome-bg` (sidebar and inset frame), `--chrome-top` (top bar, one step darker), `--chrome-text`, `--chrome-muted`, `--chrome-pill` with `--chrome-pill-text` (the active sidebar row), and the derived `--chrome-raised` (hover, 9% text mix) and `--chrome-active` (current top-bar tab, 16% mix). Selection (`--selection`, `--selection-edge`) is tinted from the chrome, so it changes with the theme.

- **Graphite** (`graphite-chrome`, `graphite-top`, default): warm charcoal. Dark appearance deepens it (`graphite-chrome-dark`, `graphite-top-dark`) and turns the pill into a raised charcoal with light text.
- **Slate** (`slate-chrome`): cool blue-gray.
- **Spruce** (`spruce-chrome`): deep green-teal.
- **Oxblood** (`oxblood-chrome`): muted dark red. It is the chrome, not a risk signal; risk in the chrome stays `--chrome-risk`.
- **Indigo** (`indigo-chrome`): muted blue-violet, never a purple-on-white gradient.

Every theme clears 4.5:1 for chrome text and muted chrome text in both appearances; `web/src/lib/themes.test.ts` checks every pair.

### Named Rules

**The Red Rarity Rule.** Red is for focus, the one commit action, and real risk. A normal screen has red on only a few high-signal elements; a selected row, a chosen enum value, and an active tab are never red.

**The Teal Means Connection Rule.** Teal means a link, a relationship, or provenance. It is never decoration and never a status: success is green (`--success`), and people (assignees) are ink, not links.

**The One Validation State Rule.** A clean validation result reads "✓ No issues" everywhere it appears (the Problems status line, Briefing, strips), once per screen, with the ✓ in `--success` and the words in `--success-ink` or ink. A result with problems reads as a count, "3 issues". Never "No issues found", "0 issues", "Valid", or a second clean badge on the same screen.

**The Chrome Owns the Theme Rule.** Themes recolour the top bar, sidebar, and selection only. Content surfaces never take a theme colour, so every theme reads the same inside the panel.

**The One Tint Rule.** The content area uses white, one tinted neutral, and hairlines. Do not introduce a third near-white; adjacent surfaces must differ by a visible step.

## 3. Typography

The three families are self-hosted (`web/public/fonts`, `font-display: swap`): Inter as variable woff2 subsets, JetBrains Mono, and Merriweather. The host, every custom-view frame, and the desktop shell load the same files, with no third-party font requests.

**Display Font:** Merriweather, Georgia, Times New Roman, serif
**Body Font:** Inter, -apple-system, system-ui, Segoe UI, sans-serif
**Label/Mono Font:** JetBrains Mono, ui-monospace, Menlo, Consolas, monospace

**Character:** Merriweather gives a note its editorial title; Inter carries the working UI and note prose with neutral, legible density; JetBrains Mono marks identifiers, paths, schema fields, GraphQL, and code, so evidence reads as evidence. Counts, dates, and ages are Inter.

### Hierarchy

The scale is fixed: `--fs-xs` 12, `--fs-sm` 13, `--fs-base` 14, `--fs-md` 15, `--fs-lg` 16, `--fs-xl` 18, `--fs-2xl` 24. Chrome uses weights 400, 500, and 600; 700 is reserved for view titles and 900 for Merriweather.

- **Display** (Merriweather 900, 24px, 1.18): the note title, and nothing else.
- **Headline** (Inter 700, 18px, 1.2, -0.01em): every view title in its header row: collections, Problems, Agent, Tasks.
- **Section** (Inter 600, 18px, 1.35): H2 in a note. H1 inside a body is 24px; H4–H6 drop to 15px muted.
- **Section heading** (Inter 600, 15px, 1.35, `--section-font` in `--section-color`, with `--section-gap` 10px below; `.section-heading`): a block or section title in a view body (Briefing blocks, Overview sections, grouped lists, a pane's section). It outranks the 14/500 row titles under it, which outrank 13px metadata.
- **Title** (Inter 600, 16px, 1.25): H3 in a note, the right pane's record title, and the sidebar's repository or vault name.
- **Prose** (Inter 400, 15px, 1.6): note body text, held to a 540px measure (65–75 characters).
- **Body** (Inter 400, 14px, 1.45): the UI base: table cells, sidebar rows, tabs, controls, and popovers. Title cells use 500.
- **Secondary** (Inter 500, 13px): column heads, document tabs, property and record labels, tool buttons, and the right pane's tabs.
- **Label** (Inter 500, 13px, 1.35, `--label-structure`, also exposed as `--label-font`, with `--label-color`): the one style for rail group heads, column heads, the Properties heading, finder group headings, and fact labels; never a block heading over rows, which takes the section heading. Sentence case, no tracking, no uppercase eyebrows.
- **Mono** (JetBrains Mono 500, 12px): identifiers, paths, code, and kbd hints, nothing else. Counts, dates, ages, and stats strips are Inter with tabular figures (`.num`), never mono. `code`, `kbd`, `samp`, and `pre` default to JetBrains Mono globally, so mono never falls back to the browser's Courier.

### Named Rules

**The Product Type Rule.** Inter owns the working UI and note prose. Merriweather appears only as a note's document title; view and workspace titles are Inter 18/700. Neither font appears in the other's role.

**The Scoped Figures Rule.** Tabular figures (`font-variant-numeric: tabular-nums`) apply only to number-bearing elements: counts, dates, ages, ids, and numeric cells. `.num` (base.css) sets Inter with tabular figures for any number or date outside a styled cell. Never set them globally; Inter widens the hyphen under `tnum`, which breaks titles and prose.

**The Fixed Scale Rule.** Product type uses the fixed token sizes, not viewport-fluid scaling, so dense tools stay predictable at every window width.

**The Human Label Rule.** Outside the Ontology Schema tab and code, show types and fields by their human display labels in sentence case ("Acceptance criteria", "Technical spec", "Effort note", "Next step"), never raw identifiers (`AcceptanceCriterion`, `featureAreas`): type `label`/`pluralLabel`, humanized field names, enum labels, and note titles with their Markdown stripped. An exact identifier appears only as secondary mono evidence where a reader inspects schema, and otherwise stays in `title` attributes. The helpers live in `web/src/lib/labels.ts` and `useTypeLabel`.

**The Structural Label Floor.** Sub-group headers, sidebar sublabels, rail sections and groups, legends, and column headers are at least 13px/500. Use `--label-structure: 500 13px/1.35 var(--font-sans)`. 12px is reserved for mono paths, IDs, and data ticks such as chart axes. Sidebar rows share one 16px leading slot: saved views use their table icon, types their shared palette dot, and groups their disclosure chevron. Sibling labels align in one column; nested types retain their hierarchy indentation. An empty type row is hidden only when a saved view with the same display name exists in its group.

**The Section Outranks Rows Rule.** A block or section heading is 15/600 with 10px below (`--section-font`, `--section-gap`, `.section-heading`); row titles under it stay 14/500 and metadata 13px. A 12px label never heads a block of rows.

**The Mono Evidence Rule.** Use JetBrains Mono only for identifiers, paths, and code: ids, file paths, schema fields, commands, query fragments, and code references.

**The Numbers and Dates Rule.** Counts, absolute dates, relative times, intervals ("3d", "in 6d", "27h ago", "every 7d"), and stats strips are Inter with tabular figures (`.num`) on every surface, never mono.

**The Plain Reader Rule.** Readers never see developer language: schema setup hints, internal field names, and system diagnostics (versions, accounts, probe output) sit behind an info disclosure, with at most one plain sentence visible. An empty cell or value is a muted "—".

**The Distinguishing Truncation Rule.** Truncation keeps the part that tells items apart: paths and ids truncate in the middle, record titles take two lines before they cut, and prose cuts at a word boundary with "…". A path's directories give way before its filename (the finder splits them into a shrinking directory span and a filename span).

**The Empty Data Recedes Rule.** A column whose values are all empty is hidden, not shown as a column of dashes. A scattered empty cell or value is a muted "—" at reduced emphasis (`.empty-dash`, faint ink, weight 400).

## 4. Layout

**Shell.** A 44px top bar on `--chrome-top` holds the brand (never a vault name, so nothing shifts between sections; the sidebar, or GraphQL's view header, names the vault), the sidebar toggle, the section tabs (Notes, Ontology, Explorer, Agent, Tasks, GraphQL), a centred global search up to 560px wide, and the tools. Below it every section is the same workbench on `--chrome-bg`: the themed sidebar, then the content panel, inset 6px from the right and bottom with a 10px corner. With the sidebar hidden, the panel insets 6px on the left too.

**Resizable panes.** The sidebar ranges from 200 to 400px (264 default) and never takes more than 45% of the window. The right pane ranges from 280 to 560px (340 default). Each has an 8px drag handle on its inner edge that shows a 2px line on hover and a red line on keyboard focus; it is a `role=separator` that moves in 16px steps with the arrow keys and jumps to its bounds with Home and End. Widths and collapsed state persist per browser; the sidebar's are shared by every section and change only when the user changes them, never on search or a section switch.

**One right pane.** The right pane hosts Record for a selected table or board row, and Info, Outline, and Graph for an active note. Its 40px header matches the note's control bar. With nothing selected on a collection it is closed; `]` toggles it. Below 1200px wide it opens over the content with an overlay shadow instead of narrowing the table.

**Content panel.** In Notes, every page (All notes, a type, a group, a configured view such as Action Items, Problems, Changes, and Search) opens under the page header, and there is one page at a time: opening a view, Problems, or a search from the sidebar, the top-bar search, or another view replaces the current page instead of adding a tab, and Back returns to the page before. The 36px document tab strip is for notes only: it shows only while a note tab is open, with the current page in the first slot (Home's house icon, or the view's, Problems', or the search's name) ahead of the note tabs, and Edit and the right-pane toggle at the right. Each control shows only where it does something: Edit on note tabs and on collection presentations (Table, Cards, Board), or while an edit session is open so it can be saved or discarded; the pane toggle on note tabs and on presentations with a record pane (Table). Briefing, Overview, Problems, and Search show neither. With no note tab open the strip hides and its controls sit at the right end of the page header's title row (`.notes-shell__frame--solo`), so every page's title is at the same height under the top bar (y≈71 at 1440px), sibling sidebar items look alike, and only opening a note brings the strip back.

**Collections.** A configured view's header is two rows: a 56px title row (title, count, a truncating description, the schema link, Find in view, Filter, and View) and a 40px row of presentation tabs. The table's 34px column heads begin directly under them. With other tabs open, under the content panel's 36px tab strip, the first data row of an ungrouped table sits 166px below the panel's top edge, and a grouped table adds its 44px group header. Below a 760px header width, the description, facts, and kbd hint drop out before anything wraps.

**Table column policy.** Every column except the title has a fixed width and never takes a share of extra space: ids 176px, enums and short text 136px, relations 220px, people 160px, dates 116px, due dates 188px (room for an Overdue label), ages 92px, numbers and flags 72px, plus a 30px select cell. The title takes all remaining width and never drops below 320px. When the columns cannot fit at those minimums, relation columns leave first, then ids, then dates; they show in the Record pane, and the footer names them. Rows are 36px in one-line density and 56px in two-line density with the summary.

**Reading and data surfaces.** A note is a capped reading surface: a centred 540px column for prose, properties, and body blocks, while the note's control row runs edge to edge with the panel gutter, as an HTML note's does. The built-in views are capped and centred on wide screens: Briefing in one centred column of about 1320px and Overview in one of about 1520px, shared by the header, tabs, status line, and body so all four start on the same edge. Data surfaces fill the width with bounded columns: tables stretch, and board columns are fixed tracks of 260–360px shared by header and cards, so a wider window shows more columns rather than wider ones. Empty board values are 140px strips above dashed drop lanes; populated columns keep their tracks when switching card density. Cards fill an auto-fill grid with a 260px minimum.

**Note rhythm.** Sections separate by space, not rules: 28px above an H2 and 24px above an H3, 8px below any heading, and paragraphs 0.75em apart (about 11px), well under the section gap. The note's lead line of links reads as 13px muted metadata above the body.

**Content gutter.** `--gutter` (22px) is the one left inset inside the content panel: every view header, tab row, status line, banner, table's first column, and composer starts there. Screens never pick their own left edge.

**Spacing scale.** `--sp-1` to `--sp-8`: 4, 6, 8, 10, 14, 18, 24, 32px. Rows and controls use the small steps; section rhythm uses 24 and 28.

### Named Rules

**The Title Never Yields Rule.** In a table, supporting columns leave before the title shrinks below its minimum.

**The Reading Measure Rule.** Note prose caps and centres, and Briefing and Overview cap and centre their whole column (about 1320 and 1520px). Tables, boards, and cards fill the panel. Never stretch prose or a briefing across a 1920px window.

## 5. Elevation & Depth

Rhizome is flat by default and layered by structure. Depth comes from the chrome around an inset panel, hairlines, sticky headers, and the one tint before it comes from shadow. Shadows mark only things that float above the work.

### Shadow Vocabulary

- **Resting Surface** (`none`): the content panel, tables, panes, sidebar rows, board columns, and cards at rest.
- **Low Lift** (`--shadow-sm`, `0 1px 2px rgba(42, 39, 36, 0.06)`): compact controls that need tactile definition.
- **Overlay Lift** (`--shadow`, `0 1px 3px rgba(42, 39, 36, 0.08), 0 2px 6px rgba(42, 39, 36, 0.04)`): menus, popovers, and editor autocomplete.
- **Command Lift** (`--shadow-lg`, `0 10px 28px rgba(42, 39, 36, 0.1)`): the ⌘P finder, the Appearance panel, and the right pane when it overlays the content below 1200px.
- **Scrim** (`--scrim`, ink at 32% in light and black at 55% in dark): behind the ⌘P finder. The desktop ⌘O and ⇧⌘O finders use it at 65% strength so the page stays readable beneath.

In dark appearance every shadow switches to black at 30–60% through `light-dark()`.

### Named Rules

**The Flat Workbench Rule.** Working surfaces are flat at rest. Use the chrome, spacing, hairlines, or the tint for hierarchy before shadow.

**The Overlay Only Rule.** Large soft shadows belong to transient overlays only.

## 6. Shapes

Corners are restrained and technical: 3px (`--r-sm`) for chips, kbd, and tab close buttons; 4px (`--r`) for segmented controls and the edit toggle; 6px (`--r-md`) for buttons, sidebar rows, inputs, board columns, and the editor's focus box; 10px (`--r-lg`) for the inset content panel and popovers; 14px for the finder palettes. Find in view uses 7px. Underlines carry selection in tab rows: a 2px ink bar under the active presentation, document tab, or right-pane tab. Status marks are 9px geometric shapes (ring, half-filled ring, filled dot, dashed ring, triangle, rotated square).

## 7. Components

### Brand Mark and Top Bar

The top bar is the theme's `--chrome-top`. The brand is a 7px AO-red diamond and "Rhizome" in 15/600 mono; the diamond is a temporary marker, not the identity. The sidebar toggle, a 30px panel-left icon button in muted chrome text, sits just before the section tabs; every section, GraphQL included, has a sidebar. Section tabs are 30px, 14/500, muted chrome text; hover raises them to `--chrome-raised`, and the current section sits on `--chrome-active` with full chrome text. The global search is a 28px field on `--chrome-raised` with a ⌘K hint; its placeholder mixes 20% chrome text into muted chrome text to clear 4.5:1 on the raised field. Right of the search, in every section and at every supported width (the search narrows first; only the narrow wrapped layout below 760px drops them), the vault's counts read "346 typed notes · 13 note types" in muted chrome text with 600 Inter tabular figures: typed notes, and note types as Overview counts them (embedded types such as user stories are not note types). The Appearance control (a 28px icon button) opens a 360px panel with Match system, Light, and Dark as a segmented control and the five themes as swatches that preview their chrome around a white panel.

### Sidebar

The sidebar sits on `--chrome-bg` and is one frame for every section: it names the vault or repository (16/600), and only its contents change. Notes holds All notes, Recently changed, and Problems, then Views, then the type tree in collapsible groups; Ontology holds an Atlas row and the same type tree; Explorer holds the vault tree; Agent holds the session list; GraphQL holds Docs and History, with Refetch schema, Keyboard shortcuts, and Settings at its foot; Tasks holds All tasks and one row per registered task with its next due time, or, when its last run failed, the risk mark and the word "Failed" in `--chrome-risk`. A type group that also holds saved views names each set with a 13/500 muted "Views" and "Types" subheading (`.notes-left-rail__subhead`), since a view is often named after the type it shows. Rows are 30px, 14px, muted chrome text with 16px icons; hover uses `--chrome-raised`, and the current row is a light pill (`--chrome-pill`, 600 weight, `--chrome-pill-text`). Counts sit at the right in 13px tabular figures; a problem count turns `--chrome-risk`. Focus inside the sidebar is a 2px `--chrome-text` ring inset on the row. A section with a plain list (Tasks, GraphQL) uses the shared `.sidebar-nav` classes (`__head`, `__list`, `__row`, `__name`, `__aside`, `__icon`, `__list--foot`) from `workbench.css`. Buttons and selects on the sidebar are chrome-tinted (`--chrome-raised`, chrome text), never bright surface boxes. There is no note list; ⌘P finds notes.

### View Header

Every screen in the content panel opens with the same header (`.view-header` and `.view-header__title-row`, from `ViewHeader` or its classes): a 56px row on the content gutter with the Inter 18/700 title (`h2`), the count (`.view-header__count`, 14px muted tabular), a one-line 13px muted description that truncates first (`.view-header__about`), and actions at the right (`.view-header__tools` for a collection's portalled tools, `.view-header__actions` for a screen's own buttons). A header without tabs closes with a `border` hairline. Never put an eyebrow, a Merriweather title, or a second line of facts above or below it; facts join the description with " · ". A nonzero problem count adds `.is-risk` (red ink, 600). On Problems a zero count is omitted, since "No issues" in the status line already says it. The status line under the header reads as one phrase, mark, result, "Checked 1h ago", and the Check details disclosure, then Refresh validation 16px after the freshness text it renews, never pushed to the far edge. A clean result reads "✓ No issues · 3 checks in 1.1s · Checked 1h ago" and lists the checks that ran in place of the disclosure, one 34px row each that leads with the check's name (Ontology, Identifiers, Broken links), then a compact `--success` ✓ and the muted result ("579 identifier nodes checked"), so the panel never holds a lone row.

Diagnostics a reader rarely needs (a harness version and account, a primary ref, commit hash, and fetch time) sit behind one ghost "Details" disclosure at the right of the title row (`.header-details`, a `<details>` whose `<dl>` opens as a small popover), never in the description. The description stays one plain sentence about the page.

### Buttons

The classes live in `base.css`; use them rather than restyling a button per screen.

- **Primary (`.button.button--primary`):** Deep Red (`--accent-fill`) with white text, 13/600, for the one commit action on a surface (Save, Run, Send, execute). Hover deepens to `--accent-fill-hover`. GraphiQL's execute button is a 28px primary labelled "Run ⌘↵".
- **Secondary (`.button`):** 28px, white surface, 1px `border-strong`, 6px corners, 13/500 ink. Toolbar actions, Refresh validation, Pause all, Dismiss, pagers, and the 28px icon-only refresh share these metrics. Hover tints to `surface-tint`, never red. Routine actions are never red, as text or fill.
- **Ghost (`.button.button--ghost`):** Filter, Display (the view's display options, distinct from the saved Views menu), the pane toggle, Copy link, Copy beside a command, an HTML note's Refresh and Open, and close buttons are transparent with muted text until hover or open, when they take the tint and full ink.
- **Disabled:** `surface-sunk` with `border` and faint ink, never a pale red.
- **Edit toggle:** 13/600 muted text with a 1px border and 4px corners; in a session it becomes Editing, Review, Save, and Done.
- **Segmented control (`.segmented`):** every mode switch (Light / Dark / Match system, enum pickers in edit mode, the Display popover's "1 line | 2 lines", a task's mode) is one tinted track (`surface-sunk`, 1px `border`, 6px corners, 2px inset) holding 22px segments in 13/500 muted text, so the control is 28px tall like a button. The chosen segment is a raised surface: the panel surface (one step lighter, `surface-strong`, in dark), 1px `border-strong`, 4px corners, and ink 600 text. Never a solid ink fill, and never red.

### Chips, Links, and Counts

- **Type chip (`.type-chip`):** a note's type is one neutral chip: tint background, `border`, 13/500 muted text, 3px corners. An untyped note gets a dashed border. Types never borrow red.
- **Note links:** in prose, teal ink text with a translucent teal underline that goes solid on hover. No chip backgrounds.
- **Quiet links (`.link-quiet`):** in dense lists, tables, rails, banners, and boards, explicit actions ("Review failed run") are teal ink with no underline until hover or keyboard focus. Record titles add `.link-title`: ink at weight 500, with a teal underline offset 2px on hover and keyboard focus. Trace, briefings, rails, and tables all use it, so equivalent relationships look the same everywhere. Only prose keeps a resting underline.
- **One overflow link (`.list-more`):** a list that holds more than it shows ends with one "Show N more" inside the list, 13/500 quiet teal with no underline until hover or focus, aligned to the list's title column. Leaving the view is a separate "Open <view>" link. Never two overflow affordances for one list.
- **Counts:** unboxed, muted, Inter with tabular figures (`.num`). A count on a tool button sits in an 18px `surface-strong` pill. A problem count uses red ink with its label beside it, and a note with problems shows an 8px red dot in lists.

### Inputs / Fields

- **Find in view:** a 30px field, 220px wide (160px in a narrow header), `border-strong`, 7px corners, a search icon, and a ⌘F hint. Focus adds the red ring with 1px offset.
- **Form controls:** `base.css` styles every native `select` and text, search, number, date, and URL `input` app-wide at element specificity: 28px, `border-strong`, 6px corners, `--surface` with `--text` in either appearance, 13/500, a faint-ink hover border, and `surface-sunk` with faint ink when disabled. A single-value `select` drops the native arrow for a 16px chevron (`#7d7872`, 3:1 or better on light and dark surfaces) 7px from its right edge. Checkboxes and radios are 16px with `accent-color: var(--text)`, so a checked box is neutral ink, never red. Component classes win over the base treatment, so restyle only when a control genuinely differs, and keep the chevron. Labels name the setting in human words ("Reasoning effort", "Approval required"), never an identifier. On the sidebar a select takes the chrome tint of the top bar's search field (`--chrome-raised`, no border, chrome text, a light chevron, `--chrome-active` on hover), never a bright surface box; its open list keeps the content surface.
- **Focus:** one keyboard focus style everywhere: a 2px `--focus-ring` outline from `:focus-visible`, offset 2px, inset on rows, cells, and tabs. Components may inset or offset it, not restyle it. On the chrome the ring is `--chrome-text`.

### Collections and the View Header

A configured view is one result set with presentations chosen from text tabs (Briefing, Table, Cards, Board, and saved views); switching presentation never resets search, filters, or sort. The title row shows the 18/700 title, a 14px muted count, a 13px muted description that truncates first, the type or interface identity ("Interface: Effort") in the header's info tooltip, which links to its Ontology detail, then Find in view, Filter, and View. View opens a popover with Group by, Sort, Rows, Columns, Reset to shared (only while the view holds personal settings), and Save view. Active filters appear as removable chips in a tinted band under the header only while a filter is active. The footer is a status line: row count, collapsed groups, and any columns moved to the Record pane.

### Tables

Column heads are sticky, 34px, 13/500 muted, with an ink sort arrow. Rows are 36px with a `border-faint` hairline; two-line rows are 56px with a 13px muted summary under the 14/500 title. Group headers are sticky 44px bands with a caret, a status mark, the 14/600 group label, and a muted count. Hover tints the row; a selected row takes `--selection` with a 3px `--selection-edge` bar at its left, so it never reads as an error. Focus is one inset ring around the row. Dates, ages, and numbers are Inter with tabular figures; ids are 12px muted mono, on a second line under the title when a row names both. A title that opens a record is a quiet link (`.link-quiet`), and secondary relation columns use `.link-secondary` in muted ink; record titles use `.link-quiet.link-title` in ink at weight 500. An empty cell is a muted "—". Staged edits tint cells warning gold and mark the title with a dot. A grouped table hides the grouped field's column, and a column hides by default when it is empty or one value fills more than 90% of at least 10 rows; the footer names hidden columns and the Display menu shows them again. Exact nonempty repeats of the previous row's value in a sorted column are dimmed within each group, including Action Items. Group headers add the overdue count when nonzero ("Open 21 · 12 overdue"). Select checkboxes appear on hover or keyboard focus for every row.

### Board and Cards

Board columns are tinted bands without a border (6px corners) holding flat white bordered cards, in schema enum order. Empty values are 140px horizontal headers above visible dashed drop lanes at least 160px tall. Populated columns stay open; the per-column chevron switches card density and is labelled "Compact cards" or "Full cards". Columns with more than 24 cards default to compact cards. Staleness is a muted header line ("12 unchanged 30+ days") and a plain age on each card, never a gold pill or a clock on ages, and relation values on cards are muted secondary links (`.link-secondary`), so card titles lead. A move stages an edit until commit or discard, and every drag has a keyboard equivalent. Cards are for low-cardinality types: dense hairline cards in aligned rows of a 260px auto-fill grid, with summaries clamped to three lines. A group header is the label followed by its count ("Proposed 16").

### Status Marks

Status is a 9px shape plus a label driven by the schema tone: hollow ring for neutral, half-filled blue ring for in progress, filled green dot (`--success`) for success, gold triangle for warning, an 11px red disc with an exclamation cut out for risk (Failed, Needs attention, errors), and a dashed ring for muted. The risk mark is never a rotated square, which reads as the brand diamond. The host and the kit (`web/kit/statusMark.tsx`, used by every bundled view) draw the same shapes; with no schema tone, a mark falls back to its lifecycle position (done is the success dot, dropped the muted ring, anything moving the half ring). Colour supports the shape and never stands alone; where a mark has no visible label (a sidebar row), it carries `aria-label` and `title`. On the chrome, the risk mark takes `--chrome-risk`.

**Status per row only in mixed lists.** When a group header or section already states one status for everything in it ("Open 21", a "Done" column, "✓ No issues"), its rows omit the status mark; a row carries its own mark only in a list that mixes statuses. Problems' clean check list is the deliberate exception: each check keeps a compact ✓ so the reader sees every check passed, not merely that it ran.

### Banners

A condition that affects a whole screen (a new task, a paused queue, unavailable search sources, a refused ref) is one `.banner`: a 36px band on the content gutter with 6px corners, a status mark with its label, concise 13px text (`.banner__text`, `.banner__aside` for a muted time), and an optional 28px `.button` action at the right (`.banner__actions` for two). Tones: neutral tint by default, `.banner--warning` (warning-soft, gold border), and `.banner--risk` (red soft, red line). Repeated conditions merge into one banner whose details sit behind a disclosure; never stack several bands. A banner is never louder than the failure it sits above. On Tasks a failed run is told by its row: the risk mark in Last run, and in the Inbox cell one quiet action, "Review failed run", the only label a failed run's action ever takes. The risk banner is reserved for an aggregate, two or more failed tasks ("2 tasks failed" and their names); one failure never repeats in a banner. A new task's notice is no banner: the row's secondary line, after the task id, carries the neutral "New" chip (as `.type-chip`, never info blue) and a muted "What it does" disclosure (muted ink with the shared chevron, never teal) that opens the schedule and purpose, with Dismiss, in a full-width row below; the failed run's action stays the row's prominent one. The Tasks header's description is a plain scheduling summary ("Next run in 6 days · 1 failed run", "A task is due now", or "Paused"); the primary ref, commit, when the repository last fetched ("Fetched 2m ago"), and where each task's mode comes from (its task default or set here) sit behind Details. The Mode column names the mode alone.

### Right Pane

The pane has a 40px header with its 15/600 title or Info / Outline / Graph tabs (13/500, ink underline when selected) and a close button. Record shows the row's 16/600 title, a muted summary, and 30px field rows with 13/500 muted labels in a 120px column. The id sits on its own 12px mono line under the title with Open at the right, and an empty value reads "Not set" in muted ink with its edit control on hover or focus. Below the fields, Record shows the same Relations and Linked notes blocks as the note Info rail, without repeating the note identity. At 1100px the collection overlay still starts below its toolbar.

### Note Reader

A note opens in a centred 540px column. Above it, an edge-to-edge control row holds the type chip, the mono path (which truncates before any control does), the note's presentations (Structure, Markdown, and any saved view) as a segmented control with no default-view dot (the tooltip names the default), and Copy link as a ghost button with a link icon. The title is Merriweather 24/900. Properties sit directly under the identity line with no visible label (an sr-only heading keeps the landmark), capped at the 540px prose measure so their edges line up with the body, as a two-column grid with a 120px column of 13/500 muted labels and 14px values in 30px rows; a long text value (such as Summary) runs full width beneath its label, showing up to six lines in read mode; only overflow offers "Show full summary", followed by "Show less" when open. Edit mode shows the full field editor. There is no rule on every row; empty properties wait behind a "Show N empty fields" toggle, and a `border-faint` hairline closes the block. Body headings are Inter 600 with more space above than below; a collapsible section's 14px chevron sits in the gutter and shows on hover, on focus, and while collapsed. Prose is 15/1.6 at the 540px measure. Inline code and code blocks use the tint and mono.

### Section Editor

In edit mode each section body becomes an editor in the same place, with read mode's typography and measure, so toggling Edit does not move the text. It has no box at rest, takes the tint on hover, and draws the red focus ring (1px border plus 1px ring) only while focused. Its padding bleeds outward through negative margins. A changed block turns warning-soft with a gold border. Empty properties stay behind "Show N empty fields" in edit mode too, unless a field has staged changes or an issue, and identifier guidance sits behind an info mark, so entering edit mode barely moves the body. Dates are edited as ISO text (`2026-10-26`) with a calendar button for the native picker.

### HTML Notes

While an HTML note's preview shows, its host header collapses to one row (title, ID, status, the metadata disclosure, and ghost Refresh and Open) on the note reader's gutter, so the page's own heading is the only large title. Metadata opens as a floating panel. The authored HTML inside the frame is vault content and is never restyled.

### Search

Search uses the shared view header (title, count, and the query as the muted description) with its filters on one row and scope as a segmented control. Header, banner, status line, and results start at the 22px content gutter in a column capped at 960px; excerpts cap at 720px. The note column and Agent conversation stay centred. Degraded sources merge into one banner with the detail behind a disclosure. Code snippets are JetBrains Mono on the tint with whitespace preserved, and each result names what matched in muted text when the data says so.

### Editing States

Every surface shows an edit's state the same way. Editing with nothing staged shows a warning-soft "Editing" chip with warning-ink text and no dot; the control row keeps its neutral border. The labelled staged-change bar remains separate. Staged edits are warning gold everywhere: changed fields, section editors, table cells, cards, the tab dot, and the save bar. Only an error or conflict turns the save bar red, and its message floats below the bar. A change the server refuses is taken back out of the session, so the rest still saves.

### Finders

The ⌘P finder is a native `<dialog>` over the scrim: a 640px palette at most 92px from the top (12% of a short window's height), 14px corners, `border-strong`, and the command lift plus a deep 64px shadow that still separates it from a dark page, entering with a 140ms fade and 6px rise that reduced motion removes. A 56px input row holds a 16px query field; a 13/500 group heading leads a listbox of fixed 46px rows: the icon, then the 14/500 title across the row's full width, then a second line with the 12px mono path and the 13/500 muted type label, right-aligned in a fixed 112px column 12px after the path (a note with no type says "Untyped", so the column never reads ragged). A long path never crowds the label: its directories shrink first, ending in an ellipsis, and the filename stays whole. The list is sized to nine whole rows so the footer never cuts one. Each note row leads with its type's 10px hue dot (`typeAccentColor`, the slot it has in the Atlas and graphs; untyped notes take the `--type-8` neutral), and code rows keep the code-file icon, so the column says what a row is. The active row takes the `--selection` fill alone, like the sidebar's active row, with no edge bar; matched characters are 650-weight ink with a selection-tinted underline, not a highlight fill. A 13px muted footer lists the keys as keycaps with a `border-strong` edge on `surface-sunk`. Focus starts in the input, ↑ and ↓ move, Enter opens, ⌘Enter opens beside, and Esc returns focus.

Rhizome Desktop's ⌘O repository and ⇧⌘O worktree finders draw the same palette in a transparent overlay webview 64px from the top, with 42px rows, a 7px runtime dot, and ⌘1–⌘9 hints on the first nine repositories.

### Desktop Toolbar

Rhizome Desktop's 38px toolbar is the theme's `--chrome-top`, which the overlay title bar extends behind the traffic lights (72px reserved at the left). Its first control after the traffic lights is the sidebar toggle, a 26px icon button pressed while the sidebar shows. Then it holds the repository name (600, up to 240px) and the worktree branch in mono as two click targets whose tooltips name ⌘O and ⇧⌘O; the worktree is quiet on the primary checkout and a `--chrome-pill` on any other. The section tabs are centred, 26px, with the current one on `--chrome-active`. A 26px search on `--chrome-raised` and the issue count in `--chrome-risk` sit at the right. Window and webview backgrounds take the theme, so opening or resizing never flashes white.

### Agent

The Agent screen uses the shared view header: the session title, its message count, and the harness and its state ("Claude · Ready") as the description. The harness version and account are diagnostics behind a ghost "Details" disclosure at the right of the title row. Harness settings sit at the foot of the sidebar as one labelled 28px chrome-tinted select per row at full sidebar width: Harness and Model always show, and Reasoning effort and Permission mode sit under a compact "Session settings" disclosure (the shared chevron, 13/500 label) whose summary still names the active permission at its right. The conversation and the composer share one centred 760px column (`--agent-column`), so a wide window never stretches the thread. The composer is one framed field (`border-strong`, 10px corners, no resize grip) whose message grows with its content up to 240px, then scrolls, above a bar with the send hint, or, when no harness can take a turn, the reason and next step (install, or log in with a copyable command), beside a 28px primary Send. The sidebar's 28px "New session" button is chrome-tinted like the selects. An empty thread that can take a turn shows "Try asking" (the section label) and three example questions directly above the composer, as quiet 32px action rows with a 16px arrow in faint ink 8px after the question, that fill the composer and focus it. The questions and the composer move together to about 40% of the panel's height (the free space splits 2 above to 3 below), so the panel never opens on a void; the composer's placeholder ("Ask Rhizome…") is its only label. The empty thread never invites a question that cannot be sent.

### GraphQL

GraphQL shares the workbench: the standard sidebar (Docs and History, then Refetch schema, Keyboard shortcuts, and Settings at its foot, each with a 16px outline icon) beside the inset content panel, which opens with the shared view header, "GraphQL" and a one-line description naming the vault, in place of the GraphiQL wordmark. GraphiQL's own tool rail is hidden; the sidebar rows press its buttons, so GraphiQL keeps each panel's state, and the open plugin's row is the current pill. GraphiQL inside the panel is flat: a 40px session tab row whose sessions are text tabs with the shared 2px ink underline under the active one (as `.view-tabs`), then the query and response panes split by one hairline (a 9px drag target), with no floating card, resting shadow, or tinted frame. The query pane leads with a 40px toolbar labelled "Query" (13/500) holding Format and Merge as 28px ghost buttons with a 16px outline icon (1.5 stroke, drawn with lucide's `absoluteStrokeWidth` so it matches the shell's 16-grid icons; without it a 16px lucide icon draws a 1px line) and a short 13/500 visible label (their accessible names start with it: "Format query", "Merge fragments into query"), and Copy as a 28px ghost icon button, then Run, the 28px primary labelled "Run ⌘↵"; the response pane opposite carries the "Response" label and a muted "Run the query to see results" until a result arrives. GraphiQL's palette restates the tokens as HSL triplets, and its Monaco themes are redefined from the syntax tokens in both appearances: keywords `--syntax-keyword`, types `--syntax-type`, `$variables` `--syntax-property`, strings `--syntax-string`, numbers `--syntax-number`, comments `--syntax-comment` (4.5:1), and field names, JSON keys, and brackets in ink or `--syntax-meta`, never VS Code's rotating bracket hues. Line numbers take the faint-ink role (the current line, muted ink). Docs uses the same roles. An empty Variables editor starts as a collapsed bar, and its toggle draws the shared disclosure chevron in a 24px target (right while closed, down while open).

### Ontology Canvas

The canvas uses neutral grid depth, typed cards, type-palette relation lines, and a persistent inspector. At every zoom a card shows its name and count; field counts stay in the tooltip and fields in type details. Structural labels follow the 13px floor. Edges stay 1.5px at every zoom, beneath opaque cards, in the opaque neutral at rest and in the source type's hue while a card is hovered or traced. Relation, implements, and neighbour lines are solid, dashed, and dotted, each in the legend; neighbour links show only while a card is traced. Cards are sized to their two-line name (about 14px on screen at fit zoom), with the description and field counts in the tooltip. A floating "Find a type" box replaces a separate type index; selection uses the neutral `--selection` tint. Canvas colours resolve from the tokens through a probe element and refresh on appearance changes. Graph colour must be labelled or legended; never colour alone.

### Graphs and Maps

Every graph surface (the overview maps, the Atlas, and Explorer) follows the same rules. Essential edges and outlines use `--type-8` or `--link`, which clear 3:1 in both appearances. Labels sit on their own layer above nodes and edges with a 4px surface-coloured halo, placed clear of other nodes and labels, and nodes keep 24px inside the plot. Overview maps cap at 280px so the Members table stays in view, give each member type its own hue (matching the Members dots), put edge counts where no other edge crosses, and state the expand hint once. Explorer opens at folder level (folders sized by what they hold and coloured by their most common type; very large folders open into subfolders and small ones fold into one node), drops node radii to 2–3px with the hovered neighbourhood emphasised, shows edge labels only on hover or match, keeps its grouped legend (Notes by type, Code, Folders) and banner outside the canvas, and shows a loading state until the first draw; a drawn graph sets `data-graph-ready="true"` on its container (also when there is nothing to draw), which the screenshot tooling waits on.

### Custom Views

Agent-written views use the kit's page primitives (`Section`, `Facts`, `Count`, `Dash`, `Segmented`, `MoreToggle`, `BarList`) and `@rhizome/ui` components restyled to these rules. Frames receive the raw token text through `boot.js` and mirror the host's `data-appearance` and `data-theme`, so a view built from defaults matches the bundled pages: Inter on the surface, hairline-separated blocks, Inter tabular counts, ink record titles and teal prose links, neutral buttons with red only for the one commit action, and neutral loading placeholders that appear after 150ms. Views never set their own theme colours.

## 8. Do's and Don'ts

### Do:

- **Do** keep the shell's model: themed chrome around a white inset panel, one right pane, and finders for navigation.
- **Do** keep supporting table columns fixed and let the title take the slack, with a 320px minimum.
- **Do** cap note prose (540px in a 680px note column), cap and centre Briefing (about 1320px) and Overview (about 1520px) as one column with their header and tabs, and let tables, boards, and cards fill the panel.
- **Do** use Merriweather only for document and workspace titles, Inter for UI, prose, numbers, and dates, and JetBrains Mono only for identifiers, paths, and code.
- **Do** read semantic tokens so every surface follows appearance and theme.
- **Do** pair colour with labels, icons, shape, or position for validation, graph state, risk, and status.
- **Do** target WCAG AA in all five themes and both appearances, keep visible focus, and respect reduced motion.
- **Do** keep motion to state feedback, overlay entrance, and loading, in the 120 to 250ms range.

### Don't:

- **Don't** set tabular figures globally or on titles and prose.
- **Don't** use red for selection, active tabs, chosen enum values, or theme chrome.
- **Don't** add a third near-white surface, beige tints, or theme colours inside the content panel.
- **Don't** stack equal-weight bands (stats, presets, toolbars) above a table; put rarely used controls behind View or Filter.
- **Don't** use 700 weights in the sidebar, labels, or pane fields; 700 is for view titles only.
- **Don't** use AI-slop aesthetics: purple-on-white gradients, glassmorphism, oversized hero layouts, decorative blobs or orbs, or soft marketing polish that reduces density.
- **Don't** turn Notion or Coda inspiration into a blocky consumer document editor; Rhizome is closer to Obsidian plus Linear plus Raycast, filtered through AO identity.
- **Don't** use thick coloured side stripes as decoration, nested cards, decorative shadows, or card grids as default scaffolding.
- **Don't** use display fonts in labels, buttons, tables, or data.
- **Don't** colour a routine action red; Refresh, Pause, Dismiss, and similar actions are secondary or ghost buttons.
- **Don't** set counts, dates, or times in mono, or show a raw identifier as a control's value or label.
- **Don't** show readers schema hints, internal field names, or diagnostics outside an info disclosure.
- **Don't** treat the temporary red diamond as the final identity.

### Round-seven additions

- **Help follows its purpose.** In Briefing and Overview, only computed judgements such as "Review suggested" keep an info icon. Descriptive sections carry their help in the heading's `title` and accessible description. The type header keeps its schema identity in its info tooltip.
- **In motion is one flat list.** All notes, group, and type Briefings use the Recent changes two-line row template: ink title, type and folder metadata, and change time. Rows carry status marks only when statuses mix, with one in-place overflow disclosure for the list.
- **Record titles stay ink.** Drew dislikes teal record titles. Use ink at weight 500 at rest, with a teal underline on hover and keyboard focus, consistently across tables, boards, search, rails, and bundled views.

### Round-five additions

- **Titles are ink at weight 500.** A navigable record title is a quiet ink link (`.link-quiet.link-title`) at `--title-weight` (500; `.link-title` sets it), never 600. Titles use `var(--text)` at rest, with a teal underline (`var(--link-line)`, offset 2px) on hover and keyboard focus; text may turn `var(--link)` only in those states. Secondary link columns (Governing specs, Source, related specs and materials, and Parent) always use `.link-secondary`: ink-muted, underlined on hover and focus, so secondary relations remain subdued.
- **One disclosure control.** Every expand, collapse, or disclosure draws `<DisclosureChevron>` (`web/src/components/DisclosureChevron.tsx`, class `.disclosure-chevron`): a 16px (`--disclosure-icon`), 1.5-stroke chevron pointing right when closed and turning down when open, through its `open` prop, a parent `[aria-expanded="true"]`, or `details[open] > summary`. As a bare icon button it sits in `.disclosure-toggle`, a 24px (`--disclosure-target`) hit target with an accessible label; inside a larger control (a row, a summary, a text button) that control is the target. Never a bare ▸, ▾, ‹, or › glyph; previous and next use the lucide chevrons in the same 24px target.
- **Counts say what they count.** When two screens show different totals for the same scope, they read the same source or name the scope beside the number ("346 typed notes", "13 note types", "16 types and 2 interfaces"). A bare "16 types" beside Overview's "13 note types" is a defect.
- **Expanders get their own line.** "Show N more" (`.list-more`) is a block on its own row, `--expander-gap` (8px) below the items, never run on from inline text or a comma list.
- **Icons keep a true 1.5px stroke.** Lucide icons at 16px pass `strokeWidth={1.5} absoluteStrokeWidth`, so they match the shell's 16-grid icons.

### Round-four additions

- **Dates read as words.** Read-only dates in tables and cards show as words ("May 31, 2026") in Inter with tabular figures, with the ISO form in the tooltip; editors keep ISO text.
- **Overflow expands in place.** "Show N more" expands the list where it is; "Open <view>" appears only once everything loaded is shown.
- **Kit sections and segmented controls.** The kit's `Section` heading follows the section-heading rule (15/600), and the kit's `Segmented` uses the same raised selected segment as the host's `.segmented`.
- **Graphs.** Overview maps draw relation links solid and plain links dashed, each in the legend, and show a legend entry only when the map draws it. Explorer labels sit on a canvas-coloured backing placed clear of every node and label, and large nodes always keep their labels. Ontology type pages use the shared header tab bar and human type labels, with identifiers as secondary mono.
- **Inspector hierarchy.** In the Info rail, parent headings (Relations, Linked notes) use the section heading (15/600 ink); type group labels are 13/500 muted; links are 14px with type labels on a muted second line.
- **Long property previews** show up to six lines and offer disclosure only on overflow, in a standalone "Show full summary" / "Show less" row with the shared chevron, never an inline "More".
