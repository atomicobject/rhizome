---
aliases:
    - SPEC-0007
id: SPEC-0007
last-updated: 2026-07-19T00:00:00Z
spec-status: archived
summary: Maps bundled agent skills to the simple-task and complex-effort workflows.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Agent skills

## Summary

This spec tells agents which bundled skills to invoke for common Agentic Engineering work. Workflow skills ship with the `agentic-engineering` starter and live canonically under `.agents/skills/`; universal Rhizome mechanics come from the separately installed base `rhizome` skill. Claude mirrors the shared tree under `.claude/skills/` when enabled, while other harnesses use their configured shared surfaces. Name skills by name and let the harness resolve the location.

The user-facing surface is intentionally small. Most users should think in terms of `agentic-engineering`, `specify`, `implement`, and `effort-finish`. `agentic-engineering` is the default entrypoint when delivery work is beginning, resuming, or unclear; it lazily routes to one phase resource at a time. The retained adapters provide direct entry to known phases and preserve their human-invocable boundaries. `specify` can also stand alone as upstream knowledge-management work by a delivery lead, product owner, or other collaborator who is teeing up implementation but not starting it.

## Goals

- give agents a single discovery surface for simple tasks, complex efforts, and closure
- keep skill invocation aligned with the installed Agentic Engineering router and local engineering policy
- make it obvious when a skill applies and when to stop
- make neighboring-skill boundaries explicit enough to reduce routing mistakes

## Non-Goals

- documenting CLI flags for `rzm agent` (current mechanics live in the base `rhizome` skill and its focused references)
- replacing the SKILL.md bodies; this file routes, it does not re-explain

## Requirements

### Must

- Load and consult this spec before starting new work in an Agentic Engineering delivery repo.
- Use the top-level routing table below unless the user explicitly asks for a specific skill or phase and that skill clearly matches the request. If the user names a skill while signaling uncertainty or a mismatched phase, route first.
- Stop at phase boundaries the selected skill marks as hard stops. Simple-task routing may intentionally combine shaping, implementation, and focused verification when the router classifies the task as clear and local.
- When a phase skill declares a required saved-query recipe or required discovery pattern, run it before making the phase decision it protects.
- `specify` MUST search related contracts before creating a new spec; update or reference an existing spec when the requested behavior belongs there.
- Effort-bearing phase skills MUST load the current effort's frozen spec set and frozen story scope before planning, implementation, alignment, reconciliation, compounding, or closure decisions.
- Skills that create typed note ids MUST use `rzm agent next-id` when the live surface advertises it, and mirror the allocated id into `aliases:` according to the type's identifier contract.
- Skills that edit specs referenced by `planned` or `active` efforts MUST account for `frozen-scope-drift`; run `rzm agent validate frozen-scope-drift` where the phase contract calls for drift evidence.
- Code-evidence discovery MAY require `semantic-query` and `file-context` patterns in addition to saved GraphQL query recipes until a multi-tool recipe format exists.
- Effort closure MUST route through `effort-finish`. `effort-finish` owns quality-gate evidence, alignment, reconciliation, compounding triage, closure checklist updates, and the final `status: complete` flip.
- Workflow skills MUST compose with the base `rhizome` skill for universal session, search, file-context, exact-evidence, and safe Markdown mechanics instead of copying a second command manual.
- The owning workflow skill keeps control of its deliverable; loading `rhizome` does not change the workflow phase or expand scope.

## Rule Ownership

- Ontology `@guidance` owns artifact semantics: fields, section meaning, identifier rules, required relations, and typed note shape.
- Process specs own cross-artifact workflow rules: phase order, closure gates, routing boundaries, and validation expectations.
- Skills own procedure: when to load context, which recipe or command to run, what decision the evidence protects, and when to stop or hand off.
- A skill may repeat a rule only as a local hard gate at the point where an agent is likely to skip it. Mark deliberate repeats with `<!-- deliberate repeat: <source> -->` so future edits do not create a second source of truth.

## Top-level routing

| User intent or state | Skill | Purpose |
|---|---|---|
| "What should I do next?", start/resume/continue work, unclear phase | `agentic-engineering` | Classify the work and load the one phase resource that owns the next decision |
| Raw transcript, sticky notes, SME conversation, meeting notes, or source artifact | `ingest-transcript` | Preserve source context with provenance and propose durable destinations |
| Source-backed requirements, domain process/workflow modeling, or requirement traceability in a repo with the complex-domain extension | `complex-domain` | Route to requirements ingest, curation, domain modeling, spec-from-domain, traceability review, or domain backport |
| Brain dump, strawman, or current conversation about desired behavior | `specify` | Turn rough context into a cohesive spec or spec recommendations |
| Desired behavior, scope, or acceptance criteria still moving | `specify` | Write or update a spec grounded in codebase context |
| Approved task or phase ready to execute, including a failure whose fix shape must be established | `implement` | Execute the approved plan or investigate within its implementation phase, then keep durable artifacts in sync |
| Refactor, reorganization, duplication cleanup, module-boundary question, review feedback, or uncertain delivery next step | `agentic-engineering` | Classify the decision, gather the relevant evidence, and route to planning, implementation, alignment, reconciliation, or compounding |
| Implementation appears complete for an effort | `effort-finish` | Run closure: quality gates, alignment, reconciliation, compounding, checklist, and status |

## Lower-level phase skills

