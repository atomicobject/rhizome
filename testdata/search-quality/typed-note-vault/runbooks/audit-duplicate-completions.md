---
type: Runbook
name: Audit Duplicate Completions
summary: Find completion events that refer to the same stop and compare operation UUIDs.
---
# Audit Duplicate Completions

Group completion facts by stop, compare operation UUIDs, preserve the earliest valid fact, and escalate any pair with distinct UUIDs for supervisor review.
