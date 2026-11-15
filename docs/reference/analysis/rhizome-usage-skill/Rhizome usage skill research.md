---
type: ReferenceDoc
summary: "Hub for the November 2026 research into a Rhizome usage skill: the goal, what each note in this folder covers, the cross-cutting findings, and the decisions left for the design council."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Rhizome usage skill research

## Goal

Rhizome needs agent guidance that explains what Rhizome is, what it can do, and how to use it. An agent should be able to load it inside the desktop app's in-app chat or standalone in a repository with Rhizome skills installed. It should take a user from simple starts, such as the AO KB add-on or the Agentic Engineering starter, through schema evolution, views, code-mode automation, and custom experiences. It should also look at a repository, recommend starters, install them, and update what is set up.

This folder is the evidence base for a design council. It does not decide the skill's shape.

## Notes in this folder

- `App flow and surfaces.md` covers install, setup, sections, Settings, and keeping current, as a user experiences them.
- `In-app chat agent environment.md` covers what the chat agent is told, which skills and tools it sees, its permissions, and what it can and cannot do.
- `Customization ladder.md` covers eleven rungs from personal view choices to packaging a module, each tested in a scratch vault.
- `Schema evolution and views for custom workflows.md` builds a sales pipeline as a team module end to end.
- `Code mode and automation.md` covers code mode, skill scripts, module actions, scheduled tasks, and module lifecycle.
- `Starter catalog.md` covers what each of the six starters installs and how they combine.
- `Onboarding mechanics.md` covers `rzm init` flags, the setup sheet, upgrades, and what an agent may run alone.
- `Starter recommendation heuristic.md` maps repository signals to starters and gives the questions to ask when signals conflict.
- `Current skill coverage and gaps.md` audits the shipped guidance against seven user questions.
- `Packaging options for usage guidance.md` compares five places the guidance could live.
- `Observed usage and friction.md` draws on a month of session history.
- `Council outcome.md` records the design council's agreement, disagreements, and recommended design.

## Cross-cutting findings

1. **The mechanics exist; the translation layer does not.** The `rhizome` skill is an operator's manual for an agent mid-task. Questions that name Rhizome already load it, and then find no routing row for "what is Rhizome", "what can I do here", or "which starter". Requests for a pipeline view or vault automation get mechanics without a decision path.
2. **Downstream repositories carry no product docs.** The guides for getting started, choosing a starter, adapting Rhizome, building an experience, scheduled tasks, and module lifecycle live only in Rhizome's repository. Whatever the skill needs, it has to carry or ship.
3. **The in-app chat gets almost nothing.** Its whole Rhizome context is the roughly 90-word `chatInstructions` constant in `pkg/app/agentchat/page_relay.go`, which names no skill. A repository set up with `--agents shared` gets no `.claude/skills`, so a Claude chat there likely sees no Rhizome skill at all. That is verified in code and not yet checked at run time.
4. **The chat agent cannot act in the app.** It can read files, run shell commands under its permission mode, and call `page_text`. It cannot navigate, open Settings, render tables, or present choice prompts. Auto permission mode means writes may not prompt. In-app directions must name controls the person presses, and changes need an explicit plain-text yes.
5. **SPEC-0080 bars naming starters in the `rhizome` skill.** Recommending starters therefore needs a spec amendment, live discovery through `rzm init --check --json` (which today omits the optional add-ons `ao-kb` and `compounding`), or a separate home.
6. **Environment problems cause most real failures.** Agents guessed commands and code-mode shapes, ran stale binaries, and misread sandbox or approval denials. A verified command map and a preflight matter more than a feature tour.
7. **Setup has traps an agent will hit.** `--addons compounding` silently drops the default `action-items`. `--workflow none` installs no starter at all. Non-interactive `rzm init` writes immediately. `rzm trust` needs a terminal. AO KB is reachable only as an add-on after first setup. A starter can collide with local schema types.
8. **Advanced use is real and has a worked example.** The maintainer's vault runs custom ontology, recipes, skill-bundled code-mode scripts, TSX dashboards, and scheduled automations. It shows the full ladder in practice.

## Small fixes found along the way

These are independent of the council's decision.

- `pkg/app/cli/init/templates/starters/agentic-engineering/agents/skills/agentic-engineering/references/effort-setup.md:14` writes `next-id` without its `rzm agent` prefix, and agents copy it.
- `docs/reference/guides/choosing-your-starter.md`, `getting-started.md`, and the README never mention AO KB.
- The `rhizome` skill's ontology-authoring reference still places schemas in `.rhizome/ontology/`, although starter schemas now live in module folders.
- `rzm init --check --json` omits optional add-ons, so no agent can discover `ao-kb` or `compounding` from live data.

## Decisions for the council

1. Where the guidance lives: new references in `rhizome`, a sibling core skill, starter overlays, the chat prompt, shipped guides, or a combination.
2. How starter recommendation squares with SPEC-0080: amend it, discover starters at run time, or move it elsewhere.
3. How the skill detects and serves its two environments, in-app chat and standalone, including the `--agents shared` gap.
4. What the skill carries itself (product model, capability map, intent-to-mechanism table, command map, preflight, egress map) and what it routes to existing references.
5. How in-app changes are applied: the JSON plan in chat, or a handoff to Settings.
6. How triggering is verified on Claude Code and Codex before shipping.
