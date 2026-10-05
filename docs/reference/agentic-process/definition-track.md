---
source: agentic-process/www/definition-track.html
summary: "Describes the upstream definition track that keeps direction, decisions, knowledge, designs, and specifications ready for agentic implementation."
reference-kind: guide
draft: v0.2
last-updated: 2026-05-06
---

# The Definition Track

> Part of Atomic's Agentic Delivery Process — the upstream work that keeps agentic engineering fed.

*Draft v0.2 — Last updated 2026-05-06. Expect continued refinement as teams put it into practice.*

The definition track is where the team figures out what to build, makes that direction explicit enough for agents to execute against, and keeps it current as the project evolves. This is not documentation that trails the work; it is product development in the medium agents consume. The *specifications* (evergreen descriptions of how a part of the system or experience works; kept current with actual behavior — when the team wants a change, the spec is updated first and implementation brings the system into alignment), knowledge, and *context* (the accumulated knowledge — decisions, domain rules, examples, constraints, and rationale — that agents and team members need to do their work; agents start fresh each session, so context must be written down to be usable) that come out of this track become direct inputs to implementation, so clarity here has immediate delivery consequences.

This is where the judgment around delivering the right product lives: what information to gather, how much specification a piece of work needs, what to confirm with the client vs. pre-decide, and how to keep the pipeline flowing fast enough that implementation never stalls for lack of ready work.

> **Scope:** This document covers the **definition track** — the ongoing work of gathering information, turning it into trusted knowledge, and shaping specs that implementation can execute against. The Agentic Delivery Process overview introduces the full model; the companion [*Implementation Track*](implementation-track.md) covers how the team plans, builds, and reviews work. The definition track starts after **RDP** — the work that establishes the landscape, users, constraints, and problems well enough that the team can start outlining what to build and prototyping immediately.

## Starting Point

Definition work doesn't start from nothing. RDP ends with enough shared understanding — users, problems, constraints, a rough product backbone or feature map, often early design exploration or prototypes — that the team has a structural picture of the product to work from. That foundation is what turns "info gathering" from an open question into a directed activity: you're always gathering information *about something*, usually the feature area the team has committed to next.

If RDP is incomplete, compressed, or constrained by the client relationship, the definition track does not disappear. It becomes the ongoing mechanism for filling in the landscape while implementation proceeds carefully. The team should make the missing context visible, choose lighter or more confirmatory plays where appropriate, and avoid asking agents to guess at high-stakes intent.

What the team commits to next comes from the backlog — the sequenced, client-facing view of what's being built and in what order, derived from the backbone but distinct from it. The backlog is a living artifact: as definition work produces clarity, epics get added, re-sequenced, decomposed, or dropped; the team revises it in conversation with the client rather than treating it as a fixed contract. See [Where the Work Lives — The Backlog](#where-the-work-lives) for how it's structured and maintained over the life of the engagement.

## How Definition Work Operates

Once a feature area is in hand, the work follows a pipeline with three stages: gather information, synthesize it into shared knowledge, and update the specs that implementation executes against.

**Diagram — Definition pipeline (vertical flow with feedback loop):**

- Stage 1 (teal box): **Info Gathering** — "Save raw info to shared repo".
- Handoff seam (dashed line) labeled "handoff ↓".
- Arrow down to Stage 2 (amber box): **Synthesis** — "Compare to KB · update KB".
- Handoff seam labeled "handoff ↓".
- Arrow down to Stage 3 (blue box): **Spec Authoring** — "Create or revise specs".
- Arrow down to a decision diamond (dark): **Ready for impl?**
- "yes" (teal arrow) flows down to a teal-bordered box: **Plan for Cycle**.
- "no" (gray dashed arrow) loops right and up back to **Info Gathering**, labeled "need more context".

Pipeline prose:

- **Info gathering** — Identify what context is needed and get it: client sessions, design exploration, domain research, prototyping, product evaluation. Raw material (transcripts, artifacts, notes) gets saved to the shared repo so anyone can pick it up.
- **Synthesis** — Compare raw material against the knowledge base and update it. A client session might surface new requirements, confirm assumptions, or contradict what the team thought it knew. Synthesis reconciles all of that, oriented around one input at a time.
- **Spec updates** — Create or revise specs to reflect what synthesis revealed. This is where work fans out: one synthesis pass can produce multiple independent spec tasks, each tracked and owned separately.
- After spec updates: is this ready for implementation? If yes, plan it into a cycle. If not, loop back for more info gathering.

