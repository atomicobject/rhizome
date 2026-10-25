---
type: TechnicalSpec
id: SPEC-0125
aliases: [SPEC-0125, Design system parity]
summary: "One design system across the host web UI, the custom-view kit, bundled views, and agent guidance: shared tokens with a dark theme, kit defaults and primitives that match DESIGN.md, AA contrast, calmer collection toolbars, designed loading states, and render performance on large vaults."
spec-status: active
last-updated: 2026-10-25
---

# Design system parity

## Summary

The 2026-10-25 Impeccable audit scored the web UI 8/16. The Notes shell, configured views, and bundled group views follow [DESIGN.md](../../../DESIGN.md); the custom-view kit, its author guidance, and the older Ontology, Explorer, Agent, and GraphQL surfaces do not. Custom views drift because `@rhizome/ui` ships stock shadcn defaults, the kit's root font size is 14px, and the custom-views skill teaches serif headings and bordered panels, while the primitives that make the bundled views look right are private to `web/bundled-views/group`.

This spec makes one token source and one component grammar serve the host and the kit, adds a dark theme for both, and fixes the audit's verified accessibility, theming, and performance defects. It extends [[custom-view-apps|SPEC-0105]], [[custom-view-diagrams|SPEC-0123]], [[view-preferences|SPEC-0114]], and [[type-collection-views|SPEC-0112]] without changing their data contracts.

## Goals

- A view an agent writes with the kit's defaults looks like the bundled views: Barlow on white, label-style section heads, mono counts, neutral controls, teal note links, and one focus style.
- Host and kit read every color, type size, and status shape from `web/src/base.css` tokens; light and dark themes switch with the operating system.
- Text and focus indicators meet WCAG 2.1 AA in both themes; status never relies on color alone.
- Loading, empty, and error states are designed: neutral, layout-stable, and never flashing a color that means something else.
- Collection toolbars fit one row at normal window widths and keep advanced controls one click away.
- Shell state changes do not re-render inactive tabs, and graph layout does not block the main thread for hundreds of milliseconds.

## Non-Goals

No phone or touch layouts, no in-app appearance override (the theme follows the OS), no new identity or logo work, no changes to view YAML, preference storage, or GraphQL contracts, no REST changes beyond the additive `familyOverrides` preference field below, no server-side aggregation for the Briefing collection read, no custom-view frame keep-alive, no ELK worker, no lucide splitting. Personal vault views are updated by this effort but are not part of the product contract.

## Requirements

### Tokens and themes

- `web/src/base.css` remains the single token source. The kit build includes every theme block from it, not only the first `:root`.
- Dark values live beside light ones and apply under `@media (prefers-color-scheme: dark)`; `color-scheme: light dark` is declared. The desktop shell (`desktop/src`) and GraphiQL follow the same tokens.
- Add semantic tokens the audit found missing: filled primary (`#c8313a` light, AA with white text), text on ink, success and info soft/ink, overlay scrim, and syntax-highlight roles. Define or remove the 11 undefined tokens (`--line`, `--r-full`, `--ink`, `--accent-danger`, `--accent-warning`, `--warning-line`, `--text-default`, `--surface-1`, `--surface-2`, `--radius` in host CSS, and similar) and delete Tailwind fallbacks that hide undefined tokens.
- A categorical type palette `--type-1..8` (each with an ink variant that meets 4.5:1 for text) replaces the hashed hue in `web/src/lib/typeAccent.ts` and the graph palettes in `graphViewShared.ts`. It excludes AO red and connection teal. A chart series palette `--series-1..8` is neutral-first, excludes red, and reserves teal for "linked/connected".
- The kit's shadcn variable names must not collide with host tokens (`--muted` becomes a kit-private name).
- Fonts are self-hosted woff2 with `font-display: swap`, served by the binary for the host and every custom-view frame; all currently requested weights remain available. License notices are generated.

### Color and type rules

- Primary filled controls use the filled-primary token. `#fd4f57` is used as text nowhere; red text uses `--accent-ink`. Note links use `--link`/`--link-line` on every surface, including Explorer and typed-section previews.
- Red is not decoration: no red side stripes on blockquotes, frontmatter, or the error boundary, no red hover on ordinary controls, and no red-soft confirmation chips.
- No uppercase eyebrows; section, rail, and dialog heads use `--label-font`. Merriweather is limited to document titles and the page title of a view host.
- Product font sizes come from the fixed scale tokens.

### Kit defaults (`@rhizome/ui`, `@rhizome/kit`)

