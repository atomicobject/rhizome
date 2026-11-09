---
summary: "Configured native views: schema-derived rows, shared query execution, deterministic presentation, and explicit saved configuration."
reference-kind: guide
last-verified: 2026-11-09
code-paths:
  - pkg/app/views
  - pkg/ontology/viewconfig
tags: [subsystem/views]
code-anchors:
  go:
    - label: subsystem.views.runtime
      ref: glob:pkg/app/views/**/*.go
    - label: subsystem.views.configuration
      ref: glob:pkg/ontology/viewconfig/**/*.go
---

# Configured views guidance

## Scope

`pkg/ontology/viewconfig` owns view configuration, loading, and static validation. `pkg/app/views.Service` owns catalogs, generated defaults, source adapters, execution, and explicit shared saves for web, CLI, and agent consumers. The browser owns presentation state; personal overrides live in the separate user-state store.

## Design constraints

- Indexed type sources use the configured noderead `Store` capability. Optional `QueryDeps` serve broader query execution and must not gate ordinary indexed type reads. Preserve catalog-only and custom-resolver construction.

- Ontology sources use `noderead.Scope.TypeInstances` and batched hydration. Rows retain canonical refs, including embedded nodes and staged edits. Do not introduce a second note scanner or path-only identity model.
- Scalar filters and ordering use the shared schema capabilities, scalar adapter, and pushdown predicate/sort model. SQL, residual evaluation, and overlays agree on typed values, null placement, repeated-value aggregation, and identity tie breaks. DateTime values compare UTC instants with fixed nanosecond precision.
- Link predicates resolve targets through the shared request scope. Staged aliases, new targets, deleted targets, and ambiguous targets use the same target identity rules as committed rows. An unresolved `valueFrom` binding returns no rows for every operator.
- Recipe sources use the shared registry, runner, and application query engine. Only the selected recipe's errors block execution. Duplicate selected IDs fail consistently. Extract rows from the declared result path or output contract; never guess the first array in an object.
- Execution proceeds from source through search, filters, sort, group summaries, and pagination. Apply supported indexed constraints before limits. Diagnostics distinguish pushed and residual constraints without changing the result.
- Repository definitions load from `.rhizome/views` and every module's `views/` folder through `assetroots.Roots` ([[rhizome-modules|SPEC-0130]]); each definition records its folder and module in `Source.Root` and `Source.Module`. View ids stay unique across all of them (`duplicate_view_id`). Saving an authored view writes only inside the folder that holds it; saving a generated view and ejecting a bundled one write to `.rhizome/views`.
- Catalog reads and interactions do not write YAML. Generated defaults remain executable runtime values. Shared saves are explicit, preserve unrelated authored configuration, and reject stale writes or existing destinations. Personal widths and expansion state stay personal until explicitly promoted.
- Nested frontmatter path fields use catalog values or current preview mappings for display, rank and relation labels. Their canonical field names remain query keys; capabilities omit editing and explain that paths are read-only.
- Editing capabilities come from the owning schema field. Enum options, cardinality, relation targets, and read-only reasons are projected facts. File mutation goes through the edit-session owner; views do not write note content.
- A definition's `params` ([[unified-view-contract#^SPEC-0110-US1|SPEC-0110 US1]]) are validated here (`invalid_view_param`, `duplicate_view_param`) and reach the browser unchanged through the catalog definition and the custom-view invocation. Names exclude the query keys the workspace and `/views` routes read; adding a routing key there means adding it to `reservedParamNames`. Values are read and coerced in the browser, never by the server.
- Fingerprints include the definition, source identity, and normalized execution state. Cache keys include overlays and every input that can change rows or capabilities.
- A configured view filter that would silently match nothing is an invalid-request error naming the field, through `pushdown.FilterFieldError` as in GraphQL ([[experiences|SPEC-0136]] R8): on an ontology source, a field no source type declares, or a computed `@neighbors` or `@reverse` relation views cannot count. Row values (`title`, `path`, `notePath`, `resolvedType`, `updatedAt`, `hasIssues`, `tags`), `frontmatter.` and `inline.` keys, `stale`, and counts of list relations stay filterable (`source_ontology_plan.go`).

## Experiences

An experience ([[experiences|SPEC-0136]]) is a views folder holding `experience.yml`; every definition in that folder or below it belongs to it (`viewconfig/experience.go`). Its identity is the views root plus the folder, so it behaves the same in the flat folder, a team module, a bundled starter's module, or an ejected copy; nothing here may branch on which.

