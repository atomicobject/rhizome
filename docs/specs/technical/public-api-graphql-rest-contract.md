---
type: TechnicalSpec
summary: "Defines the technical contract for Rhizome's minimal public API: GraphQL for composable reads, REST for operations and staged writes, NodeRef identity throughout, and frontend migration onto the same contract."
id: SPEC-0057
spec-status: active
last-updated: 2026-07-15
aliases:
  - SPEC-0057
  - public-api-graphql-rest-contract
  - Rhizome public API GraphQL REST contract
---

# Public API GraphQL/REST Contract

## Summary

Rhizome's current HTTP server already exposes frontend-oriented routes, OpenAPI-described payloads, ontology query execution, node workspace reads, edit sessions, validation, events, and agent chat. The public API contract should promote the durable vault, ontology, validation, event, and edit-session pieces into a small versioned surface rather than freezing current browser-pane or agent-chat payloads as the platform model.

The technical split is: GraphQL owns composable read selection over Rhizome knowledge, while REST owns lifecycle resources, operation packets, event streams, and staged write workflows. `noderead.Scope` remains the shared request-scoped read substrate; GraphQL is the public composition layer, not a second read engine. Existing node workspace behavior becomes a GraphQL query plus frontend adapter rather than a primary platform endpoint.

## Goals

- establish `/api/v1` as the stable public HTTP API namespace
- make GraphQL the canonical flexible read contract for nodes, typed lists, relations, graphs, schema metadata, search, and runtime read helpers
- make `/api/v1/graphql` at least as capable as the read-only GraphQL currently exposed through `rzm agent ontology-query` and checked-in saved query recipes
- make REST the canonical operation contract for status, capabilities, validation, SSE events, edit sessions, and other workflow lifecycle resources
- preserve canonical `NodeRef` identity through all public read payloads
- reuse `noderead.Scope`, ontology query execution, existing read models, validation, and edit-session services instead of building parallel stacks
- keep public reads bounded, deterministic, observable, and safe for custom apps
- publish OpenAPI for REST and GraphQL SDL/introspection for the vault-specific read schema without duplicating schema truth
- define public contracts for query recipes, ref resolution, pagination, events, capabilities, validation reads, and shared error codes before frontend migration depends on them
- make the built-in frontend prove the public contract by retiring non-agent compatibility routes after migration
- keep generated schemas/client types and contract tests in sync with the server

## Non-Goals

- removing all existing `/api/*` routes in the first implementation
- adding GraphQL mutations for markdown or ontology edits
- exposing MCP tool dispatch as a generic HTTP API
- exposing `/api/agent/*` chat, settings, session, model, or tool-event routes as public API
- adding hosted multi-tenant access control
- replacing the ontology query contract, search pipeline, validation engine, or edit-session conflict model
- implementing local token authentication in the first API slice
- making current `NodeWorkspaceResponse` the external API shape
- adding a separate read model only for the public API
- supporting unbounded graph database traversal

## Requirements

### API Namespace and Stability

- The public API MUST live under `/api/v1`.
- The server MUST continue to serve existing web routes while `/api/v1` is introduced.
- Versioned routes MUST have documented compatibility expectations and structured error shapes.
- Public API schema artifacts MUST be checked in or generated through the existing repo generation workflow.
- Compatibility shims MAY call into v1 services, but new public behavior SHOULD be added to v1 first.
- The first API slice MUST document local trusted-use defaults, CORS defaults, default bind behavior, and the reserved future token-auth header shape without requiring token enforcement.
- The initial local-first public API SHOULD bind only to localhost unless an operator explicitly opts into broader network exposure.
- GraphQL schema compatibility MUST be documented separately from vault ontology evolution: removing public runtime roots, renaming public fields, changing `NodeRef` identity semantics, removing enum values, or tightening accepted arguments is breaking; adding ontology-derived types/fields, adding optional fields, adding enum values, enriching descriptions, and adding warnings is compatible unless a checked-in query recipe or generated client contract says otherwise.

### GraphQL Endpoint

