---
type: TechnicalSpec
summary: "Defines the read-only GraphQL query contract generated from Rhizome ontology SDL: bounded roots, generated type surfaces, GraphQL variables, NodeRef locators, section resolution, relation semantics, batching, errors, limits, semantic fallback, and regression fixtures."
id: SPEC-0042
spec-status: active
last-updated: 2026-07-16
aliases:
  - SPEC-0042
  - ontology-graphql-query-contract
---

# Ontology GraphQL query contract

## Summary

Rhizome exposes a read-only GraphQL-shaped query layer over the compiled ontology schema. The query layer is not a general graph database, not a mutation API, and not a second indexing runtime. It turns the live SDL into a bounded executable schema, resolves roots through indexed note/type metadata plus optional semantic search, and hydrates selected note or section fields through the `noderead` request scope.

This spec freezes the current contract for `pkg/ontology/query`: root selectors must be bounded, generated fields must mirror the compiled ontology, GraphQL variables must be accepted for argument values, locator payloads must preserve `NodeRef` identity, structural relations must stay distinct from ambient relations, and broad list relation reads must batch through the shared read layer.

Related governing specs:

- [[structural-node-model-and-ontology-read-path]]
- [[noderef-batch-traversal-read-api]]
- [[ontology-browser-workspace]]
- [[linkable-embedded-node-identifiers]]

## Implementation Surface

- `pkg/ontology/query/schema.go` generates the executable SDL from the compiled ontology schema.
- `pkg/ontology/query/prepare.go` validates the supported query subset and root bounds before execution.
- `pkg/ontology/query/execute.go` resolves roots, note fields, section fields, locators, structural relations, ambient relations, semantic roots, and partial-data errors.
- `pkg/ontology/query/runtime_execute.go` exposes explicit runtime diagnostics such as `ontology.queryPlan(...)` for indexed ontology-node root planning.
- `pkg/ontology/query/loaders.go` creates one `noderead.Scope` per execution loader set and batches hydration, type rows, assessments, and relation traversal behind that scope.
- `pkg/ontology/query/query_test.go` is the normative regression fixture for this contract; add coverage there before changing query semantics.

## Goals

- Define the public GraphQL query shape generated from the compiled ontology schema.
- Preserve predictable read behavior for agents and browser code that query typed notes.
- Keep query execution read-only, bounded, deterministic, and safe for broad list selections.
- Support first-class GraphQL variables so reusable recipes and agent calls do not rewrite query text to inject input.
- Make `NodeRef` locator fields available for notes, structural sections, and embedded nodes.
- Document the edge cases tests must preserve when changing schema generation, preparation, loaders, or execution.

## Non-Goals

- Adding mutations, subscriptions, named fragments, query directives, or arbitrary graph traversal.
- Replacing `noderead.Scope`, ontology indexing, semantic search ranking, or node workspace projection.
- Making interfaces root query fields.
- Treating every markdown heading as a graph-worthy node.
- Guaranteeing semantic search availability in vaults that do not configure a semantic searcher; the `search` root degrades to lexical lanes there.

## Requirements

### Root Selection Bounds

- `note(path: String!): NoteNode` MUST require a path and return either one resolved typed note object or `null`.
- `notes(type:, find:, property:, semantic:, first:)` MUST require at least one of `type`, `find`, `property`, or `semantic`.
- Generated typed roots, for example `technicalSpec(path:, find:, property:, semantic:, first:)`, MUST require exactly one of `path`, `find`, `property`, or `semantic`.
- Embedded-node typed roots with a schema-level `@source(...)` MAY accept `filters` and `sort` arguments. Supported predicates and sort keys MUST push into the indexed ontology field-value read model before root pagination.
- `FieldFilterInput.op` and `SortInput.direction` MUST use explicit GraphQL enums (`FieldFilterOperator` and `SortDirection`) so clients can introspect the supported operator vocabulary; recipe text should use enum literals such as `op: eq` and `direction: asc`.
- `notes(...)` MUST accept at most one retrieval mode among `find`, `property`, and `semantic`; `type` is a filter, not a retrieval mode.
- Typed roots MUST accept at most one retrieval mode among `path`, `find`, `property`, and `semantic`.
- Prepare-time validation MUST reject unbounded roots before execution so callers cannot accidentally enumerate broad typed collections without a bound.
- Inline fragments on `Query` MUST preserve the same root validation rules as top-level selections.
- Unsupported root fields MUST return query errors, not best-effort path or type guessing.

