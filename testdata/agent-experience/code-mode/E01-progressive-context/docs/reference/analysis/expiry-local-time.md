---
type: ReferenceDoc
summary: "Superseded local-time expiration guidance retained for provenance."
reference-kind: analysis
status: superseded
derived-from:
  - "legacy-expiry-runbook.pdf §3"
last-verified: 2026-08-01
code-anchors:
  python:
    - label: historical-expiry-path
      glob: src/expiry.py
---

# Historical local-time expiration rule

The old runbook compared persisted values in local time. It is retained to
explain the migration but is superseded by the active UTC policy. It does not
govern current behavior.
