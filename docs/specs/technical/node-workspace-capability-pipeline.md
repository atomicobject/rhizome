---
type: TechnicalSpec
summary: "Defines the canonical node workspace snapshot, capability pipeline, and server-to-frontend contract for node-centric ontology browsing and editing."
id: SPEC-0019
spec-status: active
last-updated: 2026-08-03
aliases:
  - SPEC-0019
  - Node workspace capability pipeline
---

# Node workspace capability pipeline

## Summary

Rhizome already has a richer ontology node model in the projection and edit-session core than the browser currently exposes. The browser still operates through overlapping note-centric and structural payloads, which makes node-scoped rendering, status, and live updates harder to extend safely.

This spec defines the canonical node workspace contract that should sit between the ontology core and the browser. Its purpose is to make `NodeRef` the durable cross-layer identity, expose node capabilities and status through one extensible snapshot model, and let new browser features grow by extending that snapshot rather than inventing new parallel payload families. The shipped public read shape is the selectable GraphQL `node(ref:) { workspace { ... } }` projection. The frontend adapter derives the browser's pane-specific `NodeWorkspace` graph and convenience views from that public object.

The same identity rule should also govern lightweight ontology read surfaces above persistence. Type summary/detail/example flows should resolve through a shared node-centric read layer that returns canonical `NodeRef` items for both note-root and embedded-node instances, while continuing to use persisted note-path indexes as lower-level accelerators.

## Goals

- make `NodeRef` the canonical browser-facing identity across read, write, and live-update flows
- define one node workspace snapshot shape that can back note, section, and embedded-node panes
- let capability growth propagate through additive snapshot fields instead of new endpoint families
- preserve granular authored-source bindings so field, collection, and content-slice UI can stay tied to provider-authored source ranges and structures
- keep convenience projections such as outlines, structural tabs, and section cards derived from the canonical node workspace model

## Non-Goals

- specifying the transport-specific live-update runtime contract in detail; that lives in the operations spec
- requiring the browser to ship every possible editor affordance in the first pass
- preserving existing note-centric web payloads as long-term primary contracts
- forcing immediate physical storage unification for all node materializations
- replacing existing SQLite note-path indexes; those remain valid storage helpers below the node-centric read layer

## Requirements

### Must

- The server MUST expose a canonical node-scoped read contract that works for file nodes, structural sections, and embedded nodes.
- Every browser-facing node response MUST return a canonical `NodeRef` after resolving any author-facing locator input.
- Author-facing locators such as `note.html`, `note.md`, `note.md#Heading`, and `note.md#^block-id` MAY remain accepted inputs, but they MUST be canonicalized before the server persists edit intent, computes subscriptions, or keys pane state.
- The canonical node workspace snapshot MUST include these top-level concepts:
  - node descriptor and canonical identity
  - format-appropriate content, authored-source, and supported-view projections
  - field-level data and authored binding metadata when the node/provider exposes fields
  - collection-level data, order, and child refs when structural collections exist
  - relation groups scoped to the focused node
  - capability metadata for supported actions
  - node status metadata
  - a version token suitable for refresh and live-update comparison
- The node descriptor MUST preserve enough metadata to identify locator kind, parent containment, note path, authored format/provider, supported views, resolved type, and stable identity fingerprints.
- Field-level payloads, when present, MUST preserve enough metadata to support granular validation, dirty state, and capability-compatible future editor affordances without redefining field identity later.
- Collection-level payloads, when present, MUST preserve enough metadata to support capability-compatible reorder, insertion, deletion, and per-item status surfaces using canonical child refs.
- The browser pane stack MUST key its durable state by canonical node identity rather than raw note path alone.
- Root panes and stacked panes MUST both consume the same node workspace abstraction. A file-root pane is a special case of node workspace, not a separate primary model.
- Outline trees, structural tabs, section cards, and similar UI aids MUST be derived views over provider-supplied structural data in the canonical node workspace snapshot instead of separate identity-bearing payload families; root-only providers omit those views.
- Workspace relation groups MUST preserve the exact target kind and include bounded ordinary connections, backlinks, and code evidence alongside typed structural and ambient relations; when section and embedded structural views exist, they MUST root at the focused subtree rather than the host note outline.
- The server MUST support additive growth of node capabilities and status domains without forcing new top-level endpoint families for each feature.
- Existing note-centric or rendered-section payloads MAY remain as transitional projections, but they MUST be documented as derived compatibility views rather than the long-term canonical architecture.

### Should

- The canonical node workspace API should provide a single-resource read path for one node plus an explicit resolve/batch-resolve path for future multi-pane or prefetch workflows.
- Status payloads should distinguish node-level aggregation from field-level and collection-level detail so the browser can render compact badges or expanded diagnostics from the same source.
- Capability metadata should be declarative enough that the frontend can choose renderer components and affordances from authored format, supported views, and node capabilities rather than hard-coding note-versus-section behavior.
- Node content projections should support both full refresh and scoped refresh semantics, so later live-update work can replace only the affected portions of a pane when practical.
- Session and validation flows should reuse the canonical node workspace vocabulary when they expose dirty, conflicted, rebased, or stale state.

### May

- The browser MAY request supplemental public GraphQL node fields alongside `workspace` when a pane needs format-appropriate rendered, authored-source, or graph detail, as long as canonical node identity and capability/status semantics remain in the shared contract.

### Public API surface

- Public workspace reads MUST use `POST /api/v1/graphql` with `node(ref:)` and its independently selectable `workspace`, content, locator, and graph fields.
- The frontend adapter MUST translate that public GraphQL object into browser-specific pane models; pane stacks, rails, tabs, loaded-domain orchestration, and heterogeneous `nodes[]`/`edges[]` convenience views are not public transport objects.
- The unversioned `GET /api/ontology/node-workspace` and `GET /api/ontology/node-workspace/graph` routes are retired and MUST return the standard unknown-route response.
- Public v1 edit-session routes remain the write and staged-preview workflow. Workspace reads MAY include an edit-session identifier so the GraphQL projection reflects staged state without reintroducing an unversioned read route.
- `Node.workspace` should be documented around these logical shapes:
  - `NodeDescriptor`
  - `NodeSnapshot`
  - optional bounded `NodeBodyProjection` entries with ordered `NodeBodyBlock` values and typed structural binding metadata for providers that advertise structural projection
  - `NodeFieldStatus`
  - `NodeCollectionStatus`
  - `NodeCapabilities`
  - `NodeStatus`
- The browser adapter may derive a canonical heterogeneous pane graph:
  - `focusedNodeId`
  - `nodes[]` with `note | section | embedded | field | collection` payload variants
  - `edges[]` with typed containment, field-binding, collection-item, and relation edges
  - capability-appropriate `views` such as rendered content, authored source, `renderedOutline`, `structuralOutline`, and `relationGroups` as derived convenience projections
- The contract should treat `RenderedSection`, `StructuralNodeResponse`, and the browser's note-scoped `NodeWorkspace` object as adapter projections over public GraphQL node/workspace data, not as peer public models.

### Functional acceptance

- A file-root note from any registered provider can be resolved and rendered through the same node workspace contract as a section or embedded node, with its renderer selected from authored format and view capabilities.
- A section-focused or embedded-node-focused pane can be reconstructed from canonical node identity without depending on a client-side structural sidecar object.
- A new capability or status domain can be added by extending the node workspace snapshot and consuming it in the frontend without creating a second pane model.
- Dirty and validation state can be represented at field, collection, and aggregate node scope from the same node workspace payload family.
