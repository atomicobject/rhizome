---
id: "a-000000000006"
type: "assertion"
headline: "Same-day cancellation has fee"
abstract: "Patients pay a fee when cancelling on the appointment day."
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
updatedAt: "2026-10-22T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:549b6c175969f1db194553e4afe5abb50c2cad2833bfc8cb4fcb4b51da3f2208"
  locator: "L27-L28"
  speaker: "Devon, finance lead"
  speaker_role: "stakeholder"
  extracted_by: "llm@ao-kb@fixture"
---

Patients pay a fee when cancelling on the appointment day.

Context: [[cancellation]].

## Connections

- contradicts:: [[same-day-cancellation-free]]

## Sources

- [[clinic-operations-sync]]
