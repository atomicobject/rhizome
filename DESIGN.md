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
  link-dark: "#5fd6d0"
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
    fontSize: "12px"
    fontWeight: 600
    lineHeight: 1.3
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
---

# Design System: Rhizome

## 1. Overview

**Creative North Star: "The Structured Workbench"**

Rhizome is a precise professional workbench for structured markdown, code context, ontology, and agent collaboration. It is not a consumer notes app and not an AI chat wrapper. Developers and technical leads use it mid-task, so its surfaces stay dense while exposing provenance, relations, status, and next actions.

The shipped workbench follows a Slack-like model. One soft, tinted chrome colour carries the top bar and the sidebar; the work sits on a white content panel inset from the window with a rounded corner; headers stay quiet and keep rarely used controls one click away; and fuzzy finders (⌘P, and in Rhizome Desktop ⌘O and ⇧⌘O) replace long navigation lists. Five chrome themes, each with light and dark appearance, change the chrome only. Content tokens are shared by every theme and vary only by appearance.

The identity stays Atomic Object's: warm near-black ink, AO red for focus and risk, teal for connection, Merriweather for document titles, and JetBrains Mono for evidence. Inter carries the working UI. The compact red diamond in the top bar remains a temporary app-shell marker; the runner-r mark in `docs/brand/` is the reviewed identity.

The supported product surface is desktop web and Rhizome Desktop. Optimize for 1100 to 1920px working widths; narrower windows reflow where layouts naturally allow it.

**Key Characteristics:**

- Dense but calm: a 14px UI base, 30–36px rows, and progressive disclosure instead of stacked control bands.
- One tinted chrome layer, white work surfaces, one tinted neutral, and hairlines.
- Typography that separates roles: Merriweather for document titles, Inter for UI and prose, JetBrains Mono for evidence.
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

- **Connection Teal** (`connection-teal`): relation lines, the linked chart series, and the success status mark's family.
- **Link Teal** (`connection-teal-ink`; `link-dark` in dark): `--link`, the one style for any link that points at a note, with a 45% teal underline (`--link-line`) that goes solid on hover. Also `--success-ink`.
- **Teal Soft** (`connection-teal-soft`): success and provenance backgrounds.

### Tertiary

- **Info Blue** (`info-blue`): the in-progress status mark and informational tints.
- **Warning Gold** (`warning-gold`, `warning-soft`, `warning-ink`): staged (unsaved) edits everywhere, the dirty tab dot, warnings, and the connection banner. Gold means "changed, not yet saved", never "problem".
- **Type palette** (`--type-1` … `--type-8`, each with an `-ink` text variant): categorical colours for ontology types in graphs, Atlas cards, and type dots. Blue, purple, gold, slate, olive, brown, violet, and warm gray; never red or teal. A type's colour comes from a stable hash of its name, so it is the same on every surface.
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

**The Teal Means Connection Rule.** Teal means relationship, provenance, success, or a confirmed link. It is never decoration.

**The Chrome Owns the Theme Rule.** Themes recolour the top bar, sidebar, and selection only. Content surfaces never take a theme colour, so every theme reads the same inside the panel.

**The One Tint Rule.** The content area uses white, one tinted neutral, and hairlines. Do not introduce a third near-white; adjacent surfaces must differ by a visible step.

## 3. Typography

The three families are self-hosted (`web/public/fonts`, `font-display: swap`): Inter as variable woff2 subsets, JetBrains Mono, and Merriweather. The host, every custom-view frame, and the desktop shell load the same files, with no third-party font requests.

**Display Font:** Merriweather, Georgia, Times New Roman, serif
**Body Font:** Inter, -apple-system, system-ui, Segoe UI, sans-serif
**Label/Mono Font:** JetBrains Mono, ui-monospace, Menlo, Consolas, monospace

**Character:** Merriweather gives a note its editorial title; Inter carries the working UI and note prose with neutral, legible density; JetBrains Mono makes paths, ids, dates, GraphQL, and schema fields read as evidence.

