## Shared agent service composition

`Compose` constructs the shared agentapi configuration from explicit application dependencies. CLI, runtime operation, Codex, and web callers retain request preparation and resource lifetime ownership.

The supplied runtime snapshot carries the vault definition and resolved repository configuration captured together. A caller takes one snapshot per operation. Request options carry cache policy, budget, suppression, read-write authority, and indexed-read/fallback policy. An optional live capability view retains phase-specific waits and fresh capability access.

Composition initializes no runtime, format provider, cache, store, or background work. Built-in note formats and their exact metadata indexer are composed explicitly by bootstrap before application work starts. Runtime-free callers supply their configuration without initializing these resources.
