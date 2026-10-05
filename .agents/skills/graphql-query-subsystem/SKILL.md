---
name: graphql-query-subsystem
description: Use when implementing, modifying, or reviewing the typed query layer under pkg/ontology/query, pkg/ontology/pushdown, or pkg/ontology/queryrecipe. Loads query-layer design constraints and review checklist.
---

# GraphQL query subsystem

## Goal

Change or review the typed query layer (SDL-driven roots, SQLite pushdown, saved query recipes) without breaking its bounded-read contract or letting the two pushdown consumers drift apart.

## First move

Read `docs/reference/subsystems/graphql-query.md` first; its design constraints and review checklist are normative for this subsystem. Also read `pkg/ontology/query/CONTEXT.md` before editing execution code.

## Load-bearing rules

1. Compiled SDL is the contract: roots, fields, and relation names come from `schema.go`; never guess names, and any new capability must survive `Prepare` validation and introspection (`prepare.go`, `introspection.go`) so `ontology-query-schema` exposes it.
2. Typed roots stay bounded: at least one of `path`/`find`/`property`/`semantic`, at most one of find/property/semantic, with `first` caps. Do not add roots or fields that skip these checks.
3. Pushdown must be semantics-preserving: push a filter/sort only when the indexed capability allows it; sorts push the maximal prefix and residualize the coherent suffix. Pushed and residual evaluation must agree exactly — add equivalence tests for any planner change.
4. One planner, two consumers: `pkg/app/views/source_ontology.go` and `pkg/ontology/query/execute.go` share `pushdown.Planner`/`Resolver`. Never fork field-key resolution or value coercion per surface.
5. No widening on misses: missing semantic searcher, unresolved roots, unavailable runtime providers, and ambiguous targets return structured warnings/errors/nulls — never broadened matches or implicit semantic fallback.
6. Batch through `noderead`: selections compile to a `NodeReadPlan` over one shared `noderead.Scope`; per-parent resolution that requeries SQLite or projects markdown is an N+1 bug.
7. Recipes are deterministic and schema-validated: `rhizome.query-recipe.v1` envelope, unique ids, GraphQL variables (no `{{placeholders}}`), and `Validate` compiles every recipe against the live executable schema. Envelope changes must keep `rzm agent validate query-recipes` catching the new failure mode.
8. Sync docs with behavior: update `pkg/ontology/query/CONTEXT.md`, `docs/reference/subsystems/graphql-query.md`, and CLI/MCP descriptions when query-layer behavior changes.

## Pre-handoff checklist

- [ ] New/changed capability visible in SDL, introspection, and authoring-guide output
- [ ] Pushdown change has pushed-vs-residual equivalence coverage, checked for both views and GraphQL consumers
- [ ] No silently broadened results; misses produce warnings/errors/nulls
- [ ] Recipe changes pass `rzm agent validate query-recipes` semantics (schema-compiled, unique ids, variable binding)
- [ ] `go test ./pkg/ontology/query/... ./pkg/ontology/pushdown/... ./pkg/ontology/queryrecipe/...` green
- [ ] `go test -tags=integration ./...` when execution or recipe surfaces changed
- [ ] `CONTEXT.md` / subsystem note / spec links updated if behavior moved