- `theme.css` pins Tailwind's `text-xs/sm/base/lg` to 11/12/13/15px; the 14px root stays so spacing utilities remain compact. The view page background is `--surface`. The shared `:focus-visible` outline from `base.css` applies inside frames; components do not draw their own translucent rings.
- Existing component names and props keep working, restyled: `Button` defaults to the 26px neutral secondary with `variant="default"` mapping to it and a red `primary` variant opt-in; `Badge` defaults neutral like `.type-chip`; `Card` is flat (1px border, no resting shadow, 10–14px padding); `Tabs` renders like `.segmented`; `Table` uses 28px rows, label-style heads, and mono numbers; shadcn `accent` (hover, select focus) maps to a neutral tint; `Skeleton` uses a neutral sunk fill.
- `NoteLink` renders the teal note-link style by default; `className` extends it.
- `StatusMark` uses the host's tone shapes (ring, half, filled, triangle, diamond, dashed) so one enum value looks the same in native and custom views.
- New public primitives, promoted from the bundled group views so both use one implementation: `Section`, `SectionBody`, and `sectionStatus` (block head, mono count, caption, controls, and a body that renders loading, error-with-retry, and one-line empty states), `Facts`/`Fact` (one-line facts strip), `Count`, `Dash` (empty value with screen-reader text), `MoreToggle`, `Segmented`, `BarList` (proportional HTML bars with linkable labels and series tokens), and `NodeLink` (the note-link style for canonical nodes). Their CSS ships in the kit theme. Exports are additive under `/kit/v1`.
- `Diagram` lays charts (xychart) out at its container's width; other diagram types keep their natural size and shrink to fit.

### Loading and empty states

- Loading placeholders are neutral, match the shape and size of the content they stand in for, appear only after a short delay (about 150 ms) so fast loads never flash, and respect reduced motion.
- A block's heading paints immediately; only its body waits. Empty states are one line in the block's own voice; zero-count disclosures are not rendered.

### Host chrome

- "Reset to shared" renders only when the view family has personal overrides, and sits in the view-selector row instead of its own row. A host preference snapshot reports `familyOverrides` (any widget slot holds a value), so a custom view whose choices live in widget slots still offers the reset.
- Collection toolbars (configured views) show search, filter, and grouping by default; density, column choice, saved-view publishing, and refresh move into one "View options" disclosure. The toolbar does not wrap at a 1280px window with both rails open. "Problems on this page" merges into the facet line's Issues toggle.
- The header's validation count and the facet line's issue count use distinct labels so they never contradict ("N validation issues" for the type versus "N records with issues").
- Edit mode: a disabled primary button reads as disabled, not as an error tint; long text fields edit in a growing textarea; empty values read "Empty" everywhere.
- Ontology Atlas and Type Detail use the shared tokens, label style, and focus outline; their accent color marks dots and borders only, never text.

### Accessibility

- Rail rows expose the active collection and open note with `aria-current`; a modified note shows a visible shaped mark with text; type rows include their issue count in their accessible name.
- The Notes route has a `main` landmark and a heading; configured-view result counts, empty and loading states, copy-link results, and inline errors announce through status or alert regions.
- Graph surfaces have an accessible name and summary; graph search inputs are labeled; Type Detail tabs implement the tab pattern; the Explorer tree exposes expanded and current state; the Overview stage bar has a text breakdown; closable tabs expose their Delete shortcut; the `?` shortcut does not fire from text input contexts and can be turned off.

### Performance

- Inactive retained tabs, the left rail, and configured table rows do not re-render when unrelated shell state changes. Markdown parses once per source change. Search-tab scroll position is committed without per-event tab state writes.
- Global and Explorer graph layout runs off the main thread or with Barnes-Hut above 1,000 nodes, and graph data refreshes do not recreate the renderer.
- Staged-edit lookups for table cells are precomputed once per session revision.
- Heavy optional libraries (local graph, syntax highlighting) load on demand rather than in the Notes entry chunk.
- Validation-summary queries use stable keys so loading more rows does not refetch earlier rows.

### Author guidance

- The canonical custom-views skill (`pkg/app/cli/init/templates/skills/markdown/custom-views/`) gains a short "Look like Rhizome" section and rewrites its examples on the kit primitives: no page title when mounted on a type, interface, group, or All notes (node presentations and standalone pages draw one Barlow title); Barlow inside the page; counts as `Count`; red only for commit or risk; `NoteLink` for references; `useViewPreference` for remembered choices; the light and dark themes come from the kit, never from per-view overrides.

## Verification

- Unit tests cover the new kit primitives, `StatusMark` shapes, toolbar disclosure behavior, reset-button visibility, and the issue-count labels.
- `make verify` and `make web-e2e` pass; desktop changes pass `make desktop-check`.
- Screenshots of every route and bundled view in light and dark at 1280 and 1440 widths, with WCAG contrast computed for text and focus tokens in both themes, and the Impeccable detector run on changed sources.
- The graph layout benchmark at 2,000 nodes and the retained-tab re-render count are measured before and after.
