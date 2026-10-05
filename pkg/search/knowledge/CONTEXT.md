## pkg/search/knowledge

Handle system for addressing entities in unified search (notes, anchors, chunks, files).

- **Entry point**: `Handle`, `ParseHandle()`, `NoteHandle()`, `AnchorHandle()`
- **Key invariant**: Handle.String() is the canonical identity; Owner() enables diversity limiting
- **Boundary**: keep this package handle/owner focused. Ontology-node metadata belongs to ontology/noderead and is mapped into search candidates at retrieval adapter boundaries.

### Deep docs

- [[Search (Hub)]]
