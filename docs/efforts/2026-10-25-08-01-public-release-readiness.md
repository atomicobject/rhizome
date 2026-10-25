---
type: EffortNote
id: EFF-2026-10-25-08-01
aliases: [EFF-2026-10-25-08-01]
name: Public release readiness
created-at: 2026-10-25T12:19:28Z
status: active
summary: "Prepare the first release in the clean public repository, add AO 1Password Environment onboarding, and bound desktop update requests without publishing or using live credentials."
governing-specs:
  - "[[evidence-grounded-release-orchestration]]"
  - "[[open-source-preparation]]"
  - "[[desktop-distribution-and-updates]]"
---

# Public release readiness

## Scope

Prepare a reviewable release path from the clean public repository, with no previous version tags or release branch. Complete AO credential onboarding and desktop update recovery. Preserve the existing Apple Silicon, ad-hoc signing, GitHub Releases, and Homebrew boundaries.

Excluded: release publication, release-branch pushes, tags, live credentials or signing, credential rotation or 1Password administration, installation over Rhizome or Rhizome Dev, notarization, Intel desktop builds, and production release-to-release verification.

## Spec Set (Frozen)

Frozen to the 2026-10-25 revisions in this change set:

- [[evidence-grounded-release-orchestration|SPEC-0078]]: first-release evidence, canonical refs, monotonic version selection, safe retries.
- [[open-source-preparation|SPEC-0107]]: Environment and item imports, credential isolation, user-local persistence, desktop cancellation and rotation.
- [[desktop-distribution-and-updates|SPEC-0122]]: bounded updater requests and signing-key/archive verification.

## Stories In Scope (Frozen)

The requirement sections above define this slice. Existing desktop stories are extended for recovery and signing verification; their production installation and second-release criteria remain separate live acceptance checks.

## Spec Coverage Checklist

- [x] Empty-baseline evidence includes the initial commit without Git object writes; missing release branch creation preserves atomic publication.
- [x] Version floors cover prompted, supplied, environment, and reused stored choices.
- [x] Public apply checks key matching; generated archives are verified before staging.
- [x] CLI and shared desktop Environment import preserve item-reference mode and unrelated config, with bounded private transport and process-tree cancellation.
- [x] Real-plugin update regressions cover stall, progress, retry, and verified-cache preservation.
- [x] Integrated checks, packaged-binary smoke test, and documentation validation.
- [ ] Packaged visual walkthrough; native automation cannot locate the running test app's window.

## Plan

1. A Sol worker owns release scripts and tests: first-release evidence and storage, version floors, ref publication, signature checks, and synthetic crypto tests. Root owns Makefile phase guards and their behavioral tests.
2. A Sol worker owns credential CLI, Go config/desktop bridge, native cancellation, and shared Setup/Settings UI. Explicit account and Environment references remain user-local; only provider-presence status and non-secret source references cross into the UI.
3. A Sol worker owns updater timeouts and real-plugin local HTTP regressions. Use 30 seconds for feed checks and connection/read inactivity, with a 30-minute total archive limit. Preserve verified downloads after refresh failure.
4. Opus 5.5 independently reviews the plan and final diff. Root integrates accepted findings, documentation and changelog changes, runs serialized repository/desktop gates, and exercises the packaged UI with disposable synthetic credentials.

The first three workers use Sol at high reasoning effort and the standard service tier. Workers share the checkout with disjoint ownership; root owns integrated gates and final review. No worker commits, publishes, installs, or uses live keys.

## Plan Approval

Drew approved the preceding release-readiness recommendation in chat: "OK. Get sol high standard workers going on that. Have opus 5.5 subagent do another independent review of your plan and see if anything else turns up." The existing account/source interface, timeout limits, process-tree cancellation, and synthetic signing checks were routine implementation/review corrections within that authorization. Publication remains excluded.

The configured current user is absent, confirmed on 2026-10-25, so `plan-approved-by` is omitted. This record was persisted during integration because early ID allocation failed on a stale projection and then on source hashes while worker edits were active. Allocation succeeded after source freeze; no identifier was invented.

## Original Intended Delivery

A release path that works in the new public repository, an easy one-time AO credential import, and an updater that recovers from stalled networks. Deliver verified source changes and a precise live acceptance checklist before any publication decision.

