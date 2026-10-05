---
name: "Rhizome"
description: "Dense professional workbench for structured code and markdown context."
colors:
  primary-red: "#fd4f57"
  primary-red-hover: "#e63e46"
  primary-red-active: "#c8313a"
  primary-red-soft: "#ffeaec"
  primary-red-line: "#fccfd2"
  primary-red-ink: "#9c1d24"
  connection-teal: "#16cbc4"
  connection-teal-hover: "#11b5ae"
  connection-teal-soft: "#d8f7f5"
  connection-teal-ink: "#0b6a66"
  ink: "#2a2724"
  ink-muted: "#4c4845"
  ink-soft: "#5c5853"
  bg: "#f7f7f7"
  bg-tint: "#f2f1ec"
  surface: "#ffffff"
  surface-alt: "#fbfbfa"
  surface-sunk: "#f4f3ef"
  border: "#e4e2dd"
  border-strong: "#c9c6bf"
  border-faint: "#ecebe6"
  muted: "#595959"
  faint: "#706c68"
  info-blue: "#3295bd"
  type-purple: "#a5488b"
  warning-gold: "#dcad66"
  warning-soft: "#fbefd4"
  warning-ink: "#8a5a0f"
  white: "#ffffff"
typography:
  display:
    fontFamily: "Merriweather, Georgia, Times New Roman, serif"
    fontSize: "32px"
    fontWeight: 900
    lineHeight: 1.12
    letterSpacing: "-0.015em"
  headline:
    fontFamily: "Barlow, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "24px"
    fontWeight: 700
    lineHeight: 1.18
    letterSpacing: "0"
  title:
    fontFamily: "Barlow, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.22
    letterSpacing: "0"
  body:
    fontFamily: "Barlow, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: "0"
  label:
    fontFamily: "Barlow, -apple-system, system-ui, Segoe UI, sans-serif"
    fontSize: "11px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "0"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, Menlo, Consolas, monospace"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.35
    letterSpacing: "0"
rounded:
  sm: "3px"
  base: "4px"
  md: "6px"
  lg: "10px"
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
    backgroundColor: "{colors.primary-red}"
    textColor: "{colors.white}"
    typography: "{typography.body}"
    rounded: "{rounded.base}"
    padding: "8px 12px"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.base}"
    padding: "8px 12px"
  input-default:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.base}"
    padding: "8px 10px"
  chip-type:
    backgroundColor: "{colors.surface-sunk}"
    textColor: "{colors.muted}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
    padding: "1px 6px"
  nav-active:
    backgroundColor: "{colors.primary-red-soft}"
    textColor: "{colors.primary-red-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.base}"
    padding: "7px 12px"
---

# Design System: Rhizome

## 1. Overview

**Creative North Star: "The Structured Workbench"**

Rhizome should feel like a precise professional workbench for structured markdown, code context, ontology, and agent collaboration. It is not a consumer notes app and not an AI chat wrapper. The product is used mid-task by developers and technical leads who need dense surfaces that stay readable while exposing provenance, relations, status, and next actions.

The supported product surface is desktop web. Optimize the shell, panes, tables, graph canvases, and command workflows for desktop working widths; do not distort the information architecture to simulate a phone-first product.

The visual system builds from Atomic Object-inspired foundations: red for high-signal focus and risk, teal for connection and provenance, warm black for structure, warm white for calm working surfaces, and Merriweather / Barlow / JetBrains Mono as the type family. The compact red orb remains a temporary app-shell identifier until a designer-approved identity replaces it.

The current UI is evidence, not a ceiling. Preserve its real structural primitives: persistent app shell, left rail, dense note lists, stacked panes, ontology canvas, configured tables, typed chips, source paths, issue counts, and command/agent workflows. Redesign should make those surfaces more beautiful, more consistent, and easier to scan without reducing professional density.

**Key Characteristics:**

- Dense, inspectable, and keyboard-friendly.
- Light-first, warm-neutral, AO-inspired, not generic SaaS.
- Structured surfaces over decorative cards.
- Typography that distinguishes brand, UI, and code/data roles.
- Status always paired with text, icons, shape, or position; never color alone.

## 2. Colors

The palette is restrained by default: neutral work surfaces carry the interface, red appears rarely for selection or risk, and teal marks relationships, provenance, and valid links.

### Primary

- **Workbench Red** (`primary-red`): Primary action, current selection, active navigation, validation risk, and issue emphasis. Its scarcity is what gives it force.
- **Workbench Red Soft** (`primary-red-soft`): Active nav backgrounds and low-risk emphasis fields when the red foreground would be too loud.
- **Workbench Red Ink** (`primary-red-ink`): Text on red-tinted backgrounds and compact issue labels.

