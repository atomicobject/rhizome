---
type: ReferenceDoc
summary: "Decision guide for selecting and combining Rhizome starters."
reference-kind: guide
status: active
last-verified: 2026-10-01
---

# Choosing Your Starter

Each starter combines an ontology, skills, and scaffolds for a different kind of repository. Choose the smallest combination that matches the work you need to make recoverable.

The first `rzm init` asks which workflow to install. Each choice maps to a `--workflow` value you can pass to skip the question:

| Menu choice | Flag | Installs |
| --- | --- | --- |
| Agentic Engineering (default) | `--workflow agentic-engineering` | `agentic-engineering`, with `core` and `action-items` |
| Agentic Engineering with domain modeling | `--workflow domain` | `complex-domain`, with everything above |
| Search and agent guidance only | `--workflow none` | Rhizome guidance and skills without a workflow starter |

`core` and `action-items` on their own are available through the flag only. To change the workflow later, rerun `rzm init`, press `s` for settings, and choose Workflow.

## Quick decision table

| If you need | Use | Requires |
| --- | --- | --- |
| A base identity layer and shared Rhizome guidance | `core` | None |
| Action-item tracking | `action-items` | `core` |
| Agentic software delivery with specs, efforts, and phase routing | `agentic-engineering` | `core` |
| Source-backed requirements and domain traceability | `complex-domain` | `agentic-engineering` |

## `core`

```bash
rzm init --workflow core
```

Choose `core` for the shared identity ontology and managed Rhizome guidance without a higher-level workflow. It is the base for every other starter.

## `action-items`

```bash
rzm init --workflow action-items
```

Choose `action-items` for lightweight commitment tracking, ownership, and due-date queries. It can stand alone or be activated by another starter.

## `agentic-engineering`

```bash
rzm init --workflow agentic-engineering
```

Choose Agentic Engineering when the repository needs intended behavior, bounded efforts, implementation planning, verification evidence, and a shared route for coding agents. It installs:

- the specification-delivery ontology and query recipes, retaining domain-accurate names such as `spec-driven.graphql`
- create-only team extension docs under `docs/engineering/`
- the `agentic-engineering` router plus manual adapters for specification, effort setup, planning, implementation, foundation review, transcript ingestion, and closure
- managed guidance in `AGENTS.md` and enabled harness mirrors

Use it for a repository harness, not as an instruction to force every activity through a specification. Team policy belongs in the generated `docs/engineering/` documents.

## `complex-domain`

```bash
rzm init --workflow domain
```

Choose `complex-domain` when product behavior depends on regulations, contracts, research, operational rules, or another source-backed domain. It implies `agentic-engineering`, then adds requirement, source, process, workflow, and traceability assets.

Use Agentic Engineering alone when the work does not need a durable source-to-requirement layer.

## Valid combinations

- `agentic-engineering` activates `action-items` by default.
- `complex-domain` implies `agentic-engineering`, `core`, and the default action-items layer.
- Use `core` alone for a custom workflow starter or basic Rhizome integration.

## Existing `spec-driven` repositories

`spec-driven` is a legacy migration input, not a second canonical starter. Rerun `rzm init` to normalize the starter state to `agentic-engineering`. The migration removes only catalog-proven unchanged process docs; modified or unproven docs remain for explicit reconciliation in `.rhizome/migrations/agentic-engineering/README.md`.

For adoption detail, see [Agentic Engineering starter adoption guide](<Agentic Engineering starter - adoption guide.md>).
