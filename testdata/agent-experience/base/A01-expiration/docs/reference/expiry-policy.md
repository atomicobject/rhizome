---
type: ReferenceDoc
summary: "Current expiration policy for the synthetic application."
reference-kind: policy
status: active
code-anchors:
  python:
    - label: expiry-module
      glob: src/expiry.py
---

# Expiry policy

This is the current governing policy for stored expirations.

- A record is valid only before its expiry instant. Validity ends exactly at the expiry instant, so an equality comparison is expired.
- Persisted timestamps use UTC ISO-8601 values with a `Z` suffix. Readers must
  produce an aware UTC value before comparing timestamps.
- The public helper signatures and persisted representation are stable for
  this repair.

The older local-time guidance in
`docs/history/expiry-policy-local-time.md` is superseded and does not govern
the current behavior.
