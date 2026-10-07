# Views and defaults

View definitions live in tracked `.rhizome/views/` YAML files. Editing mounts, defaults, and layouts does not require changing ontology SDL. For a page implemented in HTML or TSX, load the `custom-views` skill; it owns the code and browser verification.

## Discover before writing

Start/reuse a session, then list the catalog:

```bash
rzm agent view list --session-id <id>
```

`targets` lists actual type, interface, group, and node targets with compatible `choices` and `defaultChoiceId`. `views` lists definitions with their `origin`: `repository` (`.rhizome/views`), `bundled` (shipped inside `rzm`), or `generated` (runtime type and interface defaults). Read existing files before replacing a default. The group names are effective `@display(group:, parent:)` navigation groups, not ontology instance sets.

For several calls or filtered output, use code mode:

```js
const catalog = await rzm.view({ action: "list" });
return catalog.payload;
```

To read a view's rows, run it with `rzm.view({ action: "run", id, omitCapabilities: true })`, or add `--omit-capabilities` to `rzm agent view run`. The capability catalog describes how every field can be filtered, sorted, and edited, and is usually most of the response; keep it only when you are building filters.

Use live schema discovery for source fields and canonical refs. Inspect a compatible native definition with `rzm agent view show <view-id>` rather than guessing its variant configuration. Native `variants` configure Table, Cards, and Kanban over one source, with `defaults.variant` selecting its initial layout.

## Applicability and defaults

Choose a mount independently of the source:

| Mount | Fields | Applies to |
| --- | --- | --- |
| `type` | `type: <name>` | One concrete type collection |
| `type` | `type: "*"` | Any type collection, parameterized by the selected type |
| `interface` | `interface: <name>` | Its implementing collection |
| `interface` | `interface: "*"` | Any interface collection, parameterized by the selected interface |
| `group` | `group: <name>` | One named display group |
| `group` | `group: "*"` | Any display group, parameterized by the selected group |
| `workspace` | None | The All notes page |
| `node` | `type: <name>` | Individual nodes of one concrete note or embedded-node type (not a section type) |
| `standalone` | Optional `group`, `order` | A Notes rail entry; `group` names its rail section |

`group` belongs only on `group` and `standalone` mounts; validation reports it, and the view ignores it, on the others.

Mount a view whose source is one type or interface on that type or interface, so it appears in that collection's view list rather than as a second rail entry beside the type. Use `standalone` only for a dashboard spanning several types or when the user explicitly asks for a dedicated sidebar entry.

When a native view customizes the type's standard Table, Cards, or Board (columns, card fields, sort, grouping), add `replaceGenerated: true`:

```yaml
mount:
  kind: type            # or interface
  type: IPIdea
  replaceGenerated: true
```

Its layouts then take the generated view's place, named plainly "Table", "Cards", and "Board", and it becomes the collection's default unless another view sets `mount.default: true`; `defaults.variant` picks the opening layout. Only native mounts on one `type` or `interface` may replace, never a `"*"` mount; elsewhere the field is ignored with a warning, and if several views replace one target, `rzm validate views` warns and the first by `order` then ID replaces. Other views on the subject stay selectable as additional choices named "<View> · <Layout>"; in the catalog these carry `custom: true`.

Set `mount.default: true` to make a definition the configured default. An exact default takes precedence over a generic (`"*"`) one, and both over the generated default. Avoid competing defaults at the same specificity: `rzm validate views` reports them, both views stay available, and the first by `order` then ID becomes the default. Multiple compatible views remain selectable, including the built-in presentations.

Selection is explicit link, valid remembered user choice, configured default, then fallback. Without an authored default, a type or interface opens its generated view's default layout (see below), or its replacing view's default layout, a group opens a bundled group view chosen by its shape, and a node opens Structured. To test a new default, select the view the workspace switcher marks as the default (a dot); picking it clears the remembered choice, so an earlier override does not mask the default. Switching a presentation preserves pending workspace edits.

## Generated defaults by shape

