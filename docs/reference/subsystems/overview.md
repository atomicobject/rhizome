---
summary: "One-page Rhizome architecture overview: how vault content and code flow through indexing into SQLite read models and back out through search, typed queries, and the agent surface. Entry map into the per-subsystem guidance notes."
reference-kind: guide
last-verified: 2026-06-12
tags: [subsystem/overview]
aliases:
  - Rhizome system overview
---

# Rhizome system overview

Rhizome turns a markdown vault + code roots into queryable, typed, token-budgeted context for agents. Every subsystem sits on one of two paths: the **write path** (files → indexes) or the **read path** (indexes → agent output). This page is the map; each box links to the subsystem note that owns its invariants ([README](README.md) has the full table with code paths and skills).

## Write path: files → SQLite

```
vault files + code roots
  │  watchhub (debounced fs events)        ─┐ live refresh
  │  rzm index / MCP-boot background index ─┘ batch truth
  ▼
indexing pipeline  (discovery → ingest → ontology projection → semantic lanes → cards → drain → graph → maintenance)
  │  one queued writer lane; per-kind batch flush; explicit barriers; .rhizome/index.lock
  ▼
SQLite (.rhizome/db.sqlite: WAL, FTS5, sqlite-vec; domain-versioned migrations)
```

- [indexing](indexing.md) — pipeline stages, the single queued writer lane, batching, barriers, the index lock. **Latency-first; new work joins a stage, never a bolt-on pass.**
- [stores](stores.md) — connection/pragma policy, shared per-DB write mutex, retrying write transactions, migrations, FTS5/sqlite-vec. All durable writes funnel here.
- [vault-core](vault-core.md) — what the pipeline ingests: vault discovery/config, link/tag/frontmatter primitives, unified ignore rules, and `pkg/paths` (the **only** path normalizer; indexes store vault-root-relative keys exclusively).
- [code-intel](code-intel.md) — code-side ingestion: code anchors (note→code), coderefs (code→note), pattern globs, per-language indexers.

## Read models the write path maintains

- [notemeta](notemeta.md) — canonical raw-note metadata index + `NoteSourceSnapshot`, the sanctioned raw-note DTO downstream ingesters consume. Freshness via a versioned notes hash; delta sync must equal a fresh rebuild.
- [ontology](ontology.md) — the typed contract: GraphQL SDL compiles to the typed-note model; markdown projects deterministically into span-preserving `NodeRef`s; `SyncPaths` converges `ontology_nodes`/`ontology_edges`/field-value rows through the writer lane. **Markdown is the source of truth; rows and cards are read models.**
- Embedding stores (note + code chunks) maintained by the semantic lanes ([indexing](indexing.md), [search](search.md)).

## Read path: SQLite → agent output

```
SQLite read models
  ▼
noderead (batched typed reads, edit-session overlays)   search (retrieval → ranking → packing)
graphql-query (SDL queries, pushdown, recipes)           │
  ▼                                                      ▼
agent surface: rzm agent * / agentapi → contextpack budgeting → JSON/text agents parse
```

- [noderead](noderead.md) — the one read seam for typed nodes: request-scoped `Scope` batching, hydration profiles, overlay semantics for in-flight edits. Web/MCP/CLI all consume it; read-only except one sanctioned `Resolve` seam.
- [graphql-query](graphql-query.md) — typed queries over the catalog: SDL-driven roots, SQLite pushdown (must stay equivalent to in-memory evaluation), saved deterministic query recipes.
- [search](search.md) — unified retrieval → ranking → packing; evidence merging, per-owner caps, fair chunk interleaving; ranking changes need regression coverage.
- [agent-surface](agent-surface.md) — everything agents parse: `rzm agent *` fronts → `agentapi` → shared `pkg/app/mcp` handlers (CLI/MCP parity by construction), contextpack token budgeting, answer packets, session dedupe. **Output shapes are an API.**
- [mcp-server](mcp-server.md) — the shared tool-handler layer + readiness gating: respond first, initialize asynchronously, degrade gracefully.
- [cli](cli.md) — thin Cobra fronts in `cmd/`, orchestration in `pkg/app/cli`, init templates (edit sources, never generated copies).
- [vault-runtime](vault-runtime.md) — one auto-started `rzm serve` per vault root: election on `runtime.lock`, probe-verified manifest, leased spawn, one indexing lane holding the index lock, loopback control API that `rzm index` and code mode execute through.
- [validate](validate.md) — vault checks plus neutral `pkg/app/validationrun` contracts and `pkg/app/validationproduct` projection orchestration: stable ids, bounded issues, domain-specific prerequisites, and one prepared-runtime lifetime.

## Cross-cutting invariants (the rules that repeat everywhere)

1. **Paths**: vault-root-relative via `pkg/paths` everywhere; nothing absolute enters SQLite ([vault-core](vault-core.md)).
2. **Writes**: one writer lane / shared write mutex per DB; batch in transactions; never a side write path ([stores](stores.md), [indexing](indexing.md)).
3. **Sources of truth**: markdown beats rows; schema SDL beats hardcoded type knowledge; `rzm index` beats the watcher ([ontology](ontology.md), [indexing](indexing.md)).
4. **Agent-facing output is contract**: stable JSON shapes, token budgets, structured warnings over plausible-but-wrong matches ([agent-surface](agent-surface.md)).
5. **Docs move with code**: update the touched subsystem note (bump `last-verified`) and the nearest `CONTEXT.md` in the same change set; recipe `subsystem-guidance-notes` surfaces stale notes.

## Where to start for a task

- Editing a folder → its subsystem note ([README](README.md) table) and matching `<name>-subsystem` skill.
- "How does X reach Y?" → this page, then the two notes on either end.
- Deep dives → `docs/hubs/` (per-area hubs) and the specs each note links under Related docs.
