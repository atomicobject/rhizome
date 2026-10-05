---
type: TechnicalSpec
summary: "Defines the configured view engine, repo-tracked view files, source adapters, mount model, variants, validation, and read/write contracts for reusable ontology workbench views."
id: SPEC-0058
spec-status: active
last-updated: 2026-10-04
aliases:
  - SPEC-0058
  - Configured view engine and repo config
---

# Configured view engine and repo config

## Summary

[[view-preferences|SPEC-0114]] owns durable personal settings, concrete invocation identity, reset and migration, and explicit promotion into shared YAML. Its ignored repository-local SQLite store supersedes browser-only preferences and ephemeral expansion choices in this contract.

Rhizome needs a reusable configured-view engine under the ontology browser so type lists, standalone rail entries, saved query UIs, and future workflow variants are not implemented as separate dashboards. The engine should load tracked view definitions, resolve each view's source note set, apply search/filter/sort/group defaults, and return presentation-ready results that preserve canonical node identity.

This spec defines the implementation-facing contract for that engine. `SPEC-0014` owns the product behavior; `SPEC-0058` owns the repo config format, source/mount/variant abstractions, generated defaults, validation, execution lifecycle, API shape, and safe write boundary for kanban-style interactions.

[[unified-view-contract|SPEC-0110]] extends this engine with a shared registration and invocation contract for Overview, native variants, and custom code. It owns type/group/node selection, applicability, configured-default precedence, and host integration.

## Goals

- define one durable view model that supports type-mounted views and standalone rail views
- keep view definitions in tracked repo configuration rather than browser-local or ignored runtime state
- keep generated schema-informed alternatives available alongside authored views
- support table, card, and kanban variants over the same source result set
- compose source selection, structured filters, text search, sort, grouping, pagination, and visible fields predictably
- reuse ontology query, saved query recipe, readiness, validation, node workspace, and edit-session services instead of creating a parallel data stack
- preserve canonical `NodeRef` identity for every returned row, card, and kanban item
- keep Obsidian `.base` compatibility possible without making `.base` the native config contract

## Non-Goals

- replacing `SPEC-0014` product stories or frontend experience requirements
- replacing saved query recipes; recipes remain one source adapter for views
- adding GraphQL mutations for view execution or markdown edits
- creating a separate dashboard system outside the ontology browser/workbench model
- shipping the full visual view editor in the first engine/config slice
- guaranteeing native support for every Obsidian `.base` option
- storing transient browser state such as unsaved search text, scroll position, column widths, or ad hoc grouping in repo config, except when a reader explicitly saves a view ([[type-collection-views|SPEC-0112]])

## Requirements

### Core Model

- The engine MUST model a native configured view as `id`, `name`, `source`, `mount`, `variants`, `defaults`, and optional metadata. Custom and built-in presentations normalize into the shared contract in [[unified-view-contract|SPEC-0110]].
- The view `id` MUST be stable, URL-safe, unique across loaded repo view definitions, and suitable for side-rail or route lookup.
- `source` MUST define the base node or note set before presentation state is applied.
- `mount` MUST define where the view appears without changing the source query semantics.
- `variants` MUST define supported presentations over the same source set. The initial variant kinds are `table`, `card`, and `kanban`.
- `defaults` MUST define initial search, structured filters, sort, grouping, selected variant, visible fields, and pagination defaults.
- The engine MUST treat generated defaults and authored configs as the same runtime model after normalization.
- The engine MUST preserve canonical `NodeRef` identity in every item returned to browser, CLI, or agent callers.
- The engine MUST support schema-informed fields and metadata without requiring hard-coded knowledge of specific templates.

### Repo Config Storage

- Authored view definitions MUST live in tracked repo configuration, preferably `.rhizome/views/*.yaml`.
- `rzm init` MUST keep the view config directory trackable by updating the managed `.rhizome/.gitignore` template when this storage path ships.
- View files MUST be structured data with deterministic parse and validation behavior. YAML is the native authoring format unless implementation planning identifies a stronger repo-local convention.
- The engine MUST load all valid view files from the configured views directory and report invalid files as validation issues without hiding valid sibling views.
- View file parsing MUST reject duplicate ids, missing required fields, unsupported source kinds, unsupported mount kinds, unsupported variant kinds, invalid field references when detectable, and unsafe kanban write targets.
- View config validation MUST be available through the repo validation surface so broken view files are caught before handoff.
- Generated default views MUST NOT write files until a user explicitly saves or customizes the view.
- Temporary UI state MAY remain browser-local. Meaningful personal choices MUST use the durable preferences service in [[view-preferences|SPEC-0114]] and MUST NOT implicitly modify named repo-backed view definitions.

