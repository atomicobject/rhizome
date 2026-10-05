---
type: ReferenceDoc
summary: "Layered documentation model for Rhizome: code-local docs, module docs, and long-lived reference notes with coderef/code-anchor bindings."
reference-kind: guide
derived-from:
  - docs/reference-notes/Rhizome documentation - Layering + authoring workflow.md
last-verified: 2026-04-12
status: active
---

# Rhizome documentation - Layering + authoring workflow

## Summary

Rhizome documentation works best when the closest durable contract stays closest to the code, and broader explanation moves into long-lived reference docs instead of bloating retrieval surfaces.

## Layers

- function and type docs for local behavior
- file headers for entry points and orchestrators
- `CONTEXT.md` for module boundaries and sharp edges
- long-lived specs, decisions, and reference docs for cross-cutting contracts

## Workflow

- document the local contract where behavior lives
- add coderefs at entry points when a note is the right durable home
- add targeted code anchors when dependents need the same doc automatically
- verify retrieval with representative `file_context` calls
