---
type: ReferenceDoc
summary: "Operational invariants for semantic embedding sync: adaptive dispatch, shared nodes, source-owned primary chunks, and writeback batching."
reference-kind: analysis
last-verified: 2026-04-28
status: active
---

# Search - Semantic embedding sync invariants

Use this when changing `pkg/search/semantic`. `CONTEXT.md` should stay short; these are the deeper invariants.

## Dispatch and provider lanes

- Staged embedding dispatch treats `MinTexts` as an adaptive floor, not a fixed send threshold.
- Provider defaults own unset batch/concurrency knobs; generic fallbacks are only for providers without caps.
- Throughput packers lift `MaxTexts` and byte ceilings to provider limits but keep `MinTexts` as the adaptive floor.
- Full-scan prepared pipelines and shared nodes share one policy: drain below `MinTexts`, warm bounded floor-sized batches, lower thresholds as queued work ages, and require provider-cap fullness under sustained load.
- Diagnostics must separate theoretical provider-cap calls from actual dispatch thresholds: `calls_needed_at_provider_cap`, `calls_needed_at_dispatch_floor`, and `provider.dispatch_threshold_texts`.

## Shared embedding nodes

- Shared embedding nodes dedupe identical text inside one provider batch and expand vectors back to every caller.
- Outer prepared pipelines should leave provider gates and provider call metrics to `SharedEmbeddingNode`.
- Use `dedupe_saved`, input-vs-provider batch metrics, `embed.enqueue_wait`, `embed.node_pack_wait`, `embed.node_slots_wait`, `provider.latency`, and `embed.future_wait` before changing concurrency.

## Source-owned code chunks

- Primary code semantic chunks are call-insensitive; call graph facts belong in specialized structural retrieval lanes.
- Direct field-anchor vectors are suppressed for throughput; fields remain discoverable through compact `Fields:` signals in module chunks.
- Factual enrichment is globally bounded and dropped before authored/doc/source text when a primary chunk reaches its size cap.

## Notes and ontology bodies

- Ontology body chunks are the normal note semantic surface when ontology is available.
- Typed notes and fallback untyped notes both flow through ontology-node body sync.
- Raw `doc_section` note embeddings are compatibility-only for ontology-unavailable runs.
- Compatible note/code/ontology embedding domains may share a provider lane when they target the same provider/model surface.
- Scoped ontology-node sync should compare generated node/chunk rows with existing rows before writing.

## Writeback and cache

- Code sync prep should resolve reuse from batched/prefetched hash cache state before point DB lookup.
- Owner call summaries should prefetch once per language for planning; avoid per-file owner lookup batches.
- Queue-mode code writeback should batch before enqueue and preserve producer phase attribution.
- Legacy note chunk cleanup must travel with note chunk upserts on the same queued/batched write lane.
- Embedding cache rows are write-once by content hash during sync; once a hash exists, note/code sync should reuse it.
