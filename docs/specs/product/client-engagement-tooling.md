---
type: ProductSpec
id: SPEC-0067
summary: "Defines two new binary-embedded Rhizome skills for AO client engagements: client-harness-builder (packages vault findings into Rhizome-free Claude Code deliverables) and assumption-tracker (flags and synthesizes uncertain findings into structured meeting questions)."
spec-status: active
last-updated: 2026-07-19
aliases:
  - SPEC-0067
  - client-engagement-tooling
---

# Client Engagement Tooling

## Summary

When Atomic Object runs a client engagement — such as a legacy codebase documentation project — Rhizome accelerates the internal workflow, but the client receives deliverables (markdown docs, Claude Code commands, CLAUDE.md/AGENTS.md entry points) that must work without any Rhizome dependency. There is currently no standardized skill to produce these Rhizome-free client artifacts, and no structured workflow for flagging code analysis uncertainties and converting them into actionable kickoff questions. This spec defines both.

The two new skills are binary-embedded so AO teams can produce them via `rzm init` without rediscovering the pattern. `client-harness-builder` is universal; `assumption-tracker` ships with the `action-items` starter because its authoring and synthesis contracts require the ActionItem ontology and recipes.

## Goals

- An AO engineer can run `client-harness-builder` on a Rhizome vault and produce `.claude/commands/` files, CLAUDE.md/AGENTS.md entry points, and a maintenance runbook that work without any `rzm` dependencies
- An AO engineer documenting a legacy codebase can flag uncertain findings using `assumption-tracker` with a consistent tagging convention
- An AO engineer can query all open assumptions before a client meeting and synthesize them into organized, specific questions
- Both skills ship as binary-embedded templates; `assumption-tracker` is installed with the `action-items` starter so its required ontology and recipes are present

## Non-Goals

- Does not define the engagement vault structure itself (that is guided by existing playbooks in `docs/reference/guides/`)
- Does not add new ontology types — assumptions use the existing ActionItem pattern with a tagging convention
- Does not automate client harness generation — the skill guides agent-driven authoring
- Does not define a new `rzm init` starter template (uses existing `agentic-engineering` and `action-items` starters)

## User Stories

### US1 - Package vault findings as a client-ready harness

- id:: ^SPEC-0067-US1
- summary:: An AO engineer can run the `client-harness-builder` skill to produce `.claude/commands/` files and CLAUDE.md/AGENTS.md entry points that work in the client's repo without Rhizome.
- status:: ready

When the Rhizome vault has sufficient documentation of a codebase module or subsystem, the engineer needs a repeatable way to turn those findings into the artifacts the client actually uses. The deliverable is a `client-harness/` directory containing Claude Code-native files only.

#### Acceptance Criteria

- The `client-harness-builder` skill exists in `.agents/skills/` and `.claude/skills/` after `rzm init`.
  - The skill installs via `rzm init` (any template) and appears in both harness directories.
- The skill produces `.claude/commands/` files containing no `rzm` references.
  - Every command file the skill generates uses only Claude Code patterns: context-loading `@` references, step-by-step instructions, and plain markdown. No `rzm agent`, `rzm index`, or `.rhizome/` references appear in output files.
- The skill includes three reference command templates: document-table, update-module-docs, generate-arch-overview.
  - These templates ship in the skill's `references/` directory and serve as the starting point for commands tailored to the specific codebase.
- Output is stored under `client-harness/` in the engagement vault.
  - The skill's procedure explicitly routes output to `client-harness/` to keep AO's internal work and client deliverables cleanly separated.

---

### US2 - Flag uncertain findings during documentation

- id:: ^SPEC-0067-US2
- summary:: An AO engineer documenting a legacy codebase can use the `assumption-tracker` skill to flag uncertain findings as typed action items with a consistent `#assumption/<type>` tag.
- status:: ready

During code analysis, agents encounter fields, rules, and behaviors where the code alone is ambiguous. These need to be captured immediately and near the relevant context, not in a separate catch-all list.

#### Acceptance Criteria

