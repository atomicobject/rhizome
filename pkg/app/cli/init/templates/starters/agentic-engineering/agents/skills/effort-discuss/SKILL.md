---
name: effort-discuss
description: "Use after an effort's research decision is recorded and before planning, when `docs/engineering/review-and-approval.md` lists this gate or the user asks to discuss open choices: the user decides Discuss or Skip, and a Discuss runs a resumable pass that settles user-owned decisions inside frozen scope. Also resumes an active pass, or reopens one between implementation phases. Not for scope that is still changing (`agentic-engineering specify`) or a plan-requested review of formative decisions (`foundation-review`)."
argument-hint: "[effort path]"
user-invocable: true
---

# Effort Discuss

Run the user-controlled discussion gate on one `planned` or `active` effort. The
pass resolves consequential choices inside the effort's frozen scope before
planning, or at an intentional boundary between implementation phases.
Repository instructions and the frozen effort and specs remain authoritative for
project-specific scope, commands, and note structure.

## When this gate applies

This gate applies when `docs/engineering/review-and-approval.md` lists `effort-discuss`, or when the user asks for it. When the policy does not list it and the user has not asked, do nothing and say nothing about it. A listed gate is part of the approved work, not a new approval of the plan.

**Adoption.** An effort frozen before the gate was listed does not go back through it. The adoption commit is the newest commit that put the gate's name into a policy document that did not have it. List the commits that changed how often the name appears, newest first, with `git log -S'effort-discuss' --format=%H -- docs/engineering/review-and-approval.md`, and take the first one `<c>` for which `git grep -q 'effort-discuss' <c>^ -- docs/engineering/review-and-approval.md` fails; a later commit that only adds or removes a second mention is not it. A gate listed only in uncommitted changes has no adoption commit. The effort records no separate freeze time, so compare its `created-at` with that commit's author date, from `git log -1 --format=%aI <commit>`, as instants: `created-at` is UTC and the commit date carries its own offset. When `created-at` is earlier, append one `[decision]` line to `Execution Notes` naming this gate and the adoption commit; discussion does not apply to that effort, and planning continues. The user may still ask for a pass. When the command finds no commit, the policy document is not committed yet, nothing was listed before the effort, and the effort does not adopt. An effort created after the adoption commit runs the gate.

**Where output goes.** A Markdown effort keeps `### Decision Context` under its `Plan` section and the `[decision]` and `[validation]` lines in `Execution Notes`. A workspace effort keeps `Decision Context` in its linked plan document, and the `[decision]` and `[validation]` lines in the `Execution Notes` section of its Markdown work log. Retrieval returns a workspace plan by path only, so a reader opens the linked plan document. Timestamps are UTC to the second, from `date -u +"%Y-%m-%dT%H:%M:%SZ"` when the line is written.

## Invariants

- The agent owns fact-finding and recommendations; the user owns decisions.
  Never advance an unresolved decision on the user's behalf.
- The user owns the Discuss/Skip choice. Silence is never Skip. Record the
  choice as a timestamped `[decision]` line for the current frozen scope.
- Target exactly one effort whose status is `planned` or `active`. A `complete`
  or `archived` effort is closed history: reject the invocation without writing
  to that effort; follow-on questions need a fresh effort.
- Discussion never reshapes frozen scope. If a pass reveals the frozen scope is
  invalid or materially incomplete, stop the pass and route to
  `agentic-engineering specify` and a refreeze; never repair scope from inside
  discussion.
- No pass is complete until the user confirms shared understanding. Question
  exhaustion, a fixed question count, and conversational silence are never
  completion.
- After recording a Skip or a completed pass, continue with planning when the gate
  was entered from the planning entry check or the user asked for a plan;
  otherwise stop and name the recommended next step. Never write implementation
  code or approve a plan from inside a pass.

## Where this gate sits

- After the effort's research Run-or-Skip `[decision]` is recorded for the
  current frozen scope — and after the approved research output lands when Run
  was chosen — and before planning. When the policy does not list
  `effort-research`, the assessment follows scope freeze directly.
- An explicit first invocation of this skill counts as choosing Discuss; do not
  ask a redundant Discuss/Skip confirmation.
- If the effort has an active pass, explicit invocation resumes it. If the
  latest pass is completed, confirm the user wants to reopen before creating
  another pass, and record the reopen choice and its reason as a `[decision]`;
  prior completion evidence remains unchanged.
- Reopening between implementation phases is legitimate. The new pass names the
  phase boundary or new evidence that motivated it and preserves earlier settled
  decisions unless the user explicitly reopens an affected branch.

## The Discussion assessment

Present exactly these three fields, and always name a recommendation:

```markdown
Discussion assessment for <effort> (the agent recommends; you own the decision):

- `Discussion candidates`: <material choices still open, with why each matters>
- `Skip rationale`: <what is already settled or none>
- `Recommendation`: Discuss | Skip
```

Recommend `Discuss` when frozen scope still contains material user-owned
choices, conflicting evidence, behavioral alternatives, or decisions with
meaningful downstream branches. Recommend `Skip` when relevant choices are
already locked, only factual unknowns remain, or a completed pass already covers
the unchanged scope. Append the user's choice to `Execution Notes` as a
timestamped `[decision]` for the current frozen scope. A Skip is complete after
that write.

## Evidence rules

- Before constructing questions, load the effort's frozen specs, frozen stories
  and criteria, existing decision context, execution notes, relevant code
  context, linked reference notes, and available transcript-derived evidence.
- Distinguish facts from decisions. Inspect available local evidence instead of
  asking the user for discoverable facts; choices, priorities, acceptable
  tradeoffs, and scope-sensitive judgments remain with the user.
- Questions, executive summaries, and recommendations name the evidence that
  materially shaped them. Surface conflicting sources; never silently reconcile
  them.
- Transcript evidence is represented as "a person stated or decided X" — never
  promoted to an independently verified external fact without a separate
  verification act.
- Focused research may run between question rounds when a missing fact is a
  prerequisite for a frontier decision. Research that falls under the
  repository's research gate requires a current user-approved Research Run
  decision for the frozen scope and uses that gate's provenance tags and
  output homes. A recorded Research Skip remains in force until the user
  explicitly changes it.
- When a frontier decision's factual prerequisite would require gated research
  while a Research Skip stands, neither silently research nor silently guess.
  Present a three-option user decision: reverse the Skip and run the focused
  research, decide now without the fact (recorded against the decision as an
  `[ASSUMED]` basis), or defer the branch. Record the choice like any other
  settled decision.
- A pending factual investigation blocks only its dependent branches. Continue
  with unrelated frontier questions when useful and supported by the harness;
  background sub-agents are optional, never required.

## Decision tree and frontier

- Map the selected discussion areas as a decision tree in which each decision
  records its prerequisites and the downstream choices it can unlock.
- The current frontier is the set of unresolved decisions whose prerequisites
  are settled. Defer any question whose answer depends on another unresolved
  question to a later frontier.
- Each round contains only frontier decisions. Harness capacity may split one
  frontier across multiple calls, but never pull a dependent question forward
  merely to fill a call.
- After every user answer, clarification, deferral, or research result, update
  the affected branch and recompute the frontier. Never replay a settled
  question unless new evidence or an explicit reopen decision invalidates it.
- Every substantive question provides mutually intelligible options, marks the
  recommended answer, and explains the recommendation briefly. The user may pick
  a different option or answer free-form without being challenged merely for
  disagreeing.
- Question text, executive summaries, and recommendations are self-contained:
  never name spec or effort identifiers; carry the necessary context inline so
  the user never breaks focus to look up a reference. Identifiers stay in the
  durable record — Decision Context and execution notes — for traceability.

## Asking questions

Use the harness's native structured question tool when it is available, the
decision fits its display constraints, and the per-harness posture below does
not name the chat format as the primary path; otherwise use the standard chat
question format, which preserves identical decision semantics. On a chat-first
harness the native tool is never the default, even in the modes where it works.

Per-harness posture:

| Harness | Posture | Real options per question |
|---------|---------|---------------------------|
| Claude Code (`AskUserQuestion`) | Native tool by default | up to 3, plus `Help me understand` (tool caps options at 4; the harness adds its own free-form `Other`) |
| Codex (`request_user_input`) | Standard chat format first — the native tool is option-capped, single-select, and unavailable outside plan-style modes | up to 2, plus `Help me understand`, when the native tool is used |
| No structured tool | Standard chat format | unlimited numbered options |

- `Help me understand` is always the final skill-authored option on the first
  presentation of every substantive decision — in the native tool and in chat
  alike — and `Explain better` takes that position on a re-presentation.
- Never author an `Other` option; harnesses that have one add it themselves, and
  in chat the free-form invitation replaces it.
- When a decision's real alternatives exceed the native tool's capacity after
  the `Help me understand` slot, present that decision in the standard chat
  format with the full alternative set — never collapse, split, or omit
  options. The native tool remains the default for decisions that fit it, on
  harnesses where it is the primary path.
- Area selection should use the native tool's multi-select when available;
  decision questions are single-select unless genuinely non-exclusive.

### Standard chat question format

```markdown
**<Header> — <the question>**

1. **<Option>** (Recommended) — <consequence in one or two lines>
2. **<Option>** — <consequence>
3. **Help me understand** — executive summary first, then the question again in
   simpler terms

*Reply with a number — or answer free-form, which is always accepted.*
```

