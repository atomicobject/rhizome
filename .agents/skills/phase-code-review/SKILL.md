---
name: phase-code-review
description: "Use after an effort's implementation phase completes and before the next begins, or once before closure for a single-phase plan, when `docs/engineering/review-and-approval.md` lists this gate or the user asks for a phase review: a fresh reviewer looks for logic, security-logic, and test-quality defects in the phase's diff. Not for a plan-requested review of formative decisions (`foundation-review`) and not for a pull request review."
argument-hint: "[effort path]"
user-invocable: true
---

# Phase Code Review

Run an adversarial review of a completed implementation phase before another phase
builds on it. A phase is one delivery batch of the approved plan. Repository
instructions, the approved effort plan, and the frozen scope remain authoritative.

## When this gate applies

This gate applies when the Gates table in `docs/engineering/review-and-approval.md` has a row naming `phase-code-review`, or when the user asks for it. A mention of the skill elsewhere in that document does not turn it on. When the table has no such row and the user has not asked, do nothing and say nothing about it. A listed gate is part of the approved work, not a new approval of the plan.

**Adoption.** An effort that was already being implemented when the gate was listed does not have its finished phases reviewed after the fact. Repository history cannot say reliably which efforts those are, so do not infer it from dates or commits: ask. When phases before the one just finished were completed and the effort has no `phase-code-review` line of any kind, ask the user once whether to review from the first implementation commit or to adopt the gate from the phase just finished. On adopt, append one `[decision]` line to `Execution Notes` naming this gate as adopted and the adoption commit, which is the commit that ended the previous phase as its recorded batch exit names it, or the commit the user names when none is recorded; the first review starts from that adoption commit. An adoption line that another gate recorded names a different commit and never sets a review base. Record the adoption line once per effort.

**Where output goes.** A Markdown effort keeps the `[validation]` and `[decision]` lines in `Execution Notes`. A workspace effort keeps the `[validation]` and `[decision]` lines in the `Execution Notes` section of its Markdown work log; the plan and its ticked tasks are in the linked plan document. Retrieval returns a workspace plan by path only, so a reader opens the linked plan document. Timestamps are UTC to the second, from `date -u +"%Y-%m-%dT%H:%M:%SZ"` when the line is written.

## Invariants

- Review every completed phase. A flat plan gets one review after implementation and
  before closure. There is no skip gate; an empty diff gets an explicit SKIPPED
  outcome rather than a silent pass.
- Use a fresh-context reviewer for every iteration. The reviewer writes nothing.
- The reviewer only returns its review. It never edits, reverts, commits, or mutates
  any file; the main conversation that spawned it applies the fixes.
- Read every changed file and the relevant callers/callees. Passing tests and
  compilation are not proof of correctness.
- No phase builds on unreviewed code. A review records the commit it covered, and
  the next review begins at that exact anchor. An outcome that left blockers
  standing covers nothing: the gate stays open for that phase until a later run
  ends with zero blockers or the user force-pasts.

## Review dimensions

The reviewer must cover all four and report only evidence-backed defects:

1. **Logic bugs** — incorrect conditions, state transitions, error paths, boundary
   behavior, null/empty handling, or operators.
2. **Security logic** — intent-dependent trust-boundary failures such as wrong
   principals, missing ownership/tenant filters, or data crossing an unauthorized
   boundary. Pattern-matched scanner findings are outside this review.
3. **Quality defects with a bug path** — swallowed errors, ambiguous state, or an API
   that predictably invites misuse. Style without a concrete defect path is not a
   finding.
4. **Test quality** — mocked-away boundaries, assertions that cannot fail, or tests
   coupled only to implementation details. Test existence/selection belongs to the
   repository's normal quality gates. The reviewer may also suggest, as INFO only,
   tests to retire, strengthen, or merge with others, and property-based or fuzz
   testing opportunities, but only when the repository already uses a property or
   fuzz testing framework. These suggestions never block.

