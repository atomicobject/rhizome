---
source: agentic-process/www/implementation-track.html
summary: "Describes the short planning, implementation, and review cadence used to delegate, execute, validate, and review agentic delivery work."
reference-kind: guide
draft: v0.2
last-updated: 2026-05-06
---

# The Implementation Track

> Part of Atomic's Agentic Delivery Process — how a team plans, delegates to agents, executes, and reviews work.

*Draft v0.2 — Last updated 2026-05-06. Expect continued refinement as teams put it into practice.*

> **Scope of this document:** This process covers the development cycle — how a team plans, builds, and reviews work once *specifications* (evergreen descriptions of how a part of the system or experience works; kept current with actual behavior — when the team wants a change, the spec is updated first and implementation brings the system into alignment) exist and the project has enough direction to execute. It does not cover how specifications are authored, how client intake and discovery work, or how product decisions are made upstream. That work lives in the companion [Definition Track](definition-track.md), which feeds this one.

Atomic exists to turn care, craft, and curiosity into lasting value for our clients, communities, and one another. Vibe coding puts those strengths at risk by delegating away too much judgment, exploration, and refinement. Agentic engineering is the disciplined use of coding agents as primary implementors, with human attention focused where it creates the most value: shaping direction, exercising judgment, and validating outcomes. This document is Atomic's process for agentic engineering — the cadence, responsibilities, and disciplines that make it work.

## Core Dev Process

The main workflow is a short planning → implementation → review cadence for work with enough validated direction to execute. It is meant to keep implementation volume from outrunning team comprehension. Target **two cycles per week**.

This is an internal team rhythm, not a client-demo promise. Planning and review create shared comprehension every 2-3 days; formal demos, progress reporting, and velocity conversations should stay calibrated to the client relationship.

**Diagram — Sprint Loop (Planning → Implementation → Review, then loop).**

Legend:

- **Team** — Full team activity
- **Solo** — Individual developer
- **Collab** — Pair at judgment moments — agent wait cycles become conversation space

Phases:

