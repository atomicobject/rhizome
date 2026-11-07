---
id: "a-000000000013"
type: "assertion"
headline: "Malformed locator fixture"
abstract: "The clinic may offer a special booking window."
kind: "hypothesis"
confidence: "guess"
attestation: "inferred"
status: "active"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "later"
sensitivity: "company"
validFrom: "2026-10-28"
createdAt: "2026-10-28T10:00:00Z"
updatedAt: "2026-10-30T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
provenance:
  quote_hash: "sha256:1fa4b6a934fba197f46ec44434abfb02216e1fbf2df3602b33dac44b283f32d4"
  locator: "Lbanana"
  speaker: "Casey, product analyst"
  speaker_role: "team"
  extracted_by: "llm@ao-kb@fixture"
---

The clinic may offer a special booking window.

Context: [[availability]].

## Connections

- teleports-to:: [[clinic-patient-books-own-slot]]
- depends-on:: [[missing-booking-rule]]

## Sources

- [[clinic-operations-sync]]
