---
type: ProductSpec
summary: "Defines Rhizome's minimal general-purpose API product contract for frontends, custom workflow apps, and local automation."
id: SPEC-0056
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0056
  - general-purpose-api
  - Rhizome general-purpose API
---

# Rhizome General-Purpose API

## Summary

Rhizome should expose a small, stable, general-purpose API that the built-in frontend, custom team workflow apps, and local automation can all use without scraping browser-specific payloads or shelling out to CLI commands for ordinary reads.

The product contract is GraphQL for composable Rhizome knowledge reads and REST for named workflows, runtime operations, event streams, and staged writes. The API should make custom apps feel native to Rhizome: they can browse typed knowledge, build readiness dashboards, run bounded searches, subscribe to freshness events, and stage reviewed edits through the same durable contracts the built-in frontend uses.

## Goals

- let teams build custom workflow apps on top of Rhizome without depending on web-pane-specific response shapes
- make the built-in frontend prove the same API contract external apps use
- make author-facing string refs the durable public input identity for notes, sections, embedded nodes, code, and graph/search results, with `NodeRef` returned as structured output metadata
- keep flexible reads composable while preserving bounded, explainable behavior
- keep workflow actions explicit, reviewable, and safe for local-first use
- expose enough capability and status metadata that apps can degrade gracefully when indexing, embeddings, ontology, or write features are unavailable
- provide first-class API discovery through OpenAPI for REST and GraphQL introspection/SDL for read schemas
- make public API reads at least as capable as the current agent CLI raw ontology-query and saved query-recipe GraphQL surface
- expose stable cache/version hints, query recipe metadata, ref resolution, and invalidation events so custom apps can stay correct without polling everything
- provide a minimal versioned surface before expanding into broader SDKs or hosted/team deployments

## Non-Goals

- turning every current `/api/*` route into a public v1 contract
- exposing browser pane layout state as a platform model
- replacing MCP, agent CLI, or command-line workflows
- exposing Rhizome's internal `/api/agent/*` chat/session routes as public API
- adding arbitrary command execution through the HTTP API
- making GraphQL mutations the first write surface
- requiring authentication, multi-user permissions, or hosted deployment semantics in the first API slice
- guaranteeing semantic search when embeddings are disabled or unavailable
- creating project-management-specific dashboards in core Rhizome

## User Stories

### US1 - Discover a small stable API surface and build against it without reverse-engineering the built-in frontend
- id:: ^SPEC-0056-US1
- summary:: Discover a small stable API surface and build against it without reverse-engineering the built-in frontend.
- status:: ready

#### Acceptance Criteria

- The API exposes versioned status, capability, GraphQL, event, validation, and edit-session entrypoints as the minimal public surface. ^SPEC-0056-US1-AC1
- Public API documentation distinguishes stable v1 endpoints from internal `/api/agent/*` routes and records retired compatibility routes. ^SPEC-0056-US1-AC2
- Capability responses tell apps whether ontology, embeddings, code index, validation, edit sessions, and event streams are available. ^SPEC-0056-US1-AC3
- Public examples show how to build at least one custom workflow view without importing built-in frontend code. ^SPEC-0056-US1-AC4
- REST routes are documented through OpenAPI, while GraphQL object fields are documented through introspection and SDL rather than duplicated in OpenAPI. ^SPEC-0056-US1-AC5
- Any GraphQL query or saved query recipe that can run through the agent CLI/query-recipe surface can be run through the public API with equivalent variables, bounded behavior, warnings, and result shape unless it is explicitly marked CLI-only. ^SPEC-0056-US1-AC6
- Public API discovery includes query recipe catalog metadata, recipe input schemas, and whether each recipe is HTTP-safe, GraphQL-only, or CLI-only. ^SPEC-0056-US1-AC7
- Capability and status responses expose version/hash/cache-key metadata for Rhizome, ontology schema, GraphQL schema, query recipes, indexes, validation, and provider availability. ^SPEC-0056-US1-AC8
- `/api/v1/status` and `/api/v1/capabilities` expose `indexState: "initializing" | "ready"` without waiting on slow indexing, so clients can tell whether index-dependent reads are safe. ^SPEC-0056-US1-AC9

