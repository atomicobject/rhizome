---
summary: "Bounded second untangle pass, preserving existing contracts and repairing discovered special cases."
reference-kind: analysis
---

# Second untangle pass

The user requested a second pass after the first-pass Sol loop and PR. PR #101 is open; both Sol rounds approved its behavior. Baseline for this pass is `4c08a5980eaa2088f097f2fe242e31987261b779`, with code identical to reviewed `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`. Both optional comment and test cleanup remain selected. This is follow-on maintenance under the existing behavior-preserving scope, not a reopening of the completed effort or a new schema, persistence, authority or release design.

## Work and ownership

Six fresh audit lanes found the bounded groups below. Their reports give callers, smallest changes, verification and explicit retentions. Each worker owns only its listed files; root owns cross-lane coordination, documentation, integration, test execution and publication. No worker runs builds or tests while another edits.

- [Core](core.md), C2-01 through C2-03: remove unused Store/path APIs; use `strings.Cut` at the two existing slash splits; remove adjacent narration. Preserve active transactions, migrations and matching policies.
- [Query](query.md), Q2-1 through Q2-4: remove unused sort adapter; use canonical interface traversal behind the existing Node/Note policy; share the exact unordered metadata prefix predicate; remove impossible asset-probe error branches. Characterize query behavior before consolidation.
- [Data flow](dataflow.md), I2-01 through I2-07: use the canonical path-language owner and selected indexer identity; remove retired seed/answer/codeintel/residual/progress APIs and direct their useful tests through production paths. Preserve raw-note fallback, live watching, ranking and writer semantics.
- [Runtime](runtime.md), R2-RT-1 through R2-RT-8: share graph-path preparation and MCP environment parsing; remove unused tag/runtime/credential APIs; require EOF for task-check JSON; centralize Go event vocabulary; synchronize the cancellation test on a real probe. Keep vendor/runtime-specific policy at its current boundary.
- [Browser and desktop](web.md), W2-1 through W2-4: remove unused declarations; advertise the existing preference event and reuse the browser event owner; use shared POSIX argument quoting; share only the two matching invalidation-stream transport loops. Runtime owns Go constants, web owns the capability list and TypeScript references. Preserve distinct global/node framing and endpoint admission.
- [Tooling](tooling-service.md), T2-TOOL-1 through T2-TOOL-4: route stale command removal through the existing generated-file ownership plan; share compression activation policy; delete three unused helpers. Preserve unrecorded/edited user files, recorded cleanup reporting, billing adapters, release authorization and source-template ownership.

## Verification sequence

Before production fixes, reproduce the init ownership, task-check trailing JSON, missing preference capability and parser-timeout language findings. Establish existing GraphQL interface behavior with literal public-path assertions. Root runs focused probes while owning workers pause their files; then workers implement and root reruns the same checks. Preserve useful existing tests, replacing self-confirming or obsolete-API tests only when the production route retains their behavioral assertion.

Inspect the integrated diff and direct/reverse imports. Run focused package tests, `make check`, and `make verify` with the pinned tools; verify generated init output and a disposable real init sequence. Run relevant desktop checks and an unprivileged real-shell fixture for command quoting, with no administrator prompt. Validate documentation and frozen scope. Fresh reviewers then search the changed boundaries for remaining worthwhile issues; fix concrete findings and repeat their relevant checks. The second pass stays local until reviewed and verified, then updates PR #101.

## Limits and decisions

No new dependency, universal registry, storage migration, wire rename, release or deployment. Existing first-pass retentions remain binding. External Go consumers of removed unused exports remain unverified and need migration to active APIs. Local source/caller evidence is authoritative here; some Rhizome lookups failed authentication, so successful semantic retrieval is not claimed. Six source audits do not prove every source line was manually reviewed.

## Gate-discovered transport defect

The pre-commit suite exposed a sporadic exit 1 in the simulated compounding credential-stop test. Fifteen isolated reruns passed, so a passing rerun was not accepted as a fix. A separate deterministic fixture showed that an early-exiting CLI child could trigger an unhandled stdin EPIPE in the shared client. The supplemental fix records the stdin error, preserves a nonzero child's own diagnostic, and rejects an otherwise successful response when the request was not delivered. Canonical template source and generated output must remain synchronized; no provider/model/budget policy changes.

## CI follow-through

PR #101's earlier head exposed Windows shutdown and trust-fixture failures plus Linux supervisor-fixture failures. Complete the existing contract across those platforms: normalize Windows ERROR_ACCESS_DENIED and the EINVAL returned after Wait releases its handle, only after the exact child is reaped; make the trust read-error fixture use a directory at the actual record path; explicitly timestamp synthetic ready manifests and observe restart rejection during the Vite-shutdown test. Preserve production discovery checks, timeout values, other kill errors and fail-closed trust. Verify local focused behavior, cross-compile the Windows tests and require the updated CI jobs to pass.
