---
type: ReferenceDoc
summary: "Decision guide for selecting and combining Rhizome starters."
reference-kind: guide
status: active
last-verified: 2026-10-01
---

# Choosing Your Starter

Each starter combines an ontology, skills, and scaffolds for a different kind of repository. Choose the smallest combination that matches the work you need to make recoverable.

Rhizome Desktop's setup sheet asks which workflow to install under **Workflow**, as does `rzm init` in a terminal. Each choice maps to a `--workflow` value for scripted setup:

| Workflow choice | Flag | Installs |
| --- | --- | --- |
| Agentic Engineering (default) | `--workflow agentic-engineering` | `agentic-engineering`, with `core` and `action-items` |
| Agentic Engineering with domain modeling | `--workflow domain` | `complex-domain`, with everything above |
| Search and agent guidance only | `--workflow none` | Rhizome guidance and skills without a workflow starter |

`core` and `action-items` on their own are available through the flag only. To change the workflow later, open the repository's tab in Rhizome Desktop's Settings, choose another Workflow, and apply it. In a terminal, rerun `rzm init`, press `s` for settings, and choose Workflow.

## Quick decision table

| If you need | Use | Requires |
| --- | --- | --- |
| A base identity layer and shared Rhizome guidance | `core` | None |
| Action-item tracking | `action-items` | `core` |
| Agentic software delivery with specs, efforts, and phase routing | `agentic-engineering` | `core` |
| Source-backed requirements and domain traceability | `complex-domain` | `agentic-engineering` |
| Learning from past agent sessions: context shards, document edits, and findings (optional add-on) | `compounding` | `core` |

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
- the **Delivery** experience, which maps specs, stories, and efforts from shaping to settled work and copies the prompt that continues each one
- managed guidance in `AGENTS.md` and enabled harness mirrors

Use it for a repository harness, not as an instruction to force every activity through a specification. Team policy belongs in the generated `docs/engineering/` documents.

## `complex-domain`

```bash
rzm init --workflow domain
```

Choose `complex-domain` when product behavior depends on regulations, contracts, research, operational rules, or another source-backed domain. It implies `agentic-engineering`, then adds requirement, source, process, workflow, and traceability assets.

Use Agentic Engineering alone when the work does not need a durable source-to-requirement layer.

## `compounding`

```bash
rzm init --add-addon compounding
```

An optional add-on that no workflow turns on. After setup, turn it on under **Add-ons** in the repository's tab in Rhizome Desktop's **Settings** and choose **Apply changes**. You can also run the command above, ask your agent to, or use the Add-ons section of the `rzm init` settings menu. First-time setup in the app doesn't offer it. It mines this repository's Claude Code and Codex sessions into reviewed proposals: context shards for agents, edits to documents, and findings for friction worth fixing. Installing it adds a guide at `docs/reference/guides/compounding.md`.

## Valid combinations

- `agentic-engineering` activates `action-items` by default.
- `complex-domain` implies `agentic-engineering`, `core`, and the default action-items layer.
- Use `core` alone for a custom workflow starter or basic Rhizome integration.

## Existing `spec-driven` repositories

`spec-driven` is a legacy migration input, not a second canonical starter. Rerun `rzm init` to normalize the starter state to `agentic-engineering`. The migration removes only catalog-proven unchanged process docs; modified or unproven docs remain for explicit reconciliation in `.rhizome/migrations/agentic-engineering/README.md`.

For adoption detail, see [Agentic Engineering starter adoption guide](<Agentic Engineering starter - adoption guide.md>).
