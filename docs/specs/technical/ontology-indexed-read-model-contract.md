---
type: TechnicalSpec
summary: "Defines the indexed ontology read-model contract: table ownership, NodeRef locator invariants, graph profile semantics, stale-index behavior, convergence, and diagnostics for typed graph consumers."
id: SPEC-0040
spec-status: active
last-updated: 2026-08-03
aliases:
  - SPEC-0040
  - ontology-indexed-read-model-contract
---

# Ontology Indexed Read Model Contract

## Summary

The ontology read model is the indexed boundary between format-aware typed and fallback ontology projection and graph/search/browser consumers. `pkg/ontology/sync.go` projects provider-derived note facts into SQLite rows, `pkg/ontology/readmodel` defines the read-facing graph row contract, `pkg/anchors/sqlite` implements those reads over the unified DB, and `pkg/ontology/noderead` owns request/job-scoped merging into graph endpoints and facts.

This spec freezes that contract so future changes preserve endpoint identity, table ownership, edge precedence, profile behavior, convergence rules, and diagnostics. It complements the structural node model, NodeRef traversal API, note/node indexing architecture, format-aware root projection, and indexing pipeline architecture by specifying the persisted indexed read model those higher-level APIs rely on. Those related specs are intentionally named in prose instead of linked here so ambient body-link discovery does not author false `successor` relations.

## Implementation Surface

- `pkg/ontology/node_catalog.go` builds `ontology_nodes` rows plus `ontology_node_field_values` rows and is the source for `node_ref_json`, `source_locator`, locator status, parent IDs, catalog node IDs, and node-scoped typed field normalization.
- `pkg/ontology/sync.go` owns full and incremental convergence for typed assessments, type rows, ontology edges, catalog refresh, and indexed ontology field values.
- `pkg/ontology/readmodel.GraphStore` is the storage interface; `pkg/anchors/sqlite/graph_readmodel*.go` is the SQLite implementation over ontology, doc-link, and code-intel rows.
- `pkg/ontology/noderead.Scope.Graph` and `Scope.GraphFacts` own profile merge, endpoint scoping, edge precedence, graph diagnostics, and the public graph/facts result shapes.
- `pkg/app/web/graph.go` and `pkg/app/web/node_workspace.go` are consumers. They may choose profiles or explicit code-edge flags, but they should not bypass `noderead` for endpoint identity.
- Primary regression surfaces: `pkg/ontology/noderead/scope_test.go`, `pkg/anchors/sqlite/graph_inputs_test.go`, `pkg/app/web/graph_test.go`, and the web integration tests that seed embedded ontology nodes.

## Goals

- make table ownership explicit for ontology catalog rows, ontology relation rows, and fallback document/code graph rows
- preserve canonical `NodeRef` identity through `node_ref_json`, `source_locator`, graph endpoint IDs, and consumer payloads
- distinguish embedded ontology endpoints from parent note endpoints without losing note-level fallback behavior
- define the graph profile matrix for ontology-native, notes-only, code-aware, and call-edge opt-in reads
- define precedence between typed ontology edges and provider-authored document-link fallback edges
- make stale catalog behavior and convergence boundaries visible to maintainers
- require diagnostics that explain skipped nodes, deduped edges, stale/degraded reads, and unresolved endpoints

## Non-Goals

- changing runtime behavior or API shapes as part of this documentation slice
- replacing `NodeRef`, `noderead.Scope`, or existing SQLite tables
- making every structural section graph-visible by default
- requiring live projection for all graph reads
- making code-code graph edges part of default graph profiles
- specifying the full web visualization layout or styling model

## Requirements

### Table Ownership

`ontology_nodes` is the ontology node catalog. Its owner is the ontology projection/sync path: `BuildIntelOntologyNodeReadModelWithOptions`, `SyncPaths`, and the catalog replacement boundaries. Rows are build artifacts derived from provider facts interpreted by ontology, including public typed nodes and internal fallback roots, and are intentionally independent from embeddings so endpoint resolution works when semantic indexing is disabled. Ordinary full indexing uses `ReplaceOntologyNodeReadModel`, which may prune obsolete ontology semantic rows. Validation-only convergence uses `ReplaceOntologyNodeReadModelPreservingSemantic`, which replaces catalog nodes, field values, and link dependencies while preserving ontology chunks, embeddings, and semantic sidecars. Preserved semantic ownership is keyed canonically by `ontology_nodes.node_id` and `intel_chunks.owner_id`, never by transient SQLite row IDs.

`ontology_node_field_values` is the generic node-scoped typed field sidecar for `ontology_nodes`. Its owner is the same ontology projection/sync path, and its replacement unit is the same touched note-path set as the catalog. Field rows are schema-aware execution evidence for predicates, sort, and summary hydration; they are not note-scoped property discovery and do not replace `note_property_values`.

