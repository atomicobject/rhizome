---
type: ReferenceDoc
summary: "Concise current-state map of the intel spine: canonical tables, update paths, and query helpers."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Intel Spine - Current State.md
last-verified: 2026-04-12
status: active
---

# Intel Spine - Current State

## Summary

The intel spine keeps canonical code anchors, doc sections, ontology nodes, chunks, embeddings, and typed edges in the unified SQLite index.

## Current durable shape

- `intel_code_anchors`
- `intel_doc_sections`
- `ontology_nodes`
- `intel_edges`
- `intel_chunks`
- `intel_embeddings`
- `ontology_node_embedding_state`
- `intel_fts`

Ontology node chunks use `owner_type=ontology_node` in `intel_chunks`. Their vectors still live in `intel_embeddings`, but schema-sensitive invalidation metadata is sidecarred in `ontology_node_embedding_state` so code and untyped note chunks do not carry ontology-only columns.

## Durable edge kinds

SPEC-0011 freezes `defines`, `mentions`, `calls`, and `tests` as the durable *minimum*. The live `EdgeKinds` registry (`pkg/anchors/edge_kinds.go`) is a superset; consumers should treat the registry as authoritative for weights/priorities.

- Doc-domain (weight 1.0): `wikilink`, `coderef`, `mentions`
- Code-domain (PPR-weighted, best-effort): `calls`, `type_ref`, `member_ref`, `imports`, `tests`
- Structural (no PPR weight): `defines` (module anchor → symbol anchors)

## Related

- [[semantic-code-index-spine]]
- [[Code Intel - Data model (anchors, sections, edges)]]
