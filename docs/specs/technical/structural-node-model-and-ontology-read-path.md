---
type: TechnicalSpec
summary: "Defines the ontology read model for structural note browsing: unified node identity, embedded-vs-structural distinction, embedded source spans, and the read-path contract shared by the ontology browser, graph, and future editing surfaces."
id: SPEC-0013
spec-status: active
last-updated: 2026-08-03
aliases:
  - SPEC-0013
  - Structural node model and ontology read path
---

# Structural node model and ontology read path

## Summary

Rhizome's ontology runtime must treat provider-authored structure as first-class read-model data without collapsing every heading into an independent graph node. This contract governs note providers that emit structural projections, initially Markdown. The durable contract is a unified node model that can represent file-backed nodes and embedded body-backed nodes behind one browser, graph, and query abstraction while keeping ordinary structural sections distinct. Root-only providers participate as file-backed roots but do not acquire synthetic sections, embedded nodes, or containment merely from their document syntax.

This spec consolidates the enduring runtime contract behind the structural-node work, the ontology runtime design, and the ontology browser's structural default. It freezes the identity and containment rules that later browser, API, edit-session, and subscription surfaces must build on.

## Goals

- unify file-backed notes and embedded subdocuments behind one node-centric read model
- make canonical node identity the shared contract across read, write, and live-update surfaces
- preserve the distinction between structural-only sections and graph-worthy embedded nodes
- preserve the distinction between a node's embedded role and the markdown source shape that produced it
- let browser, graph, and query surfaces operate on node identity rather than note-only assumptions
- shape the read path so future editing can target either whole-file or embedded nodes without a product redesign

## Non-Goals

- specifying a full in-place editing workflow in this phase
- defining the exact browser payload envelope or SSE transport contract; those live in follow-on specs
- turning every heading-derived section into a first-class graph vertex
- requiring physical table unification before the read model ships
- replacing ordinary markdown rendering outside the ontology workspace

## Requirements

### Must

- The ontology runtime MUST support a unified node abstraction that covers both file-backed note nodes and embedded body-backed nodes.
- Embedded body-backed nodes MUST NOT be assumed to be markdown heading sections. The runtime MUST allow an embedded node's source span to come from a schema-declared source shape such as a section, list item, or checkbox item.
- Canonical node identity MUST be represented by `NodeRef` semantics even when author-facing inputs begin as note paths plus heading or block fragments.
- Browser, API, edit-session, and subscription surfaces MUST canonicalize author-facing locators into stable node identity before persisting, replaying, or broadcasting node-scoped work.
- Structural sections MUST remain distinct from embedded nodes by default.
- Structural sections, embedded nodes, and containment MUST be produced only when the note provider advertises a structural projection; root-only providers MUST remain file-backed roots without synthetic structure.
- A type that is only structural MUST be available for navigation, prose grouping, and validation without automatically becoming a graph node.
- Embedded nodes MUST behave as first-class nodes for focus, relation grouping, graph projection, retrieval, and future editing.
- Embedded nodes MUST preserve source-shape metadata and source byte ranges sufficient for field extraction, linkability, and edit replay without redefining canonical node identity.
- Structural ownership MUST remain distinct from generic linking semantics.
- The ontology read path MUST preserve enough metadata to resolve node identity, locator kind, parent containment, and node-scoped relations.
- The canonical node model MUST preserve enough authored-source metadata to support future field-level, collection-level, and content-slice capabilities without redefining node identity.
- The browser and graph read models MUST be able to focus a specific embedded node rather than collapsing all relations back to the parent file node.
- The runtime MUST preserve enough locator information that future edits can target either a whole file node or an embedded byte-ranged node.
- Author intent via `type:` MAY aid disambiguation, but actual node membership MUST continue to come from schema selectors and containment rules.

### Should

- Embedded nodes should support author-facing deep-link targets that remain compatible with ordinary note workflows where possible.
- Convenience projections such as rendered section trees, structural tab models, or note-scoped workspaces should remain derived views over the canonical node model instead of defining parallel primary identities.
- Root-level convenience projections over embedded nodes should remain derived views rather than duplicating authored source-of-truth placement.
- Lightweight source shapes such as list items and checkbox items should remain contextual embedded nodes inside their owning note rather than being duplicated into aggregate note files.
- Structural and ambient relations should stay separate in the persisted/read model so UI surfaces can explain provenance clearly.
- Schema invalidation and resync behavior should remain incremental and best-effort rather than forcing whole-vault recomputation for minor note edits.

### May

- Some embedded nodes may initially rely on unstable internal identifiers during rollout as long as the runtime keeps their limitations explicit.
- Physical persistence for file-backed and embedded nodes may stay partially separate if the read path still presents one coherent node model.

## Related Specs

- [Non-section embedded node source shapes](non-section-embedded-node-source-shapes.md) defines list-item and checkbox-item embedded nodes as additional source shapes over this node model.
- [Format-aware root ontology projection](format-aware-root-ontology-projection.md) defines typed and fallback root participation for providers, including HTML's root-only v1 boundary.

## Open Questions

- which embedded-node deep-link shape should become the long-term author-facing default for cross-tool stability
- whether any current ontology runtime slices still need separate decision notes once this spec has been backported further
