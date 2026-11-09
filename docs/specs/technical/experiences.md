---
type: TechnicalSpec
id: SPEC-0136
aliases:
  - SPEC-0136
  - Experiences
summary: "Any module, whether a bundled starter, a team's own module, or an ejected starter, can ship an experience: a folder of views with an experience.yml that Rhizome runs as one site, with one long-lived frame per experience in a host frame layer, a sidebar panel at the bottom of the Notes sidebar, a shared record-page shell, reads cached on a composite index generation, cacheable kit assets, and query-layer fixes that make whole-scope and record reads fast for any starter. Rhizome ships one built-in panel, Folder."
spec-status: proposed
last-updated: 2026-11-09
---

# Experiences

## Summary

Starters and teams can already ship custom views ([[custom-view-apps|SPEC-0105]]), mount them on groups, types, and nodes ([[unified-view-contract|SPEC-0110]]), and group a capability's assets in a module ([[rhizome-modules|SPEC-0130]]). What they cannot ship is a fast, coherent set of pages that feels like one site: a group home, record pages, lists, and a sidebar panel. The AO KB Knowledge site ([[ao-kb-knowledge-site|SPEC-0135]]) exposed why, and none of the causes is specific to AO KB:

- Every subject gets a new iframe, every opened record gets a new tab and so a new frame, and returning to a tab reloads its frame from scratch (`CustomViewFrame.tsx:296-303`, `useNoteTabs.ts:721-724`, `ViewHost.tsx:108-126`). Each frame boots React, a query client, and the Tailwind compiler, and fetches kit assets that carry no cache validators (`server.go:447-460`, `vite.kit.config.ts:62-68`).
- Nothing is shared between frames, and no value tells a cache whether a result is still current.
- The query layer pays for work it does not return: relation lists that stop seeking their index above about 150 parents, whole-workspace builds for capability fields, and symlink resolution for every path prefix (R8). On the largest real KB, the AO KB views' whole-KB read takes 1,415 ms; an equivalent read without those costs takes 278 ms.
- A view cannot contribute to the sidebar, and a site's pages share structure only by copying code.

This spec adds the **experience**: a folder of views inside any views root, declared by an `experience.yml`. Rhizome renders an experience's pages in one long-lived frame per window, held in a frame layer that never moves while tabs come and go, and keeps each tab's page alive inside that frame. It shows the experience's panel at the bottom of the Notes sidebar; caches reads on a composite index generation; serves the kit as immutable, precompiled assets; and fixes the query-layer costs above. Every capability works the same for a bundled starter, a team's module or flat views folder, and an ejected starter. Rhizome ships one built-in panel, Folder, for notes no experience presents.

## Terms

- **Views root**: a folder that holds view definitions: the flat `.rhizome/views`, or a module's `views/` under `.rhizome/modules/<name>/` (SPEC-0130). Views-root order is the flat folder first, then modules by folder name.
- **Experience**: a folder under a views root that holds an `experience.yml`, with every view definition in that folder and below it. Its identity is the views root plus the folder path.
- **Experience frame**: the one page frame in a window that renders an experience's pages. An experience with a panel also has one panel frame.
- **Page**: one tab's rendering of one of an experience's views at one location, inside the experience frame.
- **Panel**: content at the bottom of the Notes sidebar for the active subject, from an experience's panel view or from a built-in panel.
- **Index generation**: a composite value that differs whenever the result of some cacheable read could differ (R4).

## Goals

- A team can build an experience as rich as a bundled starter's in its own `.rhizome`, with the same files, kit, and platform behaviour, and can eject a bundled starter and extend it.
- Opening records and moving between an experience's pages and tabs feel instant on the largest real KB.
- A read that many pages need executes once per index generation, not once per page, frame, or reload.
- Whole-scope reads and record reads are fast for any starter's types, not only AO KB's.

## Non-Goals

