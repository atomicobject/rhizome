---
type: UserWorkflow
id: WF-9005
aliases:
  - WF-9005
  - WF-A05
summary: "Reviewer workflow for assessing a changed retention source."
status: active
actors:
  - "Domain reviewer"
  - "Feature owner"
review-status: reviewed
last-reviewed: 2026-09-01
---

# Review retention change workflow

This **actor workflow** starts when a domain reviewer receives a revised source.
The reviewer opens both source versions, checks the exact locations, follows
the affected requirement and spec, records the conflict, and requests a
business decision before changing the product contract.

The workflow participates in the [[docs/reference/domain/processes/retention-review|PROC-A05]]
process and keeps the actor steps separate from process states. It is linked to
[[docs/reference/requirements/requirements/retention-limit|REQ-A05-RETENTION]].
