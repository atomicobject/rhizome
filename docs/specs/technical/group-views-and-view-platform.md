---
type: TechnicalSpec
id: SPEC-0111
aliases: [SPEC-0111, Group views and view platform]
summary: "Rhizome ships Briefing, Trace, and Sections display-group views as ordinary kit views, and closes the custom-view platform gaps those views expose so a repository can build views of the same complexity."
spec-status: active
last-updated: 2026-10-03
---

# Group views and view platform

## Summary

Selecting a display group today opens Overview, which lists links to its member types. This spec replaces that default with three generic group views: **Briefing** (what needs attention, what is moving, what changed, and what the group contains), **Trace** (records of one member type as rows, with everything they connect to as columns), and **Sections** (each member type as a compact table). The views read only schema metadata and records, so they work for any group without per-group code.

The views are authored as custom views against the public kit and ship inside the binary. Every capability they need therefore becomes part of the kit, the custom-view runtime, or a public API, and a repository can build, copy, and modify views of the same complexity in `.rhizome/views/`. The spec also adds a parent display role so a type's records can form a tree, and adjusts the Agentic Engineering starter so its Delivery group carries the links and tones the views read.

This extends [[unified-view-contract|SPEC-0110]], which reserved a bundled generic group page as subsequent work, and [[custom-view-apps|SPEC-0105]], which owns trusted custom-view serving, the kit, and the frame bridge. [[configured-view-engine-and-repo-config|SPEC-0058]] keeps native collection execution.

## Goals

- Every display group opens a useful page by default, chosen by the group's shape.
- The three views derive their content from schema metadata and records, never from type names.
- A repository author can build a view as complex as these with the same public kit, APIs, styling, freshness, and navigation that Rhizome's own views use, and can start from a copy of Rhizome's views.
- Views stay current without manual reloads and stay responsive on groups with hundreds of records.
- Records of a type can form a tree that views can nest and roll up.

## Non-Goals

- Publishing the kit test harness, kit type definitions, or an npm package for repositories; this spec keeps the harness internal and records publication as follow-up work.
- Backfilling feature areas in this repository.
- Parent links on specs or any type other than `FeatureArea` in the starters.
- Replacing the Notes rail's TypeScript grouping with the new membership API.
- Server-side Tailwind compilation, npm dependency bundling, or a new view config version.
- Changing ontology membership through display grouping, or a generic group query source for native views.

## Requirements

### Bundled views and selection

- Rhizome MUST ship Briefing, Trace, and Sections as custom views mounted on every display group (`group: "*"`). Their sources MUST be ordinary view files served through the same transform, kit, invocation, frame, and validation path as repository views. They MUST use only public kit exports and public HTTP APIs.
- The catalog MUST list bundled views beside authored and generated entries with a distinguishable origin. A repository view with the same id MUST replace the bundled view.
- Default selection for a display group MUST follow this precedence: a remembered or explicit choice, an exact authored group default, a generic authored group default, then the bundled default. The bundled default is Briefing for a group with two or more member roots and Sections for a group with one. Member roots are the types and interfaces the Notes rail lists directly under the group; an interface root stands for its implementing types. Overview MUST remain selectable.
- Trace MUST be offered only for a group in which at least two member roots link to each other. A link counts only when its declared target is a member root or a type within one; a field typed by a broader interface, such as `Note`, does not connect roots.
- `rzm view eject <id>` MUST copy a bundled view's folder into `.rhizome/views/<folder>/`, refuse to overwrite existing files, and leave the copy as the view that the id resolves to.

### Shared derivations

- A member type's lifecycle field, each value's stage, and its gap fields come from the type profile and enum value stages in type documentation ([[type-collection-views|SPEC-0112]]); the views MUST NOT derive them from field names or tones.
- Lifecycle values MUST render with a status mark whose fill encodes the value's position among non-terminal values and whose color encodes its tone, paired with the value's label. Values in the `done` or `dropped` stage are terminal.
- When every member's plural label starts with the same word, views MAY drop that word inside the group page. Record labels MUST otherwise use schema labels.
- Links are the forward `@link` fields among member types, excluding links from a type to itself and links whose declared target is outside the group's roots. A member type that links to no other member type is a reference type.

