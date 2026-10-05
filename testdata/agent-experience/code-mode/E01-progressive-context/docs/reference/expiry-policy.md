---
type: ReferenceDoc
summary: "Current UTC expiration policy for the code-mode fixture."
reference-kind: guide
status: active
last-verified: 2026-09-07
code-anchors:
  python:
    - label: expiry-path
      glob: src/expiry.py
---

# Expiration policy

This is the current governing policy for `src/expiry.py`.

- Persisted timestamps use UTC ISO-8601 values with a `Z` suffix.
- A record is valid only before its expiry instant. Equality at the expiry
  instant is expired.
- Readers must produce an aware UTC value before comparing timestamps.

The local-time note in
`docs/reference/analysis/expiry-local-time.md` is superseded supporting
evidence.
