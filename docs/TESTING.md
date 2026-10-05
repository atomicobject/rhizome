# Semantic testing notes

- Semantic tests use a deterministic fake provider (`--provider test`), so no network or API keys are needed. Embeddings are hash-based and stable.
- Semantic tests create purpose-specific temporary vault data and SQLite indexes.
- CLI tests exercise `semantic index/search/status/enable/disable/refresh/rebuild` with the test provider; MCP tests call `semanticQueryMatches` across note and code paths.
- CI runs the Go package and integration tests with `fts5`; semantic tests do not make live embedding calls.
