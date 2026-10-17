## pkg/vault/repoconfig

Typed repository YAML, workflow state, global CLI preferences, vault selection, and resolved subsystem settings. `config.Env` supplies host paths and environment lookup; each `Vault` owns its environment rather than changing process globals.

`Load` reads one local document, and `FromLocal` produces a `Resolved` snapshot for code indexing, note/code embeddings, graph, file context, and budget. `ValidateFor` rejects cross-vault snapshot injection. Runtime configuration changes are explicit reloads; derived access performs no file I/O.

Unknown YAML keys survive typed saves; malformed YAML, trailing documents, and wrong known-field types remain errors. Full-file saves reread and merge current disk state, use two spaces, retain existing modes, and skip byte-identical replacements. Source-preserving publication uses `pkg/fileio`. A load never writes. Only init performs the bounded retired workflow migration.
