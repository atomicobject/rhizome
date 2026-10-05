---
type: ReferenceDoc
reference-kind: architecture
summary: "Use GraphQL SDL as Rhizome's ontology authoring format and project the query, authoring-guide, and typed-note runtime from the compiled schema."
decision-domain: architecture
status: active
derived-from:
  - docs/decisions/Use GraphQL SDL for ontology authoring.md
last-verified: 2026-04-20
---

# Use GraphQL SDL for ontology authoring

## Context

Rhizome needs one ontology surface that can define note families, preserve prose guidance, validate note structure, and feed both agent-facing docs and a queryable runtime. A separate bespoke configuration language would duplicate authoring semantics and docs.

## Decision

Use GraphQL SDL in `.rhizome/ontology/*.graphql` as the ontology authoring format and compile it into the internal schema model that powers query, validation, inspect, reference, and authoring-guide surfaces.

## Consequences

- ontology authors get one versioned source of truth
- GraphQL docstrings remain the concise summary channel while layered guidance carries deeper authoring semantics
- retrieval, inspection, and query execution share one compiled ontology model
- some GraphQL concepts remain intentionally unsupported so the note model stays operationally small

## Follow-ups

- keep ontology examples and starter templates aligned with the live schema shape
- continue pushing note-family semantics into ontology surfaces instead of agent-skill prose
