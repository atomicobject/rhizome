---
type: ExperienceSpec
summary: "Defines the Notes workspace shell: pinned Home, note and project-search tabs, note-browser rails, Structure/Markdown modes, and a graph-dominant Home whose type selection dims rather than filters."
id: SPEC-0090
spec-status: active
last-updated: 2026-10-06
aliases:
  - SPEC-0090
---

# Notes workspace shell

## Summary

The Notes workspace is where humans read and edit typed notes. The previous workspace placed every open note in a fixed-width sheet sliding over the home page; within-note drill-down and cross-note navigation shared that horizontal stack, and at 1280px the first note collapsed to a strip as soon as a second opened.

This spec replaces that model with a tabbed shell. A pinned Home tab owns every collection surface (all notes, a type or interface, problems, modified), and each configured view opens as its own retained tab. Each note file gets one tab that takes the full content width. Within a note tab, structural sections are tabs and node drill-down is in-tab with a breadcrumb, so cross-note navigation is always a tab change and never a pane push. A persistent left rail navigates Home and locally filters the current collection; a collapsible right rail carries note-scoped context. Project search opens retained, full-width search tabs without either rail, as specified by [[search-workspace|SPEC-0098]]. Home is dominated by the vault graph with the listings as a compact band above it.

This spec succeeds [[ontology-browser-navigation-model|SPEC-0030]]. It carries forward SPEC-0030's structural-first reading, section tabs, and node-anchored refresh commitments and retires its right-sliding pane stack. Visual density and theming follow `PRODUCT.md` and the `DESIGN.md` tokens: hairline dividers over nested cards, one accent color, mono for identifiers and paths, usable at 1280px.

## Goals

- keep one note readable at full width without chrome competing for space, at 1440px and 1280px
- make cross-note navigation (tabs) and within-note navigation (section tabs, in-tab drill) visibly different actions
- let the user return to any collection surface in one click from anywhere, without closing notes
- keep note-scoped context (properties, outline, relations, problems, neighborhood) one glance away and dismissible
- make Home communicate the shape of the vault first and the listings second
- preserve tab state across reload and keep existing `?note=` links working
- keep the AO identity while reading as a dense professional instrument

## Non-Goals

- a whole-document Markdown editor (CodeMirror, wikilink completion, whole-file save); this spec only requires that the existing authored Markdown view remains reachable, and a separate spec owns the editor
- the Problems panel redesign and validation-count consistency fix; a separate spec owns it
- configured-view table restyling beyond what the Home tab needs to host it
- the Explorer, Agent, GraphQL, and Ontology atlas pages
- phone-sized or touch-first layouts
- adding a routing or state library; the existing `useSyncExternalStore` location adapter and TanStack Query ownership rules in [[frontend-data-lifecycle|SPEC-0074]] stay in force
- dark theme

## User Stories

### US1 - Start from a pinned Home tab that holds every collection surface

- id:: ^SPEC-0090-US1
- summary:: Start from a pinned Home tab that hosts all notes, a type or interface, a configured view, problems, and modified, so collections never compete with open notes for space.
- status:: satisfied

#### Acceptance Criteria

- The tab strip always begins with a Home tab that cannot be closed or reordered. ^SPEC-0090-US1-AC1
- Home renders exactly one collection surface at a time: all notes, a type or interface home or table, problems, or modified. ^SPEC-0090-US1-AC2
- Selecting a collection while a note tab is active switches to the Home tab without closing any note tab. ^SPEC-0090-US1-AC3
- The Home tab's selection is addressed by the existing `/notes/<selection>` path so existing links still land on the right collection. ^SPEC-0090-US1-AC4

### US2 - Open a note as a full-width tab and control when tabs multiply

- id:: ^SPEC-0090-US2
- summary:: Open a note as one full-width tab, retaining each opened file until it is closed, with a modifier click to open beside.
- status:: satisfied