- A module manifest. `experience.yml` declares an experience, not a module; a module is still its folder (SPEC-0130).
- A host-side data broker, a SharedWorker, persistent browser caches, or sharing derived computations (layouts, counts) between frames. Frames share data only through the server cache.
- Changing the trust model. Experience frames are same-origin trusted repository code under SPEC-0105; separate frames separate styles and runtimes, not security.
- Changing the workbench's tab model. Opening a record still opens or focuses its tab.
- Built-in panels other than Folder; panels spanning several display groups; node-specific panels; per-experience JavaScript bundling.
- New query roots: aggregate (`groupBy`) roots, pushdown of filters on computed relations, index-only relations on section types, and typed date ranges. Each waits for the first experience that needs it; R8 records the measurements that will justify them.

## User Stories

### US1 - Author an experience in any module

- id:: ^SPEC-0136-US1
- summary:: A team adds a folder of views with an experience.yml to its own module or flat views folder, and Rhizome runs it exactly as it runs a bundled starter's experience.
- status:: draft

#### Acceptance Criteria

- An `experience.yml` in a views folder makes that folder's views one experience, with no change to how each view is written beyond exporting its component as the module default.
- The same experience behaves identically from a bundled starter's module, a team module, the flat `.rhizome/views` folder, and an ejected starter's module.
- Validation names every problem in an `experience.yml` with its file and field, and an invalid experience leaves its views working as ordinary views.
- The custom-views skill and a reference guide show how to start an experience, add a page and a panel, use the record-page shell, and eject a starter to extend it.

### US2 - Move around an experience without reloading

- id:: ^SPEC-0136-US2
- summary:: Opening a group page, a record, another record, and a list of the same experience, each in its own tab as today, uses one frame and never reloads the kit.
- status:: draft

#### Acceptance Criteria

- From a group page to a record page to another record and back, in any tabs, the window keeps one frame for the experience, and the frame does not re-download or re-evaluate the kit or the experience's modules.
- Each tab shows its own subject, never another tab's or a previous one, with its own scroll position and component state.
- Back, Forward, reload, tab restore, copied URLs, and opening beside behave as they do for views outside experiences.

### US3 - Return to a tab instantly

- id:: ^SPEC-0136-US3
- summary:: Switching to a recently used tab of an experience shows its page as it was, without re-reading data that has not changed.
- status:: draft

#### Acceptance Criteria

- A retained page reappears with its scroll position and state, issues no reads when neither the index generation nor the edit session has changed, and refreshes once when either has.
- Hidden pages and suspended frames do no work: no events, polling, refetching, or animation.
- A page or frame dropped by the retention caps is rebuilt at the same location and params when its tab is shown again.

### US4 - See a panel for whatever you are in

- id:: ^SPEC-0136-US4
- summary:: The bottom of the Notes sidebar shows the panel that fits the active subject: the experience's panel inside an experience, Folder for other notes, and nothing for search or Problems.
- status:: draft

#### Acceptance Criteria

- Inside an experience that declares a panel, the panel shows for every subject the experience presents (R6) and stays the same frame and the same component as the subject changes.
- For a note that no experience with a panel presents, the Folder panel shows the note's folder, its ancestors, and its siblings, opens a note on activation, and starts minimised in each vault until someone opens it.
- The panel has split, minimised, and cover states, a keyboard-operable divider, and a remembered height, and never changes the outer sidebar's visibility, width, or toggle.

### US5 - Read once per index change

- id:: ^SPEC-0136-US5
- summary:: A read that a page, the panel, other tabs, and a reload all need executes once per index generation.
- status:: draft

#### Acceptance Criteria

- The panel and a page running the same query cause one execution on the server.
- After an edit, a reindex, a schema change, or a view definition change, every visible page and panel shows the new data, and none keeps showing older data.
- A read with an edit session never comes from, or goes into, the shared cache.

### US6 - Build record pages from one shell

- id:: ^SPEC-0136-US6
- summary:: An experience's record pages use a kit shell for title, identity, facts, actions, body, and aside, so pages share structure and states without sharing a look.
- status:: draft

#### Acceptance Criteria

- A record page built on the shell shows its title and identity before any whole-scope read finishes.
- Loading, partial-coverage, and error states look and behave the same on every record page that uses the shell.

### US7 - Eject a starter and extend it