Every type and interface gets a generated view built from its schema profile (shape, lifecycle, summary, primary date, and field roles; see the ontology authoring reference), never from field names:

| Shape | Grouping | Sort | Layouts |
| --- | --- | --- | --- |
| `workflow` (lifecycle with an `active` stage) | lifecycle, in stage order `active`, `open`, `done`, `dropped` | Changed, newest first | Table, Board, and Cards when a summary exists |
| `contract` (lifecycle without `active`) | lifecycle, in stage order | Changed, newest first | Table, Board, and Cards when a summary exists |
| `dated` (primary date, no lifecycle) | primary date by month, newest first | primary date, newest first | Table, Cards when a summary exists |
| `catalog` (enum without stages) | first category field | title | Table, Cards when a summary exists |
| `reference` | none | title | Table, Cards when a summary exists |

Generated table columns, up to ten: title, identifier, implementing type for interfaces, lifecycle, `KEY` ordered and category fields, key text, people, relation fields, reverse fields as counts headed by the target's plural label, the primary or first date, and Changed. Issues stays available as a column but is not a default. A workflow type opens as a Board when at least two `open` or `active` lifecycle values have records and at most 60 records are open or active; otherwise it opens as a Table. Board columns keep the enum's order.

An authored view keeps its own configuration; the profile fills only what it leaves unset, such as the board's lane field, and the stage order of lifecycle groups when the enum declares its stages (an enum with only inferred stages keeps its enum order in authored views).

## Layout settings

- `defaults.group: {field: none}` says the view is explicitly ungrouped; no default grouping applies.
- `defaults.group.bucket: month` groups one `Date` or `DateTime` field by calendar month (`YYYY-MM`, labeled like `Sep 2026`), newest first, with undated records last. Validation rejects it on any other field.
- `variants.table.density` is `two-line` (summary under the title) or `one-line`; when unset, tables use two lines if the type has a summary field.
- `variants.kanban.laneField` splits a board into lanes. When unset, the board uses the first `KEY` ordered field with values, else the first `KEY` relation whose filled records average at most 1.25 targets, else no lanes; `none` turns lanes off. A record with several lane targets appears in each lane. Readers can switch the board's column field among the lifecycle and ordered fields.
- The `missing` filter operator matches records with no value in a scalar, enum, or link field: `{field: owner, op: missing}`.

## Personal settings and shared configuration

Meaningful view choices are remembered automatically in ignored `.rhizome/user-state.sqlite`, scoped to the definition and concrete invocation. The effective value is built-in default, then authored configuration, then a valid personal override. Reset removes overrides and follows current repository defaults. For custom views, the host reset includes every child widget in that instance, even unmounted widgets; a kit accessor reset clears only its own key. Separate host instances remain independent. Reloads, server restarts, and index rebuilds retain personal choices. Different type, interface, group, node, and standalone instances remain separate, even when one generic definition serves them. Legacy sources import once and cannot restore changed or reset state.

Native views remember filters and presets, sorting, grouping, columns, density, widths, board fields, and stable expansion choices. Temporary search, paging, drag state, focus, and camera motion remain local. Custom code uses the kit's `useViewPreference` or subscribed `getViewPreference` with validated defaults from `configuration`; load `custom-views` for its contract and examples. Do not handroll persistence keys or change YAML after ordinary interaction.

## Save shared configuration

The workspace's explicit repository configuration save publishes the reviewed variant, filters, sort, grouping, visible columns, density, and board fields to view YAML directly (`POST /api/v1/views/{id}/save`). From a generated view it creates `.rhizome/views/<type>.yaml` mounted on the type or interface with `replaceGenerated: true`; from an authored view it rewrites only that file's `defaults` and `variants` keys, keeping comments and other keys. Settings the request leaves out (grouping, columns, density, board column and lane fields) stay as they are; an explicit empty density or lane field removes the key, and a group with an empty field saves `group: {field: none}`. Save refuses a result that `rzm validate views` would report any issue for, state the view runtime would reject (such as `contains` on a link or a lane field equal to the column field), and, from a generated view that another view already replaces, answers 409 naming that view. Temporary search is included only when explicitly selected for publication. After success, only promoted overrides clear; personal widths and expansion choices remain, including when a generated view receives a new authored ID. Review the diff in version control like any other view change.

