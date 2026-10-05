---
aliases:
    - SPEC-0002
id: SPEC-0002
last-updated: 2026-08-10T00:00:00Z
spec-status: archived
summary: Defines what an effort note must preserve while work is active and how it closes.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.

# Effort lifecycle

## Summary

Efforts are bounded execution records against a frozen set of specs. They are the durable home for plan, execution notes, deviations, and closure evidence — everything that would otherwise get lost in chat or commit messages.

Not every effort needs the same amount of ceremony. A simple task may keep scope and plan terse, while a complex effort needs a decision-complete plan, phase boundaries, and sometimes foundation review before later work builds on formative structure.

## Goals

- make active work legible at a glance
- preserve deviations and execution evidence
- give closure, audit, backport, and compounding work an explicit but lightweight home

## Non-Goals

- replacing specs with implementation detail
- using effort notes as permanent design documentation
- tracking day-to-day chatter; keep execution notes load-bearing

## Requirements

### Must

- Every effort note lives under `docs/efforts/` and is named `YYYY-MM-DD-HH-MM-<slug>.md`.
- Frontmatter contains:
  - `id`
  - `name`
  - `created-at`
  - `plan-approved-by` (blank until real human approval)
  - `status`
  - `summary`
- Body contains, in order:
  - `Scope`
  - `Spec Set (Frozen)` — wikilinks to every governing `SpecLike` note plus its spec `id`; links in this section are indexed as the canonical frozen-spec relation
  - `Stories In Scope (Frozen)` — wikilinks to embedded story nodes using canonical `SPEC-####.US#` display text, plus selected acceptance-criterion wikilinks using canonical `SPEC-####.US#.AC#` display text; links in this section are indexed as canonical frozen-story / frozen-criterion relations to embedded graph nodes
  - `Spec Coverage Checklist` — one item per selected story / acceptance criterion, reusing the same durable story / criterion wikilinks from `Stories In Scope (Frozen)`
  - `Plan` — populated by `plan` and approved before execution
  - `Original Intended Delivery` — what the frozen stories said this effort intended to ship
  - `Actual Delivered` — what the effort actually shipped, deferred, or changed
  - `Execution Notes` — appended during work as the effort-local journal; never rewritten historically
  - `Deviations` — each deviation names what changed and why
  - `Compounding Follow-ups` — repeated friction, missing support, capability gaps, and smallest durable fixes
  - `Closure Checklist` — one checklist item for implementation complete, audit run, backport complete, and compounding triage complete
  - `Status` — current effort status using the repo's effort-status vocabulary
- `plan-approved-by` is set only by a real human name. Placeholders (`user`, `owner`, `<pending>`, handles, shorthand) are forbidden.
- `created-at` is the canonical effort creation/start timestamp. Filename timestamp remains naming/discovery metadata only.
- `created-at` and each timestamped `Execution Notes` entry use UTC DateTime format with second precision, e.g. `2026-05-19T18:42:00Z`. Bare dates are for spec `last-updated`, not effort event history.
- `id` follows the live EffortNote identifier strategy and is mirrored into `aliases:`. Fresh Agentic Engineering projects use filename-derived `EFF-YYYY-MM-DD-HH-MM[-N]`; already-adopted sequential projects retain `EFF-XXXX` until an explicit identifier migration is approved and applied.
- Starting an effort includes choosing which `ready` user stories and criteria are in scope now, then linking each selected embedded node from `Stories In Scope (Frozen)` with a durable block-target wikilink.
- Efforts own story execution relations by linking selected story and acceptance-criterion nodes in `Stories In Scope (Frozen)`; stories do not carry live effort links.
- Effort-local task lists, coverage checklists, rationale notes, and execution notes should reuse those durable story / criterion wikilinks when they point at selected scope. Plain labels like `[SPEC-0007.US1]` are readable but not graph edges.
- `Execution Notes` entries are the effort-local log for human decisions, agent surprises, new conclusions, validation evidence, blockers, handoffs, and resume points. Use this shape: `- 2026-05-19T18:42:00Z [decision] Colthorp approved the narrower scope because ...`.
- Required `Execution Notes` entry kinds are `[decision]`, `[surprise]`, `[learning]`, `[validation]`, `[blocker]`, `[handoff]`, and `[resume]`. Use `[note]` only for load-bearing execution evidence that does not fit another kind.
- Human decisions and agent surprises stay in the effort unless they change durable spec truth, code behavior, or reusable operating guidance. In that case, backport the changed truth to the owning spec, reference, code doc, or skill, and keep the timestamped effort entry as the provenance trail.
- If a selected story is not fully delivered when the effort closes, record the carry-forward in `Actual Delivered` / `Deviations` and move the story back to an appropriate non-complete state rather than forcing closure in the spec.
- Deviations are recorded as they happen, not backfilled at closure.
- An effort closes only after:
  - every coverage checklist item is `[x]`,
  - every open deviation is resolved or explicitly carried forward,
  - required quality gates have run or been explicitly deferred according to `quality-gates.md`,
  - required audits have run,
  - durable learnings that changed authoritative contracts have been backported via `backport`,
  - repeated friction and missing agent capabilities have been triaged via `compound`, and
  - every `Closure Checklist` item is `[x]`.
