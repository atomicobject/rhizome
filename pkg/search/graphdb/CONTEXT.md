## pkg/search/graphdb

Computes persisted graph scores (HITS, communities, anchor PageRank) from indexed graph inputs.

- **Entry points**: `ComputeDocScores()`, `ComputeAnchorPageRank()`, `UpsertNoteWikilinkEdges()`
- **Key invariant**: doc scores consume ontology-aware `noderead.GraphFacts`; note endpoints must have current persisted provider projections, and embedded/section endpoints roll up to owner notes for the persisted doc-level score table
- **Raw fallback index**: `graph_doc_edges` still stores wikilinks + mentions + coderefs + calls, but is an input to noderead rather than the score builder's private graph model

### Deep docs

- [[Indexing pipeline - Graph signals (doc scores + anchor PageRank)]]
