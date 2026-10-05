# Kit API

## `@rhizome/kit`

| Export | Use |
| --- | --- |
| `getViewContext()` / `useViewContext()` | Read the invocation's type/interface/group/node/standalone context. The hook works in React; the accessor also works in HTML modules. A context change remounts the page. |
| `getViewConfiguration()` | Read the definition's JSON-compatible `configuration` object, or `undefined`. It is authored configuration, never trusted from a URL override. |
| `getViewInvocation()` | Read `{ view: { id, name, origin }, context, configuration }`; the same values except `origin` arrive as default-export component props (`ViewModuleProps`). `origin` is `bundled` or `repository`. |
| `useViewPreference<T>(key, { defaultValue, validate, slot? })` | Read and update one personal setting for the current view instance. Returns `{ value, overridden, loading, pending, error, set, update, reset, retry }`. Mutations return promises. |
| `getViewPreference<T>(key, { defaultValue, validate, slot? })` | The same setting in HTML or non-React code: `getSnapshot()`, `subscribe(listener)` returning an unsubscribe function, and promise-based `set`, `update`, `reset`, `retry`. |
| `useViewRows(id, request?, options?)` | Execute a native definition through the shared collection engine. Returns TanStack query state whose `data` contains canonical `rows`, capabilities, layouts, warnings, pagination, and normalized state. Reads include staged edits and key by invocation context. Also returns `displaySession`, `savedEditsPending`, and `editOutcome` for reader-bound edits through save. Pass `rows.displaySession` to `useSetField` when editing its rows. `options.optimistic(data, displaySession)` can apply staged values while the executor catches up. |
| `executeView(id, request?)` | The same native row executor as a promise against committed state. Custom definitions do not execute as native collections. |
| `useGraphQL<T>(query, variables?, options?)` | TanStack `useQuery` over the vault's read-only GraphQL endpoint. Returns `{ data, error, isPending, displaySession, savedEditsPending, editOutcome, partialErrors, ... }`. Reads include workspace edits and refresh on vault changes. `options.optimistic(data, displaySession)` can apply staged field values to your result shape while reads catch up. A response with errors fails the query unless `options.partial` is set and every error lies inside a root field that returned data, such as a record missing a required field. GraphQL then nulls the failing field, or its nearest nullable parent when the field is non-null (which can be the whole record), and `partialErrors` lists the tolerated errors (`{ message, path? }`) so the view can tell which records they touched. |
| `graphql<T>(query, variables?)` | The same request as a promise, against committed state. Throws on GraphQL errors. |
| `useSetField(readSession?)` | Mutation: `mutate({ target, field, value })`. Writes through the surrounding `EditSession`. Pass `query.displaySession` when editing a `useGraphQL` result. |
| `setField(target, field, value)` | Commits the same write as a promise, whatever the `EditSession`. |
| `EditSession` | `<EditSession mode="immediate">` around a view or part of one makes `useSetField` commit each write. Without it the mode is `workspace`, in any React tree, including one the view mounts itself. |
| `NoteLink` | `<NoteLink path="docs/a.md">label</NoteLink>`. Opens the note in the Rhizome workspace when the view is embedded, navigates when standalone. Cmd or Ctrl click opens the note beside the current tab when embedded, and in a new browser tab when standalone. |
| `openNote(path, { beside? })` | The same navigation from code. |
| `noteHref(path)` | The note's URL in the Rhizome UI. |
| `openNode(ref, { view?, beside? })` | Open a canonical root or embedded node, optionally selecting a named node presentation. Uses the workspace bridge when embedded. |
| `nodeHref(ref, { view? })` | A normal Rhizome node link preserving fragment, node identity, and requested presentation. |
| `openView(id, context)` | Open a registered native/custom view or normalized choice in the Rhizome workspace for an explicit subject, from either embedded or standalone code. A native definition ID selects its configured variant. |
| `workspaceViewHref(id, context)` | Build the corresponding Rhizome workspace URL, including a selected presentation. |
| `customViewHref(id, context)` | A contextual `/views/<id>` URL for embedded/direct launch parity. |
| `openCollection(name)` | Open a type or interface collection in the workspace. |
| `openIssues(scope?)` | Open the issues panel, optionally scoped to `{ kind: "type" \| "interface" \| "note", key }`. Without a scope, a view presenting a note opens that note's issues; a view anywhere else, or a standalone page, opens every issue. |
| `embedded` | `true` when the Rhizome UI frames the view, `false` when it is the page. A cross-origin parent, such as an IDE preview, does not count: the view runs as its own page there. |

Context shapes:

