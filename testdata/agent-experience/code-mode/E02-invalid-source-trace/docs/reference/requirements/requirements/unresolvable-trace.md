---
type: Requirement
id: REQ-9020
aliases:
  - REQ-9020
summary: "Malformed accepted requirement with unresolvable trace targets."
status: accepted
kind: constraint
priority: high
confidence: source_backed
review-status: needs_review
owner: "Synthetic Maintainer"
source-locations:
  - "SRC-9999: absent-source.pdf §1"
sources:
  - "[[docs/reference/requirements/sources/absent-source|SRC-9999]]"
specs:
  - "[[docs/specs/absent-spec|SPEC-9999]]"
story-refs:
  - SPEC-9999-US1
acceptance-criterion-refs:
  - SPEC-9999-US1-AC1
---

# Invalid source and trace

- Missing source: [[docs/reference/requirements/sources/absent-source|SRC-9999]]
- Missing spec: [[docs/specs/absent-spec|SPEC-9999]]

The source, story, and acceptance criterion references are deliberately
unresolvable. This note must remain an explicit invalid fixture.
