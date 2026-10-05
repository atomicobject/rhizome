---
type: TechnicalSpec
summary: "Defines source-owned primary semantic chunks and canonical NodeRef metadata as Rhizome's type-aware retrieval contract."
id: SPEC-0032
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0032
  - Primary semantic chunks and NodeRef search
  - NodeRef search
---

# Primary semantic chunks and NodeRef search

## Summary

Rhizome indexes one primary semantic stream per canonical code or ontology owner. Primary chunks combine authored/source content with compact factual identity and context. They are normal ranked evidence, not generated summaries or parallel card records.

`NodeRef` remains the canonical identity for notes, sections, and embedded nodes. Search preserves resolved NodeRef metadata so retrieval, ontology queries, graph traversal, browser navigation, and later edit flows address the same source node.

This spec extends [[Search - Answer engine packets]] and should be read with [[semantic-code-index-spine]], [[indexing-pipeline-architecture]], [[structural-node-model-and-ontology-read-path]], and [[ontology-design-principles]].

## Goals

- improve semantic recall without creating separate generated retrieval objects
- keep every semantic hit source-owned and directly navigable
- make embedded nodes understandable through bounded parent/ancestor context
- enrich code chunks with deterministic identity, surface, and named signals
- preserve canonical NodeRef metadata throughout search and answer assembly
- keep embedding work streamable, cheap, deterministic, and incrementally invalidated
- route target-shaped questions to specialized structural evidence instead of embedding generic task vocabulary

## Non-Goals

- generated answer cards, retrieval-card registries, or card-specific persistence
- generated prose or LLM-written summaries
- embedding callers/callees into early code chunks
- replacing specialized symbol, refs, calls, tests, or graph retrieval with vectors
- making every markdown heading a graph-worthy node
- guaranteeing immutable NodeRef identity across arbitrary source rewrites

## User Stories

### US1 - Retrieve an ontology node from one source-owned semantic stream with enough identity and ancestry to understand it
- id:: ^SPEC-0032-US1
- summary:: Retrieve an ontology node from one source-owned semantic stream with enough identity and ancestry to understand it.
- status:: satisfied
effort:: EFF-2026-07-11-16-30

#### Acceptance Criteria

- Every indexed ontology node emits only `node_body` primary chunks; no separate generated card chunk, registry row, or card embedding is required. ^SPEC-0032-US1-AC1
- Every primary chunk includes compact node kind, type, title, path, canonical NodeRef, and bounded root-to-parent context; the first chunk also includes selected scalar/enum identity fields. ^SPEC-0032-US1-AC2
- A bodyless organizing node still emits one identity/context primary chunk, while authored body content remains dominant whenever present. ^SPEC-0032-US1-AC3
- Parent, ancestor, or selected identity-field changes invalidate affected node chunks deterministically; unchanged source/context/provider fingerprints do not trigger embedding calls. ^SPEC-0032-US1-AC4
- Ranked and answer-shaped search results preserve canonical NodeRef metadata and point to the authored node rather than a generated intermediary. ^SPEC-0032-US1-AC5

## Requirements

### Primary semantic chunks

- Each semantic owner MUST have one primary chunk family. Multiple bounded chunks MAY represent a long owner body, but generated parallel summaries MUST NOT compete with their source.
- Chunk identity MUST be deterministic from canonical owner identity, ordinal, format version, source/context fingerprints, and provider/model fingerprint.
- Authored prose, doc comments, signatures, and source excerpts MUST remain higher priority than generated factual enrichment under size pressure.
- Enrichment MUST be factual, deterministic, and bounded. It MUST NOT add generic `Useful for`, testing, refactoring, error-handling, or other task vocabulary merely to attract queries.
- Code primary chunks MAY include identity, signature, related authored-doc titles, meaningful named literals, and deterministic boundary/surface labels.
- Code primary chunks MUST remain independent of call-graph completion so semantic indexing can stream before graph rebuild.
- Ontology primary chunks MUST include compact identity and ancestry. The first chunk MAY include selected scalar/enum fields; relation and body-sized fields MUST be excluded.
- Continuation chunks MUST repeat only the compact identity/ancestry necessary to remain meaningful independently.
- Bodyless ontology nodes MUST remain retrievable through one identity/context chunk.

### NodeRef search contract

- Search inputs MUST accept author-facing locators and canonical NodeRef-shaped seeds.
- Author-facing node locators MUST resolve to canonical NodeRef before node-scoped traversal.
- Ontology-backed results MUST preserve note path, node kind, type, fragment/node id, parent identity, structural fingerprint, and source range when available.
- NodeRef metadata MUST survive ranking, merge, answer-role selection, packing, and JSON presentation.
- Existing ranking handles MAY remain merge keys, but must not discard NodeRef provenance.
- Typed semantic surveys MUST aggregate per ontology node across that node's primary body chunks and query terms.

### Task-shaped retrieval

- Exact symbol, caller/callee, refactor-impact, test, and configuration questions SHOULD activate specialized symbol/ref/call/test/config evidence lanes.
- Target-required precision modes MUST resolve a bounded target or return ambiguity/unresolved diagnostics; they MUST NOT hide uncertainty behind broad semantic fallback.
- Vector retrieval SHOULD locate the named concept or owner. Structural lanes SHOULD answer relationship questions about that owner.
- Independent vector, lexical, symbol, refs, graph, and ontology-relation branches SHOULD remain concurrently executable under caller deadlines.

### Indexing and migration

- Indexing MUST plan, embed, write, prune, and report primary code and ontology chunks without a generated-card manifest or second card pass.
- Format-version changes MUST force compatible primary-chunk refresh without requiring source reparsing when source indexes remain valid.
- Upgrade migration MUST remove obsolete generated-card rows, chunks, embeddings, sidecar state, and tables while preserving ordinary source-owned chunks.
- A semantic rebuild MUST converge fresh and upgraded databases to the same primary chunk set.

## Verification

- Compare the search-quality corpus before/after removal using recall@5/10, MRR, required-source coverage, latency, provider texts/batches, vector count, and database size.
- Unit tests cover enrichment ordering/budgets, ancestry/field caps, bodyless nodes, invalidation, and canonical NodeRef propagation.
- Unit and integration tests collectively prove parent-aware embedded-node retrieval, NodeRef navigation, target-shaped test/refactor/config routing, and absence of generated-card state.
- Active docs, schemas, templates, and advertised agent surfaces contain no generated answer-card contract.

## Open Questions

- which factual code signals provide measurable unique-source recall without reducing precision
- whether NodeRef should eventually become the primary ranking merge identity