### Briefing

- **Needs attention** MUST list, each with a count, affected records, and an action:
  - Records with validation issues.
  - Records holding an enum value whose tone is `warning`, `risk`, or `danger` on any enum field, aggregated across types by field and value.
  - Records in an `active`-stage lifecycle value that have not changed for 30 days.
  - The profile's gap fields left empty, per type, with a field's `@policy` reason shown when present.
  - Member records linked to nothing else in the group, when the group has links.
  - A "No validation issues" line MUST appear when no record has issues.
- **In motion** MUST list records whose lifecycle value is in the `active` stage, grouped by type and ordered newest first, each with its first key text field or its summary. Types whose lifecycle has no `active` stage MUST be named with a hint to declare one.
- **Recent changes** MUST list records newest first by day and time. Four or more records changed in the same minute MUST collapse into one expandable line with counts by type.
- **Outside the group** MUST count and list notes that link to or from group records, by resolved type.
- **In this group** MUST list each member type with its count, the first sentence of its description, its lifecycle distribution, and, for an interface, counts by implementing type.
- **Connections** MUST show links between member types as a matrix of record-link counts, marking relations the schema allows but no record uses.
- Briefing MUST link the group's views and its guide, which is the companion document shared by the most member types.

### Trace

- Trace MUST render one row per record of a spine type. The default spine is the hierarchical member type (see Hierarchy) that another member type links to; otherwise it is the member type linked with the most other member types in either direction, with ties broken by record-link count. A control MUST switch the spine to any linked member type with records, marking the default.
- Columns MUST be ordered as follows: the spine; member types the spine links to, in the order the spine declares those fields; other member types by link distance, then by depth; and finally the spine's links to types outside the group, labeled with the target's group.
- A cell MUST hold the records of its column's type that link with anything already placed in the row. Cells fill until nothing changes, and a column fills only through the row and columns no farther from the spine. Expansion MUST NOT pass through reference types. Records reached only through another record MUST be marked as indirect.
- Each column header MUST report how many rows have at least one record. Columns for types with no records, and outside-group columns that no row uses, MUST collapse.
- When the spine has a lifecycle, rows MUST be banded by lifecycle value: most advanced first, terminal values last, and empty values named. Otherwise rows MUST be ordered by connection count.
- Hovering a record MUST highlight every occurrence of that record in the matrix.
- A hierarchical spine MUST nest child rows under their parents and roll subtree counts up into each parent row.

### Sections

- Sections MUST render each member type as a compact table with the title, implementing type for interfaces, lifecycle, and `KEY` fields. Columns that are empty on every shown row MUST be omitted, except the lifecycle column.
- Rows MUST be ordered most advanced first, then newest. A bounded number of rows MUST be shown, with a link to the type's collection view.
- A hierarchical type MUST render as a tree table.
- Member types MUST be ordered like Trace columns when the group has links, otherwise by record count.

### Hierarchy

- `@display(role: PARENT)` MUST be accepted on one single-valued `@link` field per note type whose target is the declaring type, or an interface the declaring type implements whose implementors are all note types. Any other placement, including section and embedded types, MUST be a schema error.
- Validation MUST report records whose parent chain repeats.
- Type documentation MUST report the parent role.
- The Agentic Engineering starter's `FeatureArea` MUST gain `parent: FeatureArea @link @display(role: PARENT)` with authoring guidance.

### Platform: data and metadata

- `GET /api/v1/display-groups` MUST return every effective display group. For each member it MUST return:
  - Name, kind (type or interface), label, plural label, description, record count, issue count, implementors, and nested children.
  - This MUST use the same membership logic as the Notes rail, proven by the shared rail fixture.
