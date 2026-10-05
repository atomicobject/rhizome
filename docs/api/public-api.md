---
summary: Public HTTP API contract for custom Rhizome applications.
id: REF-public-api
status: active
---

# Public API

Rhizome exposes a versioned public API under `/api/v1` for custom applications that need the same vault, ontology, query, validation, and edit-session capabilities as the bundled web UI.

Agent chat/session routes under `/api/agent/*` are internal Rhizome application routes. They are intentionally outside the public API, public OpenAPI, and v1 compatibility contract.

## Startup

Use `GET /api/v1/capabilities` first. It reports the Rhizome version, vault identity, provider/index state, GraphQL endpoint and schema hash, query recipe count, validation/index generations, public SSE vocabulary, edit-session support, and the shared error taxonomy.

The public API is currently localhost-oriented. Default deployments should bind to loopback unless the caller intentionally exposes the server. CORS is not opened broadly by default; future remote clients should expect a token-bearing header such as `Authorization: Bearer <token>` rather than cookie-based trust.

## GraphQL

Use `POST /api/v1/graphql` for general vault queries. The request body is the standard GraphQL HTTP JSON shape:

```json
{
  "query": "query Plans($path: String!) { plan(path: $path) { path title } }",
  "variables": { "path": "docs/specs/example.md" },
  "operationName": "Plans"
}
```

Use `GET /api/v1/graphql/schema` to fetch the vault-enriched SDL. Standard GraphQL introspection is supported through `__schema` and `__type`; clients can use ordinary GraphQL tooling to inspect the ontology-derived type system. The built-in web app also includes a local GraphiQL explorer at `/graphql`, advertised as `graphql.explorerUrl` from capabilities. The schema changes when `.rhizome/ontology/*.graphql` changes, so custom apps should compare the capabilities `graphql.schemaHash` before reusing generated views.

The shared identity surface is `Node`. Notes, ontology sections, embedded ontology nodes, code files, and code symbols all expose `ref`, `nodeId`, `nodeKind`, `path`, `title`, `resolvedType`, and `locator`. `nodeId` and `nodeKind` intentionally avoid the shorter `id` / `kind` names so ontology-authored domain fields keep their original meaning. Use `node(ref:)` when a custom app has a durable reference from a link, query recipe result, CLI result, or saved workflow state:

```graphql
query WorkflowSeed($path: String!) {
  seed: node(ref: $path) {
    nodeKind
    path
    title
    ... on Note {
      resolvedType
    }
  }
  ref: resolve(ref: $path) {
    found
    ref {
      kind
      path
    }
  }
}
```

Ontology-derived roots such as `plan(path:)` remain the right shape for typed detail/list views. The general-purpose `notes(...)` root returns a connection with `nodes`, `pageInfo`, and warnings. `node(ref:)` is the cross-surface bridge: the same string ref contract should round-trip through the web API, CLI JSON, query recipe outputs, and front-end route state.

`node(ref:)`, `nodes(refs:)`, `note(ref:)`, and `resolve(ref:)` accept author-facing string refs rather than structured ref objects. They use the shared node-read resolver for note and section targets. Supported strings include canonical source locators, normal note paths, Obsidian-style wikilink strings, copied note URLs with `note`, `ref`, `path`, or `file` query parameters, heading fragments, generated/block fragments, and unique frontmatter aliases. If the vault ontology declares a preferred `@identifier`, its authored value is also a public handle for the note or embedded node; for example, a workflow can resolve `SPEC-0042` or `SPEC-0042.US1` without knowing the file path. Code paths continue to resolve as `CodeFile` / `CodeSymbol` nodes after note resolution fails.

Every `Node` can also select `neighborhood(...)` and `localGraph(...)`. Use `neighborhood` for bounded typed relations around one node, with `direction`, `relation`, `type`, `first`, and `truncated` in the payload. Use `localGraph` for node-workspace-style graph views; it returns stable graph node ids, optional `NodeRef` values, endpoint metadata, edge provenance, relation names, and truncation state. `GraphProfile` keeps the default ontology-native and requires explicit opt-in for broader notes/code-aware profiles.

