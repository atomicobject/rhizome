## pkg/store

The unified SQLite store for `.rhizome/db.sqlite`. The stores subsystem owns this package, including code, note metadata, ontology, graph, session, validation, and diagnostic SQL.

- `Store` owns one connection pool and one writer mutex. Every domain write uses its existing `withWrite` or `withWriteTx` boundary. Embedding stores sharing the database receive the same mutex through `SetWriteMu`.
- Domain files separate schema assembly/validation, note metadata, ontology identity/fields, code references and anchors, chunks/vectors, graph reads, sessions, validation publication, and analytics. Cross-domain publications retain their original transaction.
- Consumers own narrow interfaces. Ontology node/field/query-plan DTOs live in `pkg/ontology/readmodel`; code-anchor and code-index contracts remain in `pkg/anchors`.
- Normal opens validate schema authority without scanning database pages. Read-only opens do not migrate, repair, or mutate. `CheckIntegrity` is explicit maintenance.
- `ReadCodeStats` reads existing current or legacy index tables without migration. CLI code owns catalog parsing and output formatting.

See `docs/reference/subsystems/stores.md` for the writer, migration, path, and initialization rules.
