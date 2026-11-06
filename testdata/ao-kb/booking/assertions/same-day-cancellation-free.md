---
id: "a-000000000005"
type: "assertion"
headline: "Same-day cancellation is free"
abstract: "Illustrative Booking assertion."
kind: "rule"
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
updatedAt: "2026-11-03T09:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:23a4144b5227c15ab9e62552c7b0c49df9aab1e8e9a1f21df4de984e50da2c2d"
  locator: "L12-L18"
  speaker: "Morgan, clinic operations"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
---

Patients can cancel on the appointment day without a fee.

Context: [[cancellation]].

## Connections

- contradicts:: [[same-day-cancellation-fee]]

## Sources

- [[clinic-operations-sync]]

## Referenced By

- [[booking-prd]]
- [[booking-policy-review]]
- [[booking-service-outline]]
- [[reception-runbook]]