#### Acceptance Criteria

- Opening a note from the rail, Home, a relation, or a wikilink activates a note tab whose body spans the full content width between the rails. ^SPEC-0090-US2-AC1
- A single click creates or activates the file's regular tab without replacing or closing another file's tab. ^SPEC-0090-US2-AC2
- Every opened file is retained as a regular tab. A modifier click (Cmd/Ctrl) opens or reuses its tab without leaving the current one. ^SPEC-0090-US2-AC3
- Opening a note that already has a tab activates that tab instead of creating a second one, and does not refetch its workspace. ^SPEC-0090-US2-AC4
- Closing a tab activates the nearest remaining tab and never closes other tabs. ^SPEC-0090-US2-AC5
- A tab with unsaved staged edits shows a dirty marker and closing it asks for confirmation. ^SPEC-0090-US2-AC6

### US3 - Navigate from the note-browser left rail without losing open notes

- id:: ^SPEC-0090-US3
- summary:: Use one left rail for collections and the locally filtered note list, where collections navigate Home and notes open tabs.
- status:: satisfied

#### Acceptance Criteria

- The left rail has two stacked regions: collections (views, browse entries, types with counts) and the note list for the current collection with one local filter. Project search is owned by the global header. ^SPEC-0090-US3-AC1
- Clicking a collection entry navigates the Home tab and activates it; clicking a view entry opens or activates that view's retained tab. ^SPEC-0090-US3-AC2
- Clicking a note in the list follows the US2 open rules, and the active note is highlighted in the list. ^SPEC-0090-US3-AC3
- The rail stays visible and scrolls independently on Home and note tabs; it is omitted from full-width project-search tabs. The collections region and note-list region each scroll on their own. ^SPEC-0090-US3-AC4
- The rail width is fixed and the note list truncates titles rather than wrapping. ^SPEC-0090-US3-AC5

### US4 - Inspect note-scoped context in a collapsible right rail

- id:: ^SPEC-0090-US4
- summary:: See properties, relations, problems, outline, and neighborhood graph for the active note tab in a right rail that collapses to a strip.
- status:: satisfied

#### Acceptance Criteria

- The right rail shows Info (properties, relations, problems for this note), Outline, and Graph as tabs; Info is the default. ^SPEC-0090-US4-AC1
- The rail collapses to a labelled vertical strip and its open/closed state and selected tab persist per session. ^SPEC-0090-US4-AC2
- Rail content always describes the active note tab's focused node; switching tabs updates it without a loading flash when the workspace is cached. ^SPEC-0090-US4-AC3
- Clicking a relation or graph node in the rail follows the US2 open rules. ^SPEC-0090-US4-AC4
- On the Home tab the right rail is collapsed and its tabs are disabled. ^SPEC-0090-US4-AC5

### US5 - Read a note structurally with in-tab drill-down

- id:: ^SPEC-0090-US5
- summary:: Read a typed note in structural view by default, move between sections with tabs, and drill into a section or embedded node inside the same tab with a breadcrumb back.
- status:: satisfied

#### Acceptance Criteria

- Typed notes with provider-produced structural data default to Structure mode's structural body; root-only notes use their provider-declared reading view and receive no synthetic sections. ^SPEC-0090-US5-AC1
- Structure and Markdown are the two modes in a Markdown note header; Markdown shows the authored document and the last chosen mode is remembered per session. Non-Markdown notes may use format-specific labels. ^SPEC-0090-US5-AC2
- Opening a section or embedded node replaces the tab body with that node's workspace and shows a breadcrumb from the file root; the breadcrumb returns to the parent node without refetching it. ^SPEC-0090-US5-AC3
- Section navigation and in-tab drill never create a tab; only cross-note targets do. ^SPEC-0090-US5-AC4
- A node-scoped update refreshes only the affected tab's node and preserves that tab's mode, drill position, and scroll position. ^SPEC-0090-US5-AC5

