---
type: Requirement
id: REQ-9005
aliases:
  - REQ-9005
  - REQ-A05-RETENTION
summary: "Accepted records remain available for 30 days."
status: accepted
kind: constraint
priority: high
confidence: source_backed
review-status: reviewed
owner: "Synthetic Approver"
source-locations:
  - "SRC-A05-V1: signed-agreement.pdf §4.2"
  - "SRC-A05-V2: change-request-2026-09-01.pdf §2 (candidate conflict)"
verification-hint: "Retention behavior and source conflict review remain separately verifiable."
last-reviewed: 2026-09-01
---

# Retention requirement

The accepted obligation is to retain records for **30 days** under the current
signed agreement. It remains accepted and **unimplemented** until both delivery
criteria in the linked spec have evidence.

- Source: [[docs/reference/requirements/sources/policy-v1|SRC-A05-V1]] at
  `signed-agreement.pdf` section 4.2.
- Candidate conflict: [[docs/reference/requirements/sources/policy-v2|SRC-A05-V2]] at
  `change-request-2026-09-01.pdf` section 2.
- Process: [[docs/reference/domain/processes/retention-review|PROC-A05]].
- User workflow: [[docs/reference/domain/workflows/review-retention-change|WF-A05]].
- Frozen spec: [[docs/specs/retention-controls|SPEC-A05-RETENTION]].

The obligation has two acceptance criteria. One criterion has delivery evidence
in the effort record; the second remains uncovered. This is partial evidence,
not implementation completion.