### GraphQL Variables

- Query operations MUST support GraphQL variable definitions for argument values.
- Variable values MUST be supplied as a JSON object by CLI, agent CLI, in-app agent tools, MCP tools, saved-query-recipe, or future API callers.
- Variable definitions MUST preserve GraphQL type semantics for nullable, non-null, scalar, enum, list, and input-object values supported by the generated schema.
- Operation default variable values MUST be applied when a caller omits the variable.
- Missing required variables, invalid JSON values, and values that cannot be coerced to the declared GraphQL type MUST fail before execution with structured query errors.
- Variables MAY be used anywhere the generated query schema accepts argument values, including root selectors, relation fields, `children(first:)`, and built-in ambient fields.
- Variables MUST NOT change field names, type conditions, directive behavior, fragment behavior, operation kind, or root-bounding rules.
- Prepare-time root validation MUST evaluate literal arguments plus supplied variable/default values so a variable-backed root is still proven bounded before execution.
- Execution MUST pass the same validated variable value map to every `ArgumentMap(...)` call so root, relation, section, and child field arguments resolve consistently.
- Saved query recipes SHOULD bind declared inputs to GraphQL variables rather than performing placeholder substitution in query text.

### Generated Schema Surface

- `BuildExecutableSchema` MUST derive SDL from the compiled `ontology.Schema`; handwritten SDL fragments must not drift from the compiled schema.
- The generated SDL MUST include the built-in scalars `Date`, `DateTime`, `URL`, and `JSON`.
- The generated SDL MUST include `PropertySource`, `NeighborDirection`, `SectionLevel`, `OntologySemanticsKind`, `NodeLinkTarget`, and `NodeLocator`.
- `NoteNode` MUST be implemented by every note-role ontology type and expose `path`, `title`, `content`, `frontmatter`, `tags`, `locator`, `linked`, `backlinked`, `connected`, and `score`.
- `NoteNode.score: Float` MUST be nullable; it is populated only when the note was surfaced through a `semantic:` argument or the `search` root and MUST be null otherwise so callers cannot mistake a default value for a relevance signal.
- `search(query: [String!]!, type: String, first: Int = 20): NodeSearchResult!` MUST run the shared unified search engine restricted to file-backed notes, with `type` narrowing to that type's concrete implementors before retrieval. It has no mode argument. When no note searcher is configured it MUST return a `search_unavailable` warning with no nodes; engine warnings MUST be surfaced as `NodeSearchResult.warnings`.
- `NoteNode` MUST remain the universal runtime interface for all file-backed note objects. Agents and saved recipes that need "any note" semantics MUST use `NoteNode`, not `Note`.
- Authored ontology SDL MAY define or extend `interface Note` for shared general note fields, but MUST NOT define concrete `type Note`.
- Fields declared directly on authored `interface Note` MUST be nullable at the GraphQL boundary. Non-null `!` fields on `interface Note` are invalid because general note fields cannot be required across a broad vault migration.
- When authored `interface Note` exists, generated `NoteNode` MUST implement `Note`, and every file-backed note object that implements `NoteNode` MUST expose the authored nullable `Note` fields. This includes typed notes, concrete default-note reads, and otherwise-untyped fallback note reads.
- Authored nullable `Note` fields MUST behave as optional general note metadata: absent field sources resolve as null and MUST NOT produce missing-required validation issues.
- The compiler MAY inject authored nullable `Note` fields into generated file-backed note object SDL so schema authors do not repeat shared fields on every concrete type.
- `interface Note` MUST NOT carry note-selection semantics: no `@node`, no `paths`, no `matches`, and no default selector behavior. Fallback/default note behavior belongs to a separate concrete note type in the ontology compiler contract.
- When no authored `interface Note` exists, the runtime MAY provide a compatibility `Note` interface, but callers that need stable universal semantics MUST still use `NoteNode`.
- `Section` MUST be implemented by heading-derived section and embedded-node types and expose `id`, `notePath`, `title`, `level`, `content`, `locator`, and `children`.
- Concrete note types MUST get singular lower-camel query roots that return typed lists, for example `TechnicalSpec` -> `technicalSpec(...)`.
- Interface definitions MUST be emitted with their descriptions, fields, implemented interfaces, and semantics metadata, but MUST NOT get query roots.
- Object type and field descriptions from SDL MUST be preserved in the generated query schema so `ontology-query-schema`, reference, authoring guide, and agent-facing docs share one semantic source.
- Generated root names MUST be conflict-checked against built-ins and other generated roots.
- Runtime helper object names such as `OntologyRuntime`, `CodeRuntime`, `RuntimePath`, and `RuntimeWarning` MUST be reserved so authored ontology types, interfaces, or enums cannot collide with built-in GraphQL runtime types.
- Public node locator inputs MUST use string refs at the GraphQL boundary. Structured `NodeRef` remains an output/diagnostic object; callers SHOULD pass the same author-facing refs used by CLI/query recipes, links, aliases, copied URLs, typed identifiers, and saved workflow state.
- The general-purpose `notes(...)` root MUST return `NoteConnection` with `nodes`, `pageInfo`, and warnings. A separate `notesConnection(...)` root would duplicate the same concept and MUST NOT be generated.
- `nodes(refs:)` MUST be the batch equivalent of repeated `node(ref:)` reads: it preserves input order, returns `requestedRef`, and reports per-item errors for unresolved refs.

