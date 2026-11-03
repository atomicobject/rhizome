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

- [ ] Explicit server routing overrides all local keys and fails closed.
- [ ] Explicit key routing excludes server and bundle; unset preserves existing precedence.
- [ ] Private atomic key/preference saves preserve unrelated user settings and import references.
- [ ] Settings presents source choices and secret-free per-provider reports.
- [ ] Running repositories report actual connections separately from next-call decisions.
- [ ] Older runtimes show unknown; Voyage changes offer authorized headless restart.
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

Implementation is in progress on `t3/provider-access-settings`.

## Deviations

None. The approved amendments replace the design draft's environment-key override and its proposed deferral of running-repository reporting.

## Closure Checklist

- [ ] Approved scope implemented and owning documentation reconciled.
- [ ] Required gates and review evidence recorded with material limitations.
- [ ] Delivery commit and pull request identified.

## Compounding Follow-ups

Runtime reports deliberately compare routing provenance rather than credential values. Saved-key connections therefore retain restart guidance even when the source name is unchanged. Native computer-use access to the separate verification app was refused; local browser proof uses the actual frontend with synthetic bridge responses, alongside native bridge tests and packaged builds.

## Status

Active: routing, bridge and Settings implementation proceed in independent owned lanes.

## Execution Notes

- Identifier allocation and authoring-guide discovery succeeded outside the sandbox after the sandbox denied the runtime boot-time identity check. The broader code-mode context query timed out; direct owning policy, spec and package notes supply that evidence gap. Current-user discovery returned `missing_current_user`.
- Existing generated `pkg/app/web/assets/dist/index.html` changes predate this effort and remain outside the change set.
