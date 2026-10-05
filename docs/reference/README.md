---
type: ReferenceDoc
reference-kind: architecture
summary: "Hub for durable supporting context in the Agentic Engineering workflow."
---

# Reference docs

## Summary

Reference docs preserve durable supporting context that should not live only in specs or efforts.

## Families

- `decisions/`
- `requirements/`
- `personas/`
- `domain/`
- `guides/`
- `analysis/`

## Rules

- preserve provenance when the note is derived from meetings, transcripts, or source documents
- use `reference-kind` to distinguish the family unless a dedicated subtype is justified
- keep current-state guidance here, not in active effort notes
- rationale that matters beyond one effort should land here or in a decision note rather than staying only in chat or commits
- link reference docs aggressively from specs, user stories, rationale notes, and code-adjacent docs when that improves semantic traceability
