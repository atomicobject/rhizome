---
type: ReferenceDoc
summary: "Optional cache-side coderef scanning that keeps code-to-note references incrementally fresh."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Code reference scanning in cache.md
last-verified: 2026-09-11
status: active
---

# Code reference scanning in cache

## Summary

`cache.Service` can incrementally maintain code-to-note references so retrieval surfaces do not need a full repo rescan on every request.

## Contracts

- code-ref scanning is opt-in through cache options
- initial crawl builds note resolution state before scanning candidate code files
- discovery prunes ignored directories and code-excluded subtrees before expanding file globs; filtering only the final matches repeatedly walks build caches and dependencies for every source extension
- the first live note read can wait for this cache crawl through link-location lookup, even when the persisted index is ready; keep discovery bounded so background code scanning does not add unnecessary delay
- content-only markdown edits do not require coderef rescans
- deleted or renamed notes only rescan files that previously referenced them
- new notes rebuild note resolution state, then scan likely candidate files instead of the whole repo

## Related

- [[Vault cache service (Service)]]
- [[Ignore + exclude rules]]
