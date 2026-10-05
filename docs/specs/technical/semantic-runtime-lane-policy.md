---
type: TechnicalSpec
summary: "Defines the semantic runtime lane policy contract for compatible embedding work: lane keys, provider defaults, packer promotion, source/latency classes, shared gates, observability, and forbidden ad hoc batchers."
id: SPEC-0044
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0044
  - semantic-runtime-lane-policy
  - Semantic Runtime Lane Policy Contract
---

# Semantic Runtime Lane Policy Contract

## Summary

`pkg/app/semanticruntime` is the policy boundary for indexing-time embedding provider work. It turns indexing source and latency intent into packer options, maps compatible providers into shared lanes, applies provider-advertised defaults plus internal lane caps, and gives code, note, ontology primary, and intent exemplar work one provider execution node when their provider/model/endpoint/dimension contract is compatible.

This spec complements [[indexing-pipeline-architecture]], [[indexing-workflow]], [[Embeddings - indexing pipeline]], [[Embeddings - providers + configuration]], and [[Indexing pipeline - Performance tradeoffs + guardrails]]. Runtime behavior is unchanged by this document; the goal is to freeze the lane policy contract so future optimization work does not recreate independent batchers or split compatible work across invisible provider queues.

## Goals

- make the lane compatibility key explicit enough that code, note, ontology primary, and intent exemplar work can safely share provider packing
- distinguish provider defaults from internal lane caps for `batchSize` and `maxConcurrency`
- define low-latency, watcher burst, CLI path, and full-scan packer policy without turning every threshold into a public user knob
- preserve one shared gate for aggregate provider concurrency when code and note lanes are active
- keep provider packing, mixed-domain batching, dedupe, wait, and fill metrics observable under truthful phase names
- forbid ad hoc provider batchers in indexing paths that should use `semanticruntime`

## Non-Goals

- changing current runtime thresholds, provider caps, or embedding behavior
- specifying search-time query embedding behavior outside indexing-time provider lanes
- making code and note embeddings share a lane when model, endpoint, provider, or dimensions differ
- replacing provider-local HTTP retry/rate-limit behavior
- exposing every packer knob through CLI or MCP configuration
- changing semantic chunk-family ownership or SQLite writeback semantics

## Requirements

### Compatibility Invariants