- `/api/v1/graphql` MUST execute read-only GraphQL operations.
- The endpoint SHOULD reuse the existing ontology query preparation and execution path where possible.
- The endpoint MUST support every read-only GraphQL operation that the agent CLI `ontology-query` surface can execute, including operation names, variables, fragments, aliases, runtime roots, typed roots, and bounded selectors.
- Checked-in saved query recipes that validate against the live ontology query schema MUST be executable through `/api/v1/graphql` when their inputs are supplied as variables and the recipe does not depend on non-GraphQL agent retrieval sidecars.
- The endpoint MUST preserve bounded root validation from SPEC-0042.
- The endpoint MUST use `noderead.Scope` for node hydration, type instances, relations, graph slices, locators, and diagnostics wherever those reads are already owned by noderead.
- The endpoint MUST return GraphQL-standard `data` and `errors` fields plus Rhizome warning/diagnostic extensions where needed.
- The endpoint MUST accept variables as JSON and MUST validate them before execution.
- The endpoint MUST expose standard GraphQL introspection for local-first development unless a future security policy explicitly disables it.
- The endpoint MUST expose a cheap SDL/schema-fetching route such as `GET /api/v1/graphql/schema` or an equivalent documented capability.
- The endpoint MUST reject mutations until a separate write contract is specified.

### GraphQL Schema Introspection

- GraphQL schema introspection MUST reflect the current vault runtime, including ontology-derived note types, section types, embedded-node types, interfaces, enum values, input objects, custom scalars, and Rhizome runtime roots.
- GraphQL SDL and introspection MUST expose the same root fields, input shapes, runtime roots, and typed roots that `rzm agent ontology-query-schema` exposes to query recipes.
- GraphQL SDL generation MUST preserve descriptions from ontology SDL and Rhizome built-ins wherever possible.
- Object, field, argument, enum, enum value, input field, scalar, and runtime-root descriptions SHOULD explain user-facing meaning, source data, required bounds, and unavailable-provider behavior.
- Fields that are superseded or compatibility-only MUST use GraphQL deprecation metadata with a migration hint.
- Custom scalars such as `Date`, `DateTime`, `URL`, and `JSON` MUST be declared and documented in the SDL.
- Arguments that bound broad reads, such as `first`, `depth`, `type`, `find`, `property`, `semantic`, and graph/profile controls, MUST have descriptions that make the bound or default visible to GraphQL tooling.
- Rhizome warning and diagnostic extensions MUST be documented in the GraphQL developer docs even when they are not part of the standard introspection type system.
- Schema fetches SHOULD be cheap enough for developer tools and custom apps to run at startup without triggering indexing, projection, or semantic provider work.
- Contract tests MUST verify that generated SDL includes descriptions for representative ontology types, fields, arguments, enum values, and runtime roots.

### Minimal GraphQL Root Shape

The first public GraphQL schema SHOULD expose these roots or equivalent names:

```graphql
type Query {
  node(ref: String!): Node
  nodes(refs: [String!]!, first: Int = 50): NodeBatchResult!
  note(path: String, ref: String): NoteNode
  resolve(ref: String!): NodeRefResolution!
  notes(type: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20): NoteConnection!
  search(query: [String!]!, type: String, first: Int = 20): NodeSearchResult!
  validation(check: String, firstIssues: Int = 50): ValidationState!
  ontology: OntologyRuntime!
  code: CodeRuntime!
}
```

- `node(ref:)` MUST be the canonical replacement path for browser node workspace reads.
- Public ref inputs MUST be author-facing strings, not structured internal ref objects. Supported refs include locators, paths, wikilinks, aliases, fragments, copied URLs, typed identifiers, and code paths.
- `resolve(ref:)` MUST resolve author-facing refs into canonical `NodeRef` values plus warnings/diagnostics for callers that need to preflight refs without loading a node.
- `nodes(refs:)` MUST support efficient batch reads by input refs and return per-item errors instead of failing the whole batch when one ref is invalid.
- `notes(...)` MUST support bounded typed, filtered, property, and semantic lists as a connection with `nodes`, `pageInfo`, and warnings. A separate `notesConnection` root MUST NOT be exposed.
- `search(query:, type:, first:)` MUST return destination note nodes with `NodeRef` or explicit warnings. It runs the shared unified search engine restricted to notes (lexical, semantic, and structural lanes, with `type` narrowing to concrete implementors before retrieval); there is no mode argument. When no note searcher is configured it MUST return a `search_unavailable` warning, and engine warnings MUST surface as `warnings`.
- `validation(...)` MUST expose validation state as queryable read data for custom workflow views without forcing callers to invoke the REST validation workflow.
- `Node.neighborhood(...)` and `Node.localGraph(...)` MUST preserve typed endpoint identity, edge provenance, relation names, truncation, and diagnostics.
- Runtime roots MUST remain read-only and bounded, matching SPEC-0042.
- Agent/chat runtime roots MUST NOT be part of the public GraphQL schema; agent chat/session surfaces remain internal Rhizome application APIs.
- Existing ontology-derived root names and runtime roots used by saved query recipes MUST remain available, or their replacement MUST be documented through query-recipe validation failures that name the migration path.

