# Manual Greptile review

The first committed review ran with `greptile review --json` at `46e792283d3781057f247056ed12a247ce0b3460`. It returned confidence 4/5 with two findings. Greptile inspected source; it did not run tests or inspect screenshots.

## G1: ambiguous specification sections

Confirmed. Singular User Stories and Acceptance Criteria fields could return multiple sections while the adapter treated their combined contents as complete. The focused counterexample failed before the fix (`userStories/duplicate`, expected incomplete, received complete).

The shared child-read boundary now preserves returned prose and nodes while making coverage incomplete for field issues or multiple singular sections. Existing eligibility and prompt composition then refuse copied scope. Four counterexamples cover duplicate sections and field issues for both singular fields. Clean sources remain copyable. The adapter and pure library pass all 25 focused tests. The installed regression checks disabled story selection and Copy while full criteria remain readable.

## G2: required recipe presence

Confirmed. A filtering loop could omit removed or renamed recipes. The test now indexes recipes and explicitly requires both `effort-execution-context` and `closure-drift-pack` before executing their cases. All 16 named public-query cases pass. The Go wrapper also registers `reads.ts` and `queries.ts` as cache inputs, so future adapter edits invalidate cached test results.

Follow-up verification passed: strict template TypeScript, `make check`, `make build`, all 55 installed Delivery browser cases, documentation validation and frozen-scope drift. The added installed case preserves readable authored criteria while disabling selection and Copy. The initial gate caught four spacing errors in the new test; those were corrected before the passing gate. No retry, timeout or skip changed.

A subsequent committed-head manual review and CI remain required. No finding is represented as externally accepted yet.
