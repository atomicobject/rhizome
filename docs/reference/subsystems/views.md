---
summary: "Configured native views: schema-derived rows, shared query execution, deterministic presentation, and explicit saved configuration."
reference-kind: guide
last-verified: 2026-10-17
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

- Ontology sources use `noderead.Scope.TypeInstances` and batched hydration. Rows retain canonical refs, including embedded nodes and staged edits. Do not introduce a second note scanner or path-only identity model.
- Scalar filters and ordering use the shared schema capabilities, scalar adapter, and pushdown predicate/sort model. SQL, residual evaluation, and overlays agree on typed values, null placement, repeated-value aggregation, and identity tie breaks. DateTime values compare UTC instants with fixed nanosecond precision.
- Link predicates resolve targets through the shared request scope. Staged aliases, new targets, deleted targets, and ambiguous targets use the same target identity rules as committed rows. An unresolved `valueFrom` binding returns no rows for every operator.
- Recipe sources use the shared registry, runner, and application query engine. Only the selected recipe's errors block execution. Duplicate selected IDs fail consistently. Extract rows from the declared result path or output contract; never guess the first array in an object.
- Execution proceeds from source through search, filters, sort, group summaries, and pagination. Apply supported indexed constraints before limits. Diagnostics distinguish pushed and residual constraints without changing the result.
- Catalog reads and interactions do not write YAML. Generated defaults remain executable runtime values. Shared saves are explicit, preserve unrelated authored configuration, and reject stale writes or existing destinations. Personal widths and expansion state stay personal until explicitly promoted.
- Editing capabilities come from the owning schema field. Enum options, cardinality, relation targets, and read-only reasons are projected facts. File mutation goes through the edit-session owner; views do not write note content.
- Fingerprints include the definition, source identity, and normalized execution state. Cache keys include overlays and every input that can change rows or capabilities.

## Must-dos when changing this subsystem

Use fixtures through `Service` for catalog and execution behavior. For predicate or sorting changes, compare committed SQL, residual, and staged results using literal expected rows. Preserve configured and generated choice ordering, replacement/default rules, and canonical relation group keys.

## Review checklist — problems to catch

- Per-row property, relation, or source reads that bypass the shared scope.
- Filters applied after a limit when they could change collection membership.
- Alias text used as link identity, or failed bindings silently broadening a view.
- Recipe capabilities or scalar normalization drifting from GraphQL.
- YAML writes during reads, lost unrelated fields on save, or personal settings leaking into shared configuration.

## Key files

- `pkg/app/views/service.go`, `execute.go`, `source_ontology.go`, `source_query_recipe.go`.
- `pkg/ontology/viewconfig` for the authored format and validation.
- `pkg/app/views/CONTEXT.md` for detailed catalog, default, grouping, and save contracts.

## Related docs

[[graphql-query]], [[noderead]], [[ontology]], [[stores]], and [[web]].
