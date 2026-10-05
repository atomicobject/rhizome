---
type: ReferenceDoc
summary: "Why the unused backlinks and graph memoization layer was removed."
reference-kind: analysis
derived-from:
  - docs/reference-notes/AnalysisCache (backlinks + graph memoization).md
last-verified: 2026-09-22
status: active
---

# AnalysisCache (backlinks + graph memoization)

## Summary

`cache.AnalysisCache` was removed because no production request consumed its memoized backlinks or graph results. Its only graph caller was startup prewarming, which computed and stored a result that no reader used.

## Current behavior

- Web backlinks use `obsidian.CollectBacklinks` directly.
- CLI graph operations use `actions.GraphAnalysis` and `actions.DocGraphAnalysis`; web graph responses have their own cache.
- Long-running bootstrap still warms the note cache asynchronously and drains that worker before closing resources.
- The unused `CompressionCache` and its exclusive `SnapshotProvider` interface were also removed; production runtimes never configured that cache.

## Related

- [[Vault cache service (Service)]]
- [[Dirty tracking + Refresh semantics]]
