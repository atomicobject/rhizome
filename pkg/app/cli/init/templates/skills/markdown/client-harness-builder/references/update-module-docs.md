# Template: Update Module Docs Command

Starting template for `.claude/commands/update-module-docs.md`. Replace all `[PLACEHOLDER]` values with codebase-specific content before placing the file in `client-harness/.claude/commands/`.

---

Update the documentation for a module after code changes.

## Usage

Provide the module path and a brief description of what changed. This command will:

1. Read the current module documentation
2. Identify which documented behaviors, fields, or flows are affected by the change
3. Update or flag stale sections
4. Leave accurate sections unchanged

## Context to load

Before starting, load these files:

- @[PLACEHOLDER: path/to/module-docs/]
- @[PLACEHOLDER: path/to/relevant-schema-docs/ if the change touches data]
- @CLAUDE.md

## Steps

1. Read the current documentation for the module at `[PLACEHOLDER: module-doc-path]`
2. Read the changed files (provide the diff or the changed file paths)
3. For each documented behavior, field, or integration point the change touches:
   - If the documentation is now **wrong**: rewrite the affected section to match the new behavior
   - If the documentation is now **incomplete**: extend it to cover the new behavior
   - If the documentation is still **accurate**: leave it unchanged — do not rewrite for style
4. If the change introduces new fields, behaviors, or integration points not yet documented:
   - Add documentation for them following the existing format
   - If the purpose of a new field or behavior is not clear from the code, mark it `[REQUIRES CLARIFICATION: <brief description>]` rather than guessing
5. Update the `last-updated` date in the module's CLAUDE.md or AGENTS.md if one exists

## Guardrails

- Do not rewrite sections unaffected by the change — surgical edits only
- Do not invent business meaning for new fields from code inference alone — mark them `[REQUIRES CLARIFICATION]` if the purpose isn't explicit
- Keep documentation grounded in the code, not in assumptions about intent
- If the change affects multiple modules, update each module's documentation separately rather than writing a cross-module summary in one place

## Output

Produce a diff-style summary of what changed in the documentation:

```
Updated sections:
- [Section name]: [one-line description of what changed]

Added sections:
- [Section name]: [one-line description of what was added]

Unchanged sections:
- [count] sections left unchanged

Requires clarification:
- [any REQUIRES CLARIFICATION markers added and why]
```
