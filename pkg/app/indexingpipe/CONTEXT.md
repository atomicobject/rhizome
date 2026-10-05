## Bounded file discovery and read workers

Shared indexing file walker for note/code candidates. It classifies paths under ignore/default-skip rules, backpressures on the real worker queue, reads content in workers, and records filesystem/queue timings.

- **Entry points**: `CountFiles`, `ProcessFiles`.
- **Key invariants**: no unbounded walk-ahead backlog; vault-root-relative path normalization only; queue wait metrics should reflect worker readiness, not producer residence. When callers provide ordinary and system visibility matchers, traversal prunes only paths rejected by both; the classifier must choose the applicable file policy.
- **Discovery completeness**: encountered walk errors propagate after accepted workers drain, so stale cleanup cannot interpret unreadable directories as deletions. CountFiles returns the same walk error instead of a partial total. Deliberate subtree pruning and legacy individual-file read tolerance remain unchanged; missing/non-directory selected roots remain empty.

### Deep docs

- [[Indexing pipeline (Hub)]]
- [[indexing-pipeline-architecture]]
- [[Indexing pipeline - Performance tradeoffs + guardrails]]