### Secondary

- **Connection Teal** (`connection-teal`): Relation lines, provenance, confirmed links, graph focus, success states, and connected-context hints.
- **Connection Teal Soft** (`connection-teal-soft`): Low-emphasis link/provenance backgrounds and selected connection chips.

### Tertiary

- **Type Blue** (`info-blue`): Secondary type families, informational badges, and graph/data categories.
- **Schema Purple** (`type-purple`): Optional type-family color only. Never a dominant theme color.
- **Warning Gold** (`warning-gold`): Warnings, overdue dates, non-blocking validation attention, and staged (unsaved) edits.

### Neutral

- **Warm Ink** (`ink`): Primary text, high-contrast icons, graph labels, and structural strokes.
- **Warm Black** (`ink-muted`): Secondary ink, brand mark support, and subdued UI structure.
- **Workbench Gray** (`muted`, `faint`): Metadata, timestamps, counts, and disabled hints. `faint` is the lightest text that still clears 4.5:1 on every light surface (sunk, tint, red-soft); never set text lighter than it. Red text uses `primary-red-ink` or `primary-red-active`, not `primary-red`, which is 3.3:1 on white.
- **Canvas White** (`surface`, `surface-alt`): Panels, panes, tables, command palette, and note bodies.
- **Sunk Surface** (`surface-sunk`, `bg-tint`): Side rails, toolbar bands, sticky table headers, and read-only depth.
- **Warm Borders** (`border`, `border-strong`, `border-faint`): Separators, control outlines, pane edges, and table rows.

### Named Rules

**The Red Rarity Rule.** Red is for focus, action, and risk. A normal screen should not have red spread across more than a few high-signal elements.

**The Teal Means Connection Rule.** Teal means relationship, provenance, success, or confirmed link. Do not use teal as generic decoration.

**The No Purple Theme Rule.** Purple can categorize schema data, but purple-on-white gradients and purple-dominant AI-tool palettes are prohibited.

## 3. Typography

**Display Font:** Merriweather, Georgia, Times New Roman, serif
**Body Font:** Barlow, -apple-system, system-ui, Segoe UI, sans-serif
**Label/Mono Font:** JetBrains Mono, ui-monospace, Menlo, Consolas, monospace

**Character:** Merriweather gives the brand a grounded editorial signal; Barlow carries the product UI with compact warmth; JetBrains Mono makes paths, GraphQL, code refs, and schema fields feel precise. Display typography belongs to brand and document titles, not labels, buttons, tables, or data.

### Hierarchy

- **Display** (900, 32px, 1.12): Rhizome wordmark, major workspace titles, and rare document/title moments.
- **Headline** (700, 24px, 1.18): Page-level titles such as Notes Workspace, Ontology Atlas, and configured view names.
- **Title** (700, 18px, 1.22): Pane titles, section titles, selected note titles, and drawer headings.
- **Body** (400, 13px, 1.45): Main UI text, table rows, note summaries, controls, and prose excerpts.
- **Label** (700, 11px, sentence case, no tracking, `--label-font` / `--label-color`): the one style for section heads, rail group headers, table column heads, and fact labels ("Recently changed", "Properties", "What it means"). No uppercase eyebrows; hierarchy comes from weight and color. Sub-group heads under a label drop to 600 and the faint gray.
- **Mono** (500, 12px, 1.35): Paths, identifiers, code refs, GraphQL, field names, and schema metadata.

### Named Rules

**The Product Type Rule.** Barlow owns the working UI. Merriweather is brand and high-level document display only.

**The Fixed Scale Rule.** Product typography uses fixed sizes, not viewport-fluid scaling. Dense tools must remain predictable at different window sizes.

**The Human Label Rule.** Show schema labels, never identifiers: type `label`/`pluralLabel` over the type name, humanized field names ("Next step", not `nextStep`), enum labels or sentence-cased values ("Pursuing", not `pursuing`), and note titles with their Markdown stripped. Raw identifiers stay available in `title` attributes and in mono where they are evidence (paths, schema links). The helpers live in `web/src/lib/labels.ts` and `useTypeLabel`.

**The Mono Evidence Rule.** Use JetBrains Mono when the text is evidence: paths, ids, schema fields, commands, query fragments, and code references.

## 4. Elevation

Rhizome is flat by default and layered by structure. Depth comes from surface bands, separators, sticky headers, pane edges, and selected-state color before it comes from shadow. Shadows are reserved for overlays and lifted controls that temporarily sit above the work surface, such as command palettes, popovers, menus, and drawers.

### Shadow Vocabulary

