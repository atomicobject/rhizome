---
type: ExperienceSpec
summary: "Defines the ontology browser interaction model: capability-selected note views, structural tabs where providers expose sections, and right-sliding panes for node-to-node navigation."
id: SPEC-0030
spec-status: superseded
successor: "[[notes-workspace-shell]]"
last-updated: 2026-09-06
aliases:
  - SPEC-0030
---

# Ontology browser navigation model

## Summary

> Superseded on 2026-09-06 by [[notes-workspace-shell|SPEC-0090]], which replaces the right-sliding pane stack with a tabbed shell and carries forward the structural-first reading, section-tab, and node-anchored refresh commitments below. Kept for traceability.

The ontology browser should feel like an intentional reading surface rather than raw file browsing. Typed notes with provider-produced structural data default to structural view, while root-only notes use their provider-declared reading view. Moving from one node to another preserves a rightward context stack instead of replacing the current note in place.

This spec captures the durable experience contract behind the browser work without re-embedding all implementation details. Root panes and stacked panes both operate on node-anchored workspaces, whether the focused node is the file root, a structural section, or an embedded entity.

## Goals

- make typed notes faster to scan than raw authored source while preserving access to that source
- preserve context when users traverse from one note or node to another
- keep within-note structure and cross-node navigation visually distinct
- support both embedded and standalone nodes through one interaction model
- keep open panes visually stable when live updates affect only part of the current stack

## Non-Goals

- defining the complete visual design system for the workspace
- requiring synchronized scrolling, editing, or every advanced compare mode in phase 1
- making structural view the only way to read typed notes
- specifying backend APIs or persistence details here

## User Stories

### US1 - See the note in a structural view first so major sections and typed collections are immediately navigable
- id:: ^SPEC-0030-US1
- summary:: See the note in a structural view first so major sections and typed collections are immediately navigable.
- status:: ready
#### Acceptance Criteria

- Typed notes with provider-produced structural data default to structural view.
- A format-appropriate authored-source view remains available without losing the current navigation context.
- A root-only typed note defaults to its provider-declared reading view and does not receive synthetic structural tabs.
- Top-level typed structure appears in schema order rather than ad hoc heading order when the schema defines that structure.

### US2 - Open related nodes in a new pane so I can keep the context stack I navigated through
- id:: ^SPEC-0030-US2
- summary:: Open related nodes in a new pane so I can keep the context stack I navigated through.
- status:: ready
#### Acceptance Criteria

- Opening a related node creates a new pane to the right rather than replacing the current pane.
- Opening a new node from an earlier pane truncates deeper panes before adding the new rightmost pane.
- The currently focused node drives the relation rail and other context-sensitive affordances.
- When the user opens a node that is already visible as a floating pane in the stack, the workspace focuses (scrolls into view) the existing pane instead of pushing a duplicate or refetching the workspace. ^SPEC-0030-US2-AC4

### US3 - See each pane stay anchored on its own node when changes arrive
- id:: ^SPEC-0030-US3
- summary:: See each pane stay anchored on its own node when changes arrive.
- status:: ready

#### Acceptance Criteria

- If two panes point at different nodes in the same file, each pane keeps its own title, focus, and context when refreshed.
- A node-scoped update does not force unrelated panes in the stack to reset their local browsing state.
- Freshness, dirty, conflicted, or stale cues appear at the affected pane or node scope instead of only as a global file reload affordance.

### US4 - Use tabs for sections and list/detail views for typed collections without confusing those interactions with cross-note navigation
- id:: ^SPEC-0030-US4
- summary:: Use tabs for sections and list/detail views for typed collections without confusing those interactions with cross-note navigation.
- status:: ready

#### Acceptance Criteria

- Structural sections use tabs as the primary within-note navigation affordance.
- Collection-shaped structural sections can use list/detail presentation instead of flattening every child into a long scroll.
- Section navigation stays visually distinct from the pane stack used for node-to-node traversal.

## Requirements

### Must

- Typed notes MUST default to structural view when the provider supplies structural data.
- A format-appropriate authored-source view MUST remain available as an alternate display.
- Top-level structural sections MUST act as the primary within-note navigation regions when structural projection exists.
- Structural sections MUST use regular tabs as the primary within-note navigation affordance when the provider advertises that structure; root-only providers MUST NOT receive synthetic sections or tabs.
- The root pane and stacked panes MUST both be anchored on the same node-centric workspace abstraction, even when the root pane happens to focus a whole-file node.
- Cross-node navigation MUST open in right-sliding panes rather than replacing the current pane.
- The pane stack MUST support truncating from an earlier pane before opening a new node to its right.
- The focused node, not only the enclosing file note, MUST drive relation and context surfaces.
- Embedded and standalone nodes MUST render through the same core interaction model.
- Node-scoped refreshes MUST preserve pane-stack shape and local context whenever the updated node identity still resolves cleanly.

### Should

- Collection-shaped sections should support compact previews, counts, and list/detail navigation instead of eager full-body rendering.
- Compare and split-view behavior should build on the same pane-oriented navigation mental model.
- Inline expansion should feel like an extension of structural reading rather than a separate navigation mode.
- Live-update cues should make it obvious whether content, status, or both changed without forcing the user to diff the pane mentally.

### May

- Additional pane affordances, previews, or keyboard navigation layers may be added later if they preserve the same core mental model.

## Open Questions

- which preview metadata should be standard in list/detail structural sections across note families
- how much inline expansion should be first-class in phase 1 versus deferred behind pane navigation
