---
type: Requirement
id: REQ-9011
aliases:
  - REQ-9011
summary: "Accepted records remain available for 30 days under the signed agreement."
status: accepted
kind: constraint
priority: high
confidence: source_backed
review-status: reviewed
owner: "Synthetic Maintainer"
source-locations:
  - "SRC-9010: signed-agreement.pdf §4.2"
verification-hint: "Run retention_days and inspect the current retention policy."
last-reviewed: 2026-09-07
sources:
  - "[[docs/reference/requirements/sources/retention-agreement-v1|SRC-9010]]"
specs:
  - "[[docs/specs/retention-context|SPEC-9010]]"
story-refs:
  - SPEC-9010-US1
acceptance-criterion-refs:
  - SPEC-9010-US1-AC1
---

# Accepted and covered requirement

The accepted obligation is to retain records for **30 days**. Its source,
current spec, story, and acceptance criterion are explicit:

- Source: [[docs/reference/requirements/sources/retention-agreement-v1|SRC-9010]]
  at `signed-agreement.pdf` section 4.2.
- Spec: [[docs/specs/retention-context|SPEC-9010]].
- Story: `SPEC-9010-US1`.
- Acceptance criterion: `SPEC-9010-US1-AC1`.

The current governing code context is
[[docs/reference/retention-policy|retention policy]].