### Pagination, Bounds, and Batch Reads

- Every public GraphQL list root MUST document its default limit, maximum limit, ordering, cursor semantics, and truncation behavior in SDL descriptions.
- Connection-like results MUST expose `nodes`, `pageInfo`, and warnings or diagnostics when results were truncated, provider-backed, stale, or partially unavailable.
- Cursor values MUST be opaque to callers and scoped to the query shape that produced them.
- Cursor values MUST include or be invalidated by a schema/query-shape version so clients do not reuse cursors across incompatible schema, filter, sort, or bound changes.
- Broad roots MUST reject missing bounds unless SPEC-0042 already permits a safe default.
- Batch-by-ref reads MUST preserve input order and return item-level `data`, `error`, and `warning` states.
- Contract tests MUST cover limit defaults, maximum-limit rejection, truncation warnings, and one partial batch failure.

### Ref Resolution

- Public ref resolution MUST normalize author-facing inputs before cache keying, traversal, event subscription, or edit-session staging.
- Supported inputs MUST include canonical `NodeRef`, `sourceLocator`, vault-relative paths, markdown links, wikilinks, aliases, Obsidian block fragments, heading fragments, copied `/notes?...` URLs, and typed identifiers when the ontology exposes them.
- Resolution results MUST include canonical ref, source locator, title or label when available, resolved type when available, ambiguity diagnostics, and candidate refs when an input is ambiguous.
- Preferred identifiers or aliases with multiple claimants MUST resolve as structured ambiguity containing every candidate canonical ref; public reads MUST NOT use first-match wins.
- GraphQL MUST expose ref resolution for composable reads. REST MAY expose `POST /api/v1/refs/resolve` if frontend or integration clients need a small non-GraphQL helper.
- Contract tests MUST cover aliases, embedded-node block ids, heading fragments, path commas, ambiguous matches, and unresolved inputs.

### Node Workspace Migration

- The retired `/api/ontology/node-workspace` response was a browser convenience payload, not the durable public contract.
- The durable GraphQL `node(ref:)` shape MUST expose canonical ref, locator, title, type, status, capabilities, fields, content, relations, and graph slices as independently selectable fields.
- Frontend-specific concepts such as pane stacks, outline tabs, loaded flags, relation rails, and `fields[]` or `collections[]` projections SHOULD be assembled by the frontend adapter.
- The frontend adapter MUST build its pane workspace from public GraphQL node objects; the retired unversioned REST node-workspace detail and graph routes MUST return the standard unknown-route error.
- Contract tests MUST prove the GraphQL query can satisfy the core existing node workspace use case.

### REST Endpoints

The first versioned REST surface SHOULD include:

```text
GET  /api/v1/status
GET  /api/v1/capabilities
POST /api/v1/graphql
GET  /api/v1/graphql/schema
GET  /api/v1/events
GET  /api/v1/nodes/events
GET  /api/v1/query-recipes
GET  /api/v1/query-recipes/{id}
POST /api/v1/query-recipes/{id}/execute
GET  /api/v2/validate
```

