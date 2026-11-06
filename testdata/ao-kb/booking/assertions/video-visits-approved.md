---
id: "a-00000000000a"
type: "assertion"
headline: "Video visits are approved"
abstract: "Illustrative Booking assertion."
kind: "decision"
confidence: "validated"
attestation: "direct"
status: "active"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "now"
horizon: "near"
sensitivity: "company"
validFrom: "2026-10-20"
createdAt: "2026-10-20T10:00:00Z"
updatedAt: "2026-10-20T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
connections: {supports: ["a-000000000001"]}
provenance:
  quote_hash: "sha256:23a4144b5227c15ab9e62552c7b0c49df9aab1e8e9a1f21df4de984e50da2c2d"
  locator: "L12-L18"
  speaker: "Morgan, clinic operations"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
---

Booking offers same-day video appointments for existing patients.

Context: [[video-visits]].

## Connections

- resolves:: [[same-day-video-question]]
- supports:: [[clinic-patient-books-own-slot]]

## Sources

- [[clinic-operations-sync]]