- Resumed efforts record the resume point in `Execution Notes`.

### Should

- One effort per focused change; split multi-subsystem work into separate efforts.
- Cross-reference related efforts instead of merging their scopes.
- Simple-task efforts may keep required sections terse and may record scope plus plan in one pass when `development-loop.md` allows the compressed path.
- Complex efforts should include explicit phase boundaries in `Plan`, and should include a foundation phase when core schemas, APIs, storage contracts, module boundaries, or internal interfaces will shape later work.

### May

- Link to external tickets or PRs from `Execution Notes`.
- Keep terse effort-local rationale in `Execution Notes` even when no spec, reference, or code doc update is warranted.

## Effort lifecycle states

The full `EffortStatus` vocabulary is enumerated in `.rhizome/ontology/spec-driven.graphql`. Use these values in `status:` truthfully:

- `planned` — the effort exists and scope is frozen, but active execution has not started yet. Use this between `effort-new` and `plan` finishing, and between plan-approval and the first execution edit.
- `active` — execution is currently live. Keep `status`, `Execution Notes`, and `Deviations` current while work is underway.
- `complete` — the bounded slice is finished and the effort stands as the completed delivery record. See the closure procedure below; do not flip to `complete` until every closure gate is satisfied.
- `archived` — historical effort retained for provenance. Use this for retired or cancelled efforts that should remain discoverable but should not be treated as live execution surface. Cancellations land here only after a Deviation entry records why.

`SpecStatus` (`proposed`, `active`, `superseded`, `archived`) and `UserStoryStatus` (`draft`, `ready`, `satisfied`) are independent of effort status. A `complete` effort can target an `active` spec whose stories have been promoted to `satisfied`, while a `planned` effort can target a `proposed` spec only if the effort is explicitly story-shaping work.

## Closure procedure

A `complete` effort is a closure that survives quality gates, audit, backport, and compounding triage. Closure is owned by `effort-finish`, which may invoke the specialist skills as sub-agents when the harness supports that or run the same phases sequentially when it does not. The implement-phase agent does not flip `status: complete` itself.

1. **Implementation complete.** `implement` lands the last code/test/doc edits. Append `Actual Delivered` against the existing `Original Intended Delivery` baseline (do not rewrite the baseline). Carry-forward any partially delivered story by recording it in `Actual Delivered` and `Deviations`, and move the story back to a non-`satisfied` state.
2. **Quality gates.** Run the repository-defined checks from `quality-gates.md`, typically through `quality-gates-check`. Record pass/fail/deferred evidence in the effort.
3. **Alignment audit.** Use `alignment-audit` with inputs: effort note path, frozen `Spec Set` wikilinks, frozen `Stories In Scope` wikilinks, touched code paths, recorded deviations, and relevant audit types from `audits.md`. The audit emits findings only; it does not mutate specs or close the effort. Tick the audit item in `Closure Checklist` only after required audits have run and blocking findings are resolved or carried forward.
4. **Backport.** Use `backport` with inputs: effort note path, audit findings, deviations marked for spec/reference/code-doc reconciliation, and timestamped `[decision]`, `[surprise]`, or `[learning]` execution notes that changed authoritative truth. Tick each `Spec Coverage Checklist` item only when it is actually true. Reconcile any remaining gap between `Original Intended Delivery` and `Actual Delivered`. Tick the backport item in `Closure Checklist` when durable reconciliation is complete.
5. **Compound.** Use `compound` with inputs: effort note path, contents of `Compounding Follow-ups`, repeated-friction / missing-evidence / missing-access / agent-blocker observations from execution. Triage each into the smallest durable home (test, script, skill, capability request, follow-on effort). Tick the compounding item in `Closure Checklist` when triage is complete.
6. **Flip `status` to `complete`** only when all closure gates are satisfied:
   - every `Spec Coverage Checklist` item is `[x]`,
   - every open `Deviations` entry is resolved or explicitly carried forward,
   - every `Closure Checklist` item is `[x]`.

After closure the effort note is historical record. See [SPEC-0051 lifecycle immutability](lifecycle-immutability.md) for the rules governing post-closure edits and how follow-on efforts pick up where prior efforts closed.

## Open Questions

- whether the repo wants mandatory approver fields beyond `plan-approved-by`