- The `assumption-tracker` skill exists after `rzm init` installs the `action-items` starter and documents the `#assumption/<type>` tagging convention.
  - The skill installs in `.agents/skills/` and `.claude/skills/` and defines four assumption types: `business-logic`, `schema`, `integration`, `process`.
- Assumption items are authored as ActionItem checkbox items with `#assumption/<type>` in the title.
  - Convention: `- [ ] #action-item #assumption/<type> Confirm: <specific question>`. Items live in the note near the uncertain code/field, not in a catch-all file.
- The skill's flag mode requires specific "Confirm: X" phrasing.
  - Vague "Ask about Y" phrasing is explicitly listed as a guardrail violation. Each assumption must name the specific claim being confirmed.

---

### US3 - Query and synthesize open assumptions

- id:: ^SPEC-0067-US3
- summary:: An AO engineer can run a query recipe to surface all open assumptions and synthesize them into organized kickoff questions, grouped by type and subsystem.
- status:: ready

Before a client meeting, the engineer needs a filtered view of all flagged assumptions, organized by type and module, ready to turn into a meeting agenda.

#### Acceptance Criteria

- An `assessment-assumptions` query recipe exists in the `action-items` starter and returns all open action items.
  - The recipe is present in `.rhizome/query-recipes/action-items.yaml` when that starter is installed and validates against the installed ActionItem ontology.
- The recipe returns `title`, `notePath`, `due`, and `assignee` for each result.
  - These fields are sufficient for the skill's synthesize-mode procedure to group and draft meeting questions without re-reading source files.
- The skill's synthesize mode outputs to `docs/assessment/kickoff-questions.md`.
  - The procedure ends with a named output path so engineers know where to find the synthesized questions.

---

### US4 - Template engagement vault scaffold

- id:: ^SPEC-0067-US4
- summary:: An AO engineer starting a new client engagement has a documented vault scaffold that cleanly separates AO internal work from client deliverables.
- status:: ready

Without a standard scaffold, each engagement invents its own directory structure. The AO/client separation (what stays internal vs. what ships) is otherwise implicit.

#### Acceptance Criteria

- The `client-harness-builder` skill documents a canonical `client-harness/` directory convention.
  - The skill's procedure section names `client-harness/` as the output root for all client-facing artifacts.
- The engagement vault structure is documented (EFF-0032 and this spec serve as the template).
  - AO engineers can reference EFF-0032 and the `client-harness-builder` skill to set up future engagements consistently.

## Requirements

- `client-harness-builder` MUST install universally; `assumption-tracker` and `assessment-assumptions` MUST install from the `action-items` starter with the ActionItem ontology
- `client-harness-builder` output files MUST contain no `rzm` commands, no `.rhizome/` path references
- `assumption-tracker` MUST define exactly four assumption types as the standard vocabulary
- The `assessment-assumptions` query recipe MUST query the `actionItem` root directly (not through a containing note type)
- All skill files MUST follow format conventions of existing binary-embedded skills (frontmatter name/description, Goal/When to use/Non-goals/Procedure/Guardrails sections)

## Open Questions

- Should the client harness directory be named `client-harness/` (current) or something more generic like `harness/` for potential non-client uses?
- Should the `assessment-assumptions` recipe attempt a `contains` filter on `title` (simpler) or rely on the agent to filter post-query (safer, confirmed compatible)?

## Documentation Plan

- `pkg/app/cli/init/templates/skills/markdown/client-harness-builder/SKILL.md` — new binary-embedded skill
- `pkg/app/cli/init/templates/skills/markdown/client-harness-builder/references/document-table.md` — command template
- `pkg/app/cli/init/templates/skills/markdown/client-harness-builder/references/update-module-docs.md` — command template
- `pkg/app/cli/init/templates/skills/markdown/client-harness-builder/references/generate-arch-overview.md` — command template
- `pkg/app/cli/init/templates/starters/action-items/agents/skills/assumption-tracker/SKILL.md` — ActionItem-backed binary-embedded skill
- `.rhizome/query-recipes/action-items.yaml` — action-items recipe bundle containing `assessment-assumptions`