### Hierarchy

The scale is fixed: `--fs-xs` 12, `--fs-sm` 13, `--fs-base` 14, `--fs-md` 15, `--fs-lg` 16, `--fs-xl` 18, `--fs-2xl` 24. Chrome uses weights 400, 500, and 600; 700 is reserved for view titles and 900 for Merriweather.

- **Display** (Merriweather 900, 24px, 1.18): the note title and workspace headings such as Problems.
- **Headline** (Inter 700, 18px, 1.2, -0.01em): a view or collection title in its header row.
- **Section** (Inter 600, 18px, 1.35): H2 in a note. H1 inside a body is 24px; H4–H6 drop to 15px muted.
- **Title** (Inter 600, 16px, 1.25): H3 in a note, the right pane's record title, and the sidebar's repository or vault name.
- **Prose** (Inter 400, 15px, 1.6): note body text, held to a 540px measure (65–75 characters).
- **Body** (Inter 400, 14px, 1.45): the UI base: table cells, sidebar rows, tabs, controls, and popovers. Title cells use 500.
- **Secondary** (Inter 500, 13px): column heads, document tabs, property and record labels, tool buttons, and the right pane's tabs.
- **Label** (Inter 600, 12px, 1.3, `--label-font` with `--label-color`): the one style for section heads, the Properties heading, finder group headings, and fact labels. Sentence case, no tracking, no uppercase eyebrows.
- **Mono** (JetBrains Mono 500, 12px): ids, paths, kbd hints, counts beside schema links, and the footer status line. Date cells use mono at the body size.

### Named Rules

**The Product Type Rule.** Inter owns the working UI and note prose. Merriweather appears only as a document or workspace title. Neither font appears in the other's role.

**The Scoped Figures Rule.** Tabular figures (`font-variant-numeric: tabular-nums`) apply only to number-bearing elements: counts, dates, ids, and numeric cells. Never set them globally; Inter widens the hyphen under `tnum`, which breaks titles and prose.

**The Fixed Scale Rule.** Product type uses the fixed token sizes, not viewport-fluid scaling, so dense tools stay predictable at every window width.

**The Human Label Rule.** Show schema labels, never identifiers: type `label`/`pluralLabel`, humanized field names ("Next step", not `nextStep`), enum labels, and note titles with their Markdown stripped. Raw identifiers stay available in `title` attributes and in mono where they are evidence. The helpers live in `web/src/lib/labels.ts` and `useTypeLabel`.

**The Mono Evidence Rule.** Use JetBrains Mono when the text is evidence: paths, ids, schema fields, commands, dates, query fragments, and code references.

## 4. Layout

**Shell.** A 44px top bar on `--chrome-top` holds the brand, the sidebar toggle, the section tabs (Notes, Ontology, Explorer, Agent, GraphQL), a centred global search up to 560px wide, and the tools. Below it every section except GraphQL is the same two-column workbench on `--chrome-bg`: the themed sidebar, then the content panel, inset 6px from the right and bottom with a 10px corner. With the sidebar hidden, the panel insets 6px on the left too.

**Resizable panes.** The sidebar ranges from 200 to 400px (264 default) and never takes more than 45% of the window. The right pane ranges from 280 to 560px (340 default). Each has an 8px drag handle on its inner edge that shows a 2px line on hover and a red line on keyboard focus; it is a `role=separator` that moves in 16px steps with the arrow keys and jumps to its bounds with Home and End. Widths and collapsed state persist per browser; the sidebar's are shared by every section and change only when the user changes them, never on search or a section switch.

**One right pane.** The right pane hosts Record for a selected table or board row, and Info, Outline, and Graph for an active note. Its 40px header matches the note's control bar. With nothing selected on a collection it is closed; `]` toggles it. Below 1200px wide it opens over the content with an overlay shadow instead of narrowing the table.

**Content panel.** In Notes, a 36px document tab strip (Home pinned at the left, the right-pane toggle and Edit at the right) sits above the active surface.

