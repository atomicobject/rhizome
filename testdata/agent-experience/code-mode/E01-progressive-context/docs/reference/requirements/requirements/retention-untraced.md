---
type: Requirement
id: REQ-9010
aliases:
  - REQ-9010
summary: "Expiration comparisons use an aware UTC value and expire at equality."
status: accepted
kind: constraint
priority: high
confidence: source_backed
review-status: reviewed
owner: "Synthetic Maintainer"
verification-hint: "Check the persisted parser and exact-boundary behavior in src/expiry.py."
last-reviewed: 2026-09-07
---

# Accepted but untraced requirement

The accepted obligation is to compare persisted expiration values as aware UTC
timestamps and treat equality with the expiry instant as expired. This note is
intentionally missing source, spec, story, and acceptance-criterion trace
fields so coverage queries can surface it for curation.
