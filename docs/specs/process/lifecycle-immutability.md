---
aliases:
    - SPEC-0051
id: SPEC-0051
last-updated: 2026-07-16T00:00:00Z
spec-status: archived
summary: Defines what is and is not editable on a complete or archived effort, and how follow-on efforts pick up where prior efforts closed.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Lifecycle immutability

## Status

**Active.** Validation repair now enforces lifecycle protection for operations touching complete or archived efforts, and `--allow-historical` bypasses only that protection while preserving every ordinary transaction safety gate. The append-only provenance convention remains normative. A stronger repository-wide validator for arbitrary manual post-closure edits remains an open follow-up.

## Summary

A `complete` or `archived` effort is historical record. Once closed, an effort is not the right place to land new behavior, new scope, or revised plans — those belong in a follow-on effort. This spec defines exactly which post-closure edits are still permitted, which are forbidden, and how follow-on efforts pick up where prior efforts closed.

The intent is simple: closed efforts are evidence. They should remain trustworthy as a record of what was scoped, planned, executed, audited, backported, and compounded at the time. Skills that route by lifecycle state — `effort-new`, `specify`, `backport`, `alignment-audit`, `rhizome-review-feedback` — depend on closure being a real boundary, not a soft one.

## Goals

- keep `complete` and `archived` efforts trustworthy as historical record
- give follow-on work a deterministic home rather than an in-place rewrite of the prior effort
- make post-closure edits explicit and auditable (`(post-closure: ...)` provenance) instead of silent
- close the long-standing gap that "what an agent should NEVER touch on a `status: complete` effort" was never written down

## Non-Goals

- forbidding all edits to a closed effort (broken-link repairs and provenance cross-links remain allowed)
- replacing the closure procedure in [SPEC-0002 effort lifecycle](effort-lifecycle.md) (this spec layers on top of it)
- modeling effort cancellation as a separate status (cancellations land in `archived` with a Deviation entry)

## Requirements

### Must

- A `complete` or `archived` effort is treated as historical record. The fields and sections enumerated under "Allowed post-closure edits" below are the only legitimate places to edit a closed effort. Every other edit is forbidden.
- `Plan`, `Scope`, `Spec Set (Frozen)`, `Stories In Scope (Frozen)`, `Spec Coverage Checklist`, `Closure Checklist`, `Original Intended Delivery`, `Actual Delivered`, `Status`, `id`, `created-at`, `name`, `summary`, and `plan-approved-by` are read-only after closure. Do not retroactively tick checklist items, rewrite the plan, or rename the effort.
- `Execution Notes`, `Deviations`, and `Compounding Follow-ups` remain append-only after closure. Late evidence may still land there, but every post-closure entry must be prefixed with `(post-closure: <reason>)` so the closed-vs-late split stays legible.
- `status: complete → archived` is the only status transition allowed after closure, and only when the effort should remain discoverable but should not be treated as a live execution surface (e.g. retired specs, cancelled work that was already closed-as-complete). Archival never reopens an effort.
- When a new ask lands against a delivered spec slice, create a fresh effort whose `Stories In Scope (Frozen)` reselects the relevant ids. Do NOT reopen the prior effort. The prior effort stays `complete` (or `archived`); the follow-on effort owns the new scope.
- A follow-on effort owns carried-forward story relations through its `Stories In Scope (Frozen)` section. User stories do not carry authored effort links, and the closed predecessor's `Stories In Scope (Frozen)` does not change.
- Broken-link repairs in any closed effort's body are allowed when the underlying note has been moved or renamed. Such repairs preserve the original prose intent; do not edit the surrounding sentence.
- Findings that materialize after closure (security review, late audit, retro feedback) land in a follow-on effort's body, not the closed effort's `Plan`. The follow-on effort cites the closed effort in `Spec Set (Frozen)` companion text or in its `Execution Notes`.

### Should

- Prefer `archived` over deleting closed efforts; provenance survives even when the work is no longer relevant.
- When a closed effort's spec slice gets a new ask, link the prior effort from the follow-on effort's `Execution Notes` ("continues from EFF-0008's …") so navigation forward and back stays cheap.
- When in doubt about whether an edit is "broken-link repair" versus "rewriting history," default to opening a Deviation in a follow-on effort and letting the closed effort be.

## Allowed post-closure edits

These are the only edits permitted on a `complete` or `archived` effort:

1. `status: complete → archived` (one-way; never reopen).
2. Append-only entries to `Execution Notes`, `Deviations`, or `Compounding Follow-ups`, each prefixed with `(post-closure: <reason>)`.
3. Broken-link repairs in any section's wikilinks/embeds when the underlying note has been moved or renamed. Repairs preserve the surrounding prose.
4. Provenance cross-links from a follow-on effort (the follow-on cites the closed effort; the closed effort is not edited to add the citation).

Any other edit — checklist re-tick, plan rewrite, scope addition, summary rewrite, frontmatter status reopen, etc. — is forbidden. If the work needs revisiting, open a follow-on effort.

## Automated repair enforcement

- Repair planning classifies every operation touching a `complete` or `archived` effort as lifecycle-allowed or lifecycle-protected before transaction grouping.
- Allowed operations are limited to the post-closure edits above and proceed without an override.
- A connected transaction containing a protected historical edit is skipped by default with the protected paths/regions and exact replan command.
- `--allow-historical` authorizes only that lifecycle-policy exception. It does not bypass stale source preconditions, ambiguity, edit overlap, destination collision, ontology constraints, transaction recovery, or post-validation.
- Agent and CI surfaces never infer the override; they return structured blocked guidance. Human repair must pass it explicitly.
- Delivery of this enforcement satisfied the trigger for activating this spec; stronger repository-wide validation remains a follow-up.

## Follow-on efforts

When new work lands against a delivered or partially-delivered spec slice:

- Create a fresh effort via `effort-new`.
- Freeze a NEW selection of stories. The selection may include:
  - newly added `ready` stories on the same spec,
  - stories that were carried forward unsatisfied from the prior effort (already moved back to `ready` or `draft` by the prior closure),
  - previously-`satisfied` stories whose AC has been edited and the story has been moved back to `ready` first (see [story lifecycle / mid-effort acceptance-criterion changes](story-lifecycle.md#mid-effort-acceptance-criterion-changes)).
- Freeze each carried-forward story in the follow-on effort's `Stories In Scope (Frozen)`. User stories do not carry authored effort links, and the closed predecessor's `Stories In Scope (Frozen)` is NOT modified.
- Cite the closed predecessor from the follow-on effort's `Execution Notes` so navigation stays cheap.

## Open Questions

- whether the repo wants a stronger automated guardrail (validation check) that rejects arbitrary manual post-closure edits to forbidden sections, beyond the existing repair-path enforcement and `frozen_scope_drift` check covering live efforts; manual edits remain honor-system plus alignment-audit
- whether `archived` should require a Deviation entry recording the reason, or whether `(post-closure: archived because ...)` execution-note prefix is enough
