---
type: ReferenceDoc
summary: "How semantic similarity can be blended with lightweight graph features when ranking results."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Embeddings - ranking + graph blend.md
last-verified: 2026-04-12
status: active
---

# Embeddings - ranking + graph blend

## Summary

Embedding similarity can be blended with lightweight graph signals such as direct links, shared tags, or two-hop overlap when retrieval needs a broader relevance model.

## Contracts

- graph blending is an additive ranking layer, not the primary truth source
- the weight split between semantic and graph scores must stay explicit and tunable
- graph bonuses should remain lightweight enough that exact lexical or handle-based matches are not drowned out
