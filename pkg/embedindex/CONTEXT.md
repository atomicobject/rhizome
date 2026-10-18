## Embedding indexing

Owns note, code, and ontology chunk planning, hash-addressed reuse, provider execution, compatible runtime lanes, and batched writeback producers.

- **Entry points**: Syncer, NoteSyncer, OntologyNodeSyncer, prepared pipelines, NewRuntime, Runtime.EnsureLane, and PolicyFor.
- **Writer contract**: application composition supplies one queued writer. Producers batch before submitting and drain explicit barriers before freshness publication. The concrete writer does not import embedindex.
- **Reuse and retries**: chunk hashes authorize reuse. Changed hashes retire current-vector metadata and sidecars transactionally. Provider failure leaves the current hash eligible for retry; reversion may use a retained hash-addressed vector. Old stranded publications require rzm index --rebuild.
- **Lane compatibility**: provider/model/endpoint/dimensions identify shared nodes. Provider defaults and runtime policy control packing and concurrency; persisted vault config supplies no lane caps. Node sharing changes execution, while source ownership and writeback attribution remain explicit.
- **Provider lifetime**: lanes borrow providers. The application factory closes optional provider caches only after nodes drain, including distinct compatible providers sharing a node.
- **Note planning**: source loading and metadata batching have separate helpers. Metadata acknowledgements also select the planning context so a failed earlier batch cannot strand queued notes. Planning joins workers and metadata batching, then flushes the queued metadata barrier before returning. SourceHighWater controls skip decisions; LastSync is diagnostic.
- **Content ownership**: ontology partitions source body parts, anchors extracts source spans, and markdownsyntax owns rune-overlap splitting. Indexing and query share persisted labels through the embeddings leaf. Query packages do not import embedindex.
- **Diagnostics**: preparation records planned/reused work; ontology embedded counts advance after provider success. Compute performs no durable writes. Chunk, vector, and state publication keep owning phases.

### Deep docs

- [[Search - Semantic embedding sync invariants]]
- [[semantic-runtime-lane-policy]]
- [[Embeddings - indexing pipeline]]
