---
type: EffortNote
id: EFF-2026-11-02-17-54
aliases: [EFF-2026-11-02-17-54]
name: Desktop provider access settings
created-at: 2026-11-02T22:54:04Z
status: active
summary: "Make provider routing explicit in Settings, manage local keys, and report the connections running repositories actually use."
governing-specs: ["[[team-api-gateway]]"]
---

# Desktop provider access settings

## Scope

Implement the approved Opus design as one Provider access section in desktop Settings. Keep server sign-in, per-provider route selection, and local key management together. Voyage and Jev each offer Atomic Object server or Your key. Manual entry and 1Password import populate the same saved key storage. Signing in alone does not change routing. Include actual running-repository reports and restart guidance in this delivery, rather than deferring them.

## Spec Set (Frozen)

- [[team-api-gateway|SPEC-0129]], 2026-11-02 approved provider-access increment: desktop US5 and the trusted-origin/precedence and provider-access reporting requirements. Existing gateway admission, billing, deployment and identity contracts are unchanged.

## Stories In Scope (Frozen)

- [[team-api-gateway#^SPEC-0129-US5|SPEC-0129 US5]]: Settings account, explicit route controls, manual key entry and 1Password import.
- [[team-api-gateway#Trusted origin and precedence|Trusted origin and precedence]]: explicit user-global route preference, with unset preserving existing behavior.
- Provider-access reporting requirements under [[team-api-gateway#requirements|Requirements]]: active runtime connection versus next construction, unknown older runtime reports, and restart feedback.

## Spec Coverage Checklist

- [x] Explicit server routing overrides all local keys and fails closed.
- [x] Explicit key routing excludes server and bundle; unset preserves existing precedence.
- [x] Private atomic key/preference saves preserve unrelated user settings and import references.
- [x] Settings presents source choices and secret-free per-provider reports.
- [x] Running repositories report actual connections separately from next-call decisions.
- [x] Older runtimes show unknown; Voyage changes offer authorized headless restart.
- [ ] Focused and integrated checks, rendered packaged verification, independent review and Greptile pass.

## Plan

1. Implement and test the shared resolver and user-global route field. Use one secret-free report contract for factory decisions, desktop defaults and runtime snapshots. Capture constructed Voyage connections and Jev clients; expose active and next reports through verified loopback runtime health.
2. Implement desktop bridge mutations and status propagation, serialized with setup/import through the existing native global lock. Keys travel only in stdin JSON, never in command arguments, logs or results. Preserve the existing 1Password import/cancellation mechanism.
3. Implement the approved Settings hierarchy using the existing visual system. Reuse account and import flows; show next-run defaults separately from actual running repositories. Provide keyboard-accessible key replacement/clear, error handling, and restart controls that respect attached-runtime ownership.
4. Update the owning spec, package notes, credential policy, desktop guide and changelog. Run focused behavior tests, repository gates, desktop checks/build and a packaged fixture. Review the integrated diff independently, address findings, obtain current-head Greptile 5/5 and green CI through the normal PR flow. Do not deploy AWS, change budgets or publish a client release.

## Plan Approval

- 2026-11-02: Drew approved the Opus Settings design by replying "Yep" to the proposal with two explicit amendments: server selection overrides local environment keys, and status reflects the running repository including restart requirements and unknown older runtimes. This authorizes all four batches above. The current-user command reports no configured identity, so `plan-approved-by` is not inferred; chat approval is recorded here.

## Original Intended Delivery

A signed-in user can choose where Voyage and Jev calls go, enter or import their own keys, and inspect actual repository routing without exposing credentials or confusing sign-in with provider use.

## Actual Delivered

Delivered through [PR #77](https://github.com/atomicobject/rhizome/pull/77) on `t3/provider-access-settings-ready`, rebased onto main `6da57df811c551a4cd9f9b0f59104122873afa13`. Implementation and prior fixes are carried by `eb7c5054`, `1ba76374`, `1dbd09fd` and `4d5deb84`. Settings now owns explicit per-provider routing, manual key management, the existing 1Password importer, app-default reports and actual runtime connection reports with restart actions. Native configuration writes preserve unrelated values and import references; reports expose provenance without credentials.

## Deviations

The approved amendments replace the design draft's environment-key override and its proposed deferral of running-repository reporting. Packaged native visual control was refused by the computer-use harness. Direct rendered proof instead ran the actual frontend with synthetic bridge responses in desktop and narrow browser viewports; native bridge tests and both packaged builds passed. No native click-through or live paid provider traffic is claimed.

## Closure Checklist

- [x] Approved scope implemented and owning documentation reconciled.
- [ ] Required gates and review evidence recorded with material limitations.
- [x] Delivery commit and pull request identified.

## Compounding Follow-ups

Runtime reports deliberately compare routing provenance rather than credential values. Saved-key connections therefore retain restart guidance even when the source name is unchanged. Native computer-use access to the separate verification app was refused; local browser proof uses the actual frontend with synthetic bridge responses, alongside native bridge tests and packaged builds.

## Status

Active: implementation, independent review and rendered frontend proof are complete. Final configuration-preservation review, current-head Greptile, PR CI and installation remain.

## Execution Notes

- Identifier allocation and authoring-guide discovery succeeded outside the sandbox after the sandbox denied the runtime boot-time identity check. The broader code-mode context query timed out; direct owning policy, spec and package notes supply that evidence gap. Current-user discovery returned `missing_current_user`.
- Existing generated `pkg/app/web/assets/dist/index.html` changes predate this effort and remain outside the change set.
- Local verification before rebase: `make check-full` passed, including race and integration tests and 1,741 web tests. `make desktop-check` passed 111 UI tests, 110 native tests, formatting and Clippy. `make desktop-build` and the separately identified fixture app both packaged successfully.
- Independent integration review found two concrete restart gaps: a runtime that failed Voyage initialization could not apply newly available access, and saved-key restart notices vanished across windows/remounts. Both are fixed with focused regressions and a fresh approved recheck. Imports now broadcast after every outcome because cancellation can race a committed save.
- Sol-loop round 1 used GPT-6.1 Sol, high reasoning effort, on implementation commit `67d3b0fd`: APPROVE, no blocker or should findings. Sol independently passed provider/desktop/runtime Go tests and 76 selected UI tests. A changed-code recheck follows the Greptile fixes below.
- Browser proof passed server selection, password-form Escape cancellation, clear confirmation, active-versus-next reports, no fabricated Jev connection, older-runtime unknown status and narrow-layout overflow checks. Desktop and narrow screenshots were inspected. All data were synthetic, and no paid provider call was made.
- Documentation validation after rebuilding the rebased CLI passed with 0 issues. A local full-gate attempt encountered untracked cache symlinks in execution-source snapshots; those local dependency paths were excluded and the gate was restarted. The original worktree and its unrelated generated asset change remain untouched.
- Greptile's initial review scored 4/5 and identified two confirmed defects: synchronous keychain access could stall health, and an older settings-save response could overwrite a newer other-window refresh. Health now returns an immutable cached report immediately and refreshes in the background; pending reports say checking. Save responses use the same revision guard as refreshes. Focused regressions exercise a blocked session loader and delayed save responses.
- Sol-loop round 2 approved both fixes at `bd96792e`, with no new findings; the reviewer independently passed the blocked-loader race regression and all 12 provider UI tests.
- Greptile's second review scored 4/5 and identified configuration loss for unknown fields and save errors disappearing after successful refresh. Provider writes now patch only selected entries in the original YAML tree, preserving unknown nested data and rejecting trailing documents without rewriting. Refresh and save errors use independent state. Focused race-enabled desktop/config tests and all 13 provider UI tests passed, including unknown-field preservation and retained save-error regressions.
- Frozen-scope validation reports only the pre-existing SPEC-0111 drift in EFF-2026-10-03-18-33. The current gateway effort records this approved provider-settings increment as a scoped refreeze; unrelated historical work remains untouched.
- Sol-loop round 3 approved `96e377fd`, resolving configuration preservation and persistent save errors. Final local static/web checks, 113 desktop UI tests, 110 native tests, Clippy, packaging and documentation validation passed.
- Greptile's third review confirmed the four earlier findings were fixed and identified dangling YAML references after editing an anchored key. A regression reproduced corruption for both replace and clear. Generated YAML is now parsed before publication, rejecting those edits while preserving the original file byte for byte. The review loop extends one round for this concrete configuration data-loss issue.

- Sol-loop round 4 approved the final YAML-reference fix at `4d5deb84`, independently passing all provider-settings regressions with the race detector. The branch is rebased onto main `6da57df8`; the new main changes are unrelated group overview work. Final current-base gates and Greptile are running before closure.

- The rebased full gate passed with 1,765 web tests and race-enabled integration suites. Actual frontend browser proof and documentation validation passed again. Greptile round 4 confirmed G1–G5 fixed and identified YAML merge inheritance changing unrelated effective settings. Synthetic regressions reproduced root merge, target merge and shared-map side effects. Provider writes now reject inherited/shared root or target mappings without changing bytes; focused race tests passed. The final allowed Sol round checks this configuration data-loss fix.
