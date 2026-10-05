---
type: ProductSpec
id: SPEC-9010
aliases:
  - SPEC-9010
summary: "Current retention context for the code-mode fixture."
spec-status: active
last-updated: 2026-09-07
---

# Retention context

## Summary

The product retains records under the current signed agreement while a later
proposal remains under review.

## Goals

- Preserve the accepted 30-day retention behavior.
- Keep source provenance and candidate status visible to maintainers.

## Non-Goals

- Applying the candidate 14-day proposal before review.
- Treating a retrieved historical note as current authority.

## User Stories

### US1 - Preserve accepted retention context

- id:: ^SPEC-9010-US1
- summary:: A maintainer can trace the accepted retention behavior to its source and verification criterion.
- status:: ready

As a maintainer, I want the accepted retention contract and its source
evidence to remain discoverable while reviewing code.

#### Acceptance Criteria

- The accepted requirement points to the signed source, this spec story, and
  this criterion. ^SPEC-9010-US1-AC1
- Candidate evidence remains visibly separate from accepted scope. ^SPEC-9010-US1-AC2

## Requirements

The current implementation context is linked from
[[docs/reference/requirements/requirements/retention-covered|REQ-9011]].

## Open Questions

Should the candidate proposal replace the signed agreement? The reviewer must
decide before any contract change.