- **Resting Surface** (`none`): Tables, panes, cards, rails, and graph cards at rest.
- **Low Structural Lift** (`0 1px 2px rgba(42, 39, 36, 0.06)`): Compact controls that need tactile definition.
- **Overlay Lift** (`0 1px 3px rgba(42, 39, 36, 0.08), 0 2px 6px rgba(42, 39, 36, 0.04)`): Menus, popovers, and small panels.
- **Command Lift** (`0 10px 28px rgba(42, 39, 36, 0.10)`): Command palette, modal-like overlays, and high-priority drawers.

### Named Rules

**The Flat Workbench Rule.** Working surfaces are flat at rest. If a table, pane, or graph card needs hierarchy, use spacing, borders, typography, or tonal bands before shadow.

**The Overlay Only Rule.** Large soft shadows belong to transient overlays only. Do not pair decorative wide shadows with 1px card borders.

## 5. Components

### Brand Mark and App Shell

The compact red orb remains the temporary brand mark. Do not treat it as the long-term identity or replace it with an unreviewed proposal. The app shell stays compact: brand at left, primary routes as predictable tabs/buttons, search and user/system controls on the right when present.

- **Shape:** Temporary orb stays compact; app shell controls use restrained radius (4px to 6px).
- **Color:** Temporary mark uses the existing red and warm-ink treatment.
- **State:** Active nav uses red underline or red-soft background plus text; focus rings must be visible.
- **Focus:** One keyboard focus style everywhere: a 2px AO red outline from `:focus-visible` in `base.css` (inset on rows and cells). Components may inset or offset it, not restyle it.
- **Keyboard:** Long lists, table rows, and issue rows are one Tab stop each (roving focus) and move with arrow keys; F6 cycles the left rail, main panel, and context rail; `?` lists the shortcuts.

### Buttons

- **Shape:** Compact technical rectangle with restrained corners (4px).
- **Primary:** Workbench Red background with white text; use for commit/run/save/primary execution only.
- **Secondary:** White background, warm ink text, 1px `border`, 26px high, 500 12px. Toolbar buttons, Copy link, Columns, Filter, and the icon-only refresh all share these metrics.
- **Hover / Focus:** Secondary hover tints neutral (`bg-tint`, stronger border), never red. Focus uses a visible red or teal outline plus offset. Do not invent new button shapes per screen.
- **Segmented control (`.segmented`):** every mode switch (Table / Board / Cards, Home / Table, Structure / Markdown, By kind / By file) uses one control: a bordered group of 24px segments with hairline dividers; the chosen segment fills with ink. Selection is neutral, never red.

### Chips

- **Style:** Compact pill or soft-rectangle with 1px role-colored border, pale role background, and high-contrast text.
- **Type chip (`.type-chip`):** a note's type is one neutral chip (sunk background, warm border, muted 600 11px, type label in sentence case). An untyped note gets a dashed border. Types never borrow red.
- **State:** Type chips show count first, then type. Selected chips add clear outline/weight, not just color.
- **Usage:** Use chips for typed notes, ontology types, status, provenance, and filters. Do not use chips as decorative tags in prose.

### Links and Counts

- **Note links:** one style everywhere a link points at a note (body prose, property values, relation cells, crumbs): teal ink (`--link`) text with a translucent teal underline (`--link-line`) that goes solid on hover. No chip backgrounds, no red.
- **Counts:** unboxed JetBrains Mono, 10 to 11px, faint gray. A problem count uses the same metrics in red ink with the row or stat label beside it saying what is counted; no outlined pills. A note with problems shows an 8px red dot in lists.

### Cards / Containers

- **Corner Style:** Restrained technical radius (4px to 10px max).
- **Background:** Surface or surface-alt; selected/risk states may use red-soft; provenance/linked states may use teal-soft.
- **Shadow Strategy:** Flat at rest. Use separators and tonal bands for structure.
- **Border:** 1px warm border. No thick side stripes.
- **Internal Padding:** Dense by default (10px to 18px); larger padding only for page-level summaries or empty states.

### Inputs / Fields

- **Style:** White surface, 1px warm border, 4px radius, compact height.
- **Focus:** Red or teal outline depending on task context; never color-only when validation is involved.
- **Error / Disabled:** Error pairs red tint with icon/text. Disabled state reduces contrast only when still readable.

### Navigation

Top navigation is stable and familiar. Left rails are scan-first: group labels, counts, active rows, and hierarchy must be legible at density. Active navigation uses red sparingly and must not become a full red block unless the element is the primary selected workflow.

### Tables and Configured Views