Node detail clients can select `workspace` for source-preserving field and collection metadata, assessment, canonical structural refs, typed relation groups, status/capabilities, and loaded-domain flags. This is intentionally not a pane payload: clients assemble tabs, rails, outlines, and pane stacks locally. The former `/api/ontology/node-workspace` and `/api/ontology/node-workspace/graph` routes are retired; use `POST /api/v1/graphql`.

Use `nodes(refs:)` when a workflow has several saved refs and needs partial success: each item carries `requestedRef` plus either `node` or an `error` warning. Use `notes(...)` when a custom app needs list metadata; it returns `nodes`, `pageInfo`, and warnings. `pageInfo.queryShapeVersion` is currently `v1`, and public roots reject `first` values above the documented maximum instead of silently widening. Use `search(query:, type:, first:)` for ranked note search over the unified engine (lexical, semantic, and structural lanes) with warnings and returned `nodes`; there is no mode argument, and `first` up to 200 is honored. Use `validation(...)` to read the cached validation state from GraphQL when building triage queues; `/api/v2/validate` remains the REST route for the same platform state.

## REST

REST stays focused on platform and workflow resources rather than duplicating the ontology graph:

- `GET /api/v1/status`: server readiness.
- `GET /api/v1/capabilities`: public API discovery.
- `GET /api/v2/validate`: cached run lifecycle and published diagnostic snapshot. `snapshot.issueCodes` contains the complete sorted code vocabulary, independent of loaded diagnostic pages.
- `GET /api/v1/validate`: retired with HTTP 410 and `VALIDATION_API_RETIRED`. Migrate `result.checks` to `snapshot.checks`, issue lists to `/api/v1/validation/diagnostics?generation=<publishedGeneration>`, and browser repair construction to canonical `/api/v1/validation/repair-reviews`. Other v1 endpoints retain their contracts; there is no legacy result hydration or redirect to a different successful response shape.
- `GET /api/v1/files/tree`: bounded vault file tree for explorer-style clients.
- `GET /api/v1/files/view`: raw note/code/file view by vault-relative path.
- `GET /api/v1/files/rendered`: rendered Markdown note view by vault-relative path.
- `GET /api/v1/suggest`: catalog suggestions for command palettes and jump boxes.
- `GET /api/v1/search`: unified semantic/path search.
- `GET /api/v1/search/notes`: note search with type/tag/path filters.
- `GET /api/v1/graphs/global`: global vault graph.
- `GET /api/v1/graphs/local`: local graph around a path or ref.
- `GET /api/v1/graphs/expand`: expanded code module graph.
- `GET /api/v1/ontology/summary`: ontology summary, counts, and type/interface rollups.
- `GET /api/v1/ontology/types`: ontology type summaries.
- `GET /api/v1/ontology/types/{name}`: ontology type detail and example instances.
- `GET /api/v1/ontology/atlas`: ontology atlas for schema overview UIs.
- `GET /api/v1/ontology/inspect`: inspect one note's ontology projection.
- `GET /api/v1/ontology/query-schema`: JSON wrapper for the generated GraphQL SDL.
- `GET /api/v1/query-recipes`: saved query recipe catalog.
- `GET /api/v1/query-recipes/{id}`: saved query recipe detail.
- `POST /api/v1/query-recipes/{id}/execute`: bind inputs, compile, and run a saved GraphQL recipe.
- `GET /api/v1/views`: configured view catalog, including generated type/interface defaults.
- `GET /api/v1/views/{id}`: one normalized configured view.
- `POST /api/v1/views/{id}/execute`: execute a configured table view with search, filters, sort, group, pagination, and optional recipe inputs.
- `GET /api/v1/events`: SSE invalidation stream.
- `GET /api/v1/nodes/preview`: index-backed hover preview for a note or canonical node ref, with optional `from` context for relative links. The response uses ontology `@display` hints and never reads note content. A matched indexed fragment adds `fragment` with `kind` (`heading`, `block`, or `element_id`) and authored `text`; `fragmentResolved` is true when either the node ref or indexed fragment target resolves.
- `GET /api/v1/nodes/events`: SSE stream scoped to one or more resolved node refs.
- `POST /api/v1/edit-sessions`: create a staged ontology edit session.
- `GET /api/v1/edit-sessions/{id}`: read or revalidate edit-session state.
- `DELETE /api/v1/edit-sessions/{id}`: discard an edit session.
- `POST /api/v1/edit-sessions/{id}/stage`: add or replace staged edit operations.
- `POST /api/v1/edit-sessions/{id}/preview`: preview rebase/conflict/diff state.
- `POST /api/v1/edit-sessions/{id}/diff`: read modified-note summaries for UI review queues.
- `POST /api/v1/edit-sessions/{id}/commit`: apply a clean edit session or return conflict state.

