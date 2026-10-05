---
type: DomainProcess
id: PROC-9005
aliases:
  - PROC-9005
  - PROC-A05
summary: "Review retention evidence before changing the accepted contract."
status: active
review-status: reviewed
last-reviewed: 2026-09-01
---

# Retention review process

This **process** governs how the team receives source evidence, checks its
authority, records conflicts, and routes an unresolved decision. Its states are
`current`, `candidate`, `conflicted`, and `reviewed`.

- Start with the current signed source.
- Compare later evidence with its version, date, and exact location.
- Preserve both sources when values conflict.
- Keep the accepted requirement at 30 days until a reviewer resolves the
  candidate proposal.

The process constrains [[docs/reference/requirements/requirements/retention-limit|REQ-A05-RETENTION]]
and participates in [[docs/reference/domain/workflows/review-retention-change|WF-A05]].