- id:: ^SPEC-0136-US7
- summary:: A maintainer ejects the AO KB starter, adds a page and changes its panel in the local copy, and both work, with every experience capability intact.
- status:: draft

#### Acceptance Criteria

- After ejection (`management.ejected` in `.rhizome/workflows.yml`, [[init-starter-workflow]]), `rzm init` neither refreshes nor removes the module's files, and its experience, panel, setup scripts, and bindings keep working.
- A page added to the ejected experience folder joins the experience, shares its frame, and can be reached from the panel.

### US8 - Know it stays fast

- id:: ^SPEC-0136-US8
- summary:: Maintainers can see where an experience's time goes, and CI catches regressions in frame reuse, retention, caching, and query work.
- status:: draft

#### Acceptance Criteria

- Performance diagnostics report each experience's frame phases and counts and the server read cache's hits and misses, without titles, paths, or query text.
- CI fails when a record opened in a new tab of the same experience creates a frame, when switching between retained tabs at an unchanged generation issues a read, when the panel and a page run one query twice, or when R8's query work counters regress.

## Requirements

### R1. Declaring an experience

- An experience MUST be declared by `experience.yml` in a folder under any views root. Every view definition in that folder or below it belongs to the experience. An `experience.yml` below another experience's folder MUST be a validation error, and the inner folder's views MUST stay in the outer experience.
- Fields, all optional except `title`:
  - `title`: the experience's name.
  - `group`: a display group the experience presents (R6).
  - `types`: type or interface names the experience presents beyond those its views mount on (R6).
  - `panel`: the ID of a view definition in the folder whose mount is `{ kind: panel }` (R6).
  - `tailwind`: whether the experience's frames load the runtime Tailwind compiler (R5). Default true.
- A view in an experience MUST export its component as the module default; the experience frame renders it (R2). `viewscript.Scan` MUST report `HasDefaultExport` from the transformed module so validation can check it. An entry without one, and every HTML entry, MUST be reported by validation and MUST keep today's one-frame-per-subject behaviour.
- `mount: { kind: panel }` is valid only inside an experience, MUST be named by that experience's `panel`, is never a view choice, and is not listed among the sidebar's views.
- Validation MUST report, with file and field: unknown fields; a missing `title`; a `panel` that names no panel view in the folder or a view whose entry is not TSX; unknown types, interfaces, or groups; a nested `experience.yml`; and a non-default-exporting TSX entry. An invalid `experience.yml` MUST leave its views working as ordinary views.
- The catalog MUST carry, on every view choice that belongs to an experience, an opaque `experienceKey` the server computes from the views root and folder, and on every `ViewTarget` a `panel` descriptor (R6). The browser MUST NOT derive either. `experienceKey` MUST NOT change when files in the folder change; the views generation (R4) covers content.
- No capability in this spec MAY depend on whether the views root is a bundled starter's module, a team's module, an ejected module, or the flat folder.

### R2. One frame per experience, in a frame layer

