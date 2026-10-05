---
aliases:
    - SPEC-0003
id: SPEC-0003
last-updated: 2026-06-19T00:00:00Z
spec-status: archived
summary: Single-page orientation for agents starting work in a spec-driven delivery repo.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Agent workflow

## Summary

This is the file to read first when an agent starts work in a spec-driven delivery repo. It links the normative specs and the bundled skills so the workflow is self-describing.

Link this file from your repo's `AGENTS.md` or `CLAUDE.md` so every new session finds it early.

## Process

The canonical workflow has a simple-task path and a complex-effort path. Read these, in order:

1. `docs/specs/process/development-loop.md` — the normative loop.
2. `docs/specs/process/effort-lifecycle.md` — what an effort must preserve while active and how it closes.
3. `docs/specs/process/story-lifecycle.md` — how stories become `ready`, carry typed criteria, and move to `satisfied`.
4. `docs/specs/process/lifecycle-immutability.md` — what is and is not editable on a `complete`/`archived` effort, and how follow-on efforts pick up where prior efforts closed.
5. `docs/specs/process/ontology-design-principles.md` — modeling philosophy for typed notes and relations.
6. `docs/specs/process/id-allocation.md` — how to allocate top-level ids, author story ids, and mint acceptance-criterion locators on demand.
7. `docs/specs/process/agent-skills.md` — which skill to invoke at each phase.
8. `docs/specs/process/debugging-workflow.md` — how to investigate bugs before proposing fixes.
9. `docs/specs/process/review-handling.md` — how to verify and route review feedback.
10. `docs/specs/process/audits.md` — the audit types `alignment-audit` runs.
11. `docs/specs/process/compounding-work.md` — how repeated friction becomes future capacity.
12. `docs/specs/process/quality-gates.md` — what verification evidence is required before handoff.
13. `docs/specs/process/testing-policy.md` — TDD and regression-test expectations for behavior changes.
14. `docs/specs/process/specs-organization.md` — where new specs go.

## Workflow Rules

- Classify work before choosing ceremony. Use the simple-task path for narrow, clear changes; use the complex-effort path when structure, scope, or risk deserves explicit planning.
- Route raw context before implementation: source artifacts with provenance go through `ingest-transcript`; brain dumps and strawman feature ideas go through `specify`.
- Use `specify` directly when the job is defining desired behavior for later implementation; it does not require starting a development loop.
- Plan before you edit when the task is complex or the approach is not already concrete.
- Allocate the right top-level id before creating a new spec or effort, then mirror it into `aliases:`.
- Only pull `ready` stories into a new effort unless the user explicitly wants to include story-shaping work.
- Get explicit human approval recorded in the effort before you start executing complex-effort plans.
- Record deviations in the effort as they happen, not at closure.
- Use `effort-finish` to run quality gates, audit, backport durable learnings, compound repeated friction, and mark an effort `complete`.
- Quality gates come from `docs/specs/process/quality-gates.md`; starter examples are not universal commands.

## Core Skills

| Intent | Skill | Path |
|---|---|---|
| Route start/resume/what-now | `development-loop` | `.agents/skills/development-loop/SKILL.md` |
| Define intended behavior | `specify` | `.agents/skills/specify/SKILL.md` |
| Execute concrete plan/task | `implement` | `.agents/skills/implement/SKILL.md` |
| Close effort | `effort-finish` | `.agents/skills/effort-finish/SKILL.md` |

## Companion skills

- `effort-new` — create and freeze a new effort for complex work
- `plan` — produce the decision-complete technical implementation plan
- `foundation-review` — guided architecture review after a foundation-heavy phase, before later phases depend on it
- `alignment-audit` — typed findings pass for spec/doc/link/drift validation
- `backport` — reconcile implementation truth into durable specs/docs/skills
- `compound` — convert repeated friction into future-capacity work
- `debugging` — investigate bugs before proposing fixes
- `rhizome-review-feedback` — verify review comments against repo reality before changing code or docs
- `code-docs` — maintain code-adjacent docs, `CONTEXT.md`, coderefs, and code anchors
- `ingest-transcript` — convert transcript, meeting, sticky-note, SME, or other source material into durable ontology-backed notes
- `quality-gates-check` — run and summarize the repo-defined gates from `quality-gates.md`
- `rhizome` — gather orientation, use core Rhizome operations, and load focused structured-markdown or ontology guidance while the more-specific workflow skill retains deliverable ownership
- `skill-creator` (when present) — create or revise skills; ask whether Rhizome capabilities would help, and load the `rhizome` skill-authoring guidance only when the user opts in

When the Claude harness is enabled, the same bundled repo skills live under `.claude/skills/`.

## Goals

- keep agent orientation to one readable page
- link, do not duplicate; authoritative rules live in the individual specs

## Non-Goals

- re-explaining skill bodies
- replacing the managed Rhizome guidance block in `AGENTS.md` / `CLAUDE.md`; that block covers the retrieval surface

## Requirements

- This file stays short. If something grows, move it into the relevant normative spec.

## Open Questions

- whether this file should also embed a quick-start onboarding diff for new contributors
