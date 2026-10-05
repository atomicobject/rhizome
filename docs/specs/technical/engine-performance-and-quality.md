---
type: TechnicalSpec
id: SPEC-0089
spec-status: active
last-updated: 2026-09-05
summary: "Preserve Rhizome engine behavior while reducing measured read, write, and browser/runtime costs and demonstrated design complexity."
aliases:
  - SPEC-0089
---

# Engine performance and quality

## Summary

This contract governs measured improvements across the Rhizome engine and browser-to-server-to-database paths. Research must cover ontology, node reads and graphs, typed queries, indexing, storage, caches, runtime, browser data lifecycle, configuration, and related paths. Selected changes must preserve observable behavior and improve a demonstrated cost or concrete maintenance problem.

## Goals

- Remove the largest measured sources of unnecessary latency, allocations, database work, and repeated requests.
- Keep ownership explicit and Go code cohesive; simplify demonstrated duplication and awkward configuration at their owning boundaries.
- Preserve reliable regression coverage while replacing tests tied to incidental implementation details when justified.
- Leave reproducible evidence and a prioritized disposition for every material opportunity reviewed.

## Non-Goals

Speculative rewrites, abstract frameworks without demonstrated callers, unrelated dependency upgrades, production data changes, release publication, and merging the final integration PR into main are excluded.

## Requirements

1. Review all named areas and document source evidence, uncertainty, impact, dependencies, owner, and disposition in the effort docket. A measured bottleneck, verified bug, and design judgment must be distinguishable.
2. Preserve public CLI/API/configuration and stored-data behavior by default. Any exception requires a coordinator-approved decision documenting compatibility impact, migration, rollback, and verification before implementation. The user explicitly delegated these scoped approvals to the coordinator.
3. Preserve vault-relative path identity, Markdown/SDL sources of truth, the typed node read seam, SQLite single-writer discipline, index convergence, cancellation, result ordering, and cache freshness across supported writers.
4. Measure each performance change before and after with the same representative fixture, command, and environment. Include data size, relevant cold/warm state, sample count, latency and allocations or query/request counts where applicable. Timing claims from contended runs must be qualified and repeated in final verification.
5. Use bounded independent worker branches and PRs targeting the integration branch. Each accepted batch needs independent coordinator review, meaningful observable regression coverage, required repository gates, evaluated Greptile feedback, and current-head evidence before squash merge.
6. Exercise representative browser flows against the built server/database for user-facing changes. Verify mutation, invalidation, error, cancellation, and empty-data behavior relevant to each batch.
7. Keep abstractions inside their owning subsystem unless a documented cross-boundary need justifies a new seam. Update owning guidance when invariants move. Remove tests only with an explicit explanation of retained or stronger behavioral coverage.
8. Finish by integrating selected high-value batches, checking current-main ancestry, running required gates and before/after evidence, and opening a final reviewable PR to main. Document remaining lower-value or uncertain ideas; do not claim completion while selected work or material verification is outstanding.

## Open Questions

Candidate selection and implementation details depend on measured research. The coordinator resolves them in the effort decision record under the user's delegated authority. No public contract change is selected at creation.
