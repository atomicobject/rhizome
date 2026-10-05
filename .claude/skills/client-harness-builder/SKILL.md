---
name: client-harness-builder
description: >-
  Use when you need to package Rhizome-assisted documentation into a
  client-ready agentic harness that works without Rhizome dependencies —
  producing .claude/commands/ files, CLAUDE.md/AGENTS.md entry points, and
  a maintenance runbook the client can use standalone.
---

# Client Harness Builder

## Goal

Turn Rhizome vault documentation into a self-contained Claude Code harness the client can use without installing or knowing about Rhizome. The deliverable is a `client-harness/` directory containing only standard markdown files — no `rzm` commands, no `.rhizome/` references.

## When to use

- When the Rhizome vault has sufficient documentation for one or more codebase modules and you need to produce client-facing Claude Code commands
- At the end of a documentation sprint to package findings before client handoff
- When asked to "create client deliverables", "build the harness", or "package the docs" from existing vault notes

## Non-goals

- Do not include any `rzm` commands in output files — the client does not have Rhizome installed
- Do not reference `.rhizome/` paths, query recipes, or ontology files in any client-facing artifact
- Do not generate commands for undocumented modules — commands must be grounded in vault content
- Do not duplicate AO's internal vault notes into `client-harness/` — clients get the finished commands and entry points, not the raw assessment notes

## Operating mode

`Agent Delegation` — once the vault has sufficient module documentation, produce the harness files directly without iterative check-ins. Use `Agent Partnership` only when the client's anticipated workflow is unclear and it would change which commands to write.

## Procedure

1. **Orient to the vault documentation**:
   - Use `rzm agent file-context` on the relevant assessment notes and module docs
   - Note which areas are well-documented (high confidence) vs. flagged with `#assumption` items — commands should note flagged gaps

2. **Determine the command set** based on what the client dev team will do most often:
   - For each documented module: does the team need to document new tables? Update docs after code changes? Generate subsystem diagrams?
   - Default to the three standard templates in `references/` unless the module's workflow differs significantly
   - Name each command after its action: `document-table.md`, `update-module-docs.md`, `generate-arch-overview.md`

3. **Draft each command file** using the templates in `references/`:
   - Start from the relevant template
   - Replace `[PLACEHOLDER]` values with codebase-specific content (table names, module paths, doc paths)
   - Every `@` context reference must point to a doc file that will exist in the client's repo
   - Each command must be self-contained: context to load, steps to follow, output format

4. **Write CLAUDE.md and AGENTS.md entry points**:
   - Root-level files: list documented subsystems and route agents to the right module docs and commands
   - Module-level files (for large or complex modules): a focused entry point that loads only what's needed for that module
   - Entry points reference documentation files, not Rhizome tools

5. **Write the maintenance runbook** (`maintenance-runbook.md`):
   - When to run each command (new table added, code changed, new module needs coverage)
   - How to know when documentation is stale (look for `[REQUIRES CLARIFICATION]` markers)
   - Escalation path: who to contact when a documentation question can't be answered from the code alone
   - Leave placeholder: `[TBD — fill in during handoff week]` for team-specific contacts

6. **Store everything under `client-harness/`**:
   ```
   client-harness/
     .claude/
       commands/
         document-table.md
         update-module-docs.md
         generate-arch-overview.md
     CLAUDE.md
     AGENTS.md
     maintenance-runbook.md
   ```

## Guardrails

- **No `rzm` in output** — every generated file must work for a developer who has never heard of Rhizome
- **No `.rhizome/` references** — do not reference vault config, query recipes, or ontology files in any client artifact
- **Commands must be grounded** — only write commands for areas with vault documentation; mark undocumented areas as `[Not yet documented — add to documentation backlog]`
- **Separate harness from internal work** — `client-harness/` is the delivery surface; AO's vault notes stay in `docs/`; do not mix them
- **Flag known gaps** — if an assumption item (`#assumption`) was flagged for a module, note it in the relevant command as `[REQUIRES CLARIFICATION: <brief description>]`

## Reference notes

- `references/document-table.md` — starting template for a table documentation command
- `references/update-module-docs.md` — starting template for a module documentation update command
- `references/generate-arch-overview.md` — starting template for an architecture overview command
- When the active harness exposes `assumption-tracker`, use it for ActionItem-backed assumption flagging and synthesis; otherwise do not imply that optional phase is installed
- See `legacy-codebase-assessor` for the upstream assessment that identifies what to document before this skill packages it for the client
- Engagement workflow chain with the `action-items` starter: `legacy-codebase-assessor` (assess) → `assumption-tracker` (flag during documentation) → `client-harness-builder` (this skill — package for client handoff). Without action-items, assessment/documentation hands off directly here. Use this skill after the vault has validated documentation, not before.
