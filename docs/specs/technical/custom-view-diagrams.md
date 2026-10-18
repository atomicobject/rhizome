---
type: TechnicalSpec
id: SPEC-0123
aliases: [SPEC-0123, Custom view diagrams]
summary: "Live Mermaid diagrams and escape helpers in the custom view kit, with lazy Pele rendering and canonical note navigation."
spec-status: active
last-updated: 2026-10-18
---

# Custom view diagrams

## Summary

Extend [[custom-view-apps|SPEC-0105]] with `Diagram` and `mermaid` exports from `@rhizome/kit`. Views build Mermaid text during render from live query data. The component renders its current source; query freshness remains owned by the existing kit.

## Goals

- Share Markdown's Pele rendering options and app theme mapping.
- Keep Pele out of the kit's initial load and retain the previous SVG during updates.
- Escape dynamic labels and open canonical notes through accessible diagram links.

## Non-Goals

No polling, refresh callbacks, fetching props, per-type builders, per-type link generation, view dark theme, changes to personal vault views, `@rhizome/ui` exports, or Pele import-map entry.

## Requirements

### Public API

`Diagram({ source, label, links?, className? })` returns JSX or null. Source and accessible label are required. Links map Mermaid node/task ids to canonical NodeRefs. Empty or whitespace source renders null. Build source during render from query results, never store it in state or derive it in an effect.

`mermaid.text(string)` escapes `& " # < > : ; , [ ] ( ) { } |` plus backticks as Mermaid decimal `#code;` entities; callers supply syntax-specific quotes. `mermaid.csv(string)` returns a quoted CSV field with doubled embedded quotes, for sankey rows. `mermaid.id(string)` returns deterministic UTF-8 FNV-1a 32-bit, base36, prefixed with `n`.

### Rendering and links

- Markdown and Diagram use one renderer with the same Barlow/JetBrains Mono fonts, 13px font size, and `images: false`. Markdown uses `links: false`; Diagram permits only fragment links via `linkSchemes: []`.
- Load Pele only on a nonempty Diagram mount. Render identity is source plus sorted id-to-notePath/kind/nodeId targets, independent of link-map object identity. Use a React useId-derived prefix per instance. Preserve the preceding SVG and its targets while new output is pending; stale completions cannot replace newer output.
- Detect type with Pele before appending `click <id> href "#rzm-link-<n>"`. The verified Pele 1.6.0 detected types are `flowchart`, `gantt`, `class`, `state`, and `agentflow`. A small Set owns this list and a `ponytail:` comment records C4's incompatible `$link` syntax.
- For unsupported types with nonempty links, append nothing, disable rendered links, and warn once per distinct source, naming the type and supported list. Supported types warn once per distinct source for missing returned link ids. Neither case throws.
- Delegate fragment-anchor clicks to one container handler. Always prevent default for every anchor, including unknown fragments and relative author URLs. Known generated hrefs call `openNode(ref, { beside: metaKey || ctrlKey })`. Enter activates the same handler; middle-click opens a known target beside and prevents fragment navigation. Whitespace-containing ids are omitted from generated directives and reported as missing, preserving valid target indexes. Linked anchors are focusable and show pointer cursors. Author web/script and relative hrefs never navigate. If generated directives fail, retry the original source with links disabled so malformed link keys warn without hiding a valid chart. A source that is itself invalid still produces the error state.
- Share the existing `--pele-*` mapping between Markdown and hosted views. Scale down to container width without scaling beyond natural width, center the SVG, preserve aspect ratio, and show no horizontal scrollbar.
- Initial loading shows muted "Rendering diagram…" with reasonable minimum height. Errors show a bordered "Diagram error: <message>" plus expandable source, and log once per distinct source without escaping to the view error boundary.
- Unlinked containers use `role="img"`; linked containers use `role="group"`. Both use the required accessible label. Loading and error states use a group role so their text and source disclosure remain accessible.

### Author guidance and verification

Update the canonical custom-views template's kit API and skill guidance, then regenerate installed copies through `rzm init`. Document render-time derivation, helpers, flowchart links, sankey CSV, and the explicit supported link list. Verify all Pele-specific authoring claims against 1.6.0.

Test the hostile title `V/TO "and" Q4: rocks, (draft) [x] #1 a&b <i>` through real Pele flowchart, timeline, gantt, xychart, and sankey rendering with the matching helper; assert parse success and visible text. Cover id safety/determinism, component updates and navigation, equal link maps, warning/error containment, unsupported C4, and empty source. Existing Markdown tests remain unchanged. Run web units, lint, typechecks, kit build, fixture view validation, and custom-view pointer/Enter browser journeys. Record before/after kit.js sizes and ensure Pele remains a dynamic chunk.