- The host MUST render experience frames in one frame layer over the workbench's content area. A frame is created in the layer and never moved in the DOM, because moving an iframe reloads it. Tab panels hold placeholders; the layer positions the active experience frame over the active tab's placeholder and keeps it aligned through resizes, panel toggles, and scrolling of the host. The layer MUST sit in the document order of the content area for focus and reading order, and host overlays (menus, dialogs, popovers, tooltips) MUST stack above it.
- The window MUST hold at most one page frame per experience (its panel frame is separate, R6). Showing a tab whose presentation belongs to experience E MUST use E's frame if it exists, and post a validated `rhizome:location` message carrying a navigation ID, the tab ID, the view ID, the invocation (context and configuration), and params. Views outside experiences, and entries R1 excludes, keep today's frames.
- The kit MUST reply `rhizome:location-accepted` with the navigation ID on receipt; if that reply does not arrive within 1,000 ms, the host MUST replace the frame with a fresh one at the location. `rhizome:location-rendered` reports paint for telemetry only.
- Inside the frame, the kit MUST keep one page per tab, keyed by tab ID, and render only the active tab's page, keeping up to P recently shown pages mounted but hidden (React `Activity`) and dropping the least recently shown beyond P. P is a kit constant starting at 8.
- Each page MUST have its own binding: its invocation (context and configuration), params, and navigation ID. Every hook (`useViewContext`, `useViewParams`, `useViewPreference`, `useViewLocation`, and configuration) MUST read the binding of the page it renders in, through React context, so a hidden page that re-renders never sees another page's location. Imperative getters (`getViewInvocation`, `getViewParams`) return the active page's binding. Asynchronous work (reads, dynamic imports, timers, edit messages) MUST capture its page's binding and navigation ID, and its result MUST be ignored if that navigation ID is no longer current for the page.
- A location for a tab whose page shows the same view, context, and identity params MUST activate or update that page in place (UI params only), keeping its state and its navigation ID. A new navigation ID is issued only when a page's view, context, or identity params change, including the panel's in-place subject change (R6). A location with a different view, context, or identity params MUST replace the page: a new binding, a new component keyed by view ID, context, and identity params, and a reset error boundary.
- The kit MUST save each page's scroll offset when it is hidden and restore it when it is shown.
- The host MUST build invocations with the same mount and context checks as the view shell (`custom_view_context.go`), served by `GET /api/v1/views/{id}/invocation?context=...` and cached in the host per view and context, keyed by the index generation (R4).
- Navigation, params, and history stay host-owned: the URL, Back and Forward, reload, tab restore, and opening beside behave as for any view. Every message from a page MUST carry its tab ID (`panel` for a panel page) and navigation ID. The host MUST reject a message whose navigation ID is not current for its tab; a rejected `rhizome:stage` MUST receive an error reply that the kit surfaces, so an edit is never silently lost.
- Closing a tab MUST drop its page. Closing the last tab of an experience MUST suspend the frame (R3), not destroy it.

### R3. Hidden pages and suspended frames

- A hidden page MUST do no work. The kit MUST pause its queries' refetching and polling, including action polling (`web/kit/actions.ts:86-98`), and expose `useViewActive()` so custom timers and effects can stop; views MUST use it for their own timers.
- An experience frame with no visible page, and an inactive panel frame, MUST be suspended: hidden, `inert`, sent `rhizome:suspend`, and forwarded no events.
- On resume, and when a hidden page is shown again, the host MUST send the current index generation and edit-session revision, and the kit MUST refetch only the queries whose generation or session revision differs. Reads that complete while a page is hidden MUST be applied only if its navigation ID is still current.
- The host MUST retain at most K suspended frames across experiences and panels (a panel frame counts only while suspended), evicting the least recently shown, and MUST destroy frames on vault switch, reload, trust revocation, or a change to their experience's definition. K is a host constant starting at 4. The retention constants P and K MUST be set from measured memory in Rhizome Desktop's webview, within a retained-heap budget of 150 MB, before release.
- A tab whose page or frame was dropped MUST be rebuilt at the tab's location and params when shown. Views that need state to survive that rebuild keep it with `useViewPreference` or, for state scoped to the window, `sessionStorage`; the host keeps nothing else. Staged edits are host-owned and survive (`CustomViewFrame.tsx:251-264`).

### R4. Index generation and the read cache