Custom definitions use `source.kind: custom` plus a relative entry, and may provide an arbitrary JSON-compatible `configuration` object. Native definitions retain their existing source/variant/default contracts; do not put native execution defaults or variants on a custom definition.

## Bundled views

Rhizome ships custom views built on the public kit. They read schema metadata and records only, so they work for any collection or group.

Every type and interface offers **Briefing** (`type.briefing` and `interface.briefing`, mounted with `type: "*"` and `interface: "*"`) as its first choice, where Overview used to be; Overview remains only for display groups. The Briefing shows what needs attention (validation issues, warning and risk values, stale active records, profile gap fields with their `@policy` reasons, and reverse fields most records fill but some do not), active-stage records newest first, recent changes with same-minute bursts collapsed, a distribution per lifecycle, ordered, and category field, the primary date per month, fill counts and most common targets per people, relation, and reverse field, notes of other types that link in, and the companion guide. It is never the default: without an authored default, the collection still opens its generated Table or Board. It reads up to 500 records per concrete type in one query and says when a type has more.

Every display group offers four bundled views, mounted with `group: "*"`:

- **Overview** (`group.overview`): the member types as a map whose edges count the record links between them (or the same counts as a matrix), one comparable row per member with its lifecycle, gap fields, linked share, links, and issues, relations declared between members that no record uses, the notes outside the group linking to the most of its records, and the guide and authored views.
- **Briefing** (`group.briefing`): what needs attention, what is in motion, and recent changes.
- **Trace** (`group.trace`): records of one member type as rows, with the records they connect to as columns.
- **Sections** (`group.sections`): each member type as a compact table.

A group's member roots are the types and interfaces the Notes rail lists directly under it; an interface root stands for its implementing types. Without an authored default, a group with two or more member roots opens Briefing and a group with one opens Sections. Trace is offered only when two member roots link to each other through a forward `@link` whose declared target is a member root or a type within one; a field typed by a broader interface such as `Note` does not count. Overview leads the choices. If it is hidden or cannot load, the built-in list of member types takes its place under the name Types.

All notes offers two bundled views, mounted with `kind: workspace`: **Briefing** (`workspace.briefing`), its default, with what needs attention across the vault (validation issues by variant, untyped and ambiguous notes included), records in an active stage across all types, and recent changes to any note; and **Overview** (`workspace.overview`), which draws each display group as one node that expands into its types, beside ungrouped types and untyped notes, and lists untyped notes by top-level folder. Overview reads its counts from `GET /api/v1/ontology/shape`, never every record.

The views get better as the schema says more: `@view` stage, order, and tone on lifecycle enums, `KEY` importance, `@requiresWhen`, links between member types, and `@display(role: PARENT)` for trees (see `ontology-authoring.md`).

A repository definition with a bundled view's id replaces it. To customize the bundled views, copy their folder into the repository with `rzm view eject <id>` (code mode: `rzm.view({ action: "eject", id })` on a read-write connection). Eject copies the whole folder, which holds the group views and the type Briefing because they share modules, into `.rhizome/views/group/`, writes nothing if any destination file exists or any of the folder's views already comes from the repository (after an earlier release's eject, move that folder out of `.rhizome/views` and eject again), and leaves the copies resolving those ids, so default selection and Trace applicability still apply. Load `custom-views` to change the copied code.

## Verify the mounted result

Run `rzm validate views`, inspect the catalog again, and open the actual type row, group label, or node workspace. The group caret expands navigation; its label opens the workspace. Verify the default and selector alternatives, contextual links, and staged edits where present. A direct `/views/<id>` page also needs concrete context when its code depends on a type, group, or node; the kit's `customViewHref` constructs it.

A reusable group page needs only a generic registration and the shared context and services, as the bundled group views show. Group membership is navigation metadata, never an ontology relationship.
