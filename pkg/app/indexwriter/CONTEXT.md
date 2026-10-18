## Single indexing writer

Owns the bounded durable queue shared by batch and live indexing. Producers submit typed work; handlers are the composed store/service adapters.

- Shared batch-policy DTOs live in `search/embeddings`; producer queue interfaces remain in `search/semantic`. Indexing composition checks that the writer implements those ports, keeping this package independent of semantic orchestration.
- `Writer`, `Handlers`, and `New` preserve per-kind rows, bytes, idle flush policies, code priority, bounded backpressure, synchronous source-cache callbacks, prompt cancellation, and observable phase attribution.
- Ownership transitions, structural finalization, graph scores, post-index database maintenance, derived dirty marking/activation/acknowledgement, and exact ownership acknowledgement are ordered controls. Each waits for its captured submit barrier and flushes earlier payloads before performing its specific operation.
- Raw metadata drains before ontology deltas. Ontology deltas drain before standalone node read models because a full rebuild can truncate their parents.
- Derived dirty generations become durable before structural modification. Activation follows structural durability; acknowledgement follows destination durability and clears only exact current generations.
- The ordered kind table owns policy, handler availability and durable flush order. Ordered phase segments preserve same-kind submission order; urgent thresholds count all pending segments. The writer returns missing-handler errors rather than panicking.
- Domain-specific controls share a private typed envelope and acknowledgement wait. A committed result wins against simultaneous cancellation or exit; close is idempotent.
- Cancellation abandons undrained work, retains durable recovery obligations, and waits for in-flight handlers before resource cleanup. Providers and file workers compute outside store transactions; the queue performs no embedding calls.
- Metrics record actual handler flushes, attempted rows/bytes, committed rows and finite outcomes. Flush/close barrier spans include waiting for captured submissions and durable drains. Queue wait measures admission separately from deferred scheduling; ownership acknowledgement counts only committed exact generations. The constructor's context retains the operation identity and collector through the writer drain.
