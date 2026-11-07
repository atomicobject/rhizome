---
id: "a-000000000001"
type: "assertion"
headline: "Patients book their own slots"
abstract: "Patients can choose an available appointment without calling the clinic."
kind: "need"
confidence: "validated"
attestation: "direct"
status: "active"
layer: "definition"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "now"
horizon: "near"
sensitivity: "company"
validFrom: "2026-10-08"
createdAt: "2026-10-08T10:00:00Z"
updatedAt: "2026-10-28T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
reviewedBy: "Taylor, clinic lead"
reviewedAt: "2026-10-28"
contradicts_rows: ["feature:FEAT-BOOK-03"]
supports_rows: ["flow:FLOW-BOOK-01", "requirement:REQ-BOOK-02"]
provenance:
  quote_hash: "sha256:824ae1b326c7d0a61003a33fcb3760d55ce21159f335856687c112ea65bc9dbc"
  locator: "L12-L18"
  speaker: "Morgan, clinic operations"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
  source: "s-000000000001"
---

Patients can choose an available appointment without calling the clinic.

Context: [[self-service]]. An incidental [[clinic-pricing-policy]] link is not citation evidence. Also [[eligibility]].

## Connections

- supports:: [[new-waitlist-after-snapshot]]
- corroborated-by:: [[external-booking-brief]]
- corroborated-by:: [[generated-booking-summary]]
- supported-by:: [[video-visits-approved]]

## Sources

- [[patient-booking-interview]]
- [[clinic-operations-sync]]
- [[patient-booking-interview]]

## Referenced By

- [[booking-prd]]
- [[booking-policy-review]]
- [[booking-service-outline]]
- [[reception-runbook]]
