---
type: TechnicalSpec
summary: "Defines a NodeRef-centric batch traversal and hydration API for performant ontology queries, semantic indexing, search expansion, and HTTP read paths."
id: SPEC-0022
spec-status: proposed
last-updated: 2026-10-04
aliases:
  - SPEC-0022
  - NodeRef batch traversal read API
---

# NodeRef batch traversal read API

## Summary

Rhizome has a strong `NodeRef` identity model for notes, structural sections, and embedded nodes, but the GraphQL ontology query engine still executes mostly through note paths and per-field edge reads. That is adequate for small query shapes, but it does not provide the DataLoader-like performance profile we want for broad ontology queries, semantic indexing, semantic search expansion, or HTTP requests that need to hydrate related nodes across a collection.

This spec defines a high-level `NodeRef` traversal and hydration API that sits below GraphQL, primary semantic synthesis, semantic retrieval, and web endpoints. The API should be collection-first: callers submit many refs plus a small field/traversal plan, and the read layer batches SQLite reads, file/projection work, type checks, edge expansion, and node hydration behind a request- or job-scoped cache.

The API is not only a GraphQL optimization. It is the shared read substrate for node-heavy features that need predictable latency, bounded concurrency, stable ordering, and one identity model across notes, embedded nodes, sections, and semantic retrieval targets.

Related existing specs:

- [Structural node model and ontology read path](structural-node-model-and-ontology-read-path.md)
- [Node workspace capability pipeline](node-workspace-capability-pipeline.md)
- [Linkable embedded node identifiers](linkable-embedded-node-identifiers.md)
- [Rhizome indexing pipeline architecture](indexing-pipeline-architecture.md)
- [Rhizome semantic code index spine](semantic-code-index-spine.md)

## Goals

- make `NodeRef` the canonical identity for high-level ontology read and traversal use cases
- provide a flexible API that can serve GraphQL, HTTP handlers, semantic indexing, semantic retrieval expansion, and future batch jobs
- achieve DataLoader-like performance through request-scoped caching, automatic batching, chunked storage queries, and bounded concurrency
- keep consumers high level: they should request node fields, edge traversals, type filters, and hydration profiles rather than orchestrating note-path SQL and projection work themselves
- preserve stable ordering, per-source limits, and provenance so output is deterministic and explainable
- support both low-latency HTTP requests and larger offline indexing jobs without creating separate read stacks
- let ontology traversal hints, retrieval policies, and `contextInclude` edges drive efficient expansion across many source nodes
- avoid forcing full source-preserving projection when callers only need lightweight summaries, fields, relations, or bounded structural context
- make persisted `ontology_nodes` and `ontology_edges` the fast path for embedded-node identity, type-instance lists, locator summaries, and graph traversal
- present typed ontology edges and untyped note-link fallback through one canonical read facade so web, search, answer, and MCP surfaces do not each reimplement merge semantics

## Non-Goals

- replacing `NodeRef` itself or redefining node identity
- making GraphQL the primary internal execution model
- requiring immediate physical unification of notes, sections, embedded nodes, and edges in SQLite
- replacing the semantic search ranking pipeline or answer-packet assembler
- moving semantic-retrieval projection into live answer assembly
- making SQLite writes concurrent; this spec concerns read-side traversal and hydration
- building an unbounded graph query language
- treating every heading-derived section as a first-class graph node
- requiring all notes to become typed before they participate in graph queries
- using the ontology schema as a substitute for a shared graph read service

## Requirements

### Design Principles

#### Collection-first API

- The API MUST accept collections of `NodeRef` inputs as the normal case.
- Single-node reads MUST be thin wrappers over the batch API.
- Callers MUST be able to request the same traversal or hydration profile for many refs without writing their own loops.
- The API MUST preserve caller grouping when returning related nodes so a consumer can answer "which related nodes came from this source ref?"

#### NodeRef as the identity boundary

- Public read-layer inputs and outputs MUST use canonical `NodeRef` values.
- Author-facing locators MAY be accepted at outer surfaces, but they MUST be resolved to canonical refs before traversal or cache keying.
- Cache keys MUST be based on canonical `NodeRef` plus schema/index generation where needed, not raw user input.
- The API MUST support at least note refs and embedded-node refs.
- Structural-only section refs MAY be supported for direct hydration and subtree-local traversal, but they MUST remain distinct from graph-worthy embedded nodes.

#### High-level plans, low-level batching

- Callers SHOULD describe desired work as a plan:
  - root refs or root query
  - hydration profile
  - selected fields
  - relation specs
  - traversal policy
  - limits and ordering
  - execution budget
- Callers MUST NOT need to know which storage tables or projection helpers satisfy a field.
- The read layer MUST translate high-level plans into batched storage reads, projection reads, and in-memory joins.

