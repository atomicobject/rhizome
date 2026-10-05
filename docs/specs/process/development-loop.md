---
aliases:
    - SPEC-0001
id: SPEC-0001
last-updated: 2026-06-19T00:00:00Z
spec-status: archived
summary: Defines the default task and effort workflows for this repository.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Development loop

## Summary

This repo uses a spec-driven workflow so intent, execution, rationale, and future-capacity improvements stay durable and queryable. The workflow has three common paths:

- **Raw/current context intake**: transcripts, sticky notes, SME conversations, debugging sessions, or brain dumps are routed into durable source notes, specs, or effort context before implementation.
- **Simple task**: a bounded code or documentation change whose desired behavior is already clear. Effort scope, planning, implementation, verification, and a spec sanity check may happen in one compressed pass.
- **Complex effort**: a larger or riskier change that needs explicit specification, effort creation, implementation planning, foundation review when core structure is involved, phased execution, and formal closure.

The goal is not ceremony. The goal is to choose the lightest workflow that still preserves the contracts future agents and humans need.

## Goals

- keep specs normative, not retrospective
- make active work auditable through effort notes
- require deviations, backports, and compounding follow-ups instead of silent drift
- make the next skill obvious when a user asks "what now?"

## Non-Goals

- replacing issue trackers or source control
- forcing every reference note into a rigid subtype
- defining architecture, runtimes, or product behavior in this spec

## Requirements

### Must

- Start from `agent-skills.md` and route through the highest-level skill that fits the user's intent. Prefer `development-loop` when the next step is unclear, `specify` when desired behavior is being defined, and `effort-finish` when implementation appears complete.
- Raw or current context is routed before implementation:
  1. Use `ingest-transcript` for provenance-bearing source material: transcripts, interviews, meeting notes, sticky-note exports, research notes, or SME conversations.
  2. Use `specify` for brain dumps, strawman feature ideas, and conversational refinement of desired behavior.
  3. Use `effort-new` from current context only after a governing spec/story/criterion slice is clear enough to freeze.
  4. If the user asks for the wrong phase while signaling uncertainty, route them before executing the named skill.
- Classify work before choosing ceremony:
  - **Simple task** if the desired behavior is clear, the change is narrow, no durable schema/API/module boundary is being established, and no unresolved product or architecture decision blocks implementation.
  - **Complex effort** if the work introduces or changes schemas, storage contracts, public APIs, module boundaries, cross-subsystem behavior, migrations, security/data-loss-sensitive flows, or multi-phase feature delivery.
- Simple tasks may use a compressed workflow:
  1. Confirm the existing spec or documented behavior that governs the change.
  2. Do not create a new effort for a trivial typo, cleanup, or localized fix unless the user, repo policy, risk, or future traceability requires it.
  3. If durable traceability is warranted, create or update a terse effort note and record the plan inline; effort and plan may be produced in the same pass.
  4. Implement the change with tests and docs appropriate to the scope.
  5. Run the repository-defined quality gates from `quality-gates.md`.
  6. Run a spec sanity check: confirm no spec incompatibility, undocumented behavior drift, or missing code-doc/spec update was introduced.
  7. If an effort exists, close it through `effort-finish`; otherwise report verification evidence and any deferred follow-up.
- Complex efforts follow the full workflow:
  1. **Specify**: update the active spec in place by default, or create a new spec only when no authoritative note exists yet.
  2. **Effort**: choose one `YYYY-MM-DD-HH-MM-<slug>.md` prospective path under `docs/efforts/`, allocate its id through the live EffortNote strategy (`EFF-YYYY-MM-DD-HH-MM[-N]` in fresh spec-driven projects; `EFF-XXXX` in preserved sequential projects), freeze the spec set, and freeze the selected story / acceptance-criterion nodes with wikilinks to their durable block-id targets.
  3. **Plan**: derive a decision-complete implementation plan recorded in the effort.
  4. **Foundation phase**: when the work establishes core structure, make Phase 1 build the formative schemas, modules, APIs, storage contracts, or internal interfaces that later phases depend on.
  5. **Foundation review**: after the foundation phase, use `foundation-review` before later feature phases build on those decisions.
  6. **Execute**: implement remaining phases against the plan, keeping docs, tests, and effort notes current.
  7. **Finish**: use `effort-finish` to orchestrate quality gates, alignment audit, backport, compounding triage, closure checklist updates, and the final `status: complete` flip.
