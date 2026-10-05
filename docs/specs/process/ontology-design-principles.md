---
aliases:
    - SPEC-0025
id: SPEC-0025
last-updated: 2026-06-01T00:00:00Z
spec-status: archived
summary: Design principles for ontology-backed note families in the spec-driven starter.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Ontology design principles

## Summary

This starter uses the ontology as a durable operating model for note families, not as a way to turn every note into a database row. The schema should capture key semantics, validation, and retrieval shape while leaving room for meaningful prose and linked context.

## Goals

- keep ontology structure high-signal and queryable
- preserve enough openness for agents and humans to write useful notes
- make embedded work/requirement objects first-class when they matter
- let schema authors choose when path alone is enough to classify a note and when a required identifier must be present

## Non-Goals

- forcing every relationship into frontmatter
- replacing explanatory prose with rigid field lists
- encoding most workflow behavior directly into SDL

## Requirements

- The ontology MUST capture the durable note-family contracts that affect validation, retrieval, or workflow semantics.
- The schema MUST leave room for prose and links when richer explanation belongs in the note body rather than frontmatter.
- The starter MUST prefer structural links for canonical authored relationships and ambient discovery for looser supporting context.
- Embedded nodes MUST become first-class only when they represent durable domain objects rather than ordinary prose structure.

## Principles

### Structure the key semantics

- Use typed scalar/enum fields and frontmatter for note-defining information that should surface at the top of the note.
- Prefer structure for ids, lifecycle markers, canonical ownership, and other durable operating fields.
- Do not over-structure explanatory context that reads better as prose.

### Prefer prose plus links for supporting meaning

- If a relationship mainly exists to support reading, rationale, or surrounding context, prefer ordinary prose plus typed discovery rather than a required structured field.
- Links to reference docs and decisions often fit best inside the note body or embedded-node subtree where the explanation already lives.

### Use `@link` for canonical authored structure

- `@link` means "this relationship is part of the note's canonical authored structure."
- Use it for relationships such as:
  - note -> owner
  - other strongly authored, validated relations
- Do not use authored links on user stories for live execution ownership; effort notes own selected-story relations through their frozen scope.

### Use `@neighbors` for ambient typed discovery

- `@neighbors` means "derive typed related notes from normal links/backlinks in this scope."
- Use it for relationships such as:
  - user story -> selecting efforts, derived inbound from effort frozen scopes
  - user story -> reference docs
  - user story -> decisions
  - section-local related notes that should remain easy to author in prose

### Embedded nodes are first-class when they represent real objects

- Use embedded nodes by default when the object is a component of a parent note and does not make sense on its own.
- Embedded nodes that represent real work, requirements, acceptance criteria, decisions, or rationale objects should still behave as first-class graph/query citizens.
- Embedded user-story nodes carry required block-backed `- id::` metadata bullets. Acceptance criteria and other identifierless embedded nodes earn durable plain block locators on demand when an external citation actually exists or is being created (coderef emitter, agent citation, copy-link, manually authored cross-file wikilink). When an embedded node has or needs an authored `id::` field, prefer making that identifier field block-backed by writing the semantic id with a leading caret instead of adding a duplicate standalone anchor line. Internal addressing of uncited embedded nodes uses heading or item text, structural identity, and any available typed parent context. See [SPEC-0023](../technical/linkable-embedded-node-identifiers.md) for the usage-driven block-id lifecycle.
- Plain structural sections should not become graph nodes by default.

### Keep workflow semantics mostly in docs and skills

- Put concise summaries in schema docstrings and durable artifact semantics in layered ontology guidance.
- Put longer multi-artifact workflow rules in companion docs and skills.
- Prefer validation that helps maintain the intended structure without blocking useful authorship.

### Important top-level types get shared `id`

- Default to a shared `id` field across important top-level types.
- Use companion docs and skills to explain allocation policy and sequencing.
- Treat a required `@identifier` field as the classification gate for undeclared top-level notes. If a note has no explicit `type:` and a candidate type requires an identifier, path selectors discover the candidate but do not classify the note until the identifier field is present.
- Omit required identifiers only for note families where path-only classification is truly intended, such as lightweight documentation hubs. This keeps freeform sibling notes under nested spec or effort directories from becoming false specs or efforts.

### Story order is priority order

- For embedded user stories in this starter, document order is the default priority order.
- Do not add extra priority structure unless the repo actually needs it.

### Code and notes should become densely linked

- During implementation, agents should add rationale notes when decisions or divergences matter long-term.
- Agents should aggressively link code, rationale notes, specs, stories, and reference docs where that improves semantic traceability.
- Prefer the smallest durable link target that explains the constraint: a specific embedded user story or rationale block when available, otherwise the parent note. Use author-facing wiki links for internal vault targets and keep canonical `NodeRef` identity in structured data.
- When emitting a durable external link to an embedded node that does not yet have a block target, route through the node locator's on-demand `EnsureLinkTargetApply` path (which first makes the identifier field block-backed when possible, and only falls back to a standalone anchor when needed) or leave a diagnostic — do not emit a fragile heading-only link as if it were durable.
- The goal is a codebase that is richly connected to the note graph, not a pile of isolated markdown files.

## Open Questions

- whether this starter should eventually ship a dedicated rationale note family or continue using decisions/reference docs as the default durable home
