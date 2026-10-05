---
type: ProductSpec
id: SPEC-9004
aliases:
  - SPEC-9004
  - SPEC-A04-RETRY
summary: "A two-phase retry change that preserves a discoverable fresh-agent handoff."
spec-status: active
last-updated: 2026-09-07
---

# Two-phase retry handoff

## Summary

The team is delivering a small retry correction in two approved phases so a
fresh agent can continue from durable state.

## Goals

- Inspect and document edge behavior during phase 1.
- Apply the approved boundary correction during phase 2.

## Non-Goals

- Repeating completed phase 1 work.
- Treating a transcript or an inferred approval as the handoff record.

## User Stories

### US1 - Continue approved retry work

- id:: ^SPEC-9004-US1
- summary:: A fresh agent can continue the approved retry correction from durable phase state.
- status:: ready

As a maintainer, I want the second phase to discover the first phase's durable
state so the approved work can continue without reapproval.

#### Acceptance Criteria

- Phase 1 records the behavior when `max_attempts=0`. ^SPEC-9004-US1-AC1
- Phase 2 preserves that recorded behavior while correcting the retry boundary. ^SPEC-9004-US1-AC2

## Requirements

- A fresh agent must be able to identify the remaining approved phase from the
  worktree and effort note.

## Open Questions

None for this fixture.
