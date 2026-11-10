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

## G4: manual refresh includes selected scope

The third review at `4d40d2c21c0a4161f512124add2fe684d939b650` confirmed G3 resolved and returned 4/5 for selected-effort refresh. Root checked the public freshness callers rather than assuming every refresh was stale. The normal live-event path already refreshed selected details, and the first valid installed counterexample passed without a patch. With the live event feed unavailable, however, the same manual refresh made zero selected-detail reads and left Copy enabled against cached complete scope. That installed case failed before the fix. An earlier attempt with stale generated build output did not load the app and is setup evidence only.

Manual portfolio refresh now invalidates the selected-detail query prefix through the existing TanStack client. Selection query keys include the inventory graph generation. The stage combines inventory and selected-detail coverage before replacing fresh summaries or enabling Copy. Pending, failed, incomplete or mismatched reads remain blocked. The relationship caller supplies the same generation and qualifies cross-generation evidence.

The installed test holds a selected-detail response, confirms Copy stays disabled, returns a broken frozen-section field while status remains active, then restores normal responses and verifies recovery through another manual refresh. It passes with the event feed unavailable. Source files remain untouched. No retry, timeout or skip changed. The failed build-output setup, valid failing case and green case are retained separately in `/private/tmp/ae-selected-refresh-{setup-red,no-events-red,green}.log`.

Follow-up `make check` passed, including 2,169 web tests. `make build` and all 56 expanded installed Delivery cases passed. Strict transitive template TypeScript passed for both callers. Subsequent committed-head manual review and CI remain required.

## G5: extensionless wikilinks retain authored targets

The fourth review at `c6ac8827f2e1212dd78305fe1648f2d501e7a664` confirmed G4 resolved and returned 4/5 for extensionless wikilinks. Root reproduced the missing link to an existing HTML plan in a real archived-effort fixture. The old empty-link-list fallback appended `.md` before the server saw the authored wikilink.

The one-line correction uses the existing resolver's null-evidence mode for converted wikilinks. That mode decodes the URI while retaining the target and fragment unchanged, including malformed-percent handling. Markdown links keep their existing source-aware path. No new parser or shared platform behavior is added.

The installed counterexample fails before the fix and passes afterward: a unique extensionless target opens its actual HTML plan, an ambiguous basename remains unavailable, and both exact authored wikilinks reach the public preview endpoint. Existing mixed-format resolver tests already require ambiguity preservation. The fixture adds these links to archived-effort narrative without changing lifecycle, frozen selections or record counts. Strict template TypeScript, make check and make build passed. The expanded 57-case installed suite passed after correcting a test teardown race: Playwright was disposing intercepted responses before their handlers finished. The constraints suite now drains handlers with the existing documented unrouteAll wait mode. All assertions remain, and no retry, timeout, skip or error suppression changed. Post-teardown make check passed. Strict template TypeScript, make check, make build and all 57 installed cases also passed after exposing the existing parent-spec count. The refreshed 47 captures passed independent visual review, followed by root inspection of every changed capture after the count correction. Subsequent committed-head manual review and CI remain required.

## Final implementation verdict

`greptile review --json` at `1ebb77b3077ca48b0af38d47b2f660276fd4616f` returned confidence **5/5**, with **zero comments** and no new actionable findings. G1 through G5 are closed. This was source inspection, not a test run. All active CI checks passed at the same implementation head, including Windows web/CLI, Web E2E and desktop portability. The existing conditional installed-upgrade job was not applicable. [The readiness receipt](implementation-readiness.json) records the exact PR, checks and manual review result.

## G6: source-relative Markdown images

The documentation-head review at `0460bea1315f258b023eff10caf8b0cc2c32dfef` returned confidence 5/5 and one P2 comment: relative image paths resolved against the experience frame. Two installed browser cases reproduce the missing source fallback, one per theme (`/private/tmp/ae-image-fallback-red.log`).

The shared kit exposes no public source-relative binary attachment route. Delivery therefore shows the authored label and exact path with a canonical source button rather than a broken image. Absolute image URLs retain normal rendering. The installed cases verify the relative fallback, decoded absolute data image and source navigation. Strict template TypeScript, `make check`, `make build`, all 59 installed cases and documentation/frozen-scope validation passed. Root inspected both new captures. Manual review at `301faea690e22c0ec93337d652c776a4b5df9218` returned confidence **5/5** with **zero comments**, explicitly confirming the image finding resolved. All active CI jobs passed at that head. G1 through G6 are closed. [The final receipt](follow-up-readiness.json) retains the exact checks and review result. Subsequent documentation heads still receive their own review and CI.

## G7: Markdown filenames containing parentheses

Manual review at `cc10aeea35a0a15aa541649ae85125a92a7e3450` returned confidence 5/5 with one P2 comment. A valid Markdown link to `plan(v2).html` became unavailable because Delivery rebuilt the resolver input with unescaped Markdown destination delimiters. The shared resolver's simple destination parser stopped at the first closing parenthesis.

The installed counterexample opens the effort's actual original-source disclosure and follows an authored relative link to a real HTML fixture. Before the fix, the public preview request `[target](./harbor-legacy/plan(v2).html)` returned 404 and the link remained unavailable. After the fix, only literal parentheses are percent-encoded during Markdown-input serialization. The existing Markdown resolver decodes paths once, retaining canonical resolution, source-relative semantics and ambiguity. Existing escapes are left intact. No server parser or public contract changed.

The installed regression passes and verifies the exact encoded request plus the canonical HTML viewer content. Strict template TypeScript passes. `make check`, `make build`, all 60 installed cases and documentation/frozen-scope validation passed. The first broad attempt stopped on a missing blank line in the new test; the spacing correction passed lint and the full chain. Current-head manual review and CI remain pending.