#### Performance by default

- The API MUST batch all path/type/metadata/property/tag/assessment/edge reads across the current request or job scope.
- The API MUST memoize hydrated node records by canonical `NodeRef`.
- The API MUST memoize type rows, field payloads, assessments, relation rows, and lightweight projections when the same work is requested repeatedly in one scope.
- The API MUST answer embedded-node identity and summary reads from `ontology_nodes` when persisted rows are fresh enough for the requested profile.
- Embedded type-instance lists MUST use persisted ontology-node catalog rows rather than projecting every typed note.
- Projection MUST remain available for source-preserving workspace/content profiles, but graph/list/identity profiles SHOULD avoid file reads.
- The service SHOULD allow an optional process-wide shared cache to be injected by long-lived runtimes, but request/job-scoped caching MUST remain the baseline and MUST NOT depend on watcher invalidation being present.
- SQLite `IN (...)` queries MUST be chunked below the configured parameter limit.
- Independent read stages SHOULD run with bounded concurrency.
- The API MUST never introduce unbounded goroutine fanout, graph expansion, queue growth, or result materialization.
- The API MUST expose enough counters/timing data to see batch counts, cache hits, chunk counts, projection work, and relation fanout.

#### Stable and explainable output

- Output ordering MUST be deterministic.
- Relation results MUST include provenance: structural vs ambient, relation name, direction, source ref, target ref, target type, and edge evidence when available.
- Graph results MUST preserve typed relation names where available and raw link kinds where a note is untyped or no ontology relation exists.
- Ontology edges MUST be preferred over duplicate note-link fallback edges when both describe the same visible source/target pair.
- Limits MUST be explicit and enforced both globally and per source where the plan asks for per-source traversal.
- Truncated outputs SHOULD include truncation metadata so consumers can explain incomplete neighborhoods.

#### Reuse across runtimes

- The same core API MUST work for:
  - short-lived CLI commands
  - HTTP request handlers
  - long-lived web server requests
  - semantic indexing jobs
  - semantic search retrieval expansion
  - background maintenance or precompute jobs
- Runtime-specific setup MAY provide different cache lifetimes, concurrency caps, and freshness barriers, but the call model SHOULD remain the same.
- The API MUST accept `context.Context` and stop promptly on cancellation or deadline expiry.

### API Shape

The core package SHOULD expose a small service built around one scope object. Names are illustrative; implementation may choose final package and type names.

```go
type NodeReadService struct {
    Store      OntologyReadStore
    NoteReader obsidian.NoteReader
    Schema     *ontology.Schema
    SharedCache SharedNodeCache
    Options    NodeReadOptions
}

type NodeReadOptions struct {
    MaxBatchSize       int
    MaxConcurrency     int
    DefaultNodeLimit   int
    DefaultEdgeLimit   int
    EnableProjection   bool
    EnableObservability bool
}

type SharedNodeCache interface {
    Get(NodeCacheKey) (CachedNode, bool)
    Set(NodeCacheKey, CachedNode, NodeCacheMeta)
    InvalidatePaths(paths []string)
    InvalidateAll(reason string)
}

func (s *NodeReadService) NewScope(ctx context.Context, opts ScopeOptions) *NodeReadScope
```

`NodeReadScope` is the DataLoader-like boundary. It owns request/job caches, batching, cancellation, timings, and instrumentation.
When a shared cache is present, a scope SHOULD consult it only after its own request cache misses and before expensive file/projection work. The shared cache is an optimization seam for web/MCP runtimes, not a correctness dependency for CLI or batch jobs.

```go
type ScopeOptions struct {
    Purpose        string
    Freshness      FreshnessPolicy
    MaxConcurrency int
    MaxNodes       int
    MaxEdges       int
    DeadlineSoft   time.Duration
}

type NodeReadScope struct {
    // private caches, batch queues, metrics, and store handles
}
```

The scope SHOULD expose these high-level reads:

```go
func (s *NodeReadScope) Resolve(ctx context.Context, locators []NodeLocator) (ResolveResult, error)

func (s *NodeReadScope) Hydrate(ctx context.Context, refs []ontology.NodeRef, profile HydrationProfile) (HydrateResult, error)

func (s *NodeReadScope) TypeInstances(ctx context.Context, req TypeInstancesRequest) (TypeInstancesResult, error)

func (s *NodeReadScope) Traverse(ctx context.Context, req TraverseRequest) (TraverseResult, error)

func (s *NodeReadScope) Execute(ctx context.Context, plan NodeReadPlan) (NodeReadResult, error)
```