### Runtime Utility Roots

- `ontology` and `code` roots MAY expose stable runtime metadata that agents otherwise gather by calling multiple tools, but they MUST stay read-only and bounded.
- Agent chat, session, and runtime metadata MUST NOT be exposed through this public GraphQL runtime surface. Agent-only workflows belong behind internal `/api/agent/*` routes or agent CLI/MCP surfaces.
- Runtime roots MUST NOT be authored ontology types and MUST NOT create user-definable schema roots.
- Runtime field inputs that identify files or notes MUST be normalized through vault-root-relative strict path helpers before any filesystem read, stat, index lookup, or context packing.
- Runtime fields MUST reject or warn on empty required semantic inputs such as blank ontology type names instead of widening to all types.
- `ontology.authoringGuide(type:)` MUST return type-specific authoring guidance and structured unavailable/invalid warnings when the type cannot be served.
- `ontology.nextId(type:, count:)` MUST return identifier allocation state, including a contiguous `ids` batch when requested, and structured errors without writing a note.
- `code.docsForCode(path:)` MUST return bounded docs and note context for an in-vault code path without triggering indexing, code-anchor recomputation, or other write-side maintenance.
- `code.testsForCode(path:)` MUST derive bounded in-vault test candidates without probing outside-vault paths.
- `code.codeForNote(path:)` MUST treat its input as a note path; callers that only have a code path should use code-specific fields.
- The public GraphQL schema MUST NOT expose an `agent` root. Agent command, validation, report, tool, and capability metadata belong to `rzm agent surface` and the agent API/MCP boundary rather than the ontology query schema.
- Runtime execution MUST preserve partial-data behavior: provider failures should surface response-path errors while keeping structured unavailable packs and warnings for the failing field.
- Runtime object selections MUST honor supported inline fragments the same way note and section selections do.

### Locator Fields and NodeRef Identity

