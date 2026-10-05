---
type: ReferenceDoc
summary: "Implementation-grounded walkthrough of the full indexing pipeline from discovery through cache, ingest, embeddings, and graph scores."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Indexing pipeline - End-to-end walkthrough.md
last-verified: 2026-04-12
status: active
---

# Indexing pipeline - End-to-end walkthrough

## Summary

Rhizome indexing is a rebuildable pipeline with four main layers: live cache, intel ingest, embeddings, and graph-derived signals.

## Stages

- runtime cache discovers and refreshes notes under unified ignore rules
- note ingest creates doc sections and `mentions` edges
- code indexing creates anchors plus `defines`, `calls`, and `tests` edges
- coderef extraction contributes code-to-note document links
- note and code embeddings build retrieval aids on top of the intel spine
- graph-score passes derive persisted ranking priors for docs and anchors

## Operating rule

Persistent indexes are rebuildable truth. Live updating is an optimization layer on top.

## Related

- [[Indexing pipeline - rzm index orchestration]]
- [[Indexing pipeline - Live updating (watcher runtime)]]
