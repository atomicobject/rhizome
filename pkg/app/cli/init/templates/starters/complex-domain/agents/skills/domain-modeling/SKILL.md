---
name: domain-modeling
description: Use when creating or maintaining complex-domain feature areas, bounded contexts, and domain types that requirements or specs depend on.
---

# Domain Modeling

Create durable domain homes that requirements and specs can cite without copying definitions.

Load the shared retrieval and evidence guidance through the `complex-domain` router. Use the live authoring guide for `FeatureArea`, `DomainContext`, or `DomainType`. Use `domain-context-pack` for a known note. For a topic-only request, run `domain-inventory-pack` for the relevant type before using `domain-topic-survey` to rank remaining candidates.

- Prefer an existing durable note when the concept already has a stable home.
- Create a note when the concept needs reusable identity, lifecycle, ownership, or traceability.
- Keep terminology, states, relationships, invariants, and rule applicability in domain notes; keep delivery behavior in specs.
- Link requirements only where the source or accepted domain decision justifies the relation.
- Preserve competing definitions and source conflicts. Use `ActionItem` for a decision that needs an accountable owner.
- Mark stale or superseded knowledge only from evidence and link a successor when one exists.

A resolved inventory with no match can support creating a new model. An unresolved, degraded, or capped result cannot. Report the model and links created or updated, evidence used, remaining uncertainty, and result qualifications. Validate the changed note family through the Rhizome route.
