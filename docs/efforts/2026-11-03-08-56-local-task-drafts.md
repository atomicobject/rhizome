---
type: EffortNote
id: EFF-2026-11-03-08-56
aliases: [EFF-2026-11-03-08-56]
name: Discover and test local task drafts
created-at: 2026-11-03T13:57:46Z
status: active
plan-approved-by: Drew Colthorp
summary: "Show local task drafts in the selected worktree and test their captured source with separate data and visible results, while scheduled tasks continue to use the primary ref."
governing-specs: ["[[scheduled-tasks]]", "[[session-memory-starter]]"]
---

# Discover and test local task drafts

## Scope

Make the Tasks page useful while authoring a task on a branch. Discover new and modified local definitions, run a deliberate test against the selected checkout's captured source, and show its outcome and transcript. Keep automatic scheduling, task modes, and scheduled history based on the primary ref. Include the compounding installation requested in this conversation and the later authorized correction to Markdown/workflow verification coverage.

Excluded: model-default changes (owned by the separate Jev compatibility effort), automatic draft scheduling, a generic sandbox, publishing task output, and production deployment. The original verifier-guidance exclusion was superseded by Drew's explicit coverage-fix request below.

## Spec Set (Frozen)

- [[scheduled-tasks|SPEC-0127]], source at `996c2511`, SHA-256 `62e6f16fe7ff6c162dea6ded1b7caf166c710b8c1e74bedfba131fa6876aa48b`, with the local-draft amendments authorized by this plan.
- [[session-memory-starter|SPEC-0124]], initially limited to draft state isolation; amended by Drew on 2026-11-03 to include evidence-based Markdown/workflow admission and verification wording. Model defaults remain out of scope.

## Stories In Scope (Frozen)