- REST endpoints MUST use HTTP status codes plus a structured JSON error body.
- `status` MUST be cheap and MUST NOT block on slow indexing or embedding initialization.
- `StatusResponse` MUST expose `indexState: "initializing" | "ready"` as the explicit cold-start signal so callers can decide whether index-dependent reads are safe; populating `indexState` MUST stay cheap and ungated and MUST NOT block on slow indexing. `capabilities` MUST advertise the same `indexState` alongside the other version/hash metadata.
- The server MUST apply a one-shot first-launch index gate around index-dependent routes: graph reads; ontology summary, types, atlas, and inspect; view execution and field-candidate reads; and node-workspace reads. Public GraphQL and query-recipe execution MUST classify the prepared operation before choosing a gate: exact raw-note operations use the narrower provider-current note gate, while all other operations use the complete-model gate. The complete gate opens exactly once when a previously materialized model is observed before a local rebuild, when a follower observes an existing model, or when the local `BackgroundIndexer` callback completes successfully. A failed empty build MUST remain gated, and the gate MUST NOT re-close on transient `Ready=false` mid-session. While closed, gated routes MUST return `503 Service Unavailable` with `Retry-After: 5` and the structured `INDEX_INITIALIZING` code after a server-side wait of roughly 60 seconds. The ungated allowlist MUST include `/api/v1/status`, `/api/v1/capabilities`, OpenAPI document routes, SSE streams (`/api/v1/events`, `/api/v1/nodes/events`), `/api/v2/validate`, file routes, list/catalog routes, agent routes, and the GraphQL/ontology schema endpoints (`/api/v1/graphql/schema`, `/api/v1/ontology/query-schema`); these MUST remain reachable during cold start so clients can boot, observe, and degrade gracefully.
- `capabilities` MUST report read/write support, ontology schema state, indexing state, embeddings, code index, event support, and provider availability.
- `capabilities` MUST include Rhizome version, vault identity, ontology schema hash, GraphQL schema hash, query recipe hash or generation, index generation/hash when available, validation generation/hash when available, provider availability, and degraded reasons.
- Query recipe catalog resources MUST report recipe id, title/description, variables, defaults, required inputs, output expectations, source path, schema compatibility, and execution surface: `graphql`, `http-safe`, or `cli-only`.
- Query recipe execution MUST route through the same query-recipe validation/binding layer as the CLI and the same prepared-query readiness admission as public GraphQL. Exact-note recipes MUST use one request-local metadata/property/tag snapshot. Recipes that depend on non-GraphQL sidecars MUST be rejected or marked non-executable over HTTP with a structured error.
- `validate` MUST return structured validation results and MAY offer safe-fix modes only when the existing validation flow supports them.
- Edit-session capability metadata MUST point at `/api/v1/edit-sessions`; retired unversioned edit-session routes MUST NOT be advertised.
- SSE event routes MUST publish invalidation events, status/index changes, and stale signals as refetch triggers. `/api/v1/events` is vault-wide; `/api/v1/nodes/events` is the node-scoped subscription route for clients that need pane/view-level invalidation.

### Public Event Contract

- `/api/v1/events` MUST be a general invalidation stream, not a content patch stream.
- Events MUST share the current envelope with `id`, `kind`, and optional event-specific `data`.
- The first public event vocabulary MUST include `node.changed`, `validate.changed`, `validation.invalidated`, `schema.invalidated`, `query_recipe.invalidated`, `index.changed`, `index.invalidated`, `capabilities.invalidated`, and `edit_session.invalidated`.
- Event `data` SHOULD include refs, paths, type names, query recipe ids, validation checks, edit-session ids, generations, hashes, and refetch hints when known and cheap.
- SSE MUST support heartbeats and graceful reconnect. Event replay from `Last-Event-ID` is deferred from v1; clients MUST treat reconnect as a signal to refetch status, capabilities, and active views.
- `/api/v1/nodes/events` MUST accept the same public ref forms as `node(ref:)`, canonicalize them before subscription, and stream node-level `node.updated`, `node.stale`, and `node.deleted` refetch triggers. It MUST NOT be the only way to notice broad vault changes; clients with query-driven views SHOULD also use `/api/v1/events`.
- Contract tests MUST cover event envelope shape, vocabulary constants, heartbeat/reconnect behavior, and at least one node, validation, schema, and edit-session invalidation event.

### Capabilities and Schema Versioning

