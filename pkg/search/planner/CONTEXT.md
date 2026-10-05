## pkg/search/planner

Translates QuerySpec into a Plan (retrievers + ranker + packer). Handles seed expansion and intent-based weight tuning.

- **Entry point**: `Planner.Plan(ctx, spec)`
- **Key invariant**: planner builds plans, doesn't execute; seed expansion happens before retriever construction

### Deep docs

- [[Search (Hub)]]