- Type documentation MUST additionally report:
  - Each enum value's `@view` label, order, tone, and collapsed flag.
  - Each field's `@requiresWhen` conditions.
  - The effective summary field, including the conventional `summary` field.
  - The parent role.
- GraphQL note types MUST expose `updatedAt` and `issueCount`.
- The kit MUST export hooks for display-group membership, type documentation, and validation summaries.

### Platform: runtime, freshness, and speed

- A hosted frame MUST receive vault, schema, and validation change notifications from the host instead of opening its own event stream. A standalone page keeps its own stream.
- The kit MUST invalidate queries by event class:
  - Index and node changes refresh data.
  - Schema changes refresh type documentation and data.
  - Validation changes refresh validation summaries.
- A bundled view MUST NOT poll for source changes. A hosted repository view MUST reload when its folder changes without a per-frame one-second poll.
- Transformed modules MUST be cached by content and served with validators, so an unchanged file answers 304. Bundled files MAY be cached for the life of the binary.
- A side-effect import of a sibling `.css` file in a view module MUST apply that stylesheet.
- A frame MUST be able to open the issues panel, optionally scoped, and a type or interface collection.
- The kit MUST export these components: a lifecycle status mark, a record chip with shared hover highlighting, a relative time, and a type label helper.

### Platform: validation and testing

- `rzm validate views` MUST report:
  - Relative imports that do not resolve to a file.
  - Bare imports outside the import map.
  - Interpolation-free GraphQL documents passed literally to `graphql` or `useGraphQL` that fail validation against the schema.
- An internal kit test harness MUST render a view with a context, configuration, and faked kit services. The bundled views' tests MUST use it.

### Starter adjustments

- In the Agentic Engineering starter, spec status `active` MUST NOT use the `progress` tone, so in-motion lists contain work rather than current contracts.
- `EffortNote` MUST gain a `governingSpecs` link to the specs an effort carries out, and effort-creation guidance MUST fill it.

### Verification

- Go tests MUST cover:
  - Bundled catalog entries, precedence, and default-by-shape.
  - The membership endpoint against the rail fixture.
  - The type-documentation additions.
  - GraphQL `updatedAt` and `issueCount`.
  - Parent-role compile errors and cycle validation.
  - Transform caching and 304 responses.
  - CSS imports.
  - The `validate views` checks.
  - `rzm view eject`.
- Web unit tests MUST cover the shared derivations with synthetic fixtures. The fixtures MUST include a hierarchical type, a category enum, conditionally required fields, outside-group links, a reference type, a one-type group, and a group with no links. Tests MUST also cover the kit hooks, components, and invalidation classes.
- Browser tests MUST cover:
  - Each view mounted in the workspace.
  - Default selection by group shape.
  - Trace spine switching, nesting, and highlighting.
  - Refresh after an edit without a reload.
  - Navigation from a view to a record and to a collection.
- The views MUST be checked visually against real repository data before delivery.

## Decisions

- **Bundled kit views rather than React built-ins.** Drew chose this on 2026-10-03 so that every gap the views reveal becomes a platform improvement.
- **The tree lives on feature areas.** Specs stay flat contracts that attach to areas. A parent role on another type can come later with the same mechanism.
- **Hierarchical types are the preferred default spine** when another member type links to them.

## Documentation plan

- Custom-views skill template and kit API reference: the new hooks, components, CSS imports, navigation, freshness, ejecting, the test harness's internal status, and the `validate views` checks.
- Views reference in the Rhizome skill: bundled group views and default selection.
- Ontology, validate, and GraphQL subsystem notes: the parent role, cycle validation, and the new fields.
- Views and web context notes: bundled origin and frame event forwarding.
- Starter feature-areas README: parent areas.
- CHANGELOG.
- Reconcile SPEC-0105 and SPEC-0110 so they cite this spec where it changes their behavior.
