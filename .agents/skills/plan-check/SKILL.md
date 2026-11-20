---
name: plan-check
description: "Use after drafting or structurally revising an effort's plan and before asking for approval, when `docs/engineering/review-and-approval.md` lists this gate or the user asks for a plan check: a fresh reviewer checks traceability, silent scope reduction, verification that can fail, and assumptions. Not for a plan-requested review of formative decisions (`foundation-review`) and not for reviewing code (`phase-code-review`)."
argument-hint: "[effort path]"
user-invocable: true
---

# Plan Check

Run an adversarial, fresh-context check between plan drafting and approval. The
effort's frozen scope and local repository instructions remain authoritative.

## When this gate applies

This gate applies when the Gates table in `docs/engineering/review-and-approval.md` has a row naming `plan-check`, or when the user asks for it. A mention of the skill elsewhere in that document does not turn it on. When the table has no such row and the user has not asked, do nothing and say nothing about it. A listed gate is part of the approved work, not a new approval of the plan.

**Adoption.** A plan approved before the gate was listed is not re-checked, and repository history cannot say reliably when that was, so do not infer it from dates or commits. These adoption rules apply only when the gate was entered because the policy lists it; when the user asked for the gate, run it and record no adoption. A plan that is drafted or structurally revised and not yet approved is always checked, whenever it was started and whatever adoption line the effort carries. Otherwise, when the current plan revision is approved and the effort has no `plan-check` line, append one `[decision]` line to `Execution Notes` naming this gate as adopted and the current `HEAD` as the adoption commit, and stop. Record the adoption line once per effort.

**Where output goes.** A Markdown effort keeps the `[validation]` and `[decision]` lines in `Execution Notes`; the plan it checks is the `Plan` section. A workspace effort keeps the `[validation]` and `[decision]` lines in the `Execution Notes` section of its Markdown work log; the plan it checks, with any research and discussion output, is the linked plan document. Retrieval returns a workspace plan by path only, so a reader opens the linked plan document. Timestamps are UTC to the second, from `date -u +"%Y-%m-%dT%H:%M:%SZ"` when the line is written.

**Which plan an outcome covers.** Every `[validation]` outcome line ends with `plan <hash>`: the first 12 characters of the checked plan's content hash, taken after the run's last plan edit. For a Markdown effort that is `awk '/^## Plan$/{f=1;next} /^## /{f=0} f' <effort note> | git hash-object --stdin`; for a workspace effort it is `git hash-object <linked plan document>`. An outcome covers a plan that is not yet approved only when its hash equals the current one, a line with no hash covers none, and a run that stopped with blockers standing covers none unless a `force-past` line follows it: when they differ the plan changed after the check, in this session or an earlier one, and the gate runs again. The one exception is an edit this conversation made itself that changes no task, file, verification, or scope, such as a typo: append `plan-check: carried forward to plan <new hash> — <what changed>` as a `[validation]` line instead of re-running.

## Invariants

- Run after every draft or structural revision of an effort's plan. There is no
  assessment, Run/Skip question, or skip path.
- The checker assumes the plan is flawed until evidence proves otherwise. Plan
  completeness is not the same as goal achievement.
- A fresh reviewer checks each iteration, writes nothing, and returns findings only.
- The main conversation verifies findings, edits the plan, and records the outcome.
- Ask for plan approval only after the latest plan has zero blockers or the user has
  explicitly forced past the remaining blockers.

## Four required checks

1. **Story-to-task traceability.** Every frozen story maps to at least one task, and
   every task maps to frozen scope. A missing story or untraceable task is a BLOCKER.
2. **Silent scope reduction.** Inspect terms such as `v1`, `v2`, `simplified`,
   `static for now`, `hardcoded`, `future enhancement`, `placeholder`, `minimal`,
   `will be wired later`, `skip for now`, `stub`, and time/difficulty language used
   to justify omission. A match is evidence only when it reduces a frozen story;
   confirmed reduction is always a BLOCKER. Deliver fully or route to explicit
   re-slicing—never silently narrow scope.