Spec drift, architecture review, performance, formatting, comment style, and
pattern-matchable static-analysis findings retain their normal repository owners;
do not relitigate them here.

## Severity and finding contract

- **BLOCKER** — incorrect behavior, a security defect, or data-loss risk. Blocks the
  next phase or closure unless the user explicitly force-pasts it.
- **WARNING** — robustness or maintainability degradation. Advisory; it does not
  drive re-review.
- **INFO** — suggestion only.

Every finding names one severity, one dimension, `path:line`, the concrete defect,
and a fix suggestion. The reviewer returns either:

```text
PHASE CODE REVIEW CLEAN — <effort> <phase>, iteration <n>
Range reviewed: <base>..<head> (<count> files)
<four-row dimension summary>
```

or:

```text
PHASE CODE REVIEW — FINDINGS — <effort> <phase>, iteration <n>
Range reviewed: <base>..<head> (<count> files)
<counts and numbered findings, blockers first>
```

## Diff scoping

Take the base from the first of these that applies:

1. The `reviewed-up-to` SHA of the latest phase-review outcome that settled its
   range, when it is still an ancestor of `HEAD`. An outcome settles its range
   when it ended with zero blockers, was SKIPPED, or is followed by a
   `force-past` line. An outcome that left blockers standing does not move the
   base: the next review starts where that one started, so a held phase is
   reviewed again and never recorded SKIPPED.
2. For an adopted effort's first review, the adoption commit that this gate's own
   adoption line names, when it is still an ancestor of `HEAD`. When it is not,
   rule 5 applies.
3. For the first review of an effort that did not adopt this gate, the parent of
   the first implementation commit,
   derived from durable plan-approval timing and repository history. When that
   commit is the root commit, the base is the empty tree, `git hash-object -t tree /dev/null`.
4. When no implementation commit exists because the phase is uncommitted, `HEAD`:
   the working tree is the whole phase. In a repository with no commits at all,
   the whole working tree is the range and the outcome line records `no commits`
   in place of a SHA; the review after that starts from the empty tree, covers
   the earlier phase again, and says so in its outcome line.
5. Otherwise, as after a rebase or squash that removed the recorded SHA, the merge
   base with the default branch:
   `git merge-base HEAD "$(git symbolic-ref --short refs/remotes/origin/HEAD)"`.
   When that command fails, ask the user for the default branch name and use the
   merge base with it. Ask for a branch name first, and never invent a range. Say in the outcome line that the base is a merge base.

Then fix the end of the range:

- A merge base that equals `HEAD` while the phase has commits proves nothing, as
  when committed work sits on the default branch itself, whatever the working
  tree holds. Hold the phase, say why, and offer the user the two ways out:
  name the commit the phase started from, or force-past. Never record SKIPPED from a
  merge base that equals `HEAD`.
- The range ends at the working tree, not at `HEAD`, whenever the phase or a fix
  from this review is uncommitted: review `git diff <base>` plus the untracked
  files `git status --short -uall` lists, say so in the reviewer's brief, and write
  `reviewed-up-to <HEAD> plus uncommitted changes` in the outcome line. The
  empty-diff check, every re-review, and the quality-gate re-run use that same range.
- Untracked files that the phase did not create, such as install output that was
  never committed, are not part of the range. Leave them out of the changed-file
  list and say in the outcome line that they were left out.
- When an earlier outcome covered changes that are still uncommitted, the next
  review cannot tell them from the new phase: it covers the whole working tree
  again and says so in its outcome line.
- Re-review fixes from the same base through the end of the range; do not move the
  base to the prior iteration's endpoint.

## Procedure

1. Resolve the completed phase, effort, frozen scope, and approved plan. Confirm
   phase completion evidence is recorded.
2. Derive the diff range as above. If it is empty, working tree included, append
   the SKIPPED outcome and stop. Otherwise capture the normal Rhizome validation finding count as a
   pre-write baseline.
