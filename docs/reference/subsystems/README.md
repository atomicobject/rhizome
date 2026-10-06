---
summary: "Index of per-subsystem guidance notes: design constraints, must-dos, and review checklists keyed to code folders via code-paths frontmatter."
reference-kind: guide
last-verified: 2026-10-04
tags: [subsystem/index]
---

# Subsystem guidance notes

Start with [overview](overview.md) — the one-page architecture map showing how these subsystems connect along the write path (files → indexes) and read path (indexes → agent output).

One note per major subsystem. Each note carries `code-paths` frontmatter listing the folders it governs, so it can be attached as review guidance (e.g. Greptile) for those folders, and a matching skill under `.agents/skills/<name>-subsystem/` that agents load before working there.

Contract per note: `## Scope` (entry points), `## Design constraints` (hard invariants + why), `## Must-dos when changing this subsystem`, `## Review checklist — problems to catch`, `## Key files`, `## Related docs`.

| Note | Code paths | Skill |
| --- | --- | --- |
| [indexing](indexing.md) | `pkg/app/indexing`, `pkg/app/indexingpipe`, `pkg/vault/indexlock`, `pkg/indexingperf`, `pkg/vault/watchhub` | `indexing-subsystem` |
| [search](search.md) | `pkg/search`, `pkg/app/unifiedsearch`, `pkg/app/semanticops`, `pkg/app/semanticruntime` | `search-subsystem` |
| [ontology](ontology.md) | `pkg/ontology` (core: schema, projection, catalog, sync, idalloc) | `ontology-subsystem` |
| [graphql-query](graphql-query.md) | `pkg/ontology/query`, `pkg/ontology/pushdown`, `pkg/ontology/queryrecipe` | `graphql-query-subsystem` |
| [noderead](noderead.md) | `pkg/ontology/noderead`, `pkg/ontology/readmodel` | `noderead-subsystem` |
| [notemeta](notemeta.md) | `pkg/notemeta` | `notemeta-subsystem` |
| [mcp-server](mcp-server.md) | `pkg/app/mcp`, `pkg/app/agentapi`, `pkg/app/runtimeview` (handlers, catalog dispatch, live capability view) | `mcp-server-subsystem` |
| [agent-surface](agent-surface.md) | `pkg/app/agent`, `pkg/app/agentapi`, `pkg/app/agentcode`, `pkg/app/agentstart`, `pkg/app/contextpack`, `pkg/app/answer`, `pkg/app/presentation` | `agent-surface-subsystem` |
| [cli](cli.md) | `cmd`, `pkg/app/cli` | `cli-subsystem` |
| [vault-core](vault-core.md) | `pkg/fileio`, `pkg/noteformat`, `pkg/vault/notediscovery`, `pkg/vault/obsidian`, `pkg/paths`, `pkg/vault/config`, `pkg/vault/frontmatter`, `pkg/vault/ignore` | `vault-core-subsystem` |
| [stores](stores.md) | `pkg/sqliteutil` | `stores-subsystem` |
| [validate](validate.md) | `pkg/validate`, `pkg/app/validationrun`, `pkg/app/validationproduct` | `validate-subsystem` |
| [code-intel](code-intel.md) | `pkg/anchors`, `pkg/app/codeintel`, `pkg/vault/coderefs`, `pkg/vault/codepatterns` | `code-intel-subsystem` |
| [harness](harness.md) | `pkg/harness` | `harness-subsystem` |
| [vault-runtime](vault-runtime.md) | `pkg/app/runtime`, `pkg/app/bootstrap`, `pkg/app/cli/serve`, `pkg/app/runtimestop` | `vault-runtime-subsystem` |

Each note also carries `code-anchors` glob refs that broadly track its `code-paths`, so the full guidance auto-surfaces in `rzm agent file-context` for files in those folders. The two lists are intentionally not identical: anchors are tuned for retrieval (e.g. a precise file glob, or omitting a package a sibling note already claims), while `code-paths` is the folder contract for review tooling. Query recipe `subsystem-guidance-notes` (and view `reference.subsystem-guidance`) lists these notes by `last-verified` age for staleness review.

**Shared ownership is deliberate, not a copy/paste artifact:**

- `pkg/app/agentapi` appears in both [mcp-server](mcp-server.md) (catalog-backed in-process dispatch) and [agent-surface](agent-surface.md) (the bridge the `rzm agent *` commands call). A diff touching it attaches both notes in folder-scoped review — they are complementary. In `file-context` retrieval the package resolves to mcp-server only (agent-surface's anchors deliberately omit it) so agents get one note, reviewers get both.
- Other cross-cutting concerns are described where they live — [stores](stores.md) owns SQLite write discipline; [indexing](indexing.md) owns pipeline flush/barrier policy.

Maintenance: when a subsystem's invariants change, update its note in the same change set and bump `last-verified`. Prefer symbol/function names over pinned `file.go:line` references in note bodies — line numbers rot.

## Generating the review-tool mapping

The folder→guidance-note mapping for code-review tools is generated from the `code-paths` frontmatter of the notes in this directory — never maintain it by hand. `scripts/subsystem-guidance-map` fails if any note is missing `code-paths` or `summary`.

```bash
scripts/subsystem-guidance-map          # markdown table of (path, note)
scripts/subsystem-guidance-map --json   # JSON array of {path, note, summary}
scripts/subsystem-guidance-map --greptile > .greptile/config.json   # Greptile scoped rules
```

`.greptile/config.json` is the live [Greptile](https://www.greptile.com/docs/code-review/greptile-config) config: one path-scoped rule per note (`scope` = the note's `code-paths` as globs, `rule` points the reviewer at the note's Design constraints + Review checklist). Regenerate it with `--greptile` whenever a note's `code-paths` change; do not edit it by hand. `.greptile/files.json` (hand-maintained, small) adds the architecture overview + this README as always-read review context.
