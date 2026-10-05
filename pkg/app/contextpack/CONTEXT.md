## Budget-aware text packing for LLM context windows

Greedy bin-packing of `Piece` structs with optional LLM compression when content exceeds budget.

- **Entry points**: `Pack`, `PackWithIntent`, `PackDetailed`, `TrimToBudget`, `TrimToBudgetDetailed`, `TrimMarkdown`
- **Key invariant**: priority-first selection; first piece always included (trimmed if needed); stable tie-break is priority, score, key.
- **Budget**: package fallback is 70k chars; config/explicit caller budgets override it, and local `rzm agent` applies its own 150k floor.
- **Delivery evidence**: `PackDetailed` returns exact retained piece text; `TrimToBudgetDetailed` returns the unchanged trim result plus retained original-byte count, excluding its optional indicator. Callers carry that evidence through enclosing assembly before publishing session fingerprints. Compression status does not prove exact raw-body delivery.
- **Callers**: `vault_context`, `file_context`, `semantic_query`

### Deep docs

- [[Contextpack (Hub)]]
- [[Contextpack - Budget surfaces]]
- [[Intent-driven compression]]
