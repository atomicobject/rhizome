---
type: ReferenceDoc
summary: "Review of the first subsystem backport wave into the spec-driven structure: what became canonical, what source material it preserves, and what backlog slices remain."
reference-kind: analysis
derived-from:
  - docs/specs/technical/semantic-code-index-spine.md
  - docs/specs/technical/indexing-pipeline-architecture.md
  - docs/specs/technical/structural-node-model-and-ontology-read-path.md
  - docs/specs/technical/coderef-contract.md
  - docs/reference/guides/code-anchors-frontmatter-syntax.md
  - docs/reference/analysis/code-anchors-matching-scopes.md
  - docs/reference/analysis/code-anchors-watcher-incremental-updates.md
  - docs/reference/guides/code-anchors-language-support.md
  - docs/reference/analysis/documentation-binding-rationale.md
  - docs/specs/product/ontology-browser-workspace.md
  - docs/specs/experience/ontology-browser-navigation-model.md
  - docs/specs/technical/semantic-code-index-spine.md
  - docs/specs/technical/indexing-pipeline-architecture.md
  - docs/specs/product/ontology-browser-workspace.md
  - docs/specs/experience/ontology-browser-navigation-model.md
last-verified: 2026-04-12
status: active
---

# Subsystem backport wave 1 review

## Summary

This wave converted the strongest recent subsystem material into canonical spec-driven notes without forcing unstable families into the ontology prematurely.

Canonical technical specs now cover:

- the semantic code index spine
- the indexing pipeline architecture
- the structural node / ontology read path
- the coderef contract

Canonical supporting reference docs now cover:

- documentation-binding rationale
- ontology browser workspace product framing
- ontology browser navigation model
- code-anchor authoring, matching, watcher, and language-support guidance
- the high-signal code-intel domain and analysis notes now promoted from `docs/reference-notes/`

## Preservation check

- The former structural-node legacy spec is preserved primarily through [Structural node model and ontology read path](../../specs/technical/structural-node-model-and-ontology-read-path.md) plus [Ontology browser navigation model](../../specs/experience/ontology-browser-navigation-model.md).
- The former ontology-browser legacy plan is preserved primarily through [Ontology browser workspace](../../specs/product/ontology-browser-workspace.md) and [Ontology browser navigation model](../../specs/experience/ontology-browser-navigation-model.md).
- The indexing/code-intel cluster preserved stable-handle, path-normalization, single-writer, correctness-barrier, and backpressure contracts in the new technical specs plus the canonical domain/analysis reference docs.
- The documentation-binding cluster preserved the coderef contract and the bidirectional binding rationale while leaving code-anchor note-family design for a later dedicated pass.
- The high-signal legacy code-intel notes are now canonical in:
  - `[[Code Intel - IDs + path normalization]]`
  - `[[Code Intel - Data model (anchors, sections, edges)]]`
  - `[[Code Index - Unified SQLite DB]]`
  - `[[Code Intel - Query patterns + graph analysis]]`

## What remains in backlog

- keep the code-anchor ontology-family decision deferred; the support docs are canonical now, but the sidecar note family remains intentionally non-canonical
- either fix or reshape the starter's richer product/experience story contract before creating many canonical notes of those types
- retire or archive obsolete change-shaped legacy specs after their durable content is backported

## Recommended next slices

- the indexing decision trio is now canonical in `docs/reference/decisions/`
- retire or archive the source `docs/reference-notes/` copies for the code-intel notes only after the consuming links have been migrated
