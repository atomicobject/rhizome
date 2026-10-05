---
type: ReferenceDoc
summary: "How embedding providers are configured and invoked, including OpenAI, Ollama, and Voyage defaults, batching, concurrency, and model inheritance."
reference-kind: guide
derived-from:
  - docs/reference-notes/Embeddings - providers + configuration.md
last-verified: 2026-04-12
status: active
---

# Embeddings - providers + configuration

## Summary

Rhizome supports multiple embedding providers behind one provider contract. Configuration chooses the provider, endpoint, models, batching, concurrency, and optional overrides for code versus note embeddings.

## Practical rules

- note and code embeddings can share provider/endpoints while using different default models
- model changes should trigger safe re-embedding rather than silent mixed-model indexes
- Ollama defaults and auto-normalization exist to keep local setups workable without hand-tuning every endpoint path
- provider batching and concurrency are the main throughput controls

## Operator checklist

- confirm provider and endpoint
- confirm model choices for note and code embeddings
- rely on provider defaults for request batching and concurrency; persisted config does not expose `batchSize` or `maxConcurrency`
- lower byte caps when a provider rejects large inputs

## Related

- `[[Embeddings - indexing pipeline]]`
