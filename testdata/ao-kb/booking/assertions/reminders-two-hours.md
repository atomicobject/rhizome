---
id: "a-000000000004"
type: "assertion"
headline: "Reminders arrive 2h before"
abstract: "Booking sends reminders two hours before the visit."
kind: "decision"
confidence: "validated"
attestation: "direct"
status: "active"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "now"
horizon: "now"
sensitivity: "company"
validFrom: "2026-10-21"
createdAt: "2026-10-21T10:00:00Z"
updatedAt: "2026-10-29T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:13dd3f762c9945b40ff79c7e28ce0fdd0006309a0ba6b934a21492b8ca4e1895"
  locator: "L34-L35"
  speaker: "Taylor, clinic lead"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
---

Booking sends reminders two hours before the visit.

Context: [[reminders]].

## Connections

- supersedes:: [[reminders-twenty-four-hours]]

## Sources

- [[clinic-operations-sync]]