Each stage is a natural stopping point where work can be handed off, or one person can rip through all three in a single pass. A DL captures a transcript in a client meeting on Tuesday; a tech lead synthesizes it into KB updates on Wednesday; the designer updates the interaction spec on Thursday. The pace is set by the implementation track: if the pipeline can't produce ready specs fast enough to stay one *cycle* (one pass through the planning → implementation → review loop, running twice per week) ahead, engineering stalls. (Under-fed engineering is the single loudest signal that the pipeline is broken — see [Knowing When It's Breaking](#knowing-when-its-breaking).)

At any given moment the team is making a judgment call: gather more information or process what we have? Sometimes **demand-driven**, because the implementation track needs specs for next cycle, so the team synthesizes what it has and pushes specs toward ready. Sometimes **supply-driven**, because a client touchpoint is coming up and the team prepares to gather information regardless of immediate downstream demand. Info gathering draws from both external sources (client sessions, user research, domain experts) and internal generation (design exploration, technical spikes, prototyping).

### Info Gathering Activities

| Activity | Purpose |
| --- | --- |
| Client sessions | Close known gaps, get reactions, unblock decisions |
| Design exploration | Figma prototypes, interaction models, design system work |
| Ideation & expansion | Expand sparse client input into testable direction |
| Technical spikes & specs | Developer-driven investigation and specification of architecture, data models, non-functional requirements |
| Domain research | Regulations, integrations, competitive landscape, user context |
| Prototyping | Working software to test assumptions — routine now that building is cheap |
| Product evaluation | Hands-on use of the built product to surface gaps and divergence from intent |
| Friction surfacing | Identifying where tooling or automation could amplify team performance |

Not all information is equally trustworthy. Information validated by the authority (client, PO, user research, production data) can be committed to specs with confidence. Information generated by the team (design explorations, spikes, judgment calls) is a trade-off bet: the team decides how much validation to invest in before committing. Spec updates include that judgment — what's ready to commit (ready for literal agent execution, not just clear enough for a developer to interpret) and what still needs validation. Validation can happen upstream of implementation (confirm with the client, gather more information) or downstream of it (build a first pass, put it in front of the client, and let the reaction to something concrete drive the next revision). Blocking progress is one option; the more common move is to make a reasonable bet, build against it, and validate on the far side — working software usually produces sharper feedback than another round of talking about it.

> **Tip — Two modes, different tasks.** Definition work uses agents in two modes. *Agent delegation* — handing off well-defined work and reviewing the output — fits rote tasks like turning a transcript into meeting notes, formatting a spec, or drafting from clear source material. *Agent partnership* — the agent asks clarifying questions, surfaces contradictions across sources, brainstorms framings, and drafts revisions for you to edit — fits work where judgment, framing, or tradeoffs matter: shaping a tricky spec, reconciling contradictions, deciding what a piece of client feedback implies. Capture and formatting can usually be delegated; synthesis and spec authoring reward partnership. The [Agent Use Playbook](strategic-agent-use.md) gives more reusable moves for research, active interviewing, diagramming, adversarial review, documentation, and teaching.

## Who Does the Definition Work

The pipeline above is the work. The question is who does it — and the answer is the same team that does implementation, in a different hat. Agents have narrow intent: they execute what's written. People have broad intent: they understand the users, the business, and the tradeoffs. Whoever picks up a definition hat is bridging that gap.

This is development work, just not always code: specs, examples, decisions, constraints, and validated context become direct inputs to the agentic engineering process. That makes definition work more time-sensitive, more precise, and higher-stakes than traditional upstream documentation. The boundary between maker work and developer work gets more porous, even though people's strengths and primary roles still matter.

### Hats, Not Jobs

The work on every project requires six hats. These are activities people pick up and put down, not fixed jobs assigned at the start of the engagement:

- **Info gathering** — acquiring new context through client sessions, user research, design exploration, domain research, technical spikes, or prototyping.
- **Synthesis** — reconciling raw material against the knowledge base and updating shared understanding.
- **Spec authoring** — writing and revising execution-ready specifications.
- **Evaluating** — hands-on use of the built product to surface gaps and divergence from intent.
- **Implementation** — planning, executing, and reviewing cycle work. (See [The Implementation Track](implementation-track.md).)
- **Client stewardship** — managing the business relationship: expectations, account health, engagement trajectory.

"Definition track" and "implementation track" are categories of work, not rigid team boundaries. Anyone can wear any hat. A tech lead doing a spike might gather information, synthesize it, and update a spec in a single pass. A DL on a technically complex project might focus on client info gathering and hand off synthesis to someone closer to the domain. A designer might carry a feature from user research through UX design all the way to a finished spec.

Some hats have natural affinities. The DL takes point on client stewardship and client-facing info gathering. The designer leads info gathering through user research and design activities: product backbones, journey mapping, personas, UX design. The tech lead specifies data models, API contracts, and architectural decisions. Spec authoring itself usually lands with whoever owns the definition work for that area — often the DL or designer, sometimes a senior dev, frequently a pair — rather than a separate "spec writer" role. But these are starting points, not walls. Most people wear multiple hats in any given week.

The goal is right-sizing capacity so the whole team keeps moving. When the implementation track stalls for lack of ready specs, the answer is moving more makers into definition hats. When definition is running ahead, makers can shift toward implementation. The tracks show where capacity is needed, not who belongs where.

> **Caution — Definition capacity has to be real.** If delivery, design, QA, or tech-lead capacity is too thin, the process does not magically work. The team has three honest options: move more makers into definition hats, reduce implementation appetite until the pipeline catches up, or have a staffing/sales/client conversation about the shape of the engagement. What does not work is letting agents fill product, design, data, or architectural gaps by guessing.

## Where the Work Lives

The team's work lives at two levels. The **backlog** is the strategic layer: what are we building and in what order. Underneath that, three tactical surfaces support the day-to-day work: a **work board** that tracks near-term tasks across both tracks, a **knowledge base** where shared context accumulates, and **specifications** that describe what to build in enough detail for agents to execute.

**Diagram — Strategic / Tactical surfaces:**

- STRATEGIC layer (full-width amber box): **Backlog** — "feature-level priorities — what to build, in what order".
- TACTICAL layer (three side-by-side boxes):
  - **Work Board** (gray) — "near-term tasks across both tracks".
  - **Knowledge Base** (purple) — "accumulated shared context".
  - **Specifications** (blue) — "execution-ready detail".

### The Backlog

The backlog is the feature-level view of the project: "authorization system," "onboarding flow," "admin dashboard." Items live at the epic level, higher than individual tasks. Most epics are too big to land in a single cycle, so they decompose into phases that map to incremental delivery — a first increment that goes out, then a next, and so on — each shaped and built across one or more cycles. The detail lives in specs, not the backlog item.

This makes the backlog a surface clients can actually work in with the team, because it's at the level they think about. The team and client use it to prioritize what gets worked on next, sequence delivery, and maintain visibility into what's coming when.

### The Work Board

The work board is the near-term tactical view: what's in progress, what's blocked, and what's coming next across both tracks. It's where the team coordinates day-to-day, separate from the strategic backlog. It can be anything — a board in Linear or Jira, a Kanban board, stickies on a whiteboard. It doesn't need to be the same tool as the backlog, and it doesn't need to be client-facing.

We recommend a small set of task types that mirror the pipeline stages:

- **Info gathering** — scoped to the activity: a client meeting, a research session, a spike. One session often touches multiple specs.
- **Synthesis** — scoped to the input being processed: a transcript, a set of session notes. The goal is to get raw material reconciled against the KB.
- **Spec authoring** — scoped to a delta of a specific specification. One synthesis pass can produce multiple independent spec tasks.
- **Implement** — scoped to an *Effort* (the execution artifact for a work slice: an agent-ready task assignment linked to specs, kept current as a running log of plans, checkpoints, decisions, and follow-ups), a work slice tied to one or more specs that will be delivered in a single cycle.

A task flows through the pipeline: an info gathering task ("interview client about permissions model") becomes a synthesis task ("update authorization KB from interview transcript"), which fans out into one or more spec authoring tasks ("revise authorization spec," "add session-expiry spec"), each of which eventually feeds an implementation Effort.

### The Knowledge Base

The KB is the shared context that both tracks draw from. Agents reference it the way a teammate would, to understand the domain, the decisions already made, and the constraints that shape the work. Writing things down, sharing them out, and keeping them current is what makes information move fast enough to keep up with implementation. The whole team contributes: definition work adds business, user, and functional knowledge; implementation work adds technical, architectural, and operational knowledge.

Our default is to keep everything in the code repository (KB, specs, technical docs, agent guidance, tooling) because that's what agents can access directly and what travels with the project. Other arrangements can work, but the principle is the same: context that agents can't reach doesn't exist for them. Depth scales with the engagement, but the four ingredients below are the floor.

**Minimum Viable KB:**

- **Meeting notes** — Synthesized, not raw.
- **Decisions** — Captured with rationale.
- **Specifications** — Active and current.
- **Open questions** — Tracked explicitly.

> **Caution — When the KB is stale or disorganized, the system degrades silently.** Every client meeting, design decision, and review finding should move into the system quickly enough that the next cycle can trust it. But volume alone doesn't help. A huge folder of unsynthesized transcripts won't compound. The KB needs to be structured so that agents and team members can find what's relevant when it matters. When that friction repeats, treat it as [Compounding Work](compounding-work.md): a signal to improve synthesis workflows, spec health, decision tracking, or retrieval paths.

### Specifications

A spec is an evergreen description of how a part of the system or experience works: its behavior, acceptance criteria, constraints, design reference, and open questions. Specs are kept current with how the system actually behaves. When the team wants to make a change, the spec is updated first, and implementation is the work of bringing the system into alignment with the revised spec. Each spec links to the KB content that gives it context; the spec plus its linked material is what the implementation track plans and builds against.

Several kinds of spec need to stay current, covering different facets of the system:

- **Product specs** — desired behavior from the user or business perspective: what the feature does, how interactions work, what outcomes matter.
- **Experience specs** — cross-cutting UX guidance that applies across features: design systems, interaction patterns, accessibility rules, component-level behavioral expectations. A visual mockup is a strong spec input, and in agentic delivery it has more direct implementation leverage than before. But visual intent still needs behavioral, edge-case, and state context that agents can execute.
- **Technical specs** — how the system implements it: data model constraints, API contracts, architectural decisions, *non-functional requirements* (Non-Functional Requirements: qualities the system must exhibit rather than features it must have — performance, security, reliability, accessibility, observability, maintainability, and similar cross-cutting concerns). These cross-cut features and are typically owned by engineers, especially tech leads, who spend time in the definition track.
- **Process & operations specs** — how the team works and how the system is run: contributor workflows, quality gates, testing policy, observability, deployment procedures. Often the most reusable across engagements. The [Agent Use Playbook](strategic-agent-use.md) helps identify useful agent modes and repeated collaboration patterns; the [Agent Harness Playbook](agent-harness-playbook.md) shows how those patterns become agent instructions, skills, review agents, scripts, repo knowledge, and safety gates.

Product specs without technical specs leave gaps that agents will fill with wrong assumptions. Technical specs without product specs leave intent unclear. Definition and engineering work collaborate to produce both. Neither owns the full picture alone.

## The Operating Rhythm

The team's *cycle* (one pass through the planning → implementation → review loop, running twice per week) is the skeleton: Planning → Implementation → Review, twice per week. Everyone participates in planning and review as a full team; definition-hatted work happens in the space between those ceremonies. At any given moment, the team is balancing definition effort across three horizons:

- **Current-cycle support** — Answering questions, closing gaps surfaced during implementation, reconciling findings at review.
- **Next-cycle readying** — Getting specs to "ready" so the next planning has a full load of committed work.
- **Following-cycle shaping** — Gathering info and doing early synthesis for work two or more cycles out — keeping the pipeline full.

If all three horizons aren't getting attention, the system breaks. All current-cycle support means next planning is underfed. All forward shaping means implementation questions go unanswered. The definition sync (below) is where the team checks this balance.

### A Week in the Life

Implementation has a structured rhythm — Planning → Implementation → Review, repeating twice per week. Definition work fills the space around and between those ceremonies. The whole team is in planning (presenting specs, scoping) and review (demos, reconciling findings); outside those blocks, people in definition hats are doing info gathering, synthesis, and spec work while developers are in implementation.

**Diagram — Weekly cadence (Mon–Fri, AM/PM grid):**

Implementation track row (top, light-gray background):

- MON: AM Planning (red), PM Implementation (blue).
- TUE: AM Implementation (blue), PM Implementation (blue).
- WED: AM Review (teal), PM Planning (red).
- THU: AM Implementation (blue), PM Implementation (blue).
- FRI: AM Implementation (blue), PM Review (teal).
- Cycle 1 bracket spans Mon AM Planning through Wed AM Review.
- Cycle 2 bracket spans Wed PM Planning through Fri PM Review.

Definition track row (bottom, light-amber background):

- MON: AM Planning (red), PM Definition (amber).
- TUE: AM Definition, PM Definition.
- WED: AM Review (teal), PM Planning (red).
- THU: AM Definition, PM Definition.
- FRI: AM Definition, PM Review (teal).

Legend: Planning (red) · Implementation (blue) · Review (teal) · Definition work (amber).

*Caption: Planning and review are whole-team ceremonies; definition work fills the space around them. The mix of definition activities varies day to day — build your meetings accordingly.*

Within those definition blocks, the team is doing a mix of activities that shifts based on what's needed. In a typical week, expect something like:

| Activity | Approx. share |
| --- | --- |
| Info Gathering | ~30% |
| Synthesis & Spec Authoring | ~30% |
| Shared Ceremonies | ~20% |
| Evaluating | ~20% |

*Approximate — varies by project, play, and phase of the engagement.*

### Touchpoints

These are our default recommendations. Every team will adapt the specific touchpoints, cadence, and format to fit their project and client relationship.

| Touchpoint | When | What happens on the definition side |
| --- | --- | --- |
| Planning | Start of each cycle | Present candidate specs. Facilitate and contribute to pressure-testing them with the team. Answer questions, provide product guidance as developers scope and plan their implementation approach. |
| Review | End of each cycle | Evaluate the built product hands-on against intent. Identify where output diverged from specs. Surface gaps, frictions, and follow-up work that feeds the next round of info gathering or synthesis. |
| Client working session | ~2x/week | Close known gaps, get reactions, unblock decisions. May include client demos when useful — facilitated by the DL, with team members showing their own work. Always paired with a rally immediately afterward. |
| Rally | Immediately after each client session | Short team huddle to synthesize what was just said, while context is fresh: capture decisions, log open questions, and queue the KB and spec updates that need to happen before next planning. The value is in the immediacy — a day later, nuance is gone and synthesis costs more. |
| Definition sync | Daily, 15-20 min | Steering check, not status report. What gaps are open, what signal is in flight, what's closest to ready, which horizon is getting neglected. Rebalance if needed. |
| Spec-health review | Weekly, 30-45 min | Walk the active specs. Flag anything stale, contradictory, or missing coverage. Prioritize what to fix before next planning. |

## Choosing How Much Definition to Do — Plays

Not every piece of work needs the same definition investment. The team needs to match its investment to the situation. When the neighborhood of success is broad, move faster by generating more direction internally and using working artifacts to create better conversations, rather than waiting for every decision to be validated before building. When the stakes are high and hard to reverse, invest more upfront. Three dimensions drive that choice:

- **Target precision** — How narrow is the acceptable outcome space?
- **Information availability** — Can the team get the signal it needs before building?
- **Reversibility** — How costly is it to change direction after building — beyond code rewrite cost?

These dimensions determine which play fits. Narrow target, good information, hard to reverse: invest heavily in specification before building. Wide target, easy to reverse: build with less upfront definition and learn faster through use. They won't line up perfectly on every piece of work, so let the most constraining facet drive the choice.

**Diagram — Play spectrum (left = MORE DEFINITION WORK, right = LESS):**

- **Research-First Shaping** (red, leftmost) — Investigate first, then specify. Promote findings.
- **High-Precision Spec** (orange) — Full specification before build. Primary signal required.
- **Guided Increment** (teal, marked **DEFAULT PLAY**) — Build a structured pass to create better signal.
- **Ship and Refine** (blue) — Minimal shaping, ship, learn from use.
- **Show-and-React** (purple, rightmost) — Cheap bursts to force reactions that create signal.

The default bias moves toward Guided Increment when risk allows. Teams should actively challenge high-precision specification by habit: extra upfront definition has to earn its cost when a short, structured build-and-react loop would produce better signal faster. High-precision specification remains correct when the outcome space is narrow, hard to reverse, or expensive to misunderstand.

Do just enough definition to build something safe to learn from, put it in front of the client or product authority, and revise from the reaction. The play applies per cycle, not per feature lifetime. Each pass through the pipeline is a fresh classification.

Show-and-React sits outside the normal matrix. It is the fallback when decision flow is blocked badly enough that the usual factors stop being predictive, not the standard answer for ordinary low-information work.

### Play Cards

Filter dimensions surfaced by the original interactive chooser: **Precision** (Wide / Medium / Narrow), **Info** (Low / Medium / High), **Reversible** (Easy / Hard). The cards:

- **Research-First Shaping** — *Narrow target + missing knowledge.* The bullseye is small but the team can't aim yet. Research-heavy: legacy review, data analysis, user interviews, domain expert sessions. Narrow spikes validate findings before broader implementation.
- **High-Precision Spec** — *Narrow target + high info + hard to reverse.* Full research, design, and specification before building. Heavy definition investment: domain modeling, UI design, detailed specs, client review cycles. Still correct when conditions warrant it — just no longer the default.
- **Guided Increment** — *Medium target, or uncertain info, with room to revise.* The team can't fully specify the outcome, but building blindly is risky. A short, structured build pass creates a coherent working increment that generates better conversations than mockups or written specs alone. Define guardrails and constraints; expect to adapt, refine, and enrich in follow-up cycles.
- **Ship and Refine** — *Wide target + easy to reverse.* Any reasonable implementation will satisfy. Minimal definition work: capture key requirements, don't over-specify. Build, ship, refine from real feedback.
- **Show-and-React** — *Decision flow is blocked and the work is safe to unwind.* This is the exception case when the normal matrix stops being predictive because the team simply cannot get direction to stabilize. Short, bounded execution bursts force reactions and generate the signal that normal info gathering can't. Light definition work: bound the experiment, expect to throw work away.

### Scenario Map (Precision × Information × Reversibility → Best-Fit Play)

| Precision | Info | Reversibility | Best-fit play | Ruled out |
| --- | --- | --- | --- | --- |
| Wide | Low | Easy | Ship and Refine | High-Precision Spec, Research-First Shaping |
| Wide | Medium | Easy | Ship and Refine | High-Precision Spec, Research-First Shaping, Show-and-React, Guided Increment |
| Wide | High | Easy | Ship and Refine | High-Precision Spec, Research-First Shaping, Show-and-React, Guided Increment |
| Wide | Low | Hard | Guided Increment | High-Precision Spec, Research-First Shaping, Ship and Refine, Show-and-React |
| Wide | Medium | Hard | Guided Increment | High-Precision Spec, Research-First Shaping, Ship and Refine, Show-and-React |
| Wide | High | Hard | Guided Increment | High-Precision Spec, Research-First Shaping, Ship and Refine, Show-and-React |
| Medium | Low | Easy | Guided Increment | High-Precision Spec, Ship and Refine, Show-and-React |
| Medium | Medium | Easy | Guided Increment | Research-First Shaping, Ship and Refine, Show-and-React |
| Medium | High | Easy | Guided Increment | Research-First Shaping, Ship and Refine, Show-and-React |
| Medium | Low | Hard | Research-First Shaping | High-Precision Spec, Ship and Refine, Show-and-React |
| Medium | Medium | Hard | Guided Increment | Research-First Shaping, Ship and Refine, Show-and-React |
| Medium | High | Hard | Guided Increment | Research-First Shaping, Ship and Refine, Show-and-React |
| Narrow | Low | Easy | Research-First Shaping | High-Precision Spec, Ship and Refine, Show-and-React, Guided Increment |
| Narrow | Medium | Easy | Guided Increment | Research-First Shaping, Ship and Refine, Show-and-React |
| Narrow | High | Easy | Guided Increment | Research-First Shaping, Ship and Refine, Show-and-React |
| Narrow | Low | Hard | Research-First Shaping | High-Precision Spec, Ship and Refine, Show-and-React, Guided Increment |
| Narrow | Medium | Hard | High-Precision Spec | Research-First Shaping, Ship and Refine, Show-and-React |
| Narrow | High | Hard | High-Precision Spec | Research-First Shaping, Ship and Refine, Show-and-React |

Pre-decide and review is a cross-cutting tactic within any play: make the best bounded call, document the rationale, present for confirmation. Watch the confirmation backlog — unreviewed pre-decisions accumulate risk quietly, so batch them into client sessions before the pile grows.

### What This Looks Like by Project Shape

The matrix above is abstract; real projects have a center of gravity that pulls definition work toward particular plays. The table below grounds the framework in concrete examples — find the shape closest to your engagement and start there, then reclassify per cycle as the work reveals itself.

| Project shape | Where definition work concentrates | Play tendency |
| --- | --- | --- |
| Consumer-facing app | Branding, visual design, interaction patterns, accessibility, UX flows | High-Precision for visual design and brand; Guided Increment for application functionality |
| Rewrite / modernization | Mining source code for behavior, agent-assisted workflow walkthroughs, documenting behavior as assertions for automated testing | Research-First early; Guided Increment as understanding builds |
| Short-timeline build | Ruthless scoping, fast client confirmation on priorities, thin but directional specs | Ship and Refine, Show-and-React — learning by building |
| Generic technical domain | Success criteria and approach selection for well-understood problems (search, geolocation, caching, notifications) | Ship and Refine or Guided Increment — define criteria, not detailed specification |
| Complex data migration | Source and destination data models, mapping rules, edge-case enumeration, validation criteria | Research-First dominates — you can't guess at mapping rules |
| Technical / headless system | Event storming, event modeling, SLAs, API contracts, integration behavior | Research-First and High-Precision — low reversibility on contracts and data models |
| Ongoing maintenance | Bug reports, monitoring data, security vulnerabilities, dependency updates, keeping specs current with the running system | High-Precision for high-stakes changes; Ship and Refine for routine fixes and updates |

Definition work exists on every project — it just looks different. If a team does not see itself in the process, the next move is to translate the model to its project shape, constraints, and client reality, not quietly fall back to treating definition as optional.

## Estimation and Projection

Estimation happens at the backlog level: epics, not tasks. The team and client size epics relative to each other using points on the Fibonacci scale. This gives the macro picture: a total budget of points across the backlog, a way to compare the relative investment required across features, and a basis for answering the question that matters most: will we finish on time?

An epic typically starts as a single large estimate — a block of time the team expects to invest in that area. As definition work surfaces detail, most epics decompose into phases that can be sequenced across cycles and interleaved with other priorities. Each phase burns part of the epic's budget as it delivers, giving velocity signal well before the epic closes.

Epics act as **micro-budgets**. When work on an epic is pulled into a cycle, points are drawn from the epic to represent the combined cost of definition and implementation work for that cycle's delivery. A 20-point epic might burn 6 points of definition work before implementation even starts — the info gathering, synthesis, and spec authoring required to get it ready. Making definition cost visible in the budget is important: it's real work that takes real time, and it shapes how much the team can deliver.

**Diagram — Epic as Micro-Budget (example: "Authorization System", 21 pts total):**

A horizontal stacked bar shows the epic's budget consumed across cycles, alternating definition (amber) and implementation (blue) segments:

- **C1 (Week 1):** ~3 pts definition + ~2 pts implementation.
- **C2 (Week 1):** ~2 pts definition + ~3 pts implementation.
- **C3 (Week 2):** ~1 pt definition + ~4 pts implementation.
- **C4 (Week 2):** ~5 pts implementation only.
- **C5 (Week 3):** ~1 pt definition + ~3 pts implementation.
- Remaining: 7 pts.

Legend:

- Amber square — Definition work (info gathering, synthesis, spec authoring).
- Blue square — Implementation work (planning, implementation, review).
- Light gray bordered square — Remaining budget.

Velocity annotation: *Week 1: 6 pts burned · Week 2: 6 pts burned · Week 3: 2 pts burned (so far). Velocity trend → ~6 pts/week → 7 pts remaining ≈ 1 more week on this epic.*

Velocity is measured and reported at a cadence that fits the client relationship — typically weekly or bi-weekly, not tied to the *cycle* (one pass through the planning → implementation → review loop, running twice per week) cadence. Cycle-to-cycle throughput is too noisy to be useful for projection. Over a few weeks, patterns emerge: how fast the team is burning through epic budgets, whether the pipeline is flowing or stalling, whether the overall trajectory hits the timeline.

When velocity is off track, the team has a lever that didn't exist before: **play flexibility**. In addition to the traditional options of cutting scope or spending more, the team can adjust how much definition investment a feature gets, accepting less client input fidelity, building with fewer validation rounds, or choosing a lighter play. This doesn't eliminate trade-off conversations with the client, but it adds a third axis to them.

> **Caution — Throughput varies — plan for it.** Agent friction, knowledge availability, domain complexity, client decision latency, and the team's accumulated context all shape how fast work moves. A team with a mature KB and well-aligned agents on a familiar domain will move at a fundamentally different pace than a team ramping up on a novel problem space. Expect velocity to build over the course of the engagement as the KB matures and agent alignment improves. Ground timeline conversations in observed velocity trends, not early-project snapshots.

## Knowing When It's Breaking

The definition process fails in recognizable patterns. The signals below are ordered from most fundamental to most downstream — work the list top-down. Later signals are often effects of earlier ones, and fixing an earlier signal typically resolves several of the ones below it. If engineering is under-fed at planning (signal 1), don't spend time on signal 7 until signal 1 is resolved.

<a id="signal-starved"></a>
### 1. Engineering doesn't have enough ready work to fill their cycles

*Category: Fundamental.*

The entire definition process exists to prevent this. If engineering is consistently underloaded at planning — not enough specs ready, not enough confidence to commit — something upstream has broken down. Everything else in this list is secondary until this is resolved.

**What You're Seeing.** Planning produces a partial load. Engineers finish committed work mid-cycle and pull in lower-priority or unspecified items. The team starts doing discovery work during execution. Throughput is lower than capacity because the constraint is upstream, not downstream.

**What To Do.** This is a symptom, not a root cause. Diagnose the upstream failure: capacity misallocation toward implementation (see [#3 all reactive](#3-all-definition-work-is-responding-to-the-current-cycle)), synthesis stalling before commit ([#4 KB not leveraging](#4-the-kb-isnt-building-leverage)), or the team avoiding decisions that require client input. Fix the cause, not the symptom.

<a id="signal-planning"></a>
### 2. Planning generates more questions than commitments

*Category: Fundamental.*

Specs presented at planning aren't ready. The team discovers gaps that should have been resolved in the definition loop. Planning becomes a discovery session instead of a commitment checkpoint.

**What You're Seeing.** Engineers ask questions the spec should answer. Whoever shepherded the spec says "I'll find out" or "we haven't decided that yet." Multiple specs get deferred to next cycle. The team leaves planning without a full load of committed work.

**What To Do.** Say "not ready yet" — don't push half-formed specs into implementation. Find where the pipeline stalled. This is often downstream of [#3 (all reactive)](#3-all-definition-work-is-responding-to-the-current-cycle): definition capacity is burning on current-cycle fires with nothing being shaped two cycles out. Other common causes: rallies being skipped (synthesis gap), or the team avoiding hard decisions that require client input.

<a id="signal-reactive"></a>
### 3. All definition work is responding to the current cycle

*Category: Capacity.*

No one is shaping next cycle's work. The board shows everything "in progress" or "blocked," nothing in "shaping" or "upcoming." The three-horizon balance has collapsed into one.

**What You're Seeing.** Everyone wearing a definition hat spends all their time answering developer questions, handling gaps surfaced during implementation, and reconciling review findings. There's no time left for info gathering or synthesis aimed at future cycles. Next planning will be underfed.

**What To Do.** The team is misallocating capacity — too many people in implementation hats, not enough in definition. Pull a developer into definition work to help with the bottleneck, shift the DL or designer's time toward next-cycle readying, or reduce engineering appetite until definition can get ahead again. This usually shows up together with [#1 (engineering not fed)](#1-engineering-doesnt-have-enough-ready-work-to-fill-their-cycles) — they're the same capacity problem viewed from opposite sides.

<a id="signal-kb-leverage"></a>
### 4. The KB isn't building leverage

*Category: Knowledge Infrastructure.*

The knowledge base exists but isn't compounding the team's ability to spec work or align agents. Key decisions either aren't being captured, or they're captured but never surface when they're relevant. The problem is structure and agent guidance, not volume.

**What You're Seeing.** Agents lack the context to stay aligned with the product and technical landscape. The team re-explains the same material repeatedly. Important decisions exist somewhere but don't surface when needed. Specs are written from scratch rather than building on accumulated understanding. The KB is a filing cabinet, not a working system.

**What To Do.** Capture the right material — key decisions with rationale, domain constraints, behavioral expectations, not just raw session transcripts. Make it findable when it matters: agent guidance, linked references in specs, structured context that surfaces automatically. Invest in the tooling and structure that makes the KB earn its maintenance cost.

<a id="signal-bypass"></a>
### 5. Developers bypass specs and ask the DL directly

*Category: Definition-to-Engineering Interface.*

The knowledge base has become write-only. People contribute to it but don't consult it. Developers find it faster to ask the DL a question than to locate and read the relevant spec.

**What You're Seeing.** The DL is a constant interrupt source for the dev team. Specs exist but developers don't trust them to be current, can't find the right one, or find them at the wrong level of detail. In agentic delivery this is especially damaging — the agents can't ask the DL, so they work from whatever is written down.

**What To Do.** Check [#4 (KB not leveraging)](#4-the-kb-isnt-building-leverage) first — bypass is usually a symptom of a weak or unfindable KB, not a habit problem. From there: fix the specs, not the habit. If specs aren't trusted, they're probably stale — run a spec-health review and commit to keeping them current. If they're not findable, restructure. If they're at the wrong level, adjust. The goal is specs that are faster to read than to ask about.

<a id="signal-reframing"></a>
### 6. Client reactions keep reframing what was specified

*Category: Client Interface.*

Demos or reviews consistently produce "that's not what I meant" rather than refinements. The specs are capturing the team's interpretation, not the client's actual intent. This is normal early on, but if it's still happening mid-project, info gathering isn't extracting real commitments.

**What You're Seeing.** Client says "looks good" in sessions but reacts differently when they see the built thing. The team interprets vague approval as commitment, then discovers the gap at demo time.

**What To Do.** Shift to a more confirmatory play. Extract specific decisions in sessions, not just reactions. Use prototypes or mockups to force concrete feedback before building. Surface the pattern to the client as a delivery risk. Adjacent to [#7 (pre-decided calls stacking up)](#7-pre-decided-calls-stack-up-unreviewed) — same client-interface problem, opposite failure mode.

<a id="signal-predecide"></a>
### 7. Pre-decided calls stack up unreviewed

*Category: Client Interface.*

The team is making good autonomous calls — pre-decide-and-review is a legitimate tactic — but the *review* half is quietly dropping off. Unconfirmed decisions accumulate.

**What You're Seeing.** Specs are full of team-generated decisions with documented rationale, but the client hasn't actually validated most of them. The team feels confident; the risk is invisible until a significant call turns out wrong.

**What To Do.** This isn't a "stop pre-deciding" signal — pre-deciding is a valid tactic. It's a "protect the review cadence" signal. Make the accumulated unconfirmed decisions visible and establish a confirmation rhythm — batch them into client sessions rather than letting them pile up. Adjacent to [#6 (client keeps reframing)](#6-client-reactions-keep-reframing-what-was-specified) — both are client-interface failures in opposite directions. Accept rework as the explicit cost if confirmation can't happen faster.

<a id="signal-play"></a>
### 8. The team has settled into one play for everything

*Category: Strategic.*

The team found a groove — usually Guided Increment or Ship and Refine — and stopped classifying. Everything gets the same treatment regardless of target precision, information availability, or reversibility.

**What You're Seeing.** The team doesn't discuss which play to use because the answer is always the same. This works until it hits something with low reversibility — an integration contract, a data model decision, a UX pattern that anchors client expectations — and the rework cost is real.

**What To Do.** Reintroduce classification as a deliberate step at planning. Ask "what play fits this work?" for each feature area entering the cycle. The three dimensions (target precision, information availability, reversibility) should produce different answers for different work.

<a id="signal-velocity"></a>
### 9. Velocity isn't stabilizing

*Category: Systemic.*

The team has been running for several cycles but throughput isn't improving — or it's erratic. Velocity should build as the KB matures, agent alignment improves, and the team finds its rhythm. If that's not happening, something structural is off.

**What You're Seeing.** Work regularly takes more than one cycle end-to-end. The team doesn't feel faster than it did three weeks ago. Items that should be routine still require heavy definition investment. Compounding investments aren't paying off.

**What To Do.** This is a systemic signal — work through the earlier ones first to localize the cause. Common culprits: work items don't fit inside a single cycle (break them down), the play mix is creating precision blockers (see [#8 one play for everything](#8-the-team-has-settled-into-one-play-for-everything)), or compounding investments haven't started paying off (see [#4 KB not leveraging](#4-the-kb-isnt-building-leverage)). If earlier signals all check out, recalibrate how much specification each piece of work actually needs.

---

*Companion relationship: The Implementation Track explains how the team executes short agent-assisted cycles. The Definition Track explains how those cycles stay fed with explicit, current, trustworthy direction.*

*Thanks to Bryan Elkus, Meg Kretz, Nick Keuning, and Sarah Brockett for contributions that shaped this work.*
