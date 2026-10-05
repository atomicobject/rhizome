---
type: ExperienceSpec
id: SPEC-0066
summary: "Two related bugs in EffortNote frozen section rendering: (1) wikilinks to newly written notes resolve to unresolved short names in the section pane context because the server's in-memory noteCache is stale and fswatcher events are dropped when the cache was nil at write time; (2) frozen sections display as click-through PANE buttons rather than inline markdown in the structural view."
spec-status: proposed
last-updated: 2026-06-05
aliases:
  - SPEC-0066
  - effort-note-frozen-section-inline-display
---

# Effort Note Frozen Section Rendering

## Summary

Two related rendering issues in how EffortNote frozen sections (Spec Set, Stories In Scope) behave in the structural view:

**Bug A — Wikilink resolution failure in section pane context**

When clicking a link inside the "Spec Set (Frozen)" section pane (Pane 3 → Pane 4), the link resolves to the unresolved short name (e.g. `note="ontology-aware-search"`) instead of the full vault path. The server logs `Cannot find note in vault`. This persists even after `rzm index` because:
- The section pane fetches its own workspace via `buildNodeRenderedResponse` which calls `resolveLinks` using the server's in-memory `s.noteCache`
- `rzm index` updates the on-disk intel store but does NOT invalidate `s.noteCache`
- The in-memory cache only refreshes via fswatcher events, which are silently dropped (early return) when the cache was nil at the time the new file was written

**Bug B — PANE display mode hides frozen section content**

`specSetFrozen` and `storiesInScopeFrozen` on `EffortNote` use `@contains(display: PANE)` (the default). In the BodyWalker structural view this renders both sections as click-through buttons. Authors must click to open a secondary pane to see the frozen links, which diverges from Obsidian behavior.

## Goals

- Clicking a wikilink in the Spec Set section pane navigates correctly to the target note, even for notes written in the current session
- `specSetFrozen` and `storiesInScopeFrozen` render inline in the structural view so frozen links are immediately visible
- Running `rzm index` (or the fswatcher firing) causes `s.noteCache` to invalidate so subsequent link resolution sees the updated vault

## Non-Goals

- Changing the content contract or validation rules for frozen sections
- Changing how these sections render in Obsidian

## User Stories

### US1 - Frozen section wikilinks resolve to the correct note

- id:: ^SPEC-0066-US1
- summary:: Clicking a wikilink inside the Spec Set (Frozen) pane opens the correct target note, not an empty "Cannot find note in vault" pane.
- status:: ready

#### Acceptance Criteria

- A wikilink to a note written in the current session (not yet in a previously built noteCache) resolves to the full vault path after `rzm index` or after the fswatcher fires.
  - Specifically: `s.noteCache` is invalidated when the intel store is updated, OR the cache-nil drop in `updateNotePathCacheForWatchEvents` is removed so events are queued/applied on first build.
- Clicking the link opens the target note pane correctly.

### US2 - Frozen sections render inline

- id:: ^SPEC-0066-US2
- summary:: Spec Set (Frozen) and Stories In Scope (Frozen) sections display their wikilinks inline in the BodyWalker structural view without requiring a secondary pane click.
- status:: ready

#### Acceptance Criteria

- Both sections render inline (not as PANE buttons) in the structural view.
- Other EffortNote PANE sections are unaffected.

## Requirements

- `s.noteCache` MUST be invalidated when `rzm index` updates the intel store, OR fswatcher events MUST be queued when cache is nil and applied on first build
- `specSetFrozen` MUST use `display: INLINE` in its `@contains` directive
- `storiesInScopeFrozen` MUST use `display: INLINE` in its `@contains` directive

## Technical Notes

Relevant code paths:
- `pkg/app/web/files.go:296` — `notePathCache()` lazy init with in-memory lock
- `pkg/app/web/files.go:354` — `updateNotePathCacheForWatchEvents()` — drops events when cache nil (line 360-362)
- `pkg/app/web/node_workspace.go:803` — `buildNodeRenderedResponse` default/section branch uses `resolveLinks` → `notePathCache`
- `.rhizome/ontology/spec-driven.graphql:915-930` — `specSetFrozen` and `storiesInScopeFrozen` `@contains` directives

## Documentation Plan

No user-facing docs changes needed. Schema change to `spec-driven.graphql` is self-contained.