### Source Adapters

- The first source adapters SHOULD include `ontology_type`, `ontology_interface`, and `query_recipe`.
- `ontology_type` sources MUST resolve through the typed node/type-instance read path and support both file-root and embedded-node instances.
- `ontology_interface` sources MUST resolve all implementing types that have instances and preserve resolved type metadata per item.
- `query_recipe` sources MUST execute through the saved query recipe runner and MUST declare which returned field or node projection supplies the result items, either with view `source.resultPath` or recipe `outputContract.rowPath`.
- Source adapters MUST expose their available fields, filterable fields, sortable fields, grouping fields, and write-capable fields when known.
- Ontology source adapters MUST expose computed relation fields as batched numeric count fields and populate referenced counts before residual filtering, sorting, and pagination.
- Query-recipe sources that extract canonical ontology node rows MAY expose the same schema-derived field capabilities as ontology sources. Recipes choose the row set; ontology schema remains the authority for safe editability.
- Source adapters MUST report pushed/residual constraints, source cap policy, candidate count, source completeness, and reliability so callers can distinguish exact results from residual work over a capped candidate set.
- Field metadata MUST distinguish schema-legal enum values, observed facet values, legal filter options, and legal edit options. `enumValues` is the schema truth; `facets.values` is the current-result observation set.
- Field metadata MUST expose canonical field identity, source aliases, value kind, semantic role, indexed/residual filter operators, indexed/residual sortability, grouping metadata, and edit metadata when known so browser controls do not infer behavior from display names.
- Write-capable field metadata MUST distinguish observed field values from schema-legal edit values so enum editors can offer every valid enum option, including values not present in the current result set.
- Write-capable relation fields MUST declare the target type or interface needed to populate picker candidates through the ontology read model.
- Source adapters MUST return warnings when a source is unavailable, stale, unsupported, unbounded, cannot resolve a declared result path, or returns items that cannot be resolved to canonical nodes.
- Later source adapters MAY include GraphQL, saved search, tag, frontmatter predicate, and derived subset sources.
- Source adapters MUST be additive; adding a new adapter must not require changing the table, card, or kanban variant execution logic.

### Mounts and Navigation

- `type` mounts MUST attach a view to one ontology type or interface and make it discoverable from the matching type rail row.
- The type row's primary action MUST open the selected/default compatible presentation according to [[unified-view-contract|SPEC-0110]], including Overview, native variants, or custom code.
- If multiple views mount to the same type or interface, one MUST be selectable as the default direct action or the engine MUST choose a deterministic default and report the available alternates.
- A native type or interface mount MAY set `replaceGenerated: true` so its layouts become that subject's standard Table, Cards, and Board in place of the generated default's; [[unified-view-contract|SPEC-0110]] owns the resulting choices and default.
- `standalone` mounts MUST appear as standalone side-rail navigation entries with optional group and ordering metadata. A view sourced from one type or interface belongs on that subject's mount; standalone suits dashboards spanning several types or an explicitly requested sidebar entry.
- A view may be unmounted when it should be addressable by id or linked from another surface but hidden from primary navigation.
- Mount resolution MUST be independent from source kind. A standalone rail view may use an ontology type source, and a type-mounted view may use a query recipe source scoped to that type.

### Variants

