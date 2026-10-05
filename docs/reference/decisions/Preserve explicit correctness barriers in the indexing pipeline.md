---
type: ReferenceDoc
reference-kind: architecture
summary: "Keep explicit correctness barriers in the indexing pipeline where later stages depend on final cross-file state, even when more streaming overlap would look faster."
status: active
decision-domain: architecture
tags: [type/decision, subsystem/indexing, subsystem/intel, subsystem/embeddings]
derived-from:
  - docs/decisions/Preserve explicit correctness barriers in the indexing pipeline.md
last-verified: 2026-04-12
---

# Preserve explicit correctness barriers in the indexing pipeline

## Context

Some indexing work is safely path-local and streams well. Other work depends on a stable cross-file view:

- call-edge-sensitive semantic planning
- reverse-index fallback behavior
- deferred rebuild stages
- global derived signals

Past regressions came from treating those boundaries like accidental overhead and pushing more work past them for extra overlap.

## Decision

Preserve explicit correctness barriers wherever downstream stages require final cross-file state.

The pipeline should overlap early work aggressively, but it should stop and consolidate when correctness depends on:

- the full changed-path set
- final call-edge and reverse-index state
- final ownership of derived rows
- globally recomputed graph-style outputs

## Consequences

- The pipeline remains a hybrid of streamed and staged work, not a fully streaming DAG.
- Throughput tuning must respect barrier semantics; more overlap is not always an improvement.
- Documentation should call out which stages are safe to move earlier and which are not.

## Follow-ups

- [Indexing pipeline architecture](../technical/indexing-pipeline-architecture.md)
- [Indexing pipeline - Performance tradeoffs + guardrails](../../reference-notes/Indexing pipeline - Performance tradeoffs + guardrails.md)
- [Indexing pipeline - rzm index orchestration](../../reference-notes/Indexing pipeline - rzm index orchestration.md)
