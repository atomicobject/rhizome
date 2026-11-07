---
id: "a-000000000005"
type: "assertion"
headline: "Same-day cancellation is free"
abstract: "Patients can cancel on the appointment day without a fee."
kind: "rule"
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
validFrom: "2026-10-20"
createdAt: "2026-10-20T10:00:00Z"
updatedAt: "2026-11-03T09:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:309770f0818c61b8e10e0813beac18571faba7763e14723f9a474dc5361d14c5"
  locator: "L30-L32"
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
