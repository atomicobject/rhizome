# Scope, group, and type views

Overview, Briefing, Trace, and Sections are the pages Rhizome opens for a display group; Overview and Briefing are also the pages for All notes; and the type Briefing is the first choice for every type and interface collection. They read only schema metadata, aggregate counts, and records, so they work for any vault, group, or collection without per-type code. They are ordinary custom views: the same kit, APIs, and file rules apply to them as to any view in `.rhizome/views/`.

| View                                                                                                                 | Definition                | Entry                    |
| -------------------------------------------------------------------------------------------------------------------- | ------------------------- | ------------------------ |
| Overview (`group: "*"`): what the group holds, how its members link, and how complete they are                       | `overview.yaml`           | `overview.tsx`           |
| All notes Overview (`workspace`): the same page for the whole vault, untyped notes included                          | `workspace-overview.yaml` | `overview.tsx`           |
| All notes Briefing (`workspace`): issues by variant, work in motion, and recent changes across every note            | `workspace-briefing.yaml` | `workspace-briefing.tsx` |
| Briefing: what needs attention, what is moving, and what changed in the group                                        | `briefing.yaml`           | `briefing.tsx`           |
| Trace: records of one member type as rows, with everything they connect to as columns                                | `trace.yaml`              | `trace.tsx`              |
| Sections: each member type as a compact table                                                                        | `sections.yaml`           | `sections.tsx`           |
| Type Briefing (`type: "*"`): what needs attention, what is moving, what changed, and how the records spread and link | `type-briefing.yaml`      | `type-briefing.tsx`      |
| Interface Briefing (`interface: "*"`): the same page for an interface's implementing records                         | `interface-briefing.yaml` | `type-briefing.tsx`      |

## Customizing them

`rzm view eject group.sections` copies this folder into `.rhizome/views/group/`. Ejecting one view copies all of them, since they share the modules below, and the copies then replace the bundled views with the same ids. Rhizome never overwrites an existing file. Edit the copies and save: the workspace reloads a view when its folder changes. `rzm validate views` checks imports and literal GraphQL documents.

Rules the views follow, and a copy must keep:

- Import only `@rhizome/kit`, `@rhizome/ui`, `react`, `react-dom`, `@tanstack/react-query`, and `lucide-react`, plus sibling files.
- Write relative imports with their extension (`./model.ts`); the browser resolves them as written.
- Read data only through public APIs. Styling comes from the kit's theme tokens, Tailwind classes, and `group.css`, which `components.tsx` loads with a side-effect import.

## Remembering display choices

Use the public kit's `useViewPreference(key, { defaultValue, validate, slot? })` for Trace's row type, bands and nested-row expansion, stable Briefing expansion controls, and the Overview's map or matrix choice and expanded groups. The hook infers the view and concrete group, type, or interface; reusable wildcard mounts do not share settings between subjects. Read shared defaults from validated YAML `configuration` and persist only explicit interaction. A stable authored slot separates intentionally independent widgets; mount IDs, labels, and tab IDs are not preference identity.

Expansion keys must name the field or band and stable group or record identity. Data reloads, staged saves, filtering, and loading more records must not reset unchanged expansion choices. Keep temporary query text, focus, drag state, and scroll out of the store. `loading`, `pending`, and `error` distinguish hydration, optimistic writes, and failures; mutations return promises, and a failed write must show a retry path. `reset()` removes a key's override so current authored defaults apply.

The ignored `.rhizome/user-state.sqlite` store survives index rebuilds and server restarts. It is personal to this checkout. Shared structure stays in `.rhizome/views/` and changes only through deliberate edits or explicit shared configuration save. Ejected copies use the same public preferences API; avoid custom localStorage keys. The canonical custom-views kit reference documents the React and subscribed HTML contracts and examples.

## Module layout

Data flows in one direction: the loader reads the APIs, the model turns the responses into typed members and records, and the derivations compute what each view shows. Everything but the loader and the components is plain TypeScript with no React, so it can be tested without rendering.

| File             | Owns                                                                                                                             |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `api.ts`         | The GraphQL records query and parsers for every API response.                                                                    |
| `model.ts`       | `GroupModel`: members, merged fields, lifecycles, gap and key text fields, links, records, guide, and label helpers.             |
| `graph.ts`       | The member link graph, Trace's spine, columns, and cells, record trees, the connections matrix, and Sections' order and columns. |
| `matrix.ts`      | Trace's matrix: row cells, column coverage, lifecycle bands, and nested rows with subtree rollups.                               |
| `traceRows.tsx`  | Trace's bands, rows, and cells: nesting controls, record chips, and rollup lines.                                                |
| `activity.ts`    | The Briefings' needs attention (including reverse-field gaps), in motion, recent changes, and outside-the-records lists.         |
| `collection.ts`  | The type Briefing's field spreads, primary-date months, and fill counts with most common targets per relation and reverse field. |
| `load.ts`        | `useGroupModel()` and `useCollectionModel()`, the hooks every view starts from.                                                  |
| `components.tsx` | Shared React pieces: state handling, the facts strip, record marks, titles, and links.                                           |
| `group.css`      | Dense table and strip styling.                                                                                                   |

