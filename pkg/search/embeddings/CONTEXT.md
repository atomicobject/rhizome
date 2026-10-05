## pkg/search/embeddings

Embedding providers, indexing, chunking, and SQLite storage for semantic search.

- **Entry points**: `Provider` interface, `Indexer.Index()`, `Store.Search()`
- **Key invariant**: provider dimensions must match index dimensions; chunks are hash-stable for incremental updates

### Deep docs

- [[Embeddings (Hub)]]
