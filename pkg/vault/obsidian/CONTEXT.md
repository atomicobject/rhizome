## pkg/vault/obsidian

Vault definitions, note I/O and discovery, cached link resolution, filesystem link lookup, and authored note mutations. Pure Markdown scanning lives in `pkg/markdownsyntax`; configuration and vault preferences live in `pkg/vault/repoconfig`; graph analysis and backlinks live in `pkg/graph`; health scans live in `pkg/vault/health`; SQLite composition lives in `pkg/indexstore`.

`NotePathCache` retains every collision claimant, gives a unique exact authored path precedence, and preserves canonical suffixes and fragments. Markdown destinations split raw fragments and decode path escapes once. Markdown resolution classifies the decoded authored spelling before cleaning or source joining. URI and drive shaped spellings have only exact registered path authority when cleaning preserves that spelling; protocol-relative authorities are rejected. Source-relative paths may contain a legitimate parent segment, but an escaped root is rejected before basename fallback. The candidate API retains real ambiguity, and consumers never retry rejected Markdown as a generic note reference. `VaultFileIndex` mirrors Obsidian attachment/ignored-file fallback and stays separate from configured note admission.

Rewrite, tag, and property mutations use the same protected syntax spans as scanners. Code is protected; valid frontmatter property links remain visible. Note discovery admits exact system `CONTEXT.md` outside ordinary includes/excludes while retaining containment and infrastructure/ignore boundaries.

`architecture_test.go` prevents dependencies on configuration, graph, health, stores, embeddings, LLM, and application orchestration from returning to this core.