- `table` MUST remain the native definition's fallback variant when none is explicitly selected. Workspace presentation selection follows [[unified-view-contract|SPEC-0110]].
- Table variants MUST support visible columns, column labels, structured filters, text search, sort, grouping, pagination, and row actions.
- Table grouping MAY include value metadata for labels, order, and default collapsed values. Authored view config overrides schema enum metadata, and explicit personal expansion choices persist per concrete instance and grouping field under [[view-preferences|SPEC-0114]].
- Table variants MUST support inline safe field edits when the source adapter declares a field write-capable. Safe edit kinds include enum selection, checkbox/boolean toggles, single-node relation selection, and authored scalar fields that are not identifiers, built-ins, lists, computed/index-derived values, or policy-confirmation fields.
- Every native executable collection view MUST offer `table`. When it declares another variant and no `variants.table`, the engine synthesizes table columns from the card spec. Custom code and Overview are separate renderer choices and need no native variants.
- Card and kanban variants MUST share one card spec: an `eyebrow` field, a `title` field, a `preview` field, and an ordered `fields` list with compact labels. Absent values resolve at execution from field capabilities: the identifier-role field, `title`, the profile's summary field when it is not a DETAIL field, and the lifecycle field plus leading table columns. For ontology sources, generated columns, sort, and grouping come from the schema-derived type profile in [[type-collection-views|SPEC-0112]], never from field names, and never default to DETAIL fields.
- Card variants MUST group by the view's group state and open items exactly as table rows do.
- Kanban variants MUST define `columnField`, an explicit single-valued schema field, frontmatter field, or source-adapter-declared field. Kanban variants MUST NOT infer columns from prose.
- Kanban columns MUST come from schema-legal values for enum fields, including values with no items, in schema rank order; from `false` then `true` for boolean fields; and from observed values otherwise. Labels, order, and default collapse come from authored group value metadata first, then schema enum `@view` metadata. `hideEmptyColumns` MAY suppress empty columns. An observed value outside the schema's legal values MUST get its own trailing column rather than disabling the board.
- Kanban execution MUST group by `columnField` regardless of requested grouping, and column counts MUST be totals over the filtered set rather than the returned page. A kanban variant MAY set `laneField` to split the board into lanes, with defaults and ordering defined by [[type-collection-views|SPEC-0112]].
- A kanban whose column field cannot be resolved to a single-valued capability MUST return a structured warning and no board layout instead of failing the execution.
- Kanban moves MUST stage one explicit field update through the ontology edit-session model and MUST NOT mutate markdown invisibly. A board over a field without a safe enum, boolean, or single-node edit capability MUST be read-only.
- Kanban moves MUST have a keyboard-operable alternative to pointer drag.
- Grouping by a list-valued field MUST place an item under each value it holds rather than under one joined value.
- Enum value `@view` metadata MAY declare a `tone` (`neutral`, `info`, `progress`, `success`, `warning`, `risk`, `muted`). When absent the engine infers one. Presentations MUST pair tone with a label or shape and never rely on color alone.
- Generated native defaults MUST offer a table variant, a card variant when the type profile has a summary field, and a kanban variant when it has a lifecycle field ([[type-collection-views|SPEC-0112]]). Their default variant follows SPEC-0112's shape rule; workspace default resolution and availability follow [[unified-view-contract|SPEC-0110]].
- The browser MUST remember the last selected variant through the target-scoped selection contract in [[view-preferences|SPEC-0114]].
- Variant configs MUST be independent enough that a view can expose table and card initially, then add kanban later without rewriting the source definition.

### Filtering, Sorting, Grouping, and Search

- Filters MUST use a structured property-filter model rather than ad hoc query-string parsing.
- Filter operands MUST preserve field identity, operator, value, and value type.
- The engine MUST support text search as a composable input alongside structured filters.
- Sorting MUST be stable and deterministic for equal values.
- Grouping MUST support readiness, type/interface, path prefix/folder, tags, enum/status-like fields, frontmatter fields, and source-adapter-declared fields when available.
- Search, filters, sort, grouping, and variant selection MUST compose predictably. Applying one control MUST NOT silently clear the others unless a reset action requests that.
- The execution response MUST echo the normalized query state so browser and agent callers can explain what was run.

### API and Execution

