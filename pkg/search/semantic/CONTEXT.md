## Semantic embeddings spine

Queries persisted chunk embeddings for retrieval and similar-note consumers.

- **Entry points**: Searcher, QueryEmbedder, QueryEmbeddingMemo, type surveys, and read-only note retrieval.
- **Boundary**: query execution does not import pkg/embedindex. Index planners, provider lanes, chunk reuse, and writeback producers belong there. Ontology owns source body partitioning, anchors owns code-span extraction, and the embeddings leaf owns persisted chunk labels.
- **Catalog identity**: ontology chunks require current catalog identity. Visible and supplemental root regions query separate pre-limit partitions; owner grouping preserves fair interleaving.
- **Section positions**: ordered section IDs use start-byte and section-ID order with a request-local path cache. A missing section keeps its note-level fallback.
- **Query vectors**: one request memo shares each complete same-text code/note pair across facets and continuation pages. The computing caller owns its context; followers cancel independently and may retry after that caller ends. Failures remain uncached, provider causes survive sibling cancellation, and every returned or snapshotted vector is copied.

### Deep docs

- [[Embeddings (Hub)]]
- [[unified-search-answer-architecture]]