### US2 - Build Rhizome's own UI from the same composable read contract that external apps use
- id:: ^SPEC-0056-US2
- summary:: Build Rhizome's own UI from the same composable read contract that external apps use.
- status:: ready

#### Acceptance Criteria

- The frontend reads node details, relations, graph slices, schema metadata, and search results through public GraphQL queries or documented v1 REST endpoints. ^SPEC-0056-US2-AC1
- Browser-shaped concepts such as pane stacks, relation rails, outline widgets, and list grouping remain frontend adapters over durable API objects. ^SPEC-0056-US2-AC2
- The built-in frontend uses public v1 REST and GraphQL for every non-agent read/write workflow; `/api/agent/*` is the only intentionally unversioned internal application surface. ^SPEC-0056-US2-AC3
- Frontend tests include at least one contract fixture that proves the public API can satisfy the core node workspace experience. ^SPEC-0056-US2-AC4
- Frontend reads survive a cold-start window: the AppShell shows a non-blocking index banner driven by status/SSE refresh, and the API client retries one bounded `503 INDEX_INITIALIZING` response after its `Retry-After` delay. ^SPEC-0056-US2-AC5

### US3 - Compose typed Rhizome knowledge into workflow-specific dashboards, queues, and handoff views
- id:: ^SPEC-0056-US3
- summary:: Compose typed Rhizome knowledge into workflow-specific dashboards, queues, and handoff views.
- status:: ready

#### Acceptance Criteria

- Apps can query typed node lists with filters, limits, readiness or validation signals, and caller-selected fields. ^SPEC-0056-US3-AC1
- Apps can open any returned note, section, embedded node, relation target, search result, or graph node by passing the returned `NodeRef.ref` string to `node(ref:)`. ^SPEC-0056-US3-AC2
- Apps can request relation neighborhoods and local graph slices without receiving unrelated browser workspace payload. ^SPEC-0056-US3-AC3
- Apps can use public examples to build views such as incomplete specs, ready prompt recipes, validation triage, or implementation handoff queues. ^SPEC-0056-US3-AC4
- Apps can discover the current vault-specific GraphQL schema, including ontology-derived types, fields, enums, descriptions, and deprecation metadata. ^SPEC-0056-US3-AC5
- Apps can execute or adapt checked-in saved query recipes against the same public GraphQL schema that the agent CLI uses. ^SPEC-0056-US3-AC6
- Apps can resolve author-facing paths, wikilinks, aliases, fragments, copied URLs, and typed identifiers into canonical `NodeRef` values before composing reads. ^SPEC-0056-US3-AC7
- Apps can use consistent pagination, batching, search, validation-state reads, and truncation warnings across typed lists, node lists, graph slices, and search results. ^SPEC-0056-US3-AC8

### US4 - Run operational workflows through explicit REST resources while keeping reads bounded and writes reviewable
- id:: ^SPEC-0056-US4
- summary:: Run operational workflows through explicit REST resources while keeping reads bounded and writes reviewable.
- status:: ready

#### Acceptance Criteria

- Validation, event subscription, and query recipe workflows use REST resources with explicit lifecycle states and structured errors. ^SPEC-0056-US4-AC1
- Staged writes preserve preview, rebase, conflict, and commit review semantics rather than silently mutating markdown through GraphQL. ^SPEC-0056-US4-AC2
- Event streams publish invalidation and status changes as refetch triggers, not as authoritative content patches. ^SPEC-0056-US4-AC3
- API errors and warnings are structured enough for apps and automation clients to decide whether to retry, degrade, ask a user, or stop. ^SPEC-0056-US4-AC4
- Event streams use a documented invalidation vocabulary with reconnect behavior and enough event metadata for clients to refetch only the views that may be stale. ^SPEC-0056-US4-AC5
- REST and GraphQL failures share a stable error-code taxonomy so apps can handle bad requests, validation failures, unavailable schemas, unsupported streams, missing resources, and internal failures consistently. ^SPEC-0056-US4-AC6
- Index-dependent graph, ontology, GraphQL execute, view execute/field-candidate, query-recipe execute, and node-workspace reads wait up to 60 seconds, then return `503 Service Unavailable`, `Retry-After: 5`, and `INDEX_INITIALIZING`; status, capabilities, schema endpoints, event streams, validation, file, list/catalog, and agent routes remain ungated. ^SPEC-0056-US4-AC7