| Phase | Skill | Purpose |
|---|---|---|
| Effort creation | `effort-new` | Create the effort note and freeze the spec set for complex efforts |
| Planning | `plan` | Produce a decision-complete implementation plan in the effort |
| Foundation review | `foundation-review` | Critique formative schemas, modules, APIs, and boundaries before later phases build on them |
| Quality evidence | `agentic-engineering` | Load the quality-gates phase resource and report pass/fail/deferred evidence from local policy |
| Alignment, reconciliation, or compounding | `agentic-engineering` | Load the corresponding router phase resource; create durable follow-up work when evidence shows a gap |

## Common asks

- "What skill should I use next?" -> `agentic-engineering`
- "Turn this brain dump into a strawman spec." -> `specify`
- "Create an effort from the current context." -> `agentic-engineering` first; `effort-new` only after the scope is stable enough to freeze
- "Ingest this raw transcript / sticky-note export / SME conversation." -> `ingest-transcript`
- "Should we refactor/reorganize this module?" -> `agentic-engineering` (planning or compounding phase)
- "This test is flaky / worked yesterday / crashes sometimes." -> `implement`
- "Is this spec still true?" -> `agentic-engineering` (alignment phase)
- "Docs say one thing but the code does another." -> `agentic-engineering` (alignment or reconciliation phase)
- "Run the pre-merge checks / quality gates." -> `agentic-engineering` (quality-gates phase)
- "Implementation is done; close this effort." -> `effort-finish`
- "Review this foundation/refactor before we build on it." -> `foundation-review`
- "The agent keeps asking the same question / we keep hitting this blocker." -> `agentic-engineering` (compounding phase)
- "A review comment says to change this; verify what it means." -> `agentic-engineering`

## Routing Regression Examples

These examples are deterministic skill-routing fixtures. Keep them aligned with skill descriptions, frontmatter hints, and the common-asks table so starter updates do not silently lose trigger coverage.

| Input shape | Expected skill |
|---|---|
| "This flaky test worked yesterday; find the root cause." | `implement` |
| "Docs say X but code does Y; is the spec still true?" | `agentic-engineering` |
| "Should we split this package or leave it alone?" | `agentic-engineering` |
| "Run pre-merge checks and tell me what failed." | `agentic-engineering` |
| "Implementation is done; close the effort." | `effort-finish` |
| "We keep hitting the same missing-evidence problem." | `agentic-engineering` |
| "Turn this conversation into a strawman spec." | `specify` |

## Companion skills (not phase-gated)

- `agentic-engineering` — top-level router for clear tasks, complex efforts, resume states, and phase handoffs. Its resources own quality gates, alignment, reconciliation, and compounding.
- `rhizome` — universal Rhizome mechanics and lazy routing for sessions, search, file context, onboarding, installation, configuration, indexing, validation, safe Markdown mutation, structured notes, ontology operations, reports, and troubleshooting. Compose it with the owning workflow skill when those capabilities are material.
- `foundation-review` — run a guided review after a foundation-heavy phase before later phases build on top of it.
- `ingest-transcript` — turn transcripts, structured meeting notes, sticky-note exports, SME conversations, and raw source artifacts into ontology-backed specs, reference docs, domain notes, candidate work slices, and effort-context updates with provenance.
- `skill-creator` (when present) — own general skill creation or revision. Ask whether Rhizome capabilities such as search, GraphQL queries, or query recipes would help; if the user opts in, load the `rhizome` skill-authoring guidance.

## Boundary cues

- `specify` vs `plan`: use `specify` while desired behavior or scope is still moving; use `plan` once requirements are stable and the task is technical execution design.
- `plan` vs `agentic-engineering`: use the router when the question is whether to restructure, reconcile, or compound; use `plan` when the desired behavior is stable and the implementation approach needs sequencing.
- `ingest-transcript` vs `specify`: use `ingest-transcript` when source provenance matters; use `specify` when the user is brainstorming or reacting to a strawman and the output should become intended behavior.
- `agentic-engineering` vs `effort-new` / `plan`: use the router when deciding simple task vs complex effort; use `effort-new` or `plan` only when that lower-level phase is already known.
- `plan` vs `implement`: use `plan` to decide approach, task graph, foundation phase, and checks; use `implement` only after the plan is decision-complete and approved, or when a simple-task change is already concrete enough for the compressed path.
- `agentic-engineering` vs `implement`: use the router when the investigation may change scope or phase; use `implement` when the failure belongs to an approved implementation slice or the fix can be bounded as a clear task.
- `implement` vs `effort-finish`: use `implement` to change behavior; use `effort-finish` once the effort's implementation scope appears complete and closure evidence needs orchestration.
- `effort-finish` vs `agentic-engineering`: use `effort-finish` for closure orchestration; use the router's reconciliation or compounding phase when durable correction or leverage is the current goal without closing an effort.
- `domain-backport` vs `agentic-engineering`: use `domain-backport` when delivery evidence changes requirements, sources, processes, workflows, or domain notes; use the router's reconciliation phase for specs and delivery artifacts.
- `rhizome` is a capability skill, not a delivery-phase owner: use it for Rhizome mechanics, then continue under the workflow skill that owns the outcome.
- `agentic-engineering` is a coordinator skill: use it until the correct top-level path is known, then hand off to the owning workflow or phase adapter.

## Open Questions

- whether an optional reporting bundle (`diff-specs`, `stand-up`, `remaining-items`) should ship once the core loop stabilizes
