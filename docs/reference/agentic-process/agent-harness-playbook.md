---
source: agentic-process/www/agent-harness-playbook.html
summary: "Field guide for choosing durable agent-harness surfaces, including instructions, skills, scripts, review agents, repository knowledge, observability, and gates."
reference-kind: guide
draft: v0.2
last-updated: 2026-05-06
---

# Agent Harness Playbook

> Choosing where durable agent support should live: instructions, skills, scripts, review agents, repo knowledge, observability, and gates.

*Draft v0.2 — Last updated 2026-05-06. Expect continued refinement as teams put it into practice.*

> **Note:** Read this after [Compounding Work](compounding-work.md). Compounding Work tells you when to improve the system. [Agent Use Playbook](strategic-agent-use.md) helps name the collaboration patterns worth repeating. This playbook tells you where the improvement should live.

> **Boundary with Compounding Work:** This playbook is not the whole improvement loop. It starts after the team has noticed recurring friction and decided the missing support belongs in the agent harness. Use it to choose the smallest durable surface for that support; use Compounding Work to decide whether the investment is worth making and how the team will keep watching whether it helped.

Agentic delivery depends on more than choosing a strong model. Teams need a *delivery harness* (the operating environment that lets agents do useful work safely and repeatedly: repo context, specs, scripts, validation, observability, review agents, workflows, and safety boundaries): the instructions, workflows, scripts, knowledge, review layers, and permission boundaries that let agents do useful work without a human constantly translating the project.

This is a field guide for configuring that harness in a real repo. Codex and Claude Code have different native surfaces, but the underlying delivery model should stay portable: use instructions for orientation, skills for reusable workflows, custom agents for narrow roles, scripts and hooks for deterministic checks, repo knowledge for durable context, and human gates for risk.

## What Harness Engineering Is

*Harness engineering* (the practice of making the delivery harness more agent-operable by improving context, tools, feedback loops, validation, structure, permissions, and boundaries) improves the environment around the model so future agents can work with less translation, stronger validation, clearer evidence, and safer boundaries. It is where project-specific delivery taste becomes operable.

The goal is not more process. The goal is less repeated human intervention. A good harness improvement lets the next agent start from a better system instead of relying on a longer prompt.

Keep the loop small: create the support, test whether it changes agent behavior, put it where future work will find it, and watch ordinary delivery signals to see whether it is helping or hurting.

**Diagram — Harness improvement loop:** a four-step cycle showing how friction becomes durable harness improvement.

- **Step 1 — Friction signal:** Repeated review feedback, unclear spec, weak evidence, manual setup, noisy failure, risky permission.
- **Step 2 — Surface decision:** Choose instructions, skill, agent, script, repo knowledge, or safety gate.
- **Step 3 — Durable improvement:** Make the next run start from a stronger system, not a better memory of this chat.
- **Step 4 — Evaluation:** Replay, audit, or fresh-agent test the improvement against real work.

## The Harness Surface Model

Every harness improvement should have a home. The surface matters because each one fails differently when misused: instructions become manuals, skills become fact dumps, agents become generic assistants, and scripts become mysterious if nobody can interpret their output.

> Choose the smallest surface that future agents will naturally encounter at the moment they need it.

| Failure shape | Default surface | Examples |
|---|---|---|
| Objective rule | Automate it | Test, lint, formatter, CI check, policy script |
| Repeated workflow | Make a skill | Validation, review, distillation, handoff, issue triage |
| Narrow judgment lens | Use a custom agent | Architecture reviewer, evidence reviewer, security reviewer |
| Durable fact or decision | Store repo knowledge | Spec, Effort, decision record, architecture note, KB entry |
| Orientation problem | Improve entry instructions | AGENTS.md, CLAUDE.md, project map, stop conditions |
| Risk boundary | Add a gate | Permission, approval, release boundary, safety spec |
| Noisy or slow tool | Wrap it | Focused script, compact failure summary, evidence collector |

### Surface Smells

