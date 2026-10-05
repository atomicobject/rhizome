---
type: ProductSpec
summary: "Defines dependency API cards as a file-context view over the shared semantic code index spine, with deterministic dependency grouping, provenance, and doc attachment."
id: SPEC-0031
spec-status: active
last-updated: 2026-04-12
aliases:
  - SPEC-0031
---

# Dependency API Cards V1

## Summary

Rhizome should surface dependency API cards inside `file_context` so a caller can understand which external or intra-repo packages matter for a target file, what symbols are used, and which owned docs explain those dependencies.

## Goals

- give `file_context` a deterministic dependency-focused layer
- make dependency docs provenance explicit
- package existing semantic code index data as a user-facing view
- degrade cleanly when index coverage is partial

## Non-Goals

- synthesizing documentation when no owned docs exist
- replacing the underlying code-intel handle model with package tuples
- performing deep semantic or type inference beyond indexed signals
- turning API cards into a full docsite generator

## User Stories

### US1 - See which dependencies matter for the target and which symbols are actually used
- id:: ^SPEC-0031-US1
- summary:: See which dependencies matter for the target and which symbols are actually used.
- status:: ready

#### Acceptance Criteria

- Dependency cards group usage deterministically for the current file or directory target.
- Each card lists the used dependency surface with stable underlying handles when available.
- Ranking prefers dependencies with broader used surface and owned docs.

### US2 - See whether each dependency has owned docs and where that documentation came from
- id:: ^SPEC-0031-US2
- summary:: See whether each dependency has owned docs and where that documentation came from.
- status:: ready

#### Acceptance Criteria

- Card fields that came from docs include source path and match reason.
- The attachment precedence remains `mentions`, then code anchors, coderefs, and ancestor docs.
- Missing documentation is represented explicitly rather than fabricated.

## Requirements

- Dependency API cards MUST remain a packaging layer over canonical `anchor_id` and `section_id` handles.
- Cards MUST preserve provenance for docs and dependency identity fields.
- The surface MUST degrade with structured “unavailable” or “no docs” signaling when index inputs are missing.
- Card ranking MUST remain deterministic.
- `file_context` SHOULD be the primary initial surface for API cards.
- JSON and text output modes SHOULD expose the same core semantics.

## Open Questions

- how much additional package-level enrichment is worth adding before it obscures the canonical handle model
