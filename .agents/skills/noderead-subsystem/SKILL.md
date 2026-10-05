---
name: noderead-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/ontology/noderead or pkg/ontology/readmodel, or their consumers' read paths. Loads noderead design constraints and review checklist.
---

# Noderead subsystem

## Goal

Change or review the typed node read seam (request-scoped `Scope` batching, hydration profiles, edit-session read overlays, graph assembly) without breaking the invariants that web, MCP, and CLI consumers all rely on.

## First step

Read `docs/reference/subsystems/noderead.md` before any edit or review in these packages. Treat its design constraints and review checklist as normative — deviations need explicit justification in the PR/effort, not silence.

## Load-bearing rules

1. **Read-only, one write seam.** Scope methods never edit notes or catalog rows except `Resolve` ensure-apply (`pkg/ontology/noderead/resolve.go`), which also refreshes the catalog read model and drops scope projection/snapshot caches. No new write paths.
2. **Fresh `Scope` per request/job.** Scopes memoize aggressively; sharing one across requests serves stale rows. Match the consumer pattern in `pkg/app/web/node_workspace.go` and `pkg/app/mcp/tool_files.go`.
3. **Canonical `NodeRef` + profile in every cache key.** Never key by note path or `NodeRef.String()`; normalize via `noderead.NormalizeRef` first.
4. **Overlay merge contract:** `ScopeOptions.ReadOverlay` is the only edit-session hook. Touched notes project from preview content, merge with committed rows by canonical ref, and filter/sort/page apply *after* the merge. Never let a read path bypass an active overlay or pre-filter before merging.
5. **Batch, never N+1.** Store contracts (`Store`/`CatalogStore`/`GraphStore` in `noderead/types.go`, `readmodel/graph.go`) batch by paths/sources. No per-ref loops over `Hydrate`/`Traverse`, no per-row store methods.
6. **Graph merge belongs to `Scope.Graph`/`GraphFacts`:** endpoint selectors prevent embedded-node requests from widening into the parent note's ambient neighborhood; ontology edges beat wikilink fallback; code edges are opt-in. Consumers never join graph tables directly.
7. **Profiles gate I/O.** Summary hydration reads indexed metadata only — no file reads; content/workspace opt in. `_FallbackNote`/`_FallbackSection` stay out of public type lists.
8. **`readmodel` stays DTOs + store interfaces** — no browser, search, or profile semantics; those live in noderead so all consumers share one rule set.

## Pre-handoff checklist

- [ ] Constraints in `docs/reference/subsystems/noderead.md` reviewed against the diff; review-checklist failure modes (stale cache, overlay bypass, identity collapse, N+1, surface divergence) ruled out.
- [ ] Semantics changes verified consistent across web, MCP, and CLI consumers — no surface-local special cases.
- [ ] Cache invalidation follows any data change (scope caches, `nodeProjectionCache` modTime/schemaHash guard, overlay revision key).
- [ ] Store contract changes covered by `store_contract_test.go` / `scope_test.go`; new overlay predicates implemented in noderead, not callers.
- [ ] `go test ./pkg/ontology/noderead/... ./pkg/ontology/readmodel/...` passes (plus `go test -tags=integration ./...` when store row shapes or graph merge changed).
- [ ] `pkg/ontology/noderead/CONTEXT.md` and `docs/reference/subsystems/noderead.md` updated if entry points or invariants moved.