- The server MUST expose an **index generation**: an opaque string `epoch.writes.schema.views.validation.config`, where `epoch` identifies the server process; `writes` is a counter advanced after every commit through the store's write path (`withWrite` and `withWriteTx`, `pkg/store`); `schema` is the schema hash; `views` is the configured-views generation; `validation` is the validation generation; and `config` is a hash of `.rhizome/config.yml` and `.rhizome/ignore`, recomputed on the watcher's `StaleIgnoreChanged` signal (`watchhub`) and on the bootstrap's `reloadVaultDef` effect for `.rhizome/config.yml` (`bootstrap/watcher_sync.go`). The embedding stores write through their own runtime (`pkg/embeddings/sqlite/runtime.go`) and do not advance `writes`, so reads that depend on them are never cached (below). Generations are compared for equality only; nothing orders two generations.
- Every GraphQL response MUST carry the generation read before execution in `extensions.generation`; view-execution, catalog, and invocation responses carry it in the `Rhizome-Generation` header; every vault event that can change reads carries it as `generation`; and the status API reports it in place of today's RFC3339 `generation` (`public_api.go:352`, `:642-646`), a contract change recorded in `openapi.yaml` and the changelog.
- The public GraphQL handler and view execution MUST cache responses in one byte-bounded LRU per vault (64 MiB default) keyed on the generation and a hash of the normalised request: for GraphQL, query text, variables, and operation name; for view execution, the whole execution request (definition reference, source, filters, sort, grouping, pagination). Identical concurrent requests MUST execute once, on a context detached from any one caller, so a cancelled caller never fails the others and the others execute on their own if it errors.
- A response MUST NOT be stored if the generation changed during execution; if it carries a transport or execution error (`errIndexInitializing` among them); if the request carries any edit session, by ID or by operations; if it uses `semantic` arguments or the `search` root; or if it selects a field on the fixed list of caller-dependent fields in code (today `currentUser`). A test MUST enumerate every query root and fail when one is neither covered by the generation nor on the bypass list. Responses whose errors are all field errors with a `path` MAY be stored. Readiness and trust admission MUST run before a cache hit is returned.
- The kit MUST keep a current generation per frame, set by every event, resume message, and applied response. Each request records the current generation when it is sent, and for each query only the response to its latest-issued fetch is applied. A response is stale when the frame's current generation changed between send and arrival and differs from the response's generation: it is dropped and its query refetched once. Otherwise it is applied, and its generation becomes current. Whenever the current generation changes, by event, resume message, or applied response, every visible query fetched at a different generation refetches once; a hidden page does so when shown (R3). After the event stream reconnects, the host MUST post the current generation from the status API to every frame.
- A diagnostics setting MUST re-execute a sample of cache hits and report any mismatch.
- Tests MUST cover: the write counter advancing for an index publish, a watcher write, a derived-lane completion, and a validation run; each non-store component changing for a schema reload, a view definition change, a configuration change, and an ignore change; publish during a read; a late response after a newer event; reconnect; a server restart; an edit session by ID only; a `currentUser` read; a semantic read and a `search` read after an embedding-only update; both orders of a response and an event; a G1 response applied before the G1 event, with a sibling query fetched at G0 refetched once; two concurrent responses at G1 applied with no refetch; and a write that changes nothing.

### R5. Assets and startup

- The kit MUST be served under `/kit/v1/<build>/` with `Cache-Control: public, max-age=31536000, immutable`; the view shell, the import map, and the boot script MUST reference that path, and `/kit/v1/` without a build MUST keep working for existing pages.
- The theme, tokens, the kit's UI components, and the utilities used by the kit and by Rhizome's bundled starter views MUST ship as a precompiled stylesheet built with Tailwind's `@source` over those files. The runtime Tailwind compiler MUST move out of `boot.js` into its own file, loaded for views outside experiences (unchanged behaviour) and for experiences with `tailwind: true`. Bundled starters' `experience.yml` MUST set `tailwind: false`; a team or ejected experience that adds utilities the precompiled sheet lacks keeps the default.
- View modules keep content-hash revalidation, once per experience frame. Per-experience bundling waits for R10's telemetry to show module loading above 50 ms at p95 on a real experience.

### R6. Sidebar panel

- An experience E **presents** a subject when:
  - the subject is one of E's standalone views; or
  - the subject's concrete type, or one of its interfaces, is a mount target of E's views or is listed in E's `types`; or
  - the subject is a group view of a group G, a type or interface collection in `viewconfig.NavigationTree.Members(G)`, or a node whose concrete type is in that tree, where G is E's `group` or a group one of E's views mounts on; or
  - the subject is an embedded or section node whose type is in no group's tree, and the type of its owning note (at its reference's `notePath`) qualifies under the rules above.