- **Validity is proportionate.** `experience.yml` takes `title` (required), `group`, `types`, `panel`, and `tailwind` (default true). A decode error or a missing `title` invalidates the experience and its views work as ordinary views. An unknown field, type, interface, or group is a warning that drops that name, and a panel problem drops only the panel, because a starter's file meets vaults with different schemas. Validation reports each with its file and field. A nested `experience.yml` declares nothing (`nested_experience`); its views stay in the outer experience.
- **Pages must default-export.** A script entry whose transformed module has a default export (`viewscript.Scan`'s `HasDefaultExport`) renders in the experience frame. An HTML entry or one without a default export keeps its own frame (`experience_view_own_frame`, a warning) and gets no `experienceKey`, but still counts toward what its experience presents.
- **Catalog identity.** `CatalogEntry.ExperienceKey` and `ViewChoice.ExperienceKey` are `Experience.Key()`, a hash of the views root and folder that never changes with folder contents. The browser never derives it.
- **Panel mounts.** `mount: { kind: panel }` is valid only inside an experience, must be named by that experience's `panel`, and must be a script entry that default-exports. A panel view is never a view choice and is not listed among the sidebar's views.
- **Panel resolution.** `viewconfig.PanelResolver` gives every `ViewTarget` one `panel` descriptor, most specific first: (1) the experience owning a standalone view; (2) an experience with a panel presenting the concrete type by mount or `types`; (3) one presenting an interface of it; (4) one presenting it through a group (its `group` or a group its views mount on). A node target past those levels gets Folder when its type is in a group's tree and `{ kind: owner }` when it is not; a type collection, interface, or group target past them gets none. Within a level, views-root order (flat folder, modules by name, bundled views) then folder path wins, and validation warns when two experiences that declare panels tie. The panel depends only on the target, never on which view the switcher shows. Wildcard mounts present nothing in particular.
- **Hidden views resolve.** A hidden view still takes part in resolution and in the invocation route. A hidden standalone experience page keeps a target with no choices, so a tab that opens it reads its experience's panel (`targets.go`).
- **Owner descriptors.** For an embedded or section node, the host uses the descriptor of the Note target it already resolved for the node's `notePath`, using `NodeDescriptor.NoteType` from the node workspace; for a note, it uses Folder.
- **Invocation route.** `GET /api/v1/views/{id}/invocation?context=...` (`pkg/app/web/custom_view_invocation.go`) answers the invocation a host posts to an experience or panel frame, with the view shell's lookup, trust, mount, and context checks, the script entry and check URLs, and the index generation in `Rhizome-Generation`, read before the invocation is built. A `panel` mount passes exactly when resolution gives that target this panel; for `{ kind: owner }` the route follows the context's `ref.notePath` to the owning note's type and checks that note's target.
- **Tailwind.** For an experience with `tailwind: false`, the view shell marks the boot script `data-tailwind="false"`, so the frame loads only the precompiled `kit.css`.

The host side (one frame per experience in a frame layer, pages per tab, suspension, the panel slot, and tab params) is in [web/CONTEXT.md](../../../web/CONTEXT.md). Two host rules that views and their authors depend on:

- **Tab params** ([[ao-kb-knowledge-site|SPEC-0135]] R6a). A workbench tab keeps every query key the Notes route does not own as its view params, at most 32 keys and 2,048 characters (`boundedViewParams` in `web/src/components/notesRoute.ts`), and restores them when the tab is shown again, after a close, from persisted tabs, and on reload.
- **Switcher order** (SPEC-0135 R7). `ViewSelector.tsx` draws custom choices first, ranked by the `custom` flag rather than the renderer, then standard choices, inline while they fit at the measured width, and the rest in More. The catalog's choice order and selection precedence do not change.

## Must-dos when changing this subsystem

Use fixtures through `Service` for catalog and execution behavior. For predicate or sorting changes, compare committed SQL, residual, and staged results using literal expected rows. Preserve configured and generated choice ordering, replacement/default rules, and canonical relation group keys.

## Review checklist — problems to catch

- Per-row property, relation, or source reads that bypass the shared scope.
- Filters applied after a limit when they could change collection membership.
- Alias text used as link identity, or failed bindings silently broadening a view.
- Recipe capabilities or scalar normalization drifting from GraphQL.
- An `experience.yml` problem that invalidates more than it must, a capability that branches on the views root's origin, or a browser that derives an `experienceKey` or panel instead of reading the catalog's.
- YAML writes during reads, lost unrelated fields on save, or personal settings leaking into shared configuration.

## Key files

- `pkg/app/views/service.go`, `execute.go`, `source_ontology.go`, `source_ontology_plan.go`, `source_ontology_rows.go`, `source_ontology_capabilities.go`, `source_query_recipe.go`.
- `pkg/ontology/viewconfig` for the authored format and validation.
- `pkg/app/views/CONTEXT.md` for detailed catalog, default, grouping, and save contracts.

## Related docs

[[graphql-query]], [[noderead]], [[ontology]], [[stores]], and [[web]].
