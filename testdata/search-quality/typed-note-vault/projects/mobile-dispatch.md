---
type: Project
name: Mobile Dispatch
owner: "[[people/maya]]"
decisions:
  - "[[decisions/offline-conflict-policy]]"
  - "[[decisions/route-ownership]]"
summary: Field technicians receive route assignments and complete stops when connectivity is intermittent.
---
# Mobile Dispatch

The mobile client keeps a local operation log. It uploads operations in sequence after reconnecting and never overwrites a newer server assignment.

## Reliability target

A technician can complete an assigned stop during a 30 minute outage. Reconnection drains the queue within five minutes without duplicate completion events.
