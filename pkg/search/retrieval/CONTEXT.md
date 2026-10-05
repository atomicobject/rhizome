## pkg/search/retrieval

Individual retriever implementations producing Candidates with Evidence for the ranker.

- **Entry points**: `VectorRetriever`, `GraphRetriever`, `DocLinksRetriever`, etc.
- **Key invariant**: same Handle = same entity; evidence merges across retrievers. Composite and auto-expansion base lanes merge in configured order, so completion timing cannot choose entity metadata. Composite output uses canonical handle order; auto-expansion retains its score order.
- **Provider root lexical evidence**: visible and supplemental search regions use the canonical note handle. Supplemental BM25 weighting is applied in SQLite before the result limit, so scripts and inline data cannot consume the visible result window at full strength.
- **Typo fallback**: only single-token broad queries of at least five characters may match a title within one edit (including adjacent transposition). Multiword queries are never corrected from a partial token match. A close-match notice describes additional title evidence, not a replacement query.
- **Deadline behavior**: broad search treats timed-out nested retrievers as degraded lanes; use `search.RetrieverStageContext` when adding parallel sub-retrievers so one slow lane cannot consume the whole caller deadline.
- **Graph boundary**: graph-aware retrievers may consume `noderead` graph facts/read-model rows for ontology-aware neighborhoods, but must not depend on `pkg/app/web` graph JSON (`GraphNode`, module collapse, typed `nodeRef` payload shape, colors, or click-routing fields). Indexed-only graph sources use fresh derived scores or a persisted-readiness `GraphSnapshot` without claiming live note freshness and never call the live note reader; ordinary callers retain the measured filesystem fallback. Web graph APIs are adapters over canonical graph reads; search owns candidate/evidence semantics only.

### Deep docs

- [[Search (Hub)]]