## Requirements

### Public Surface

- Rhizome MUST expose a minimal versioned API surface under `/api/v1`.
- The minimal public surface MUST include status, capabilities, GraphQL, events, validation, and query recipe workflows.
- Edit-session workflows MUST be exposed under `/api/v1/edit-sessions*` and discoverable in capabilities.
- Unversioned non-agent `/api/*` routes MUST be retired. `/api/agent/*` remains the sole intentionally unversioned internal application surface and carries no public compatibility guarantee.
- `/api/agent/*` routes are internal Rhizome app routes, not public API resources; they MUST NOT be advertised as stable v1 capabilities or modeled in public OpenAPI.
- The public API MUST identify Rhizome version, vault name/path, available indexes, ontology availability, embedding availability, read/write availability, and degraded runtime state.
- Status and capability responses MUST expose a non-blocking index readiness state; `initializing` MUST NOT be represented as an empty successful read.
- Capability responses MUST include hashes or generation identifiers for ontology schema, GraphQL schema, query recipes, indexes, and validation state when the server can compute them cheaply.
- Capability responses MUST include unavailable/degraded reasons, not only booleans, for optional providers and indexes.
- The REST API MUST publish an OpenAPI document for the versioned REST surface.
- The OpenAPI document MUST describe REST routes, auth/CORS expectations, HTTP status codes, error envelopes, request and response payloads, SSE event transport, and the `/api/v1/graphql` HTTP wrapper.
- The OpenAPI document MUST NOT duplicate the full GraphQL object schema as REST-style component models.
- Public docs MUST include examples for browser replacement, custom workflow apps, and local automation.

### GraphQL Reads

- GraphQL MUST be the canonical flexible read surface for Rhizome knowledge objects.
- GraphQL MUST accept author-facing string refs for public locator inputs and expose canonical `NodeRef` metadata for notes, sections, embedded nodes, relation targets, graph nodes, and ontology-backed search results.
- GraphQL MUST let callers choose fields and fragments for nodes, typed lists, relations, graph slices, schema/type metadata, search results, and runtime read helpers. Search mode must be explicit; modes whose backends are not yet wired into GraphQL must return structured availability warnings rather than silently changing behavior.
- GraphQL MUST support at least the read capabilities available through `rzm agent ontology-query` and saved query recipes, including variables, runtime roots, typed roots, bounded selectors, and partial-data warnings.
- Saved query recipes that validate against the live ontology query schema MUST be executable or directly adaptable through the public GraphQL endpoint unless a recipe declares an explicit CLI-only dependency outside GraphQL.
- The API MUST expose query recipe discovery so callers can list recipe ids, descriptions, variables, defaults, result expectations, and execution surface (`graphql`, `http-safe`, or `cli-only`).
- GraphQL MUST preserve existing bounded-root behavior so callers cannot accidentally enumerate unbounded vault state.
- GraphQL list roots MUST use consistent pagination and explicit limits, including default limits, maximum limits, cursor/page metadata, and truncation warnings. The general `notes(...)` root MUST return a connection with `nodes`, `pageInfo`, and warnings rather than duplicating list and connection variants.
- GraphQL MUST support efficient batch reads by string refs without forcing callers into repeated single-node queries.
- GraphQL MUST expose ref resolution for author-facing locators so callers can normalize paths, wikilinks, aliases, fragments, copied URLs, and typed identifiers before cache keying or traversal.
- GraphQL MUST expose validation state as read data that can be selected alongside nodes, types, search results, and workflow views.
- GraphQL MUST return structured partial-data errors and warnings instead of widening silently when a provider, index, root, or relation is unavailable.
- GraphQL MUST support standard schema introspection for local-first development unless explicitly disabled by a future security policy.
- GraphQL MUST expose the generated SDL through a cheap schema-fetching route or equivalent developer-facing capability.
- GraphQL introspection MUST reflect the current runtime vault schema, including ontology-derived types and Rhizome built-in runtime roots.
- GraphQL schema descriptions MUST preserve ontology descriptions, field guidance, enum value meaning, argument bounds, and deprecation metadata wherever the source schema provides them.

