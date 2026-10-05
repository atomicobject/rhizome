---
type: Runbook
name: Recover Dispatch Queue
summary: Quarantine a terminal operation and resume ordered queue processing.
---
# Recover Dispatch Queue

1. Confirm the operation has a terminal validation failure.
2. Copy its UUID and payload hash into the quarantine record.
3. Advance the technician queue cursor.
4. Verify later operations drain in sequence.
5. Link the quarantine record to the incident.
