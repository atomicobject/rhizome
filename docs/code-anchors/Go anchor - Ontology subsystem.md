---
summary: "Code anchors for ontology projection, NodeRef read paths, query execution, embedded-node linking, and primary ontology retrieval."
tags: [type/reference, subsystem/ontology, subsystem/codeanchor]
code-anchors:
  go:
    - label: ontology-load-schema
      symbol: github.com/atomicobject/rhizome/pkg/ontology.LoadSchema
    - label: ontology-node-ref
      symbol: github.com/atomicobject/rhizome/pkg/ontology.NodeRef
    - label: noderead-scope
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope
    - label: noderead-hydrate
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Hydrate
    - label: noderead-type-instances
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.TypeInstances
    - label: noderead-traverse
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Traverse
    - label: noderead-neighborhood
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Neighborhood
    - label: noderead-relation-counts
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.RelationCounts
    - label: noderead-graph
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Graph
    - label: noderead-graph-facts
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.GraphFacts
    - label: noderead-code-graph-links
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.CodeGraphLinks
    - label: noderead-resolve
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Resolve
    - label: noderead-locators
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Locators
    - label: noderead-projection
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Projection
    - label: ontology-query-schema
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.BuildExecutableSchema
    - label: ontology-query-prepare
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.Prepare
    - label: ontology-query-execute
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.Execute
    - label: ontology-query-runtime-schema
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.runtimeRootFieldsSDL
    - label: ontology-query-runtime-execute
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.executor.resolveRuntimeRoot
    - label: ontology-node-catalog
      symbol: github.com/atomicobject/rhizome/pkg/ontology.BuildIntelOntologyNodes
    - label: ontology-scalar-field-assessment
      symbol: github.com/atomicobject/rhizome/pkg/ontology.AssessScalarFieldValues
    - label: ontology-sync
      symbol: github.com/atomicobject/rhizome/pkg/ontology.SyncPaths
    - label: ontology-published-catalog-paths
      symbol: github.com/atomicobject/rhizome/pkg/ontology.PublishedCatalogPaths
    - label: ontology-published-runtime
      symbol: github.com/atomicobject/rhizome/pkg/ontology.PublishedRuntimeWithStore
    - label: ontology-build-index-from-note-sources
      symbol: github.com/atomicobject/rhizome/pkg/ontology.BuildIndexFromNoteSources
    - label: ontology-graph-store
      symbol: github.com/atomicobject/rhizome/pkg/ontology/readmodel.GraphStore
    - label: ontology-graph-sqlite-nodes
      symbol: github.com/atomicobject/rhizome/pkg/anchors/sqlite.Store.GraphOntologyNodes
    - label: ontology-graph-sqlite-edges
      symbol: github.com/atomicobject/rhizome/pkg/anchors/sqlite.Store.GraphOntologyEdges
    - label: ontology-graph-sqlite-docedges
      symbol: github.com/atomicobject/rhizome/pkg/anchors/sqlite.Store.GraphDocEdges
    - label: ontology-web-node-workspace
      symbol: github.com/atomicobject/rhizome/pkg/app/web.Server.nodeWorkspace
    - label: ontology-web-local-graph
      symbol: github.com/atomicobject/rhizome/pkg/app/web.Server.buildLocalGraphRefMode
---

# Go anchor - Ontology subsystem

Ontology code is identity-sensitive. Keep `NodeRef` canonical across projection, query, graph, primary semantic retrieval, and browser paths; do not collapse embedded-node or section refs back to plain note paths after resolution.

## Must-preserve contracts

- `LoadSchema` checks Section ancestry against the complete parsed SDL, so declaration order and type names cannot change relation-target admission.
- `NodeRef` identity must survive projection, GraphQL selection, noderead hydration, and graph output.
- `ontology_nodes` and `ontology_edges` are indexed read-model tables with separate ownership from fallback doc/code graph evidence; preserve the split in [[ontology-indexed-read-model-contract]].
- Structural edges and ambient links are distinct signals; do not merge them in read models or query loaders.
- Full and incremental indexed builds must enter through canonical `NoteSourceSnapshot` inputs and reuse the same parsed `DocumentSnapshot`; persisted metadata cannot replace Markdown source content.
- `PublishedRuntimeWithStore` is a post-barrier read-only gate: it may verify durable metadata and ontology convergence but must not rediscover notes, read sources, project, or repair state.
- `PublishedCatalogPaths` admits selected navigation only when assessment source hash, schema, and catalog witness agree with current metadata, including intentionally empty projections.
- Query execution is read-only over indexed state plus bounded projection. Do not introduce a second live indexing path here.
- `AssessScalarFieldValues` shares indexed field-validation rules with staged typed selections using already-extracted values and authored presence, without source/store reads. Raw workspace values remain available for editing.

## Bound contracts

- `NodeRef` and `ProjectNode` implement [[ontology-browser-workspace#^SPEC-0014-US2-AC1]]: browser-facing code needs canonical node identity, not path-only identity.
- `Scope.Hydrate`, `Scope.TypeInstances`, `Scope.Traverse`, `Scope.Neighborhood`, and `Scope.RelationCounts` implement [[noderef-batch-traversal-read-api#^SPEC-0022-US7-AC2]] while preserving [[noderef-batch-traversal-read-api#^SPEC-0022-US7-AC3]] for content/workspace projection.
- `Scope.Graph`, `Scope.GraphFacts`, and `Scope.CodeGraphLinks` implement [[noderef-batch-traversal-read-api#^SPEC-0022-US8-AC8]]: graph edges carry endpoint refs, relation/provenance, structural marker, and deterministic order.
- `Scope.Graph` profile/debug behavior is reviewed in [[Ontology graph diagnostics and profile review]]; endpoint-scoped reads must not broaden silently when catalog endpoints are missing.
- `BuildIntelOntologyNodes`, `SyncPaths`, `GraphStore`, and `Scope.Graph` implement [[ontology-indexed-read-model-contract]].
- `Scope.Resolve` implements [[ontology-browser-workspace#^SPEC-0014-US3-AC4]]: ontology-backed browser/search results should open by resolved NodeRef or return explicit warnings.
- `BuildExecutableSchema` + `Prepare` implement [[ontology-graphql-query-contract]]: generated roots stay concrete and every broad query has a bound.
- Runtime roots `ontology`, `code`, and `agent` implement [[ontology-graphql-query-contract]]: they are read-only provider adapters, not authored ontology types or arbitrary command execution.
- `query.Execute` implements [[noderef-batch-traversal-read-api#^SPEC-0022-US1-AC1]]: broad relation queries batch through the read layer rather than per-parent edge loops.
- `query.Execute` also implements [[ontology-graphql-query-contract]]: GraphQL results preserve relation provenance, NodeRef locator fields, and partial-data error behavior.

## Read order

- ![[ontology]]
- [[Ontology (Hub)]]
- [[ontology-indexed-read-model-contract]]
- [[ontology-graphql-query-contract]]
- [[ontology-edit-replay-conflict-contract]]
- [[Ontology graph diagnostics and profile review]]
- [[structural-node-model-and-ontology-read-path]]
- [[noderef-batch-traversal-read-api]]

## Package docs

- [[pkg/ontology/CONTEXT]]
- [[pkg/ontology/noderead/CONTEXT]]
- [[pkg/ontology/query/CONTEXT]]
