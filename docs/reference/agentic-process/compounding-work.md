---
source: agentic-process/www/compounding-work.html
summary: "Explains how teams turn delivery friction from definition and implementation into durable improvements that increase future delivery capacity."
reference-kind: guide
draft: v0.2
last-updated: 2026-05-06
---

# Compounding Work

> *Part of Atomic's Agentic Delivery Process - how teams turn delivery friction from definition and implementation into future delivery capacity.*

*Draft v0.2 — Last updated 2026-05-06. Expect continued refinement as teams put it into practice.*

> **Scope of this guide:** The [Implementation Track](implementation-track.md) explains how the team plans, delegates, validates, and reviews short cycles of work. The [Definition Track](definition-track.md) explains how direction, decisions, designs, and specs stay ready enough to feed those cycles. This guide explains what the team does with the friction both tracks reveal.

> **Companion guide boundary:** Compounding Work decides whether and why to improve the delivery system. Use the [Agent Use Playbook](strategic-agent-use.md) to name repeated collaboration modes; use the [Agent Harness Playbook](agent-harness-playbook.md#the-harness-surface-model) to choose where agent-operable improvements should live.

## Why You Are Here Now

By this point in the process, the team has two tracks running. Definition keeps direction, decisions, designs, knowledge, and *specifications* (an evergreen description of how a part of the system or experience works; kept current with actual behavior — when the team wants a change, the spec is updated first and implementation brings the system into alignment) current. Implementation plans, delegates, validates, and reviews short *cycles* (one pass through the planning → implementation → review loop, running twice per week) of work. Both tracks expose friction.

Implementation exposes weak checks, repeated agent mistakes, architecture drift, missing evidence, review bottlenecks, and places where a human has to keep intervening. Definition exposes stale context, ambiguous specs, design handoff gaps, client decision drag, and places where planning turns back into discovery.

**Compounding Work** *(delivery-capacity work that makes future cycles better: stronger specs, better examples, agent skills, runtime evidence paths, validation, patterns, tooling, and review workflows; first-class delivery work, not cleanup)* **is the loop that turns those signals into future delivery capacity.** Without it, the same friction returns every cycle. With it, each cycle leaves the delivery system slightly easier to use, safer to delegate into, clearer about where human input is required, and easier to review.

> Compounding work is delivery work whose primary output is improved future delivery capacity.

A cleanup task may be valuable. A process idea may be reasonable. But a compounding task has to change how the next similar piece of work behaves.

## The Basic Move

When a team is new to agentic delivery, the natural response to poor agent output is to write a better prompt. Sometimes that is enough. But if the same problem appears again, the team should stop treating it as a prompting problem and start treating it as a system problem.

> If someone has to explain the same thing twice, it probably belongs in the system.

**Diagram — Compounding work improvement loop** *(runs inside normal delivery)*: Friction leads to diagnosis, durable improvement, retry or continue, distillation, and a better next delegation.

- **Notice friction**
- **Diagnose missing support**
- **Add durable improvement**
- **Retry or continue**
- **Distill learning**
- Edges: Notice → Diagnose → Add durable improvement → Retry or continue → Distill learning, then loops back to Notice friction.
- Outcome at base of diagram: **Better next delegation** — one failure becomes a shared asset instead of a private prompt correction.

The exact form matters less than the direction: move stable knowledge out of private conversation and into shared practice.

Before creating the improvement, ask what it unlocks. Can an agent validate more of its own work? Can it surface assumptions instead of making unilateral decisions? Can a designer compare implementation against intent faster? Can a delivery lead see risk without manual archaeology? Can the team safely delegate something it previously had to babysit? If the answer is unclear, the work may still be useful cleanup, but it is not yet a strong compounding candidate.

## A Compounding Task Has Five Parts

A frustration is not a compounding task until the team can name the future failure it prevents. Good compounding work is specific enough to plan and durable enough to help someone who was not in the original conversation.

### Trigger
What repeated failure, manual workaround, review feedback, design mismatch, status gap, or runtime-evidence gap prompted this?

### Missing support
What could the team or agent not see, run, validate, infer, access, compare, explain, or safely decide?

### Durable home
Where will future work naturally find the improvement?

### Done check
What proves the improvement was implemented, validated, and made discoverable from the normal workflow?

### Signal
What ordinary delivery signal will show whether the friction is declining, staying flat, or getting worse?

> We saw ___; future work needs ___; we will put it in ___; it is done when ___; we will keep watching ___.

The task is not done when someone has an idea. It is done when the improvement is in its durable home, works for its intended use, and the team knows what signal to keep watching.

## When to Invest

Do not collect every annoyance. The default bias is to finish the local work unless the friction is likely to recur, blocks validation, reduces trust, hides risk, causes agents to decide where they should ask, or prevents safe delegation. Strong signals are repeated, observable, tied to delivery risk or trust, and specific enough to become work.

> **Local fix or system fix?** Fix the local issue when the friction is one-off, low-risk, and unlikely to recur. Improve the system when the same correction repeats, validation is missing, review trust drops, status requires archaeology, design intent is hard to inspect, assumptions need human judgment, or future agents will otherwise need the same private explanation.

| Situation | Default move |
| --- | --- |
| One-off friction, low recurrence | Finish the local work. Log a follow-up only if the lesson is likely to matter later. |
| Same correction appears twice | Create a small compounding task before the third repetition becomes normal. |
| Agent cannot validate or prove its own work | Stop and improve the validation path, evidence expectation, runtime signal, or scope boundary. |
| Agent makes assumptions that needed human input | Improve decision capture, ambiguity flags, escalation rules, or the agent collaboration mode before asking for more implementation. |
| Developer repeatedly babysits the same step | Add a script, workflow, skill, sharper task boundary, or more compact tool output. |
| Review feedback repeats | Use automation for objective rules; use docs, examples, skills, or rubrics for judgment-based conventions. |
| Bad trajectory is cheaper to restart than repair | Improve the context, preserve useful findings, discard the run, and retry cleanly. |
| Planning or definition keeps arriving ambiguous | Improve spec readiness, synthesis workflow, decision capture, or definition-track intake before forcing work into implementation. |
| Design intent is repeatedly reconstructed from memory | Add Figma references, screenshots, state inventories, accessibility notes, and design-to-spec guidance. |
| Status or runtime behavior requires manual archaeology | Add workboard, PR evidence, blocker, smoke-path, log-query, metric, or runbook synthesis. |

## Reserve Capacity

Compounding work must be planned as real delivery work. Reserving capacity means shipping less feature surface in that cycle. That is the point. The bet is that the next cycles become safer, easier to review, easier to delegate, and easier to steer with agents because the team invested instead of repeatedly paying the same tax.

A team should usually reserve somewhere between **10% and 50%** of a cycle or short phase for compounding work. Treat that as a dial, not a quota. Capacity should follow the signal.

| Allocation | Mode | When to use |
| --- | --- | --- |
| **10%** | Healthy flow | Use when delivery is moving and the team mostly needs small leverage tasks: one command, one example, one template improvement, one regression check, one review rule, one stale item removed. |
| **25%** | Visible recurring friction | Use when the team is shipping but the same friction is visible across review, definition, design, delivery visibility, validation, or runtime evidence. This is a common steady-state allocation. |
| **50%** | Foundation or trust repair | Use temporarily early in a project, when entering a new product or architecture phase, before production release, or when review trust has dropped. Build foundations before asking agents to move faster. |

> **Bias toward small.** Many compounding tasks should be Simple tasks. Pack them into the next cycle instead of waiting for a hardening sprint or platform initiative.

## Where Compounding Enters the Existing Cadence

Compounding work is not a new ceremony. It changes how the team uses the existing cadence: reserve capacity during planning, treat blockers as signals during implementation, turn repeated review feedback into system learning, and carry the distilled task into the next cycle.

| Moment | Compounding move | Output |
| --- | --- | --- |
| Planning | Reserve explicit capacity and classify compounding candidates by signal strength. | Visible tasks on the workboard, sized small when possible, with owner and signal named. |
| Definition shaping | Notice unresolved decisions, stale KB, vague acceptance criteria, weak design handoff, and repeated clarification loops. | Spec, KB, decision-tracker, synthesis, design-to-spec, or retrieval improvements feed the next implementation slice. |
| Implementation blockers | When an agent stalls, ask what support was missing before reprompting. | A script, doc, test, example, skill, access path, or follow-up task is created when the blocker will recur. |
| Review | Treat repeated feedback as information about the system that produced the work. | The local issue is fixed; durable guidance or validation is added when the feedback is objective or repeated. |
| Runtime evidence | Turn manual reproduction, log spelunking, metric checks, and release evidence collection into agent-operable paths. | Developers, designers, delivery leads, and reviewers can inspect behavior without waiting on one engineer for every question. |
| Post-review distillation | Look across session logs, CI failures, PR feedback, design review, delivery notes, runtime findings, and human interventions for patterns. | The next cycle starts with a small queue of compounding tasks, not a vague memory that things were painful. |

## From Friction to Workboard

The failure signals in the [Implementation Track](implementation-track.md#knowing-when-its-breaking) and [Definition Track](definition-track.md#knowing-when-its-breaking) are not only diagnostic. If a repeated problem does not become visible work, it stays a private memory and the next cycle pays the same cost again. Capture alone is not enough: the task still needs a durable home, a done check, and a signal.

1. **Notice friction** — Review feedback, design mismatch, delivery status archaeology, failed run, unclear spec, noisy tool, manual log pull.
2. **Name the support** — Ask what was missing: context, validation, evidence, design signal, access, architecture, safety, or workflow.
3. **Create work** — Capture a specific compounding task on the workboard, sized small when possible.
4. **Make it stick** — Put the improvement in its durable home, validate it, and watch whether the friction persists.

A compounding task is done when the improvement has a home, works for its intended use, and has a signal the team will keep noticing during ordinary delivery. Do not create a separate audit ritual by default. Watch review feedback, blocked work, repeated questions, failed runs, weak evidence, and manual workarounds.

Deletion is part of the loop. A stale skill, ignored checklist, noisy script, obsolete template, or outdated instruction is negative compounding. When that shows up, treat it as new friction and prune or replace it.

Some improvements should stay project-local. Others should become shared AO assets: starter templates, reusable skills, delivery-lead prompts, design-to-spec patterns, review-agent instructions, or safety guidance. Route the work based on where future teams will benefit, and prune local copies when shared guidance replaces them.

## What Counts Across the Team

Compounding work is not only developer tooling. It shows up anywhere the team repeatedly pays the same coordination, interpretation, validation, evidence, or decision cost.

| Surface | Repeated cost | Durable improvement |
| --- | --- | --- |
| Definition | Same decisions re-explained; specs enter planning with gaps; synthesis does not change the KB. | Decision tracker, spec template, transcript-to-decision workflow, spec-health review, KB retrieval guidance. |
| Design | Designer reconstructs intent manually; built UI cannot be compared quickly to intended states. | Figma references, screenshot evidence, state inventory, accessibility notes, interaction rules, design-to-spec template. |
| Delivery leadership | Status, blockers, risk, and follow-ups require manual archaeology. | Workboard, Effort, PR, evidence, and risk synthesis; cycle-review prompts; visible compounding lane. |
| Implementation | Agents repeat mistakes; setup is fragile; validation is slow or unclear. | Focused commands, architecture examples, regression tests, scripts, skills, repo guidance, compact failure summaries. |
| Review | Feedback expands scope, repeats, or lacks severity discipline. | Severity model, review-agent rules, author pushback policy, reusable rubrics, follow-up routing. |
| Runtime evidence | Bugs are hard to reproduce; logs and metrics require one expert; release proof is ad hoc. | Smoke paths, log queries, metric links, runbooks, test accounts, release-readiness evidence, escalation boundaries. |
| Team workflow | The same coordination tax repeats across roles. | Workboard task shape, compounding-capacity planning, done checks, pruning habit, shared AO routing. |

If implementation keeps waiting on direction, visual intent, or project-health archaeology, the highest-leverage compounding work is often upstream or cross-role.

## Simple Tasks Matter

The most valuable compounding work is often small. It removes one repeated snag, one vague handoff, one manual evidence step, or one noisy output stream. It can also remove one stale rule or unused workflow that is now slowing the team down.

- Add one focused validation command.
- Write one architecture example for the pattern agents keep missing.
- Tighten one spec template section that repeatedly causes questions.
- Add a compact failure summary to one noisy script.
- Add one delivery-status summary prompt or workboard view.
- Add one screenshot expectation for a UI work type.
- Delete one stale rule, skill, checklist, or script that people no longer trust.

## One Common Home: The Delivery Harness

Many compounding improvements land in the *delivery harness* (the operating environment that lets agents do useful work safely and repeatedly: repo context, specs, scripts, validation, observability, review agents, workflows, and safety boundaries): the context, scripts, validation, observability, review agents, examples, skills, and safety boundaries that make agent work easier to delegate, easier to partner with, and easier to review.

*Harness engineering* (the practice of making the delivery harness more agent-operable by improving context, tools, feedback loops, validation, structure, permissions, and boundaries) is ordinary delivery work whenever the missing support is technical: the test that should have caught a bug, the script that summarizes noisy output, the architecture example agents keep needing, the PR evidence requirement reviewers rely on, or the safety gate that makes autonomy acceptable.

But not every compounding task is harness engineering. Some belong in definition workflow, design handoff, delivery visibility, staffing, client decision cadence, or product evaluation. Once the team decides the missing support belongs in agent-operable instructions, skills, custom agents, scripts, repo knowledge, or gates, use the [Agent Harness Playbook](agent-harness-playbook.md#the-harness-surface-model) to choose the smallest durable surface.

**Diagram — Definition and implementation tracks sit on the delivery harness**: The Definition Track and Implementation Track sit above a Delivery Harness made of context, scripts, validation, observability, review agents, architecture conventions, skills, and safety gates.

- **Definition Track** (top-left): information, synthesis, specs, decisions kept explicit enough to build from.
- **Implementation Track** (top-right): planning, delegation, validation, review inside short delivery cycles.
- Both tracks sit on the **Delivery Harness** (bottom bar): "the operating environment that lets agents do useful work safely and repeatedly."
- Harness components (foundation chips): Context, Scripts, Validation, Observability, Review Agents, Architecture, Skills, Safety.
- Edges: dashed arrows from each track down into the harness.

## When a Repeated Move Becomes a Skill

Agent skills deserve attention because they sit between private prompting and full automation. A good skill turns repeated judgment, sequencing, evidence expectations, escalation points, and team conventions into reusable operating guidance that agents can invoke during real work.

Use a skill when the team keeps teaching the same workflow: how to validate a UI change, distill a client meeting into spec deltas, review a PR against project rules, prepare release evidence, or recover from a bad agent run. Do not use a skill for facts that belong in a spec, checks that belong in tests or scripts, or background material that should live in the KB.

For skill authoring mechanics, see [Skills in the Agent Harness Playbook](agent-harness-playbook.md#skills).

> **Use the practitioner guides deliberately:** Use the [Agent Use Playbook](strategic-agent-use.md) to expand the ways agents can help with research, critique, validation, documentation, teaching, review, and human-guided decision work. Use the [Agent Harness Playbook](agent-harness-playbook.md#the-harness-surface-model) when recurring friction points to repo, tooling, instruction, safety, or agent-capability gaps.

## Review as Learning

Review feedback is local when it only fixes the current change. It is compounding when it teaches the system how to avoid, detect, or explain the same issue next time. When the feedback does not need to block merge, capture it as follow-up work rather than letting it expand the PR indefinitely.

| Review feedback type | Compounding response |
| --- | --- |
| Missing acceptance behavior | Update the spec and add a test or acceptance check when practical. |
| Repeated convention or architecture issue | Add an example, review rubric, lint, skill, architecture note, primitive, or reviewer check. |
| Missing evidence | Improve the evidence template, validation command, screenshot path, smoke path, log query, or metric link. |
| Ambiguous product or design decision | Route to Definition Track decision capture and update the workboard. |
| Scope-expanding suggestion | File follow-up instead of derailing the PR. |
| Security or data concern | Add a safety rule, approval gate, regression test, or human review requirement. |

Review agents and authoring agents both need severity discipline. Otherwise review becomes a scope-expansion machine. Authoring agents should be allowed to accept and fix, reject with rationale, defer to follow-up, ask for clarification, convert feedback to a compounding task, or escalate to human decision. For review-agent configuration patterns and the severity model, see [Custom Agents and Review Agents](agent-harness-playbook.md#custom-agents-and-review-agents).

## Practical Examples

The examples below all use the same shape: fix the local issue, then change the system so the next similar task starts from a better place.

### Repeated timeout bug
**Weak response:** Fix this timeout.
**Compounding response:** Fix the timeout, add a regression check if practical, document that outbound calls require timeouts, update the relevant review rule, and consider a shared HTTP client with timeout defaults.

### Noisy test output
**Weak response:** Read the logs more carefully.
**Compounding response:** Add a focused test or failure-summary script, suppress passing output, print the failing file and assertion first, and document the command where agents will find it.

### Wrong architecture pattern
**Weak response:** Move this into the use-case layer.
**Compounding response:** Fix the current implementation, add an architecture note with a good example, update the relevant skill or reviewer, and consider a generator for new use cases.

### Definition ambiguity repeats
**Weak response:** Ask the client better questions next time.
**Compounding response:** Add a decision tracker, improve the spec template for unknowns and non-goals, and create a synthesis step that turns meeting notes into decision deltas.

### Design review is too manual
**Weak response:** Designer pulls the branch and compares screens by memory.
**Compounding response:** Give agents access to the relevant design reference, require implementation screenshots for UI changes, and add a design-to-spec template that captures states, variants, behavior, and accessibility expectations.

### Approaching production release
**Weak response:** Manually check logs, deploy carefully, and hope the team notices problems quickly.
**Compounding response:** Invest in release evidence: smoke checks, rollback path, production log and metric access, alert/runbook links, and a lightweight release-readiness review.

## Minimum Viable Compounding Loop

A project does not need a sophisticated agent platform to begin. It needs enough structure to capture friction, prioritize improvements, and make future work easier.

1. The workboard has a visible lane, label, or task type for compounding tasks.
2. Each task names trigger, missing support, durable home, done check, and signal.
3. Planning reserves explicit capacity for compounding work based on current signal strength.
4. Definition, design, delivery, implementation, review, and runtime-evidence friction are all eligible signals.
5. Completed improvements are validated for their intended use and discoverable from the workflow that needs them.
6. The team watches ordinary delivery signals instead of creating a heavy audit ceremony by default.
7. Stale guidance, unused scripts, noisy checks, and heavy rituals are pruned before they erode trust.

For the minimum technical harness — entry instructions, scripts, skills, review agents, repo knowledge, and safety gates — use the [Minimum Viable Harness](agent-harness-playbook.md#minimum-viable-harness) section of the Agent Harness Playbook.

## What Better Looks Like Over Time

Teams do not jump straight from prompting to bounded autonomy. Compounding maturity increases as repeated friction moves from private memory into documented context, repeatable validation, agent-operable workflows, and eventually a harness that improves when failures occur.

| Level | Stage | Unlocks | Risk |
| --- | --- | --- | --- |
| Level 0 | Prompt-dependent | Individual prompting | Success depends on supervision |
| Level 1 | Documented context | Agents can find rules | Docs may not validate behavior |
| Level 2 | Repeatable validation | Agents can check work | Workflow still depends on people |
| Level 3 | Agent-operable workflow | Plan, build, validate, evidence | Failures may not improve the system |
| Level 4 | Self-improving harness | Failures produce harness work | Autonomy can outrun gates |
| Level 5 | Bounded autonomy | Safe retry and low-risk landing | Boundaries must stay explicit |

The goal is not to reach the highest level everywhere. Some project areas should remain human-gated. Maturity means the team can choose the right level of delegation for each risk surface because the context, validation, evidence, and approval boundaries are explicit.

## Anti-Patterns

The goal is not more ceremony. It is better leverage. These patterns add process, prompts, or tooling without increasing trust, autonomy, evidence, or shared learning.

### Prompt patching
**Symptom:** The developer writes longer prompts to compensate for missing shared context.
**Correction:** Move stable guidance into specs, docs, skills, examples, tests, scripts, or workboard tasks.

### Permanent babysitting
**Symptom:** The developer watches the agent terminal and intervenes constantly on the same step.
**Correction:** Add validation, clearer checkpoints, better tool output, or smaller task boundaries.

### Autonomy without evidence
**Symptom:** Agents produce PRs faster, but review trust decreases.
**Correction:** Require evidence bundles and strengthen validation before reducing review.

### Compounding work as leftovers
**Symptom:** The team agrees improvements matter but never reserves capacity for them.
**Correction:** Plan compounding work explicitly and pack Simple improvements into every cycle.

### Capability wishlist
**Symptom:** The team invents platform work without a repeated delivery signal.
**Correction:** Require a trigger, future beneficiary, durable home, done check, and signal before investing.

### Negative compounding
**Symptom:** Rules, templates, skills, and gates accumulate until the process becomes harder to use.
**Correction:** Prune stale assets and ask what autonomy, evidence, or trust each improvement still unlocks.

## Appendix — Templates and operating notes

### Compounding Work Note

```
# Compounding Work

## Trigger
What failure, repeated feedback, manual workaround, design friction, delivery visibility gap, runtime evidence friction, or definition gap prompted this?

## Missing support
What could the team or agent not see, run, verify, compare, infer, access, explain, or safely decide?

## Smallest durable fix
What doc, script, test, lint, skill, template, primitive, access path, evidence path, or workflow change should we make?

## Durable home
Where will future work naturally find this?

## Done check
What proves this was implemented, validated, and made discoverable from the normal workflow?

## Scope
- Project-local
- Shared AO candidate
- Already covered by shared guidance

## Autonomy unlocked
What can someone safely delegate, inspect, validate, compare, explain, or decide after this improvement?

## Signal to keep watching
What ordinary delivery signal will show whether the friction persists?

## Follow-up
What remains unresolved?
```

### Workboard Task Shape

```
# Compounding Task

## Problem pattern
What repeated friction does this prevent?

## Future beneficiary
Who benefits: delivery lead, designer, definition lead, developer, verifier, reviewer, agent, client?

## Proposed change
Smallest durable improvement.

## Durable home
Where should the change live so future work finds it?

## Local or shared
Should this stay project-local, or become AO-wide guidance, template, skill, or tooling?

## Autonomy unlocked
What future delegation, review, design, delivery, or runtime-evidence work becomes easier?

## Done when
The improvement is in its durable home, works for its intended use, is discoverable from the normal workflow, and the signal to watch is named.

## Links
- Triggering review / design note / delivery note / runtime evidence note / agent run:
- Related Effort:
- Related spec or KB entry:
```

### Review Feedback to Compounding Task

```
# Review Learning

## Local issue
What needs to be fixed in this change?

## Pattern
Has this appeared before? Where?

## Severity
- P0 / P1 / P2 / P3

## System response
- Test / lint / script
- Spec / KB / architecture note
- Skill / review rubric / custom reviewer
- PR evidence template
- Design-to-spec guidance
- Runtime evidence path
- Follow-up task only

## Owner

## Signal
What will show whether this feedback stops recurring?
```

### Rework Note

```
# Rework Note

## Why this run is not mergeable

## Root cause
- Missing context
- Missing validation
- Missing architecture guidance
- Missing design reference
- Missing runtime evidence
- Missing tool or access path
- Ambiguous spec
- Unsafe scope
- Other:

## System improvement before retry

## What to preserve

## What to discard

## Retry instructions
```

---

*Companion relationship: The Implementation Track explains how the team executes agent-assisted cycles. The Definition Track explains how those cycles stay fed. Compounding Work explains how the team converts friction from both tracks into future delivery capacity.*