`Execute` is the highest-level entrypoint. `Resolve`, `Hydrate`, `TypeInstances`, and `Traverse` are reusable building blocks for callers that already have a simpler plan.

### Request Types

```go
type NodeLocator struct {
    Path     string
    Fragment string
    NodeID   string
    Ref      *ontology.NodeRef
}

type HydrationProfile string

const (
    HydrateIdentity HydrationProfile = "identity"
    HydrateSummary  HydrationProfile = "summary"
    HydrateFields   HydrationProfile = "fields"
    HydrateRelations HydrationProfile = "relations"
    HydrateContent   HydrationProfile = "content"
    HydrateWorkspace HydrationProfile = "workspace"
)

type FieldSelection struct {
    Names          []string
    IncludeBuiltin bool
    IncludeBindings bool
    IncludeIssues   bool
}

type TypeInstancesRequest struct {
    TypeName      string
    IncludeIssues bool
    Filter        NodeFilter
    First         int
    After         string
    Order         NodeOrder
}
```

`HydrateWorkspace` MAY use source-preserving projection. Other profiles SHOULD satisfy as much work as possible from persisted metadata, indexed fields, cached snapshots, and lightweight parsing before falling back to full projection.

```go
type TraverseRequest struct {
    Sources       []ontology.NodeRef
    Traversals    []TraversalSpec
    Hydrate       HydrationProfile
    Fields        FieldSelection
    Limits        TraverseLimits
    Budget        TraverseBudget
}

type TraversalSpec struct {
    Name           string
    RelationNames  []string
    Direction      TraversalDirection
    SourceKinds    []ontology.NodeKind
    TargetTypes    []string
    IncludeAmbient bool
    IncludeStructural bool
    Scope          TraversalScope
    MaxDepth       int
    MinStructuralHits int
    Intent         string
    Provenance     []string
}

type TraverseLimits struct {
    FirstPerSource int
    FirstTotal     int
    MaxDepth       int
}
```

Traversal specs SHOULD be able to come from:

- explicit caller request
- ontology field metadata
- `@traversal` policy
- `contextInclude` relations
- primary chunk context plans
- search intent policy

### Result Types

Results SHOULD keep grouped and flat views so callers do not rebuild common shapes.

```go
type NodeReadResult struct {
    Nodes       map[string]NodeView
    Groups      []NodeGroup
    Edges       []NodeEdgeView
    PageInfo    PageInfo
    Diagnostics NodeReadDiagnostics
}

type NodeGroup struct {
    Source ontology.NodeRef
    Name   string
    Items  []NodeGroupItem
    Truncated bool
}

type NodeGroupItem struct {
    Ref   ontology.NodeRef
    Edge  *NodeEdgeView
    Score float64
}

type NodeView struct {
    Ref          ontology.NodeRef
    Title        string
    ResolvedType string
    Summary      string
    Fields       map[string]FieldValue
    Issues       []NodeIssue
    Content      *ContentView
    Bindings     map[string]FieldBindingView
}

type NodeEdgeView struct {
    Source     ontology.NodeRef
    Target     ontology.NodeRef
    Relation   string
    Direction  TraversalDirection
    Structural bool
    Provenance string
    TargetType string
}
```

`NodeReadDiagnostics` MUST be cheap to collect when enabled and safe to omit when callers do not need observability.

```go
type NodeReadDiagnostics struct {
    StoreQueries      int
    StoreQueryBatches map[string]int
    CacheHits         map[string]int
    CacheMisses       map[string]int
    ProjectionCount   int
    EdgeRowsScanned   int
    NodesHydrated     int
    DurationByStage   map[string]time.Duration
    Truncations       []TruncationNotice
    Warnings          []string
}
```

### Internal Loaders

The first implementation SHOULD include request-scoped loaders for:

- canonical locator resolution
- note metadata by path
- note properties by path and property name
- note tags by path
- ontology type rows by path
- ontology assessments by path
- structural edges by source and relation
- ambient edges by source, direction, provenance, relation, and target type
- note content by path
- document snapshots by path
- lightweight node summaries by ref
- source-preserving projections by ref

Each loader MUST have:

- canonical cache key
- `LoadMany` shape
- chunked store access
- duplicate suppression
- stable output order
- cancellation propagation
- observability hooks

Loaders MAY dispatch immediately when called from synchronous code. They do not need to copy Node.js event-loop tick semantics. The required property is automatic collection-level batching at the API boundary and across each plan stage.

### Execution Model

The read layer SHOULD execute plans in stages:

1. Resolve locators and normalize root refs.
2. Load root types and metadata in chunks.
3. Hydrate requested root profile from the cheapest source available.
4. Expand traversal specs depth by depth.
5. Batch all edge reads for the current frontier.
6. Filter targets by type, kind, traversal scope, and caller policy.
7. Hydrate target nodes in batches according to the requested profile.
8. Assemble grouped output with deterministic ordering, limits, diagnostics, and warnings.