- The browser-facing API SHOULD expose view catalog, view detail, and view execution resources under `/api/v1` once the public API contract is implemented.
- A catalog response MUST include id, name, description, mount metadata, source summary, default variant, the ordered list of executable variants, and validation/degraded state.
- A detail response MUST include the normalized runtime config, generated/default markers, available fields, and warnings.
- An execution request MUST accept selected variant, search, filters, sort, grouping, pagination, and requested detail level.
- An execution response MUST include normalized query state, total count when available, groups when requested, items, the resolved card layout for card and kanban executions, the resolved board layout for kanban executions, facets or available filters when available, warnings, and source/version fingerprints.
- API responses MUST use structured errors and warnings compatible with `SPEC-0057`.
- Agent and CLI surfaces SHOULD expose the same catalog/detail/execute model so configured views can become agent handoff targets.
- Execution MUST be bounded by default and MUST reject unbounded broad sources unless the source adapter declares a safe default.
- View edit affordances MUST stage changes through the ontology edit-session APIs. A browser cell edit may create an edit session automatically, but MUST NOT commit or mutate markdown outside that reviewable session.
- View execution responses MUST expose enough edit metadata for the browser to build field editors without opening each row in the node workspace pane first.
- Browser table cells SHOULD overlay open edit-session values for matching canonical node fields even when a cell is read-only. Overlay keeps views consistent with the shared session and MUST NOT by itself make the cell editable.

### Validation and Freshness

- View validation MUST check syntax, required fields, duplicate ids, source availability, mount validity, variant validity, field references, filter operators, sort/group fields, and kanban write safety where detectable.
- Validation MUST distinguish fatal config errors from degraded runtime warnings.
- Execution fingerprints SHOULD include view config hash, ontology schema hash, source adapter hash, query recipe hash when relevant, readiness profile/version when relevant, and index or validation generation when available.
- Schema changes, query recipe changes, validation changes, index changes, and edit-session changes SHOULD invalidate affected view executions through existing event/capability surfaces.
- Invalid view files MUST not prevent generated default views from being available for unrelated types.

### Native Config Shape

The native config should stay small and extensible. The first implementation should use a shape like this unless planning identifies a concrete conflict:

```yaml
apiVersion: rhizome.view.v1
id: spec-backlog
name: Spec backlog
description: Specs needing planning, review, or follow-through.
source:
  kind: ontologyType
  type: ProductSpec
mount:
  kind: rail
  group: Views
  order: 20
defaults:
  variant: table
  sourceCap: 5000
  search: ""
  filters:
    - field: spec-status
      op: in
      value: [proposed, active]
  sort:
    - field: last-updated
      direction: desc
  group:
    field: spec-status
variants:
  table:
    columns:
      - field: id
      - field: title
      - field: specStatus
        label: Status
  card:
    eyebrow: id
    title: title
    preview: summary
    fields:
      - field: specStatus
        label: Status
  kanban:
    columnField: specStatus
    hideEmptyColumns: false
    card:
      eyebrow: id
      title: title
      fields:
        - field: lastUpdated
          label: Updated
```

The `variants` block above is the delivered contract; the `source`, `mount`, and `defaults` spellings in shipped files use the snake_case kinds the loader accepts (`ontology_type`, `standalone`). `variants.kanban.card` is optional and falls back to `variants.card`, then to resolved defaults. Any change must preserve the source/mount/variant separation.

### Obsidian Base Compatibility

- Native Rhizome view config MUST remain the source of truth for Rhizome-specific behavior.
- Obsidian `.base` compatibility MAY be added as import, export, or source adapter support after the native contract is stable.
- Compatibility MUST NOT weaken Rhizome requirements around `NodeRef` identity, ontology-derived readiness, query recipe sources, repo validation, or edit-session write safety.
- If `.base` import is supported later, unsupported fields MUST produce explicit warnings rather than silent partial behavior.

### Integration Points

- `SPEC-0014` owns product behavior for ontology browser views and workbench UX.
- `SPEC-0057` owns the public API namespace, GraphQL/REST split, error model, and event/capability contracts.
- `SPEC-0052` owns saved query recipe format and execution semantics used by the `queryRecipe` source adapter.
- `SPEC-0053` owns readiness computation and facets used by default grouping, filters, and row/card badges.
- `SPEC-0019` owns canonical node workspace identity and pane-opening behavior.
- `SPEC-0017` and `SPEC-0018` own validation-fix and edit-session safety boundaries that kanban drag and future quick actions must respect.

### Testing

- Unit tests MUST cover config parsing, normalization, validation errors, duplicate ids, generated defaults, and invalid sibling isolation.
- Unit tests MUST cover source adapter registration and source-to-runtime field capability reporting.
- API tests MUST cover catalog/detail/execute responses, structured warnings, pagination bounds, and selected variant behavior.
- Integration tests MUST cover at least one type-mounted generated default, one repo-authored type-mounted view, one standalone rail view, and one query-recipe-backed view.
- Kanban tests MUST prove drag stages a field update through edit sessions and rejects unsafe or unsupported column fields.
- Validation tests MUST prove broken view files are reported through repo validation.

