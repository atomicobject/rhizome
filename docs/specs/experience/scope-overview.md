---
type: ExperienceSpec
id: SPEC-0117
summary: "One Overview view shows the shape of All notes and of every display group: a type-level link map, a member table, and scope-only blocks. Briefing shows activity at every level, All notes gains one, and every block loads on its own so switching scopes never freezes the workspace."
spec-status: proposed
last-updated: 2026-10-06
aliases:
  - SPEC-0117
---

# All notes and display group Overview

## Summary

Two pages above the type level say little today. A display group's Overview lists links to its member types, which the rail already shows with counts. The All notes Overview puts a band of recently changed, most linked, and problem notes over a graph of every note. In a vault of 2,515 notes, that graph draws about 2,900 nodes and 17,500 edges, which is too dense to read, Explorer already draws it, and building it freezes the workspace for over a second each time All notes opens. Meanwhile the group Briefing carries the group's structure in its side column ("In this group" and the Connections matrix), so the group Overview has nothing left to add.

This spec gives both levels one view, scoped by the rail node the user selected. **Overview** answers what the scope holds, how its members connect, and how complete they are. **Briefing** answers what needs the user now, with the same three blocks at every level: Needs attention, In motion, and Recent changes. Group structure moves from Briefing to Overview, and All notes gains a Briefing, its new default. Only two Overview blocks depend on the scope: untyped notes by folder at All notes, and a group's unused relations, outside notes, guide, and views. Overview ships as a bundled kit view beside Briefing, Trace, and Sections, reads one aggregate endpoint instead of records, and loads each block independently.

This spec revises [[group-views-and-view-platform|SPEC-0111]], whose Briefing loses its structure blocks, extends [[unified-view-contract|SPEC-0110]] with a mount for All notes, and supersedes [[notes-workspace-shell|SPEC-0090]] US7 for the All notes page. The type and interface Briefing from [[type-collection-views|SPEC-0112]] are unchanged.

## Terms

- **Scope**: the rail node above a type that the Home tab shows. All notes is the root scope; each display group, including the rail's `Other` group of ungrouped types, is a group scope.
- **Members**: for a group scope, the group's members as [[group-views-and-view-platform|SPEC-0111]] defines them; an interface member stands for its implementing types. For All notes, the members are each named display group, each type no named group lists, and untyped notes as one member. Embedded types are never members: their records are fragments of notes already counted, and the rail omits them for the same reason.
- **Link**: a pair of notes or records connected in either direction by a schema relation or a plain note link, counted once per pair.
- **Relation link**: a link through a relation field whose declared target is the member at the other end, meaning its type or an interface that type implements, as in [[group-views-and-view-platform|SPEC-0111]]. If any relation field connects a pair, the pair is a relation link and does not also count as plain. Every other link is a plain link, including links through a broad field such as a `related` field typed by `Note`. Counting plain links is specific to Overview; Trace and the other SPEC-0111 views keep counting relation links only.
- **Linked share**: the fraction of a member's records with at least one link to another member of the scope. At All notes, any other type and untyped notes count.

## Goals

- Every scope above a type opens a page that shows its shape, derived from schema metadata, type profiles, and links, without per-type code.
- Overview and Briefing do not repeat each other: shape on Overview, activity on Briefing.
- All notes accounts for untyped notes, which can be most of a vault (1,478 of 2,515 notes in the vault measured below).
- The map stays readable at vault scale because its nodes are groups and types, not notes.
- Switching to any scope paints its frame at once; slow parts load in place behind their own indicators and never freeze the workspace.
- A repository can eject Overview and change it with the same public kit and APIs Rhizome's own views use.

## Non-Goals

- Changing the type or interface Briefing, Trace, or Sections, or how Trace counts links and decides when it is offered.
- Removing the note graph from Explorer, or the neighborhood graph from the note rail.
- Suggesting types for untyped notes, or classifying them automatically.
- Editing the schema from the map.
- Adding folders as a rail level.
- Speeding up the current All notes graph; this spec removes it from All notes instead.

