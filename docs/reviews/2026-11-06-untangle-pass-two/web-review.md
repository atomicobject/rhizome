---
summary: "Fresh second-pass web integration review."
reference-kind: analysis
---

# Second-pass browser, desktop and event integration review

**Findings: none.** No actionable regression, missed caller, or worthwhile surplus was found in the reviewed second-pass changes.

Read-only source review against `4c08a5980eaa2088f097f2fe242e31987261b779`, including current tracked changes and the new runtime constants, stream helper and tests. Read the second-pass plan, browser/runtime audits, delivery evidence, and `/tmp/rhizome-untangle-pass2-implemented-web.md`. The source files were frozen for integration review; other agents and root retained their own work.

## Boundaries inspected

- **Event vocabulary and capabilities:** runtime constants preserve all former Go wire values and the reconciliation-failure reason. Bootstrap producers, the derived scheduler, serve readiness/refresh consumers, web publishers and finder invalidation use the new owner. `view_preferences.changed` appears in both advertised global stream/support lists; the invalidation and node-event categories retain their prior membership. No old web/bootstrap constants or removed runtime accessor callers remain in repository Go sources.
- **SSE transport and ownership:** the shared helper preserves response headers, initial connected comment, 20-second heartbeat, JSON payloads, event IDs, flushing, marshal-error skipping, closed-channel termination and request cancellation. Global frames retain the named `event:` line; node frames remain unnamed. Each handler still owns method/streaming admission, node resolution where applicable, subscription release, view-folder watching and runtime holds. Agent and index-job stream implementations remain separate.
- **Browser and kit:** the preference event keeps its existing spelling and now comes from `vaultEvents.ts` for subscription, preference refresh and standalone kit forwarding. Hosted forwarding, origin/source checks, reconnect handling and event classification are unchanged. No public kit export was removed by this pass.
- **Desktop link commands:** `shellquote.Quote` provides the same literal POSIX arguments for the command's link directory, link path, expected prior target and replacement. Existing mutation order and exact prior-target refusal remain intact. The administrator command is still a single AppleScript argument. The rewritten tests execute the generated command in temporary unprivileged fixtures and retain swapped-link/file preservation, relative-target removal, permission fallback and cancellation coverage.
- **Deletion/test cleanup:** the table constant and private broker convenience methods have no remaining callers. Removed LiveRuntime accessors have no repository callers; phase waits, snapshots, stored configuration and active accessors remain. New SSE tests use literal named/unnamed frames, cancellation and a closed event channel rather than deriving expected output from the helper.

## Evidence limits

I performed source/diff/caller inspection only. I did not run tests, builds, a browser, desktop commands, providers, commits or publication. Baseline failures and shell/capability results described in the supplied evidence belong to root; they are not independent executions from this review.
