---
type: Decision
name: Offline Conflict Policy
status: accepted
tags: [type/decision]
summary: Resolve offline edits by operation identity and server assignment version.
---
# Offline Conflict Policy

Each offline operation has a stable UUID. The server accepts an operation once. Assignment edits carry the version the technician observed; a stale assignment edit becomes a review item instead of silently replacing current work.