## User Stories

### US1 - See how a group's member types connect

- id:: ^SPEC-0117-US1
- summary:: See a group's member types as a map whose edges count the record links between them, so the group's real structure is visible at a glance.
- status:: ready

#### Acceptance Criteria

- The map draws one node per member, with area proportional to its record count. A member with no records is drawn hollow.
- An edge joins two members when at least one link connects their records. Its width follows the link count and its label shows the count. An edge is drawn as a relation edge when relation links are at least half its links, and as a plain edge otherwise.
- A relation field that a member declares toward another member but no record uses is drawn as a dashed hairline with no count.
- Types and untyped notes outside the group that link to its records appear as faded nodes on a ring at the map's edge, at most seven, each with its link count; the legend says how many more exist.
- Members with no links to other members sit in a labeled row along the bottom of the map.
- Hovering or focusing a node highlights its edges and shows its record count, its links to other members, and links among its own records. Hovering an edge shows its relation fields with counts and its plain-link count.
- Activating a member node opens that type. Activating an edge with relation links opens Trace for the pair; a plain-only edge has no action, because Trace would show no connection.
- A Matrix toggle replaces the map with a table of the same counts: members as rows and columns, the diagonal showing links among a member's own records, and an Outside column summing links that leave the group. It is the accessible alternative to the map.
- The layout is deterministic: the same data draws the same positions on every load.

### US2 - Compare members in one table

- id:: ^SPEC-0117-US2
- summary:: Compare every member's size, progress, completeness, and connection in one dense table, which replaces Briefing's "In this group" block.
- status:: ready

#### Acceptance Criteria

- On a group, each member row shows record count, a lifecycle bar by stage with the active count named, gap fields with how many records leave each empty, linked share, links to other members, links outside the group, issue count, and last change. At All notes, the two link columns become one total link count.
- Gap counts use neutral styling unless a policy marks the field required, because an optional field left empty is not a problem.
- A member without a lifecycle field says so instead of drawing a bar. A member with no records reads as empty.
- Hovering a row shows the first sentence of the type's description. An interface member lists its implementing types with record counts as nested rows.
- Activating a row opens the member's type. The issue count opens Problems scoped to the member.

### US3 - See the whole vault's shape from All notes

- id:: ^SPEC-0117-US3
- summary:: Open Overview at All notes and see how groups, ungrouped types, and untyped notes connect, without a note-level graph.
- status:: ready

#### Acceptance Criteria

- The All notes map draws each named display group as one node, collapsed by default and marked as a group, plus one node per ungrouped type and one Untyped node. Links among a collapsed group's own records appear in its hover, not as edges.
- Activating a group node expands it in place into its member types, colored by group; the legend offers to collapse it again. Expansion is remembered and shared with the member table.
- The member table lists each named group as a row with summed counts, expanding into its types, then ungrouped types under a "No group" heading, then Untyped.
- The page header states total notes, typed share, note type count, ambiguous notes, and issue count, each once on the page. The ambiguous and issue counts open Problems.

### US4 - Understand the untyped part of the vault

- id:: ^SPEC-0117-US4
- summary:: See where untyped notes live and which types they already link to, so the user can decide what to type next.
- status:: ready

#### Acceptance Criteria

- At All notes, an "Untyped notes by folder" block lists top-level folders by untyped count, each with its untyped notes, their share of the folder, and the two types its untyped notes link to most.
- Activating a folder sets the rail's note-list filter to the folder path.
- A coverage line beneath names types with no records, types with records but no links, and embedded types with their record counts, which the map does not draw.
- This block appears only at All notes.

### US5 - Start the day from the All notes Briefing

- id:: ^SPEC-0117-US5
- summary:: Open All notes on a Briefing that shows what needs attention, what is in motion, and what changed across the whole vault.
- status:: ready

#### Acceptance Criteria

