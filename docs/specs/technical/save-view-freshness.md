---
type: TechnicalSpec
id: SPEC-0109
summary: Keep edited views stable through save, publish readable changes promptly, and let interactive saves interrupt background indexing safely.
spec-status: active
last-updated: 2026-10-02
aliases:
  - SPEC-0109
---

# Save and view freshness

## Summary

Saving an ontology edit session already refreshes the changed files in the metadata and ontology read models. Views must retain their displayed edits until a query started after successful persistence supplies the committed result. Interactive saves must not wait for unrelated query refetches or unbounded background computation.

## Goals

- Preserve displayed values and ordering across successful saves.
- Notify other readers when committed data is available.
- Reduce measured save latency while preserving validation and durable writes.
- Give configured views and custom view apps the same save lifecycle.

## Non-Goals

No additional indexing trigger, general client-side GraphQL interpreter, persisted freshness protocol, database migration, or writer-lock replacement.

## Requirements

- Separate dirty edits from committed changes awaiting a fresh view result. A response begun before commit acknowledgement must not retire the retained display state.
- Preserve edits made while save or refresh is in flight. Failed and conflicted saves retain their edits and visible errors. Discard clears the intended dirty edits without resurrecting an older session.
- End the saving state and release the mutation queue once persistence completes. Read refresh failure must remain distinguishable from persistence failure.
- Configured views retain local field values and placement until canonical adoption. Custom views reuse the shared query/session lifecycle and may supply an explicit optimistic updater for their own result shape.
- Reference translation belongs to the retained read snapshot that produced an edit. A reader adopting canonical data retires its mappings without affecting other pending readers. Fresh references after external source changes remain unchanged. Queued edits may use verified mappings from commits that completed after the edits were captured.
- Freshness events announce affected paths and domains only after successful read-model and cache publication. Committed saves with a failed refresh must not claim that readers are current.
- Background semantic and graph processing eventually sees saved files even when foreground cache refresh consumes dirty paths. Stream invalidation and reconnection trigger fresh reads.
- Background indexing observes interactive priority promptly, stops accepting new work, and drains already accepted durable writes before releasing the existing writer lease. Cancellation must not lose queued writes or falsely report completion.
- Optimize projection and postcheck work only when measurements identify redundant or overly broad work. Preserve relation-dependent validation and the required read-model closure.

## Verification

Cover save before the staged read returns, stale responses, rapid consecutive saves, edits during save, discard, failure/conflict, slow refetches, freshness events, and background cancellation. Compare the same save workload before and after. Record visible-state continuity, persistence acknowledgement, canonical query adoption, and background convergence separately.