- The server MUST resolve one panel for every `ViewTarget`, most specific first: (1) the experience that owns a standalone view; (2) an experience with a panel presenting the subject's concrete type by mount or `types`; (3) one presenting an interface of that type; (4) one presenting the subject through a group; (5) the built-in Folder panel, for any subject backed by a note; (6) none, for search, Problems, Explorer, and subjects without a note. A type collection's concrete type is the collection's type; an interface collection resolves at level (3). An experience at level (1) without a panel gives no panel. Within a level, views-root order, then folder path, wins, and validation warns when two experiences that both declare a panel tie. The panel depends only on the target, never on which view the switcher shows.
- `ViewTarget`s are per kind and name. A `node` target whose type is in no group's tree and that resolves at none of levels (1) to (3) carries the descriptor `{ kind: owner }`: for an embedded or section node, the host uses the descriptor of the Note target it already resolved for the node's `notePath`; for a note, the host uses Folder.
- The Notes sidebar MUST hold one panel slot at its bottom, below the type tree. A panel view runs in its own frame in the slot, created on first use. Its invocation's context is the active target's context; the invocation route's mount check MUST pass for a `panel` mount exactly when R6 resolves that target to this panel. The panel frame holds one page with tab ID `panel`, keyed by view ID only, and a new subject updates that page's binding in place, so its component stays mounted and its own state (find text, lists, scroll) survives navigation. `useViewPreference` in a panel page MUST be scoped to the vault and the panel's experience, not to the subject, so what a panel stores survives subject changes and eviction.
- **Folder** is native host UI, not a frame: the active note's folder with its ancestors and siblings, built on `web/src/components/folderTree.ts`, `useFolderExplorer.ts`, and `RailTree`. Activating an entry opens the note; the folder name opens Explorer at that folder. It starts minimised in each vault until someone opens it, and remembers that (Drew, 2026-11-09).
- Every panel, built-in or custom, MUST share these rules:
  - States: split, with a divider; minimised to its title bar; and cover, filling the sidebar's contents. Cover hides the normal navigation and makes it inert; minimised hides the panel body and makes it inert; the title bar and its controls stay reachable. Before hiding a region that holds focus, the host moves focus to the panel's surviving control. Escape returns from cover to split after a child menu has had the chance to consume it. Cover is not modal.
  - Divider: a focusable separator with an accessible name, value attributes, Up and Down in 16 px steps, Home and End; `PanelResizeHandle.tsx` is generalised with an axis so one implementation serves width and height.
  - Persistence: split height and minimised state through the view preference service, keyed by vault and the panel's experience identity (or `builtin:folder`), never by content.
  - Shortcuts and motion: the workbench sidebar shortcut keeps working while focus is inside a panel frame, with the same text-field exclusion; motion respects reduced-motion settings.
  - Colour: panel text uses `--chrome-text` and `--chrome-muted`; a new token `--chrome-link` (`#75b9b5` in both appearances, at least 4.25:1 on every chrome background in `tokens.css`, checked by `themes.test.ts`) is for lines and highlighted marks only, never text.
  - The panel never changes the outer sidebar's visibility, width, or toggle (SPEC-0128).

### R7. Kit additions

- `useViewLocation()`: the current view ID, tab ID, context, params, and configuration, reactive under R2.
- `useViewActive()`: false while the page is hidden or the frame suspended (R3).
- `markReady()`: a page calls it when its required reads resolve; telemetry uses it as data-ready.
- `readNodes(refs, selection)`: batched node reads that chunk refs at the `nodes(refs:)` maximum with `first` set, merge by canonical identity, and return combined coverage and errors, so a scope larger than one batch is read completely.
- `RecordPage`: header slots (kind chip, title, one facts line, actions), body, optional aside, and loading, partial-coverage, and error states, built from `Section`, `SectionBody`, `Facts`, `StatusMark`, and `RecordChip`. It sets structure and spacing only, matches the host's `ConfiguredViewHeader` in structure and tokens without sharing code, and fetches nothing.
- All additions are additive; existing views keep working.

### R8. Query layer

Measured on 2026-11-09 through `/api/v1/graphql` with this checkout's binary, warm medians of five runs, on disposable copies of a current-schema AO KB (416 records), the largest legacy AO KB (1,195 records), and this repository's Agentic Engineering docs (2,532 records): the AO KB views' whole-KB read takes 517 ms and 1,415 ms (2,090 ms uncapped); an orientation read of the same records with scalars and relation paths takes 100 ms and 278 ms; a record page's scoped read takes 23 ms and 44 ms. Parsing, JSON encoding, and HTTP cost under 10 ms. Each fix below serves any starter's types:

