## pkg/embeddings

Embedding providers, vector contracts, chunking and content-hash caching. Index producers live in `pkg/embedindex`; query execution lives in `pkg/search/semantic`.

- `Provider` supplies `EmbedTexts` and `Dimensions`; `NewProviderForConfig` resolves configuration with an injected environment.
- `sqlite.OpenNotes` and `sqlite.OpenCode` use one shared engine with typed note/code adapters. Every open declares `ReadExisting` or `WriteIndex` and an owned file or borrowed database.
- Provider, model and dimensions bind an index. Queries never migrate or repair it. Paid cache, independent sync visibility and transactional recovery survive domain rebuilds.

### Deep docs

- [[Embeddings (Hub)]]
- [[stores]]
