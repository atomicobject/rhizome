---
type: ExperienceSpec
summary: "Surfaces three at-a-glance context signals in the notes workspace: a status pill on type-home note cards for types with a lifecycle field, the vault name in the app shell so multiple Rhizome windows are visually distinguishable, and inbound-reference count badges + click-to-focus on embedded nodes so drill-down from a user story to the efforts that reference it does not require manual graph navigation."
id: SPEC-0071
spec-status: active
last-updated: 2026-06-15
aliases:
  - SPEC-0071
---

# Notes workspace context signals

## Summary

When working across multiple notes or multiple Rhizome instances, three pieces of context are invisible until you dig: the lifecycle status of a note (only visible after opening it), which vault the current window belongs to (indistinguishable at a glance), and which other notes reference a specific embedded node like a user story (requires hopping out to the graph view or grepping for wikilinks). This spec adds all three signals at the earliest useful moment — a status pill on each type-home card, the vault name in the app shell brand, and an inbound-reference count badge + click-to-focus interaction on embedded story headings — without changing navigation or adding new API surface.

## Goals

- show the status of a note without requiring the user to open it
- make the status pill generic enough to work for any typed note that carries an identity-level enum field, not just Specs
- make individual Rhizome browser windows distinguishable when multiple are open
- expose inbound references on embedded nodes (user stories, acceptance criteria, requirements) as a first-class, click-to-focus signal in the spec pane, rather than requiring graph-view or grep
- keep the changes additive and non-disruptive to types that have no status field and to embedded nodes with no inbound references

## Non-Goals

- rolling up child-note statuses (e.g., aggregating UserStory statuses on a Spec card)
- adding filter-by-status controls to the type home list (separate concern)
- redesigning the type home card layout beyond the pill addition
- changing the vault name source or adding a vault-picker UI
- rebuilding the structural outline or moving the inbound-references panel to a new pane — this spec only adds a heading-level badge + a click-to-focus interaction; the existing relation-groups panel keeps its current location
- new backend data — `sectionRelationGroups` already includes a per-section `section-backlinks` group; this story relies on what's already populated

## User Stories

### US1 - See a status pill on type home cards so I can scan note lifecycle without opening each one

- id:: ^SPEC-0071-US1
- summary:: See a status pill on type home note cards for types that carry a lifecycle status field.
- status:: satisfied

#### Acceptance Criteria

- **Pill renders when field has a value**: A note card in the type-home grid shows a status pill when its identity-level enum field (resolved via `pickIdentityFields`, same as the identity strip) has a value. The pill uses the existing `EnumPillWidget` styling.
  verification:: Open a Spec type home; each note card with a `spec-status` value shows the pill.
- **No pill when field is absent or empty**: Cards for types with no identity enum field, or notes where the field has no value, render exactly as today — no empty placeholder shown.
  verification:: Open a type with no status field (e.g., Person); cards are visually unchanged.
- **Pill style matches the identity strip**: The pill on the card uses the same CSS class and ontology-CSS color as the corresponding pill inside the open note pane.
  verification:: Open a Spec card and then open the same note; the status pill color and label are identical in both contexts.
- **Pill is read-only on the card**: Clicking the pill on a card does not open an editor. Editing remains available only inside the open note pane.
  verification:: Clicking the pill on a card produces no edit interaction.

---

### US2 - See the vault name in the app shell so I can tell multiple Rhizome windows apart

- id:: ^SPEC-0071-US2
- summary:: See the vault name in the app shell brand area and browser tab title so multiple Rhizome windows are visually distinguishable.
- status:: satisfied

#### Acceptance Criteria

- **Vault name replaces the generic subtitle**: The static subtitle "Vault explorer + notes workspace" is replaced with the vault name from `StatusResponse.vaultName` (`/api/v1/status`). The brand word "Rhizome" is unchanged.
  verification:: App shell subtitle shows the configured vault name, not the hardcoded string.
