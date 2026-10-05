---
type: Incident
name: Queue Starvation
severity: high
runbook: "[[runbooks/recover-dispatch-queue]]"
summary: Large failed uploads prevented later valid operations from being retried.
---
# Queue Starvation

A permanently invalid operation stayed at the head of one technician queue. Later completions never uploaded. The fix quarantines terminal failures and continues ordered processing for the remaining operations.
