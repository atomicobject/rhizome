---
type: ReferenceDoc
summary: "Persisted unified-graph ranking signals: per-doc HITS authority/hub + communities, and anchor PageRank over call edges."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md
last-verified: 2026-05-29
status: active
---

# Indexing pipeline - Graph signals (doc scores + anchor PageRank)

Persisted, graph-derived ranking signals over the **unified doc graph** (notes + code, scored from `noderead.GraphFacts`). Distinct from the in-memory vault graph in [[Graph analysis (wikilinks + communities + authority)]]. Depth: [[Graph (Hub)]].

## What it computes

- **Doc scores** — `ComputeDocScores` (`pkg/search/graphdb/doc_scores.go`): HITS hub/authority over **doc-domain edges only** (`calls` excluded), plus `weightedLabelPropagation` communities over the **full undirected** adjacency (calls included), 20 iters. Embedded/section endpoints roll up to their owner note (`graphScoreDocNode`). Persisted to `graph_doc_scores`.
- **Anchor PageRank** — `ComputeAnchorPageRank` (`pkg/search/graphdb/anchor_pagerank.go`): `graphalg.PageRank` (damping 0.85, 30 iters) over caller→callee anchor edges from `intel_edges(kind='calls')`. Persisted to `graph_anchor_scores`.

## Produced

- `compute_graph` phase in `pkg/app/indexing/unified_post.go`: rebuild wikilink edges → `ComputeDocScores`/`ReplaceGraphDocScores` + `ComputeAnchorPageRank`/`ReplaceAnchorScores`. Mirrored for live edits in `pkg/app/bootstrap/schedulers.go`. Phase is gated on notes changing or code being indexed.

## Consumed

- `GraphDocScoreRanker` — `graph_hits_authority`, `graph_hits_hub`, `same_community`, `graph_edge_confidence` evidence.
- `GraphAnchorScoreRanker` — `graph_anchor_pagerank` evidence.
- Query-time PPR (`GraphPPRRetriever`) reads `graph_doc_edges`, not these score tables.

## Contracts

- `ReplaceGraphDocScores` / `ReplaceAnchorScores` do a full `DELETE`+reinsert; edge rebuilds clear stale per-path links (incl. for deletions) before recompute.
- Scores are read-only request inputs once written; structure can return without scores, which backfill asynchronously.

## Related

- [[Graph (Hub)]]
- [[Graph analysis (wikilinks + communities + authority)]]
