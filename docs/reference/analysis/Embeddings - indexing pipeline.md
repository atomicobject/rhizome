---
type: ReferenceDoc
summary: "How source-owned code and ontology primary chunks plus compatibility note chunks are planned, embedded through shared semantic lanes, and persisted."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Embeddings - indexing pipeline.md
last-verified: 2026-04-29
status: active
---

# Embeddings - indexing pipeline

## Summary

Embeddings indexing turns note content into persisted vectors through a staged flow: discover notes, upsert note metadata, project ontology nodes, stream source-owned primary chunks, embed changed chunks, and persist vector plus chunk metadata. Code indexing emits one bounded primary stream per module/anchor, enriching authored/source text with factual identity, surface labels, and named signals.

Code embedding synthesis enriches one source-owned anchor/module primary chunk with bounded factual context: related note/spec labels from `doc_links`, deterministic surface labels, meaningful named literals, and compact implementation identity. Call facts stay out of primary chunks so embeddings can stream before call-graph rebuild barriers finish; caller/callee and refactor questions use structural lanes. Notes use source-owned ontology-node chunks as their primary semantic surface: typed notes use concrete projections with bounded NodeRef identity, selected scalar/enum fields, ancestor context, and authored body, while genuinely untyped notes use an internal fallback projection. Raw authored-section embeddings remain ontology-unavailable compatibility only.

## Core invariants

- chunk updates are incremental and hash-based
- code primary chunks use source-owned anchor IDs and bounded enrichment; no extra generated chunk ordinal is emitted
- ontology-node embeddings use `owner_type=ontology_node` `node_body` chunks for typed note roots, section nodes, embedded nodes, and internal fallback untyped note roots; headers carry bounded identity/ancestry while authored body remains dominant
- raw authored-section embeddings use `owner_type=doc_section` only when ontology is unavailable; ontology-ready paths are explicitly pruned from raw note/vector state after ontology assessment and semantic node planning
- ontology body chunks use source-preserving body blocks as split hints: prose and inline fields stay with their node, while child section/embedded-node text belongs to child node chunks
- oversized ontology body chunks split on markdown paragraph/code-fence boundaries before falling back to byte overlap
- ontology-node re-embedding is gated by sidecar state: chunk text hash, source content hash, node structure fingerprint, provider/model, and a relevant schema signature
- ontology-node embedding batches share the normal note-embedding provider concurrency and global gate; schema invalidation should broaden projection only as needed while preserving hash-stable embedding skips
- shared semantic runtime lanes are the only indexing-time provider path for compatible code, note, ontology primary-chunk, and intent exemplar work; compatible lanes may be promoted for burst throughput, but temporary intent-only/provider-only lanes are forbidden
- note last-sync markers are written only after streamed ontology primary-chunk writes have drained successfully, so an interrupted final flush does not make the next run skip unfinished semantic work
- ontology nodes use only source-owned `node_body` chunks; no parallel generated chunk family competes with authored content
- primary-chunk convergence is checked by comparing planned owners and source/context/format/provider fingerprints with persisted chunks and embeddings; semantic rebuild is the broad repair path
- stale chunks are removed instead of left to drift
- ignore rules must shape discovery before expensive provider work happens
- provider throughput settings affect end-to-end runtime more than chunk logic once the note set is large

## Practical takeaway

For large edits or broad churn, one deliberate `rzm index` resync is usually safer and cheaper than relying on many small incremental embedding updates.

## Where the lanes live

- `pkg/app/semanticruntime` owns provider setup, compatible lane construction, packer ceilings, gates, and shared-node execution policy.
- `pkg/app/indexing.RunUnifiedCore` decides which semantic domains run for batch indexing.
- `pkg/search/semantic.OntologyNodeSyncer` renders source-owned ontology primary chunks, embeds changed text through the supplied shared node when available, and writes chunks/embeddings through the writer surface.
- [[primary-semantic-chunks-and-noderef-search]] defines primary chunk identity/context, bounded enrichment, and invalidation.
- `pkg/app/indexing.write_queue` keeps semantic writeback batched before SQLite instead of submitting one durable write per provider task.
- `pkg/search/embeddings/indexer.go` remains the raw-note compatibility path when ontology projection is unavailable.
