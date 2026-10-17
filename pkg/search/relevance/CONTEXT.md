## pkg/search/relevance

Optional ranking stages add persisted graph evidence, query specificity, locality, and final cross-encoder reranking. The pure weighted scorer and owner diversity live in `pkg/search/weighted_ranker.go`.

- **Entry points**: `SpecificityRanker.Rank()`, `GraphDocAdjuster.Adjust()`, `GraphAnchorAdjuster.Adjust()`, `RerankAdjuster.Adjust()`
- **Key invariant**: same-handle candidates merge evidence before final ranking; repeated evidence facts must not double-vote one handle.
- **Key invariant**: `query_specificity` is deterministic metadata overlap, not an embedding call or LLM reranker.

### Deep docs

- [[Search (Hub)]]
- [[Search - Intent and weight tuning]]
- [[Search - Answer engine packets]]
