---
type: EffortNote
id: EFF-2026-11-03-08-50
aliases: [EFF-2026-11-03-08-50]
name: Jev model compatibility
created-at: 2026-11-03T13:58:32Z
status: complete
summary: "Use the stable Jev alias in compounding and accept explicit Jev releases through gateway validation and accounting."
governing-specs: ["[[session-memory-starter]]", "[[team-api-gateway]]"]
---

# Jev model compatibility

## Scope

Use `jev-latest` for compounding screening and ranking, including model resolution, estimates, usage accounting, and generated installations. Accept the stable alias and explicit numeric Jev release IDs through gateway validation and billing, retaining request validation, per-person/global limits, provider allowances, conservative holds, and other providers' policies.

Provider-source selection belongs to the related active thread and is excluded. Mining campaigns, live provider calls, deployment, and installed-app updates remain outside this effort. The subsequent full-send instruction authorizes carrying the implementation through review, pull request, and merge.

## Spec Set (Frozen)

- [[session-memory-starter]]: model profile, provenance, and cost accounting, amended for the requested stable alias.
- [[team-api-gateway]]: gateway contract and admission/budgets, amended for explicit Jev releases.
- Source baseline: `996c25118e26e990107ead22942b134f977241a8`.

## Stories In Scope (Frozen)