- Every note and section selection MUST expose `locator: NodeLocator!`.
- Note locators MUST resolve from a note-root `NodeRef`.
- Section locators MUST resolve from a section or embedded-node `NodeRef`, depending on the concrete section type role.
- Embedded-node locators MUST prefer durable block-id link targets when available.
- If an embedded node cannot be durably linked, the locator MUST expose `requiresFix: true` or unresolved status rather than emitting a fragile heading-only link as if it were stable.
- `NodeLocator` MUST preserve `sourceLocator`, `status`, optional `linkTarget`, optional `wikilink`, optional `markdown`, `exists`, and `requiresFix`.
- `NodeLinkTarget` MUST preserve `markdown`, `wikilink`, `displayLabel`, `exists`, `requiresFix`, and `blockId`.
- Query execution MUST use the same locator/link-target contract as the node workspace and copy-link flows.

### Section and Embedded Resolution

- Section fields MUST resolve through the owning note or owning section; sections do not have standalone query roots.
- Exact heading text plus exact heading level MUST bind `@contains(heading:, level:)` fields.
- List section fields without `heading` MUST return direct children at the requested level in document order.
- Note-scoped list-no-heading matching MAY unwrap a single H1 root so H2 children under a document title can be queried naturally.
- Singular section fields MUST error when multiple headings match.
- Required section fields MUST error when the matching section is missing or empty.
- Section `children(first:)` MUST preserve document order and carry the best concrete section type only when the match is unambiguous.
- Ambiguous child section typing MUST fall back to the built-in `Section` interface rather than guessing one concrete subtype.
- Section scalar and enum fields MUST read inline properties from the section's own content with descendant ranges subtracted.
- Section inline property casing MUST be derived from the enclosing note type's `propertyCase`, so reused section types can work under different note conventions.
- Embedded-node authored identifier fields MUST override the synthetic section `id` in GraphQL selections when the ontology declares an inline `@identifier` field such as `id: ID! @field @identifier(preferred: true)`.
- Packed Dataview-style inline properties are tolerated by parsing, but authoring guidance SHOULD prefer one field per line. For section-backed embedded nodes, the preferred form is a metadata bullet such as `- key:: value`.

### Structural vs Ambient Relations

- `@link` fields MUST resolve structural ontology edges only.
- `@neighbors` fields MUST resolve ambient typed neighbors derived from body links, backlinks, or other indexed ambient evidence.
- Built-in `linked`, `backlinked`, and `connected` fields MUST remain generic ambient escapes and MUST NOT stand in for authored structural fields.
- Structural and ambient rows MUST remain separate in read models and query loaders so provenance stays explainable to browser and agent surfaces.
- Interface type filters on relation fields MUST load broadly when necessary, then filter returned records by concrete implementation.
- Duplicate ambient relation rows MUST be deduped before GraphQL results are emitted.
- Section-local outbound neighbors with `scope: SUBTREE` MUST scan only the matched section subtree and ignore links inside fenced or inline code examples.
- Section-local inbound or `BOTH` neighbors MUST resolve only when the target can be matched by durable block/heading or indexed embedded-node edge evidence.
- Section structural link fields MUST resolve inline property link values through the note path cache, including aliases, and MUST type-check the resolved note against the field target type or interface.

### Request-Scoped Batching

- Query execution MUST create one loader set per `Execute` call.
- Loaders MUST use a `noderead.Scope` for note hydration, type lookups, assessments, and relation traversal.
- Relation fields across a parent list MUST batch by relation spec across all parent paths before resolving child selections.
- Nested relation fields selected under list results MUST batch per depth and relation spec, not per parent object.
- Repeated target notes in one request scope SHOULD hydrate once for the selected summary/profile path.
- Query-local caches MAY sit above `noderead.Scope`, but their keys must include relation/provenance/type/limit semantics and must mark empty source groups as loaded.
- Structural relation batches MUST be cached by relation name.
- Ambient relation batches MUST be cached by provenance, relation name, destination type filter, and per-source limit.
- Batch caches MUST record empty source groups as loaded so repeated misses do not requery storage.
- Query execution MUST remain read-only over indexed state plus bounded markdown projection/hydration.

### Indexed Ontology Node Root Execution

