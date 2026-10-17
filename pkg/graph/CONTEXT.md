## pkg/graph

`GraphSnapshot` is the shared source-neutral graph input for analysis and backlink derivation. Live note entries and persisted note metadata supply the same DTO. Algorithms consume snapshots; provider-specific parsing and SQLite access stay in their adapters.

Live graph-document discovery may broaden configured file-context patterns beyond ordinary note includes. Root containment, infrastructure skips, Git ignores, and `.rhizome/ignore` still apply. `CONTEXT.md` retains the system-note admission rule. Link edges use the canonical `obsidian.NotePathCache` and preserve each fragment/subtype.
