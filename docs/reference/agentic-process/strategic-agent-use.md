---
source: agentic-process/www/strategic-agent-use.html
summary: "Playbook for choosing agent collaboration modes across exploration, critique, validation, implementation, partnership, delegation, and learning."
reference-kind: guide
draft: v0.2
last-updated: 2026-05-06
---

# Agent Use Playbook

> *Choosing the right mode of agent collaboration for exploration, critique, validation, implementation, and learning.*

*Draft v0.2 — Last updated 2026-05-06. Expect continued refinement as teams put it into practice.*

> **This guide applies across both tracks.** The [Definition Track](definition-track.md) uses agents to clarify intent, reconcile sources, and author execution-ready specs. The [Implementation Track](implementation-track.md) uses agents to build, review, validate, explain, and harden. Strategic agent use is the shared skill that makes both tracks more creative and more reliable.

Most teams learn agentic delivery by delegating implementation tasks. That is useful, but it is only the first move. The larger shift is learning when to use agents for exploration, critique, validation, teaching, and durable learning before asking them to produce more code.

## Why Delegation Is Not Enough

*Agent delegation* (handing off well-defined work to an agent and reviewing the output; fits core buildout, mechanical hardening, and simple tasks that can be specified clearly) works when the task is bounded, the contracts are clear, and the output can be reviewed. Many high-value delivery problems are not like that yet. They are fuzzy, cross-cutting, or quality-sensitive. The job is not "go build this." The job is "help us understand what this should be, what could go wrong, and what support would let future work move more safely."

*Agent partnership* (working with an agent as a thinking collaborator — asking questions, pressure-testing plans, explaining what changed, brainstorming gaps; fits planning, review, and human-judgment steps where framing matters) fills that gap. It uses the agent as a thinking, reviewing, teaching, and operating partner while people keep authority over intent, tradeoffs, and acceptance.

> Do not ask only, **what can the agent do for me?** Ask, **what kind of thinking or evidence would make the team stronger right now?**

> Before asking for output, make three decisions: what mode of collaboration you need, what altitude the agent should work at, and what evidence would make the result trustworthy.

## Not Just for Coding

Strategic agent use helps any role turn messy context into clearer judgment, stronger evidence, or durable learning.

| Role | Useful modes |
| --- | --- |
| Delivery lead | Status synthesis, risk scans, blocker summaries, client decision batching. |
| Designer | State extraction, screenshot comparison, accessibility review, design-to-spec translation. |
| Definition lead | Transcript synthesis, active interviewing, contradiction scan, spec readiness review, decision tracking. |
| Developer | Onboarding, plan critique, foundation review, validation, explanation. |

## The Core Moves

These six moves are the meta-skills underneath most advanced agent use. They can happen inside one prompt or across a delivery cycle.

1. **Aim** — Name the job: explore, critique, interview, test, teach, implement, distill, or improve the harness.
2. **Ground** — Load the source material that should constrain the work: repo, spec, transcript, prototype, design, data, logs, or product surface.
3. **Challenge** — Ask for pressure, alternatives, edge cases, hidden assumptions, and failure modes before the team commits too much surface area.
4. **Prove** — Require evidence appropriate to the risk: tests, screenshots, line references, source links, command output, or known limitations.
5. **Distill** — Turn the session into durable learning: decisions, specs, examples, review queues, and next prompts.
6. **Encode** — When a useful move repeats, move it out of chat and into durable support.

## Choosing the Right Mode

Mode is the first decision. When work feels risky, slow, or unclear, choose the collaboration pattern that changes the shape of the problem before asking for more output.

| Situation | Mode | Default move |
| --- | --- | --- |
| I do not know the system yet. | Onboarding Scout | Ask for a map before asking for a patch. |
| The team has several plausible directions. | Research Partner or Plan Critic | Compare options and pressure-test tradeoffs. |
| The spec feels tangled. | Boundary Mapper | Split product, UX, technical, data, and operational decisions. |
| The agent keeps guessing. | Active Interviewer | Have it ask for the next missing decision instead of filling gaps. |
| A feature is too large to trust in one pass. | Foundation Builder and Reviewer | Build contracts first, then review the system shape. |
| The UI might look done but behave wrong. | Interactive Smoke Tester | Have the agent use the product and bring back evidence. |
| The same failure keeps coming back. | Harness Converter | Turn repeated friction into instructions, skills, scripts, review agents, or examples. |

## Control the Altitude

Altitude is the second decision. Many bad agent runs happen because the agent is solving at the wrong layer. Before asking for work, name whether you want landscape, decision, contract, foundation, implementation, validation, or distillation.

