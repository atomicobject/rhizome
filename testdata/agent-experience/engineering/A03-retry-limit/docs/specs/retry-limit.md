---
type: ProductSpec
id: SPEC-9003
aliases:
  - SPEC-9003
  - SPEC-A03-RETRY
summary: "Bounded retry behavior for the synthetic worker."
spec-status: active
last-updated: 2026-09-07
---

# Retry limit

## Summary

The worker needs predictable retry behavior for a failed operation.

## Goals

- Permit at most three attempts for one operation.
- Stop retrying after a successful operation; there is no retry after success.

## Non-Goals

- Backoff and jitter are separate work and are not part of this approved change.

## User Stories

### US1 - Retry a failed operation

- id:: ^SPEC-9003-US1
- summary:: A worker can retry a failed operation within a deterministic three-attempt limit.
- status:: ready

As a worker, I want a bounded retry decision so a failed operation cannot retry
forever.

#### Acceptance Criteria

- The helper allows retries before the three-attempt limit. ^SPEC-9003-US1-AC1
- The helper returns false once three attempts have completed or once the
  operation has succeeded. ^SPEC-9003-US1-AC2

## Requirements

- The retry decision must remain deterministic and preserve the helper's public
  signature.

## Open Questions

None for this bounded effort.
