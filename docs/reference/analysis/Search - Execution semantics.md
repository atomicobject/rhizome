---
type: ReferenceDoc
summary: "Execution semantics for the unified search pipeline: retriever ordering, timeouts, fallbacks, evidence merging, and packing."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Search - Execution semantics.md
  - docs/reference-notes/Search - Seed expansion strategies.md
last-verified: 2026-04-12
status: active
---

# Search - Execution semantics

## Summary

`Service.Search()` runs a unified pipeline: retrieval, ranking, rollup, then packing. Planning happens earlier, but execution semantics determine how Rhizome behaves under deadlines, how it merges evidence, and how seed expansion turns partial user intent into concrete search work.

## Runtime behavior

- retrieval runs progressively in priority order when the context has a deadline
- partial results may return on timeout instead of failing the whole search
- ranking and packing have fallback modes so timeout does not produce an empty answer when usable evidence already exists

## Retrieval and expansion

- fetch-like intents bias execution toward direct definition/test/call-edge evidence first
- text-only and seed-only queries use different retriever mixes
- directory seeds expand into bounded file sets, then into notes and anchors where appropriate
- seed expansion stays intentionally small and biased toward high-centrality or directly linked surfaces

## Ranking invariants

- candidates merge by stable handle before ranking
- ranking uses max evidence per evidence type rather than naive summation
- owner diversity constraints prevent one file or note from dominating the result set

## Packing and budgets

- packers choose chunk bodies or file spans depending on the available indexed detail
- context budgets scale with query count, but remain bounded
- continuation tokens are the paging contract, especially for broad multi-query work

## Answer assembly

The agent-facing default is an answer packet, not a bare ranked list. `pkg/app/answer` runs after ranking and before rendering. It does not retrieve more data; it chooses role coverage from existing ranked evidence, reports missing slots, and emits follow-up queries. Keep this layer deterministic and cheap so broad search stays low-latency.

Answer packet budgets must account for duplicated preview text in `mustRead` and `supporting`, not only the rendered body. Small budgets should degrade to short labels and empty previews rather than returning oversized JSON.

## Practical debugging heuristic

When a result is missing, check in this order:

1. whether the planner selected the right retrievers for the intent
2. whether seeds expanded into the expected files, notes, or anchors
3. whether evidence merged under the expected handle
4. whether diversity or budget limits packed it out of the final response
5. whether answer role selection omitted it because a stronger item already filled that role
