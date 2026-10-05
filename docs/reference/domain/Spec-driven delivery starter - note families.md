---
type: ReferenceDoc
summary: "Defines the main note families in the spec-driven starter and when the shared `ReferenceDoc` and decision families are sufficient."
reference-kind: architecture
derived-from:
  - docs/reference-notes/Spec-driven delivery starter - note families.md
last-verified: 2026-04-12
status: active
---

# Spec-driven delivery starter - note families

## Summary

The spec-driven starter divides work across a small set of long-lived note families: specs, efforts, decisions, reference docs, and folder-level documentation hubs.

## Main families

- process, product, technical, experience, and operations specs
- effort notes for bounded execution
- architecture and product decisions for durable rationale
- reference docs for supporting context
- README-based hubs for folder orientation only

## Design rule

Prefer the shared `ReferenceDoc` family until a new note family truly needs a different contract. Prefer semantic subtyping of decisions and specs over proliferation of nearly identical note types.
