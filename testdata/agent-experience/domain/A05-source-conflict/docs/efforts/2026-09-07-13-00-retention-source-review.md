---
type: EffortNote
id: EFF-2026-09-07-13-00
aliases:
  - EFF-2026-09-07-13-00
name: Retention source review
created-at: 2026-09-07T17:00:00Z
plan-approved-by: Synthetic Approver
status: active
summary: "Assess the revised retention source and preserve the current contract."
---

# Retention source review

## Scope

Review the changed source and record implications for
[[docs/reference/requirements/requirements/retention-limit|REQ-A05-RETENTION]]. This is a
source and traceability review. The approved work permits link and documentation
repairs; it does not authorize a business decision or a frozen spec rewrite.

## Spec Set (Frozen)

[[docs/specs/retention-controls|SPEC-A05-RETENTION]] remains the current
contract: 30 days.

## Stories In Scope (Frozen)

The source-review story and its two criteria are in scope.

## Spec Coverage Checklist

- [ ] Preserve v1 and v2 provenance and exact source locations.
- [ ] Record conflict and partial delivery evidence.

## Plan

1. Load the changed source and the requirement trace.
2. Compare the candidate 14-day proposal with the current 30-day agreement.
3. Repair links or review documentation only; route the unresolved decision.

## Original Intended Delivery

Produce a source-grounded review record without promoting candidate evidence.

## Actual Delivered

Pending review. The baseline has one of two criteria delivered; the requirement
must remain unimplemented until the second criterion has evidence.

## Execution Notes

Current evidence: 30 days from `SRC-A05-V1` at `signed-agreement.pdf` section
4.2. Candidate evidence: 14 days from `SRC-A05-V2` at
`change-request-2026-09-01.pdf` section 2. The conflict is unresolved.

## Deviations

None recorded.

## Closure Checklist

- [ ] Source versions and exact locations remain preserved.
- [ ] Conflict and partial delivery evidence are recorded without changing the contract.

## Compounding Follow-ups

- Route the unresolved candidate decision to the named reviewer.

## Status

Open pending a targeted reviewer decision.

## Partial Backport

The atomic obligation has two acceptance criteria. One has delivery evidence;
the other remains uncovered. This is partial evidence, so the requirement stays
accepted and unimplemented. Only authorized links and documentation may be
repaired during this review.
