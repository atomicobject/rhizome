---
aliases:
    - SPEC-0024
id: SPEC-0024
last-updated: 2026-07-16T00:00:00Z
spec-status: archived
summary: Defines user-story authoring, readiness, satisfaction, acceptance-criterion nodes, and carry-forward behavior in the spec-driven workflow.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Story lifecycle

## Summary

User stories are the main execution unit inside a spec. They stay embedded in the authoritative spec, carry typed acceptance criteria, and move through a small status vocabulary so effort scoping stays explicit without turning the spec into a running log or effort tracker.

## Goals

- keep story authoring simple and uniform
- define what `ready` means before a story enters an effort
- make incomplete work easy to carry forward without corrupting the spec history
- keep planning and execution ownership in efforts rather than story properties

## Non-Goals

- forcing authors to model inter-story dependencies
- replacing implementation plans with story prose
- requiring fine-grained estimation systems or story points
- tracking current effort ownership from the story itself

## Requirements

### Must

- Every delivery-driving spec includes a `User Stories` section.
- Every user story includes:
  - a descriptive H3 title in the form `USn - <outcome>`
  - a stable authored `- id::` metadata bullet with a leading caret before the story id, such as `- id:: ^SPEC-0007-US1`
  - a `- summary::` metadata bullet
  - a `- status::` metadata bullet
  - an `Acceptance Criteria` subsection
- Story titles should describe the work/outcome, not the role phrase. Prefer `US1 - Unified code and note context for development tasks` over `As an implementer starting a task`.
- User stories do not carry their own requirements list or requirement field. Put story-local obligations in acceptance criteria; put cross-story or system-level obligations in the spec's `Requirements` section.
- Each acceptance criterion is a direct unordered list-item embedded node under the story's `Acceptance Criteria` subsection.
- Acceptance criteria do not have authored `id::` fields. Their list-item text and optional indented continuation lines define the criterion; a durable external link target is added only when the criterion is cited from code, coderefs, other notes, or selected as a subset into an effort's `Stories In Scope (Frozen)`.
- When an acceptance criterion needs a durable target, run `rzm agent node-link --target <spec#criterion-fragment> --ensure plan` and use the returned locator. The repair mints a plain standalone block id such as `^SPEC-0007-US1-AC1`; it must not invent an `id::` property for the criterion.
- Validation cleanup MAY remove an unreferenced acceptance-criterion block locator when no inbound external references remain. Per [SPEC-0023](../technical/linkable-embedded-node-identifiers.md), that locator is opt-in linkability, not durable identity. The cleanup is opt-in, reviewable, and never default-on.
- Acceptance-criterion bullets should be short observable statements. Use `**Short title**: outcome` only when the title improves scanning. Indented continuation lines describe behavior, state transition, evidence, or edge case only when the one-line criterion is not enough. Use Given/When/Then only when it makes that description clearer.
- Acceptance criteria may include a concise indented `verification::` inline property when the expected check is already known.
- User stories may carry an optional spec-local `increment` number:
  - `1` means first intended delivery increment for that spec
  - `2` means second intended delivery increment for that spec
  - and so on
- `increment` is scoped to the parent spec, not to the whole repo or a global release train.
- User stories use exactly these statuses:
  - `draft` — not yet shaped enough to enter an effort
  - `ready` — eligible to be selected into a new effort
  - `satisfied` — delivered behavior satisfies the criteria and the spec/backport reflects reality
- A story may be marked `ready` only when:
  - its acceptance criteria are present as typed criterion nodes,
  - each acceptance criterion is independently observable; add indented detail when the criterion bullet alone is not self-explanatory,
  - its wording is specific enough for planning,
  - major blocking open questions are resolved or explicitly parked outside the story, and
  - any must-read supporting references or decisions are linked from the story subtree when they materially affect implementation.
- User stories do not carry estimates. Effort sizing, risk, sequencing, and time-boxing belong in the effort `Plan` prose.
- User stories do not carry a current effort link. Efforts own the relationship by linking selected story and criterion nodes in `Stories In Scope (Frozen)`; use durable block-target wikilinks, not plain text ids.
- `increment` expresses intended rollout grouping only. It does not replace `status`:
  - a story may be `increment: 2` and still `draft`
  - a story may be `increment: 2` and later become `ready`
  - a story may be `increment: 2` and eventually become `satisfied`
- Efforts select stories from the set of `ready` stories unless the user explicitly asks to pull in unfinished shaping work.
- When a spec uses `increment`, effort creation may use it as a selection aid, such as "create an effort for increment 3 of this spec," but the effort still freezes the exact chosen story ids and criterion locators rather than relying on increment number alone as the execution contract.
- A story becomes `satisfied` only after:
  - the effort that executed it is closed or effectively complete,
  - the delivered behavior satisfies the frozen acceptance criteria or an explicit backport changed them,
  - and the spec/backport now describes reality.
- If an effort closes without fully delivering a selected story:
  - the story does not move to `satisfied`,
  - the closing effort records the carry-forward in `Actual Delivered` and `Deviations`,
  - the story stays `ready` or moves back to `draft` if the criterion contract needs more shaping,
  - and the next effort re-freezes the story ids and selected criterion locators explicitly if work continues.

### Mid-effort acceptance-criterion changes

- Story ids stay stable once assigned (covered above). Story content — body prose, acceptance-criterion text, and selected AC locators — has stricter rules once an effort has frozen the story:
  - If an active or planned effort lists the story or any of its criteria in `Stories In Scope (Frozen)`, AC text changes or AC locator removals require a `Deviations` entry on every affected effort that names what changed and why.
  - Adding a block locator to a previously uncited frozen AC is **not** a deviation; the locator just makes the criterion externally addressable without changing the criterion's content. Selecting an uncited AC into an effort's `Stories In Scope (Frozen)` counts as an external citation and triggers locator minting at freeze time so the effort's frozen scope cites a durable target.
  - If the AC contract needs reshaping enough that the frozen behavior no longer matches the new AC text, move the story back to `draft` first, let the affected effort close out (carry-forward, do not mark `satisfied`), and re-freeze the selected AC locators in a follow-on effort. See [[SPEC-0051|SPEC-0051 lifecycle immutability]] for the follow-on-effort handoff rules.
  - The `frozen_scope_drift` alignment-audit check (`rzm agent validate frozen-scope-drift`) flags edits to a frozen spec made after the effort's `created-at` so this rule is enforceable rather than honor-system.

### Cross-references

- `EffortStatus` (`planned`, `active`, `complete`, `archived`) and `SpecStatus` (`proposed`, `active`, `superseded`, `archived`) are documented in [[SPEC-0002|SPEC-0002 effort lifecycle]] and the schema's `@guidance`. Story status is independent of either: a story can be `ready` under a `proposed` spec, and `satisfied` while its parent spec is still `active`.

### Should

- Keep one user-facing outcome per story.
- Put supporting links in the story subtree rather than scattering them elsewhere in the spec.
- Put test/evidence hints on the smallest applicable acceptance criterion when a concise `verification::` value is known.
- Use document order as the default priority order unless the repo later adds a stronger priority field.
- When a spec spans multiple intended revisions, use `increment` to make the rollout grouping explicit instead of relying on open-ended prose labels.

## Open Questions

- none currently; interruption details belong in effort `Execution Notes`, not story status