3. Spawn a fresh-context, read-only reviewer with repository root, artifact paths,
   exact range, and changed-file list. Require all four dimensions and the exact
   return contract. Brief the reviewer to run tests in the foreground and to split
   any run that could outlast the tool's command timeout into per-file commands: a
   single long command times out and the reviewer stalls with no result. Include
   the phase's ticked tasks, and require the reviewer to report any whose named
   artifact is absent from the reviewed range: a ticked box is a claim, not evidence.
   The reviewer never mutates code, by hand or with a tool: it flags tests that
   look unable to fail, and says so in its findings. Say this in the brief.
   Mutation testing means running the
   repository's own mutation tests, if it has any, once per effort under its own
   policy; if it has none, nothing replaces them and the outcome line omits
   mutations.
   If the reviewer stops before returning its contract, resume it by id rather
   than spawning a new one, and count the resumes in the outcome line.
   When the harness has no subagent, run the same review inline and put
   `degraded: no fresh-context reviewer` in the outcome line.
4. Present the complete return. Re-read every cited location and confirm the defect
   before acting; a suggestion is guidance, not a patch to apply blindly.
5. The main conversation — never the reviewer — acts on the findings: BLOCKERs
   must be fixed or forced past, and WARNINGs and INFO may be fixed at its
   discretion. Make one revertible change per finding it
   chooses to fix. Commit each change only where the repository's agent
   instructions or the commit policy in `docs/engineering/` allow agent commits;
   otherwise leave it uncommitted and say so in the outcome line. Run a light,
   targeted check for that finding. If the check fails or the change cascades beyond
   the finding, roll back only that finding and report it unresolved.
6. Record every logic-error fix as its own `[decision]` line in `Execution Notes`
   carrying the literal `requires-human-verification`, then continue: this gate is
   part of the approved work and does not wait for sign-off. Closure surfaces every
   such line for a human to check.
7. Re-review from the same anchor through the end of the range with a fresh
   reviewer. Stop when
   there are zero blockers. Maximum three iterations per run; stop early if the
   blocker count does not decrease AND the findings are the same ones — a new
   blocker introduced by the previous iteration's own fix is progress, not a stall. On exhaustion/stall, let the user choose a
   different fix, force-past, or hold the phase. User-routed re-entry starts a new
   three-iteration run.
8. Re-run the quality gates that `docs/engineering/quality-gates.md` gives for the
   paths the phase changed, on the final reviewed state, committed or not, before
   recording the outcome. The fixes from this review land after the phase's own
   quality-gate run, so otherwise nothing checks them.
9. Append exactly one UTC `[validation]` Execution Note for the run, including
   the phase it covers, verdict, iterations, blockers found/fixed, degraded-review notes, and
   `reviewed-up-to <final-head>`. When fixes were left uncommitted, say so after
   the SHA.
10. A user force-past adds a separate `[decision]` line using the literal
   `force-past` term. Closure must surface every such line.
11. Re-inspect the effort note and compare Rhizome validation with the baseline;
    only new findings belong to the review.

Example outcomes:

```markdown
- 2026-06-02T15:30:00Z [validation] phase-code-review: phase 2 PASS after 2 iterations; 2 blockers found and fixed (missing owner filter, inverted retry guard), 1 warning outstanding; reviewed-up-to c47ab19.
- 2026-06-02T15:32:00Z [validation] phase-code-review: SKIPPED — empty diff for phase 3 (range c47ab19..HEAD contains no changes); reviewed-up-to c47ab19.
- 2026-06-02T15:41:00Z [decision] phase-code-review: force-past — user accepted 1 remaining BLOCKER (owner-filter fix deferred); phase entered with the blocker standing.
```

Use the repository's quality gates for formatting and static checks,
`foundation-review` for formative architecture, and `agentic-engineering align`
for frozen-spec drift. A pull request review covers the whole branch for its own
reviewers; it does not replace this gate, and this gate does not replace it. This
skill owns intent-aware review of the implemented phase diff.
