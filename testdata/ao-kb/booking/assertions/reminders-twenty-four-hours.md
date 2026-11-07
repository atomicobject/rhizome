---
id: "a-000000000003"
type: "assertion"
headline: "Reminders arrive 24h before"
abstract: "Booking sends reminders 24 hours before the visit."
kind: "decision"
confidence: "validated"
attestation: "direct"
status: "active"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "next"
horizon: "near"
sensitivity: "company"
validFrom: "2026-10-01"
createdAt: "2026-10-01T10:00:00Z"
updatedAt: "2026-10-06T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:5e42e2a60b58c2a5100135874f61810619bd4f5d5a52049598cc319905853073"
  locator: "L6-L7"
  speaker: "Morgan, clinic operations"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
---

Booking sends reminders 24 hours before the visit.

Context: [[reminders]].

## Connections

- contradicts:: [[reminders-forty-eight-hours]]
- supersedes:: [[reminders-forty-eight-hours]]

## Sources

- [[clinic-operations-sync]]
