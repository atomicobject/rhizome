---
name: search-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/search, pkg/app/unifiedsearch, pkg/app/semanticops, or pkg/app/semanticruntime. Loads the search subsystem design constraints and review checklist before edits.
---

# Search subsystem

## Goal

Change or review the unified search pipeline (retrieval → ranking → packing) and semantic search without breaking its core invariants: evidence merging, owner diversity, layer boundaries, budgets, and fairness.

## First step

Read `docs/reference/subsystems/search.md` before any edit or review in these packages. Treat its design constraints and review checklist as normative — deviations need explicit justification in the PR/effort, not silence.

## Load-bearing rules

1. **Same-handle candidates merge evidence** via `MergeCandidate(existing, incoming)` (`pkg/search/merge.go`): bounded evidence (12 total, 3 per type), first-non-empty metadata. New retrievers must emit handles consistent with `pkg/search/knowledge` so merging works.
2. **`MaxPerOwner` is the diversity contract.** Always set `Candidate.Owner`; never assemble ranked output outside `Ranker.Rank` / `approxRankCandidates` where caps are enforced.
3. **Planner plans, service executes.** `Planner.Plan()` never runs retrieval; retrievers run concurrently inside `Service.Search` and are registered through the service retriever list only — no retrieval from `pkg/app/answer`, `cmd`, or post-rank adapters.
4. **Diagnostics are additive observability.** Raw/explain payloads must not change retriever selection, ranking, pack budget, warnings, or answer role selection.
5. **Semantic fair interleaving:** limit = total chunks; group by note ordered by top-chunk score; round-robin so no note hogs slots (`interleaveScoredChunksByOwner`, `pkg/search/semantic/search.go`). Do not bypass or change the diversity key casually.
6. **Ontology-node chunks need catalog identity** before becoming candidates; skip stale chunks without it.
7. **Ranking/weight changes require regression coverage** in `pkg/search/archetype_regression_test.go` plus affected `search-quality-evaluation-corpus` entries (compare answer-shaped vs raw output).
8. **Respect budgets and lane policy:** packing honors caller token budgets; embedding work goes through `semanticruntime` lanes (no ad hoc provider batchers); `semanticops` is provider construction only, not a command-flow or retrieval layer.

## Pre-handoff checklist

- [ ] Constraints in `docs/reference/subsystems/search.md` reviewed against the diff; checklist failure modes ruled out.
- [ ] New/changed rankers also implement `ApproxScoreProvider`; timeout fallback still degrades instead of erroring.
- [ ] Regression tests updated for ranking/interleaving changes.
- [ ] Tool descriptors (`ToolDescriptor` in `pkg/app/agentapi/catalog.go`), `pkg/app/mcp/CONTEXT.md`, and `README.md` updated if user-facing behavior changed.
- [ ] Tests: `go test ./pkg/search/... ./pkg/app/unifiedsearch/... ./pkg/app/semanticops/... ./pkg/app/semanticruntime/...`
- [ ] Full gate before commit: `make check`.