Depth expansion MUST be breadth-first by default so all sources at the same depth share edge and hydration batches.

### Concurrency Model

- Read-only store calls that do not require shared mutable state MAY run concurrently.
- File reads, snapshot parsing, and projection work SHOULD use a bounded worker pool.
- SQLite reads SHOULD respect the store's concurrency behavior and avoid a large number of simultaneous queries when one chunked query would be better.
- The API SHOULD default to low but useful concurrency for HTTP requests and allow indexing jobs to opt into higher concurrency.
- Cancellation MUST close work queues promptly and return partial diagnostics when practical.

### Freshness Model

The API MUST let callers pick an explicit freshness policy:

- `FreshnessIndexed`: use persisted index state only; fastest and best for semantic indexing after a correctness barrier
- `FreshnessLive`: use live runtime/cache state when available; best for web and server requests
- `FreshnessEnsureFresh`: require freshness barrier before execution; best for CLI commands that promise current results
- `FreshnessBestEffort`: use what is available and report stale/degraded warnings

Freshness policy MUST be visible in diagnostics when the result may be stale or degraded.

### Consumer Contracts

#### GraphQL ontology query

- GraphQL execution SHOULD compile the selection set into a `NodeReadPlan`.
- Root fields SHOULD resolve to `NodeRef` collections first, then hydrate selected fields.
- Relation fields across a list MUST become one traversal stage per relation spec, not one store query per parent.
- Interface and type filters MUST use batch type lookup and chunked storage reads.
- Query output may remain GraphQL-shaped, but execution should be driven by grouped `NodeRef` results.

#### HTTP APIs

- HTTP handlers SHOULD create one `NodeReadScope` per request.
- Batch endpoints SHOULD accept many locators/refs and one hydration/traversal profile.
- Single-resource endpoints SHOULD call the same scope API with one ref.
- Responses SHOULD include optional diagnostics in debug mode only.
- Web handlers SHOULD reuse live runtime freshness where available and degrade with explicit warnings when live state is not ready.

#### Primary semantic indexing

- Primary semantic indexing SHOULD use one scope per indexing batch or per bounded chunk of nodes.
- Primary semantic indexing SHOULD query node instances in batches and hydrate only the fields and bounded ancestry needed for deterministic chunk text.
- Answer assembly MUST prefer indexed/persisted evidence over live projection.
- Structural relationship claims SHOULD come from grouped traversal results with relation provenance and stable ordering.
- Large indexing jobs MUST set explicit node, edge, and duration budgets.

#### Semantic search and retrieval expansion

- Search retrievers SHOULD use the traversal API when expanding typed seeds through ontology policy.
- Retrieval expansion SHOULD request lightweight node summaries and edge evidence, not full content, unless the ranker or packer needs more detail.
- Intent-specific traversal policy SHOULD map into `TraversalSpec` rather than introducing search-only relation traversal code.

#### Node workspace

- Node workspace SHOULD keep `NodeRef` identity and source-preserving projection semantics.
- It MAY use `HydrateWorkspace` for single-node reads and batch resolve/prefetch endpoints.
- It SHOULD not force all other consumers to pay for workspace projection when summary or relation hydration is sufficient.

### Performance Acceptance Criteria

- A GraphQL query over `N` root nodes and one relation field SHOULD use O(1) relation store batches per depth and relation spec, not O(N) relation queries.
- A query over `N` root nodes with repeated target nodes SHOULD hydrate each canonical target node at most once per scope/profile.
- Type lookup for more than one SQLite parameter chunk MUST split into multiple bounded store reads without failing.
- A request for 200 root notes with two relation fields SHOULD expose diagnostics proving batched edge reads and note hydration.
- Primary chunk synthesis over a large typed collection SHOULD process nodes in bounded chunks without unbounded memory growth.
- Repeated relation traversals in one scope SHOULD show cache hits rather than repeated store calls for identical source/relation keys.
- Cancellation under a request deadline SHOULD stop traversal before starting the next depth or hydration stage.

### Migration Strategy

1. Introduce the `NodeReadService` and scope APIs beside existing query and web code.
2. Add store helpers needed for chunked type lookup and batch edge reads by source/relation/provenance/type.
3. Port note/type/edge/assessment reads in `pkg/ontology/query` to the scope loaders.
4. Add GraphQL regression tests that count store calls for multi-parent relation queries.
5. Add diagnostics and targeted benchmarks for list-plus-relation query shapes.
6. Wire semantic indexing to use traversal plans for context targets and related node summaries.
7. Let web node workspace adopt batch resolve/prefetch where it helps without making workspace projection the default read path.
8. Remove duplicated traversal helpers only after GraphQL, search retrieval, and semantic indexing share the new layer.