Note and embedded-node typed roots SHOULD plan from the indexed ontology catalog when the query can be answered by catalog identity, source locator, built-in summary fields, indexed scalar/enum/boolean/date/list fields, and link identity rows.

Supported filters MUST be converted to typed field predicates using the schema field definition. Equality, membership, existence, and scalar comparisons are allowed only when the field capability says the operator is legal. Link predicates MUST accept author-facing note inputs: canonical refs, vault-relative paths with or without `.md`, wikilinks, titles, and aliases. Resolved inputs compare through indexed link field row metadata; unresolved inputs retain raw normalized matching so authored unresolved values can still be found. Ambiguous title or alias inputs MUST NOT widen to multiple targets, and `ontology.queryPlan(...)` MUST warn when a link filter is unresolved or ambiguous.

Supported sort MUST use indexed field rows before root pagination, even when some filters remain residual. Date and scalar sorts should remain deterministic by applying catalog tie-breakers such as note path and source order. If a sort cannot be pushed, execution must treat it as residual rather than pretending the root is fully indexed.

Unsupported filters MAY run as residual filters only after a bounded indexed type candidate set is loaded. Residual filtering MUST NOT trigger unbounded note projection before pagination. Queries that select `content`, workspace/source-preserving section bodies, children, or non-indexed fields MAY project markdown for the bounded result set.

Selections that only require catalog identity, locator metadata, titles, note path, resolved type, indexed scalar/enum/boolean/date/list values, and link identity rows SHOULD avoid markdown content reads. Link relation object selections may still need normal note-resolution support, but they should not require reprojecting the source embedded item.

`ontology.type(...).fields.capability` is the executable field capability surface. It MUST expose the union of legal filter operators plus indexed and residual operator splits so clients can show valid controls without confusing pushed execution with table/runtime residual work.

`ontology.queryPlan(type:, filters:, sort:, select:, first:)` is the explicit debug surface for this behavior. Normal successful roots should stay quiet; callers that ask for a plan can distinguish indexed execution, indexed execution with residual constraints, projection fallback, unavailable index state, link-filter resolution warnings, and empty/stale-suspect indexed roots.

### Error and Nullability Behavior

- `Prepare` MUST return errors for unsupported operations, empty queries, named fragments, directives, fragment spreads, unsupported roots, invalid variable values, and invalid root bounds.
- `Execute` MUST return errors for missing schema, prepared query, store, or note reader dependencies.
- Execution MUST attach a best-effort response path to field-level errors.
- Field-level errors SHOULD omit only the failing field while preserving sibling fields and parent objects when possible.
- Required scalar, enum, structural relation, and section fields MUST report errors when required values are missing or invalid.
- Optional scalar or enum fields with invalid values SHOULD return `null` without leaking raw invalid strings.
- Empty authored list fields MUST round-trip as empty lists when the field exists and validates.
- Missing optional singular relations or sections MUST return `null`.
- Missing optional list relations or sections MUST return an empty list.
- Typed roots with a `path` that resolves to a different concrete type MUST return an empty list, not an error.
- Invalid typed notes MUST still resolve at roots; selecting the invalid required field is what emits the field error.
- Store, hydration, note-reader, or semantic-search failures MUST be surfaced as query errors instead of silently widening or fabricating results.

### Limits and Ordering

- `first` MUST default to `20` for generic roots, typed roots, relation fields, and section children.
- Root `first` values less than or equal to zero MUST behave as the default `20`.
- Root `first` values greater than `200` MUST be capped at `200`.
- `find` roots MUST rank exact substring matches before token matches, then sort ties by path.
- Property roots MUST preserve the store-provided path order after type filtering.
- Semantic roots MUST dedupe by `NoteID` or path before type filtering.
- Section matches and section children MUST preserve markdown document order.
- Built-in ambient relation fields MUST apply requested `first` after interface-aware filtering so broad interface filters return the first matching records, not just the first raw rows.
- Per-source relation limits MUST be explicit in the loader request whenever the field asks for bounded ambient results.