- **Document title includes the vault name**: `document.title` is set to `Rhizome · <vaultName>` on mount so browser tabs and window switchers show distinguishable labels.
  verification:: Two Rhizome windows open on different vaults show different tab titles.
- **Vault name fetched once at app-shell level**: The status fetch happens in `AppShell`, the vault name is passed down as a prop, and no repeated fetches occur on tab switch.
  verification:: Network tab shows a single `/api/v1/status` request on load; switching between Notes/Ontology/Explorer does not re-fetch.
- **Neutral fallback while loading**: While the fetch is in flight the subtitle renders blank rather than the old hardcoded string.
  verification:: On a throttled connection the subtitle is blank until the name resolves, never showing "Vault explorer + notes workspace".

---

### US3 - Drill from an embedded node to the notes that reference it without leaving the spec pane

- id:: ^SPEC-0071-US3
- summary:: See an inbound-reference count badge on user-story (and other embedded) section headings, and click the heading to focus that node so the existing relation-groups panel reveals its inbound references — no graph-view detour, no grep.
- status:: ready

#### Acceptance Criteria

- **Count badge on embedded section headings**: When a section corresponds to an embedded ontology node (e.g., a UserStory) and `sectionRelationGroups[<sectionId>]` includes a `section-backlinks` group with N items, the heading renders a small inbound-count badge ("↩ N" or equivalent) next to the title. Badge only renders when N ≥ 1.
  verification:: Open SPEC-0071 in the notes pane; the US1 and US2 headings show a count badge reflecting the number of efforts (or other notes) that reference each story.
- **Click the heading focuses the embedded node**: Clicking on the embedded section heading (the title, not whitespace) sets it as the focused structural node, so `selectWorkspaceRelationGroups` returns that section's scoped buckets and the relation-groups panel re-renders.
  verification:: Click the US1 heading inside the SPEC-0071 pane; the relation-groups panel updates to show buckets scoped to US1, including the "Section backlinks" bucket listing EFF-0036.
- **Focused-node panel surfaces inbound references prominently**: With an embedded node focused, the "Section backlinks" group (rendered via the existing bucket renderer) appears above other relation buckets for that node, or with a visual treatment that distinguishes it as the drill-down result.
  verification:: With US1 focused, the inbound-references bucket is visible without scrolling past the section's outbound links or other groups.
- **No badge and no panel when empty**: When a section has no inbound references, no count badge appears on the heading and clicking the heading does not reveal a phantom "Section backlinks" bucket.
  verification:: Open a spec note with a story that no effort references; the story heading has no badge and focusing the story shows only its outbound buckets (if any), no empty "Section backlinks" group.

## Requirements

- MUST render the status pill using the same `pickIdentityFields` resolution path used by `OntologyIdentityStrip` to avoid divergence between the card and pane displays.
- MUST NOT render an empty pill placeholder for notes or types with no identity enum field.
- MUST source vault name exclusively from `StatusResponse.vaultName` (already fetched in `ExplorerWorkspace`; lift to `AppShell`).
- SHOULD keep the status fetch result in component state rather than a global store, consistent with how other workspace data is fetched today.
- MUST NOT break or visually regress the type home cards for note types that have no status field.
- MUST source the inbound-references count from the existing `sectionRelationGroups[<sectionId>]` payload (the `section-backlinks` group); no new backend endpoint and no new query.
- MUST keep the click-to-focus behavior limited to embedded section headings that already correspond to an ontology node (i.e., a `structuralNode.nodeId` exists); headings of non-embedded sections remain unchanged.
- MUST NOT change the location or shape of the existing relation-groups panel; US3 adds a heading badge + a click handler, not a new panel.

## Open Questions

None. Both changes use existing data and existing components; no new API surface required.

## Documentation Plan

- Update `pkg/app/mcp/resources.go` agent guide if vault-name exposure changes how agents identify which instance they are connected to (low probability; check after implementation).
- No new Hub note required; both changes are additive UI.
