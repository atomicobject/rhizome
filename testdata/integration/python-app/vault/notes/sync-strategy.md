# Sync Strategy

Sync sends a best-effort push to an external queue after local persistence.

- The queue mock lives in `todoapp.services.sync`
- Sync is triggered from `add_task` but can be bypassed for dry runs
- Engineering context: [[communities/engineering]]
- Release context: ![[release-plan]]