- Indexing-time embedding work MUST flow through `pkg/app/semanticruntime` when a shared runtime is available. This extends [[indexing-pipeline-architecture#^SPEC-0012-US1-AC2]].
- Compatible code, note, ontology primary, and intent exemplar work MUST converge onto one `semantic.SharedEmbeddingNode` instead of constructing separate provider-only batchers.
- The runtime MUST keep separate lanes when the compatibility key differs. Separate lanes are required when code and note embeddings use different models, endpoints, providers, or effective dimensions.
- `Runtime.CodeLane`, `Runtime.NoteLane`, and `Runtime.IntentLane` are lookup aliases over lanes; they are not separate provider queues. Intent exemplars SHOULD use the code lane when one exists, otherwise the note lane.
- Lane sharing MUST NOT change semantic ownership: code chunks still write to code embedding stores, ontology primary chunks still use `owner_type=ontology_node`, and raw `doc_section` chunks remain compatibility-only for ontology-unavailable runs.
- Runtime close MUST drain each shared node once, in reverse creation order, so late provider work is not abandoned during final semantic flush.

### Lane Compatibility Key

- The lane compatibility key is `(provider, model, endpoint, dimensions)` as normalized by `semanticruntime.KeyFor`.
- Provider names MUST be lowercased and trimmed before comparison.
- Endpoints MUST be trimmed and trailing slashes removed before comparison.
- Models MUST be trimmed before comparison; model aliases or provider-specific equivalence MUST NOT be inferred by the runtime.
- Dimensions MUST prefer the live provider's `Dimensions()` value when a provider is available; otherwise they use configured dimensions from `ProviderConfig`.
- The compatibility key MUST NOT include source class, lane kind, path count, packer thresholds, batch size, or max concurrency. Those inputs can promote lane policy, but they do not make otherwise-compatible provider work incompatible.

### Provider Defaults And Runtime Policy

- Persisted embedding config MUST NOT expose `batchSize` or `maxConcurrency`; if legacy config files contain those fields, loaders MUST ignore them and writers MUST omit them.
- Provider defaults are the normal source of request batch/concurrency limits before falling back to generic Rhizome defaults. See [[Embeddings - providers + configuration]].
- Provider constructors own provider-specific hard caps. Examples in current code include Voyage's 1000 text batch cap and 32 request concurrency cap, OpenAI's 2048 text batch cap and 250 default concurrency, and Ollama's local defaults/caps.
- `semantic.EffectiveBatchSize` and `semantic.EffectiveMaxConcurrent` are the shared default resolution helpers; indexing paths SHOULD NOT duplicate their fallback logic.
- `semantic.ApplyProviderPackerLimits` may raise full-scan packer ceilings to provider-advertised request limits, but watcher/low-latency packers MUST stay conservative unless source policy explicitly promotes them.
- Internal `LaneRequest.BatchSize` remains a hard cap for non-configured callers that construct a lane directly; embedding config loaders MUST NOT populate it from vault config.
- Provider default examples are implementation snapshots, not normative public caps. Verify `pkg/search/embeddings/provider_*.go` before quoting exact OpenAI, Voyage, or Ollama limits in diagnostics or docs.

### Packer Merge And Promotion Rules

- `PolicyFor` MUST derive packer policy from `IndexRequest.Source`, `PathCount`, and `LatencyClass`.
- `LatencyThroughput` MUST force throughput packers; `LatencyLowLatency` MUST force low-latency packers; `LatencyAuto` MUST derive behavior from source and path count.
- `IndexSourceFullScan` under auto policy MUST use throughput packers.
- `IndexSourceWatcher` under auto policy MUST use low-latency packers until the watcher burst threshold is reached, then use watcher-burst packers.
- CLI/path-scoped work under auto policy SHOULD promote to throughput when the path count reaches the promotion threshold.
- Throughput packers currently use larger request ceilings and longer waits than low-latency packers; watcher-burst packers sit between those classes. These thresholds are implementation policy, not a public compatibility key.
- When an existing compatible lane receives a stronger incoming policy, merge MUST be monotonic for throughput: max concurrency rises to the largest effective value, `MinTexts`, `AdaptiveMinTexts`, `MaxTexts`, `MaxBytes`, and `MaxWait` rise to the larger compatible values, then explicit batch-size caps are re-applied.
- Lane promotion MUST reconfigure the existing shared node instead of replacing it or creating a second node.

### Source And Latency Classes

- `IndexSourceFullScan` represents whole-vault or whole-code-root indexing where provider throughput matters more than immediate per-file latency.
- `IndexSourceCLIPaths` represents user-selected path sets; it may promote to throughput for broad path sets while keeping small sets responsive.
- `IndexSourceWatcher` represents live file-change refresh; it defaults to low-latency packing but promotes watcher bursts to avoid flooding provider slots with tiny batches.
- `LatencyAuto` is the default caller posture and should preserve source-derived behavior.
- `LatencyLowLatency` is for explicit responsiveness-sensitive refresh work.
- `LatencyThroughput` is for explicit batch throughput work such as full scans or rebuild-like syncs.

### Shared Gate And Provider Execution

- When both code and note syncers are active, indexing MUST create one shared gate sized to the larger effective max concurrency across the active providers.
- The shared gate limits aggregate `EmbedTexts` concurrency across active code/note lanes even when compatible work is split into separate lanes because the key differs.
- The gate is in addition to each lane's shared-node `MaxConcurrent`; it is not a replacement for provider-local caps or provider-local retry/rate-limit behavior.
- Ontology primary work MUST reuse the note lane's provider, shared node, batch/concurrency values, and gate.
- Intent exemplar sync MUST reuse an existing shared lane and MUST fail rather than silently creating an intent-only provider path when no lane exists.

### Observability Counters And Timing

- Provider calls MUST be attributed to semantic phase names that describe the owning work, such as `embed_code`, `embed_notes`, `embed_ontology_nodes`, or the lane phase selected for intent exemplars.
- Shared-node observability MUST preserve logical text counts, provider input counts, provider call counts, batch text/byte sizes, batch fill ratios, bytes fill ratios, provider capacity, inflight gauges, queue age, submit/enqueue wait, node pack wait, slot wait, gate wait, future wait, and close-drain wait where applicable.
- Mixed-domain batches MUST report that they were mixed and MUST charge each contributing phase with its share of provider calls/texts so phase summaries remain explainable.
- Deduping identical texts inside a provider batch MUST be observable through saved text/byte counters.
- Final drain metrics MUST distinguish provider execution from queue/writeback wait so `--timings` does not label planning, queueing, or SQLite writes as provider latency.
- New provider-lane policy work SHOULD add regression tests for lane compatibility, promotion, explicit config caps, and metric attribution when behavior changes.

### Forbidden Ad Hoc Batchers

- Indexing paths MUST NOT call provider `EmbedTexts` directly for code, note, ontology primary, or intent exemplar work when a shared runtime lane is available.
- New indexing domains MUST NOT instantiate standalone `BatchExecutor`, `Syncer`, or provider-only goroutine pools that bypass `semanticruntime` compatibility, gate, packer merge, and observability policy.
- New provider default/cap fallback logic MUST NOT be copied into indexing packages; use provider constructors plus `semantic.Effective*` helpers.
- New full-scan optimizations MUST NOT widen raw `doc_section` embeddings for typed notes; source-owned ontology body chunks remain the primary ontology-ready note surface.
- New watcher or CLI path refresh code MUST NOT mark note/code last-sync state until shared-node embedding work and queued semantic writes have drained successfully.

## Open Questions

- Whether watcher and CLI path source classes need published latency targets once live refresh metrics are stable enough to compare across machines.
- Whether mixed-domain provider batches should expose per-domain batch fill histograms outside `--timings`.
- Whether provider-specific rate-limit feedback should eventually feed packer thresholds, or remain inside provider implementations.
