---
type: TechnicalSpec
id: SPEC-0132
aliases: [SPEC-0132]
summary: "Remove evidenced duplicate policy, unused implementation, and hidden dependencies while preserving Rhizome's supported behavior and subsystem invariants."
spec-status: active
last-updated: 2026-11-05
---

# Codebase untangling

## Summary

This contract governs the refactoring proposed in the [November 5 untangle plan](../../reviews/2026-11-05-untangle-codebase/plan.md), approved by the user in the working conversation. The [audit inventory](../../reviews/2026-11-05-untangle-codebase/README.md) and [independent challenge](../../reviews/2026-11-05-untangle-codebase/inventory-review.md) define the concrete starting evidence and accepted scope corrections.

The target is one appropriate owner for each shared rule, fewer unused paths, and direct dependencies that express actual requirements. The existing single indexing writer, batch/live operation sharing, request-scoped reads, and backend transaction boundaries remain governing constraints.

## Goals

- Make the inventoried changes without dropping supported behavior, data, errors, or recovery guarantees.
- Delete obsolete production APIs and implementation after migrating their real consumers and useful test scenarios.
- Preserve boundary-specific behavior while removing duplicated mechanisms.
- Leave a verified result and a disposition for every accepted candidate group.

## Non-Goals

- New features, dependencies, schemas, data migrations, provider changes, releases, or deployment.
- A new framework, generic registry, transaction layer, or cross-vendor harness abstraction.
- Standardizing intentional CLI/HTTP, query/web, wildcard, storage-adapter, or batch/live policy differences.
- Removing documented extensions solely because this checkout has no production caller.

## Requirements

- **R1: Shared rules.** Canonical alias, section, language, schema-documentation, ranking-tier, preview, and property-lookup rules MUST have one appropriate implementation. The fuzzy matcher MUST preserve its distinct wildcard behaviors. Preview callers MUST retain their existing missing-value behavior.
- **R2: Retirements.** The unused APIs, runtime registry, legacy embedding Indexer, progress hooks, and forwarding adapters named in the approved inventory MUST be removed with their callers, obsolete imports, and stale documentation. Useful tests of retired entrypoints MUST migrate to live owners. The live watcher and documented `RankRelated` extension MUST remain.
- **R3: Indexing.** Code-priority scheduling MUST use explicit code-kind identity rather than a metrics label or list position. Ontology batching MUST remain owned by its orchestration layer. Removing unreachable queue paths MUST preserve writer order, phase barriers, shared gates, metric meanings, partial-success caching, cancellation and drain behavior.
- **R4: Views.** Configured type views MUST gate indexed execution on their actual read-store capability. The narrow read-store port and optional broader query capabilities MUST remain distinct. Removing adapters and reorganizing source resolution MUST preserve limits, warning codes, overlays and edit candidates.
- **R5: Application boundaries.** Shared graph mechanisms MUST sit below CLI and HTTP while preserving each caller's current output policy. Repository trust MUST be independent of scheduling. Operation contracts MUST have coherent construction sites while preserving public shapes, ordering and validation; the stale continuation description MUST be corrected.
- **R6: Browser contracts.** Handwritten transport types MUST derive compatible fields from generated types without weakening existing discriminated unions. Shared note-target parsing MUST preserve queries, fragments, encoding and node identity. Theme bootstrap MUST remain synchronous and preserve desktop precedence and storage-failure fallback.
- **R7: Runtime.** Cache and bootstrap reorganization MUST preserve bodies, lock ownership and initialization order. Claude session construction MUST have one private owner while retaining status noninteractivity and timeout defaults. Desktop credential mutation MUST reuse existing metadata without changing provider allowlists, canonical keys or resolution precedence.
- **R8: Tooling.** Shared Markdown protection MUST preserve current supported syntax or prove an explicit defect before changing it. Compounding executable selection and finding projection MUST have one owner. Every dev-backend restart MUST perform readiness and origin reconciliation. Workflow Go versions MUST come from the appropriate module, preserving independent scanner requirements. Release tests MUST assert command behavior where source spelling is incidental. Overlay checking MUST remain present on command, index, live and fallback validation paths after composition changes.
- **R9: Team API.** Identity MUST receive the existing ledger directly. Shared receipt rules MUST preserve conflict precedence, reusable not-dispatched requests, unknown-usage holds, invalid-outcome rejection, and adapter-owned atomicity and lease checks. Pricing MUST use the existing wire input bound. Both SQLite and disposable DynamoDB Local contracts MUST execute.
- **R10: Tests and comments.** Remove implementation narration and obsolete tests only where their removal preserves useful coverage. Replace identified incidental timing or spelling dependencies with observable behavior. The baseline harness shutdown failure MUST be diagnosed and resolved without declaring a passing rerun a fix or merely increasing a timeout.
- **R11: Evidence.** Every accepted candidate MUST be implemented or have a concrete, reviewed retention disposition. Focused tests, repository gates, generated-surface checks, packaged desktop validation, UI verification and final independent review MUST match the affected scope. Unavailable checks MUST be reported as unavailable.
- **R12: Documentation and authority.** Owning notes, source bindings and entry maps MUST move with changed ownership. Historical completed efforts and unrelated work MUST remain intact. The approved plan authorizes routine implementation and review fixes; material changes to behavior, schema or authority require a new scoped decision.

## Verification

The [approved plan](../../reviews/2026-11-05-untangle-codebase/plan.md#verification-and-completion) maps these requirements to ten batches and concrete gates. The [baseline evidence](../../reviews/2026-11-05-untangle-codebase/baseline-evidence.md) distinguishes pre-existing failures and environmental limitations from regressions. Completion requires final integrated evidence, not only passing individual worker tests.
