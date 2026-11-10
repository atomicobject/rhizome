# Manual Greptile review

The first committed review ran with `greptile review --json` at `46e792283d3781057f247056ed12a247ce0b3460`. It returned confidence 4/5 with two findings. Greptile inspected source; it did not run tests or inspect screenshots.

## G1: ambiguous specification sections

Confirmed. Singular User Stories and Acceptance Criteria fields could return multiple sections while the adapter treated their combined contents as complete. The focused counterexample failed before the fix (`userStories/duplicate`, expected incomplete, received complete).

The shared child-read boundary now preserves returned prose and nodes while making coverage incomplete for field issues or multiple singular sections. Existing eligibility and prompt composition then refuse copied scope. Four counterexamples cover duplicate sections and field issues for both singular fields. Clean sources remain copyable. The adapter and pure library pass all 25 focused tests. The installed regression checks disabled story selection and Copy while full criteria remain readable.

## G2: required recipe presence

Confirmed. A filtering loop could omit removed or renamed recipes. The test now indexes recipes and explicitly requires both `effort-execution-context` and `closure-drift-pack` before executing their cases. All 16 named public-query cases pass. The Go wrapper also registers `reads.ts` and `queries.ts` as cache inputs, so future adapter edits invalidate cached test results.

Follow-up verification passed: strict template TypeScript, `make check`, `make build`, all 55 installed Delivery browser cases, documentation validation and frozen-scope drift. The added installed case preserves readable authored criteria while disabling selection and Copy. The initial gate caught four spacing errors in the new test; those were corrected before the passing gate. No retry, timeout or skip changed.

A subsequent committed-head manual review and CI remain required. No finding is represented as externally accepted yet.

## G3: coverage belongs to the source parent

The second review at `83a6195aa1b960a1f1b7197a310ec8eddcc95944` confirmed G1 and G2 resolved, but returned 4/5 for a reachable consequence: aggregating field issues across all parents disabled clean neighboring stories. Root reproduced the failure before changing the adapter.

Child hydration remains batched. Field issues and singular-section cardinality now contribute coverage through their owning parent. Each spec receives its own story-section evidence; each story receives its own criterion-section evidence. Ambiguity in one story does not disable a clean sibling, and ambiguity in an active or archived spec does not disable a clean neighboring spec. Transport and generation failures remain explicit read failures. The expanded focused cases pass while preserving the original refusal and readable prose checks. Installed acceptance additionally selects a clean neighboring story while the ambiguous source remains unselectable.

Follow-up strict template TypeScript, all 25 focused tests, `make check`, `make build`, and all 55 installed Delivery cases passed. The installer lifecycle cases also pass with the Windows newline assertion correction. A subsequent committed-head manual review and current-head CI remain required.
