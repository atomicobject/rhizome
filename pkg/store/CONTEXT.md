## pkg/store

The unified SQLite store for `.rhizome/db.sqlite`. The stores subsystem owns this package, including code, note metadata, ontology, graph, session, validation, and diagnostic SQL.

- `Store` owns one connection pool and one writer mutex. Every domain write uses its existing `withWrite` or `withWriteTx` boundary. Embedding stores sharing the database receive the same mutex through `SetWriteMu`.
- Domain files separate schema assembly/validation, note metadata, ontology identity/fields, code references and anchors, chunks/vectors, graph reads, sessions, validation publication, and analytics. Cross-domain publications retain their original transaction.
- Consumers own narrow interfaces. Ontology node/field/query-plan DTOs live in `pkg/ontology/readmodel`; code-anchor and code-index contracts remain in `pkg/anchors`.
- Normal opens validate schema authority without scanning database pages. Read-only opens do not migrate, repair, or mutate. `CheckIntegrity` is explicit maintenance.
- `intel_embedding_cache` (Intel v78, backfilled from existing ontology vectors by the same migration and, once per database, from plain-note vectors by the index run when its section limit is the default) is the content-addressed vector cache for ontology bodies and plain-note chunks, keyed by provider, model, exact dimensions, and chunk-text hash. Retirement, rebuilds, and intact-table `ResetDomain` keep its rows; only `PruneEmbeddingCache` removes them, and only unreferenced rows from an earlier chunk-text format.
- `ReadCodeStats` reads existing current or legacy index tables without migration. CLI code owns catalog parsing and output formatting.

See `docs/reference/subsystems/stores.md` for the writer, migration, path, and initialization rules.

## SPEC-0133 boundary, last verified 2026-11-06

**Neutral structural persistence (SPEC-0133).** Intel schemas 79 and 80 persist source presence/value kind, reference member qualifiers, relation occurrence JSON, and composition-dependent RECORD identity context through the normal single-writer transaction. Empty structured containers have a presence row; null does not satisfy exists. Query SQL preserves the existing indexed Markdown exists branch and adds a separately indexed structured-presence branch. Migration/restart tests retain all occurrence evidence; no alternate store or compatibility schema is introduced.
