# Spec Template Reference

Use this reference for starter examples. The live ontology authoring guide and companion docs remain authoritative for typed spec semantics.

## Common Sections

- **Summary**: 1-2 paragraphs describing the contract, audience/system, and why it matters.
- **Goals**: observable outcomes this spec must preserve.
- **Non-Goals**: exclusions that prevent likely scope creep.
- **User Stories**: embedded stories with descriptive `USn - Outcome` headings, required metadata bullets (`- id:: ^...`, `- summary::`, `- status::`), and direct acceptance-criterion bullets when the spec family uses story-based decomposition.
- **Requirements**: testable MUST/SHOULD/MAY obligations, including cross-story constraints and codebase constraints.
- **Open Questions**: real unresolved decisions with proposed answers or decision prompts.

Include key entities, edge cases, dependencies, assumptions, and documentation plan when relevant.

Acceptance criteria do not get `id::` fields. When an external artifact needs a durable plain block locator, inspect the source edit with `rzm agent node-link --target <spec#criterion-fragment> --ensure plan`. A planned locator must not be cited. Apply the repair through a write-capable Rhizome surface only when authorized, then rerun the helper and use the returned link after `requiresFix` is false.

## TODO Markers

For any decision that needs user input, write the proposed answer with a `[TODO: ...]` marker:

```markdown
## Key design decisions

### Caching strategy

[TODO: Confirm with user] Using Redis for distributed caching because X, Y, Z. Alternative considered: in-memory with sync.

### API format

[TODO: Confirm with user] REST over GraphQL because the team has more REST experience and the queries are simple.
```

This preserves analysis in the file, survives context compaction, makes open questions searchable, and allows work to continue while waiting for answers.

## Starter Skeleton

```markdown
---
type: ProductSpec
id: "SPEC-0007"
summary: "<1-2 sentence summary>"
spec-status: proposed
last-updated: YYYY-MM-DD
aliases:
  - SPEC-0007
---

# <Feature name>

## Summary

## Goals

## Non-Goals

## User Stories

### US1 - <descriptive outcome>

- id:: ^SPEC-0007-US1
- summary:: <one user-facing outcome, including actor/context when useful>
- status:: ready

#### Acceptance Criteria

- <Observable criterion that makes the story satisfied.>
  verification:: <optional concise check>

## Requirements

- MUST <testable obligation>
- SHOULD <strong recommendation>
- MAY <allowed option>

## Open questions

[TODO: Confirm with user] <question and proposed answer>

## Documentation plan
```