```ts
type ViewContext =
  | { kind: "standalone" }
  | { kind: "type"; type: string }
  | { kind: "interface"; interface: string }
  | { kind: "group"; group: string }
  | { kind: "node"; type: string; ref: NodeRef };
```

`"*"` is an applicability pattern, never an invocation group. Node refs require `notePath` and `kind`; preserve all supplied identity fields. Query `ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }` for contextual navigation, using the alias to match the REST/kit field name, and pass it unchanged. Native rows already carry canonical refs. Ordinary `NoteLink`/`openNote` are conveniences for whole-note paths; use canonical node helpers for embedded nodes.

`useViewRows` accepts the public native execution request: `variant`, `search`, `filters`, `sort`, `group`, and `page`, among its schema-defined fields. Discover the existing definition and API before supplying field names. The hook handles the workspace session; do not pass a separately created session or duplicate collection logic. See `mounted-example.md`.

Writes:

- `target` is the node's `ref`. Always select `ref { notePath fragment nodeId structuralFingerprint }` on anything you intend to write, and pass that object unchanged.
- Bind edits to the query whose rows supply their targets: `const query = useGraphQL<Data>(queryText); const setField = useSetField(query.displaySession);`. Each reader retains its own saved-reference mappings until its canonical response arrives. Passing its display session safely maps edits on retained rows without changing fresh rows from another reader. `useSetField()` remains available for targets that need no retained-reference mapping.
- `value` is a string, a string array for list fields, or `null` to unset. Checkbox-backed fields such as an action item's `done` take `"true"` or `"false"`.
- Where a write goes depends on the `EditSession` mode:
  - `workspace` (default): when the Rhizome workspace frames the view, the write is staged in the workspace's edit session. The view shows it immediately, and the user saves or discards it with the workspace's other edits. A write made before the workspace connects waits for it, and fails rather than commits if the frame is not Rhizome's. When the view is its own page, the write commits as in `immediate`.
  - `immediate`: the write commits to Markdown and refreshes the metadata and ontology read models before success. A freshness event refreshes mounted readers. There is no undo beyond Git. If the workspace has unsaved edits to the same note, saving them later reports a conflict to resolve in Review.
- Keep the default for ordinary editing, toggles included. The user reviews staged changes before saving. Choose `immediate` when each write must reach the file by itself, for example a view that another process watches.
- Anything the kit does not wrap is still available: the view is same-origin with the REST API described by `/openapi.yaml`.

GraphQL is read-only. Enum values are lowercase where the schema says so (`direction: asc`). Default `first` is 20; pass a larger `first` for a board or report.

### Display edits through save

An optimistic updater returns a new result with the supplied session's operations applied. Match each operation to its target `ref`; apply absolute field values rather than increments, since server data may already include the operation. Keep this function pure and leave unrelated rows unchanged. Rhizome does not infer your query's shape or sort order.

`savedEditsPending` stays true while a successful save awaits its canonical query result. The updater receives retained saved operations followed by newer dirty operations, so the display remains stable. A request started before successful persistence cannot clear those retained operations. `editOutcome` reports the latest workspace save or discard outcome as `saved`, `discarded`, `failed`, `conflicted`, or `null`. Query errors still appear in `error`; a refresh error does not mean persistence failed.

## Personal view preferences

`useViewPreference<T>(key, { defaultValue, validate, slot? })` uses the current registered invocation, including its view ID and concrete context. HTML calls `getViewPreference` with the same arguments and subscribes to snapshot changes. Launch through the registered view URL so the invocation is available. Canonical node identity, vault cache identity, persistence, and synchronization belong to the library; preserve the supplied context and avoid hand-built storage keys.

- Use a stable semantic key such as `showCompleted` or `rowType`. Supply a predicate `(value: unknown) => value is T` that checks the complete value, including supported enum choices or existing schema fields. Invalid stored values use the default and expose an error; invalid writes reject.
- `defaultValue` is the built-in fallback refined by validated authored `configuration`. Reading or mounting never saves it. An absent override follows later default changes; setting a value equal to the default removes the override.
- `value` is effective state; `overridden` reports a valid personal override. `loading` covers the initial read, `pending` queued writes, and `error` failed reads or writes or invalid stored data. Wait for hydration before running a native query whose request depends on the setting. After a failed read, defaults can render alongside an error and retry.
- `set(value)` and `update(pureFn)` apply optimistic changes. Update functions may be replayed while rebasing conflicts, so keep them pure. A failed write retains its unsaved value and visible error and rejects its promise. Handle the rejection, keep the error visible, and offer `retry()`; do not report it as saved.
- `reset()` removes this key's override in this widget, rather than saving a copy of today's default. The host's custom-view reset clears the instance and all of its child widget preferences, including widgets not currently mounted. It stays within the same view, concrete context, and host instance; other instances remain independent. Neither reset changes shared configuration or staged content edits.
- Use `slot: "comparison-left"` only when distinct widgets intentionally need separate preferences within one invocation. The kit nests this widget identity under the inherited host instance, so the same widget slot in two host instances remains independent. Keep it authored and stable, never a mount ID, label, random value, or tab ID. For group expansion, include the grouping field and stable group identity in the key so changing grouping cannot reuse another field's choices.

