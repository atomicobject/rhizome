---
type: TechnicalSpec
id: SPEC-0121
aliases: [SPEC-0121]
spec-status: active
last-updated: 2026-10-17
summary: Correct the subsystem audit defects, consolidate duplicated semantics, and simplify package ownership with behavior and upgrade evidence.
---

# Critical subsystem refactor

## Summary

Implement the approved [[docs/reviews/2026-10-17-subsystem-review/README|subsystem audit roadmap]] against baseline `5251923fc1ac06ac6c9702eb3db25ca76da67d2f`. Each finding receives a recorded disposition supported by code and verification. A disproved finding is corrected in the delivery record rather than implemented as an unnecessary change.

## Goals

- Searches read existing indexes without migrations, metadata resets, or paid-cache loss.
- Note edits preserve unrelated source bytes and reject stale or ambiguous changes.
- Full and incremental indexing produce equivalent persisted facts and validation results.
- SQL, residual queries, overlays, and views share scalar, predicate, and ordering semantics.
- Request work scales with selected nodes and affected dependencies.
- Each shared concept has one owning implementation, with replaced code and its redundant tests removed.

## Non-Goals

The audit's uncovered subsystems receive only integration changes required by this work. This effort does not add unrelated features or authorize a release, deployment, merge, or desktop installation.

## Requirements

### Must

- Cover every production and test finding in the nine audit reports through implementation, consolidation, or an evidence-backed correction.
- Follow the audit's six-phase roadmap and its seven accepted recommendations. Preserve useful free-form search modes through the common inference implementation.
- Preserve paid embedding caches and authored files during ordinary opens, upgrades, cancellation, and failures.
- Keep original filesystem paths while folding link lookup keys by case and NFC. Colliding claims remain explicitly ambiguous.
- Encode DateTime comparison values as fixed-width UTC instants, preserving authored display text. Invalidate derived materializations when semantics change.
- Use the existing schema relation contract and authored wikilinks for frozen scope references. Plain identifier text does not create graph edges. Track unresolved link dependencies so target creation, deletion, identifier edits, and collisions converge incrementally.
- Use a shared Markdown syntax implementation for frontmatter boundaries, protected spans, and link scanning. Invalidate provider projections alongside parser changes.
- Consolidate source transactions with recovery for existing journal formats. Quarantine retains evidence and admission barriers for unstabilized paths. Unknown paths require a vault-wide barrier.
- Preserve exact write obligation acknowledgement and restart convergence when indexing is cancelled. Do not release resources before active handlers finish.
- Measure a consistent workload before and after performance changes. Retain quality and behavior checks alongside the measurements.
- Add meaningful regression, differential, upgrade, crash, and batching coverage. Delete low-signal tests together with the code or redundant contract they pin.
- Update owning subsystem documentation, change notes, generated contracts, callers, and fixtures in the same change set.
- Obtain Opus 5.5 High and Astra High reviews before dependent structural changes and on the integrated result.
- Pass the repository's applicable gates, open a reviewed pull request, force a Greptile CLI review, and address actionable findings until the current commit reaches 5/5.

## Open Questions

No product decision remains open from the audit. Implementation choices must preserve the requirements above and record evidence when an audit proposal is corrected.
