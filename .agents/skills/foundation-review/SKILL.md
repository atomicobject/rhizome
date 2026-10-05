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

Record decisions as they settle. Hold only the edits that depend on an unresolved decision; continue independent authorized fixes, then apply the settled decisions in one pass.

## Authority

Route through the installed `agentic-engineering` skill for workflow state and evidence. The human owns formative schema, API, ownership, and extension decisions; you own surfacing and sequencing them.

## Decision boundaries

The human settles the formative decisions; hold dependent phases only until those are settled, then continue. This is not ordinary code review or a substitute for an approved plan.