Briefing's blocks are split across `briefing-attention.tsx` (Needs attention), `briefing-activity.tsx` (motion rows, stage hints, Recent changes, and notes outside the records, shared with the type Briefing and the group Overview), `briefing-contents.tsx` (the Overview's guide and views, and an interface's implementing types), and `briefing-parts.tsx` (the block frame, its loading and failure body, and the pieces they share); `briefing.tsx` holds In motion and the layout. `type-briefing.tsx` lays out the type Briefing from the same blocks. The group Briefing shows only activity: its former In this group and Connections blocks became the Overview's member table and Matrix, and Outside the group, Views, and Guide moved to the Overview's side column.

## Scope pages: Overview and the All notes Briefing

The scope pages are SPEC-0117's. A scope is the rail node above a type: All notes (`{ kind: "workspace" }`) or a display group, including the API's `Other` group of ungrouped types. Their counts come from the aggregate endpoint rather than records, so they stay fast on large vaults, and every block loads on its own: the facts strip and each block's heading paint at once, each block shows its own loading line in reserved space, and a failed read shows what failed and a retry without touching the other blocks. While the index rebuilds, the page says so instead of drawing empty counts.

| File                     | Owns                                                                                                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `aggregate.ts`           | Requests for and parsers of `GET /api/v1/ontology/shape?parts=…` and `GET /api/v1/ontology/types`.                                                            |
| `scope.ts`               | Scope membership (members by role, collapsed and expanded groups at All notes), member edges by relation majority, outside neighbors, and declared relations. |
| `members.ts`             | Member table rows (lifecycle by stage, gaps, linked share, links, issues), untyped folder rows, and the coverage line.                                        |
| `map-layout.ts`          | The map's deterministic positions: a seeded force layout, the bottom row of unlinked members, the outside ring, and label placement.                          |
| `scope-load.ts`          | One query per aggregate part, type summaries, the scope model, member documentation, and each block's loading, failure, and retry.                            |
| `overview.tsx`           | The Overview entry for groups and All notes: facts strip, map or Matrix block, member table, layout, and remembered choices.                                  |
| `overview-map.tsx`       | The SVG map: nodes focusable most records first, hover and focus highlight with tooltips, Trace from a relation edge, and the legend.                         |
| `overview-tables.tsx`    | The Matrix and the member table.                                                                                                                              |
| `overview-side.tsx`      | Untyped notes by folder (activating a row opens project search for the folder), coverage, Declared unused, Outside the group, and Guide and views.            |
| `workspace-activity.ts`  | The All notes Briefing's parsers and its active-stage records query.                                                                                          |
| `workspace-briefing.tsx` | The All notes Briefing.                                                                                                                                       |

The Overview reads:

- `GET /api/v1/ontology/shape?parts=members`, `…=links`, and `…=folders`, each part by the blocks that need it, never records or the note graph for its counts.
- Display groups, type documentation (for lifecycle stages, required gap fields, and declared relations), and `GET /api/v1/ontology/types` for labels, roles, and descriptions.
- On a group, Outside the group reads the neighbors of each member's 20 newest records, 10 each, so at most 200 notes per member, and says its list is partial when a record or member has more; Guide and views reads only the guide note and the view catalog. Each is its own request.
- At All notes, the published global issue count for the facts strip.

The All notes Briefing reads the published validation generation and `POST /api/v1/validation/groups` for issues by variant, the aggregate's members part with type documentation and one GraphQL query for each type's newest records in an active lifecycle value (at most five per type, one more read to tell there are more), and `GET /api/v1/ontology/types/__all__?limit=12` for the newest notes, untyped included.

Each member's lifecycle field, gap fields, and key text fields come from the `profile` in its type documentation; an interface member reads the interface's own documentation. Lifecycle values are read by their `stage`: `active` values are in motion and can go stale, `done` and `dropped` values are terminal, and a lifecycle with no `active` stage gets a hint to declare one. Tone only colors marks and flags `warning`, `risk`, and `danger` values. The views never guess a lifecycle from a field's name or tones.

A group's members are its member roots as the Notes rail lists them, plus any type nested under a root that the root does not already stand for. An interface member stands for its implementing types. Links count only when their declared target is a member, so a field typed by a broad interface such as `Note` connects nothing.

The type Briefing models its collection as a group of one member: the type's or interface's node in the display-groups tree, without the types the rail nests under it. Its query also reads the profile's reverse fields, and its neighbors are only the notes that link in. Every block is computed from the loaded records in one pass per field, so a type of several hundred records stays fast; signals open the collection's Table, where the header facets filter them.

`useGroupModel()` reads:

- `GET /api/v1/display-groups` (via `useDisplayGroups`) for membership.
- Type documentation (via `useTypeDocs`) for every member and every concrete member type.
- One GraphQL query for at most 500 records of each type, most recently changed first, with their links, neighbors, `updatedAt`, and `issueCount`, plus the guide note. It reads with `partial: true`, so a record missing a required field stays on the page with its issue count instead of failing the load. A type with more records shows its 500 most recently changed and says so. A `read` (`GroupRead`) narrows the query: fewer records per member or type, no neighbors, links, or scalar fields, or only the guide note. The group Briefing's facts strip and each of its blocks read only what they show, each in its own request, so one block's failure or slow read leaves the others: Needs attention reads values and links without neighbors, In motion values without links, and Recent changes the 100 newest records of each type.
- `GET /api/v1/views` for the authored views the switcher offers, generic views included, and for a collection, its Table choice.
- `GET /api/v1/ontology/types` for labels of types outside the group; interface labels come from the display groups.

The kit refreshes these on vault, schema, and validation events, so the views never poll.
