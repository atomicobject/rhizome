# Shared query execution

`Engine` retains the ontology schema, its compiled executable, and the query dependencies. Construction and execution check the source identity captured by the compiler. Consumers prepare once and pass the prepared query to `Run`.

`Dependencies` reuses `unifiedsearch.Options` for common vault, index, and search providers. It rejects inconsistent stores or vault paths and preserves explicit exact-read callbacks, path ownership, overlays, and custom providers. Missing embeddings leave semantic search unavailable on every consumer.

`runtimeproviders` owns the ontology and code runtime capabilities formerly embedded in the CLI. CLI, MCP, public HTTP, and configured views compose these providers from their existing query ports and optional code-anchor service. Exact-read readiness and provider-current snapshots remain the web runtime's responsibility. Runtime construction and mutation remain outside this package.

Recipe discovery and selection belong to `pkg/ontology/queryrecipe`. Its caller-owned registry caches parsed sources using content digests. Its runner rejects ambiguous target IDs, validates only the selected recipe, ignores unrelated catalog diagnostics during execution, and passes the compiled prepared query directly to the consumer.
