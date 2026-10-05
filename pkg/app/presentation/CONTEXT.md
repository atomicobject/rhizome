## pkg/app/presentation

Renders search results into budgeted text. Implements `Packer` interface.

- **Entry points**: `DefaultPacker.Pack`, `AdaptCodeIndex`
- **Key invariant**: priority = 900−rank; FTS body preference for code chunks
- **Boundary**: may do bounded reads for already-ranked results only; retrieval, ranking, and answer role selection live upstream/downstream.

### Deep docs

- [[Search (Hub)]]
- [[unified-search-answer-architecture]]
- [[Search - Answer engine packets]]
