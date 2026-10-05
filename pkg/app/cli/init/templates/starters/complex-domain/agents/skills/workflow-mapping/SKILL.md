---
name: workflow-mapping
description: Use when modeling or updating complex-domain processes, user workflows, workflow variants, and process steps that constrain requirements or specs.
---

# Workflow Mapping

Keep operating processes separate from actor-facing workflows. A `DomainProcess` describes business or system operation, states, handoffs, decisions, and invariants. A `UserWorkflow` describes an actor's goal, tasks, interaction path, variants, and friction.

Load the shared retrieval and evidence guidance through the `complex-domain` router. Use the live authoring guide for the selected type. Use `domain-context-pack` for a known process or workflow. For topic-only work, run `domain-inventory-pack` for `DomainProcess` and `UserWorkflow` before optional `domain-topic-survey` candidate ranking.

- Capture process triggers, states, handoffs, exceptions, rules, and requirement points in `DomainProcess`.
- Capture actors, goals, entry/exit conditions, task flow, variants, exceptions, and friction in `UserWorkflow`.
- Link a process and workflow when the actor journey participates in the broader operation; do not collapse them into one model.
- Link requirements only when the obligation depends on that process step or workflow variant.
- Preserve unconfirmed variants and conflicting exception handling as explicit uncertainty or `ActionItem` entries.
- Keep implementation details in delivery artifacts unless they define durable domain behavior.

Report the created or updated models, justified links, sources, unresolved variants, and retrieval qualifications. A capped or degraded inventory does not prove a model is new. Use the requirements-by-process view for triage when useful, then validate only the changed content and relevant links.