**Collections.** A configured view's header is two rows: a 56px title row (title, count, a truncating description, the schema link, Find in view, Filter, and View) and a 40px row of presentation tabs. The table's 34px column heads begin directly under them. Under the content panel's 36px tab strip, the first data row of an ungrouped table sits 166px below the panel's top edge, and a grouped table adds its 44px group header. Below a 760px header width, the description, facts, and kbd hint drop out before anything wraps.

**Table column policy.** Every column except the title has a fixed width and never takes a share of extra space: ids 176px, enums and short text 136px, relations 180px, dates 104px, numbers and flags 72px, plus a 30px select cell. The title takes all remaining width and never drops below 240px in one-line rows or 320px in two-line rows. When the columns cannot fit at those minimums, relation columns leave first, then ids, then dates; they show in the Record pane, and the footer names them. Rows are 36px in one-line density and 56px in two-line density with the summary.

**Reading and data surfaces.** A note is the one capped reading surface: a 680px column with prose at a 540px measure. Briefing and Overview fill the content panel and align with the presentation tabs above them, so their columns hug the header rather than float as a centred block. The note's control row aligns to the same column. Data surfaces fill the width: tables stretch, and board columns range from 260 to 360px, so a wider window shows more columns rather than wider ones. Collapsed board values become 34px strips. Cards fill an auto-fill grid with a 260px minimum.

**Note rhythm.** Sections separate by space, not rules: 28px above an H2 and 24px above an H3, 8px below any heading, and paragraphs 0.75em apart (about 11px), well under the section gap. The note's lead line of links reads as 13px muted metadata above the body.

**Spacing scale.** `--sp-1` to `--sp-8`: 4, 6, 8, 10, 14, 18, 24, 32px. Rows and controls use the small steps; section rhythm uses 24 and 28.

### Named Rules

**The Title Never Yields Rule.** In a table, supporting columns leave before the title shrinks below its minimum.

**The Reading Measure Rule.** Note prose caps and centres; collection presentations, Briefing and Overview included, fill the panel under their tabs. Never stretch note prose across a 1920px window.

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

The top bar is the theme's `--chrome-top`. The brand is a 7px AO-red diamond and "Rhizome" in 15/600 mono; the diamond is a temporary marker, not the identity. The sidebar toggle, a 30px panel-left icon button in muted chrome text, sits just before the section tabs; it is disabled on GraphQL, which has no sidebar. Section tabs are 30px, 14/500, muted chrome text; hover raises them to `--chrome-raised`, and the current section sits on `--chrome-active` with full chrome text. The global search is a 28px field on `--chrome-raised` with a ⌘K hint. The Appearance control (a 28px icon button) opens a 360px panel with Match system, Light, and Dark as a segmented control and the five themes as swatches that preview their chrome around a white panel.

### Sidebar

The sidebar sits on `--chrome-bg` and is one frame for every section: it names the vault or repository (16/600), and only its contents change. Notes holds All notes, Recently changed, and Problems, then Views, then the type tree in collapsible groups; Ontology holds an Atlas row and the same type tree; Explorer holds the vault tree; Agent holds the session list. Rows are 30px, 14px, muted chrome text with 16px icons; hover uses `--chrome-raised`, and the current row is a light pill (`--chrome-pill`, 600 weight, `--chrome-pill-text`). Counts sit at the right in 12px tabular figures; a problem count turns `--chrome-risk`. Focus inside the sidebar is a 2px `--chrome-text` ring inset on the row. There is no note list; ⌘P finds notes.

### Buttons

