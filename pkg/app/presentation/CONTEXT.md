## pkg/app/presentation

Renders search results into budgeted text. Implements `Packer` interface.

- **Entry points**: `DefaultPacker.Pack`, `AdaptCodeIndex`
- **Key invariant**: priority = 900−rank; FTS body preference for code chunks
- **Ontology context**: `NoteStore` and `NoteFormats` are explicit dependencies when a schema and note reader are supplied. The packer hydrates the canonical `SourceRef.NodeRef`; it does not parse serialized identity or infer a note store from the code reader.
- **Boundary**: may do bounded reads for already-ranked results only; retrieval, ranking, and answer role selection live upstream/downstream.

### Deep docs

- [[Search (Hub)]]
- [[unified-search-answer-architecture]]
- [[Search - Answer engine packets]]
