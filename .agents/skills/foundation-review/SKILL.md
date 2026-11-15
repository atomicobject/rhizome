---
name: foundation-review
description: Use for a plan-requested review of foundational design decisions before later phases build on them. Not ordinary code review.
argument-hint: "[foundation path]"
user-invocable: true
---

# Foundation Review

## Deliverable

Settle only the unresolved formative decisions before dependent phases continue. Inspect the relevant modules, contracts, tests, and file context; treat decisions already settled by the approved plan or governing record as evidence rather than questions to reopen.

Present one item at a time:

- `Key decision`: the architectural choice needing confirmation
- `Tension found`: the pressure, tradeoff, or risk
- `Recommendation`: the direction you believe is right and why
- `Files to inspect`: the exact seams the human should examine
- `Question to resolve now`: the minimum input needed

Record decisions in the effort's execution notes as they settle. The review is done when every unresolved formative decision it covers is settled and recorded; then apply the settled decisions in one pass and continue the dependent phases.

## Authority

Route through the installed `agentic-engineering` skill for workflow state and evidence.

## Decision boundaries

The human owns formative schema, API, ownership, and extension decisions; you own surfacing and sequencing them. Hold only the edits that depend on an unresolved decision, and continue independent authorized fixes meanwhile. This is not ordinary code review or a substitute for an approved plan.