### Semantic Root Selection

- Semantic roots MUST use the configured `SemanticSearcher`; when none is available, execution MUST return `semantic search is unavailable`.
- The `semantic` argument MUST accept `[String!]`. Per GraphQL input coercion, a single-string value MUST coerce into a one-element list so existing recipes continue to parse without rewrites.
- An empty list or omitted `semantic` value MUST NOT be treated as a retrieval mode; if no other retrieval mode is supplied, prepare-time validation MUST fail with a structured error rather than widening to all notes.
- Each input term MUST be embedded and queried independently against the embedding store; results MUST be unioned (not intersected) and aggregated by max similarity across terms and across the matching node's chunks.
- Typed-root and `notes(type:, semantic:, ...)` selections MUST pre-filter by ontology type at the embedding store before similarity search; the resolver MUST NOT apply a post-hoc type filter on top of an already-truncated cross-type candidate list.
- Interface-typed selections (e.g. `notes(type: "SpecLike", semantic: ...)`) MUST resolve to the union of concrete implementing types and pre-filter the store query against that set.
- Typed-filter semantic results MUST aggregate per typed node — one entry per `ontology_node.id` — taking the max similarity across the node's primary body chunks and across input terms; the resolver MUST NOT return chunk-grouped or note-path-grouped results when a type filter is present.
- Untyped `notes(semantic: ...)` selections (no `type` argument) MUST aggregate per note path for cross-type chunk discovery; only typed-filter selections invoke the node-level survey path.
- Semantic results MUST surface through `NoteNode.score` so callers can triage borderline matches and see ranking confidence without rerunning retrieval; non-semantic selections MUST return null for `score`.
- Result ordering MUST be by `score` descending, with ties broken by note path (then node id when applicable) for determinism.
- Semantic retrieval MUST remain an explicit root selector; it MUST NOT become a fallback for unbounded typed roots, unresolved paths, unsupported fields, or relation misses.

### Fixture and Regression Expectations

- `pkg/ontology/query/query_test.go` is the primary fixture surface for this contract.
- Schema-generation tests MUST cover built-ins, descriptions, semantics metadata, generated typed roots, interfaces, section types, and embedded-node built-ins.
- Prepare tests MUST cover unsupported root forms, variable-backed root bounds, missing required variables, invalid variable coercion, variable defaults, named fragments, directives, unbounded roots, multiple root modes, and semantic detection inside root inline fragments.
- Execute tests MUST cover structural links, ambient built-ins, interface type filters, section matching, section-local neighbors, embedded-node locators, section scalar validation, authored embedded IDs, and semantic roots.
- Batching tests MUST count store calls for multi-parent structural and ambient relation selections and fail on per-parent relation query regressions.
- Fixture notes MUST include both structural frontmatter/inline links and ambient body links/backlinks so relation semantics cannot collapse accidentally.
- Tests that add new section or embedded-node behavior SHOULD include block-id or heading locator coverage when linkability matters.

## User Stories

### US6 - Compose typed note reads with ontology authoring-guide and identifier-allocation context in one GraphQL operation
- id:: ^SPEC-0042-US6
- summary:: Compose typed note reads with ontology authoring-guide and identifier-allocation context in one GraphQL operation.
- status:: ready

#### Acceptance Criteria

- A query can select typed note data beside `ontology.authoringGuide(type:)`, `ontology.nextId(type:, count:)`, and type metadata.
- Runtime ontology fields return structured unavailable or invalid warnings instead of forcing agents to scrape CLI failures.

### US7 - Load code, docs-for-code, tests, and note-related code context through bounded GraphQL runtime fields
- id:: ^SPEC-0042-US7
- summary:: Load code, docs-for-code, tests, and note-related code context through bounded GraphQL runtime fields.
- status:: ready

#### Acceptance Criteria

- Code runtime fields normalize and enforce vault-scoped paths before reading, statting, or packing context.
- Code runtime fields expose docs, notes, tests, and code paths as bounded `RuntimePath` lists with warnings for unavailable evidence.
- Read-only code runtime fields do not trigger indexing, code-anchor recomputation, or other write-side maintenance.