- **Primary:** Deep Red (`--accent-fill`) with white text, 13/600, for the one commit action on a surface (Save, Run, execute). Hover deepens to `--accent-fill-hover`.
- **Secondary:** 28px, white surface, 1px `border-strong`, 6px corners, 13/500 ink. Toolbar actions, pagers, Copy link, and the 28px icon-only refresh share these metrics. Hover tints to `surface-tint`, never red.
- **Ghost:** Filter, View, the pane toggle, and close buttons are transparent with muted text until hover or open, when they take the tint and full ink.
- **Edit toggle:** 13/600 muted text with a 1px border and 4px corners; in a session it becomes Editing, Review, Save, and Done.
- **Segmented control (`.segmented`):** every mode switch (Light / Dark / Match system, enum pickers in edit mode, the View popover's "1 line | 2 lines") is one bordered group of 24px segments with hairline dividers. The chosen segment fills with ink and surface-coloured text; selection is neutral, never red.

### Chips, Links, and Counts

- **Type chip (`.type-chip`):** a note's type is one neutral chip: tint background, `border`, 12/600 muted text, 3px corners. An untyped note gets a dashed border. Types never borrow red.
- **Note links:** teal ink text with a translucent teal underline that goes solid on hover. No chip backgrounds.
- **Counts:** unboxed, muted, tabular. A count on a tool button sits in an 18px `surface-strong` pill. A problem count uses red ink with its label beside it, and a note with problems shows an 8px red dot in lists.

### Inputs / Fields

- **Find in view:** a 30px field, 220px wide (160px in a narrow header), `border-strong`, 7px corners, a search icon, and a ⌘F hint. Focus adds the red ring with 1px offset.
- **Form controls:** 28px, `border-strong`, 6px corners, 13/500.
- **Focus:** one keyboard focus style everywhere: a 2px `--focus-ring` outline from `:focus-visible`, offset 2px, inset on rows, cells, and tabs. Components may inset or offset it, not restyle it. On the chrome the ring is `--chrome-text`.

### Collections and the View Header

A configured view is one result set with presentations chosen from text tabs (Briefing, Table, Cards, Board, and saved views); switching presentation never resets search, filters, or sort. The title row shows the 18/700 title, a 14px muted count, a 13px muted description that truncates first, the type's schema name as a mono link, then Find in view, Filter, and View. View opens a popover with Group by, Sort, Rows, Columns, Reset to shared (only while the view holds personal settings), and Save view. Active filters appear as removable chips in a tinted band under the header only while a filter is active. The footer is a status line: row count, collapsed groups, and any columns moved to the Record pane.

### Tables

Column heads are sticky, 34px, 13/500 muted, with an ink sort arrow. Rows are 36px with a `border-faint` hairline; two-line rows are 56px with a 13px muted summary under the 14/500 title. Group headers are sticky 44px bands with a caret, a status mark, the 14/600 group label, and a muted count. Hover tints the row; a selected row takes `--selection` with a 3px `--selection-edge` bar at its left, so it never reads as an error. Focus is one inset ring around the row. Ids, dates, and numbers are mono with tabular figures; ids are 12px muted. Relation values are teal links. Staged edits tint cells warning gold and mark the title with a dot.

### Board and Cards

Board columns are tinted bands with a hairline border and 6px corners holding flat white cards, in schema enum order with empty values included and collapsed values as 34px strips. A move stages an edit until commit or discard, and every drag has a keyboard equivalent. Cards are for low-cardinality types: dense hairline cards in a 260px auto-fill grid.

### Status Marks

Status is a 9px shape plus a label driven by the schema tone: hollow ring for neutral, half-filled blue ring for in progress, filled teal dot for success, gold triangle for warning, red rotated square for risk, dashed ring for muted. Colour supports the shape and never stands alone.

### Right Pane

The pane has a 40px header with its 15/600 title or Info / Outline / Graph tabs (13/500, ink underline when selected) and a close button. Record shows the row's 16/600 title, a muted summary, and 30px field rows with 13/500 muted labels in a 104px column.

### Note Reader

A note opens in a centred 680px column. Above it, a tinted control bar holds the type chip, the mono path (which truncates before any control does), the note's presentations (Structure, Markdown, and any saved view) as the same text tabs a collection uses, and Copy link. The title is Merriweather 24/900. Properties sit under a 12/600 "Properties" label as a two-column grid with 13/500 muted labels and 14px values in 30px rows, without a rule on every row; empty properties wait behind a "Show N empty fields" toggle, and a `border-faint` hairline closes the block. Body headings are Inter 600 with more space above than below; a collapsible section's 14px chevron sits in the gutter and shows on hover, on focus, and while collapsed. Prose is 15/1.6 at the 540px measure. Inline code and code blocks use the tint and mono.

### Section Editor

In edit mode each section body becomes an editor in the same place, with read mode's typography and measure, so toggling Edit does not move the text. It has no box at rest, takes the tint on hover, and draws the red focus ring (1px border plus 1px ring) only while focused. Its padding bleeds outward through negative margins. A changed block turns warning-soft with a gold border.

### Editing States

Every surface shows an edit's state the same way. Editing with nothing staged is neutral. Staged edits are warning gold everywhere: changed fields, section editors, table cells, cards, the tab dot, and the save bar. Only an error or conflict turns the save bar red, and its message floats below the bar. A change the server refuses is taken back out of the session, so the rest still saves.

### Finders

The ⌘P finder is a native `<dialog>` over the scrim: a 640px palette at most 92px from the top (12% of a short window's height), 14px corners, `border-strong`, and the command lift, entering with a 140ms fade and 6px rise that reduced motion removes. A 56px input row holds a 16px query field; a 12/600 group heading leads a listbox of 46px rows with icon, 14/500 title, type, and a middle-truncated mono path. The active row takes `--selection` with a 3px edge; matched characters are 650-weight ink with a selection-tinted underline, not a highlight fill. A footer lists the keys. Focus starts in the input, ↑ and ↓ move, Enter opens, ⌘Enter opens beside, and Esc returns focus.

Rhizome Desktop's ⌘O repository and ⇧⌘O worktree finders draw the same palette in a transparent overlay webview 64px from the top, with 42px rows, a 7px runtime dot, and ⌘1–⌘9 hints on the first nine repositories.

### Desktop Toolbar

Rhizome Desktop's 38px toolbar is the theme's `--chrome-top`, which the overlay title bar extends behind the traffic lights (72px reserved at the left). Its first control after the traffic lights is the sidebar toggle, a 26px icon button pressed while the sidebar shows. Then it holds the repository name (600, up to 240px) and the worktree branch in mono as two click targets whose tooltips name ⌘O and ⇧⌘O; the worktree is quiet on the primary checkout and a `--chrome-pill` on any other. The section tabs are centred, 26px, with the current one on `--chrome-active`. A 26px search on `--chrome-raised` and the issue count in `--chrome-risk` sit at the right. Window and webview backgrounds take the theme, so opening or resizing never flashes white.

### Ontology Canvas

The canvas uses neutral grid depth, typed cards, type-palette relation lines, readable field rows, and a persistent inspector. Canvas colours resolve from the tokens through a probe element and refresh on appearance changes. Graph colour must be labelled or legended; never colour alone.

### Custom Views

Agent-written views use the kit's page primitives (`Section`, `Facts`, `Count`, `Dash`, `Segmented`, `MoreToggle`, `BarList`) and `@rhizome/ui` components restyled to these rules. Frames receive the raw token text through `boot.js` and mirror the host's `data-appearance` and `data-theme`, so a view built from defaults matches the bundled pages: Inter on the surface, hairline-separated blocks, mono counts, teal note links, neutral buttons with red only for the one commit action, and neutral loading placeholders that appear after 150ms. Views never set their own theme colours.

## 8. Do's and Don'ts

### Do:

- **Do** keep the shell's model: themed chrome around a white inset panel, one right pane, and finders for navigation.
- **Do** keep supporting table columns fixed and let the title take the slack, with a 240px minimum (320px in two-line rows).
- **Do** cap note prose (540px in a 680px note column) and let collection presentations, Briefing and Overview included, fill the panel under their tabs.
- **Do** use Merriweather only for document and workspace titles, Inter for UI and prose, and JetBrains Mono for paths, ids, dates, GraphQL, and schema evidence.
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
- **Don't** treat the temporary red diamond as the final identity.
