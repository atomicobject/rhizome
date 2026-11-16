---
type: EffortNote
id: EFF-2026-11-13-14-55
name: macOS app-owned updates
created-at: 2026-11-13T20:00:13Z
status: active
summary: Preserve the installed app and CLI paths while allowing user-owned macOS apps to update Contents under a protected parent.
aliases: [EFF-2026-11-13-14-55]
governing-specs:
  - "[[desktop-distribution-and-updates]]"
---

# macOS app-owned updates

## Scope

Implement the macOS installation adapter in `desktop/src-tauri/src/updater/`. Preserve download verification, `prepare_update`, cancellation, window recovery, restart markers, and the CLI handoff. No migration chown, helper, Applications permission change, live-app replacement, or change to other platforms.

## Spec Set (Frozen)

- [[desktop-distribution-and-updates|SPEC-0122]], the macOS updater requirements added on this branch on 2026-11-13, as approved in plan v2 and the final council amendments.

## Stories In Scope (Frozen)

- [[desktop-distribution-and-updates#^SPEC-0122-US2|SPEC-0122 US2]], macOS installation and preservation on failure.
- [[desktop-distribution-and-updates#^SPEC-0122-US3|SPEC-0122 US3]], preserve the existing CLI link while the app updates.

## Spec Coverage Checklist

- [x] Validate every replacement before fallback or installed mutation.
- [x] Atomically swap only Contents in eligible user-owned bundles.
- [x] Preserve root, CLI path, lifecycle, rollback copy, and failure reporting.
- [x] Complete focused regression checks and the original desktop gates.
- [ ] Resolve the app-instance gate failure from the 2026-11-16 refresh.
- [x] Record signed standard-account A to B to C results and Drew's accepted deviation. The no-password requirement did not pass.
- [x] Independent source reviews completed at 9/10 from Opus, Astra, and Sol on the preserved implementation.
- [ ] Current-head Greptile approval and CI after the final branch refresh.

## Plan Approval

The user explicitly requested Opus and Astra plan council, then execution and Full Stop delivery. Root relayed Astra v2 approval at 9/10 and Opus v2 approval at 8.5/10 with the required clean-root/current-signature/Apple-anchored requirement amendments incorporated. The implementation lane is Sol xhigh ultrafast and stops at local commits until root requests a PR. `current-user show` reports no configured person, so `plan-approved-by` remains absent.

## Plan

1. Start at fresh origin/main and inspect the latest updater lifecycle and engineering policy.
2. Add a macOS-only adapter around `Update.verify` and `Update.install`. Scan all archive entries before extraction, validate identity/version and strict Apple/team signature, and reject invalid updates without fallback.
3. Require user ownership, writability, actual directories, clean root, valid current signature, and same-volume staging for the atomic Contents swap. On post-swap failure, roll back; if rollback fails, retain and name the old copy. Only pre-mutation unsupported or permission failures may fallback.
4. Add focused regression tests, protected-parent and public-signed-fixture checks. Run desktop-check and desktop-build. Record genuine standard-user acceptance separately.
5. Update the owner documentation, spec, and changelog, then commit locally for root's independent review. Root coordinates council, sol-loop, Greptile, PR creation, and CI. Never merge.

Blocking first steps were the approved contract and fresh-base source inspection. One implementation owner handles the coupled archive and transaction code; root owns independent review and signed-account feasibility. All shared writes stay in this task worktree.

## Original Intended Delivery

An existing installation updates in place with at most one further administrator password, then updates its Contents without admin under a genuine standard user. Root and CLI links remain stable. Signature, archive, identity, transaction, and recovery failures preserve a working app or an explicit recovery copy.

## Actual Delivered

Implementation is published in [PR #145](https://github.com/atomicobject/rhizome/pull/145). Drew accepted the incomplete manual acceptance as a delivery deviation on 2026-11-16 and requested merge preparation without more manual testing. Current-head review and CI still govern merge readiness. The new adapter validates the archive, identity/version, and Apple-anchored current-team requirement before fallback. Eligible installs exchange Contents, strictly verify, and atomically roll back on failure. Failed rollback retains the old Contents in a unique private directory under `~/Library/Application Support/<identifier>/recovery/`. If that move fails, the error names the kept staging copy and warns about macOS temporary cleanup. Post-commit cleanup is best effort. The CLI and existing lifecycle stay unchanged. Signed A to B and B to C installs succeeded, but the B to C run used full-bundle fallback and requested administrator authorization. This effort does not establish password-free updates for new users. Verification results follow in Execution Notes.

## Execution Notes

- 2026-11-13T20:00:13Z [decision] Reset only the clean isolated task branch to `684a3517c` from origin/main. The primary checkout was not changed.
- 2026-11-13T20:00:13Z [validation] The launcher was initially absent. After the fresh CLI build, the minimal session returned source context; its sandbox runtime access was denied. The escalated local identifier allocator allocated EFF-2026-11-13-14-55. Current person is not configured. No secret file or value was read.
- 2026-11-13T20:00:13Z [validation] A controlled red run of the protected-parent regression used the old whole-bundle swap in a temporary copy of the actual Rust adapter. It failed with EACCES and Fallback instead of Installed. The same test passes with Contents swapping. This proves the filesystem mechanism, not App Management or standard-account behavior.
- 2026-11-13T20:00:13Z [validation] The temporary harness imports the real adapter and tests directly. Nine focused tests pass. The separately invoked signed fixture requires host trust-service access.
- 2026-11-13T20:00:13Z [blocker] Initial desktop-check runs exposed public-origin access and inherited Go 1.27.3 GOROOT issues. With host access and matching installed Go 1.27.4, the suite reached Rust tests: 155 passed, one signed-fixture test remained explicitly opt-in, and the pre-existing paused-clock read-stall test failed its upper elapsed-time assertion. No retry or timeout increase is used as a fix.
- 2026-11-13T20:00:13Z [blocker] Root's capability report found no usable Developer ID identity or disposable standard GUI user. Its public signed Python swap proof is partial evidence. Genuine signed A to B to C acceptance remains required and blocks readiness.

- 2026-11-13T20:02:43Z [validation] Final focused updater tests passed 23 tests, with the signed fixture selected separately. The full desktop-crate signed fixture passed through the actual adapter using public v0.52.1 bytes: strict Apple/team verification, stapler and Gatekeeper, quarantine/root-xattr preservation, CLI execution and link preservation, unsigned-replacement rejection, mismatched identifier/version rejection, and fallback for damaged current signatures. This is same-version filesystem acceptance, not a signed running-app upgrade.
- 2026-11-13T20:02:43Z [validation] `make desktop-build` built the real development app bundle. `cargo clippy --locked --all-targets -- -D warnings`, `cargo fmt --check`, the app-instance script, and desktop frontend checks passed. Documentation and frozen-scope-drift validation passed with host runtime access. No app was installed over Rhizome Dev.
- 2026-11-13T20:02:43Z [blocker] The existing paused-clock test passed in the focused rerun, which is diagnostic only. Its original full-suite failure remains unresolved. A draft clock-control change was investigated and removed; the committed timing test is unchanged. Root must address this failure before treating the full desktop gate as green. This historical blocker was resolved by `825d18739c6bbbc91dc3e0499d7faa2dd299ed6d`, as recorded below.

- 2026-11-13T20:03:24Z [validation] The packaged development bundle's `Contents/MacOS/rzm --version` reports v0.52.1. A strict codesign check reports "code has no resources but signature indicates they must be present" for that local development artifact; it is not a Developer ID signed release candidate. The separate public signed archive fixture provides the signature-preservation evidence.

- 2026-11-13T20:20:07Z [validation] Clock follow-up `825d18739c6bbbc91dc3e0499d7faa2dd299ed6d` resolved the earlier paused-clock blocker. It measures time from the client's consumed chunk and acknowledges chunks before the blocking fixture proceeds. The controlled delay failed before the fix and passed after it without changing the timeout or upper bound. Retained evidence in `/private/tmp/rhizome-update-permissions-v0521/clock-desktop-check.log` shows `PATH=/Users/colthorp/.cargo/bin:/Users/colthorp/.local/share/mise/installs/go/1.27.4/bin:$PATH GOROOT=/Users/colthorp/.local/share/mise/installs/go/1.27.4 make desktop-check` exited 0. Frontend tests passed 187 tests in 12 files; native tests passed 156 with the existing opt-in signed test ignored; formatting, Clippy, and app-instance checks passed. `clock-repeatability.log` records 56 further full native suite passes. These are retained results for that commit, not new runs at the F1 revision.
- 2026-11-13T20:20:07Z [decision] Accepted final-review findings F1 and F2 are the only follow-up scope. F1 moves the old Contents out of temporary staging on failed rollback. Its tests check the reported path, retained bytes and inode, existing recovery data, and a failed durable rename that keeps and names the temporary copy. F2 reconciles this effort with the completed clock follow-up while retaining the original failure record. Optional F3 certificate-type hardening is declined for this scoped fix; the existing Apple anchor, current signing team, configured identifier, and updater signature checks remain the accepted contract.
- 2026-11-13T20:23:00Z [validation] F1 regression fails before the fix with the actual Contents path in TMPDIR. After the fix, `PATH=/Users/colthorp/.cargo/bin:$PATH cargo test --locked --manifest-path desktop/src-tauri/Cargo.toml updater::macos::tests` passes 10 tests with one opt-in signed test ignored. `f1-recovery-red.log` and `f1-recovery-green.log` retain the outputs under `/private/tmp/rhizome-update-permissions-v0521/`.
- 2026-11-13T20:23:00Z [validation] Current F1 code passes `PATH=/Users/colthorp/.cargo/bin:/Users/colthorp/.local/share/mise/installs/go/1.27.4/bin:$PATH GOROOT=/Users/colthorp/.local/share/mise/installs/go/1.27.4 make desktop-check`. Frontend tests pass 187 tests in 12 files; native tests pass 157 with one opt-in signed test ignored; formatting, Clippy with warnings denied, and app-instance checks pass. The same toolchain runs `make desktop-build` successfully and produces the development app bundle without installation. Logs are `f1-desktop-check.log` and `f1-desktop-build.log` in the same evidence directory.
- 2026-11-13T20:23:00Z [validation] `PATH=/Users/colthorp/.cargo/bin:$PATH RZM_SIGNED_UPDATE_ARCHIVE=/private/tmp/rhizome-update-permissions-v0521/Rhizome-macos-arm64.app.tar.gz cargo test --locked --manifest-path desktop/src-tauri/Cargo.toml signed_archive_uses_real_adapter_under_protected_parent -- --ignored` passes one test through the actual adapter. Log `f1-signed-adapter.log` retains the result. This remains same-version acceptance in disposable directories; signed standard-account A to B to C acceptance, independent final review, Greptile, and current-head CI remain outstanding.

- 2026-11-14T02:01:33Z [resume] Root requested a local rebase of PR #145 onto current main, preservation of the reviewed updater behavior, and an evidence update. Stop before pushing. Do not merge, enable auto-merge, log out, switch users, or reboot.
- 2026-11-14T02:01:33Z [validation] GitHub reports PR #145 open at `5112f1a5df99a6e6757bf7b388b8c442931d7a7f`, with successful CI and merge state `DIRTY`. Root reports completed Opus, Astra, and Sol source reviews at 9/10 and Greptile at 5/5 for that original head. These are prior-head results, not approval or CI for the rebased head.
- 2026-11-14T02:01:33Z [validation] Fetched `origin/main` at `9eebbcc238da34441ce7e1b0ee76c1cd3b504a9c`. Main added `e59c3b065` and `9eebbcc23` after the original base `684a3517ce6b6d667371fc6ea2bee84bc6429a85`. The only conflicting file was `CHANGELOG.md`: main added two legacy-upgrade entries at the same insertion point as this branch's macOS updater entry. All three entries remain. No source hunk required resolution.
- 2026-11-14T02:01:33Z [validation] Replayed commits are `f24cfd35bf58a20301fb22ffdb08d912b2467e42` to `95005c8639b2528b2624446a660e3fb421d59701`, `825d18739c6bbbc91dc3e0499d7faa2dd299ed6d` to `0fc325fd3a18e46b3729ab15ef1ed8d3ba72b9a7`, and `5112f1a5df99a6e6757bf7b388b8c442931d7a7f` to `b0ee32fcbbc15a48808c63d5e90437973aae67d0`. `git range-diff` shows only changed changelog context for the first commit and identical patches for the other two. Direct tree comparison confirms unchanged updater source, tests, Cargo files, desktop updater guidance, and distribution spec. `prepare_update`, import/sign-in cancellation, window recovery, restart markers, CLI handoff, Contents swapping, and durable failed-rollback recovery retain their reviewed behavior.
- 2026-11-14T02:01:33Z [validation] Root reports the standard GUI account `rzm-update-test` fully verified and ready. This lane's read-only `id rzm-update-test` confirms uid 503 and no admin-group membership. No local Developer ID is available. Account setup is no longer the blocker; signed packaged A to B to C acceptance through app and CLI is still outstanding. No GUI session transition or live-app replacement was attempted.
- 2026-11-14T02:01:33Z [validation] On rebased source `b0ee32fcbbc15a48808c63d5e90437973aae67d0`, `make desktop-deps` and `PATH=/Users/colthorp/.cargo/bin:/Users/colthorp/.local/share/mise/installs/go/1.27.4/bin:$PATH GOROOT=/Users/colthorp/.local/share/mise/installs/go/1.27.4 make desktop-check` exit 0. The gate passes 200 frontend tests, six build-config tests, and 157 native tests, with one existing opt-in signed fixture ignored. Typechecks, formatting, Clippy with warnings denied, sidecar builds, and disposable app-instance checks pass. The frontend retains its pre-existing unused-parameter lint warning. Logs are under `/private/tmp/pr145-rebase-20261113/`.

- 2026-11-14T02:02:40Z [validation] The same pinned toolchain runs `make desktop-build` successfully and produces `desktop/src-tauri/target/release/bundle/macos/Rhizome.app`. It was neither installed nor launched. `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` both exit 0 with no issues or errors. The corresponding logs are `desktop-build.log`, `validate.log`, and `frozen-scope-drift.log` in `/private/tmp/pr145-rebase-20261113/`.
- 2026-11-14T02:02:40Z [validation] The initial context-shard lookup could not authenticate to the configured service. Host runtime access later completed the local EffortNote authoring-guide query and document validation. No credential file or value was read. Existing Git hooks remain enabled. The only new commit after replay updates this effort; generated HTML changes from the local builds were excluded. No broad Go gate was repeated for the changelog-only conflict. Prior CI is retained as prior-head evidence, while CI and review for the rebased head remain root's next steps.
- 2026-11-14T02:02:40Z [handoff] `git ls-remote` confirms main is still `9eebbcc238da34441ce7e1b0ee76c1cd3b504a9c` and the published PR branch is still `5112f1a5df99a6e6757bf7b388b8c442931d7a7f`. The local branch is `t3/macos-app-update-adapter`. Root should inspect the rebase and this evidence-only commit before deciding whether to publish. No push, merge, auto-merge, primary-checkout edit, or GUI session change occurred.

- 2026-11-16T13:23:38Z [resume] Drew requested merge preparation and accepted no further manual testing: "Let's just let it ride and we'll see if it fixes the problem for new users." This authorizes branch refresh, truthful documentation, local gates, review, and publication. It does not authorize merge, release, app installation, account changes, or TCC changes.
- 2026-11-16T13:23:38Z [validation] Signed Developer ID and notarized A to B and B to C installs completed under standard UID 503 in `/Applications`. Installed version, signature, ownership, and CLI-link checks passed. A to B authentication count is unknown because Drew revised the initial no-password observation to possible Touch ID authorization. Drew reported restart success but did not explicitly confirm both repository windows restored. The B to C root inode changed from `544204617` to `544226748`, confirming full-bundle replacement rather than a preserved root. Native `authd` records confirm an administrator dialog with an expired cached credential. Retained evidence is `/private/tmp/rhizome-update-permissions-v0521/acceptance/observed-a-to-b.json`, `observed-b-to-c.json`, and `pr145-b-to-c-diagnosis-20261116.md` in that directory.
- 2026-11-16T13:23:38Z [learning] The diagnostic report records a Ghostty App Management notification immediately before elevation. The fixture's direct Python GUI launcher is a probable contributor, not a proven cause. The exact fallback branch remains unknown. A temporary LaunchServices launcher correction is test infrastructure only and is not part of this branch. The separate "Update Rhizome Files" 404 uses unpublished synthetic CLI versions 0.52.2 and 0.52.3 and does not establish an app-updater regression. Fixture runtime repair evidence is `acceptance/signed-43752192740/fixture-runtime-diagnosis.md` under the same evidence root.
- 2026-11-16T13:23:38Z [decision] Drew accepted deferral of the incomplete no-password, normal-launch attribution, and complete window-restoration acceptance. The failed B to C requirement remains failed; it is not reclassified as passed. Repository release policy permits delivery covered by a recorded deviation. Current-head Greptile and CI are still required and are not waived.
- 2026-11-16T13:23:38Z [validation] Fetched main at `721d1281b893f2ac69e27955b69476b2cd313181` and remote PR head at `b8d5d8c3b77765dc94e2516762bddf01ef6b5c83`. Main added seven commits, including desktop chat settings and window reporting. No updater or restart-lifecycle overlap required a design choice. Rebased all four commits without conflicts to `c9dbcee8e3c424d6198391e07a9ea9d48fbbd737`; `git range-diff` reports identical patches for each. Direct tree comparison confirms unchanged updater source, tests, and Cargo files. Earlier 9/10 source reviews and 5/5 Greptile are retained prior-head evidence. No local review log was available for those historical ratings beyond the existing PR record and root's report.

- 2026-11-16T13:25:31Z [validation] On rebased source `c9dbcee8e3c424d6198391e07a9ea9d48fbbd737`, `make desktop-deps` exits 0. With `PATH=/Users/colthorp/.cargo/bin:/Users/colthorp/.local/share/mise/installs/go/1.27.4/bin:$PATH` and `GOROOT=/Users/colthorp/.local/share/mise/installs/go/1.27.4`, `make desktop-check` passes 208 frontend tests, seven build-configuration tests, and 161 native tests with one existing opt-in signed fixture ignored. Typechecks, formatting, sidecar builds, and Clippy with warnings denied pass. The final `desktop/scripts/app-instances_test.sh` step fails with `FAIL: copies still running: 63994` during the stubborn fake-app test, so the overall gate exits 2. This process is absent on later inspection. The helper and test are byte-identical to main and the previously reviewed head. A single instrumented disposable copy passes, which is diagnostic only and does not resolve the original failure. Logs are `/private/tmp/pr145-ready-20261116/desktop-check.log` and `app-instance-diagnosis.log`. No retry, longer wait, skip, or product change is used to mask this failure.
- 2026-11-16T13:25:31Z [validation] The same toolchain runs `make desktop-build` successfully and creates the development bundle at `desktop/src-tauri/target/release/bundle/macos/Rhizome.app`. The bundle is not installed or launched. Log is `/private/tmp/pr145-ready-20261116/desktop-build.log`. Build-generated tracked HTML was restored to its pre-build content and excluded from this documentation change.
- 2026-11-16T13:25:31Z [validation] Reviewed the complete source diff and the integration with main's chat/window changes. No source or dependency patch changed during the rebase, and no source changes were needed. Existing independent source approvals remain prior-head evidence. No new reviewer child was run because this lane is explicitly a leaf. Current-head Greptile and CI remain required. Local runtime inspection confirms the EffortNote contract; no current person is configured, so the approval frontmatter remains absent.

- 2026-11-16T13:25:52Z [validation] Final `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` both exit 0 with zero issues or errors after the documentation edits. Logs are `/private/tmp/pr145-ready-20261116/validate.log` and `frozen-scope-drift.log`. `git diff --check` passes.

## Deviations

SPEC-0122 frozen-scope-drift acknowledged via EFF-2026-11-13-14-55 on 2026-11-16. The spec now records the existing permission-denial fallback and this accepted delivery exception. The frozen scope and original intended delivery above remain unchanged.

Drew explicitly accepted delivery with incomplete signed standard-user acceptance on 2026-11-16 and deferred further manual testing. Both signed transitions installed successfully, but B to C requested administrator authorization and replaced the bundle root. A to B authentication count is unknown. Complete restoration of both windows was not explicitly confirmed. The no-password acceptance gate remains failed, and behavior for normally launched new-user installs is unverified.

The direct Python launcher and Ghostty App Management attribution may explain the fallback, but causation and the exact fallback predicate remain unproved. This deviation accepts that uncertainty without changing product code, relaxing signature checks, or claiming the intended one-authorization transition has been demonstrated. The existing signed fixture remains filesystem and signature evidence only. Current-head source review and CI are not waived.

## Compounding Follow-ups

No new compounding mechanism is proposed. Preserve the protected-parent and failure-injection tests as the reusable proof.

## Closure Checklist

- [x] Implementation and independent source reviews are complete; the rebased source preserves those patches.
- [x] Drew accepted the recorded manual-acceptance deviation on 2026-11-16.
- [x] Local gate outcomes recorded after the final branch refresh, including the failed app-instance gate.
- [x] Final documentation and frozen-scope validation pass.
- [ ] Resolve the app-instance gate failure.
- [ ] Current-head Greptile 5/5 with no unresolved comments and green CI.
- [x] Root created [PR #145](https://github.com/atomicobject/rhizome/pull/145), which remains open and unmerged.

## Status

Active pending current-head verification and the unresolved local app-instance gate failure. The implementation is delivered in PR #145 with Drew's accepted manual-testing deviation. No more manual acceptance is required for this preparation pass. Keep the effort active until the app-instance failure is resolved, final validation passes, and current-head Greptile and CI are recorded. Then close it on this PR under repository release policy. No merge or release is authorized by this update.
