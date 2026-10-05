## pkg/search/graphalg

Pure graph algorithms for ranking, community detection, path finding, and
surprise scoring. No I/O or persistence.

- **Entry points**: `ComputeHITS()`, `PageRank()`, `LabelPropagation()`,
  `ShortestPath()`, `FindSurprisingConnections()`
- **Key invariant**: algorithms are stateless; they take adjacency maps and return score maps

### Deep docs

- [[Indexing pipeline - Graph signals (doc scores + anchor PageRank)]]