### Testing Requirements

- Unit tests MUST cover duplicate source refs, duplicate target refs, aliases, interfaces, embedded-node refs, structural-only section refs, and missing refs.
- Store tests MUST prove chunked `IN` query behavior for large path sets.
- Query tests MUST include call-count spies proving relation reads batch across parent lists.
- Traversal tests MUST cover structural-only, ambient-only, mixed structural/ambient, type-filtered, per-source limited, and depth-limited expansion.
- Cancellation tests MUST prove context cancellation stops queued work.
- Diagnostics tests MUST prove cache hit/miss and batch counters remain stable enough for regression detection.
- Existing `go test ./pkg/ontology/query` MUST stay green during migration.

### Observability Requirements

- Debug diagnostics SHOULD be available to CLI and web callers without changing core result semantics.
- Logs SHOULD quote exact stage names and batch sizes when debug mode is enabled.
- Metrics SHOULD distinguish:
  - root resolution
  - type loads
  - metadata/property/tag hydration
  - assessment loads
  - edge loads
  - snapshot parsing
  - projection
  - output assembly
- Error messages SHOULD identify the canonical source ref, relation spec, and stage when possible.

### Delivery Phases

This spec is intentionally broader than the first extraction slice. Phase 1 establishes the shared scoped read boundary and proves the DataLoader-like shape in production callers. Phase 2 turns that boundary into the general graph traversal and context-loading service that indexers, retrieval expansion, answer assembly, GraphQL, and future HTTP batch APIs can share.

#### Phase 1: Scoped NodeRef Read Foundation

Phase 1 owns the reusable read-scope substrate:

- `NodeReadService` and request/job-scoped `NodeReadScope`
- canonical `NodeRef` projection and hydration caches
- type-instance listing for note-root and embedded node types
- batched type, assessment, record, projection, and relation reads where current callers need them
- GraphQL selection-set compilation into `NodeReadPlan` for note-root field hydration and nested note relation batching
- semantic-retrieval and presentation surfaces using a scope instead of owning projection caches
- optional process-wide cache seam only; no watcher-backed shared cache dependency

Delivered Phase 1 behavior now includes `Scope.Execute` as a real plan executor: callers submit roots, field selections, relation selections, hydration profile, limits/budget, and diagnostics; noderead executes relation work breadth-first by depth, coalesces compatible same-depth batches, hydrates unique targets once per scope/profile, and returns per-call diagnostic deltas. GraphQL uses that plan boundary before projection, while keeping aliases, fragments, nullability/errors, JSON shape, and section rendering in `pkg/ontology/query`.

Phase 1 MAY keep some traversal helpers narrow if they are explicitly called out as temporary. In particular, the current section subtree/neighbor behavior remains GraphQL-local, and the first `Traverse` shape remains note-source oriented even though `Execute` can schedule nested note relation plans. Phase 1 is landable when it removes the current N+1/caching gaps without forcing every node-heavy subsystem onto the final graph API.

Phase 1 SHOULD NOT:

- introduce a broad graph query language
- make indexing depend on one global mutable scope
- replace all projection-based indexer code when the indexer already projects each changed note once
- silently mutate notes for linkability or block-id repair
- add a mandatory process-wide cache that needs watcher invalidation for correctness

#### Phase 2: General Graph Traversal and Context Loading

Phase 2 makes noderead the shared read-side graph access layer. The goal is that any caller with a collection of `NodeRef`s can ask for related nodes, context, edge evidence, or hydration through one bounded, diagnosable plan instead of writing bespoke SQL/projection loops.

Phase 2 SHOULD add first-class APIs beside the Phase 1 primitives rather than overloading the initial narrow `Traverse` method:

```go
func (s *NodeReadScope) Edges(ctx context.Context, req EdgeRequest) (EdgeResult, error)

func (s *NodeReadScope) Neighborhood(ctx context.Context, req NeighborhoodRequest) (NeighborhoodResult, error)

func (s *NodeReadScope) Expand(ctx context.Context, plan ExpansionPlan) (ExpansionResult, error)

func (s *NodeReadScope) HydrateRelated(ctx context.Context, req HydrateRelatedRequest) (HydrateRelatedResult, error)
```

The exact names may change, but Phase 2 MUST preserve these capabilities:

- accept many source refs, including note-root refs and embedded-node refs
- support outbound, inbound, and both-direction traversal
- support structural-only, ambient-only, and mixed structural/ambient edge sets
- support relation-name, provenance, target-type, target-interface, source-kind, target-kind, and traversal-scope filters
- preserve and generalize breadth-first depth expansion across all sources so each depth shares edge and hydration batches
- preserve and generalize enforcement for `FirstPerSource`, `FirstTotal`, `MaxDepth`, `MaxNodes`, `MaxEdges`, and deadline budgets
- return grouped results by original source plus a flat edge/node view for rankers and packers
- retain requested source refs in groups while canonicalizing frontier identity, including supported catalog NodeID-only refs; locator aliases must not collapse distinct source groups
- apply the minimum positive `FirstTotal`, `MaxEdges`, and edge budget to every emitted evidence row, including a shared frontier edge emitted separately for multiple original sources
- report truncation per source and per traversal stage
- include relation provenance and enough edge evidence for semantic retrieval, search context, and diagnostics
- dedupe canonical target refs within a scope while preserving source grouping
- expose deterministic ordering independent of map iteration, goroutine scheduling, or SQLite row coincidence

Phase 2 should treat traversal as a plan execution problem. A caller should describe the desired context:

```go
type ExpansionPlan struct {
    Sources     []ontology.NodeRef
    Steps       []ExpansionStep
    Hydrate     HydrationProfile
    Fields      FieldSelection
    Limits      TraverseLimits
    Budget      TraverseBudget
    Purpose     string
}

type ExpansionStep struct {
    Name              string
    Direction         TraversalDirection
    IncludeStructural bool
    IncludeAmbient    bool
    RelationNames     []string
    Provenance        []string
    TargetTypes       []string
    TargetInterfaces  []string
    SourceKinds       []ontology.NodeKind
    TargetKinds       []ontology.NodeKind
    MaxDepth          int
}
```

Callers SHOULD be able to build plans from:

- explicit API requests
- GraphQL selection sets
- ontology field metadata
- `contextInclude` edges
- semantic context plans
- semantic search retrieval policies
- node-workspace prefetch hints
- indexer context-loading policies

Phase 2 SHOULD also make concurrency an explicit, bounded part of the service contract. `ScopeOptions` and service defaults should grow enough policy for high-concurrency runtimes:

```go
type ScopeOptions struct {
    Purpose        string
    Freshness      FreshnessPolicy
    MaxConcurrency int
    MaxNodes       int
    MaxEdges       int
    MaxBatchSize   int
    DeadlineSoft   time.Duration
}
```

Concurrency rules:

- one scope SHOULD remain the consistency and cache boundary
- HTTP handlers SHOULD use one scope per request
- background indexers SHOULD usually use one scope per worker, shard, or bounded batch rather than one process-global scope
- scopes MUST be safe for concurrent use, but the implementation SHOULD avoid serializing independent work unnecessarily
- projection/file work SHOULD use bounded worker pools
- SQLite reads SHOULD prefer chunked set queries over many concurrent point queries
- cancellation MUST stop queued expansion work before starting the next depth or hydration stage
- diagnostics MUST distinguish useful concurrency from lock contention, query fanout, and cache misses

Phase 2 indexer usage:

- semantic ontology-node indexing MAY keep projecting each changed note once when that is cheaper and already source-local
- indexers SHOULD use noderead when they need cross-note context, relation expansion, semantic context targets, or repeated projection/hydration across a collection
- long-running indexing jobs SHOULD create scopes per bounded worker batch so cache locality improves without unbounded memory growth
- indexers SHOULD pass explicit freshness barriers after note metadata and ontology sync complete
- indexer plans SHOULD set hard node/edge/depth budgets and emit truncation diagnostics rather than materializing large graph neighborhoods accidentally

Phase 2 should unlock these migrations:

- presentation ontology context: move neighbor/contextInclude edge expansion from direct `OntologyEdgesForPath` calls to `Neighborhood`
- semantic context targets: request grouped related nodes with provenance instead of bespoke context-link/projection loops
- semantic retrieval expansion: convert intent-specific typed expansion into `ExpansionPlan`
- GraphQL execution: continue shrinking GraphQL-owned section/subtree reads once section-node traversal is expressible through noderead
- HTTP batch APIs: expose batch resolve/hydrate/neighborhood endpoints backed by the same scope
- node workspace: use batch prefetch for adjacent panes while keeping source-preserving workspace projection for focused nodes

Phase 3 should consolidate graph reads:

- embedded identity and type-instance reads should use the persisted `ontology_nodes` catalog before projection
- graph consumers should request one indexed graph model from noderead instead of joining `ontology_edges` and `graph_doc_edges` themselves
- typed notes, embedded nodes, untyped notes, and code endpoints should share one endpoint/edge envelope
- schema-driven relations should remain the semantic source of truth, but schema should not carry graph assembly policy
- untyped notes should appear as ordinary note endpoints with no `resolvedType`, preserving the base note-link graph as fallback