- All notes offers Briefing and Overview, with Briefing as the default.
- The All notes Briefing shows Needs attention (validation issues grouped by issue variant, including issues on untyped and ambiguous notes) and In motion (records in an active lifecycle stage across all types) in one column, and Recent changes across all notes, untyped notes included, in the other.
- "Most linked" and the note graph no longer appear on All notes. Explorer remains the graph's home.

### US6 - Keep the group Briefing to activity

- id:: ^SPEC-0117-US6
- summary:: See only activity on a group's Briefing, with its structure on Overview.
- status:: ready

#### Acceptance Criteria

- The group Briefing shows Needs attention, In motion, and Recent changes, and drops "In this group", Connections, "Outside the group", Views, and Guide.
- The group Overview's side column shows "Declared, unused" (each member's relation fields toward other members that no record uses), "Outside the group" (the notes outside the group linking to the most of its records, without the group's guide note), and "Guide and views" (the shared guide note and the group's authored views).
- The group default stays as [[group-views-and-view-platform|SPEC-0111]] sets it: Briefing for groups with two or more members, Sections for one.

### US7 - Every scope opens fast and fits its data

- id:: ^SPEC-0117-US7
- summary:: Switch to any scope and see its frame at once, with each block filling in as its data arrives, on any vault and in any data state.
- status:: ready

#### Acceptance Criteria

- Activating a scope paints the header, view switcher, and every block's heading at once, without waiting for any block's data.
- Each block loads from its own request and shows a localized loading indicator in space reserved for it, so blocks fill in independently without shifting the page. A page-level loading state appears only while the scope's own facts are still loading.
- A block whose request fails says what failed and offers retry, without affecting the other blocks.
- One view definition serves All notes and every group, including `Other`.
- A group whose members have no links draws the nodes, says that no member records link to each other, and hides the Matrix toggle. A one-member group shows the map with its outside neighbors and a one-row table.
- While the index is rebuilding, the page says so instead of drawing empty counts.

## Requirements

- MUST ship Overview as a bundled kit view in `web/bundled-views/group/` and follow its module rules: public kit and APIs only, derivations in plain TypeScript, and ejection with the other group views.
- MUST derive every block from schema metadata, type profiles, and links. It MUST NOT branch on type, field, or group names.
- MUST serve Overview from a public aggregate endpoint, proposed as `GET /api/v1/ontology/shape`, and load no records and no note graph. The endpoint returns per member type: record count, lifecycle value counts, gap-field empty counts, issue count, last change, and enough to compute linked share for any scope (such as record counts by the set of types each record links to). It also returns type-pair link counts split by relation field and plain links, with untyped notes as one node, and the untyped folder rollup. It MUST respond within 200 ms on a 2,500-note vault and within one second on a 20,000-note vault with a warm index.
- MUST load the All notes Briefing without reading every note or record. Needs attention reads the validation groups API. Recent changes reads a capped, newest-first page of notes, untyped included; add a limit to the public note list if it lacks one. In motion reads only records whose lifecycle value is in an active stage, capped per type.
- MUST paint a scope's frame within 100 ms of activation, and MUST NOT run a main-thread task longer than 50 ms while switching scopes or filling blocks. Work that cannot meet that budget runs in a worker or in bounded chunks.
- MUST add a `workspace` mount kind to [[unified-view-contract|SPEC-0110]], with `{ kind: "workspace" }` as its concrete context, so Overview and Briefing can mount on All notes alongside `group: "*"`. The change reaches the view configuration's mount kinds, catalog targets, view-context validation, view-preference canonicalization and its store, the OpenAPI contract, and the kit's types.
- MUST stop offering the built-in group navigation list as a choice once the bundled Overview is available. It remains only as the fallback [[unified-view-contract|SPEC-0110]] requires when no bundled view can load, under a label that does not collide with Overview.
- MUST remember the map/matrix toggle per scope and the expanded groups at All notes through the kit's view preferences ([[view-preferences|SPEC-0114]]).
- MUST let a kit view set the rail's note-list filter through the host bridge, for US4's folder action.
- MUST give map nodes keyboard focus in descending record-count order, activation with Enter or Space, and accessible names, and keep the Matrix a complete text alternative.
- MUST use the kit's theme tokens and stay dense: hairline dividers, mono numbers, no cards inside cards. The page MUST fit without horizontal scrolling at 1280px with the rail open, and the member table SHOULD begin above the fold at 1440 by 1000.
- SHOULD reuse the Briefing building blocks, including the existing "Outside the group" list and the views-and-guide block, instead of duplicating them.
- SHOULD keep Overview's modules under about 500 lines each, like the other group views.

