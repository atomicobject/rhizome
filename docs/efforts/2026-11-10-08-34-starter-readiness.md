---
type: EffortNote
id: EFF-2026-11-10-08-34
name: Starter readiness and recovery
created-at: 2026-11-10T13:34:58Z
status: active
summary: Make installed starters complete their setup and indexing with visible readiness and recovery, and refresh ontology data after folder-only changes.
aliases: [EFF-2026-11-10-08-34]
governing-specs:
  - "[[starter-readiness]]"
---

# Starter readiness and recovery

## Scope

Deliver [[starter-readiness|SPEC-0137]] using the existing module-action and runtime-indexing owners. The motivating incident is an AO KB installation in `agentic-process` that collided with an existing lowercase schema and exposed no complete setup or recovery flow. Existing per-file automatic ontology refresh remains in place.

## Spec Set (Frozen)

- [[starter-readiness|SPEC-0137]], initial revision on this branch dated 2026-11-10, all requirements and US1 through US3.

## Stories In Scope (Frozen)

- [[starter-readiness#^SPEC-0137-US1]]
- [[starter-readiness#^SPEC-0137-US2]]
- [[starter-readiness#^SPEC-0137-US3]]

## Spec Coverage Checklist

- [ ] Shared readiness and automatic eligible setup, including CLI installation and runtime reopen.
- [ ] Persistent visible status, concrete recovery actions, conflict and input handling.
- [x] Ontology folder-only event refresh and client invalidation.
- [ ] Regression tests, required gates, and real fixture UI verification.

## Plan

1. Compare two Sol design sketches against one readiness truth, reuse of existing execution owners, unambiguous automatic work, and bounded recovery. Record the selected contract before implementation.
2. Implement and test the independent ontology-folder watcher fix in an isolated worktree.
3. Implement the shared starter readiness/setup owner and AO KB integration, then its desktop/API consumers. Reuse action trust and the existing index lane. Keep authored content unchanged.
4. Verify focused behavior, full repository gates, generated starter consistency, and the visible flow in a packaged fixture app. Independently review the integrated change and resolve accepted findings.
5. Record delivery evidence, deviations, and closure on the feature branch. Do not install the unmerged branch over Drew's Rhizome Dev app.

Throughput checkpoint: design and the shared contract precede implementation; watcher hardening is independent; writers use isolated worktrees; one owner holds the coupled readiness implementation to avoid competing state models.

## Original Intended Delivery

A starter installation reaches ready automatically when possible, otherwise remains visibly blocked with an appropriate recovery action. Schema conflicts do not look like empty content. Whole ontology folder changes refresh typed data.

## Actual Delivered

Implementation is in progress.

## Execution Notes

- 2026-11-10T13:34:58Z: Drew approved the preceding recommendation with "OK. Make the changes. Use sol". All delegated design, implementation, and review use GPT-6.1 Sol. The current user is not configured in this checkout, so approval is recorded here and `plan-approved-by` remains absent.
- Existing unrelated changes in `AGENTS.md` and `pkg/app/web/assets/dist/index.html` remain untouched.
- Grounding verified that individual `.graphql` changes already force ontology republication. The reported `artifact` collision occurs while building the executable query schema, which must be checked separately from authored SDL validity.
- The Sol watcher implementation was reviewed and integrated as `323462c5`. Watch admission and change classification now share `assetroots.IsAssetRootOrAncestor`; the full live ownership test checks typed records and client invalidation after create, remove, and rename events for each ontology ancestor. The worker passed race-enabled tests and vet for assetroots, watchhub, and bootstrap, plus the file-size gate. Full integrated verification remains pending.
- Integrated verification: the folder regression passes with host permissions. The sandboxed run cannot acquire the macOS process-identity-backed lane lease; this is an environment constraint, not a flaky pass.
- Design decision: use a runtime-owned readiness flow declared by a module and executed through existing actions. Its states distinguish checking, working, required input, blocked, failed, and ready. Keep initial indexing and starter readiness separate. Reuse the runtime's existing index owner, reconstruct or persist unfinished work, and reject stale results. Reject a desktop-only checklist because it misses CLI installs and reopened repositories. The second Sol sketch remains an implementation review input.

## Deviations

None.

## Compounding Follow-ups

None identified yet.

## Closure Checklist

- [ ] Approved scope delivered or deviations recorded.
- [ ] Required gates and UI evidence recorded.
- [ ] Independent review completed and accepted findings fixed.
- [ ] Delivery branch and commit recorded.

## Status

Active on `feat/starter-readiness`.
