---
type: ReferenceDoc
summary: "Current expiration rule available through direct note inspection."
reference-kind: policy
status: active
---

# Expiry policy

The current rule is direct and local: a record is valid only before its expiry
instant, and validity ends exactly at the expiry instant. Persisted timestamps
are UTC ISO-8601 values with a `Z` suffix.

This note is available even when semantic retrieval or a code index is not.