| Smell | Likely correction |
|---|---|
| Entry instructions are becoming a manual | Shorten the front door; route to linked docs, skills, and scripts. |
| A skill contains static facts | Move facts to specs, Efforts, repo knowledge, or architecture docs. |
| A review agent gives generic advice | Add source of truth, exclusions, severity, and output discipline. |
| Script output is too noisy | Add failure-first summary output and a verbose mode for deep debugging. |
| A human gate blocks routine safe work | Narrow the gate to the actual risk boundary. |
| A custom agent edits outside its role | Restrict tools, role, source material, and expected output. |
| A context package is reused but not evaluated | Run a fresh-agent trial or lightweight replay against real work. |

## Minimum Viable Harness

A project does not need every customization surface on day one. It needs enough structure that a fresh agent can orient, work safely, validate, and explain what happened.

1. Short entry instructions for Codex and Claude Code.
2. A repo-local map of specs, Efforts, architecture guidance, validation, and safety docs.
3. Scripts for bootstrap, dev, focused tests, full validation, and smoke checks.
4. A PR evidence expectation with validation, risk notes, and known limitations.
5. A small skill set for validation, review, and distilling repeated friction.
6. Read-only review agents for spec fit, architecture fit, and evidence sufficiency.
7. A clear approval boundary for releases, data, credentials, external communication, and high-risk changes.

## Entry Instructions

Entry instructions are the front door. They should orient the agent and route it to deeper material. They should not become the project encyclopedia.

### Include

Project purpose, where work lives, how to run and validate, PR evidence, safety links, and when to stop.

### Exclude

Full architecture manuals, copied specs, long troubleshooting history, and personal preferences that are not team rules.

### Verify

Start a fresh session and ask how it would safely implement a small spec-driven change. It should mention specs, Efforts, validation, evidence, and safety.

```
# Agent Guide

## What this project is
Short product and domain summary. Link to the project overview.

## How to work safely
Link to the safety spec and approval-required operations.

## Work model
Specs define committed behavior. Efforts define implementation slices.

## How to run and validate
Bootstrap, dev, reset, focused tests, full validation, and smoke commands.

## Architecture and conventions
Short map plus links to deeper docs and examples.

## PR evidence
Linked Effort/specs, validation run, risk notes, limitations, follow-ups.

## Stop and ask when
Product intent, safety, validation, release risk, or architecture direction is unclear.
```

## Skills

Skills encode repeatable workflows that require judgment. They are progressive disclosure for process: the agent loads the procedure when the task calls for it instead of carrying every workflow in the top-level instructions.

