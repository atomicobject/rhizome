# Kit API

## `@rhizome/kit`

| Export | Use |
| --- | --- |
| `getViewContext()` / `useViewContext()` | Read the invocation's type/interface/group/node/standalone context. The hook works in React; the accessor also works in HTML modules. A context change remounts the page. In an [experience](experiences.md) the hook reads the page it renders in and the accessor the page on screen, so components use the hook. |
| `getViewConfiguration()` / `useViewConfiguration()` | Read the definition's JSON-compatible `configuration` object, or `undefined`. It is authored configuration, never trusted from a URL override. |
| `useViewLocation()` | `{ viewId, tabId?, context, params, configuration? }` for the page this component renders in; `tabId` is the workbench tab (`panel` in a panel) and is set only in an experience frame. |
| `useViewActive()` | False while the page is hidden or its frame suspended. Kit queries and action polling pause on their own; stop your own timers, intervals, and animations with it. |
| `markReady()` / `useMarkReady()` | Mark the page's required reads as resolved, once; performance diagnostics time data-ready from it. `useMarkReady()` returns the function bound to the calling page, for async work that may finish while the page is hidden; `markReady()` marks the page on screen. |
| `getViewInvocation()` | Read `{ view: { id, name, origin }, context, configuration }`; the same values except `origin` arrive as default-export component props (`ViewModuleProps`). `origin` is `bundled` or `repository`. |
| `useViewPreference<T>(key, { defaultValue, validate, slot? })` | Read and update one personal setting for the current view instance. Returns `{ value, overridden, loading, pending, error, set, update, reset, retry }`. Mutations return promises. |
| `getViewPreference<T>(key, { defaultValue, validate, slot? })` | The same setting in HTML or non-React code: `getSnapshot()`, `subscribe(listener)` returning an unsubscribe function, and promise-based `set`, `update`, `reset`, `retry`. |
| `useViewRows(id, request?, options?)` | Execute a native definition through the shared collection engine. Returns TanStack query state whose `data` contains canonical `rows`, capabilities, layouts, warnings, pagination, and normalized state. Reads include staged edits and key by invocation context. Also returns `displaySession`, `savedEditsPending`, and `editOutcome` for reader-bound edits through save. Pass `rows.displaySession` to `useSetField` when editing its rows. `options.optimistic(data, displaySession)` can apply staged values while the executor catches up. |
| `executeView(id, request?)` | The same native row executor as a promise against committed state. Custom definitions do not execute as native collections. |
| `useGraphQL<T>(query, variables?, options?)` | TanStack `useQuery` over the vault's read-only GraphQL endpoint. Returns `{ data, error, isPending, displaySession, savedEditsPending, editOutcome, partialErrors, ... }`. Reads include workspace edits and refresh on vault changes. `options.optimistic(data, displaySession)` can apply staged field values to your result shape while reads catch up. A response with errors fails the query unless `options.partial` is set and every error lies inside a root field that returned data, such as a record missing a required field. GraphQL then nulls the failing field, or its nearest nullable parent when the field is non-null (which can be the whole record), and `partialErrors` lists the tolerated errors (`{ message, path? }`) so the view can tell which records they touched. |
| `graphql<T>(query, variables?)` | The same request as a promise, against committed state. Throws on GraphQL errors. |
| `readNodes<T>(refs, selection, { signal? })` | Read every node `refs` names with `selection` (fields of the node, usually inline fragments such as `... on Assertion { path title }`) in batches of `NODES_BATCH_MAX` (200), against committed state. Returns `{ nodes, unresolved, errors, truncated, complete }`: one node per distinct canonical identity in ref order, refs that resolved to nothing with the server's reason, field errors from every batch, capped reads, and whether all of it was clean. Batches answered at different index generations are read once more. A failed batch throws. |
| `responseGeneration(data)` | The index generation attached to data returned by `graphql`, `useGraphQL`, or an aggregate `readNodes` result. Undefined when the response omitted generation or the kit did not read the data. Compare it across related reads, together with source revision for source-link pages; missing or changed values cannot prove coherent coverage. This exposes existing response facts without changing the read protocol. |
| `Markdown` | `<Markdown skipHtml>{source}</Markdown>` renders full authored Markdown with the existing memoized GFM, code, and Mermaid renderer. Pass stable `components` and `urlTransform` props when customizing them. Use canonical source-link data separately for graph identity and navigation; rendered links do not establish resolved scope. |
| `convertWikilinks(source)`, `resolveRenderedLinkTarget(href, { links })` | The native narrative reader's conversion and known-link handling. Pass stable Markdown components. For unresolved source-relative targets, retain authored Markdown or wikilink syntax and use `getNodePreview` with the containing path; do not resolve against the view iframe URL. Rendered links never prove frozen scope. |
| `getNodePreview(target, from?, { signal? })` | The existing public preview read. It resolves the authored target relative to its source through the canonical server resolver. `fragmentResolved` distinguishes exact fragment evidence. Navigate using its returned canonical `ref`, and keep failed reads explicit. |
| `markdownChecklist(source)` | Returns `{ checked, total }` for recorded GFM task items using the renderer's Markdown grammar. Counts nested and quoted tasks and excludes code examples and prose. These counts describe authored checkboxes, not verified delivery or acceptance. |
| `truncation(data)` | The capped reads in data `graphql` or `useGraphQL` returned, from `extensions.truncated`: `[{ path, returned, limit }]` for each `nodes(refs:)`, `search`, or `neighborhood` read that stopped at its limit, one entry per response path without list indices. Empty when nothing was cut off. Say what was not read instead of presenting the length. |
| `typedRootCoverage(data)` | Typed-root coverage for data that `graphql` or `useGraphQL` returned, by response alias: `{ assertion: { status, requestedFirst, returnedCount, offset, hasMore, nextOffset } }`. Status `complete` means the root returned every match; any other status (`capped`, `unknown_page_bound`, `unknown_candidate_bound`) means the array is not the whole set, so say what was not read instead of presenting its length as a total. Undefined for data the kit did not read or a query with no generated type root. It is keyed by the data object, so it follows the data on screen, and `useGraphQL`'s return shape is unchanged. Nested lists are not covered: declared relation fields (`@neighbors`, `@contains`) without `first` return every row, so leave `first` off them in whole-set reads. |
| `useSetField(readSession?)` | Mutation: `mutate({ target, field, value })`. Writes through the surrounding `EditSession`. Pass `query.displaySession` when editing a `useGraphQL` result. |
| `setField(target, field, value)` | Commits the same write as a promise, whatever the `EditSession`. |
| `EditSession` | `<EditSession mode="immediate">` around a view or part of one makes `useSetField` commit each write. Without it the mode is `workspace`, in any React tree, including one the view mounts itself. |
| `NoteLink` | `<NoteLink path="docs/a.md">label</NoteLink>`. Opens the note in the Rhizome workspace when the view is embedded, navigates when standalone. Cmd or Ctrl click opens the note beside the current tab when embedded, and in a new browser tab when standalone. Styled as a note link (teal with a light underline); `className` adds to it. |
| `NodeLink` | `<NodeLink node={row.ref} view="team.detail">{row.title}</NodeLink>`. The same link for a canonical note or embedded node, optionally opening a named node presentation. |
| `openNote(path, { beside? })` | The same navigation from code. |
| `noteHref(path)` | The note's URL in the Rhizome UI. |
| `openNode(ref, { view?, beside? })` | Open a canonical root or embedded node, optionally selecting a named node presentation. Uses the workspace bridge when embedded. |
| `nodeHref(ref, { view? })` | A normal Rhizome node link preserving fragment, node identity, and requested presentation. |
| `openView(id, context, params?)` | Open a registered native/custom view or normalized choice in the Rhizome workspace for an explicit subject, from either embedded or standalone code. A native definition ID selects its configured variant. `params` sets the target's [view params](#view-params). |
| `workspaceViewHref(id, context, params?)` | Build the corresponding Rhizome workspace URL, including a selected presentation and param values. |
| `customViewHref(id, context, { params? })` | A contextual `/views/<id>` URL for embedded/direct launch parity. |
| `useViewParams()` / `getViewParams()` / `subscribeViewParams(listener)` / `setViewParams(patch)` | Read and change the view's declared URL params. See [View params](#view-params). |
| `useActionQuery(id, input?, options?)` / `useActionCommand(id)` / `ActionError` | Run a module action. See [Module actions](#module-actions). |
| `openCollection(name)` | Open a type or interface collection in the workspace. |
| `openSearch({ folder?, query? })` | Open a project search tab, ranking matches when `query` is set. `folder` filters it to a vault folder, listing the folder's notes without a query; `folder: "/"` lists the notes at the vault root, which sit in no folder. Without `folder` the search covers the whole vault. |
| `openIssues(scope?)` | Open the issues panel, optionally scoped to `{ kind: "type" \| "interface" \| "note", key }`. Without a scope, a view presenting a note opens that note's issues; a view anywhere else, or a standalone page, opens every issue. |
| `usePageNavigation()` | `{ openNode, openView, openNote, openIssues, openCollection, openSearch, setViewParams }` bound to the page this component renders in. In an experience a hidden page's late call, such as a debounced param change after a tab switch, still names its own page; the imperative exports act for the page on screen. |
| `RecordPage` | The record-page shell for experiences. See [Record pages](experiences.md#record-pages). |
| `embedded` | `true` when the Rhizome UI frames the view, `false` when it is the page. A cross-origin parent, such as an IDE preview, does not count: the view runs as its own page there. |

Context shapes:

```ts
type ViewContext =
  | { kind: "standalone" }
  | { kind: "workspace" }
  | { kind: "type"; type: string }
  | { kind: "interface"; interface: string }
  | { kind: "group"; group: string }
  | { kind: "node"; type: string; ref: NodeRef };
```

`workspace` is the All notes page, which has no subject. `"*"` is an applicability pattern, never an invocation group. Node refs require `notePath` and `kind`; preserve all supplied identity fields. Query `ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }` for contextual navigation, using the alias to match the REST/kit field name, and pass it unchanged. Native rows already carry canonical refs. Ordinary `NoteLink`/`openNote` are conveniences for whole-note paths; use canonical node helpers for embedded nodes.

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

## View params

Params put a view's navigable state on its URL, so a link can open it on a tab or item and Back undoes a change. Declare them in the definition:

```yaml
params:
  - { name: tab, kind: string, default: overview }   # UI state: updates in place
  - { name: item, kind: string, identity: true }     # part of the invocation: remounts
```

`kind` is `string`, `int`, or `bool`; a `default` must match it. Names match `[A-Za-z][A-Za-z0-9_]*` and cannot be a key the workspace URL already uses, such as `view`, `presentation`, `note`, `search`, `scope`, `folder`, or `context`; `rzm validate views` lists the rest.

- `useViewParams()` returns `{ [name]: value }` for the declared params, each read as its kind, with defaults applied. A param with no default and no value is absent. Undeclared keys and values that do not parse are ignored. `getViewParams()` and `subscribeViewParams(listener)` do the same for HTML.
- `setViewParams({ tab: "health" })` adds a history entry. A UI param updates `useViewParams()` without remounting; an identity param remounts the view with the new value. `null` returns a param to its default, and a default stays off the URL. An undeclared name or a value of the wrong kind throws.
- Back and Forward follow the same split: a changed identity param remounts, anything else updates in place.
- Links carry params as plain query keys: `/notes?view=team.board&tab=health` for a standalone mount, `/notes?presentation=team.board&item=a` on All notes, `/notes/<Type>?presentation=team.board&tab=health` on a type, and `/views/team.board?tab=health` for the page alone. Build them with `workspaceViewHref(id, context, params)` or `openView(id, context, params)`.
- Keep params for state worth linking to or going back over. A remembered display choice belongs in `useViewPreference`; transient UI state stays in React.

## Module actions

A module's actions, declared in `.rhizome/modules/<module>/actions/<name>.yaml`, run a command or script the module ships and answer with `{ status: "ok" | "refused" | "error", result?, reason?, stderrTail? }`. Ids are `<module>/<name>`.

```tsx
const health = useActionQuery<Health>("compounding/health");
const dismiss = useActionCommand<null, { cluster: string; reason: string }>("compounding/dismiss");

<Section title="Health" status={sectionStatus(health)} what="health">{() => <Report data={health.data} />}</Section>
<Button onClick={() => dismiss.mutate({ cluster, reason })} disabled={dismiss.isPending}>Dismiss</Button>
{dismiss.error && <p className="text-risk">{dismiss.error.reason ?? dismiss.error.message}</p>}
```

- `useActionQuery(id, input?, options?)` is a TanStack query keyed `["action", id, input]` whose `data` is the ok `result`. It polls every 15 s while the page is visible, refetches on vault changes like other data, and never retries a refused action. `options` takes ordinary query options such as `enabled`.
- `useActionCommand(id)` is a TanStack mutation; `mutateAsync(input)` resolves to the ok `result`. On ok it refetches every `useActionQuery` once.
- Both reject with `ActionError`, whose `status` is `refused` (a deliberate no, such as an untrusted repository; show `reason`) or `error` (a failure, an unknown action, or invalid input; `stderrTail` holds the command's last output). Show the reason beside the control that ran it.
- Input is a JSON object matching the action's declared inputs; the server rejects anything else. An action runs with the user's authority, so run commands only from a deliberate click.

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
| `useTypeColors()` | A function `(name) => color` giving a type its palette color as a CSS value (`var(--type-N)`), the same hue the Atlas, Explorer, and type pages use; interfaces get the neutral. Use it for type dots, legends, and graph nodes so a type keeps one color everywhere. Colors are for marks, never text. |
| `useValidationSummaries(scopes)` | `{ summaries, generation, health, isLoading, error, refreshGeneration }` for the published validation generation; `generation` is null until validation publishes. `health` is the run's state (`current_clean`, `current_issues`, `stale`, `running`, `incomplete`, `failed`, or `never_checked`); show a clean result only for `current_clean`. Pass `generation` to a generation-bound read such as `POST /api/v1/validation/groups`; when it answers 410 Gone, call `refreshGeneration()` to reread the published generation. `summaries` is a `Map`; read it with `summaries.get(validationScopeKey(scope))`. A scope is `{ kind, key? }` with kind `global`, `file`, `note`, `node`, `type`, or `interface`. Memoize the scopes array. |

Type documentation includes everything a generic page needs to read a type without naming it:

- `fields[]` with `kind`, `typeName`, `required`, `list`, `display` (`role` `SUMMARY`, `PARENT`, or `RANK`, `importance` `KEY`, `NORMAL`, or `DETAIL`), `policy.reason`, and `requiredWhen` conditions (`{ field, equals }`; the field is required whenever any condition holds).
- Enum values with their `@view` `label`, `order`, `tone`, and `collapsed` flag, on `enums[]` and on each enum field. When a value declares `@view(stage:)` without a tone or `collapsed`, those report the stage's defaults (`open` neutral, `active` progress, `done` success, `dropped` muted and collapsed). Each value also reports its lifecycle `stage` (`open`, `active`, `done`, or `dropped`) and `stageDeclared`, true when the schema declares the stage and absent when it was inferred from authored tone or `collapsed`; `stage` is absent when the enum has no stages.
- `summaryField`, including the conventional `summary` field, and `parentField`, the field carrying `@display(role: PARENT)`.
- `profile`, the server's schema-derived type profile, on note types and interfaces (absent on section and embedded types): `shape` (`workflow`, `contract`, `dated`, `catalog`, or `reference`), `lifecycleField`, `summaryField`, `rankField`, and `primaryDateField` when the type has one, and the field lists `orderedFields`, `categoryFields`, `peopleFields`, `keyTextFields`, `relationFields`, `reverseFields`, and `gapFields`, always present and possibly empty. Read lifecycle, gaps, and key text from it rather than guessing from field names or tones: a record is in motion when its `lifecycleField` value has stage `active`, finished when it has `done` or `dropped`, and `gapFields` are the KEY fields a record may leave empty. An interface's profile names the interface's own fields.
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
| `StatusMark` | `<StatusMark value={record.status} values={enumValues} />` renders a lifecycle value as a 9px mark that names its tone by shape and color: a hollow ring for neutral, a half-filled ring for `info` or `progress`, a filled dot for `success`, a triangle for `warning`, a disc with an exclamation cut out for `risk`, and a dashed ring for `muted`. The label always accompanies it. A value without a tone takes one from its position: `done` succeeds, `dropped` is muted, an active value past the first is in progress, and the first is neutral. `hideLabel` keeps the label for the tooltip and assistive technology. Values in the `done` or `dropped` stage are terminal, whatever their tone; an enum without stages has no terminal values. `orderedEnumValues`, `isTerminalValue`, and `statusPosition` expose the same rules. |
| `HighlightProvider` / `RecordChip` | `<RecordChip recordKey={path} mark={...} indirect onOpen={...}>label</RecordChip>` is a compact record reference. Inside one `HighlightProvider`, pointing at or focusing a chip highlights every chip with the same `recordKey`; only chips whose state changes re-render. The label truncates to one line; `wrap` lets it take two, for narrow table cells. `indirect` marks a record reached through another record: the chip turns italic and fainter, screen readers hear "(indirect)", and `data-indirect` is set for further styling. |
| `RelativeTime` | `<RelativeTime value={record.updatedAt} />` renders a compact age ("5m", "3h", "12d", then a date) with the exact time as its tooltip; screen readers hear both. It does not tick: the age is computed when the view renders, which vault changes trigger. Pass `now` to set the comparison time. |
| `Diagram` | `<Diagram label="Ideas by impact" source={source} links={links} />` renders Mermaid text with Pele and the view's theme. `label` is required; `links` maps diagram ids to canonical node refs. Empty or whitespace source renders nothing. Optional `className` styles the container. See [Diagrams](#diagrams) for the API and examples. |
| `mermaid` | `text(value)` escapes non-CSV labels with Mermaid entities; `csv(value)` produces a quoted Sankey CSV field; `id(key)` produces a deterministic, syntax-safe node id. |
| `typeLabel(labels, { count?, plural?, prefix? })` | A type's schema label, plural when `plural` is set or `count` is not 1, with a shared prefix dropped. |
| `sharedLabelPrefix(labels)` | The first word every label shares, with its trailing space, or `""`. Pass it to `typeLabel` as `prefix` inside a group page. |

## Page primitives

The blocks Rhizome's own pages are built from. They carry the house style, so a page made of them needs little Tailwind beyond layout.

| Export | Use |
| --- | --- |
| `Section` | `<Section title="Opportunities" count={rows.length} caption="Ranked by signal." controls={…} status={sectionStatus(query)} what="opportunities" empty={rows.length ? undefined : "No opportunities yet."}>{() => …}</Section>`. A block with a bold heading, a count, controls at the right, and a caption. `info` puts a short explanation of a computed judgement behind an info mark beside the title; `info={{ text, display: "title" }}` keeps descriptive help on the heading's tooltip with no mark. Assistive technology hears either as the section's description. Its heading paints at once; the body shows "Loading opportunities…" after 150 ms, a failure with Retry, the one-line `empty` text, or the children. Pass children as a function to build them only once the data is ready. |
| `SectionBody` | The same body states without the heading, for a Section whose controls need loaded data. |
| `sectionStatus(...queries)` | One status from any TanStack queries (`isPending`, `error`, `refetch`): failed when any failed, with a retry that refetches each failed query; loading while any is pending. |
| `Facts` / `Fact` | `<Facts note="Ranked by the opportunity's rank field."><Fact value={12}>opportunities</Fact><Fact tone="risk" value={2}>with issues</Fact></Facts>`. The strip of figures under a page title, on the right, an optional note on the left. `tone="ok"` adds a check. |
| `Count` | `<Count>{n}</Count>`: unboxed tabular numerals. `tone="risk"` for problem counts, with a word beside it saying what is counted. |
| `Dash` | An empty value: "—" on screen, "None" to screen readers. |
| `Segmented` | `<Segmented label="Lens" value={lens} onChange={setLens} options={[{ value: "rank", label: "Rank" }, { value: "impact", label: "By impact", count: 5 }]} />`. One mode switch; the chosen segment is raised on a surface. Native radios underneath, so arrow keys move the choice. `showLabel` shows the label before it. |
| `MoreToggle` | `<MoreToggle hidden={rest.length} open={open} onToggle={…} />`: "Show N more" on its own line, revealing the rest in place, then "Show fewer". |
| `DisclosureIcon` | `<DisclosureIcon open={open} />`: the one disclosure chevron, pointing right and turning down when open. Put it inside a control with an accessible name; a bare icon button takes the `rz-disclosure` class for a 24px target. |
| `BarList` | `<BarList label="Time by office" items={[{ label: "Ann Arbor", value: 35.25, display: "35.25 h", series: 2, onOpen: () => openNote(path) }]} />`. Proportional bars as a list, for a handful of named amounts; long labels truncate rather than drop. `max` fixes the scale; `series` picks `--series-N` or `"link"`. |

The theme also gives Tailwind `bg-surface`, `bg-sunk`, `bg-tint`, `text-faint`, `text-link`, and `text-risk`, and the CSS variables `--series-1` through `--series-8`, `--series-link`, and `--type-1` through `--type-8` (each with an `-ink` text variant) for colors a view draws itself. Every token has a dark value, chosen by the operating system.

A small mounted page:

```tsx
import { Count, Fact, Facts, NodeLink, Section, sectionStatus, useGraphQL } from "@rhizome/kit";

// The type and its reverse-link count come from the vault's schema; discover them first.
export default function Features() {
  const query = useGraphQL<{ feature: Feature[] }>(`{ feature(first: 200) { ref { notePath kind fragment nodeId } title specsCount } }`);
  const features = query.data?.feature ?? [];

  return (
    <main className="px-5 py-3">
      <Facts><Fact value={features.length}>features</Fact></Facts>
      <Section title="Most specified" status={sectionStatus(query)} what="features" empty={features.length ? undefined : "No features yet."}>
        {() => (
          <ul className="divide-y divide-[var(--border-faint)]">
            {features.map((feature) => (
              <li key={feature.ref.notePath} className="flex items-baseline gap-2 py-1">
                <NodeLink node={feature.ref}>{feature.title}</NodeLink>
                <Count className="ml-auto">{feature.specsCount}</Count>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </main>
  );
}
```

## Diagrams

Import `Diagram` and `mermaid` from `@rhizome/kit`. Build `source` during render from query data, never in `useState` or `useEffect`, and the diagram stays current automatically:

```tsx
const { data } = useGraphQL<Data>(QUERY);
const source = data ? portfolioSankey(data) : "";   // pure function → Mermaid text
return <Diagram label="Ideas by impact" source={source} />;
```

Use `mermaid.text` for labels in flowchart, gantt, timeline, xychart, and other non-CSV types. Add quotes where the diagram syntax requires them. Use `mermaid.id` to turn a note path into a stable node id, then key `links` with that same id:

```tsx
const source = data
  ? `flowchart LR\n${data.ideas
      .map((idea) => `${mermaid.id(idea.ref.notePath)}["${mermaid.text(idea.title)}"]`)
      .join("\n")}`
  : "";
const links = Object.fromEntries(
  data?.ideas.map((idea) => [mermaid.id(idea.ref.notePath), idea.ref]) ?? [],
);
return <Diagram label="Ideas" source={source} links={links} />;
```

`links` supports flowchart, gantt, classDiagram, stateDiagram/stateDiagram-v2, and agentflow-beta. Click or keyboard Enter opens the note; Cmd, Ctrl, or middle click opens it beside the current note. The kit supplies the Mermaid click directives. An unsupported type or a missing id warns in the console. Author-written links never navigate the view.

Sankey uses CSV quoting rather than Mermaid entities. Use `mermaid.csv` for both text fields and provide a numeric weight:

```tsx
function portfolioSankey(data: Data): string {
  return `sankey-beta\n${data.ideas
    .map((idea) => `${mermaid.csv(idea.title)},${mermaid.csv(idea.impact)},${idea.weight}`)
    .join("\n")}`;
}
```

For xychart legends, name series, for example `bar "Sales" [1, 2]`. Mermaid frontmatter sizing uses `config.sankey` and case-sensitive `config.xyChart`. Sankey nodes and links expose their labels through `data-id` for scoped category styling.

`mermaid.csv` quotes the field and doubles embedded quotes. `mermaid.id` uses FNV-1a 32-bit, base36, with an `n` prefix. Pele loads on the first Diagram mount with nonempty source. A pending update keeps the previous SVG visible; errors show their message and expandable source inside the view.

## Freshness

A view's kit queries stay current without polling. A frame hosted by the workspace receives the workspace's vault, schema, and validation events by `postMessage` and opens no event stream; a standalone page opens its own. Events within 150 ms refresh once, by class:

- Index and node changes refresh data: GraphQL, native rows, display groups, and a view's own `useQuery` keys.
- Schema changes refresh type documentation as well as data.
- Validation changes refresh validation summaries and display-group issue counts.
- A reconnected stream refreshes everything, because missed events are not replayed. A hosted repository view also compares its folder stamp then and reloads if its source changed while the stream was down.

A `views.changed` event reloads a repository view whose own folder changed; an experience frame is rebuilt at the active tab's location, and its other tabs' pages rebuild when shown. Bundled views never reload. A standalone repository page also checks its folder each second as a fallback; a hosted frame does not.

In an experience, GraphQL and view responses carry the index generation, and a retained page shown again, or a resumed frame, refetches only queries whose generation or edit session changed. A hidden page or suspended frame refetches nothing until it is shown.

## `@rhizome/ui`

shadcn/ui components with their usual names, props, and composition, restyled to Rhizome's design: `Button` is a neutral 26px button by default, with `variant="primary"` for the one commit, run, or save action and `destructive` for removal; `Badge` is a neutral chip by default, `destructive` for risk; `Card` is flat with a hairline border; `Tabs` look like Rhizome's segmented control; `Table` has 28px rows and label-style heads; `Skeleton` is a neutral placeholder that appears after 150 ms. Focus uses the app's one outline. The components: `Button` (`variant`, `size`, `asChild`), `Badge`, `Card` `CardHeader` `CardTitle` `CardDescription` `CardAction` `CardContent` `CardFooter`, `Input`, `Label`, `Checkbox`, `Select` `SelectTrigger` `SelectValue` `SelectContent` `SelectItem` `SelectGroup` `SelectLabel` `SelectSeparator`, `Dialog` `DialogTrigger` `DialogContent` `DialogHeader` `DialogTitle` `DialogDescription` `DialogFooter` `DialogClose`, `Tabs` `TabsList` `TabsTrigger` `TabsContent`, `Table` `TableHeader` `TableBody` `TableRow` `TableHead` `TableCell` `TableCaption` `TableFooter`, `Tooltip` `TooltipTrigger` `TooltipContent` `TooltipProvider`, `Separator`, `Skeleton`, and `cn`.

Import them from `@rhizome/ui`, not from `@/components/ui/...`. Icons come from `lucide-react`. A shadcn component that is not listed can be written in the view folder from its usual source; `radix-ui` is not in the import map, so build it on plain elements or import Radix from a URL.
