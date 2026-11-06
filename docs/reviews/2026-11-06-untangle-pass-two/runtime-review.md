---
summary: "Fresh second-pass runtime integration review."
reference-kind: analysis
---

# Second-pass runtime and tooling integration review

**Verdict: APPROVE, source review. No findings.**

Reviewed the current tracked diff against `4c08a5980eaa2088f097f2fe242e31987261b779`, the untracked implementation/test files in this lane, the second-pass plan and audit reports, delivery evidence, and both worker implementation summaries. No tests, builds, provider calls, credential reads, commits, or repository edits were performed. Root-owned verification is not claimed as independently run.

## Boundaries checked

- **Generated command ownership.** Followed the new recorded-file enumeration through classification, preview, grouped decisions, application, reporting, and record updates. Only recorded immediate children absent from current templates enter the removal plan. Edited files stay without a positive decision; declined removals stay declined; unrecorded prefixed files are not candidates. Accepted removal uses `os.Remove` on the command path, retaining the target of a direct symlink. The new tests exercise all five command/prompt directories through `Run`, unchanged preview bytes, repeated apply, grouped accept/refuse, and direct symlink targets. Existing directory-symlink behavior and generic generated-file ownership mechanics are retained.
- **Task output and admission.** The typed decode and unknown-field rejection are unchanged; requiring `io.EOF` from the next token now rejects trailing closers, garbage, and a second value. Traced failures through scheduled runs, drafts, rechecks, and explicit task action paths: each returns the failed outcome before agent work. The untracked public-path test covers scheduled/draft behavior and verifies zero agent calls.
- **Harness MCP environment.** Compared `MCPServer.Environment` with both deleted parsers: first-equals splitting, empty values, duplicate precedence, whitespace-key rejection, and errors are preserved. Claude retains its JSON config and validation; Codex retains its own argument encoding and stricter server-name rules. Shared and vendor-specific tests use literal expected values. No session startup, cancellation, process ownership, or permission behavior changes accompany this extraction.
- **Runtime events and readiness.** Checked bootstrap publishers, derived-work publication, serve refresh/admission consumers, web publishers, and public capability values. Event strings and the reconciliation-failure reason match their prior values. Failed reconciliation still invalidates retained validation and suppresses a replacement refresh. The removed accessors had no remaining source callers; underlying state, waits, snapshots, and active capabilities remain. The cancellation test observes the fake runtime's real health-handler probe before cancelling and retains bounded completion plus no-spawn assertions.
- **Compression and retired APIs.** Compression warning activation now calls the existing helper with equivalent nil/unset/false/default/normalization behavior, retaining its broader provider-key lookup and warning fallback. Tag deletion removes only the two uncalled file-target entrypoints; active whole-vault mutations and file-target addition retain their validation and lease handling. Removed credential/default exports have no remaining repository callers.
- **Graph-path integration.** The shared graph-fact preparation retains endpoint aliases, last-alias precedence, edge order/filtering, canonical lookup, ontology relation naming, and confidence mapping. CLI/MCP admission and result/error formatting remain at their original boundaries.

## Limits

This is a bounded source review, not independent runtime or gate evidence. Root still owns the focused regressions, full gates, disposable init sequence, and any real-interface verification. External consumers of retired Go exports were not examined, as recorded in the approved scope.