| Altitude | Agent should produce |
| --- | --- |
| Landscape | Map, risks, unknowns, relevant surfaces, likely constraints |
| Decision | Options, tradeoffs, recommendation, evidence that would change the call |
| Contract | API shape, data model, UX states, acceptance criteria, boundaries |
| Foundation | Seams, first tests, fixtures, skeleton, invalid-state handling |
| Implementation | Scoped code or document changes with validation |
| Validation | Commands, screenshots, runtime evidence, failures, limitations |
| Distillation | Decisions, spec deltas, examples, next prompts, harness candidates |

## Evidence Ladder

Evidence is the third decision. Raise the evidence requirement as risk rises. Low-stakes exploration can use assumptions. Work that will drive implementation needs sources, file references, command output, screenshots, runtime evidence, or explicit human decisions.

| Evidence level | Use when |
| --- | --- |
| Stated assumption | Early thinking and low-stakes exploration |
| Source reference | Research, synthesis, definition work, spec drafting |
| File or line reference | Code review, architecture review, doc review |
| Command output | Tests, typecheck, setup, validation, generated artifacts |
| Screenshot or video | UI behavior, design comparison, product flow review |
| Runtime evidence | Logs, metrics, traces, production-like behavior, release confidence |
| Human decision | Product intent, client tradeoff, safety, risk acceptance |

## Mode Palette

Use modes deliberately. This palette is a set of examples, not a checklist to exhaust. Pick the specialized purpose that fits the moment: exploring unclear work, strengthening implementation quality, or preserving learning for future cycles.

### Explore and Shape

Use agents to make the problem more legible before asking them to change the system.

#### Onboarding Scout

The agent reads the terrain and reports the implementation surface, contracts, missing pieces, and likely risk areas.

#### Research Partner

The agent loads sources, compares options, and helps narrow the team's next decision rather than producing a final answer too early.

#### Active Interviewer

The agent asks targeted questions one at a time, updates the working plan, and keeps ambiguous definition work moving.

#### Boundary Mapper

The agent separates product behavior, technical contracts, data ownership, UX source of truth, and operational concerns.

### Build and Validate

Use agents as quality multipliers around implementation, not only as implementation workers.

#### Plan Critic

The agent argues against the plan, names hidden coupling, and looks for simpler or safer sequencing.

#### Foundation Builder

The agent establishes APIs, data shapes, module boundaries, fixtures, and first tests before filling in the whole feature.

#### Foundation Reviewer

The agent reviews the seams while they are still cheap to change: contracts, authz, invalid states, and future replacement paths.

#### Specialist Reviewer

A scoped agent reviews one concern such as security, accessibility, data correctness, architecture fit, or test evidence.

#### Interactive Smoke Tester

The agent uses browser or computer-use tools to walk the product, capture screenshots, and report visible behavior.

### Teach and Compound

Use agents to preserve understanding so the next cycle starts from a stronger system.

#### Explainer

The agent walks through what changed, why it changed, where the risks are, and how a teammate should review it.

#### Doc Steward

The agent updates specs, effort notes, module context, decision logs, and examples so reality stays written down.

#### Tool Failure Interpreter

The agent separates introduced failures, baseline noise, environment blockers, and unrelated repo drift.

#### Harness Converter

The agent identifies repeated friction and proposes the smallest durable support surface to prevent it next time.

## Prompt Anatomy

A useful strategic prompt tells the agent what collaboration mode this is, what source material constrains it, where the boundary is, and what evidence-backed artifact should exist when the turn is done.

### Mode

Tell the agent what kind of work this is: onboarding, critique, interview, smoke test, documentation, review, or implementation.

### Context

Name the source material it must use: files, specs, prototype, transcript, design, ticket, logs, data, or product surface.

### Boundary

Say what not to solve yet: no code changes, no redesign, only contracts, only questions, only review findings.

### Output and evidence

Ask for the artifact you need next and the proof that would make it trustworthy.

## Example Prompts

These prompts are meant to be edited, not copied blindly. The pattern is the important part: name the mode, ground the agent, set a boundary, and ask for a reviewable output.

*Onboarding Scout*

### Onboard Before Output

```
Onboard into [files/specs/prototype]. Do not implement yet.

Tell me:
- what already exists
- what contracts and boundaries matter
- what is missing or ambiguous
- where the risky decisions are
- the smallest useful first slice

Ground the answer in the repo and call out assumptions separately.
```

*Active Interviewer*

### Active Interview

```
Help me turn this rough idea into an execution-ready spec.

Ask me one decision at a time. After each answer:
- update your working model
- tell me what became clearer
- ask the next highest-leverage question

Do not give me a giant questionnaire.
```

*Plan Critic*

### Adversarial Plan Review

```
Review this plan adversarially before implementation.

Focus on:
- hidden assumptions
- contract mismatches
- missing data or authz concerns
- places the agent might overbuild
- validation gaps
- simpler sequencing options

Return findings ordered by severity, then suggest a revised plan.
```

*Foundation Reviewer*

### Foundation Review

```
Review this foundation pass before we build more surface area.

Focus on contracts, boundaries, invalid states, authz, fixture realism, and test coverage.
Return blocking issues first with file references.
```

