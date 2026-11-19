---
name: effort-research
description: "Use after an effort's scope is frozen or re-frozen and before planning, when `docs/engineering/review-and-approval.md` lists this gate or the user asks for research: the user decides Run or Skip, and a Run records checked facts, dependencies, pitfalls, and assumptions on the effort. Not for scope that is still changing (`agentic-engineering specify`), a plan-requested review of formative decisions (`foundation-review`), or ordinary exploration with no frozen effort."
argument-hint: "[effort path]"
user-invocable: true
---

# Effort Research

Run the bounded research gate between scope freeze and implementation planning.
Repository instructions and the frozen effort and specs remain authoritative for
project-specific scope, commands, and note structure.

## When this gate applies

This gate applies when `docs/engineering/review-and-approval.md` lists `effort-research`, or when the user asks for it. When the policy does not list it and the user has not asked, do nothing and say nothing about it. A listed gate is part of the approved work, not a new approval of the plan.

**Adoption.** An effort frozen before the gate was listed does not go back through it. Repository history cannot say reliably which efforts those are, so do not infer it from dates or commits. When the effort already has an approved plan and no research decision, it predates the gate: append one `[decision]` line to `Execution Notes` naming this gate as adopted and the current `HEAD` as the adoption commit, then continue; research does not apply to it. When the effort has no approved plan, present the assessment as usual and offer a third answer beside Run and Skip, `This effort predates the gate`, which records the same adoption line and lets planning continue. Record the adoption line once per effort. The user may still ask for research on an adopted effort.

**Where output goes.** A Markdown effort keeps the research sections under its `Plan` section and the `[decision]` line in `Execution Notes`. A workspace effort keeps the research sections in its linked plan document under the same headings, and the `[decision]` line in the `Execution Notes` section of its Markdown work log. Retrieval returns a workspace plan by path only, so a reader opens the linked plan document. Timestamps are UTC to the second, from `date -u +"%Y-%m-%dT%H:%M:%SZ"` when the line is written.

## Invariants

- The user owns the Run/Skip decision. Never skip autonomously.
- Record one timestamped `[decision]` line for the current frozen scope before
  continuing. Do not repeat a decision already recorded for that exact scope.
- Research is read-only except for its effort-note output. Never install packages.
- Research owns checked facts and explicit assumptions. Planning owns design,
  sequencing, and task breakdown.
- A finding that invalidates frozen scope stops the pass. Present the evidence and
  let the user choose re-slicing or an explicitly recorded adaptation.

## Evidence contract

Every research claim carries exactly one tag earned by an act in this session:

- `[VERIFIED]` — a command was run or a local file was read. Cite the command or
  `path:line` evidence.
- `[CITED]` — a URL was fetched. Cite the fetched URL near the claim.
- `[ASSUMED]` — the claim remains unchecked. Point to its Assumptions Log row.

A claim that something is the only writer or reader of a column, table, or state is
`[VERIFIED]` only when the search covered every spelling of it: the SQL name and the
ORM field name, in every host language. Otherwise it is `[ASSUMED]`.

Use the highest-authority source available: installed/locked truth, repository
truth, registry metadata, version-matched official docs, then community sources.
Training-data recall is only a hypothesis. Never copy fetched prose into the effort
note, and never put confidential terms into web queries.

For a dependency absent from the repository, verify registry existence, age,
repository provenance, and runtime/peer compatibility without installing it.

## Operating model

Use a fresh-context research subagent when the platform supports one. Give it the
effort path, frozen-spec paths, and derived research questions; do not paste the
answers or bias it with an implementation proposal. The subagent writes the defined
research sections directly into the effort note and returns only a compact summary.
If subagents are unavailable, run the identical procedure inline.

During delegated research, relay bounded progress after orientation and each major
area: 3–5 safe findings with earned tags and evidence pointers, remaining areas,
and any degraded lane. Never relay credentials, signed URLs, client data, complete
response bodies, or copied web prose.