- Superseding a spec is exceptional. When a spec is marked `superseded`, it must point at an explicit `successor`; normal workflow should keep the active spec authoritative and evolve it in place.
- Every effort note includes: `id`, `name`, `created-at`, `plan-approved-by`, `status`, `summary`, scope, frozen spec set, frozen story/criterion scope, coverage checklist, plan, original intended delivery, actual delivered, execution notes, deviations, compounding follow-ups, `Closure Checklist`, and status narrative.
- Complex-effort execution does not begin until all of the following are true:
  - The effort note exists under `docs/efforts/`.
  - The `Plan` section is decision-complete (tasks, tests, and acceptance criteria).
  - The plan has explicit user approval recorded in the effort header, with `plan-approved-by` set to the approver's actual human name rather than a placeholder, handle, or shorthand.
- A simple task may record effort scope and plan together when the user has already authorized the concrete change and the work satisfies the simple-task criteria above.
- Implementation phases belong only in `Plan` prose. They are a planning convention, not typed ontology workflow state.
- Complex-effort plans that establish long-lived structure must include an explicit foundation phase before feature-completion phases.
- Specs that drive execution include a `User Stories` section with story-level ids, statuses, and typed acceptance criteria.
- Story readiness, satisfaction, acceptance-criterion structure, and carry-forward rules are defined in `story-lifecycle.md` and apply to every story selected into an effort.
- Effort creation includes an explicit story-selection step. The default is not "all stories"; decide which `ready` stories the effort will execute now.
- When a spec uses story `increment` numbers, effort creation may use those numbers to narrow the candidate slice, but the effort still freezes the exact selected story and criterion nodes as wikilinks in `Stories In Scope (Frozen)`.
- Behavior-changing structural refactors require a spec delta before implementation edits begin.
- If execution is interrupted or aborted, the default next step is back to `Plan`.
- Execution resumes after an interruption only when the user explicitly confirms resume and scope has not changed.
- Deviations discovered during execution or audit are recorded in the effort note.
- `Deviations` is never left as `None` when scope, sequence, acceptance criteria, or implementation approach changed.
- Backport updates are applied before closing an effort when behavior diverges from specs.
- Human decisions, agent surprises, and new conclusions discovered during work are logged in the effort's `Execution Notes` with UTC DateTime entries and event-kind tags such as `[decision]`, `[surprise]`, or `[learning]`.
- Decisions and surprises stay in the effort unless they change durable spec truth, reusable reference guidance, code documentation, or skill behavior. Backport only that changed truth; do not create a separate decision artifact for routine effort rationale.
- Behavior-changing structural refactor efforts must include a `spec-alignment` audit before closure.
- An effort is not truthfully `complete` until `Closure Checklist` confirms implementation, quality gates, audit, backport, and compounding work are done.
- Effort closure is owned by `effort-finish`. That skill may use `quality-gates-check`, `alignment-audit`, `backport`, and `compound` as sub-agent or sequential helper phases, but it does not flip `status: complete` until the closure checklist is true. See [`effort-lifecycle.md`](effort-lifecycle.md) "Closure procedure" for the exact handoff and inputs.
- During implementation and backport, agents should keep effort-local rationale in `Execution Notes`; create or update specs, reference notes, code docs, or skills only when the rationale changes reusable or authoritative guidance.
- When linking implementation rationale, agents should prefer the smallest durable ontology node that explains the constraint, such as an embedded user story, typed acceptance criterion, or rationale block with a block-ID wikilink, rather than linking only to the parent spec.
- During compound, agents should record repeated friction and missing agent capabilities in `Compounding Follow-ups` rather than leaving them in chat, PR comments, or private prompts.

### Should

- Audits include at least `spec-alignment` and one domain-specific type from `audits.md`.
- Efforts are scoped so they can be completed and audited in a single focused pass.
- `rzm agent validate` runs before handoff for every effort, scoped to checks relevant to the change.
- Agents should choose the simple-task path when the full complex-effort path would add traceability noise without reducing risk.
- Agents should choose the complex-effort path whenever a later phase will depend on foundational design decisions made now.

### May

- Add additional audit types when risk or scope warrants it.

## Phase Exit Criteria

- **Raw/current context intake**: source material is preserved with provenance, or the rough context has been routed to `specify`, `effort-new`, or the simple-task path with unresolved questions made explicit.
- **Simple task**: behavior is clear, implementation is complete, relevant tests/docs are updated, repo-defined quality gates have run or been explicitly deferred, and the spec sanity check found no unresolved drift.
- **Specify**: target behavior is documented and no unresolved MUST-level ambiguity blocks implementation.
- **Effort**: effort note exists with frozen spec set, frozen story scope, and populated coverage checklist.
- **Plan**: implementation approach is decision-complete with tests, acceptance criteria, and any required foundation phase.
- **Foundation review**: formative schemas, modules, APIs, interfaces, and ownership boundaries have been reviewed before later phases depend on them.
- **Execute**: code changes are complete and required quality checks have run according to `quality-gates.md`.
- **Finish effort**: quality gates, audit, backport, compounding triage, closure checklist, and status update are complete.

## Open Questions

- none currently; `plan-approved-by` is the only required approval metadata