The recommended option is marked inline exactly as the native tool renders it.
Every substantive chat question includes `Help me understand` as its last
numbered option on first presentation, and `Explain better` in that position on
a re-presentation. Multi-select chat questions ask for comma-separated numbers.

### Clarification escape hatches

- `Help me understand` leaves the decision unresolved and preserves the round's
  other answers. Present a concise executive summary — what is being decided,
  why it matters now, what the evidence says, practical tradeoffs, the
  recommendation and its reasoning, and the downstream choices — then ask the
  same underlying decision again in simpler language, with `Explain better` as
  the final option.
- `Explain better` leaves the decision unresolved, suspends structured question
  mode, and continues in ordinary chat. The transition includes this
  invitation: “Let’s chat about what might seem unclear, and I can continue
  asking questions when you’re ready.”
- During clarification chat, answer questions and inspect or research authorized
  facts, but never advance the unresolved branch for the user. When the user
  says they are ready, or explicitly invokes this skill while the pass is
  active, recompute the frontier and resume structured questioning.

## Pacing checkpoints

- Discussion must not outlast the user's appetite. After every five substantive
  decision questions since the pass began or since the most recent checkpoint,
  pause and present a continuation checkpoint before asking more: `Continue` or
  `Wrap up`, always with a recommendation one way or the other, grounded in the
  value of the remaining frontier.
- `Help me understand` and `Explain better` re-presentations, clarification
  chat, and the checkpoint itself do not count toward the five-question cadence.
- `Wrap up` converts every remaining unresolved branch into an intentional
  deferral and completes the pass; the choice itself is both the explicit
  deferral and the user's completion confirmation. Deferred branches stay
  visible in Decision Context for a later pass to reopen.
- An explicit user request to stop at any point triggers the same wrap-up path
  immediately, without waiting for the next checkpoint.
- Checkpoint choices are recorded in the pass's Decision Context; only
  completion appends an `Execution Notes` entry.

## Durable state and completion

Keep each pass's durable state in `### Decision Context`, in the home named
under "Where output goes", as compact synthesis, never a transcript. Raw source
material stays in its provenance-bearing note and is linked from here:

```markdown
### Decision Context

#### Discussion pass <n> — active | completed

- Scope: <pre-plan choices or the phase boundary this pass serves>
- Areas: <the discussion areas selected for this pass>
- Settled: <decisions settled so far>
- Deferred: <intentional deferrals or none>
- Unresolved: <open branches and the current frontier, while active>
- Clarification: <active decision under clarification chat, when applicable>
- Evidence: <links and named sources that shaped the pass>
- Remaining factual unknowns: <what stays unknown>
```

- The pass ordinal is stable. A completed pass is immutable: later passes append
  as a new ordinal and may supersede a prior decision only by naming it
  explicitly.
- Discussion is complete only when the frontier is empty or every remaining
  branch is explicitly deferred, and the user confirms shared understanding (a
  `Wrap up` choice is that confirmation).
- Completion updates the pass state to `completed` and appends one timestamped
  `[validation]` entry to `Execution Notes` naming the pass, its scope or phase,
  settled decisions, intentional deferrals, and remaining factual unknowns.
- An active pass must be completed or explicitly abandoned by user decision
  before effort closure. Abandonment preserves the unresolved branches and the
  reason in `Execution Notes`; it is not a successful completion.
- When resuming after a conversation boundary, show a compact recap of settled
  decisions and the current frontier before the next round.
- Capture the repository's normal validation finding count before the pass's
  first Decision Context write. After recording completion or abandonment,
  re-inspect the effort note with the same validation scope against that
  baseline; only findings this pass's writes introduced belong to it.

## Style

- Keep rounds small enough that recommendations and answers stay easy to map,
  while still asking independent frontier questions together when the tool
  supports it.
- Prefer plain language and concrete consequences over architecture jargon.
- Treat diminishing decision value as a reason to recommend `Wrap up` at a
  checkpoint rather than continuing an unbounded interview.
- Recommend reopening a completed pass only when new evidence, a phase boundary,
  or an observed implementation result invalidates a prior decision.

## Neighboring skill boundaries

- Use `agentic-engineering specify` while desired behavior is still moving;
  discussion assumes frozen scope.
- Use `effort-research` for provenance-bearing fact gathering; discussion
  consumes its output and never replaces it.
- Use `foundation-review` when an approved plan asks for a review of formative
  schema, API, or ownership decisions before later phases build on them.
- Use `agentic-engineering plan` after the pass completes; discussion never
  drafts phases, task breakdowns, or an approval-ready plan.
