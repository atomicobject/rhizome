# Group views and the type Briefing

Briefing, Trace, and Sections are the pages Rhizome opens for a display group, and the type Briefing is the first choice for every type and interface collection. They read only schema metadata and records, so they work for any group or collection without per-type code. They are ordinary custom views: the same kit, APIs, and file rules apply to them as to any view in `.rhizome/views/`.

| View                                                                                                                 | Definition                | Entry               |
| -------------------------------------------------------------------------------------------------------------------- | ------------------------- | ------------------- |
| Briefing: what needs attention, what is moving, what changed, and what the group contains                            | `briefing.yaml`           | `briefing.tsx`      |
| Trace: records of one member type as rows, with everything they connect to as columns                                | `trace.yaml`              | `trace.tsx`         |
| Sections: each member type as a compact table                                                                        | `sections.yaml`           | `sections.tsx`      |
| Type Briefing (`type: "*"`): what needs attention, what is moving, what changed, and how the records spread and link | `type-briefing.yaml`      | `type-briefing.tsx` |
| Interface Briefing (`interface: "*"`): the same page for an interface's implementing records                         | `interface-briefing.yaml` | `type-briefing.tsx` |

## Customizing them

`rzm view eject group.sections` copies this folder into `.rhizome/views/group/`. Ejecting one view copies all five, since they share the modules below, and the copies then replace the bundled views with the same ids. Rhizome never overwrites an existing file. Edit the copies and save: the workspace reloads a view when its folder changes. `rzm validate views` checks imports and literal GraphQL documents.

Rules the views follow, and a copy must keep:

- Import only `@rhizome/kit`, `@rhizome/ui`, `react`, `react-dom`, `@tanstack/react-query`, and `lucide-react`, plus sibling files.
- Write relative imports with their extension (`./model.ts`); the browser resolves them as written.
- Read data only through public APIs. Styling comes from the kit's theme tokens, Tailwind classes, and `group.css`, which `components.tsx` loads with a side-effect import.

## Remembering display choices

Use the public kit's `useViewPreference(key, { defaultValue, validate, slot? })` for Trace's row type, bands and nested-row expansion, and stable Briefing expansion controls. The hook infers the view and concrete group, type, or interface; reusable wildcard mounts do not share settings between subjects. Read shared defaults from validated YAML `configuration` and persist only explicit interaction. A stable authored slot separates intentionally independent widgets; mount IDs, labels, and tab IDs are not preference identity.

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

Briefing's blocks are split across `briefing-attention.tsx` (Needs attention), `briefing-activity.tsx` (motion rows, stage hints, Recent changes, and notes outside the records, shared with the type Briefing), `briefing-contents.tsx` (In this group, Connections, views and guide), and `briefing-parts.tsx` (the pieces they share); `briefing.tsx` holds the rest and the layout. `type-briefing.tsx` lays out the type Briefing from the same blocks.

Each member's lifecycle field, gap fields, and key text fields come from the `profile` in its type documentation; an interface member reads the interface's own documentation. Lifecycle values are read by their `stage`: `active` values are in motion and can go stale, `done` and `dropped` values are terminal, and a lifecycle with no `active` stage gets a hint to declare one. Tone only colors marks and flags `warning`, `risk`, and `danger` values. The views never guess a lifecycle from a field's name or tones.

A group's members are its member roots as the Notes rail lists them, plus any type nested under a root that the root does not already stand for. An interface member stands for its implementing types. Links count only when their declared target is a member, so a field typed by a broad interface such as `Note` connects nothing.

The type Briefing models its collection as a group of one member: the type's or interface's node in the display-groups tree, without the types the rail nests under it. Its query also reads the profile's reverse fields, and its neighbors are only the notes that link in. Every block is computed from the loaded records in one pass per field, so a type of several hundred records stays fast; signals open the collection's Table, where the header facets filter them.

`useGroupModel()` reads:

- `GET /api/v1/display-groups` (via `useDisplayGroups`) for membership.
- Type documentation (via `useTypeDocs`) for every member and every concrete member type.
- One GraphQL query for at most 500 records of each type, most recently changed first, with their links, neighbors, `updatedAt`, and `issueCount`, plus the guide note. It reads with `partial: true`, so a record missing a required field stays on the page with its issue count instead of failing the load. A type with more records shows its 500 most recently changed and says so.
- `GET /api/v1/views` for the authored views the switcher offers, generic views included, and for a collection, its Table choice.
- `GET /api/v1/ontology/types` for labels of types outside the group; interface labels come from the display groups.

The kit refreshes these on vault, schema, and validation events, so the views never poll.