### US8 - Discover agent capabilities from the agent surface rather than public GraphQL
- id:: ^SPEC-0042-US8
- summary:: Discover agent command, validation, report, and tool capabilities from the live agent surface while keeping public GraphQL limited to ontology and bounded code reads.
- status:: ready

#### Acceptance Criteria

- The generated public GraphQL schema exposes `ontology` and bounded `code` runtime roots but no `agent` root.
- `rzm agent surface` is the authoritative discovery path for current `rzm agent` commands, reports, validations, tools, mutation classification, and capability state.
- Agent surface metadata is projected from the same descriptor catalog used for dispatch, with parity tests proving every advertised shared tool is reachable exactly once.
- Capability metadata uses fresh independent readiness snapshots without weakening handler-local wait/degrade behavior or exposing arbitrary command execution.

### US9 - Keep runtime utility roots bounded, read-only, partial-data-friendly, and covered by the same regression surface as existing note queries
- id:: ^SPEC-0042-US9
- summary:: Keep runtime utility roots bounded, read-only, partial-data-friendly, and covered by the same regression surface as existing note queries.
- status:: ready

#### Acceptance Criteria

- Runtime helper GraphQL type names and root names are reserved against authored ontology collisions, with `interface Note` allowed as the sole authored overlap and `NoteNode` preserving universal note reads while implementing authored nullable `Note` fields. ^SPEC-0042-US9-AC1
- Runtime selections preserve inline-fragment behavior and partial-data errors with structured warnings. ^SPEC-0042-US9-AC2
- Runtime root behavior is covered by `pkg/ontology/query/query_test.go` and provider boundary tests. ^SPEC-0042-US9-AC3

### US5 - Survey an entire typed slice for one or more topics and get a ranked, deduped list of typed nodes with score, so candidate triage does not require rerunning chunk-based searches and does not silently drop type-relevant nodes when the typed corpus is small
- id:: ^SPEC-0042-US5
- summary:: Survey an entire typed slice for one or more topics and get a ranked, deduped list of typed nodes with score, so candidate triage does not require rerunning chunk-based searches and does not silently drop type-relevant nodes when the typed corpus is small.
- status:: ready

#### Acceptance Criteria

- Typed-root and `notes(type:, semantic:, ...)` selections accept `semantic: [String!]` as a multi-term union; a single-string value coerces to a one-element list, and an empty list fails prepare with a structured error when no other retrieval mode is supplied.
- Typed-filter semantic execution pre-filters by ontology type at the embedding store before similarity search, so chunks from notes whose type is not requested are never considered, including when the type filter is an interface that resolves to multiple concrete implementations.
- Typed-filter semantic results aggregate per `ontology_node.id`, taking the max similarity across the node's primary body chunks and across input terms; one entry per node is returned, ordered by score descending, with ties broken deterministically.
- `NoteNode.score: Float` is populated for results surfaced through a `semantic:` argument and null otherwise, exposing the per-node aggregate score so callers can triage borderline matches without rerunning retrieval.
- Untyped `notes(semantic: ...)` selections retain note-path-grouped chunk discovery for cross-type cases; only typed-filter and typed-root selections use the node-level survey path.
- Replacing the prior post-hoc type filter with the pre-filter survey is covered by `pkg/ontology/query/query_test.go` regression tests on a corpus where the post-filter would have silently dropped type-relevant results.

## Open Questions

- Should diagnostics for relation batch counts, hydration counts, and cache hits move from tests into a debug result envelope for `ontology query`?
- Should typed roots eventually accept cursor pagination, or is `first` sufficient while the query layer remains agent-oriented and bounded?
- Should section-local inbound neighbors stay in this query layer long term, or move behind a richer `noderead` structural-node traversal plan?
- Should the typed semantic survey expose per-card or per-intent match payloads beside the per-node aggregate `score`, or is the rolled-up node score sufficient for triage?