3. **Task completeness and meaningful verification.** Each task needs concrete
   owned files/surfaces, an action, a fail-capable verification, and done criteria.
   Flag vague actions and checks that can pass without testing the property. Common
   BLOCKER patterns include impossible anchored greps over tree-formatted output,
   swallowed errors feeding permissive comparisons, `|| true`/`|| :` masking a
   measured command, and a semicolon that makes a trailing pass token unconditional.
   A hard-coded count without measurement provenance is a WARNING. So is a phase
   exit line that does not name the gate `docs/engineering/quality-gates.md`
   gives for every kind of path the phase changes.
4. **Frozen facts and assumptions.** Schedule every unresolved research question's
   verification before dependent work. Do not rebuild/remove anything research
   marked `EXISTS — reuse, do not rebuild`. Treat research as architectural context,
   not as a substitute for live measurement. Preserve every Assumptions Log row
   unchanged into the approval request; flag contradictions. Flag a decision resting
   on something being the only writer or reader of a column unless research verified
   that with a search covering both its SQL and ORM names (WARNING).

## Severity and return contract

- **BLOCKER** — frozen scope will not be delivered. It blocks approval unless the
  user explicitly force-pasts it and that decision is recorded.
- **WARNING** — quality or maintainability is degraded. Advisory; it never drives a
  revision iteration.
- **INFO** — suggestion only.

Every finding names exactly one severity, its check, the location, the unmet
property, and a concrete fix hint. The reviewer returns either:

```text
PLAN CHECK PASSED — <effort>, iteration <n>
<four-row check summary>
```

or:

```text
PLAN CHECK — ISSUES FOUND — <effort>, iteration <n>
<counts by severity>
<numbered findings, blockers first>
```

## Procedure

1. Resolve the effort containing the current plan: an explicitly named effort
   first; otherwise the effort whose plan this conversation just drafted or
   revised; when more than one candidate remains, ask rather than guess. Load
   the frozen stories, acceptance criteria, research decision and synthesis,
   open questions, Assumptions Log, and complete plan. Do not review a stale
   revision. Capture the repository's normal validation finding count as a
   pre-write baseline for step 6's differential re-inspection.
2. Spawn a fresh-context, read-only checker. Give it artifact paths rather than an
   inlined plan, require all four checks above, and require the return contract.
   When the harness has no subagent, run the same checks inline and put
   `degraded: no fresh-context reviewer` in the outcome line.
3. Present the entire result. Confirm every finding against the actual artifacts.
4. Fix all confirmed BLOCKERs and appropriate advisories without altering content
   the checker did not flag. Re-run a fresh checker over the full revised plan.
5. Stop when there are zero BLOCKERs. Maximum three iterations per run. If the
   blocker count fails to decrease between consecutive iterations, stop early as a
   stall. On exhaustion or stall, let the user choose a different fix, force-past,
   or abandon the plan. A later user-routed re-entry starts a new three-iteration
   run.
6. Append exactly one UTC `[validation]` Execution Note for the run, recording
   verdict, iteration count, blockers found/fixed, and `plan <hash>`. Mention outstanding warnings
   in that line when the user approves over them. Then re-inspect the effort note
   with the repository's normal validation scope; only findings the plan edits
   introduced belong to this run.
7. When the user force-pasts a BLOCKER, append a separate `[decision]` line using
   the literal `force-past` term and naming the blocker and that approval is
   requested with it standing. Closure surfaces every such line.
8. Request plan approval only now. Quote Assumptions Log rows verbatim, or state
   `Assumptions: none.`

Example routine outcome:

```markdown
- 2026-06-02T15:30:00Z [validation] plan-check: PASS after 2 iterations; 2 blockers found and fixed (traceability gap, vacuous verification), 1 warning fixed; plan 3f9a1c0b7d2e.
```

Use `agentic-engineering plan` to draft or structurally revise the plan,
`foundation-review` when an approved plan asks for a review of formative
decisions, and a phase review for an implemented diff. This skill never
reviews code or edits the plan from reviewer context.
