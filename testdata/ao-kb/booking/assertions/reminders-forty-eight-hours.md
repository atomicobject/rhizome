---
id: "a-000000000002"
type: "assertion"
headline: "Reminders arrive 48h before"
abstract: "Booking sends reminders 48 hours before the visit."
kind: "decision"
confidence: "validated"
attestation: "direct"
status: "active"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "later"
horizon: "never"
sensitivity: "company"
validFrom: "2026-09-15"
createdAt: "2026-09-15T10:00:00Z"
updatedAt: "2026-09-22T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:60b88dc64d7da3fe6c7e467abd7ce5bd021c0aee0303dcc484b77d839d35d633"
  locator: "L3-L4"
  speaker: "Taylor, clinic lead"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
---

Booking sends reminders 48 hours before the visit.

Context: [[reminders]].

## Connections

- contradicts:: [[reminders-twenty-four-hours]]

## Sources

- [[clinic-operations-sync]]