- [[session-memory-starter#^SPEC-0124-US1|SPEC-0124 US1]]: screening/ranking model binding and cost reporting only.
- [[team-api-gateway#^SPEC-0129-US2|SPEC-0129 US2]]: accepted Jev model identifiers only.
- [[team-api-gateway#^SPEC-0129-US4|SPEC-0129 US4]]: preserve budget admission, reservation, and settlement for those identifiers.

## Plan

The initiating request explicitly authorized implementing these three numbered scope items and relevant regression tests. Its direction was: "Use jev-latest for compounding, and don't reject specific jev versions."

1. Confirm routing ownership and canonical template locations.
2. Change the shipped Jev binding and alias price; retain repository pins and historical measurement attribution.
3. Share a bounded Jev identifier policy between gateway validation and price reservation. Apply the alias's credit charge to unlisted releases while retaining explicit overrides.
4. Prove regressions before fixes, test generated installation and the synthetic mining pipeline, run repository gates, and obtain independent review.
5. Under the subsequent full-send approval, run sol-loop, complete Greptile review, open a pull request, resolve applicable findings and CI failures, and merge through the normal repository gate.

## Original Intended Delivery

A local, reviewable implementation with regression tests and durable contract updates. Publication and deployment require their separate repository workflows.

## Spec Coverage Checklist

- [x] Canonical module profile and installed skills describe `jev-latest`.
- [x] Alias cost estimates and actual usage remain budgeted.
- [x] Explicit repository pins retain precedence.
- [x] Alias and explicit releases pass gateway validation and billing.
- [x] Malformed/unrelated IDs remain rejected; Voyage keeps its exact policy.
- [x] Per-person/global/provider limits and conservative unknown-outcome accounting remain covered.
- [x] Provider-source selection remains with its existing owner.

## Actual Delivered

Profile version 3 selects `jev-latest`; the cost table prices the alias at the existing reviewed input rate. Gateway validation and reservations share a bounded numeric-release grammar. Unlisted releases inherit the alias's shared-credit charge, and receipts preserve the requested model ID.

Tests verify forwarding, settlement, admission limits, uncertain holds, explicit charge overrides, model resolution, and alias pricing. Fresh installation and a synthetic installed pipeline run pass.

Delivery: [PR #76](https://github.com/atomicobject/rhizome/pull/76), carrying implementation commit `79930b0584590c5846d46e8d59ad7535b04710fc`. The merge gate rechecks the final closure commit; deployment and installed-app updates remain excluded.

## Execution Notes

- 2026-11-03T14:21:01Z: closure prepared for PR #76. Greptile reviewed `86500a1113e6c2010cb667449c112d78b244ea6b` at 5/5 with no comments; all five required CI checks passed on that head. The final documentation commit will receive its own required checks and Greptile recheck before merge. No behavior changed after Sol approval.

- Sol-loop round 1 reviewed head `79930b0584590c5846d46e8d59ad7535b04710fc` against `6da57df811c551a4cd9f9b0f59104122873afa13`: APPROVE, no blockers or should-level findings. Reviewer: Work/Codex, `gpt-6.1-sol`, `xhigh`; usage not reported. Ledger: S1, nit, deferred as the pre-existing optional Jev 1.14 pin-price gap outside the default-alias request. No behavior changes were needed. Behavior diff remains the reviewed version; documentation adds this record.
- Rebased on main `6da57df811c551a4cd9f9b0f59104122873afa13`, whose intervening changes are limited to unrelated group views. Compounding Node suite and gateway race suite passed after rebase.

- 2026-11-03T14:08:02Z: user invoked sol-loop and full-send, authorizing review, PR, and merge. Refreshed origin/main is still `996c25118e26e990107ead22942b134f977241a8`; the existing full-gate and installed-fixture evidence covers the unchanged implementation.

- Confirmed the related provider-source thread reports its implementation complete and still under review. No source-selection code changed here.
- Canonical profile has moved to `pkg/app/cli/init/templates/starters/compounding/module/models.json`; edits use module/template sources, not generated skill copies.
- Red phase: the starter tests reproduced its old default and zero alias estimate; gateway tests reproduced `model_not_allowed` and the independent unpriced-model failure.
- Verified [TypeSafe's model documentation](https://docs.typesafe.ai/models) on November 3, 2026: explicit version IDs are accepted even when omitted from the model list; Jev 1.13 and 1.14 share the existing $0.042 per million input tokens, free output, and 64k request context. The alias follows stable releases. This is compatibility work, not a new quality comparison.
- `node --test --test-reporter=dot pkg/app/cli/init/testdata/compounding/*.test.mjs`: passed.
- `make build`: passed after allowing dependency downloads; no deployment occurred.
- Fresh temporary `rzm init` with compounding, Claude and Codex, search off, and an external binary manager, followed by `rzm init --check`: passed. Installed profile bytes equal the canonical profile; version 3 selects `jev-latest`; installed alias pricing charges $0.000042 for 1,000 synthetic input tokens.
- Running the installed `mine` module with synthetic sessions and fake provider clients completed, with all 23 screening/ranking items using `jev-latest` and the manifest recording that binding.
- Gateway `go test -race ./...` and `go vet ./...`: passed. Only synthetic providers were used.
- Independent combined-code review: APPROVE, no actionable findings. It independently passed the two focused alias regressions.
- `make check-full GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4 CHECK_JOBS=2`: passed, including race-enabled Go tests, all 1,762 web tests, repository content/security checks, and both integration layers. The first unrestricted-concurrency run hit an unrelated 300 ms harness-shutdown timeout; that test passed three isolated race-enabled reruns, then passed in the full lower-concurrency run.

## Deviations

The subsequent user instruction, "$sol-loop and $full-send", extends delivery from a local diff through reviewed PR and merge. It does not authorize a production deployment or installed-app update. Implementation scope is unchanged.

The first full-gate attempt hit sandbox restrictions in an existing synthetic credential test; the local-access rerun is the applicable result.

Final `./scripts/rzm validate` passed with zero issues. `./scripts/rzm validate frozen-scope-drift` exited 1 for the pre-existing active effort `docs/efforts/2026-10-03-18-33-type-briefing-and-stage-reading-views.md`, whose frozen SPEC-0111 predates that spec's October 6 update. Neither that effort nor `docs/specs/technical/group-views-and-view-platform.md` changed here. Classification: unrelated/pre-existing; no historical scope was rewritten. Initial Rhizome retrieval had no indexed enrichment in this fresh worktree; known owning contracts were read directly. Identifier allocation required repairing/refreshing the local projection before recording this effort.

## Compounding Follow-ups

Historical benchmark pins remain fixed to their measured releases. Sol S1 identified the existing missing compounding price for an optional `jev-1.14.0` pin. It is outside the requested default-alias path and is deferred; the shipped alias and existing `jev-1.13.0` pin are priced. Gateway billing accepts and accounts for both explicit releases.

## Closure Checklist

- [x] Required implementation and focused regressions delivered.
- [x] Generated installation and synthetic mining behavior verified.
- [x] Independent review completed.
- [x] Full repository gate passed.
- [x] Final documentation validation passed with zero issues. Frozen-scope validation reports no drift for this effort; its one repository-wide issue is recorded below.
- [x] Sol-loop approval on the final behavior diff: one round, GPT-6.1 Sol, xhigh.
- [x] Greptile 5/5 with no findings on the reviewed implementation head; final documentation head is rechecked by the merge workflow.
- [x] Carrying PR #76 and green required CI on `86500a11` recorded; final-head CI remains an enforced merge gate.

## Status

Implementation, local verification, and review are complete in PR #76. The full-send workflow owns final-head checks and merge. Deployment and installed-app changes remain excluded.
