---
type: ReferenceDoc
summary: "How the watcher runtime keeps indexes fresh: async bootstrap, dirty-path draining, incremental ingest, embeddings scheduling, and graph-score updates."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Indexing pipeline - Live updating (watcher runtime).md
last-verified: 2026-10-04
status: active
aliases:
  - Code Intel - Indexing + watcher integration
---

# Indexing pipeline - Live updating (watcher runtime)

## Summary

The live watcher uses the same structural indexing core as a batch run. Debounced filesystem events wake publication directly. Embeddings and graph scores converge separately from durable work records, including quiet retries after failures.

## Contracts

- startup must stay responsive; heavy init runs in background capability phases
- a background index on boot is best-effort and lock-coordinated
- note changes update intel sections, note embeddings, and note graph edges
- note deletes converge through the same live cleanup boundary as changes: codeanchor note rows, raw note embeddings, ontology primary chunks, graph edges/scores, and validation/web invalidation
- code changes update anchors, recompute scopes, queue code embeddings, and trigger graph scoring; full note reingest is reserved for cache resync/stale recovery paths
- multi-process runtimes coordinate watcher leadership separately from index-writing leadership
- leader/follower hints are invalidation hints, not proof of index convergence; path hints carry dirty kind so followers preserve delete and rename semantics, while malformed or truncated hints force resync
- each dirty drain owns a live epoch with started/completed/failed/degraded state; status surfaces report current and most recent epochs so API/UI callers can distinguish partial live convergence from completed background work
- structural publication has no per-path rewrite cooldown; bounded watch-hub coalescing collects event bursts and retains events received during a running job
- committed metadata, ontology, and code publication emits `node.changed` before provider completion; derived completion emits `index.changed`
- provider computation runs outside the writer lease; publication checks the work generation and structural source fingerprint under the lease
- inactive derived work forces structural recovery after interruption; active work retries independently of filesystem events

## Related

- [[LiveRuntime (async server bootstrap)]]
- [[Watcher design + degraded mode]]
