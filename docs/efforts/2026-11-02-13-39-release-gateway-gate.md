---
type: EffortNote
id: EFF-2026-11-02-13-39
aliases: [EFF-2026-11-02-13-39]
name: Release gateway deployment gate
created-at: 2026-11-02T18:55:18Z
status: active
summary: "Deploy the exact release server source and verify it before any supported client publisher uploads artifacts."
governing-specs: ["[[evidence-grounded-release-orchestration]]"]
---

# Release gateway deployment gate

## Scope

Implement the approved [release deployment plan](../reference/analysis/rhizome-team-api-release-gate-plan.md). Server source follows `release`; trusted workflow code and AWS authorization stay on `main`. Cover guided publication/resume, direct Make commands, S3-only retries, and the Actions fallback. Preserve private/public source isolation, existing runtime settings and service identity. No client release is cut or published as a test.

## Spec Set (Frozen)

- [[evidence-grounded-release-orchestration|SPEC-0078]], 2026-11-02 revision, limited to the new gateway publication requirements and the existing canonical release boundary they extend.

## Stories In Scope (Frozen)

- [[evidence-grounded-release-orchestration#^SPEC-0078-US4|SPEC-0078 US4]] supplies the existing canonical-ref publication contract.
- The gateway gate is a requirements-only increment under [[evidence-grounded-release-orchestration#requirements|Requirements]]. Existing evidence generation, version selection, and desktop packaging behavior remain outside this increment.

## Spec Coverage Checklist

- [ ] Exact release source and private/public committed build-input agreement.
- [ ] Trusted-main deployment, source/request correlation and resumable receipts.
- [ ] Every supported publisher waits, then rechecks release topology before uploads.
- [ ] Preflight/dry-run modes remain read-only.
- [ ] Independent review, required CI and deployment-only acceptance pass.

## Plan

Execute the approved plan in three batches: source selection and correlation; integration across publishers/workflows; verification, reviewed merge, and deployment-only acceptance. Preserve the first-release flow that creates a missing release branch during the canonical atomic push. Remove the main-push gateway deployment trigger. Existing retained-image rollback stays available.

## Plan Approval

- 2026-11-02: Drew said "Let's do that" after confirming that deployment currently runs from main and that switching server source to release, before client publication, was pending. This approves the linked plan and the bounded scope above. No client publication is authorized as a test. The local current-user setting is absent, so it was not inferred or changed; approval is recorded here from the explicit conversation.

## Original Intended Delivery

Client publication cannot run ahead of its intended gateway source. Operators use the existing release commands and receive a precise resume command after a failed or uncertain deployment. No manual SHA selection or AWS credentials are added to private release workflows.

## Actual Delivered

Implemented on `t3/rhizome-release-gateway-gate`: exact public/private committed source agreement, pinned resumable deployment receipts, trusted-main workflow separation, and gating across guided/direct GitHub, S3 and fallback publishers. Ordinary main pushes no longer trigger the new workflow. Production remains on the previously verified provider-budget image until this change lands and its deployment-only acceptance runs. No client release was cut or published.

## Execution Notes

- Source selection, correlation, workflow and publisher implementations have focused synthetic coverage. Independent review found two edge cases, now fixed with regressions: unsupported Docker context mounts and stale saved-success handoffs. Docker context mounts/parser directives fail closed; process ancestry and start identity limit receipt borrowing to the live publication invocation, with older receipts starting a fresh attempt.
- The repository has no release branch yet. The existing first-release atomic push remains its creation point; no branch or client release is created merely for verification.
- Local Rhizome identifier allocation was delayed by this task's background semantic indexing. Stopping only that task-owned runtime allowed the allocator to complete and return this identifier. Live ontology discovery succeeded; broader runtime-backed context retrieval timed out, so direct owning source/spec reads supply that evidence gap.

- Verification: `python3 -m unittest discover -s scripts/release -p '*_test.py'` passed 311 tests. After adding a real child-process handoff regression, the final core suite passed all 20 tests. Deployment helper tests passed all 15 tests; actionlint 1.7.7 passed both workflows; Bash syntax, file-size and credential gates passed. `./scripts/rzm validate` passed with 0 issues. Frozen-scope validation reports only the pre-existing unrelated SPEC-0111 drift in EFF-2026-10-03-18-33.
- The first integrated Python run exposed historical test setup still assuming flat checkout ontology files and a missing desktop test dependency. The synthetic schema fixture now reads the current module schemas while retaining its intended historical flat layout, and locked desktop dependencies were installed. First-release fixtures inject the new deployment seam and assert the release branch exists before requesting deployment. Production ontology and signing behavior are unchanged.

## Deviations

None beyond the explicit removal of automatic main-push deployment needed to implement Drew's release-source decision.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

None recorded yet.

## Status

Active. Local implementation and verification are complete; current-head Greptile, CI, reviewed merge and deployment-only acceptance remain. No client publication is part of this acceptance.