Edit-session responses are shared with the built-in UI: every response includes the canonical op list, touched paths/refs, status, conflict details when present, and enough snapshot state for a client to restore a lost in-memory session through preview or commit. Custom apps should use the v1 paths.

Configured views load repo-tracked native configs from `.rhizome/views/*.yaml` and merge runtime-generated defaults for ontology types/interfaces that do not have explicit default mounts. Generated defaults are returned by catalog/list APIs but are not written back to the repo. A minimal execute request looks like:

```json
{
  "search": "backlog",
  "filters": [{ "field": "specStatus", "op": "in", "values": ["draft"] }],
  "sort": [{ "field": "updatedAt", "direction": "desc" }],
  "page": { "offset": 0, "first": 50 },
  "source": { "maxRows": 5000 }
}
```

The response echoes normalized state and returns table columns, rows with canonical `ref`/`path` identity, optional group metadata, page info, warnings, machine-readable source plan metadata, and definition/source/execution fingerprints. Ontology-backed rows can also include `relationValues`, keyed by field name, with each source `value` and an optional resolved target `ref` and `title`. The plan reports pushed/residual constraints, candidate limit/count, cap policy, source completeness, and constraint reliability (`exact`, `cap_bound`, or `not_planned`). Query-recipe-backed views require `source.resultPath` or recipe `outputContract.rowPath`; unresolved or non-array paths return structured warnings instead of falling back to an arbitrary array.

OpenAPI documents the REST wrapper, SSE discovery, query recipe and configured view routes, GraphQL HTTP transport, and shared JSON error envelope. The GraphQL SDL remains the source of truth for ontology query fields.

## Events

`GET /api/v1/events` streams server-sent events. Each data frame is a JSON envelope:

```json
{ "id": "g-1", "kind": "schema.invalidated", "data": { "reason": "filesystem" } }
```

Clients should treat reconnect as a refetch signal. Replay by `Last-Event-ID` is deferred; after reconnect, refetch capabilities, status, schema, active query recipe metadata, validation, and any active view queries. Heartbeats are sent as SSE comments and should be used to detect stale connections.

Public invalidation kinds include node, validation, schema, query recipe, index, capabilities, and edit-session invalidations.

`GET /api/v1/nodes/events?ref=<node-ref>` accepts the same public ref forms as `node(ref:)`, including repeated `ref` parameters and aligned optional `nodeId`, `structural`, and `kind` arrays. It streams node-level `node.updated`, `node.stale`, and `node.deleted` refetch triggers for clients that keep detail panes or focused graph views open. Use the vault-wide stream for query-driven list invalidation; use the node-scoped stream for focused views.

Each node frame carries the subscribed `ref` so clients can match their open panes. When the node's canonical identity has moved — an embedded item without a block ID is addressed by byte offset, so an edit above it renumbers the ref — the frame also carries `canonicalRef`. Clients MUST refetch from `canonicalRef` when it is present; refetching the subscribed `ref` would resolve nothing.

## Errors

REST errors use:

```json
{ "error": "missing required parameter: query", "code": "BAD_REQUEST" }
```

GraphQL validation errors use standard GraphQL `errors[]` entries with `extensions.code`. The public taxonomy currently includes `BAD_REQUEST`, `GRAPHQL_VALIDATION_FAILED`, `METHOD_NOT_ALLOWED`, `NOT_FOUND`, `QUERY_RECIPE_COMPILE_FAILED`, `SCHEMA_UNAVAILABLE`, `STREAMING_UNSUPPORTED`, and `INTERNAL`.