`ontology_edges` is the typed ontology relation table. Its owner is the ontology assessment/sync delta path: `computeOntologyJob`, `buildOntologyDelta`, and `ApplyOntologyDelta`. Structural rows come from schema-authored fields and node-scoped structural edges; ambient rows come from indexed note-link/backlink evidence mapped through ontology neighbor policy.

`graph_doc_edges`, `doc_links`, `intel_edges`, and `intel_code_anchors` are not owned by ontology sync. They are fallback graph/code-doc evidence owned by note metadata, doc-link, and code-intel indexing. The ontology graph read model may read them, but must not treat them as typed ontology truth.

`pkg/ontology/readmodel.GraphStore` is the storage contract boundary for graph reads. `pkg/anchors/sqlite` implements it; `pkg/ontology/noderead` consumes it. Consumers should depend on `noderead` or the readmodel interface rather than reaching into SQLite table details.

### Node Field Value Rows

Each `ontology_node_field_values` row MUST belong to one projected ontology node and one schema field. Rows MUST preserve node identity (`node_id`, note path, type name, field name), authored text, normalized comparable value, value kind, list ordinal, source kind, source locator, schema hash, and updated timestamp.

Scalar field rows MUST normalize by ontology scalar type. Booleans use a boolean column, integers and floats use numeric columns, dates and datetimes use sortable ISO text columns, URLs and IDs keep normalized text, enums normalize case for comparison while preserving authored text, and list fields produce one row per item with stable list ordinals.

Link field rows MUST store the authored link input plus canonical target metadata when resolution succeeds: target note path, target type name, target node ID, target `NodeRef` JSON, and target source locator. Link-field dependency rows MUST track the source note/type/field and resolved target so changing a linked target can refresh dependent source rows precisely.

Field rows MUST be replaced atomically with catalog rows for the touched note-path set. A replacement deletes stale field rows and link dependencies for touched paths, deletes stale catalog nodes for removed node IDs, inserts current nodes, then inserts current field rows and dependencies in the same SQLite write transaction.

`write_ontology` and ontology-body writeback instrumentation SHOULD expose field rows planned and field rows written. These counters are for timing/debug visibility; queue coalescing or batching changes should be added only when measured write timings show they are needed.

### NodeRefJSON And Source Locator Invariants

Each `ontology_nodes` row MUST carry a non-empty `node_ref_json` encoding the canonical `NodeRef` for that projected node. Read paths MUST prefer `node_ref_json` when reconstructing refs, then fall back to row columns only for legacy or damaged rows.

Each `ontology_nodes` row MUST carry a `source_locator` usable as the durable author-facing locator for lookup and display. For note nodes, it is the note path. For embedded or section nodes, it is the note path plus the best available fragment, block ID, node ID, or structural fingerprint.

Catalog and read hydration MUST preserve or resolve authored format and provider provenance from canonical note-source facts. Consumers MUST NOT guess the source format from content, a missing structural projection, or an extension switch outside the provider registry.

Endpoint selector matching MUST support canonical node IDs plus author-facing fragments, block IDs, `source_locator`, and `node_ref_json`-encoded `nodeId`. This lets callers resolve older links, block links, and structured refs to the same endpoint without widening an embedded request into an unrelated note-scoped graph.

`NodeRef.String()` is only an author-facing note-or-fragment locator. It is not a complete cache or graph identity for embedded nodes because it omits kind, type, structural fingerprint, and some node IDs. Cache keys and endpoint IDs must use canonical `NodeRef` identity or catalog `node_id`.

### Embedded Versus Note Endpoint Identity

Graph endpoints MUST keep separate endpoint kinds for notes, embedded nodes, structural sections, and code. The endpoint ID shape is `<kind>:<key>`, where ontology child endpoints use the catalog `node_id` and note endpoints use the note path.

Embedded node requests MUST remain endpoint-scoped when the source ref carries kind, node ID, fragment, or structural fingerprint. If the catalog cannot resolve that endpoint, the read must return an empty/degraded endpoint-scoped result or diagnostic rather than silently expanding to the whole parent note neighborhood.

Parent note endpoints MAY be added alongside embedded endpoints so graph views have containment context. That parent endpoint does not replace the embedded endpoint as the source or target of typed node-scoped relations.

Structural sections MAY appear as explicit endpoints only when directly requested or needed to preserve source identity. They MUST NOT become graph-visible children by default the way embedded ontology nodes do.

### Graph Profile Matrix

`ontology_native` is the typed profile. It includes ontology catalog nodes and typed ontology edges, includes embedded ontology endpoints, excludes untyped doc-link fallback edges, and excludes code endpoints. Use this when callers want the native typed graph without ambient note graph fill-in.