Create a skill when a workflow repeats, has judgment, has clear inputs and outputs, and agents skip it without support. Do not create a skill when a script, lint, CI check, doc, or one-off instruction would handle the need more cleanly. If the repeated move is still unclear, use the [Agent Use Playbook](strategic-agent-use.md#mode-palette) to name the collaboration mode before turning it into a skill.

```
---
name: validate-change
description: Use when a code change is complete and needs focused validation, failure diagnosis, and PR evidence.
---

# Validate Change

## Inputs
- linked Effort
- linked product, experience, technical, or process spec
- changed files
- available validation commands
- known risk areas

## Procedure
1. Read the Effort and spec acceptance criteria.
2. Identify the narrowest useful validation for the changed behavior.
3. Run focused validation first.
4. Broaden validation when risk warrants it.
5. If validation fails, summarize the failure before repair.
6. Produce PR evidence.

## Stop when
Validation requires product, architecture, safety, or release judgment.
```

## Custom Agents and Review Agents

Custom agents are useful when a role is repeated, narrow, and benefits from a separate context window or restricted tool access. Review agents are especially valuable when they review against explicit sources of truth rather than general taste.

> **Design rule:** every reviewer needs a named source of truth, named exclusions, severity discipline, and actionable output. A reviewer without a source of truth becomes generic advice.

| Reviewer | Question | Source of truth |
|---|---|---|
| Spec compliance | Did we build what the product, experience, or technical spec says? | Linked specs and acceptance criteria |
| Architecture | Does the change fit the system shape and boundaries? | Architecture docs, examples, module boundaries |
| Test strategy | Is validation appropriate to risk? | Validation docs, changed files, risk notes |
| Security / data | Are auth, permissions, data exposure, and credentials safe? | Safety spec, security guidance, focused checks |
| Evidence | Can a reviewer understand the change without rediscovering everything? | Effort, PR evidence, validation output |

### Review Severity Model

| Severity | Meaning | Default action |
|---|---|---|
| P0 | Must block merge | Fix before merge |
| P1 | Usually blocks unless explicitly waived | Fix or document waiver |
| P2 | Non-blocking improvement | Address if cheap or file follow-up |
| P3 | Useful only as reusable learning | Usually omit or capture as future improvement |

```
name = "architecture-reviewer"
description = "Reviews changes against documented architecture rules and module boundaries."
developer_instructions = """
Review against:
- docs/architecture/overview.md
- docs/architecture/module-boundaries.md
- linked Effort
- linked specs
- changed files

Focus on boundary violations, duplicate abstractions, inconsistent package
structure, missing standard primitives, and changes that make future agent
work harder.

Ignore generic style preferences and speculative refactors unless they reflect
a documented convention.

Output:
1. Summary judgment: pass / pass with follow-ups / block.
2. Blocking issues first, with file references.
3. Non-blocking follow-ups separately.
4. Suggested harness improvements for repeated patterns.
"""
```

## Scripts, CI, and Agent-Legible Tooling

Some behavior should not depend on model judgment. If a rule is objective, repeatable, cheap to check, and likely to recur, put it in automation. Make the output compact enough that agents can run the command, understand the result, and choose the next safe action without a human reading 2,000 lines of logs.

| Need | Prefer | Why |
|---|---|---|
| Formatting and style | Formatter, lint, CI | Model judgment adds no value to objective rules |
| Validation evidence | Focused scripts, smoke scripts, PR evidence collectors | Agents need compact commands and reviewable output |
| Noisy failures | Failure summary scripts or post-command hooks | A short, shaped failure beats 2,000 lines of logs |
| Protected surfaces | Hooks, approvals, CI checks | High-risk paths need visible friction |
| Generated artifacts | Scripts or CI checks | Generated output should not depend on memory |

Common script set:

- `scripts/bootstrap`
- `scripts/dev`
- `scripts/reset`
- `scripts/test-focused`
- `scripts/validate`
- `scripts/smoke`
- `scripts/summarize-failure`
- `scripts/collect-evidence`

## Runtime Evidence and Observability

Runtime evidence belongs in the harness when behavior cannot be proven from code and tests alone. If runtime behavior matters, the harness should let a future agent or reviewer inspect it without waiting on the one engineer who knows where the logs live.

| Surface | What it gives the agent |
|---|---|
| Smoke paths | Small flows that prove important runtime behavior still works. |
| Log queries | Safe commands or links for recent errors, warnings, and request traces. |
| Metrics and dashboards | Named runtime indicators with enough explanation to interpret them. |
| Trace lookup | How to connect UI, API, background work, and source code when a flow fails. |
| Test accounts and seed data | Known-safe data that makes important behavior reproducible. |
| Escalation boundary | When runtime evidence needs a human, release owner, or client decision. |

## Repo Knowledge and Context Budgeting

Agents need project knowledge that is retrievable at the point of work. Keep entry instructions short, link to deeper docs, and store running state in files rather than only in conversation history.

Use specs for committed behavior, Efforts for current work slices, architecture docs for system shape, decision records for settled tradeoffs, validation docs for evidence expectations, and safety docs for stop conditions. Entry instructions should point to these surfaces rather than copy them.

> **Context rule:** if a doc section would not change an agent's behavior, it probably does not belong in the automatic context path. Link to depth; do not flood the front door.

## Safety, Permissions, and Human Gates

Configure tool access by role. Reviewers should usually be read-only. Implementers can edit local files and run local validation. External writes, releases, data movement, credentials, and client-visible communication need explicit approval paths.

### Read-only

Explorer, planner, reviewer. Reads files and docs, searches code, returns findings.

### Local edit

Implementer. Edits repo files, runs focused validation, prepares PR evidence.

### External write

Issue, PR, Slack, calendar, docs, or system updates only when bounded and requested.

### Human approval

Production release, irreversible migration, credentials, data export, risk acceptance.

## Codex / Claude Code Crosswalk

Use each tool's native mechanisms, but keep the substance portable. Tool surfaces change. The durable decision is whether the support is orientation, workflow, role-specific review, deterministic enforcement, repo knowledge, runtime evidence, or risk gating.

| Concept | Codex surface | Claude Code surface | Guidance |
|---|---|---|---|
| Repo entry instructions | `AGENTS.md` | `CLAUDE.md / project memory` | Keep short; route to deeper docs |
| Reusable workflow | `Skill directory with SKILL.md` | `Skill directory with SKILL.md` | Use for procedures, not static facts |
| Specialist role | `.codex/agents/*.toml or spawned subagent role` | `.claude/agents/*.md` | Constrain role, tools, and source of truth |
| Deterministic enforcement | `Scripts, CI, config, approvals` | `Hooks, scripts, permissions, CI` | Automate objective rules |
| Project config | `.codex/config.toml` | `.claude/settings.json` | Commit only shared project settings |
| Tool integration | `MCP, plugins, narrow CLI wrappers` | `MCP, plugins, hooks, narrow CLI wrappers` | Prefer the smallest useful surface |
| Safety boundary | `Sandbox, approvals, config` | `Permissions, hooks, settings` | Humans own high-risk acceptance |

## Evaluation and Maintenance

Harness work should be tested against real or replayed work. A fresh agent should orient faster, ask fewer repeated questions, produce better evidence, or stop making the targeted mistake.

For judgment-based evaluations, use repeated runs or trend evidence rather than treating one LLM judgment as deterministic pass/fail. Use CI only when the rule itself is deterministic.

When the signal is still fuzzy, return to [Compounding Work](compounding-work.md#when-to-invest) and clarify what ordinary delivery evidence should improve.

| Method | What it tests |
|---|---|
| Fresh-agent trial | Can a new agent work from repo context alone? |
| Before / after replay | Did a skill, script, or agent reduce friction on a real task? |
| Review feedback audit | Is repeated review feedback declining or becoming harness tasks? |
| Evidence audit | Are PRs easier to review without extra archaeology? |
| Review-agent calibration | Do reviewers catch known bad cases without noisy generic advice? |
| Stop-rate audit | Are humans still being asked the same questions repeatedly? |

> If the same failure happens twice, decide whether it belongs in a doc, skill, script, review agent, test, lint, primitive, template, or safety gate.

## Appendix — Starter templates

### Minimum Project Layout

```
repo/
  AGENTS.md
  CLAUDE.md
  docs/
    core-beliefs.md
    architecture/overview.md
    validation/test-strategy.md
    safety/agent-safety-spec.md
  specs/
  efforts/
  scripts/
    bootstrap
    dev
    test-focused
    validate
    smoke
  .agents/skills/
  .codex/agents/
  .claude/agents/
```

### Harness Scorecard

```
# Harness Scorecard

| Area | Question | Current evidence | Rating | Follow-up |
|---|---|---|---|---|
| Entry instructions | Can a fresh agent start safely? |  |  |  |
| Specs / Efforts | Is work traceable and clear? |  |  |  |
| Skills | Are repeat workflows reusable? |  |  |  |
| Validation | Can agents check their own work? |  |  |  |
| Review agents | Do they catch project-specific risks? |  |  |  |
| Evidence | Are PRs reviewable quickly? |  |  |  |
| Observability | Can runtime behavior be inspected? |  |  |  |
| Safety | Are boundaries explicit and enforced? |  |  |  |
| Distillation | Do failures improve the harness? |  |  |  |
```

### Session Handoff

```
# Session Handoff

## Current goal

## Completed

## Current branch / worktree / PR

## Key decisions

## Validation run

## Open issues

## Next recommended step

## Risks / stop conditions
```

*Companion relationship: Compounding Work explains when repeated delivery friction should become system improvement. This playbook explains how to place that improvement into the agent harness.*

*For Rhizome-specific harness setup (starters, `rzm init`, skill verification, team identity, first effort): [Playbook: Agentic Harness Setup](../guides/playbook-agentic-harness-setup.md).*