- [[scheduled-tasks#^SPEC-0127-US1|SPEC-0127 US1]]: local draft discovery and explicit testing.
- [[scheduled-tasks#^SPEC-0127-US3|SPEC-0127 US3]]: preserve one scheduler and its shared execution lock.
- [[scheduled-tasks#^SPEC-0127-US4|SPEC-0127 US4]]: checks and agent work use the same captured source.
- [[scheduled-tasks#^SPEC-0127-US7|SPEC-0127 US7]]: draft tests do not advance scheduled clocks.
- [[scheduled-tasks#^SPEC-0127-US8|SPEC-0127 US8]]: visible draft outcomes and transcripts.
- [[session-memory-starter#^SPEC-0124-US1|SPEC-0124 US1]]: evidence-based verification eligibility for Markdown and workflow learnings, with unchanged provenance and quality safeguards.
- [[session-memory-starter#^SPEC-0124-US8|SPEC-0124 US8]]: draft task execution uses separate compounding state.

The story locators resolved with `requiresFix: false`. Individual acceptance-criterion locators need repair, so this effort freezes the durable parent stories rather than invented block IDs.

## Plan Approval

Drew authorized this plan and end-to-end delivery in chat on 2026-11-03: "$council this plan, then implement $full-send . Have opus 5.5 high do any frontend/design work". He then approved "Use the two available providers" for council. Opus 5.5 high owns frontend/design. The approval covers routine corrections from council and code review within this scope, verification, the pull request, and merge.

### Scope amendment

Drew subsequently asked: "fix the compounding skill's coverage of Markdown/workflow improvements" and "keep the existing isolation/Tasks work intact." The amendment covers canonical prompt and eligibility changes, behavior tests for both Jev ranking and unscreened fallback, an isolated replay against captured vault sources, all 17 held workflow dispositions, and anonymized examples. Existing live mining state must remain byte-identical. Eligibility is the goal, not a positive proposal count; redundant, unsupported and obsolete claims remain rejectable. Coordinate with the separate Jev compatibility effort and preserve its default/version changes when integrating.

## Spec Coverage Checklist

- [ ] US1: new, modified, invalid, and absent-local task states are clear without hiding scheduled tasks.
- [ ] US3 and US7: test execution cannot change scheduled clocks, modes, data, or task tags.
- [ ] US4: a captured source baseline includes local helpers and both check forms use that baseline.
- [ ] US8: outcomes, failures, and transcripts are visible, with bounded retention.
- [ ] SPEC-0124 US1: repository evidence, including Markdown/schema, determines verification eligibility; both ranked and fallback mining preserve provenance and negative controls.
- [ ] SPEC-0124 US8: compounding tests use a fresh per-attempt store and leave real proposals and cursors unchanged.

## Plan

### Foundation and council

Review the concrete plan with Opus 5.5 high and Astra high. Resolve the source baseline, draft execution identity, state isolation, and minimal UI before implementation. Update the governing specs with the agreed draft exception. Keep existing scheduled behavior intact.

Council revision 2 fixes these decisions:

- Capture effective source bytes using a private copy of the Git index, then an unsigned snapshot commit and detached worktree. Preserve tracked dependencies, modes, deletions and symlinks; use Git ignore rules for untracked files. Compare test output with this snapshot.
- Read the definition and script from the snapshot. Run command checks and agent work there. A code-mode check uses the captured script against the selected checkout's existing indexed vault, avoiding a full index and unexpected embedding calls for each test. That contextual query surface is the selected live checkout, not a historical snapshot of its index.
- Store test records separately from scheduled History/Last/Tokens, with an explicit test marker, source checkout and snapshot. Reuse the shared running slot and execution lock. Tests create no ordinary inbox item, branch, commit or task tag. Retain their results and transcripts through the current run views with separate limits.
- Give every attempt fresh data. Pass an explicit test context to compounding's shipped state resolver so mine/report/lookup/discovery use a fresh store and never migrate or modify real mining state.
- Keep the selected checkout on the service. Reject cross-repository draft paths. HTTP identifies a discovered task by ID and uses the existing detached CLI launch pattern. Reject mode changes for a task absent from the primary ref.
- Use one Tasks row per ID, a local-version strip with Test once, and Test labels in chronological history. Local-only tasks have no schedule controls; a scheduled task absent locally stays visible. Opus owns these UI changes.

### Backend

Own task discovery, snapshot capture, isolated draft state and execution, CLI/HTTP surfaces, and tests in the task package and its callers. Capture HEAD plus effective working-tree contents without changing the source checkout or index; unstaged bytes win, ignored local files remain excluded, and tracked dependencies remain present. Parse the executable definition from that capture and compare output against the captured baseline.

Use one fresh task-data directory per attempt and preserve ordinary locking, trust, cancellation, and cleanup. Add an explicit draft context for compounding so its main-checkout state and migration are bypassed for a test. Reuse the existing runner, transcript representation, and retention machinery instead of introducing another scheduler.

### Frontend

Opus 5.5 high owns the Tasks page changes and their frontend tests. Reuse its existing navigation and run-detail components. Make the local and scheduled versions distinguishable, expose Test once, and show test progress, errors, and results without implying the draft is scheduled.

### Markdown and workflow coverage

Separate whether a claim is checkable in repository evidence from whether it concerns code, environment, or workflow. Preserve legacy-state behavior without upgrading unknown or likely provenance to confirmed human instructions. Broaden software-only wording where it prevents valid vault work from being examined. Bump prompt/cache version. Exercise the shipped mining workflow with synthetic cases in both normal Jev ranking and unscreened mode, including duplicate and obsolete negative controls.

Privately review the supplied 17 held workflow candidates against current source, recording disposition and uncertainty. Capture the vault with the disposable helper and track the captured source before replay; no worktree binary may replace the live vault runtime. Each replay uses fresh private state, explicit episodes and checkout, fixed input/source hashes, and an approved spend estimate. Public evidence contains anonymized examples and counts, never raw sessions.

### Integration and proof

Use real temporary Git repositories to prove snapshot fidelity, no-op output on a dirty source checkout, local helper availability, code-mode source context, ignored-file exclusion, and unchanged scheduled state. Include new/modified/invalid local definitions and same-ID drafts from different worktrees. Prove the actual UI with a disposable fixture and deterministic task runner.

Run focused tests, the repository's required gates, generated-surface checks, and content validation. Stop worker tests before integrated gates. Review the integrated diff with council and sol-loop, fix realistic findings, obtain Greptile 5/5, and merge only with current green CI.

### Delivery

Rebase onto current main before filing. Include closure evidence in this effort on the branch, link the PR, and preserve every unrelated edit. After merge, update and install Rhizome Dev only from the primary checkout on main.

## Original Intended Delivery

One reviewed pull request that makes branch-local task authoring and testing work in Desktop, installs compounding in this repository, and leaves primary-ref scheduling unchanged.

## Actual Delivered

Implemented local discovery, isolated test snapshots and history, per-session harness environment, and the Opus-authored Tasks UI. Canonical compounding coverage changes and source-based workflow dispositions are implemented; integrated gates, measured replay, and final reviews remain in progress.

## Execution Notes

- 2026-11-03T13:57:46Z [approval] Recorded Drew's full-send authorization and explicit two-provider council approval.
- 2026-11-03T13:57:46Z [validation] Existing `go test -tags fts5 ./pkg/app/tasks` passed outside the sandbox in 52.965s. The sandbox cannot read the macOS boot identity used by lock fixtures.
- 2026-11-03T13:57:46Z [review] Astra plan round 1 scored 8/10, requesting explicit immutable source-baseline semantics and a compounding draft context that isolates shared mining state. Both are accepted in the plan above; Opus review is pending.

- 2026-11-03T14:01:13Z [review] Astra plan round 2 approved at 9/10 with no remaining material R1 issues. The immutable baseline, compounding isolation, test history, and explicit code-mode context tradeoff are accepted. Opus round 2 is pending.

- 2026-11-03T14:02:14Z [review] Opus plan round 2 approved at 9/10. Its nonblocking correction is accepted: code-mode tests must require an existing selected-checkout index, with a clear error instead of automatic indexing. Both council members now approve implementation.

- 2026-11-03 [implementation] Task, web and harness package suites passed. Compounding isolation passed 39 tests, and workflow coverage passed 46 focused tests plus the final five-case workflow suite. No paid models ran in these tests.
- 2026-11-03 [UI proof] Rhizome Dev discovered the uncommitted check-only task and helper. Test `20261103T141714Z-49eb40a1` completed as `not-needed`, displayed its source and snapshot `d56973a5`, and left scheduled state byte-equivalent after excluding test records. The preceding ignored-helper fixture failed visibly with retained stderr, then passed when the helper was moved to a nonignored path.

- 2026-11-03 [evaluation approval] Drew approved the four-run isolated comparison estimated at $6.28 with an $8 hard cap: "Approve this four-run batch". All four runs completed for $0.8084008; the source snapshot and per-attempt private state were preserved. The workflow coverage report records both paired repeats, case dispositions and limitations.
- 2026-11-03 [review] Astra implementation review scored 8/10 and identified a same-task launch race across checkouts. The launch response now carries canonical sourceCheckout, and the UI requires it when matching a test. Opus frontend review scored 8/10; ignored-file copy and wrapped local explanations were corrected by Opus.
- 2026-11-03 [verification] `make verify` passed: race-enabled Go tests, integration suites, 1,770 web unit tests and 110 browser E2E tests. Later review corrections passed focused task/HTTP and 36 frontend tests; final pre-commit checks are in progress.

- 2026-11-03 [evaluation] Paired unscreened repeats verified 2/5 baseline versus 7/9 candidate clusters, with zero proposals in all four. Writing and optional-field rules consistently moved from held to correctly rejected duplicates. Semantic-search recovery was not demonstrated. The obsolete-publication control rejected in all four; identity was absent in one candidate and rejected in the other. All 134 live state files remained byte-identical. This supports the narrow eligibility fix, not a broad extraction-quality or untouched-holdout claim.
- 2026-11-03 [verification] Final `make check` passed after launch-provenance and copy fixes, including 1,771 web unit tests.

- 2026-11-03 [validation] Default `rzm validate` passed with zero issues. Repository-wide frozen-scope validation found one unrelated existing drift in EFF-2026-10-03-18-33 against SPEC-0111 (group views); this effort was not flagged. The historical/other effort was left unchanged.

## Deviations

Council uses two providers, explicitly approved by Drew, because no third provider is enabled.

Code-mode contextual queries use the selected checkout's current index, while the script bytes come from the immutable test snapshot. This avoids creating a new indexed vault and provider work merely to test a task.

## Closure Checklist

- [ ] Agreed scope delivered and deviations recorded.
- [ ] Focused and integrated proof recorded.
- [ ] Council, sol-loop, Greptile, and CI gates satisfied.
- [ ] Pull request and merge recorded.
- [ ] Merged Rhizome Dev installed from the primary checkout.

## Compounding Follow-ups

The Markdown/workflow coverage request is now in scope. Any remaining tool defect or vault-specific document change identified by the disposition review is a follow-up unless implemented and verified in this effort; no private vault proposals are published automatically.

## Status

Active. Implementation is complete; integrated verification, workflow replay, and final reviews are in progress.
