APPROVE

Reviewed only the canonical-reply guard, selection-refresh acknowledgement, and diagnostic ownership needed by those changes. No verified reachable findings.

- reads.ts:67-70 compares explicit requested public ref strings with returned public ref strings. It does not compare SourceLinkRef fingerprints with canonical NodeRef structure. An omitted explicit canonical response becomes a shared failure. Host refs without an explicit public ref continue through SDK completeness.
- reads.ts:61-78 accepts diagnostic ownership only for exact requested strings. Missing or foreign ownership remains shared. Truncation and unavailable generation still block completeness. children() assigns coverage to each parent's requested refs without poisoning healthy siblings with another request's owned diagnostic.
- stage.tsx:227 disables the reduction acknowledgement while stale. Its Page caller supplies retained-error or fetching state. The selected snapshots remain intact until an explicit enabled acknowledgement filters them. Copy is already blocked by the same stale state.
- Reviewed the SDK readNodes implementation, coverage composition, data hooks, caller wiring, pure fault regressions, canonical-ref regression, and held-refresh regression. The held-refresh test releases the request in finally, waits for the acknowledgement to re-enable, then verifies US1 remains and US3 is removed from the prompt.

Evidence: independently inspected /private/tmp/ae-selection-reduction-red.log, which records expected-disabled/received-enabled before the fix. Independently inspected /private/tmp/ae-g8-e2e-final.log, which ends with 64 passed (1.0m), including the held-refresh regression and four healthy-neighbor cases. The 27 pure-test pass is supplied by the brief; I inspected those tests but did not rerun them.

Limits: no competing build, gate, browser session, repository edits, delegates, or external actions. /private/tmp/ae-g8-foundation-review.md was absent. Rhizome context-shard lookup reported authentication unavailable, so live retrieval evidence was unavailable. Approval covers only the requested deltas, not overall PR readiness.

Type System Discipline guided checking the adapter's optional unknown requestedRef against the SDK error contract and its runtime narrowing, rather than assuming ownership was a string.

Review ran in-thread on codex/gpt-6.1-sol high, as requested. No child agents.