### REST Operations

- REST MUST own named workflows and protocol-like operations: status, capabilities, validation, events, query recipes, and other future lifecycle resources.
- REST MUST expose query recipe catalog and execution resources for saved recipes as named workflow assets.
- REST SHOULD own opinionated product packets whose shape is intentionally fixed, such as answer-shaped search or validation reports.
- REST MUST NOT mirror every graph concept when GraphQL already provides the durable read contract.
- Edit-session writes MUST remain reviewable through create, stage, preview, diff, commit, and delete lifecycle operations when exposed on the public write surface.
- Event streams MUST be documented as invalidation/freshness signals and SHOULD support enough metadata for callers to refetch affected nodes or views.
- Event streams MUST define a general vocabulary for node, schema, query recipe, index, validation, capability, and edit-session invalidation.
- Event streams MUST define reconnect behavior. If replay is not available, clients MUST be instructed to refetch status/capabilities and any active views after reconnect.
- Event payloads MUST include event id and kind; event data SHOULD include affected refs/paths/types/queries, relevant versions/hashes, and refetch hints when known and cheap.

### Error and Safety Model

- REST error bodies and GraphQL `errors[].extensions` MUST share stable error codes.
- The initial public error-code taxonomy MUST cover bad requests, GraphQL validation failures, method-not-allowed, not found, query-recipe compile failures, schema unavailable, streaming unsupported, index initialization, and internal failure.
- `INDEX_INITIALIZING` MUST be paired with HTTP 503 and `Retry-After` on gated index-dependent reads; clients MAY retry after the advertised delay and SHOULD use status/events to decide when to refresh.
- Public API docs MUST explain which errors are retryable, which should degrade UI, which need user action, and which indicate a bug.

### Frontend and Custom App Experience

- The built-in frontend SHOULD migrate core non-agent reads and workflows to the public v1 API.
- The built-in frontend MUST keep any agent chat/session calls behind an internal API client boundary so public API dogfooding does not accidentally freeze agent implementation details.
- Browser-specific adapters MUST sit above GraphQL/REST contracts rather than defining the contracts.
- Custom apps SHOULD be able to build useful typed listings, readiness dashboards, graph panes, and handoff views with no private imports.
- The API SHOULD include generated client types or schema artifacts for TypeScript consumers.
- REST client types SHOULD come from OpenAPI, while GraphQL query types SHOULD come from GraphQL SDL/introspection tooling.

### Safety and Scope

- The first API slice SHOULD default to local-first trusted use.
- The API MUST NOT expose arbitrary shell execution, raw filesystem traversal outside the vault, or unreviewed ontology/content mutation.
- The API SHOULD make future authentication and CORS policy possible without changing core resource identities.
- The first API slice MUST document the local trusted default, default localhost bind posture, the default CORS posture, and the intended future token-header shape even if token authentication is deferred.
- The API MUST make unavailable optional providers explicit rather than hiding missing embeddings, stale indexes, or absent ontology schema.

## Decisions

- Answer-shaped REST search is deferred from the first v1 slice. GraphQL `search(...)` is the composable public read shape; an opinionated REST search packet should be specified only when its product payload is fixed.
- Local token authentication is deferred from the first v1 slice. The public docs reserve the future `Authorization: Bearer <token>` shape while the current API remains local-first and localhost-oriented.
- Custom app examples live in `docs/api/` for the first slice. Starter-template propagation should wait until the v1 API shape survives more frontend and custom-app use.