### US6 - Keep my tab set across reloads and shareable links

- id:: ^SPEC-0090-US6
- summary:: Reload or share a link and come back to the same tabs and the same active note.
- status:: satisfied

#### Acceptance Criteria

- The set of open tabs, their order and the active tab are restored after reload within the same browser session and vault. ^SPEC-0090-US6-AC1
- The URL always addresses the active tab: `/notes/<selection>` for Home, `/notes/<selection>?note=<path>[#fragment]` for a note tab, `/notes/<selection>?search=<query>` with effective filters for a search tab, and `/notes?view=<id>` for a view tab. ^SPEC-0090-US6-AC2
- Opening a `?note=` link in a fresh session creates Home plus that one note tab. ^SPEC-0090-US6-AC3
- Browser back and forward move between previously active tabs without creating duplicates. ^SPEC-0090-US6-AC4

### US7 - See the vault graph as the dominant element of Home

- id:: ^SPEC-0090-US7
- summary:: Land on a Home whose graph fills the page and whose listings sit in a compact band above it, with a selected type highlighted inside the full graph rather than filtered out of it.
- status:: satisfied

Superseded for the All notes page by [[scope-overview|SPEC-0117]], which replaces the note graph there with a Briefing and a type-level Overview; Explorer keeps the note graph. Type and interface pages already open the type Briefing from [[type-collection-views|SPEC-0112]].

#### Acceptance Criteria

- On all-notes and type or interface Home, the graph occupies all vertical space below a single info band; the band holds recently changed, most linked, and problems plus counts or status, each as a hairline list. ^SPEC-0090-US7-AC1
- The graph keeps find, zoom, a type legend, and a hover card with title, type, id, and link counts. ^SPEC-0090-US7-AC2
- When a type or interface is selected, the graph still renders every node in the same layout, nodes outside the selection drop to a low opacity and their edges fade, the legend dims the same entries, and the caption states how many nodes are highlighted. ^SPEC-0090-US7-AC3
- Faded nodes remain hoverable and clickable so a link into another type can be followed without leaving the selection. ^SPEC-0090-US7-AC4
- Selecting a native layout in a type workspace replaces the graph and band with that layout; the choice is remembered per vault and type through the shared view selection in [[unified-view-contract|SPEC-0110]], which supersedes the former Home/Table toggle. ^SPEC-0090-US7-AC5

## Requirements

- MUST keep the note body readable at 1280px wide with both rails open: no horizontal overflow, no note narrower than 600px.
- MUST keep the content area's vertical chrome above the note body under 90px (tab strip plus note header).
- MUST expose the tab strip as an accessible tablist with keyboard activation, and the right rail tabs likewise.
- MUST preserve the [[frontend-data-lifecycle|SPEC-0074]] invariants: identity-keyed reads, cancellation of superseded reads, and node SSE keyed by the stable set of open canonical `NodeRef`s across all tabs.
- MUST keep the ontology edit session as the single write path; the shell does not introduce a second save mechanism.
- MUST use the `base.css` design tokens for every color, font, radius, and spacing value; no new hex literals in shell styles.
- MUST respect `prefers-reduced-motion` for tab and rail transitions.
- SHOULD render tab titles as the note's display title with the identifier in mono before it when the type has a preferred identifier.
- SHOULD keep the whole shell in files under about 500 lines each, with shell styles in their own stylesheet.
- MAY offer keyboard shortcuts for next/previous tab and close tab.

## Open Questions

- Resolved after interactive testing: every file opens as a retained tab; there is no replaceable preview tab.
- Does the edit session need a whole-body `setSource` op before Markdown mode can become an editor? Proposed answer: yes, and it belongs to the whole-document editor spec, not this one.
- Should tab persistence use `sessionStorage` (per browser tab) or `localStorage` (per vault across windows)? Proposed answer: `sessionStorage`, so two windows of the same vault keep independent tab sets.
