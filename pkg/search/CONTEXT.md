## pkg/search

Unified search pipeline: retrieval → ranking → packing. Orchestrates multiple retrievers, blends evidence, renders budgeted context.

- **Entry point**: `Service.Search(ctx, spec)`
- **Diagnostics**: each recorder-backed search owns a correlated child operation and installs a bounded collector when absent. Quiet start and terminal events retain duration, outcome, and result count. Detailed reports are written only for calls taking at least 250 ms, failures, cancellations, panics, or observed fallback/degradation; fast healthy calls skip snapshot serialization and report publication. Existing collectors and text timings are reused; `metrics_scope=context` explicitly identifies cumulative caller metrics, while `search_operation` identifies a new collector. Outcomes disclose fallback/degraded evidence, cancellation, and panics without persisting queries, seeds, results, or error text.
- **Key invariant**: same-handle candidates merge evidence; `MaxPerOwner` limits per-note/file results; ontology-node chunks must have catalog identity before they become candidates; exact-symbol filters preserve ranked anchor identities through module rollup.
- **Rationale evidence**: `rationale_fts` is support-only. It can add `rationale_fts_match` evidence to an anchor/file found by another retriever, but support-only candidates are filtered before ranking so code comments do not become standalone search results.

### Deep docs

- [[Search (Hub)]]
