---
type: ProductSpec
id: SPEC-9005
aliases:
  - SPEC-9005
  - SPEC-A05-RETENTION
summary: "Current retention controls for records under review."
spec-status: active
last-updated: 2026-08-15
---

# Retention controls

## Summary

The product retains records under the current signed agreement while later
source proposals receive explicit review.

## Goals

- Keep the accepted retention value at 30 days until a decision changes it.
- Make conflicting source evidence and review status visible.

## Non-Goals

- Applying the 14-day proposal before a reviewer accepts it.
- Rewriting frozen scope from a candidate source alone.

## User Stories

### US1 - Review a changed source

- id:: ^SPEC-9005-US1
- summary:: A reviewer can trace a changed source without rewriting the accepted contract.
- status:: ready

As a domain reviewer, I want to trace a changed source to the current feature
so I can request a decision without losing provenance.

#### Acceptance Criteria

- Preserve both source versions, exact locations, and the conflict; retain the
  current 30-day contract. ^SPEC-9005-US1-AC1
- Record partial delivery evidence and leave the accepted requirement
  unimplemented until both criteria have evidence. ^SPEC-9005-US1-AC2

## Requirements

The feature is constrained by [[docs/reference/requirements/requirements/retention-limit|REQ-A05-RETENTION]],
[[docs/reference/domain/processes/retention-review|PROC-A05]], and
[[docs/reference/domain/workflows/review-retention-change|WF-A05]].

## Open Questions

Which authorized reviewer will decide whether the candidate 14-day proposal
should replace the signed agreement? Preserve this as an open review request.