Tables are first-class product surfaces, not afterthoughts. Use sticky headers, predictable row height, aligned numeric columns, compact inline controls, visible selected row state, and optional right-side detail drawers for structured fields. Avoid modal-first editing.

A configured view is one result set with up to three presentations chosen from a segmented switcher: Table, Board, and Cards. They share the header, preset chips, toolbar, and mono footer status line, so switching presentation never resets search, filters, or sort.

- **Table:** 28px rows, the title is the open affordance (no separate Open column), identifiers and dates in mono, ellipsis truncation, visible sort arrows, sticky group headers, neutral hover and selection (the selected row carries only a 2px accent edge, so it never reads as an error), and staged edits in the save bar's warning gold: tinted cells plus a dot beside the title. Relation values are teal links, not chips.
- **Board:** columns are sunk bands with hairline borders holding flat white cards. Columns come from the schema enum in rank order, empty ones included; values the schema marks collapsed render as narrow vertical strips. A move stages an edit and marks the card until commit or discard. Every drag has a keyboard equivalent.
- **Cards:** for low-cardinality types only. Dense hairline cards in an auto-fill grid under hairline section headers, never nested and never the default scaffold for large sets.
- **Status marks:** status is a shape plus a label driven by the schema tone (hollow ring neutral, half-filled in progress, filled success, dashed muted, distinct shapes for warning and risk). Color supports the shape and never stands alone.

### Editing States

Every surface shows an edit's state the same way. Editing with nothing staged is neutral. Staged edits are warning gold everywhere they appear: fields marked "Changed", narrative blocks, table cells, cards, the tab dot, the "Staged" chip, and the save bar ("Ready to save", "N notes · N changes"). Only an error or a conflict turns the save bar red, and its message floats below the bar rather than wrapping inside the fixed-height tab row. A chosen enum value is a neutral selection, not red. A change the server refuses is taken back out of the session, so the rest still saves.

### Stacked Panes

Panes are the core reading/editing pattern. They need clear headers, type/status metadata, path display, structure/markdown toggles, related-section rows, keyboard focus, and horizontal overflow that feels intentional rather than accidental.

### Ontology Canvas

The canvas uses neutral grid depth, typed cards, role-colored relation lines, readable field rows, and a persistent inspector for selected nodes. Graph color must encode relation/type/status with labels or legends; never rely on color alone.

### Command Palette

The command palette is keyboard-first, Raycast-like in discipline, and Rhizome-specific in content. It should expose agent actions, validation, provenance, graph navigation, query generation, and note operations without becoming a chatbot page.

## 6. Do's and Don'ts

### Do:

- **Do** preserve dense professional workflows: left rail, typed lists, stacked panes, ontology canvas, configured tables, and command palette.
- **Do** use AO-inspired red, warm black, teal, and supporting palette as the product foundation.
- **Do** keep repeated workflows fast to scan, compare, filter, and act on.
- **Do** make confidence inspectable with provenance, links, counts, status, and source paths.
- **Do** use Merriweather for brand/display, Barlow for UI, and JetBrains Mono for paths, code, ids, GraphQL, and schema evidence.
- **Do** pair color with labels, icons, shape, or position for all validation, graph state, risk, and status signals.
- **Do** target WCAG AA, preserve visible focus, and respect reduced motion.
- **Do** use motion only for state feedback, overlay entrance, focus, and loading feedback in the 150ms to 250ms range.
- **Do** treat current UI as context to learn from, not a visual ceiling.

### Don't:

- **Don't** use AI-slop aesthetics: purple-on-white gradients, glassmorphism, oversized hero layouts, vague productivity SaaS copy, generic card grids, decorative blobs/orbs, or soft marketing polish that reduces information density.
- **Don't** use consumer-app bloat: empty whitespace, gamified onboarding, overanimated transitions, modal-heavy flows, cheerful simplification, or single-purpose layouts that cannot scale to expert work.
- **Don't** drift away from Atomic Object brand foundations into an unrelated palette or generic SaaS type system.
- **Don't** treat the temporary red orb or any unreviewed proposal as the final identity.
- **Don't** make Rhizome feel like a chatbot wrapper, landing page, toy knowledge graph demo, or glossy consumer knowledge app.
- **Don't** turn Notion/Coda inspiration into a blocky consumer document editor. Rhizome is closer to Obsidian plus Linear plus Raycast, filtered through AO identity.
- **Don't** use thick colored side stripes, nested cards, huge rounded containers, decorative shadows, or card grids as default layout scaffolding.
- **Don't** use display fonts in labels, buttons, tables, or data.
- **Don't** rely on color alone for status, validation, graph state, risk, or selected state.
- **Don't** hide complexity behind friendly summaries when the user needs structure, provenance, and constraints.