`notes_only` includes embedded ontology endpoints, typed ontology edges, untyped note endpoints, and provider-authored document-link fallback edges. It excludes code endpoints and code-code edges. Use this when callers need the familiar note graph with typed ontology identity preserved.

`code_aware` includes everything in `notes_only` plus note-to-code evidence such as coderef and mentions edges. It still excludes code-code call/import/type/member edges unless the caller explicitly opts into call edges.

`IncludeCodeEdges` is the explicit `GraphRequest` opt-in for code-code graph edges. `GraphFactsRequest.IncludeCalls` maps to that same lower-level read-model flag. Setting either implies code endpoints are included, but code-aware reads without the explicit call/code-edge flag must keep code-code edges out of graph facts and graph visualizations.

### Ontology Edge Versus Authored Document-Link Precedence

Typed ontology edges MUST win over duplicate visible provider-authored document-link fallback edges for the same source and target endpoint pair. Markdown wikilinks/links and HTML anchors are syntax-specific inputs to this common edge model. The duplicate fallback edge should be suppressed and reported in diagnostics when diagnostics are enabled.

Fallback doc-link edges MAY still be included when no typed ontology edge covers the same visible pair and the active profile allows doc edges. Their kind, weight, and confidence remain fallback evidence, not ontology relation semantics.

When ontology relations have embedded endpoints, duplicate detection must compare the visible endpoint IDs after endpoint resolution, not only note paths. A note-level authored link must not erase a node-scoped ontology edge.

### Stale Catalog Behavior

The catalog is indexed state. Graph/list/identity reads should use it as the fast path and report missing or unresolved endpoints when a requested catalog row is absent. Summary hydration may fall back to projection for requested embedded refs, but graph endpoint-scoped reads must not hide missing catalog state by broadening scope.

Incremental ontology sync MUST refresh `ontology_nodes` for touched or catalog-affected paths even when the ontology delta has no assessment/type/edge rows. Catalog-only schema, locator, provider-version, provider-projection, or ownership-transition changes must not leave note roots or embedded endpoints stale.

Incremental ontology sync MUST refresh `ontology_node_field_values` whenever the corresponding catalog rows are refreshed. Deleted paths, removed embedded nodes, changed field values, changed link targets, and changed source locators must prune stale field rows through the shared replacement boundary.

Link-target repair may refresh catalog rows for affected refs after safe fixes are applied. That refresh is a local convergence aid, not a replacement for normal indexing or ontology sync.

Field rows store the producing schema hash per row. Read paths do NOT filter by current schema hash; rows from a stale hash answer queries until the next replacement boundary runs. This keeps reads quiet during a reindex window so callers do not see empty result sets the moment a schema changes. Stale-hash drift is observability data: diagnostics-enabled reads SHOULD expose `schema_hash` mismatches between the current schema and field rows so maintainers can distinguish "no rows match" from "rows match but predate the current schema."

### Rebuild Versus Incremental Convergence

A full ontology rebuild owns replacing assessment, note-state, type, edge, type-policy, schema-state, catalog, and obsolete semantic rows for the schema snapshot. It is required when schema readiness/hash changes or no ready schema state exists.

Incremental sync owns changed paths, deleted paths, provider/projection-version invalidations, ownership transitions, paths affected by note graph edges, and paths affected by existing ontology edges. It must replace source-scoped ontology rows and delete removed path state without disturbing unrelated rows.

Validation-only projection refresh is an incremental catalog convergence mode, not a semantic rebuild. It must replace the affected catalog/field/dependency rows while preserving ontology chunks, embeddings, and sidecars, including across catalog row-ID replacement. A later authoritative full/semantic index remains responsible for pruning or regenerating those semantic rows.

The indexing pipeline remains the correctness barrier for generated ontology graph/index evidence. Consumers may request live/best-effort resolution where supported, but durable graph facts should converge through indexed rows rather than ad hoc consumer projection loops.

### Diagnostics

Graph diagnostics SHOULD report node additions, edge additions, skipped nodes/edges, and deduped fallback edges with enough reason text to explain profile filtering, unresolved endpoint scopes, ontology-vs-doc precedence, and code-edge exclusion.

Read-scope diagnostics SHOULD distinguish catalog hits/misses, projection fallback, edge loads/hits, unsupported refs, truncation, and stale/degraded freshness where the caller asks for observability.

Structural-context and graph diagnostics should use stable strings suitable for indexing, tests, and UI/debug display. Diagnostics are observability data; they must not change the semantic result shape for callers that did not request them, and diagnostic-enabled reads must remain cache-distinct from non-diagnostic reads.

## Open Questions

- Whether `ontology_native` should eventually expose a stricter stale-state warning when schema hash differs from row hash, rather than only relying on catalog miss/skip diagnostics.
- Whether graph profile names should become public API constants in MCP/agent docs once more external clients depend on them directly.