- **Relation lists MUST cost the same per parent at any parent count.** `pkg/store/ontology_neighborhood.go:45-77` joins edges to a `VALUES` CTE through `COALESCE(e.src_node_id,'')`, which SQLite stops planning through the edge index above about 150 parents: 6 ms at 100 parents, 453 ms at 878. The statement MUST seek the edge index (a materialised CTE or an `IN` list, without the `COALESCE`); typed neighbours MUST filter `dst_type` in SQL instead of in Go (`noderead/neighborhood.go:44-90`, 9,787 rows read for 417 kept); and the other `VALUES` joins (`intel_query.go`, `ownership_transition_lookup.go`) MUST be checked the same way.
- **Selections MUST NOT do work they do not return.** A `workspace` selection asking only for capabilities builds, projects, and hashes every record's whole workspace (`query/workspace.go:16-27`, `node_workspace.go:149`, `:334-346`): 130 to 410 ms and about 300 MB allocated per request. Capability fields MUST be computed by a selection-aware path that returns identical results (provider mutation support, projected fields and collections, child presence, and source ownership), projecting only what those fields need, and the version hash MUST be computed only when `version` is selected. A read selecting no body, content, passage, or capability field MUST NOT load note bodies.
- **Path canonicalisation MUST be memoised for reads.** `paths/physical.go:37-60` resolves symlinks for every prefix of every record's path; it is about half the handler CPU on scalar reads. Query reads MUST share one prefix cache for the duration of one request. Write admission (`sourceownership.Policy.Check`, note creation in `operations/create.go`) MUST keep resolving fresh, because a symlink can be retargeted without any indexed or configuration change.
- **No silent results.** A filter on an unknown field, or on a computed `@neighbors` or `@reverse` relation, matches nothing and reports `complete` (`pushdown/planner.go:57-59`). Both MUST be errors that name the field. The capped reads, `nodes(refs:)` (default 50, maximum 200), `search`, and `neighborhood`, MUST report truncation as `extensions.truncated: [{path, returned, limit}]` beside `extensions.typedRoots`, and through the kit's coverage helper. Declared relation fields without `first` return every row, as today, and MUST stay uncapped.
- **CI asserts work, not time.** Go tests MUST assert: the edge index is used in `EXPLAIN QUERY PLAN` at 5,000 parents; rows read for typed neighbours stay within a constant multiple of rows returned; symlink resolution runs at most once per distinct directory per request, and a write after retargeting an allowed alias into an owned directory is still rejected; a body-free selection opens no note body (a counter on the note reader); a capability-only `workspace` selection builds no workspace, and its capability results equal today's on Markdown roots, provider-backed roots, embedded nodes, empty collections, and owned files; and `testing.AllocsPerRun` per record stays within a recorded bound.
- Budgets, checked in dogfood (R10): an orientation-class read of 1,000 records at most 150 ms warm; a record page's scoped read at most 50 ms.
- For the deferred query roots (Non-Goals): grouping by kind, status, or folder costs 167 to 278 ms on the legacy KB today because it fetches every record, and stories to efforts costs 658 ms against 13 ms for paths because section relations project Markdown. One observation needs diagnosis before release: for several minutes after a first index, reads ran 10 to 20 times slower while waiting inside `sqlite3_step`, consistent with readers blocked by post-index background writes. The build MUST reproduce it and fix it, or bring the measurement back to Drew.

### R9. Ejected starters

- An ejected starter's experiences, panels, scripts, and bindings MUST keep working from the team's copy, and every capability in this spec MUST behave as it does for the bundled module. Ejection already freezes module files (`template_plan.go:289,356`); this requirement guards that.
- A test MUST eject the AO KB starter in a fixture, add a page and change the panel in the local copy, run `rzm init`, and show that the files are untouched and the new page and panel work, including a new Tailwind utility after the test sets `tailwind: true` in the ejected `experience.yml`, which `rzm init` then preserves.

### R10. Measurement and budgets