Preferences persist in ignored `.rhizome/user-state.sqlite` for this checkout, independently of the rebuildable index. They survive browser-origin changes but are not cloud-synced or shared across separate checkouts. Matching mounted clients receive acknowledged changes; refresh after focus or reconnect covers missed notifications. Shared structure remains in `.rhizome/views/`: promote it only through an explicit repository configuration save, never every click. Personal-only widths and expansion choices remain after that save.

See `example.md` for a board toggle and `mounted-example.md` for an HTML subscription.

## Schema, membership, and validation

These hooks read public APIs through TanStack Query and refresh with the [freshness](#freshness) classes. Their result types (`DisplayGroup`, `DisplayGroupMember`, `TypeDoc`, `TypeFieldDoc`, `EnumValueDoc`, `TypeProfile`) are exported from `@rhizome/kit`.

| Export | Use |
| --- | --- |
| `useDisplayGroups()` | Query state plus `groups`: every effective display group from `GET /api/v1/display-groups`, sorted by name with the rail's `Other` group (ungrouped types) last. |
| `useDisplayGroup(name?)` | One group's members, defaulting to the invocation's group. `group` is `undefined` while loading and `null` when no such group exists. Members are the roots the Notes rail lists under the group, with `name`, `kind` (`type` or `interface`), `label`, `pluralLabel`, `description`, `count`, `issueCount` (published validation issues in the member's type or interface scope, the count `openIssues({ kind, key: name })` opens on), `implementors`, and nested `children`. |
| `useTypeDocs(names)` | `{ docs, isLoading, error }`: type documentation from `GET /api/v1/ontology/types/{name}?notes=none`, keyed by the names that have loaded. Lists such as `fields` and `enums` are always present, possibly empty, and labels fall back to the type name. A response that does not match this contract fails the read with an error naming the mismatched part, as does one from `useDisplayGroups`. |
| `useValidationSummaries(scopes)` | `{ summaries, generation, isLoading, error }` for the current validation generation. `summaries` is a `Map`; read it with `summaries.get(validationScopeKey(scope))`. A scope is `{ kind, key? }` with kind `global`, `file`, `note`, `node`, `type`, or `interface`. Memoize the scopes array. |

Type documentation includes everything a generic page needs to read a type without naming it:

- `fields[]` with `kind`, `typeName`, `required`, `list`, `display` (`role` `SUMMARY` or `PARENT`, `importance` `KEY`, `NORMAL`, or `DETAIL`), `policy.reason`, and `requiredWhen` conditions (`{ field, equals }`; the field is required whenever any condition holds).
- Enum values with their `@view` `label`, `order`, `tone`, and `collapsed` flag, on `enums[]` and on each enum field. When a value declares `@view(stage:)` without a tone or `collapsed`, those report the stage's defaults (`open` neutral, `active` progress, `done` success, `dropped` muted and collapsed). Each value also reports its lifecycle `stage` (`open`, `active`, `done`, or `dropped`) and `stageDeclared`, true when the schema declares the stage and absent when it was inferred from authored tone or `collapsed`; `stage` is absent when the enum has no stages.
- `summaryField`, including the conventional `summary` field, and `parentField`, the field carrying `@display(role: PARENT)`.
- `profile`, the server's schema-derived type profile, on note types and interfaces (absent on section and embedded types): `shape` (`workflow`, `contract`, `dated`, `catalog`, or `reference`), `lifecycleField`, `summaryField`, and `primaryDateField` when the type has one, and the field lists `orderedFields`, `categoryFields`, `peopleFields`, `keyTextFields`, `relationFields`, `reverseFields`, and `gapFields`, always present and possibly empty. Read lifecycle, gaps, and key text from it rather than guessing from field names or tones: a record is in motion when its `lifecycleField` value has stage `active`, finished when it has `done` or `dropped`, and `gapFields` are the KEY fields a record may leave empty. An interface's profile names the interface's own fields.
- `label`, `pluralLabel`, `description`, `implements`, and `companionDocs`.

GraphQL note and embedded types expose `updatedAt` (the source note's indexed modification time, RFC3339) and `issueCount` (issues in the published validation snapshot). Select them like any field, within these limits:

- `updatedAt` is the file system modification time, not the last commit. A fresh clone or checkout gives every file the same time, so "recently changed" and "stale" signals stay flat until files change locally.
- An authored field named `updatedAt` or `issueCount` replaces the runtime one and keeps its authored type.
- An interface declares them only when every implementor carries them, so a generic view should select them on concrete types (`... on FeatureArea { updatedAt }`), not on an interface.
- Typed roots sort by `updatedAt`: `featureArea(first: 50, sort: [{ field: "updatedAt", direction: desc }])` reads the 50 most recently changed records, however many the type has.

List `@reverse` and `@neighbors` fields take `first`, which caps the targets read per record, and have a `<field>Count` that counts every target. A record of a hub type can have hundreds of links in, so read the count for "how many" and a capped list (`specs(first: 20) { title }`) only where the view shows targets.

## Components

| Export | Use |
| --- | --- |
| `StatusMark` | `<StatusMark value={record.status} values={enumValues} />` renders a lifecycle value: the fill shows its position among non-terminal values, the color its tone, and the label always accompanies it. `hideLabel` keeps the label for the tooltip and assistive technology. Values in the `done` or `dropped` stage are terminal, whatever their tone; an enum without stages has no terminal values. `orderedEnumValues`, `isTerminalValue`, and `statusPosition` expose the same rules. |
| `HighlightProvider` / `RecordChip` | `<RecordChip recordKey={path} mark={...} indirect onOpen={...}>label</RecordChip>` is a compact record reference. Inside one `HighlightProvider`, pointing at or focusing a chip highlights every chip with the same `recordKey`; only chips whose state changes re-render. The label truncates to one line; `wrap` lets it take two, for narrow table cells. `indirect` marks a record reached through another record: the chip turns italic and fainter, screen readers hear "(indirect)", and `data-indirect` is set for further styling. |
| `RelativeTime` | `<RelativeTime value={record.updatedAt} />` renders a compact age ("5m", "3h", "12d", then a date) with the exact time as its tooltip; screen readers hear both. It does not tick: the age is computed when the view renders, which vault changes trigger. Pass `now` to set the comparison time. |
| `typeLabel(labels, { count?, plural?, prefix? })` | A type's schema label, plural when `plural` is set or `count` is not 1, with a shared prefix dropped. |
| `sharedLabelPrefix(labels)` | The first word every label shares, with its trailing space, or `""`. Pass it to `typeLabel` as `prefix` inside a group page. |

## Freshness

A view's kit queries stay current without polling. A frame hosted by the workspace receives the workspace's vault, schema, and validation events by `postMessage` and opens no event stream; a standalone page opens its own. Events within 150 ms refresh once, by class:

- Index and node changes refresh data: GraphQL, native rows, display groups, and a view's own `useQuery` keys.
- Schema changes refresh type documentation as well as data.
- Validation changes refresh validation summaries and display-group issue counts.
- A reconnected stream refreshes everything, because missed events are not replayed. A hosted repository view also compares its folder stamp then and reloads if its source changed while the stream was down.

A `views.changed` event reloads a repository view whose own folder changed. Bundled views never reload. A standalone repository page also checks its folder each second as a fallback; a hosted frame does not.

## `@rhizome/ui`

shadcn/ui components with their usual names, props, and composition: `Button` (`variant`, `size`, `asChild`), `Badge`, `Card` `CardHeader` `CardTitle` `CardDescription` `CardAction` `CardContent` `CardFooter`, `Input`, `Label`, `Checkbox`, `Select` `SelectTrigger` `SelectValue` `SelectContent` `SelectItem` `SelectGroup` `SelectLabel` `SelectSeparator`, `Dialog` `DialogTrigger` `DialogContent` `DialogHeader` `DialogTitle` `DialogDescription` `DialogFooter` `DialogClose`, `Tabs` `TabsList` `TabsTrigger` `TabsContent`, `Table` `TableHeader` `TableBody` `TableRow` `TableHead` `TableCell` `TableCaption` `TableFooter`, `Tooltip` `TooltipTrigger` `TooltipContent` `TooltipProvider`, `Separator`, `Skeleton`, and `cn`.

Import them from `@rhizome/ui`, not from `@/components/ui/...`. Icons come from `lucide-react`. A shadcn component that is not listed can be written in the view folder from its usual source; `radix-ui` is not in the import map, so build it on plain elements or import Radix from a URL.