## Decisions

Confirmed by the product owner on 2026-10-06:

- **All notes gets a `workspace` mount kind.** All notes renders today as a context-free standalone page, which a view cannot target, and `standalone` already means rail-listed views. Treating All notes as a reserved group would leak a fake group into the display-groups API and the rail.
- **The record-level "Outside the group" list moves from Briefing to Overview.** Which notes touch a group is shape, not activity, and the move gives Briefing the same three blocks at every level.
- **Named groups always start collapsed on the All notes map,** rather than collapsing past a type-count threshold. This keeps the map readable at any vault size and avoids redrawing a group's internal structure, which the group's own Overview already shows.

## Open Questions

None.

## Evidence

A throwaway prototype drew these pages from a snapshot of a real personal vault (2,515 notes, 13 note types, 2 embedded types, one seven-type display group), and the current page was measured in a browser against the same vault.

- Opening All notes today blocks the main thread for about 1.5 seconds, followed by three more stalls of 0.3 to 0.75 seconds. Every API response arrives in under 250 ms cold, and the page fetches them before the click. The time goes to building a graphology graph of 2,900 nodes, a synchronous ForceAtlas2 layout (`web/src/components/useSigmaGraph.ts`), and a new Sigma WebGL renderer on every visit.
- Untyped notes carry 5,096 links, more than any type. The largest type links mostly through a broad `related` field. A map that counted only relation links would show untyped notes as an island.
- All 37 validation issues sit on ambiguous notes, which count as untyped. A records-only Briefing would miss all of them.
- In the seven-type group, every record links to another member, while one declared relation and every relation of a type with no records go unused. The unused relations are the group-specific news.
- The group model behind Briefing reads at most 500 records per type and no untyped notes, so it cannot produce All notes figures; hence the aggregate endpoint.

## Dependencies and Assumptions

- Record lifecycle stages, gap fields, and summary fields come from type profiles as [[type-collection-views|SPEC-0112]] defines them.
- Display group membership comes from `GET /api/v1/display-groups`, which already excludes embedded types and synthesizes `Other`.
- The group Briefing keeps its current loader; only its block list changes.

## Verification

- Unit tests for the derivations from shape-endpoint fixtures: map nodes and edges with relation, plain, and mixed pairs, unused declared relations, outside neighbors, collapsed and expanded groups, member rows, linked share, and the folder rollup. They need no browser.
- Endpoint tests for the shape aggregate against the integration fixture vault, including interface members, embedded types, and broad fields.
- Browser checks at 1280px and 1440px on the fixture vault for All notes Overview and Briefing, a multi-type group, a one-type group, and a group with no member links. With responses delayed, check that the frame paints first and each block shows its own indicator; with one response failing, check that only that block shows the error.
- A performance check that switches from a group to All notes and asserts the frame paints within 100 ms and no long task exceeds 50 ms.
- A dogfood pass on a real vault with a large untyped share, checking the map, table, and folder block against counts from the API.

## Documentation Plan

- Mark [[notes-workspace-shell|SPEC-0090]] US7 superseded for the All notes page, pointing here.
- Update [[group-views-and-view-platform|SPEC-0111]]'s Briefing contents and add Overview to its view list.
- Add the `workspace` mount to [[unified-view-contract|SPEC-0110]] and the custom-views reference.
- Document the shape endpoint and the note-list limit in the public API contract.
- Update `web/bundled-views/group/README.md` with the Overview modules, their data, and the moved blocks.
