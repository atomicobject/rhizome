## pkg/vault/config

Low-level config path/value resolution. Local project `.rhizome/config.yml` parsing lives in `pkg/vault/obsidian`; this package owns home config paths and generic `env:` lookups.

- **Entry points**: `CliPath`, `ObsidianFile`, `ResolveValue`
- **Key invariant**: runtime value resolution prefers process env first, then the home config `env:` block; full CLI config parsing still lives in `pkg/vault/obsidian`