Phase 2 should not require:

- a watcher-backed process-wide LRU cache for correctness
- rewriting every indexer projection path when the indexed pipeline already has a cheaper source-local projection
- source mutation, embedded block-id fix-up, or linkability repair
- replacing search ranking or answer-packet assembly
- introducing unbounded arbitrary graph query semantics

Phase 3 should not require:

- physical unification of `ontology_edges`, `ontology_nodes`, and `graph_doc_edges` before read-model unification
- rewriting the ontology schema to model every graph-view concern
- forcing every markdown note to validate as a typed note before graph traversal works
- removing projection from node workspace or editing paths that need exact source-preserving bodies

## User Stories

### US1 - GraphQL list relation batching
- id:: ^SPEC-0022-US1
- summary:: Relation fields across a list batch automatically so broad queries remain fast and predictable.
- status:: ready

#### Acceptance Criteria

- A query shaped like `first: 200 { relation { title } }` batches edge reads across all source nodes for the relation field. ^SPEC-0022-US1-AC1
- Store-call tests fail on per-parent relation query regressions.
- Repeated target nodes hydrate once per request scope.
- Diagnostics show relation batch count, node hydration count, and cache hit/miss counts.

### US2 - Primary semantic collection traversal
- id:: ^SPEC-0022-US2
- summary:: Primary semantic indexing resolves bounded ancestor/context data across many NodeRefs without N+1 projection work.
- status:: ready

#### Acceptance Criteria

- Primary semantic indexing can request node records and bounded ancestor context through one scoped API.
- Traversal uses explicit per-source and total limits.
- Output preserves canonical NodeRef and ancestor provenance suitable for deterministic primary chunks.
- Large collections can be processed in bounded chunks.

### US3 - Shared HTTP and batch read API
- id:: ^SPEC-0022-US3
- summary:: The same NodeRef read API supports single-node requests and batch/prefetch requests with explicit freshness and budget controls.
- status:: ready

#### Acceptance Criteria

- HTTP handlers can create one read scope per request.
- Single-node endpoints call the same API as batch endpoints.
- Freshness policy is explicit and visible in diagnostics when degraded.
- Request cancellation stops pending traversal and hydration work.

### US4 - General graph traversal planning
- id:: ^SPEC-0022-US4
- summary:: Consumers describe multi-source graph expansion as a bounded plan so traversal, edge loading, filtering, hydration, and diagnostics are shared across GraphQL, search, semantic retrieval, HTTP, and indexers.
- status:: draft
- increment:: 2

#### Acceptance Criteria

- Expansion plans accept many source `NodeRef`s and preserve grouped output by original source.
- Traversal supports inbound, outbound, and both directions across structural, ambient, and mixed edge sets.
- Plans enforce per-source, total, depth, node, edge, and deadline budgets with truncation diagnostics.
- Output includes relation provenance, source/target refs, target type, structural/ambient markers, and deterministic ordering.
- Repeated target refs hydrate once per scope while preserving all source group memberships.

### US5 - Indexer-scoped context loading
- id:: ^SPEC-0022-US5
- summary:: Indexing jobs use noderead scopes per worker or bounded batch so large context-loading work is concurrent, cache-efficient, and memory-bounded.
- status:: draft
- increment:: 2

#### Acceptance Criteria

- Indexing callers can create scopes with explicit purpose, freshness, concurrency, node, edge, and deadline budgets.
- Multiple worker scopes can run concurrently without relying on a process-global mutable cache for correctness.
- Context-loading plans can expand `contextInclude`, semantic-retrieval, or search-policy relations across a collection without bespoke per-indexer traversal loops.
- Scope diagnostics expose projection work, store batch counts, cache hits, truncation, and cancellation state for indexing jobs.
- Existing source-local projection paths may remain when they are cheaper than graph expansion and do not duplicate cross-node context loading.

### US6 - Batch neighborhood API
- id:: ^SPEC-0022-US6
- summary:: A batch neighborhood API that loads related nodes by endpoint direction and relation policy so packers, web handlers, and retrieval expansion do not call low-level edge stores directly.
- status:: draft
- increment:: 2

#### Acceptance Criteria

- `Neighborhood` or equivalent API supports structural, ambient, and mixed edge neighborhoods.
- Callers can request source, target, or both endpoint directions for many refs.
- The API supports type/interface filters and returns hydrated summaries when requested.
- Presentation ontology context can migrate from direct `OntologyEdgesForPath` reads to this API.
- HTTP/debug callers can request optional diagnostics without changing normal response shapes.

