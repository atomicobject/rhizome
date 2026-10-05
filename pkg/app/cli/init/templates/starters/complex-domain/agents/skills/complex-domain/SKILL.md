---
name: complex-domain
description: Use when routing domain modeling, requirements ingestion, requirements curation, spec-from-domain, traceability review, or domain backport work in a complex-domain Rhizome project.
---

# Complex Domain

Use this as the router for work that depends on domain concepts, processes, workflows, sources, or traceable requirements. Specs remain the delivery contract; complex-domain notes explain the upstream evidence and constraints.

## Route

- Source material to atomic candidate requirements: use `requirements-ingest`.
- Candidate review, duplicate/conflict analysis, source refresh, or an authorized lifecycle decision: use `requirements-curation`.
- Feature areas, bounded contexts, vocabulary, or domain types: use `domain-modeling`.
- Domain processes, actor workflows, steps, exceptions, or variants: use `workflow-mapping`.
- A spec grounded in existing domain and requirement context: use `spec-from-domain` before `agentic-engineering specify`.
- Coverage or drift between requirements, specs, stories, criteria, and delivery evidence: use `traceability-review`.
- Evidenced delivery changes that belong in domain notes or requirements: use `domain-backport` during `agentic-engineering reconcile`.

Load [retrieval and evidence](references/retrieval-and-evidence.md) when the task needs domain retrieval or a completeness claim. Compose with the installed `rhizome` skill for session, live schema, authoring, mutation, and validation mechanics. Compose with Agentic Engineering for spec, effort, approved-plan, alignment, reconciliation, and completion authority.

## Domain Decisions

- Keep one `Requirement` per obligation and keep `candidate` distinct from `accepted`.
- Treat source provenance, structural links, and semantic similarity as evidence, not authorization for acceptance or lifecycle changes.
- Keep `DomainProcess` for business or system operation and `UserWorkflow` for actor-facing tasks and variants.
- Preserve prior source versions and cited evidence when a source changes. Unknown earlier content stays an evidence gap.
- Use `ActionItem` for an unresolved accountable decision; do not invent an assignee.

Validate mutations through the Rhizome route in proportion to the changed content. Read-only review needs no blanket validation suite.
