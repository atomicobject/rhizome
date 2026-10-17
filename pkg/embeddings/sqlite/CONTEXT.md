## pkg/embeddings/sqlite

One SQLite engine with typed `NoteStore` and `CodeStore` adapters. Existing `emb_*` and `code_*` schemas retain independent migrations, metadata and visible generations.

`OpenNotes` and `OpenCode` require an explicit access mode and exactly one `File` or `Borrowed` source. Read access rejects all mutators and only checks existing vector mirrors; borrowed connections retain their PRAGMAs and ownership. `ValidateExisting` checks configured metadata without writes. The public API exposes no raw database handle.

The engine owns metadata, cache, generation publication, vector codecs and mirrors, connection lifetime, serialized writes and transaction-bound recovery. Domain adapters own typed item/chunk reads, candidate SQL, schemas and legacy row translation. Notes keep the first cached vector per hash; code updates it. Failed recovery rolls back schema and paid data together.

- [[stores]]
- [[Embeddings - SQLite store + locking hazards]]
