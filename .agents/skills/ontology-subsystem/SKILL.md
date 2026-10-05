---
name: ontology-subsystem
description: Use when implementing, modifying, or reviewing Go code under pkg/ontology (schema, projections, node catalog, sync, policy, identifiers) — not for editing .rhizome/ontology/*.graphql content, which routes through the rhizome skill's ontology-authoring guidance. Loads ontology subsystem design constraints and review checklist.
---

# Ontology subsystem

## Goal

Change or review ontology core Go code without breaking the typed-note contract: schema compile, source-preserving projection, indexed read-model sync, type matching, and identifier allocation.

## First move

Read `docs/reference/subsystems/ontology.md` first; its constraints and review checklist are normative for this subsystem. For boundary work, the GraphQL query layer (`pkg/ontology/query`, `queryrecipe`) and `noderead`/`readmodel` have their own sibling notes — do not apply this skill's rules past the indexed-row handoff.

## Load-bearing rules

1. Schema is the single source of truth: never hardcode type names, field shapes, or id formats the compiled `Schema` already declares; thread new directives through `schema.go` compile + validation and `guide/render.go`.
2. Markdown is source of truth; SQLite rows and generated cards are read models that must converge — never treat catalog rows as authoritative over file content.
3. Projection is deterministic and span-preserving: derive everything from `NodeProjection` (fields carry `ByteRange`s); edits patch spans, never re-render whole notes.
4. All `ontology_nodes`/`ontology_edges`/`ontology_node_field_values` writes go through `SyncPaths` and the `deltaWriteQueue` lane, replacing rows atomically for touched paths — no direct SQLite writes.
5. Type checks go through `typeMatchesOrImplements`; selector ambiguity resolves only by strict specificity dominance (`type_specificity.go`) — never pick an arbitrary winner.
6. Identifier shape lives in `@identifier(prefix/pad/separator)` + `identifier_format.go`; allocation only via `idalloc` (max+1, gaps preserved, sibling types pooled on shared number-lines).
7. Keep `NodeRef` canonical: never collapse section/embedded refs to plain note paths; keep structural (`@link`) and ambient (`@neighbors`) relations separate; keep `_Fallback*` types out of public type surfaces.
8. Edit replay rebases through unrelated drift but must conflict on missing nodes, collection drift, duplicate-heading ambiguity, and span ownership violations — do not weaken `ConflictKind` coverage.

## Pre-handoff checklist

- [ ] Review the "Review checklist — problems to catch" section of `docs/reference/subsystems/ontology.md` against your diff.
- [ ] Tests added/updated beside the implementation (table-driven; derive tests for projection changes).
- [ ] `go test ./pkg/ontology/...` passes.
- [ ] `go test -tags=integration ./...` when sync/catalog row shapes changed.
- [ ] `pkg/ontology/CONTEXT.md` and `docs/code-anchors/Go anchor - Ontology *.md` updated if entry points or invariants moved.
- [ ] `rzm agent validate` per repo handoff policy.
