## Agent-ready answer packet assembly

`pkg/app/answer` turns ranked search results into answer packets for `rzm search` and `rzm agent semantic-query`: `mustRead`, supporting evidence, coverage, confidence, and next queries.

- **Entry points**: `Build`, `RenderText`
- **Key invariants**: retrieval/ranking happen upstream; this package selects role coverage from existing evidence and must not invent sources. Explanatory queries require docs + code, not tests unless tests are requested.
- **Boundary**: no filesystem walks, embedding calls, ontology projection, or structural-context resolution here; upstream callers pass canonical `nodeRef` and link-target provenance in.
- **Diagnostics**: answer traces describe role normalization, must-read/supporting selection, specificity decisions, coverage, confidence, and next-query generation from already-adapted inputs.

### Deep docs

- [[search-answer-workflow]]
- [[search-quality-evaluation-corpus]]
- [[unified-search-answer-architecture]]
- [[search-diagnostics-explain-architecture]]
- [[Search - Answer engine packets]]
- [[Search (Hub)]]
