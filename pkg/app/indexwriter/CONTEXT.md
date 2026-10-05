## Single indexing writer

Owns the bounded durable queue shared by batch and live indexing. Producers submit typed work; handlers are the composed store/service adapters.

- `Writer`, `Handlers`, and `New` preserve per-kind rows, bytes, idle flush policies, code priority, bounded backpressure, callback timing, cancellation drain, and observable phase attribution.
- Ownership transitions, structural finalization, graph scores, derived dirty marking/activation/acknowledgement, and exact ownership acknowledgement are ordered controls. Each waits for its captured submit barrier and flushes earlier payloads before performing its specific operation.
- Raw metadata drains before ontology deltas. Ontology deltas drain before standalone node read models because a full rebuild can truncate their parents.
- Derived dirty generations become durable before structural modification. Activation follows structural durability; acknowledgement follows destination durability and clears only exact current generations.
- Intent snapshots publish through the same writer. Providers and file workers perform computation outside store transactions; the queue performs no embedding calls.
- Metrics record actual handler flushes, attempted rows/bytes, committed rows and finite outcomes. Flush/close barrier spans include waiting for captured submissions and durable drains. Queue wait measures admission separately from deferred scheduling; ownership acknowledgement counts only committed exact generations. The constructor's context retains the operation identity and collector through the writer drain.
