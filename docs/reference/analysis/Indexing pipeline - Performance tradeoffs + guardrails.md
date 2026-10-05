---
type: ReferenceDoc
summary: "Checklist of performance guardrails for unified indexing so throughput improvements do not regress correctness or UX."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Indexing pipeline - Performance tradeoffs + guardrails.md
last-verified: 2026-04-24
status: active
---

# Indexing pipeline - Performance tradeoffs + guardrails

## Summary

Unified indexing deliberately overlaps parse, embed, and queued-write work, but several barriers remain load-bearing for correctness and observability.

## Guardrails

- keep call graph rebuild work behind an explicit final barrier, but keep primary code semantic chunks call-insensitive so they stream before that barrier
- prefer in-memory same-run state for touched paths instead of rereading fresh sqlite rows
- let discovery backpressure on worker capacity instead of building huge hidden queues
- keep code-ingest writer policy separate from semantic writeback policy
- treat writer priority as a bounded scheduling hint, not a new correctness barrier
- trust wall-time metrics before cumulative diagnostics when judging regressions
- preserve the low-noise default progress UX; move detail into `--timings`
- track primary chunk counts, provider texts/batches, vector count, and database size so enrichment cost stays visible
- cap factual enrichment inside each source-owned code chunk; enrichment must never create additional provider texts or displace authored/doc/source content
- suppress direct field-anchor vectors in code embeddings; field names should remain searchable through compact module-level `Fields:` signals, and stale field-owned chunks must be pruned eagerly during sync.
- stream ontology-node primary embeddings after note ingest and ontology projection are current, including fallback untyped-note bodies; pack them through the shared note-provider lane
- run raw note embedding eligibility after ontology assessment is current: ontology-ready repos use ontology-node chunks for typed notes and internal fallback untyped notes, then prune any old raw `doc_section` semantic chunks; raw note embeddings are reserved for ontology-unavailable indexing
- use provider-advertised defaults for unset embedding batch/concurrency values before falling back to generic defaults
- use larger full-scan embedding batches than watcher batches; full scans lift max request size to provider ceilings but keep a lower adaptive minimum so provider concurrency can warm quickly without forcing every steady-state call to stay half-full. Code full scans use a bounded `256` adaptive warm-up after field-vector suppression; note/ontology throughput work keeps `512`.
- dispatch embedding batches through one adaptive threshold in both prepared pipelines and shared nodes; drain can go below `MinTexts`, warm-up can use the adaptive floor, aging can lower the threshold, and sustained load should pack toward provider ceilings instead of flooding all slots with small calls.
- report provider capacity/fill (`provider_capacity`, `provider_inflight_utilization`, `batch_fill_ratio`, `bytes_fill_ratio`) before changing concurrency; compare `calls_needed_at_provider_cap`, `calls_needed_at_dispatch_floor`, and `provider.dispatch_threshold_texts` to tell provider-cap packing apart from dynamic floor-triggered dispatch
- dedupe identical texts inside a provider batch and report input-vs-provider batch sizes so repeated generated surfaces do not silently burn embedding throughput
- split shared-node wait (`embed.enqueue_wait`, `embed.node_pack_wait`, `embed.node_slots_wait`, `embed.future_wait`, close drain) so phase wall time can be separated from provider wall time
- split primary ontology-chunk planning/rendering, embedding, writeback, and prune metrics so provider timing names only real provider work and queue wait
- reuse persisted primary embeddings when final text and source/context/format/provider fingerprints still match
- after a clean ontology sync, skip primary follow-up work entirely; when ancestor/context changes affect descendants, scope rendering/pruning to the deterministically affected owner set
- compare scoped ontology-node rows/chunks before writeback so unchanged typed notes do not rewrite node owners/chunks or fan out to embedding state checks beyond the scoped path set
- keep automatic `VACUUM` gated by SQLite freelist stats, not elapsed time or WAL size; `ANALYZE`, `PRAGMA optimize`, and checkpointing remain the cheap always-run post-index maintenance
