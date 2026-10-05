---
type: ReferenceDoc
summary: "In-memory Obsidian vault graph over wikilinks: HITS authority, label-propagation communities, recency cascade, and cross-community bridges."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Graph analysis (wikilinks + communities + authority).md
last-verified: 2026-05-29
status: active
---

# Graph analysis (wikilinks + communities + authority)

The **Obsidian vault graph**: an in-memory, wikilink-only graph computed on demand from note content. Distinct from the persisted unified doc graph (notes+code+calls) — see [[Indexing pipeline - Graph signals (doc scores + anchor PageRank)]]. Both live under [[Graph (Hub)]].

## What it computes

`ComputeGraphAnalysis` (`pkg/vault/obsidian/graph.go`) builds a directed wikilink adjacency from notes, then derives:

- **HITS hub/authority** — `computeHITS` delegates to `graphalg.ComputeHITS` (30 L2-normalized iterations). Hub = curates links; authority = referenced.
- **Communities** — `labelPropagation` delegates to `graphalg.LabelPropagation` (unweighted, undirected view, up to 20 iters, lexical tie-break).
- **Recency** — `applyNeighborRecency` infers freshness from frontmatter/filename dates; cascades up to `recencyPropagationPasses` (2) when `RecencyCascade` is on (default true), each hop staler by `neighborStalenessOffset`.
- **Bridges** — `computeBridges` flags nodes on cross-community edges.
- **Structure** — `stronglyConnectedComponents`, weak components, orphans (`ComputeGraphStats`).

## Invariants

- Pure-graph steps (HITS, label prop) live in `graphalg`; this layer adds vault I/O, recency, bridges, and summaries.
- Empty / heavily filtered graphs are valid inputs.
- Deterministic: ties broken lexically.

## Produced / consumed

- Produced on demand by graph consumers; the unused [[AnalysisCache (backlinks + graph memoization)]] layer has been removed.
- Consumed by `rzm graph stats/clusters/dead-ends`, `rzm orphans` (via `actions.GraphAnalysis`), and `vault_health` (`ComputeGraphStats`).
- Not used for unified ranking — that uses the persisted doc graph.

## Related

- [[Graph (Hub)]]
- [[Indexing pipeline - Graph signals (doc scores + anchor PageRank)]]
- [[AnalysisCache (backlinks + graph memoization)]]
