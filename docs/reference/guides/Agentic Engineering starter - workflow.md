---
type: ReferenceDoc
summary: "Recommended delivery loop for repositories using the Agentic Engineering starter."
reference-kind: guide
last-verified: 2026-07-19
status: active
---

# Agentic Engineering starter workflow

## Summary

The default route is classify -> specify -> effort -> plan -> implement -> verify -> close. The router keeps phase-specific guidance lazy; manually useful adapters remain available when the phase is already clear.

## What to preserve

- Specs describe intended behavior and durable contracts.
- Efforts freeze the selected scope, plan, evidence, deviations, and follow-up work.
- Tests and quality gates make completion observable.
- Documentation preserves rationale and improves retrieval from the code that needs it.

Teams own the defaults in `docs/engineering/`. Use the installed `rhizome` guidance for schema, ontology, retrieval, validation, and safe-mutation mechanics.

## Foundation and closure

Pause for review after a formative architecture phase before broadening implementation. Close an effort only with the repository's required verification evidence and truthful deviations. Downstream repositories record their local policy in `docs/engineering/workflow.md`.
