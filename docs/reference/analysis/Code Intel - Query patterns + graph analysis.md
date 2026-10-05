---
type: ReferenceDoc
summary: "Deterministic query and traversal patterns over the code-intel spine, including expansion queries and graph-derived documentation heuristics."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Code Intel - Query patterns + graph analysis.md
  - docs/reference/analysis/subsystem-backport-wave-1-review.md
last-verified: 2026-04-12
status: active
---

# Code Intel - Query patterns + graph analysis

## Summary

This note captures the practical query shapes built on top of the code-intel spine.

## Common expansions

- docs for a symbol: `mentions` edges from `section_id` to `anchor_id`
- tests for a symbol: `tests` edges
- callers and callees: `calls` edges, best-effort

## Graph signals

- high fan-in
- many downstream dependents
- sparse doc mentions
- bounded reachability via recursive CTEs

## Usage guidance

- Keep traversal depth bounded.
- Prefer deterministic ordering and explicit limits.
- Treat these queries as analysis helpers, not the canonical storage model.

## References

- [[Code Intel - Data model (anchors, sections, edges)]]
- [[Code Index - Unified SQLite DB]]
