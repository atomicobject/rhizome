---
id: "a-00000000000a"
type: "assertion"
headline: "Video visits are approved"
abstract: "Booking offers same-day video appointments for existing patients."
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
validFrom: "2026-10-14"
createdAt: "2026-10-14T10:00:00Z"
updatedAt: "2026-10-15T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
connections: {supports: ["a-000000000001"]}
provenance:
  quote_hash: "sha256:94cdf1e481ac1a23f323ac86900de1881bb3d1f4f63e1a8551bf2f513a610e6b"
  locator: "L24-L25"
  speaker: "Taylor, clinic lead"
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