*Interactive Smoke Tester*

### Interactive Smoke Test

```
Use the browser/computer-use tools to smoke test [flow].

Capture what you tried, what happened, screenshots for visual issues, console or network errors, and mismatches against the spec. Do not fix anything until you report findings.
```

*Doc Steward*

### Distill Durable Context

```
Distill this session into a durable working note.

Include:
- decisions made
- open questions
- links to relevant files/specs
- risks and assumptions
- next prompts or tasks
- what future agents should know before continuing

Keep it specific enough to survive context compaction.
```

## Apply It by Track

The same meta-skills show up in different clothing. Definition work needs better questions and synthesis. Implementation work needs better boundaries and evidence. Compounding work needs better recognition of what should become reusable support.

### Definition Track

*Clarify intent*

- Have the agent synthesize transcripts, research, prototypes, and stakeholder feedback into decisions and open questions.
- Use active interviewing to keep spec authors from getting buried in reading homework.
- Ask for contradiction scans and spec-readiness reviews before committing behavior.

### Implementation Track

*Improve quality*

- Start complex work with onboarding, then phase one foundation work, then foundation review.
- Run adversarial reviews before implementation and again while the foundation is cheap to change.
- Use interactive smoke tests to inspect flows agents cannot validate from code alone.

### Compounding Work

*Raise capacity*

- Watch for repeated prompts, repeated review feedback, setup friction, missing examples, and weak validation evidence.
- Convert stable moves into skills, scripts, custom review agents, fixtures, repo knowledge, or safety gates.
- Prefer small reusable supports over a giant prompt that only helps one session.

## A Complex Feature Example

Imagine a reporting feature with a prototype, ETL constraints, exports, permissions, and several product views. A naive delegation prompt asks an agent to build the page. A strategic sequence makes the work smaller, more inspectable, and easier to review.

1. **Orient** — Have the agent map the feature surface, source material, constraints, and safest first slice before proposing implementation.
2. **Decompose** — Separate product behavior, UX states, technical contracts, data ownership, validation, and release risk.
3. **Pressure-test** — Review the plan and foundation while contracts are still cheap to change.
4. **Validate and distill** — Collect evidence, classify failures honestly, update durable context, and name any harness candidates.

## First Reps to Try

Advanced agent use is learned through reps. These are small enough to try on current work without redesigning the whole process.

### Before a build

Ask an agent to onboard into three files and return the implementation surface, risks, missing contracts, and recommended first slice.

### Before approving a plan

Ask for an adversarial review: what could be wrong, too broad, under-specified, or hard to validate?

### After a prototype

Ask the agent to extract UI states, interaction contracts, empty states, and data assumptions.

### After a working session

Ask for a durable context note with decisions, unresolved questions, links, and next prompts.

### After a bad run

Ask what went wrong, what context or tool was missing, what to preserve, what to discard, and what should change before retrying.

## Anti-Patterns

The goal is not more ceremony. It is better leverage. These patterns add tokens without adding judgment, evidence, or durable learning.

### One giant handoff

The agent receives a broad feature request and starts coding before it understands the terrain.

### Generic reviewer

A review agent is asked whether the work is good without a lens, source of truth, or severity model.

### Prompt as landfill

Every lesson gets pasted into the next chat instead of moving into docs, scripts, skills, or examples.

### Unbounded critique

The agent keeps finding new concerns but never helps decide what matters now.

### Toy implementation

Static data or prototypes bypass real auth, routing, validation, or contract boundaries.

### Silent doc drift

The code changes but specs, effort notes, examples, and operating docs still describe the old system.

## When a Move Becomes Harness

Strategic agent use is often the discovery layer for *harness engineering* (the practice of making the delivery harness more agent-operable by improving context, tools, feedback loops, validation, structure, permissions, and boundaries). The first time you use a move, it can live in a chat. The second or third time, use [Compounding Work](compounding-work.md#the-basic-move) to decide whether it should leave the chat, then use the [Agent Harness Playbook](agent-harness-playbook.md#the-harness-surface-model) to choose the durable surface.

| Signal | Likely durable home |
| --- | --- |
| Repeated prompt | Skill or entry instruction |
| Repeated review feedback | Specialist reviewer, lint rule, test, or example |
| Repeated setup failure | Script, bootstrap check, or clearer repo entry doc |
| Repeated uncertainty about source of truth | Decision record, architecture note, or spec link hygiene |
| Repeated safety hesitation | Permission boundary, approval gate, or safety spec |

> **Next practitioner guide:** Use [Compounding Work](compounding-work.md#when-to-invest) to decide when a friction point deserves system investment. Use the [Agent Harness Playbook](agent-harness-playbook.md#the-harness-surface-model) to decide whether that investment belongs in instructions, skills, custom agents, scripts, repo knowledge, or safety gates.
