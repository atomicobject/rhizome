## pkg/app/semanticops

Semantic provider construction shared by indexing and unified search.

- **Entry point**: `PrepareProvider`
- **Key invariant**: command flows live in their owning app packages (`pkg/app/indexing`, `pkg/app/unifiedsearch`); this package does not perform retrieval, indexing, or status rendering.

### Deep docs

- [[Embeddings (Hub)]]
- [[Indexing pipeline (Hub)]]
