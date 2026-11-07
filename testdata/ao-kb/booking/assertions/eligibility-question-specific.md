---
id: "a-00000000000d"
type: "assertion"
headline: "Check referral before booking?"
abstract: "Must a referral be verified before a patient reserves an appointment?"
kind: "question"
confidence: "guess"
attestation: "direct"
status: "active"
phase: "definition"
lenses: ["patient", "operations"]
products: ["booking"]
tags: ["booking"]
priority: "now"
horizon: "near"
sensitivity: "company"
validFrom: "2026-10-26"
createdAt: "2026-10-26T10:00:00Z"
updatedAt: "2026-10-27T10:00:00Z"
createdVia: "codex"
extracted_by: "llm@ao-kb@fixture"
connections:
  - type: "refines"
    target: "a-000000000001"
provenance:
  quote_hash: "sha256:84bde4c4a677fb28f2c14520651ff7476423e08fa233fa64639866db14e31338"
  locator: "L37-L38"
  speaker: "Riley, reception coordinator"
  speaker_role: "team"
  extracted_by: "llm@ao-kb@fixture"
---

Must a referral be verified before a patient reserves an appointment?

Context: [[eligibility]].

## Connections

- supersedes:: [[eligibility-question-old]]

## Sources

- [[clinic-operations-sync]]