### US7 - DB-first embedded node reads
- id:: ^SPEC-0022-US7
- summary:: Embedded-node identity, type-instance lists, and lightweight summaries come from the persisted ontology-node catalog so common reads do not re-project markdown files.
- status:: ready
- increment:: 3

#### Acceptance Criteria

- Embedded type-instance listing uses `ontology_nodes` rows for identity, label, type, note path, parent, locator, and issue ownership.
- `Hydrate` with identity or summary profile can satisfy embedded refs from the catalog and indexed field-value rows without file projection when catalog rows are present. ^SPEC-0022-US7-AC2
- Full content/workspace profiles still project from source so editing and source-preserving views remain correct. ^SPEC-0022-US7-AC3
- Diagnostics distinguish catalog hits, projection fallbacks, stale/missing catalog rows, and unsupported refs.
- Tests prove large embedded-type listings do not call `ProjectNode` once per note.

### US8 - Canonical indexed graph reader
- id:: ^SPEC-0022-US8
- summary:: One canonical indexed graph reader that merges ontology edges, embedded nodes, untyped note links, and code edges so callers do not duplicate graph assembly policy.
- status:: ready
- increment:: 3

#### Acceptance Criteria

- A noderead-owned graph API returns endpoints for typed notes, embedded nodes, untyped notes, and code where requested.
- The graph API reads persisted ontology nodes/edges, indexed note/code paths, and document/code graph edges through batched store helpers.
- Graph store helpers return storage-neutral graph read-model rows rather than exposing SQLite row structs across the noderead service boundary.
- Graph callers select explicit profiles such as ontology-native, notes-only, or code-aware instead of composing graph semantics from ad hoc booleans.
- Ontology edges take precedence over duplicate note-link fallback edges for the same visible pair.
- Embedded nodes appear by default with `embeds` containment edges from the nearest visible parent endpoint.
- Plain structural sections stay out of default graph output except as internal bridges needed to locate embedded nodes.
- Edge output includes kind, relation name/label, provenance, structural marker, source/target node refs, and stable deterministic ordering. ^SPEC-0022-US8-AC8
- Graph cache fingerprints include ontology-node and ontology-edge changes.
- Note-link graph algorithms outside the mixed ontology graph reader consume one source-neutral snapshot contract that can be built from live note content or persisted notemeta rows; persisted consumers MUST NOT synthesize Markdown to reuse live algorithms.
- Statistics and analysis use persisted snapshots with producer-parity tests. Fragment-sensitive backlinks remain live-source reads until persisted edge rows retain heading/block fragment text.

### US9 - Shared caller migration
- id:: ^SPEC-0022-US9
- summary:: Web graph endpoints and adjacent retrieval surfaces consume the canonical noderead graph API so ontology graph behavior is consistent across UI, answer, search, and agent reads.
- status:: ready
- increment:: 3

#### Acceptance Criteria

- `/api/graph/*` endpoints call the shared graph reader instead of assembling ontology/doc edges locally; web remains responsible for response adaptation, scoring, module collapse, and styling hints.
- Notes workspace graph clicks can open embedded-node workspaces when node refs or source locators are present, with note fallback for untyped notes.
- Answer/search/MCP traversal plans can opt into the same graph reader where they need mixed typed/untyped neighborhoods.
- Existing untyped-note graph behavior remains covered by tests.
- Existing ontology neighborhood APIs remain stable while graph-specific response shaping moves behind the shared read facade.
- Ontology full builds and incremental synchronization consume the same canonical note-source projector and converge across typed/fallback nodes, renames, and deletes without raw-reader or provider fallback branches.

## Open Questions

- Should `NodeReadService` live in `pkg/ontology/read`, `pkg/ontology/nodequery`, or another package name that does not imply GraphQL ownership?
- Which profiles should be stable public contracts first: identity, summary, fields, relations, or workspace?
- Should traversal specs expose ontology `@traversal` policy directly, or should callers pass intent and let the read layer resolve policy internally?
- How much of section-local traversal should be served from persisted edge rows versus lightweight per-note parsing in the first implementation?
- Should diagnostics be returned as structured fields on public HTTP responses in debug mode, or kept as internal logs and tests until the API settles?
- In Phase 2, should one expansion scope support internal parallelism across independent stages, or should the recommended high-concurrency indexing shape remain many smaller scopes with limited internal parallelism?
- Which context-loading policies should be modeled in ontology SDL versus supplied by search/indexing callers at runtime?
- Should the canonical graph reader live directly in `pkg/ontology/noderead` or in a sibling package that depends on noderead for hydration and identity canonicalization?
- Which graph response profiles should be first-class: UI graph, traversal graph, ranking graph, or one base graph plus projection adapters?