- The kit MUST post phase marks (shell start, module loaded, first render, data ready from `markReady`, location painted) to the host. The host MUST aggregate them per experience per window into SPEC-0116's `web.client` batches as a fixed "Experience frames" measurement, identified by a hash of the experience identity (never its path), with p50 and p95 per phase and counts of frames created, locations reused, pages retained, resumes, and evictions. Fixed diagnostic regions MUST wrap the frame layer and the panel slot. `rzm diagnostics perf` MUST report server cache hits, misses, bytes, and execution time per query hash. Nothing recorded includes titles, paths, or query text, and recording stays behind SPEC-0116's profiling opt-in.
- CI MUST assert structure, not time, in Go, web unit, and Playwright tests: a record opened in a new tab of the same experience creates no frame; switching between retained tabs at an unchanged generation and session issues no read; the panel and a page running one query cause one execution; an edit refetches once per visible page and never in a hidden page or suspended frame; suspended frames receive no events; a delayed navigation A followed by B renders B in every API; a stale `stage` gets an error reply; Back and Forward, reload, and tab restore land on the right page; R8's work counters.
- Latency budgets, measured before release in Rhizome Dev on a named machine and build, at least twenty samples per path, cache states stated, profiling on, using Chromium long tasks or WebKit frame stalls as SPEC-0116 does:

| Path | Budget (p95) |
| --- | --- |
| Open a record of the same experience in a new tab, read cache warm | 100 ms to data paint, no main-thread task over 50 ms |
| Switch to a retained tab, same generation | 50 ms to content, no reads, no new frame |
| First open of an experience, kit cached, read cache warm | 400 ms to data paint |
| First open of an experience, read cache cold | the page's read time plus 300 ms |
| Panel, warm | 100 ms to content, delaying the page's data paint by at most 50 ms |

## Amendments

- **SPEC-0110.** "Inactive custom frames MUST unmount and release subscriptions and polling" becomes "Inactive custom frames MUST unmount and release subscriptions and polling, except experience frames and pages, which are retained and suspended under SPEC-0136 R2 and R3." The identity-param rules ("an identity param is part of the invocation, so the frame remounts"; Back and Forward remount the frame when identity params changed) gain: "For an experience view, the page remounts inside the retained frame (SPEC-0136 R2)." The catalog gains `experienceKey`, the `panel` mount kind, and the target's `panel` descriptor. Invocation and query-cache identity still include the subject, now through the kit's per-location invocation.
- **SPEC-0105.** The kit moves to `/kit/v1/<build>/` with immutable caching, ships a precompiled stylesheet, and loads the runtime Tailwind compiler from its own file (R5). It gains R7's additions and the location, accepted, rendered, suspend, and resume messages. On `views.changed`, an experience frame rebuilds at its tabs' locations instead of reloading one page.
- **SPEC-0128.** The non-goal "Changing what the Notes sidebar holds" gains an exception: the Notes sidebar holds one resolved panel at its bottom (R6).
- **SPEC-0130.** A views folder may hold an `experience.yml`; a module still has no manifest.
- **SPEC-0116.** Web diagnostics gain the "Experience frames" measurement beside frame stalls (`persistent-diagnostics.md:82-85`) and the frame-layer and panel regions; server diagnostics gain read-cache statistics (R10).

## Open Questions

None. Drew decided on 2026-11-09 that Folder starts minimised. P and K are set by measurement under R3's memory budget.

## Documentation plan

- The custom-views skill template (`pkg/app/cli/init/templates/skills/markdown/custom-views/SKILL.md` and its `references/`): experiences, `experience.yml`, panel views, default exports, `RecordPage`, `useViewLocation`, `useViewActive`, `readNodes`, Tailwind in experiences, that module-level state and injected styles persist across pages in a frame, and ejecting a starter to extend it, including setting `tailwind: true` before adding utilities.
- A reference guide, "Build an experience", under `docs/reference/guides/`, with a minimal team-module example.
- Subsystem notes, with `last-verified` bumped: `docs/reference/subsystems/views.md` (experiences, the panel mount, resolution, the frame layer, retention), the GraphQL query note (generation, read cache, R8's rules), and the stores note (the write counter).
