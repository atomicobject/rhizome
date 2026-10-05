## Semantic runtime lane policy

Owns indexing-time embedding provider lanes: source/latency policy, compatible lane keys, provider-default packer limits, shared gates, and shared-node metrics.

- **Entry points**: `PolicyFor`, `Runtime.EnsureLane`, `Runtime.SyncIntentExemplars`.
- **Key invariants**: compatible provider/model/endpoint/dimension work shares one node; provider defaults and runtime policy control batch/concurrency; persisted vault config must not feed batch-size/concurrency caps into lanes.
- **Provider lifetime**: lanes borrow supplied providers. The application factory owns optional cache closers and closes providers after its runtime lanes have drained, including compatible providers that share one node.
- **Boundary**: lane sharing changes provider execution only, not chunk ownership or SQLite writeback; code, ontology body, card, and intent work still report their owning phases.

### Deep docs

- [[semantic-runtime-lane-policy]]
- [[Embeddings - indexing pipeline]]
- [[Embeddings - providers + configuration]]
