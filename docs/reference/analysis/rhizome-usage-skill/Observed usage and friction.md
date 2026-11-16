---
type: ReferenceDoc
summary: "How Rhizome was actually used by its maintainer and agents from 2026-10-18 to 2026-11-15, drawn from Claude Code and Codex session history, and the friction a usage skill must prevent."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Observed usage and friction

## Summary

Session history shows three contexts. In the Rhizome repository, agents use Rhizome as a developer tool. In the maintainer's personal work vault, scheduled agents run code mode about a thousand times a month against a heavily customized schema. In other repositories, Rhizome is mostly absent or sits on stale pins. Most failures were not missing features. Agents guessed commands and input shapes, ran an old binary, misread sandbox or approval denials as Rhizome failures, or installed a starter that did not fit. A usage skill has to prevent those failures before it teaches features.

The evidence is about 1,000 Claude Code transcripts and about 1,800 Codex rollouts in the window. Client and company repositories are described generically. Counts are from scripted extraction and are approximate.

## Usage by context

**The Rhizome repository.** Agents run the agentic-engineering starter for specs, efforts, and closure, plus `rzm validate`, `init` against throwaway fixtures, `agent ontology-query`, and code mode for audits. The `rhizome` skill was explicitly invoked only a handful of times. Agents more often read its files directly.

**A personal work vault.** This is the most advanced real use, and the best worked example of the customization ladder.

- It installs `core`, `action-items`, `agentic-engineering`, and `compounding`. The Agentic Engineering workflow sits on a vault with no code delivery, because no notes-oriented workflow choice exists.
- It has seven hand-written ontology files for meetings, commitments, goals, perspectives, an idea pipeline, and vault structure.
- It has nine query recipes, four TSX views (a home dashboard, a work desk, report prep, a goals slate), and two YAML views.
- Vault-local skills ship code-mode scripts piped into `rzm agent code execute`. One pipeline writes a typed note from a script, renders it in a custom view, and publishes an external site from the same note.
- Scheduled Codex automations drive most calls: a morning briefing and an hourly meeting-transcript catch-up that writes typed meeting notes and reconciles commitments. In the window, `agent code execute` ran about 1,040 times, `agent code describe` about 200 times, and `agent start` about 170 times.

**Other repositories.** Several client and company repositories run Rhizome on pins from v0.48 to v0.50 with configuration untouched since summer. One active client team already uses another agent workflow, and the maintainer did not propose Rhizome there. One knowledge repository serves as the real AO KB fixture.

**The desktop app.** The first in-app chat sessions (2026-11-14) answered "what am I looking at", quoted note text, and found overdue action items through `page_text` and the `action-items` skill. They made no `rzm` calls. The first colleague to install the app said the dashboard "did not make the next action obvious", and needed a walkthrough to migrate existing specs.

## Friction log

Each entry gives what happened and the guidance that would have prevented it.

1. **Guessed or shortened commands.** Agents typed `rzm next-id` (at least seven times in six sessions), `validate --files`, `rzm status`, and wrong query-recipe id forms. One cause is in a shipped skill: `agentic-engineering/references/effort-setup.md:14` writes `next-id --type …` without the `rzm agent` prefix. Prevention: a command map of full command lines, checked against `rzm agent surface` in a test.
2. **Guessed code-mode input shapes.** Agents wrote `input` for `inputs` (14 times), `op` for `action`, a string where an `anchor` array belongs, and invented operation names, and they relearned the contract with `describe` on every automation run. Prevention: exact shapes for the operations people actually use (`validate`, `queryRecipe`, `ontologyQuery`, `ontologyAuthoringGuide`, `ontologyQuerySchema`, `files`, `ontologyInspect`, `currentUser`, `semanticQuery`), plus guidance for unattended runs.
3. **The wrong binary.** An old PATH `rzm` (v0.50.5) lacked code mode, agents invented `bin/rzm` about 15 times, and fresh worktrees reported `devBinaryDir target missing` about 60 times. Prevention: a preflight that names which `rzm` runs, its version, and the repository pin.
4. **Sandbox and approval denials read as Rhizome failures.** A `kern.boottime` lock error (about 72 times), "persistence unavailable", and approval reviewers that denied eight Rhizome operations because content could leave the machine for embeddings or a team server. Automations stalled until the maintainer approved in his own words. Prevention: a map of which operations send data off-machine, and a durable approval line a user can put in AGENTS.md or an automation brief.
5. **Optional services treated as blockers.** Compounding lookup failed on a locked keychain in about two thirds of vault calls. Prevention: say which failures degrade work and which block it, and require reporting the degraded path.
6. **Unclear write surfaces.** A heading rename failed through MCP, the agent CLI refused `--apply`, and only `rzm note rename-heading --apply` worked. A note move reported "recovery pending" after it had committed. Prevention: one table of which surface plans and which applies, and how to read receipts.
7. **A starter that collided.** Installing a starter into the vault added a second `Person` type. Prevention: a fit check and a schema-collision check before recommending or installing.
8. **Schema evolution by deletion.** Faced with 42 ambiguous coaching notes, an agent deleted the specific type. The maintainer explained that subtypes carry specialized guidance, which led to interfaces. He also taught that schema docstrings and `summary` exist for agents scanning the vault, not as introductions. Prevention: pattern-based schema guidance covering interfaces for subtypes, docstrings as agent rules, and fixing validation drift instead of baselining it.
9. **Stale installations.** Pin upgrades did not refresh skills until PR #133. Prevention: an upgrade and repair flow.

## Principles the maintainer repeated

- Generic modules and experiences a user can eject into their own `.rhizome`.
- Stay in the user's harness and model provider.
- Fix root causes; a documented workaround the agent never finds is still friction.
- Put rules where agents read them, in the schema and skills.
- Desktop first, the CLI as the alternative, with progressive disclosure.
- Comps or mock-ups before building anything large.
- Never re-embed unchanged text, and name paid calls before making them.
- Preserve concurrent and unrelated edits.

## Implications for the usage skill

1. Lead with an environment check, not a feature tour.
2. Carry a verified command map and code-mode input cheat sheet.
3. Organize onboarding by the user's situation: a new code repository, a repository with existing specs from another workflow, an AO KB, a personal or team notes vault, and a team that already has its own workflow.
4. Treat upgrade and repair as a first-class flow.
5. Use a vault like the one above as the worked example of the customization ladder.
6. State data egress, cost, and approval facts before automation.
7. Defer to repository-local skills and conventions before writing notes.
