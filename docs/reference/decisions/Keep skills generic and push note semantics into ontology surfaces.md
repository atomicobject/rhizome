---
type: ReferenceDoc
reference-kind: architecture
summary: Keep Rhizome skills workflow-oriented and generic; put note-family semantics in ontology surfaces and companion docs
decision-domain: architecture
status: active
derived-from:
  - docs/decisions/Keep skills generic and push note semantics into ontology surfaces.md
last-verified: 2026-04-12
---
# Keep skills generic and push note semantics into ontology surfaces

## Context

As typed-note repos become more ontology-driven, there is a risk that skills start hard-coding section names, note-family rules, and repo-local policy that really belongs in the ontology and its companion docs.

## Decision

Keep Rhizome skills generic and workflow-oriented. Use ontology docstrings, `rzm ontology authoring-guide`, and companion docs as the authoritative surface for typed-note semantics.

## Consequences

- skills stay reusable across repositories
- repo-specific note semantics can evolve through ontology changes instead of skill rewrites
- skill examples should demonstrate workflow shape, not restate local note contracts

## Follow-ups

- keep starter skills thin where ontology surfaces can carry the semantic load
- maintain companion docs when workflow detail outgrows ontology docstrings