- `/api/v1/status` MUST remain cheap and readiness-oriented.
- `/api/v1/capabilities` MUST be the richer feature-discovery contract for client bootstrapping.
- Capabilities MUST distinguish `available`, `degraded`, `unavailable`, and `unsupported` states where booleans would hide why a feature cannot be used.
- Capabilities MUST expose stable cache keys or hashes for GraphQL SDL, ontology schema, query recipes, indexes, validation state, and provider state when known.
- Clients MUST be able to compare capabilities before and after `schema.invalidated`, `query_recipe.invalidated`, `index.changed`, `validation.invalidated`, or `capabilities.invalidated` events to decide which generated artifacts or active views need refresh.

### OpenAPI Contract

- REST routes under `/api/v1` MUST be described by an OpenAPI document.
- The public OpenAPI source SHOULD live outside browser-specific `web/` ownership once `/api/v1` exists. `pkg/app/web/openapi.yaml` is the embedded public REST transport contract that also drives frontend type generation.
- OpenAPI MUST describe request/response bodies, status codes, structured error envelopes, auth/CORS policy, SSE transport details, and operation ids for generated clients.
- Public OpenAPI MUST omit internal routes, including `/api/agent/*` and unversioned compatibility routes, unless a route is explicitly tagged as internal in a separate non-public artifact.
- OpenAPI MUST describe `POST /api/v1/graphql` only as the HTTP transport wrapper: request body with `query`, `variables`, and optional `operationName`; response body with `data`, `errors`, and optional `extensions`.
- OpenAPI MUST NOT attempt to model the full GraphQL object, field, relation, or ontology schema. GraphQL SDL/introspection is the source of truth for those shapes.
- REST TypeScript clients SHOULD be generated from OpenAPI; GraphQL operation types SHOULD be generated from GraphQL schema artifacts plus checked-in operation documents.
- Contract tests MUST verify that the checked-in/generated OpenAPI route list matches the server's public v1 REST registrations.

### Search Boundary

- GraphQL MUST expose a composable `search(...)` root for custom apps that need typed destinations, selected fields, provenance, and warnings.
- The `search` root MUST run the unified engine rather than one retrieval lane, and `semantic:` selectors MUST stay chunk-only; neither may silently fall back to the other. Unavailable lanes MUST surface as structured warnings.
- REST MAY expose opinionated search packets when the product shape is fixed, such as answer-shaped search from SPEC-0034, but this is deferred from the first slice unless a frontend fixture proves it is needed.
- Search results MUST preserve owner/source provenance and canonical NodeRef metadata.
- Search APIs MUST surface unavailable embeddings, missing code indexes, ambiguous targets, and stale indexes as warnings.
- Search APIs MUST keep limits explicit and deterministic.

### Validation Read Contract

- GraphQL MUST expose validation state for nodes, types, checks, and workflow dashboards without invoking fix application.
- Validation reads SHOULD include issue code, check name, severity when available, affected ref/path/field/target, message, fixability summary, and stale/degraded state.
- REST validation endpoints MUST own validation execution, safe-fix application, and lifecycle-like operations.
- GraphQL validation reads and REST validation workflow responses MUST share issue-code and error-code vocabularies.

### Data Identity and Error Model

- Public read payloads MUST use canonical `NodeRef` values as identity.
- Author-facing locators such as markdown links, wikilinks, paths, and fragments MAY be accepted as inputs but MUST be resolved before cache keying or traversal.
- Responses SHOULD include both canonical refs and author-facing locators when a human app needs copy/link affordances.
- REST error bodies and GraphQL `errors[].extensions` MUST share the same public error-code taxonomy.
- The initial taxonomy MUST include `BAD_REQUEST`, `GRAPHQL_VALIDATION_FAILED`, `INDEX_INITIALIZING`, `METHOD_NOT_ALLOWED`, `NOT_FOUND`, `QUERY_RECIPE_COMPILE_FAILED`, `SCHEMA_UNAVAILABLE`, `STREAMING_UNSUPPORTED`, and `INTERNAL`. `INDEX_INITIALIZING` is the canonical retry signal returned with `503 Service Unavailable` and `Retry-After` while the cold-start index gate is closed; clients SHOULD retry once after the advertised delay before surfacing a degraded UI.
- Errors MUST distinguish invalid input, unresolved target, unavailable provider, stale index, validation issue, conflict, unsupported operation, auth state, and internal failure.
- GraphQL field-level failures SHOULD use partial data with `extensions.code`, `extensions.ref`, `extensions.path`, or `extensions.hint` where useful.
- REST failures MUST include `code`, `message`, optional `details`, and optional `hint`.
- Partial data MUST be preferred over all-or-nothing failure when one field or provider fails and the remaining result is still meaningful.