## Procedure

1. Reuse the current Rhizome session or start one targeted at the effort and frozen
   specs. Resolve an explicitly named effort first; otherwise choose the newest
   frozen/planned effort that has no research decision for its current scope.
2. Load the frozen stories, acceptance criteria, assumptions, exclusions, prior
   research, and dependency diff. Prefer a repository-supplied execution-context
   recipe when available; otherwise read the artifacts directly.
3. Capture the repository's normal Rhizome validation finding count as the pre-write
   baseline.
4. Perform only a cheap candidate scan. Name uncertain external contracts, new or
   changed dependencies, uncovered subsystems, and new module boundaries. Do not
   begin the research itself yet.
5. Present exactly these assessment fields, starting with the resolved effort:
   `Research candidates`, `Skip rationale`, and `Recommendation`. Recommend Skip
   only for trivial/well-understood work with no dependency change and complete
   prior coverage. Even zero candidates receive the assessment.
6. Ask Run/Skip through the platform's structured question tool when available;
   otherwise ask plainly. Mark the assessed recommendation, but let the user decide.
7. Immediately append one UTC `[decision]` line:
   - `Research run: user-approved; output in Plan > Technical context and Assumptions Log.`
   - or `Research skipped: <reason> (user-approved).`
   A Skip is complete after this write.
8. For Run, derive at least one bounded question per frozen story: what must be true
   in the repository or ecosystem for that story to be implementable as frozen?
9. Investigate each question in authority order. Cover the full frozen story set,
   then stop; research is not open-ended exploration.
10. For every pitfall, determine the failure, trigger, avoidance, and detection
    signal. Record the concise conclusion, not a long narrative.
11. Write the outputs below into the effort note. A negative result worth preserving
    becomes an append-only timestamped `[learning]` line in Execution Notes.
12. Re-inspect the effort note with Rhizome and rerun the same validation scope.
    Only new findings belong to this pass. A broken Rhizome binary is a visible
    blocked state; degraded semantic/vector recall may continue, but affected
    negative claims are capped at MEDIUM confidence and the confidence line names
    the recall gap.

## Required effort-note output

Under `Plan > Technical context`, write:

- A gap table framed as current state as of a date and commit, explicitly “input to
  planning, not a task list.” Each frozen story is `met`, `partially met`, `missing`,
  or `diverges`, with evidence. Mark reusable implementation as `EXISTS — reuse, do
  not rebuild` and lead with missing/divergent rows.
- Dependency findings, including an explicit `No new dependencies needed` when
  that is what was checked and found.
- Pitfalls.
- Open questions. Each states what is known, what is unclear, and the concrete
  verification path. An unresolved question must become the plan's first task,
  before dependent work.
- Exactly one line per researched area: `Confidence: HIGH|MEDIUM|LOW — researched
  YYYY-MM-DD`. A researched empty area still gets a one-line nothing-found result
  above its confidence line.

Under `Plan > Assumptions Log`, write exactly the active `[ASSUMED]` claims:

```markdown
| Assumption | Risk if wrong |
|------------|---------------|
| <unchecked claim> [ASSUMED] | <concrete consequence> |
```

Remove a row only after the complete claim earns a higher-authority tag. If none
remain, write `Assumptions: none.` When requesting plan approval later, quote these
rows verbatim or state `Assumptions: none.`

## Terminal states

- **Complete** — name the recorded decision and, for Run, the Technical context,
  Assumptions Log, learning entries, and post-write validation result.
- **Blocked** — name the concrete blocker and user-owned route. Valid blockers
  include no frozen stories, scope-invalidating evidence, an unresolved delegated
  failure, or an unusable required repository tool.

Use `agentic-engineering plan` after this gate. Use `agentic-engineering specify`
while scope is still changing, `foundation-review` when an approved plan asks for a
review of formative decisions, and ordinary repository exploration when no frozen
effort exists.
