## pkg/search/relevance

Ranking stage: blends evidence from retrievers into final scores, adds direct query-specificity evidence, and enforces diversity via MaxPerOwner.

- **Entry points**: `SpecificityRanker.Rank()`, `FusionRanker.Rank()`, `WeightedRanker.Rank()`
- **Key invariant**: same-handle candidates merge evidence before final ranking; per-retriever ranks must not double-vote one handle.
- **Key invariant**: `query_specificity` is deterministic metadata overlap, not an embedding call or LLM reranker.

### Deep docs

- [[Search (Hub)]]
- [[Search - Intent and weight tuning]]
- [[Search - Answer engine packets]]