### Implementation Boundaries

- `pkg/app/web` MAY continue owning HTTP server wiring, but durable v1 services SHOULD live behind a package boundary that is not browser-specific.
- GraphQL resolvers MUST call shared services and `noderead.Scope` rather than reaching directly into SQLite or markdown projection helpers for each field.
- REST handlers MUST call shared validation/edit/event/runtime services rather than duplicating existing CLI or web logic.
- Query recipe HTTP handlers MUST call `pkg/ontology/queryrecipe` rather than parsing recipe YAML or GraphQL independently.
- Public event handlers MUST use one shared event envelope builder so global, node, validation, schema, query recipe, and edit-session events do not diverge.
- Frontend client code SHOULD separate public API helpers from internal Rhizome app helpers so agent chat can stay internal while vault, ontology, validation, graph, search, event, and edit-session behavior dogfoods `/api/v1`.
- OpenAPI generation for REST and GraphQL schema generation MUST be part of the repo's normal check/generation path.
- OpenAPI and GraphQL SDL MUST be generated or validated as separate artifacts because they describe different layers of the API contract.
- TypeScript frontend clients SHOULD be generated or typed from the public contract.

### Testing and Validation

- Unit tests MUST cover GraphQL root bounds, variable validation, NodeRef resolution, relation batching, partial-data errors, and graph identity preservation.
- HTTP contract tests MUST exercise `/api/v1/graphql`, status, capabilities, validation, events, and edit-session lifecycle routes through an actual test server.
- HTTP contract tests MUST execute representative raw agent-CLI GraphQL queries and saved query recipes through `/api/v1/graphql` and compare result shape, warnings, and errors with the existing ontology-query/query-recipe execution path.
- Contract tests MUST exercise query recipe catalog, query recipe execution, ref resolution, pagination/bounds, batch reads, validation reads, shared error codes, capability hashes, and public event vocabulary.
- OpenAPI validation MUST ensure every documented v1 REST endpoint has a server route and every public v1 REST route is documented.
- GraphQL introspection tests MUST ensure representative ontology-derived descriptions and Rhizome runtime-root descriptions survive schema generation.
- Frontend tests SHOULD include generated-client usage against representative API fixtures.
- Regression tests MUST prove the frontend node workspace is assembled from public GraphQL with canonical NodeRef identity and that retired unversioned workspace routes return the standard unknown-route error.
- Contract tests SHOULD include at least one custom-app-style query that builds a typed readiness or validation triage view.

### Documentation

- README or docs MUST document the minimal API, the GraphQL/REST boundary, and the stability level of v1.
- API docs MUST include example GraphQL queries for node detail, typed list, relation neighborhood, local graph, and search.
- API docs MUST include REST examples for status, capabilities, validation, events, and edit-session lifecycle.
- API docs MUST include query recipe catalog/execution examples, ref resolution examples, pagination examples, event reconnect guidance, and shared error-code guidance.
- API docs MUST explain that OpenAPI is the REST transport contract and GraphQL SDL/introspection is the read-schema contract.
- Docs MUST explain that GraphQL reads are bounded and read-only, while writes go through explicit REST edit sessions.
- Docs SHOULD include guidance for custom team workflow apps and local automation usage.

## Decisions

- `/api/v1/graphql` is the canonical public GraphQL execution route. `/api/ontology/query` and the remaining unversioned non-agent compatibility routes are retired; callers MUST use the versioned public surface.
- `/api/agent/*` is an internal Rhizome application surface. It may remain unversioned and used by the built-in Agent workspace, but it is excluded from public capabilities, public OpenAPI, and public compatibility guarantees.
- Answer-shaped REST search is deferred from the first v1 slice. GraphQL search owns composable search reads; REST should gain an opinionated search packet only after that product shape is specified.
- Local token authentication is a follow-on security slice. The first v1 API documents localhost/default CORS posture and reserves the future bearer-token header shape without enforcing it.
