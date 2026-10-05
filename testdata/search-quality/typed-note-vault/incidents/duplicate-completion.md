---
type: Incident
name: Duplicate Completion
severity: medium
runbook: "[[runbooks/audit-duplicate-completions]]"
summary: A retry created two completion facts before operation UUID enforcement.
---
# Duplicate Completion

A timeout hid the first successful response. The client retried with a new request ID, creating a second completion. Stable operation UUIDs now make the retry idempotent.
