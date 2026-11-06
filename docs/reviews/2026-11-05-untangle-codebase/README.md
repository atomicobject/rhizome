---
summary: "Audit, approved scope and final delivery evidence for the whole-repository untangle pass."
reference-kind: analysis
---

# Untangle review

The audit found concrete opportunities to remove duplicate policy, unused APIs, and hidden initialization order. It does not support replacing Rhizome's existing subsystem architecture.

**State:** implementation and integrated verification complete; final local commit/closure in progress in [[2026-11-05-18-48-untangle-codebase|EFF-2026-11-05-18-48]]. Baseline: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. The user requested the untangle-codebase workflow, selected both optional comment and test cleanup passes, and approved the plan in chat.

Read the [delivery evidence](delivery-evidence.md) for results, candidate dispositions and checks. The [execution plan](plan.md) preserves the approved starting scope. The nine audit reports contain 46 candidate groups, their callers, evidence, verification, and retained exceptions. The [independent challenge](inventory-review.md) narrows several proposals and takes precedence where it corrects an audit recommendation.

| Area | Evidence |
| --- | --- |
| Indexing and writer | [Indexing audit](indexing.md) |
| Vault, storage, ontology primitives | [Core audit](core.md) |
| Queries, validation, configured views | [Query audit](queries.md) |
| Agent, CLI, and application surfaces | [Surface audit](surfaces.md) |
| Web application | [Web audit](web.md) |
| Runtime, desktop, harness | [Runtime audit](runtime.md) |
| Init, compounding, development/release tooling | [Tooling audit](tooling.md) |
| Search, code intelligence, embeddings | [Search/code-intel audit](search-codeintel.md) |
| Team API service | [Service audit](team-service.md) |

## What the audit established

The repository already has useful boundaries: one indexing writer, shared batch/live operations, request-scoped typed reads, and separate persistence adapters. A source import census found no non-application core package importing `pkg/app`. These are constraints to retain.

The strongest reductions use existing owners: canonical alias and section matching, language identification, ledger-backed usage reads, and deletion of genuinely unconsumed state. The old standalone embedding `Indexer` is not the active raw-note fallback: both batch and live use `embedindex.NoteSyncer`.

The audit is source-backed, with caller searches and targeted probes. It is not a claim that every source line was manually read or every possible coupling was found. After implementation, fresh reviewers must inspect the changed seams and run another bounded structural pass. Each new worthwhile finding must receive an explicit fix or evidence-backed retained disposition.

## Workflow and provenance

- Requested workflow: [caldoai untangle-codebase](https://github.com/caldoai/skills/blob/main/skills/untangle-codebase/SKILL.md), inspected November 5, 2026.
- Governing local policy: [review and approval](../../engineering/review-and-approval.md), [architecture](../../engineering/architecture.md), [testing](../../engineering/testing-policy.md), and [quality gates](../../engineering/quality-gates.md).
- Nine independent read-only audit lanes and one independent challenge review produced these reports. The findings informed the plan; explicit implementation approval subsequently came from the user.
- No application source, configuration, schema, or tests changed during this audit. The local build regenerated tracked web HTML; that generated-only change was restored to its starting bytes.
- The CLI and web build completed. The nested team API build initially failed on sandbox DNS; `make team-api-build` succeeded with dependency-download access.
- A minimal Rhizome session succeeded. Typed planning retrieval subsequently timed out; the audit also observed unavailable gateway authentication in compounding lookup. This limitation applied to the initial audit. The typed spec, allocated effort and explicit approval were subsequently recorded in the owning effort.

## Execution ledger

All 46 candidate groups are implemented with the approved retentions. Five integrated review passes are resolved. The final code passed `make verify`, including 1,844 web unit tests and 112 browser tests; both billing adapters executed against disposable stores. Desktop checks/build and the packaged fixture passed, including the additional heading-reload fix. Generated init output converged. Documentation and frozen scope checks passed before final reconciliation and are rerun at closure. The last pre-commit gate and carrying-commit record are in progress.

The baseline shutdown failure, its separate reproduced defects and final verification are distinguished in [delivery evidence](delivery-evidence.md). The approved [plan](plan.md) remains the frozen approval snapshot; execution updates live here and in the effort.
