---
name: client-harness-builder
description: >-
  Package validated Rhizome documentation for client handoff as a standalone
  Claude Code harness with commands, agent entry points, and a maintenance runbook.
---

# Client Harness Builder

## Goal

Turn Rhizome vault documentation into a self-contained Claude Code harness the client can use without installing or knowing about Rhizome. The deliverable is a `client-harness/` directory containing only standard markdown files: no `rzm` commands, no `.rhizome/` references.

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

7. **Check the harness before handoff.** It is done when all required files exist, every guardrail below holds, every `@` target resolves in the intended client repository layout, and no `[PLACEHOLDER` tokens remain. Check that each open `#assumption` item noted in step 1 appears in its module's command as a `[REQUIRES CLARIFICATION: ...]` marker. The team-contact `[TBD]` from step 5 may remain until handoff.

## Guardrails

- **No Rhizome in output** — every generated file must work for a developer who has never heard of Rhizome: no `rzm` commands, and no references to `.rhizome/` paths, vault config, query recipes, or ontology files
- **Commands must be grounded** — write commands only for areas with vault documentation; mark undocumented areas as `[Not yet documented — add to documentation backlog]`
- **Separate harness from internal work** — `client-harness/` is the delivery surface and gets the finished commands and entry points; AO's vault and assessment notes stay in `docs/` and are not copied into it
- **Flag known gaps** — carry each open assumption item (`#assumption`) for a packaged module into its command as `[REQUIRES CLARIFICATION: <brief description>]`

## Reference notes

- `references/document-table.md` — starting template for a table documentation command
- `references/update-module-docs.md` — starting template for a module documentation update command
- `references/generate-arch-overview.md` — starting template for an architecture overview command
- When the active harness exposes `assumption-tracker`, use it for ActionItem-backed assumption flagging and synthesis; otherwise do not imply that optional phase is installed
- Engagement workflow chain with the `action-items` starter: `legacy-codebase-assessor` (assess, identifying what to document) → `assumption-tracker` (flag during documentation) → `client-harness-builder` (this skill — package for client handoff). Without action-items, assessment/documentation hands off directly here. Use this skill after the vault has validated documentation, not before.