## User Stories

### US1 - Load, validate, and normalize repo-authored and generated view definitions through one runtime model
- id:: ^SPEC-0058-US1
- summary:: Load, validate, and normalize repo-authored and generated view definitions through one runtime model.
- status:: ready

#### Acceptance Criteria

- Repo-authored view files load from tracked config. ^SPEC-0058-US1-AC1
  Valid `.rhizome/views/*.yaml` files produce normalized runtime views, while invalid files produce validation issues without hiding unrelated valid views.
- Generated defaults use the same runtime shape. ^SPEC-0058-US1-AC2
  Types and interfaces without authored view files can receive generated schema-informed defaults that execute through the same engine.
- Duplicate and invalid configs are explainable. ^SPEC-0058-US1-AC3
  Duplicate ids, missing required fields, unsupported source or mount kinds, and invalid variant fields report stable issue codes.

### US2 - Resolve ontology and query-recipe sources into bounded node sets with field capabilities
- id:: ^SPEC-0058-US2
- summary:: Resolve ontology and query-recipe sources into bounded node sets with field capabilities.
- status:: ready

#### Acceptance Criteria

- Source adapters are additive. ^SPEC-0058-US2-AC1
  Adding a new source adapter does not require changing variant execution logic.
- Items resolve to canonical nodes. ^SPEC-0058-US2-AC2
  Every executable source either returns canonical `NodeRef` items or explicit item-level resolution warnings.
- Source capabilities drive UI options. ^SPEC-0058-US2-AC3
  Adapters report available filter, sort, group, visible-field, and write-capable fields when detectable.

### US3 - Execute configured views through a shared bounded API that browser, CLI, and agent callers can use
- id:: ^SPEC-0058-US3
- summary:: Execute configured views through a shared bounded API that browser, CLI, and agent callers can use.
- status:: ready

#### Acceptance Criteria

- Catalog and execution shapes are shared. ^SPEC-0058-US3-AC1
  Browser and agent callers can list views, inspect normalized config, execute a selected variant, and receive the same item identity and warning model.
- Execution echoes normalized state. ^SPEC-0058-US3-AC2
  Responses include the applied search, filters, sort, grouping, pagination, variant, warnings, and source/version fingerprints.
- View items open through node workspace identity. ^SPEC-0058-US3-AC3
  Table rows carry enough canonical identity to open the existing node workspace pane model. Later card and kanban variants must use the same item identity contract.

### US4 - Stage safe table-cell edits from configured views through ontology edit sessions
- id:: ^SPEC-0058-US4
- summary:: Stage safe table-cell edits from configured views through ontology edit sessions.
- status:: ready

#### Acceptance Criteria

- View fields expose edit capabilities. ^SPEC-0058-US4-AC1
  Execution responses identify which displayed fields can be edited, which edit operation they map to, and whether they are enum, boolean, or node-reference values.
- Enum and boolean cells stage set-field edits. ^SPEC-0058-US4-AC2
  Changing an enum value or toggling a checkbox/boolean field stages a `setField` operation against the row's canonical node ref and automatically starts an edit session when none exists.
- Node-reference cells offer typed picker candidates. ^SPEC-0058-US4-AC3
  Editable relation fields expose enough target-type metadata for the browser to offer a filterable picker of valid target nodes and stage a `setLinkField` operation.
- View edits remain reviewable. ^SPEC-0058-US4-AC4
  View-originated edits update the shared edit-session state, dirty indicators, modified-note summaries, preview, discard, and commit flows rather than writing markdown directly.

## Open Questions

- whether the first implementation should ship `queryRecipe` sources with the same slice as `ontologyType` and `ontologyInterface`, or land query recipes immediately after the registry/API foundation
- whether tag and frontmatter predicate sources are required in the first user-visible slice or should wait until schema-informed table/card controls exist
- exact `/api/v1` route names for catalog, detail, and execution
- exact route shape for type-mounted views versus standalone rail views in the browser
- whether native config should allow multiple documents per YAML file or require one view per file for simpler review diffs