- **Planning** (linked to [Phase 1: Planning](#phase-1-planning))
  1. Refine Specifications — Team
  2. Scope & assign — Team
  3. Analyze & plan — Solo / Collab (heavier step)
  4. Review the plan — Team
  - Approximate task time bar: refine (3) / scope (1) / analyze (5, heavy) / review (2)
- **Implementation** (linked to [Phase 2: Implementation](#phase-2-implementation))
  1. Identify the next agent blocker — Solo
  2. Provide human input to unlock it — Solo / Collab
  3. Let the agent work, then loop — Solo
  - ↺ Repeat this loop through the cycle
- **Review** (linked to [Phase 3: Review](#phase-3-review))
  1. Review significant items — Team (Demo, explain, and probe each together.)
  2. Remediate or scope follow-ups — Solo / Collab (heavier step)
  - Approximate task time bar: review-loop (5) / remediate (5, heavy)
- ↩ Next cycle begins

This cadence forces clarity before build work begins, keeps increments small enough to validate and merge, creates frequent human alignment points before drift compounds, and preserves team orientation as implementation accelerates.

Planning and review take more time than they would in a traditional process, and that is deliberate: short cycles with significant planning and review spend part of the agent efficiency gain on team judgment rather than coding volume. **Analyze & plan** and **Remediate** are the heavier steps, done solo or paired; the other rituals are shorter alignment points.

### Weekly Cadence

**Diagram — Weekly cadence:** two internal cycles per week, with client communication, design, spec authoring, and context capture running in parallel beneath the cycle loop.

Week grid (Mon–Fri, AM/PM):

- **Mon AM** — Planning
- **Mon PM** — Implementation
- **Tue AM** — Implementation
- **Tue PM** — Implementation
- **Wed AM** — Review
- **Wed PM** — Planning
- **Thu AM** — Implementation
- **Thu PM** — Implementation
- **Fri AM** — Implementation
- **Fri PM** — Review

Cycle bands:

- **CYCLE 1** — Mon AM (Planning) through Wed AM (Review)
- **CYCLE 2** — Wed PM (Planning) through Fri PM (Review)

**Definition Track** (feeds the next cycle, runs in parallel beneath the cycle loop):

- Client communication
- Design
- Spec authoring
- Context capture

Each week runs two cycles, each bookended by planning and review with implementation filling the middle. Inside the cycle, the work focuses on delivering the committed increment and the support it needs — cleanup, exploratory testing, and [compounding work](compounding-work.md) (delivery-capacity work that makes future cycles better: stronger specs, better examples, agent skills, runtime evidence paths, validation, patterns, tooling, and review workflows — first-class delivery work, not cleanup) tied directly to the delivery.

Alongside it, the [Definition Track](definition-track.md) feeds the cycle with decisions, designs, specs, examples, and rationale. If definition lags, throughput stops being implementation-bound and starts waiting on direction.

> **Two modes, different tasks.** The cycle uses agents in two modes. *Agent delegation* — handing off well-defined work and reviewing the output — fits core buildout, mechanical hardening, and simple tasks that can be specified clearly. *Agent partnership* — the agent asks clarifying questions, pressure-tests a plan, walks through what it built, or brainstorms what might have been missed — fits planning, review, and the human-judgment steps inside implementation (Shape, Explain, Validate). The implementation middle runs on *agent delegation*; the edges of the cycle lean on *agent partnership*. The [Agent Use Playbook](strategic-agent-use.md) expands those modes into practical techniques for research, critique, foundation review, smoke testing, documentation, and harness improvement.

## Phase 1: Planning

Planning creates enough shared understanding, scope, and validation intent for fast execution to remain safe.

| Step | Activity | What Happens | Who |
|---|---|---|---|
| 1 | Refine Specifications | Review prioritized specs. Pressure-test them together to surface missing context, ambiguity, blockers, and architectural fit. Agree what is ready to build. | Team |
| 2 | Scope & assign | Freeze work slices and ownership. Mix complex, simple, and compounding work per maker. | Team |
| 3 | Analyze & plan | Create Effort documents and implementation plans for key work. Use agents to explore options and phase checkpoints. | Solo / Collab |
| 4 | Review the plan | Review the plan as a team, focusing on the high-stakes elements and surfacing shared needs early. | Team |

### Planning in Detail

**1. Refine Specifications** — A **Specification** is an evergreen description of how a part of the system or experience works: behavior, acceptance criteria, constraints, and areas of discretion. Specs are kept current with how the system actually behaves; when the team wants to make a change, the spec is updated first and implementation brings the system into alignment. At the start of a cycle, the team reviews specs and pressure-tests them together to surface what is missing, ambiguous, or blocked, then agrees on what is ready to build.

As a team, also ask how the prioritized work fits into the system's evolving structure. If a spec implies an approach that diverges from established patterns, name it now — intentional departures are fine; unexamined ones compound quickly when agents are doing the building.

**2. Scope & assign** — The team freezes cycle slices and assigns ownership. Assignments should mix higher-attention work with smaller tasks, and the team should treat **compounding work** — work done to improve future delegation, such as stronger specs, better examples, agent instructions, seeded patterns, and validation automations — as high-priority cycle work rather than subordinating it to feature work by default; in an agentic loop, that investment raises future delegation capacity quickly.

Right-size tasks to fit the cycle — work taken on at the start should be completable by the end, with mergeable progress along the way. If you can't articulate what you'd merge right now, the task is probably too large.

#### Within 2. Scope & assign — Mix Complex and Simple Tasks

**Complex** and **simple** describe how much human input a task needs, not how much code changes. A new feature with unfamiliar requirements may need extensive iteration to get the design and edge cases right; a large refactor may be straightforward to delegate because the agent can take general guidance and reason about how to apply it across the codebase, using tests to confirm it did the right thing.

Plan a mix. While a longer implementation step runs on one complex task, you can shape the next complex task or clear smaller ones. The mix matters because simple tasks create safe progress while one complex task holds most of your attention; too many active complex tasks raise cognitive load fast.

**Diagram — Complex / Simple spectrum.**

- **Complex**
  - Major features, new systems
  - High attention, multiple checkpoints, meaningful design decisions
  - Usually one focus item at a time
- **Simple**
  - Small fixes, refinements, tweaks
  - Clear to specify; useful to pull while longer agent work runs
  - Lower-friction work beside one complex task

Axis: ← Higher human attention | More delegatable →

**3. Analyze & plan** — Analysis is the primary opportunity for human judgment before implementation begins. What gets decided here — approach, decomposition, risks, checkpoints, release strategy — shapes every delegated step that follows. A sound plan multiplies across generation; a weak one multiplies rework.

Developers create an **Effort** for each work slice they own. An Effort is the execution artifact: an agent-ready task assignment linked to one or more Specs and kept current as a running log of plans, checkpoints, decisions, deviations, and follow-ups. Because it is written down, it also becomes working context for the agent.

Developers also create implementation plans for key work, especially complex tasks. The Effort is the container for the work; the implementation plan is the proposed shape of how to execute it. If the approach is unclear, use the agent to inspect relevant code, propose alternatives, and help converge on a phased plan. If the approach is already forming, seed the conversation with a brain dump and pressure-test it.

Pressure-test the plan across structure, architecture, *non-functional requirements* (NFRs — qualities the system must exhibit rather than features it must have: performance, security, reliability, accessibility, observability, maintainability, and similar cross-cutting concerns), validation, approval gates, and release strategy. Break it into phases with human checkpoints, and review key design elements before broader implementation begins.

> **Planning is the main quality lever in this process.** The difference between agentic engineering and vibe coding is deliberate direction before large-scale generation.

**4. Review the plan** — Review the plan as a team before execution begins. Focus on the high-stakes elements: architecture, infrastructure, shared patterns, dependencies, and other decisions that would be costly to unwind later. A key question for each plan: does this approach fit how we are building this system, and where it diverges, is that intentional and understood by the whole team? The goal is to align on the blueprint and surface shared needs before execution begins — not to rehash every detail.

## Phase 2: Implementation

### Default Feature Workflow

The workflow below is the default for **complex tasks** — work entangled with decisions that need explanation or clarification: new features, architectural shifts, changes that cross multiple specs. Simple tasks (contained changes, applying patterns, adding tests for existing criteria) skip this cycle; they can be delegated with light guidance and merged once reviewed.

Because code authoring is no longer the bottleneck, complex tasks are often broader and more holistic than traditional development work items, moving the system toward an updated specification rather than a single ticket completion. Most benefit from a structured process that establishes good foundations early, builds in phased and potentially mergeable increments, and closes by explaining, validating, and reconciling the work so quality and human understanding keep up with generation.

This is a guide, not a rigid stage-gate. Teams should move back and forth as new evidence appears, but the overall pattern remains: make the important decisions early, then scale implementation against them.

**Diagram — Complex task workflow.** Three bands: Shaping → Building → Validating, with an inner "Repeat Per Phase" loop spanning Build/Harden, and an outer "Reopen on findings" loop that returns from Validate back to Shape.

Steps:

- **Shape** — Establish foundations. Key design decisions: schemas, APIs, module boundaries. (Solo)
- **Review** — Pressure-test the design. Walk through trade-offs with a review prompt or a pair before scaling up. (Solo / Collab)
- **Build** — Let the agent build in phases. Implement against the plan, review at each phase boundary. (Solo)
- **Harden** — Check and commit. Code review, tests green, address gaps, merge the phase. (Solo)
- **Explain** — Walk through what changed. Agent walks through the work; the pair builds the shared picture the stress-test will need. (Solo / Collab)
- **Validate** — Stress-test the whole. Exploratory testing, lateral review, NFR check, catch what phases missed. (Solo / Collab)
- **Close** — Reconcile and deliver. Reconcile with spec, back-port intentional deviations, capture follow-ups, merge. (Solo)

Loops:

- Inner loop: Build ↔ Harden, "Repeat Per Phase".
- Outer loop: Validate → back to Shape, "Reopen on findings".

Within implementation, Build and Harden form an inner loop at each phase checkpoint — the *agent delegation* (handing off well-defined work to an agent and reviewing the output; fits core buildout, mechanical hardening, and simple tasks that can be specified clearly) middle of the workflow. The per-phase Harden is lightweight and mechanical: run the *code review agent* (automated code review tools that focus human attention by scanning agent output for issues before the slower human-judgment layer), make sure tests are green, address obvious gaps, and get the phase committed. Keep moving.

Once the build phases are complete, the work shifts from *agent delegation* back into *agent partnership* (working with an agent as a thinking collaborator — asking questions, pressure-testing plans, explaining what changed, brainstorming gaps; fits planning, review, and human-judgment steps where framing matters). Explain comes first: the agent walks through what actually changed — design choices, deviations from the plan, surprising edges — and the owner builds a clear mental picture of what got delivered. Without that picture, stress-testing is shallow; with it, attention can target the real risk surfaces.

Validate is a different kind of work from per-phase hardening. Step back and stress-test the feature as a whole: exploratory testing across the full surface, lateral brainstorming about what the phase-by-phase loop might have missed, and a check against the NFRs and testing strategy established during planning. Validate is a gate, not a station — if something surfaces, it reopens Build rather than getting pushed into Close as a follow-up. The beginning and end of a complex task deserve more human attention than the middle phases.

Review, Explain, and Validate are the natural pairing moments. Shape is mostly agent-led, and driving parallel agents through Build and Harden is solo work — the context moves too fast across chats for a second human to share the load. Review, Explain, and Validate are different: they are points of human judgment where a second perspective catches what the primary driver cannot. Pair when the stakes warrant it — especially on complex tasks, unfamiliar territory, or areas where the cost of missing something is high.

### The Developer as Scheduler

Once work is shaped enough to run, execution becomes a loop: identify the next blocker that needs human input, clear it, then let the agent work. In practice, keep an ordered sense of current priorities and checkpoints, keep WIP low, and shift attention to other work while longer agent runs continue. Use worktrees or secondary checkouts when parallelizing.

**Diagram — Developer Loop.**

Current Priorities (ordered list, top-down):

- **COMPLEX (▲ TOP)** — Checkout flow. Review phase 1 and start phase 2.
- **SIMPLE** — Complete and commit. Merge a reviewed checkpoint.
- **COMPLEX** — Reporting dashboard. Start shaping phase 1 when room opens.
- **SIMPLE** — Add another signup field. Update form, validation, storage, and tests.

Pick → Developer Loop:

- **BRIDGE THE CHECKPOINT** — Wrap or start, then tee up the next agent run.
- → **AGENT RUNS** — Agent works autonomously until the next checkpoint.
- → **TOO MUCH WIP?**
  - **yes:** monitor / guide active runs (loops back into Agent Runs).
  - **no:** shift to next priority (loops back to Bridge the Checkpoint via the priority list).

Core disciplines (footer band):

- Rotate from clear handoff points
- Complex tasks get priority attention
- Keep WIP low

Caption: When agents struggle or repeat mistakes → diagnose root causes → improve the system (tests, CLAUDE.md, skills, refactoring).

In practice, usually keep one main complex task as the priority path to completion. Pulling a second complex task can make sense when the first has longer-running build phases, but that raises cognitive load quickly, so simpler tasks are often the better companion work. They create useful progress without competing for as much attention. Completion work belongs in that prioritization too: finishing, committing, and merging a reviewed checkpoint is still work that may deserve attention next. Those priorities also shift as frictions, follow-ups, or newly discovered work surface during execution.

> **Note:** Keep the Effort log current: decisions, deviations, findings, follow-ups, release notes. This makes review much easier by keeping a running record of what actually got done and showing where specs may need to be updated to reflect intentional implementation drift. Lean on agent instructions and process documentation to keep this automatic — if updating the log depends on human discipline, it will drift; if it's baked into how the agent works, it stays current without thought. Merge stable checkpoints into main aggressively — large unintegrated branches are a failure mode, not a virtue. Flag incomplete work.

### Done Means

- Spec and implementation agree
- Validation is complete; runtime evidence exists when needed
- Important findings are fixed or captured explicitly
- The owner can explain the design, checks, risks, and merge/release judgment

### When to Pull Someone In

This process changes when and how developers collaborate, but not whether they do. The biggest shift is that collaboration moves earlier, into planning and review, where design decisions, validation strategy, and system-shaping choices carry more leverage. During execution, pull someone in when a decision is both a judgment call and will shape what comes after. The common moments:

- Key design decisions on complex tasks (schemas, APIs, architecture boundaries)
- Security-sensitive logic (auth, permissions, data access)
- Novel integrations the agent hasn't encountered in this codebase
- Failure modes that would be silent rather than loud
- When you can't clearly explain what the agent produced
- When Specs are ambiguous and the answer isn't obvious

## Phase 3: Review

Team review is the team's primary human quality gate. It keeps implementation volume from outpacing team comprehension by applying experienced judgment every 2–3 days rather than after a full week or more of agent-driven output.

| Step | Activity | What Happens | Who |
|---|---|---|---|
| ↻ per item | Demo & explain it | For each significant piece of work, the owner explains what changed, how it is structured, how it was verified, and what frictions slowed delivery. | Team |
| ↻ per item | Probe it together | Ask questions, surface risks and implications, explore alternatives. Repeat until the team has reviewed every significant piece of work. | Team |
|  | Exploratory testing | Step back and exercise the cycle's combined output hands-on: end-to-end flows, integration between newly landed pieces, regressions, UX friction. Surface findings for remediation. | Solo / Collab |
|  | Remediate or scope follow-ups | Break into solo or pair work. Fix low-hanging issues immediately; turn larger issues into specs or tasks for the next cycle. | Solo / Collab |

Demo and probe form a loop per item. Once the team has worked through every significant piece of work, people break off for hands-on exploratory testing of the combined output, then converge on remediation — fixing issues, writing follow-up specs, or scoping the next cycle.

## Entering New Architectural Territory

The cycle above assumes the team has a shared understanding of how the system is built — established patterns, module boundaries, interface conventions, and a shared sense of where the architecture is heading. That understanding is what makes high-throughput delegation safe. When it doesn't exist yet, the team needs to operate differently before settling into the standard rhythm.

The most common case is a new project, but this applies any time the team is entering genuinely unfamiliar structural ground: a major new subsystem, a significant domain that doesn't follow existing patterns, or a foundational refactor that changes how the system is layered.

### Practices for Architectural Establishment

**Seed the structure deliberately.** Before delegating at scale, the lead developer — or the team together — should think through and make explicit choices about layering, module boundaries, interface design, and key abstractions. Write foundational code by hand to force deliberate structural decisions. A few well-considered examples set the pattern that agents will follow for the rest of the project.

**Mob program around key decisions.** Getting the whole team writing and reading the same code together is one of the fastest ways to build shared architectural intuition.

**Spike, iterate, then commit.** It can be useful to have an agent spike out a structural approach, but treat the result as a conversation starter, not a foundation. Iterate on it, reshape it, and make sure the team understands and agrees with the structural choices before building on top of them.

Once the team has a shared structural foundation and can confidently answer "does this fit how we build this system?", the standard cycle takes over and the planning-phase alignment checks keep things on track.

## Cross-Cutting Concerns

These four concerns change in kind in agentic engineering and become first-class objectives throughout the process. They shape whether delegation stays aligned, safe, and sustainable across planning, execution, and review. Two are worked inside the cycle; two have to stay ahead of it and feed it.

**Inside the Loop**

- **Quality & Validation** — Quality work becomes more layered and more demanding. Because agent output scales faster than human review, teams need stronger validation, regression signals, and agentic feedback loops so mistakes are caught quickly and agents can course-correct.
- **System Improvement** — System improvement becomes first-class delivery work. Teams are continuously engineering the engineering system itself: patterns, tooling, guidance, review flows, and boundaries that make future delegation safer and more effective.

**Feeds the Loop**

- **Discovery & Decision Flow** — Discovery and clarification become a primary constraint. Higher throughput raises the demand for client access, product decisions, design detail, QA thinking, and fully shaped specs that can keep the process supplied with clear work.
- **Shared Context** — Shared context can no longer live mostly in the team's heads. Agents start fresh each time, so decisions, rationale, domain knowledge, examples, and integration constraints need to be captured in distilled forms that support both human alignment and agent execution.

## Quality: Layered, Not Line-by-Line

Line-by-line review of every agent-generated change should not be the default quality strategy. The call to action is to reduce the need for it: engineer layered checks that surface correctness, design issues, and drift early, then reserve deep code reading for the places where it has real leverage — foundations, architecture, security, unfamiliar territory, silent failure modes, and moments where human understanding is the point.

This does not mean "do not read the code." It means read code deliberately. Deep review is still appropriate for high-risk, novel, security-sensitive, architectural, or silently failing areas, and for work the owner needs to understand deeply. The point is to move routine correctness into stronger layers so humans can spend their attention where judgment matters most.

Specifications are the backbone — turn their acceptance criteria into automated checks that define what "correct" means. But tests aren't only spec-derived: agents should follow red-green TDD during implementation (write the test, watch it fail, make it pass), every bug fix should add a regression test, and edge cases surfaced while building often reveal spec gaps. When they do, fix the test and backport the insight into the spec so the knowledge lives where the next agent will look — not just as an extra check.

The lower layers should look familiar: types, linters, and tests are not new, but they should be much stronger than they were in a classical loop. The newer adaptation is adding review-agent layers above them so delegated output gets more feedback before the slower human-judgment layer becomes the limiting step.

**Diagram — Quality pyramid (top to bottom, narrow to broad):**

- **Human Judgment** — targeted review, pairing, team review
- **Specialized Review Agents** — security, conventions, design decisions
- **Code Review Agents** — general review, spec compliance, architecture
- **System & Integration Tests** — acceptance criteria, end-to-end flows
- **Unit Tests** — implementation logic, invariants, edge cases
- **Types, Linters & Formatters** — covers every line

Lower layers are broad and fast; upper layers are narrower and slower. No single layer catches everything, but stacked together few holes align — the [Swiss cheese model](https://en.wikipedia.org/wiki/Swiss_cheese_model). Push as much as possible into automated checks so human judgment stays focused on what machines cannot evaluate.

Review agents are particularly useful because they focus human attention instead of asking humans to scan everything. They add a new layer above the familiar ones; they do not replace the need for solid types, tests, and runtime checks underneath.

## Improving the Delivery System

Teams are not only shipping features — they are also doing **compounding work**: improving the system that agents and developers use to ship them. In agentic engineering, the ROI on this work has shifted on both sides. Returns are higher: one better pattern, instruction, example, boundary, or tool improves many future delegated steps, while one weak spot causes repeated drift, repeated prompting, or recurring mistakes. Costs are lower: agents help build the improvements themselves, so new patterns, skills, scaffolds, and automations are dramatically cheaper to produce than they used to be.

These effects compound in both directions: accumulated friction lowers delegation quality and team throughput, while accumulated improvements raise future delegation capacity and overall leverage. When agents struggle, treat that as a signal to improve the system, not just the task.

That includes codebase patterns, instructions, skills, scaffolds, specialized workflows, and the tools agents need to inspect the system well: production metrics and logs, visual inspection, data introspection, and CI results. The companion [Agent Use Playbook](strategic-agent-use.md) expands the ways agents can help with research, critique, validation, documentation, and review; [Compounding Work](compounding-work.md) turns recurring friction into planned future-capacity improvements; the [Agent Harness Playbook](agent-harness-playbook.md) explains how to configure the agent-operable parts of that system in Codex, Claude Code, scripts, repo knowledge, and safety gates.

## Knowing When It's Breaking

The implementation track fails in recognizable patterns. The signals below are ordered by priority — the earlier ones are the highest-leverage fixes because they're what most of the others depend on. Work the list top-down: if quality gates are weak (signal 1), don't spend time on signal 7 until that's resolved.

### 1. Quality gates or feedback loops are weak (Fundamental)

Every other check in this process assumes you can trust the feedback you get back. Types pass, tests are green, review agents surface real issues, validation catches regressions — when any of those layers are flaky, missing, or slow, delegation volume turns into hidden risk. This is the foundation; nothing else in this list matters until the feedback is reliable.

**What You're Seeing:** CI is flaky or takes too long to be useful. Tests have obvious gaps — behavior the spec describes has no check. Review agents run inconsistently, or their output is noisy enough to be ignored. Bugs reach production that should have been caught by an earlier layer. The team starts routing around the checks instead of fixing them.

**What To Do:** Treat fixing the feedback infrastructure as higher priority than new feature work. For each missed issue, ask which layer should have caught it and strengthen that layer — don't just patch the top. TDD during implementation, regression tests on every bug fix, and reliable review agents are the scaffolding that makes specs-as-checks actually work. Strengthen one layer at a time until you trust the stack.

### 2. Planning is rushed, thin, or skipped entirely (Fundamental)

Analysis is the primary opportunity for human judgment before implementation begins. When planning gets compressed — no Effort document, no implementation plan, no team review of the approach — the agent executes on weak direction and rework multiplies across every subsequent step. A sound plan multiplies across generation; its absence multiplies too.

**What You're Seeing:** Developers start tasks without an Effort or implementation plan. Specs enter planning with unresolved questions that get pushed into execution ("we'll figure it out when we get there"). The team is in a rush to start building. Mid-cycle, work stalls waiting for decisions that should have been made up front.

**What To Do:** Reduce cycle scope before reducing planning. The cadence already allocates generous planning and review time — if that's being skipped to fit more work in, the team is optimizing for output volume against its own process. Reinstate Analyze & plan as the gate it's supposed to be. If planning capacity is genuinely insufficient, cut scope or slow the cycle, not the analysis.

### 3. The team lacks shared architectural direction (Foundation)

Agents replicate patterns. When there is no cohesive, shared understanding of how the system should be structured — layering, module boundaries, interface shape, where the architecture is heading — every developer and every session makes reasonable but inconsistent choices. The codebase works but fights itself, and the problem scales with delegation throughput.

**What You're Seeing:** Similar work gets solved differently across efforts. Reviewers disagree about what "fits the system." Agents reproduce whichever pattern they encountered last. Duplicate abstractions accumulate. Tests pass but structure drifts. Refactor cost rises faster than feature velocity falls.

**What To Do:** Establish architectural direction before delegating at scale. Mob program around key decisions. Write foundational code by hand to force deliberate choices. Capture the direction in code, documentation, and examples agents can follow — see [Entering New Architectural Territory](#entering-new-architectural-territory). Until alignment holds, keep delegation narrower and increase human review around structural decisions.

### 4. The same agent mistakes keep recurring (Compounding Work)

Agents start every session fresh. If the team keeps re-prompting the same corrections — "don't do X," "the convention here is Y," "remember that Z is required" — that knowledge exists only in people's heads, not in the system. The cost isn't just repetition; it's that every future session starts from zero until someone teaches the system what the team already knows.

**What You're Seeing:** Review agents flag the same class of issue over multiple cycles. Developers paste the same corrections into conversations with agents. The codebase accumulates workarounds around the same few patterns the agent keeps getting wrong. "The agent just doesn't get it" becomes a team saying.

**What To Do:** Treat every recurring mistake as a signal to improve the system, not the task. Add the missing context to agent instructions, seed the correct pattern with an example the agent can reference, build a review-agent check for the class of issue. This is compounding work and the ROI has never been higher — agents help build the improvements themselves. If the same correction has happened twice, bake it into the system before it happens a third time.

### 5. Developers cannot clearly explain what landed during review (Review)

The Explain step in the feature workflow exists precisely to prevent this. When it's skipped or too light, the demo becomes an agent walkthrough instead of an owner walkthrough. The owner doesn't know why something was implemented a certain way; probing surfaces "the agent did it" instead of design reasoning. Review can't catch what it doesn't understand.

**What You're Seeing:** Demos are hand-wavy. Questions get "I'm not sure, that's what the agent did" as an answer. Design choices look arbitrary — the owner can describe what the code does but not why. When probing uncovers something unexpected, the owner can't tell whether it was intentional or a drift.

**What To Do:** Rebuild the Explain step. Before demo, have the agent walk through what changed, design choices made, deviations from the plan, and surprising edges — the owner builds a clear mental picture of what got delivered. Pair on Explain if needed, and don't merge until the owner can answer "why this?" for the key decisions.

### 6. Review feels perfunctory and concerns are not surfaced (Review)

Team review is the primary human quality gate beyond automation. When it's not catching anything, either the automation has already caught everything (unlikely at any real scale) or the review has gone empty — social demo, no pushback, no probing, no hands-on exploration. Problems accumulate until they surface somewhere less forgiving.

**What You're Seeing:** Nothing surfaces during probe. Demos are well-received but lightweight. Exploratory testing consistently finds things demo didn't. Engagement has settled into fixed patterns — the same few people always push back, the rest stay quiet. No one volunteers concerns that might slow the cycle.

**What To Do:** Put pressure back into review. Rotate who drives exploratory testing so fresh eyes exercise the surface. Invite someone outside the cycle's owner group to probe. Use the demo/probe loop to discuss tradeoffs and alternatives, not just describe what shipped. If review has gone stale, the cadence needs human friction — not more process.

### 7. Specifications are not staying ahead of implementation (Upstream Interface)

This signal shows up in implementation but it's an upstream failure. Planning runs into specs with gaps; developers ask questions mid-cycle that specs should answer; implementation stalls waiting on direction. The implementation track can't fix this by working harder — the lever is in the Definition Track.

**What You're Seeing:** Specs arrive at planning with visible gaps — missing acceptance criteria, TBD sections, unresolved questions in the spec body itself. The pattern repeats cycle over cycle, not as a one-off. Developers ask questions specs should answer and stall waiting on definition-side people for responses. The bottleneck isn't how the team engages with specs; it's that the specs aren't ready to be engaged with.

**What To Do:** Raise it with the team wearing definition hats. The Definition Track has its own failure-mode breakdown — see [signal-starved](definition-track.md#signal-starved) and [signal-reactive](definition-track.md#signal-reactive). Don't push half-formed specs into implementation just to fill the cycle. If definition is underfed, the honest response from the implementation side is to reduce appetite until the pipeline catches up.

### 8. Delivery volume is outpacing team comprehension (Systemic)

Agents make it possible to ship more than any team can reasonably hold in its head. When volume outpaces comprehension, nobody knows the current state of the system, regressions span cycles, and the team loses the ability to reason about what it built. This is usually a downstream symptom of several earlier signals.

**What You're Seeing:** New features pile up and nobody remembers why they were built. Regressions emerge across cycles, traced to interactions nobody understood. Review skims because there is too much to look at properly. Onboarding someone new gets harder every cycle. The codebase grows faster than the team's mental model of it.

**What To Do:** Slow the cadence. Not every cycle needs a full slate. Reduce appetite and invest in compounding work — patterns, examples, documentation, review agents — that makes the system easier to reason about. This signal rarely has a single cause; check the earlier signals first and fix what you find. If they all check out, the team genuinely has more capacity than the domain absorbs, and the right move is investing leverage rather than output.

## What Doesn't Change

Human accountability, maintainability, transparency, and judgment do not disappear. The craft shifts toward defining intent, shaping systems, building quality layers, and reviewing with discernment.

---

*Thanks to Nick Keuning, Jake Silas, Bryan Elkus, John Fisher, Nick Hazekamp, and Kealy Williams for feedback and contributions that shaped this work.*
