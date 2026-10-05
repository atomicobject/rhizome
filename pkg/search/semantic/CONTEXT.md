## Semantic embeddings spine

Builds deterministic source-owned note/code chunks, syncs vectors into the unified embedding store, and serves vector similarity for `pkg/search/retrieval`.

- **Entry points**: `Searcher`, `ChunkBuilder`, `Syncer`, prepared note/code pipelines.
- **Key invariants**: chunk hashes drive reuse; ontology body chunks are the normal note surface when ontology is available; root-only formats use provider search regions without parsing raw source; visible and supplemental regions are queried in separate pre-limit partitions; source-owned primary chunks remain directly navigable evidence.
- **Section result positions**: hydrate handles from ordered section IDs only, using `(start_byte, section_id)` order and a request-local path cache. A missing section keeps the note-level fallback; content-bearing readers remain available to body consumers.
- **Dispatch**: `MinTexts` is an adaptive floor, provider defaults own unset batch/concurrency caps, and shared nodes own provider gates/metrics.
- **Derived diagnostics**: preparation records reuse and planned content; ontology embedded counters advance only after successful provider computation. Ontology provider work uses `embed_ontology_nodes`, and chunk, vector, and embedding-state publication retain `writeback_ontology_nodes` attribution in both derived and legacy routes. Compute remains free of durable writes.
- **Query vectors**: one request memo shares each successful complete code/note pair across same-text facets and continuation pages. The computing caller owns its context; followers cancel independently and may retry only after that caller ends. Failures remain uncached, provider causes survive sibling cancellation, and every returned or snapshotted vector is copied.
- **Writeback**: batch before queueing, reuse content-hash cache rows, and keep chunk upserts plus stale cleanup on the same durable write lane.
- **Changed-content retries**: chunk publication transactionally retires canonical vector metadata and ontology sidecars when an existing content hash changes. A failed provider call must leave the new hash eligible for retry, and reverting ontology content must not reuse a sidecar for an absent vector. Equal hashes preserve evidence, and code-content reversion can reuse the hash-addressed cache. Evidence already stranded by older publication behavior requires `rzm index --rebuild`.

### Deep docs

- [[Embeddings (Hub)]]
- [[Search - Semantic embedding sync invariants]]
- [[unified-search-answer-architecture]]
