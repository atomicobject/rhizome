---
type: ReferenceDoc
summary: "Historical local-time expiration rule retained for provenance."
reference-kind: policy-history
status: superseded
superseded-by: "[[docs/reference/expiry-policy]]"
---

# Superseded local-time rule

This note records an older proposal that compared persisted expiry strings in
the machine's local timezone. It was superseded when the application adopted
UTC-aware persistence and the exact expiry boundary rule.

Do not use the local-time rule as current guidance. The active policy is
`docs/reference/expiry-policy.md`.
