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
- **Ontology execution**: `ComputeDerived` owns bounded slice scheduling and retains successful slices after a sibling fails. Direct-provider slices make one gated physical call; compatible shared nodes retain their own batching. Code writeback producers submit batched items through the queue and drain before freshness publication.

### Deep docs

- [[Search - Semantic embedding sync invariants]]
- [[semantic-runtime-lane-policy]]
- [[Embeddings - indexing pipeline]]

## SPEC-0133 boundary, last verified 2026-11-06

**Structural source ownership (SPEC-0133).** Providers emit schema-free owner/value trees; ontology prepares each captured host once and alone selects catalog/search owners. RECORD semantic chunks include selected scalar fields and own unbound scalar leaves, excluding selected descendants and sibling evidence. Empty/null/container presence is persisted without Markdown normalization. A RECORD host change forces rebinding of the declared RECORD source set, including previously unresolved references; full/indexed refreshes use the existing atomic publication. Materialization version 19 invalidates prior derived state.

Native chunk planning covers every selected RECORD, including globally selected children without a parent contains field. Such children use the prepared semantic parent; their evidence remains excluded from the parent chunk. Chunk preparation applies the hosts' indexed record context in one batched catalog read, and touched RECORD hosts expand to their whole source set, so positional titles match the catalog in full and incremental runs.
