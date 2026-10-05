# Search test ownership

- Verify provider requests and input-to-vector order through local fake HTTP. Counts alone do not prove batching or retry output.
- Test concurrency caps with more work than the cap, blocked requests, and both lower and upper bounds. Use inputs where age and default policy change the outcome.
- Prove sync writes by reading persisted IDs and vectors after `Sync` or `SyncPaths`. Keep legacy and unified store modes separate; do not manufacture persisted receipts.
- Let generic packer and writeback tests own shared scheduling and error behavior. Adapter tests own their protocol, configuration, and source-owned persistence.
- Keep explicit retry-budget, cancellation-drain, schema-upgrade, read-only metadata, and generation-visibility contracts.
- Trace executable and documentation references before deleting test support. Preserve documented extension APIs independently of private test-only shims.
- Embedding plan skip tests assert `CacheHits` as well as `TotalWork`. `TotalWork` excludes cache hits, so discarded chunk states still plan zero work.