## Actual Delivered

Implemented on `t3/rhizome-release-readiness` in `8bc1e502`, then integrated public main `73f4eae9` through merge `e699a834`. No release has been published. [Draft PR #39](https://github.com/atomicobject/rhizome/pull/39) retains the open packaged visual check; review corrections are committed in `1b7dcdaa`.

- First-release collection uses empty prior fields, includes root content, preserves safe retry topology, and atomically creates the release branch. Version selection continues from source `v0.50.5` and rejects a version at or below the floor on every input path.
- A pinned Minisign helper verifies candidate archives before staging. Future public apply signs and verifies a disposable probe before mutations; execution in this effort used only temporary synthetic keys.
- CLI Environment import and a shared native Setup/Settings panel support one-time import and re-import. Source references persist in private user config. Missing keys, errors, cancellation, timeouts, and status refresh are covered by synthetic tests.
- Update checks and downloads have explicit bounds and preserve a verified cached update when refresh fails.
- Release, credential, source-publication, signing, and acceptance documentation is reconciled. The full production checklist lives in [[docs/RELEASING.md#Release acceptance|RELEASING]].

## Execution Notes

- 2026-10-25T12:19:28Z [record] Persisted the allocated effort record during integration. Three workers are frozen; root is running integrated verification.
- 2026-10-25T12:19:28Z [validation summary] Release suite: 261 tests pass. Root repaired a baseline template-test fixture missing the shipped `governing_specs_yaml` placeholder and added Makefile subprocess tests for all four empty-base phase commands.
- 2026-10-25T12:19:28Z [validation summary] Real repository at `900efa2c`: 30 delivery units, 6,929 root paths, 11 completed root efforts and 20 effort entries overall. One generation packet uses 245,364 of 620,000 characters, with no omitted or truncated details. Active/planned efforts are excluded. No model or GitHub call was made.
- 2026-10-25T12:19:28Z [validation summary] Feed and archive hangs were reproduced separately with real plugin requests before their fixes. Final updater tests: 10 pass, including stall, progress, total cap, retry, signature failure and cached update preservation.
- 2026-10-25T12:19:28Z [validation summary] Credential cancellation first reproduced a surviving child process. After process-tree cancellation, both fixture processes exit within two seconds and config bytes/mtime remain unchanged. Before-save cancellation and after-commit status are separately covered. Focused Go tests, 19 UI tests, three native lifecycle tests, type checks, formatting, and changed-file lint pass.
- 2026-10-25T12:19:28Z [review summary] Opus final review found all earlier findings fixed and no blocking code defects. Root corrected its signing-recipient documentation finding; the missing effort record is this note.
- 2026-10-25T12:42:59Z [validation] `make verify` passed, including race-enabled Go and integration tests and all 102 browser tests. `make desktop-check` passed, including 54 UI tests, 70 native tests, formatting, and clippy. `make desktop-build` produced the packaged app.
- 2026-10-25T12:42:59Z [validation] The actual packaged `rzm` and desktop bridge passed a disposable synthetic smoke test for import, private permissions, named missing-key failure without config changes, rotation, source persistence, marker cancellation, process exit, and unchanged config on cancel. The temporary app was stopped; installed apps and real credentials were untouched.
- 2026-10-25T12:42:59Z [gap] Native automation returned `cgWindowNotFound` for the packaged app, including a variant with a unique bundle identity. The process ran and persisted window geometry, but no visual walkthrough is claimed. This remains an open check for the draft PR.
- 2026-10-25T12:42:59Z [validation] Default validation passed with zero issues. Frozen-scope validation reported three existing findings for SPEC-0045, SPEC-0111, and SPEC-0124 in unrelated active efforts. Their specs and efforts are byte-unchanged from `HEAD`; none concerns the three specs amended here. They remain classified as unrelated/pre-existing.

- 2026-10-25T13:01:21Z [integration] Merged current public main. `make verify` passed again, including all 102 browser tests. No source conflicts remained after preserving both sets of changelog entries.
- 2026-10-25T13:01:21Z [review] Greptile scored the first implementation 4/5 and identified competing-window cancellation, source edits overwritten on status refresh, and missing CLI subsystem guidance. Root fixed these locally: competing imports are rejected immediately, only the owning window can cancel, readiness refresh preserves edits, and the owning guide now records Environment imports and the early capture route.
- 2026-10-25T13:01:21Z [validation] Added two UI regressions and captured both failures before the fix; all five CredentialImport tests now pass. Added a native competing-window regression; full desktop gate passes with 56 UI tests and 71 native tests, formatting and clippy.
- 2026-10-25T13:01:21Z [validation] Captured CLI delegation failures in an untrusted pinned checkout and a configured development checkout. The shared invoking-executable exception now includes user-global credentials. Focused regressions pass, and the actual built CLI imported synthetic keys inside an untrusted older-pinned fixture without executing its sentinel binary.

- 2026-10-25T13:10:59Z [validation] Final `make check` passed, including all Go unit tests and 1,580 web tests. The initial invocation completed its tests but failed because another checkout held the global lint lock; after confirming its process exited, the full retry passed. `make desktop-check` passed with 56 UI tests and 71 native tests.
- 2026-10-25T13:10:59Z [artifact] Built the final app and local DMG with only a minimal non-credential environment. The 41.53 MiB DMG passed integrity verification, read-only mounting, expected app version and Applications link, deep ad-hoc signature verification, and an actual bundled-CLI synthetic import inside the older-pinned fixture. Detached the disk image. This is local packaging proof, not production signing, installation, or publication.

- 2026-10-25T13:13:29Z [validation] Final default documentation validation passed with zero issues. Frozen-scope validation still reports only the same three unrelated baseline findings. Opened draft PR #39; its packaged visual check remains open.

- 2026-10-25T13:26:49Z [review] The next Greptile pass confirmed the earlier findings fixed, then identified stale setup choices captured during import and manual search keys erased after failure/cancel. Three real-panel integration regressions reproduced both defects. Setup now triggers its existing report effect with current choices and preserves typed keys; all 59 UI tests and 71 native tests pass. The final `make check` also passed, including all Go unit tests and 1,580 web tests.

- 2026-10-25T13:39:16Z [review] A further Greptile pass confirmed all five earlier findings fixed and identified overlapping setup/import user-config writes. A native regression reproduced setup reaching repository execution while import held the global lock. Setup now takes that same app-wide lock before its worktree lock and retains it through the bridge call; this serializes writes across windows. The regression and full desktop gate pass: 59 UI tests, 72 native tests, formatting and clippy. Final-head artifact and review results are tracked on PR #39.

## Deviations

Independent review added key/archive matching, source-version floors, named missing-key errors, and process-tree cancellation after concrete defects were identified. The initial five-minute archive cap was replaced by a stall timeout and a 30-minute cap so progressing slow transfers can complete. These strengthen the approved release-readiness outcomes without adding a publication target or live execution. The visual walkthrough remains blocked by the native automation service and is recorded separately from passing packaged-binary and automated UI evidence.

## Closure Checklist

- [x] Focused worker tests and independent final source review.
- [x] Combined repository verification and desktop gates.
- [ ] Packaged visual walkthrough with synthetic credentials; native automation blocked.
- [x] Ontology, links, frozen-scope and documentation validation; unrelated baseline drift is classified above.
- [x] Final alignment, actual outcomes, and remaining live acceptance checks recorded.
- [x] Draft PR and commit evidence: [#39](https://github.com/atomicobject/rhizome/pull/39), implementation `8bc1e502`, integration `e699a834`, review fixes `1b7dcdaa`.

## Compounding Follow-ups

Avoid allocating a new effort while shared source is changing: its required index snapshot can be invalidated. Reuse the allocator result after source freeze. The release-test fixture now follows the shipped template's governing-spec metadata. Setup tests now mount the real shared credential panel; mocking it hid completion interactions with preview choices and manually entered keys.

## Status

Implementation, integrated verification, local DMG and packaged-binary smoke testing are recorded in draft PR #39. Opus found no blockers in the initial frozen implementation; subsequent Greptile fixes are committed. Current-head review and CI results are tracked on the PR. The packaged visual walkthrough remains open due to the native automation error. Production credentials, publication, browser/Gatekeeper installation, and a real second-release update remain outside the executed evidence. Keep this effort active until its remaining in-scope visual check is resolved or an explicit deviation closes it.
