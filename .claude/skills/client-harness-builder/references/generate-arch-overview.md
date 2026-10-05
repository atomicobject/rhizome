# Template: Generate Architecture Overview Command

Starting template for `.claude/commands/generate-arch-overview.md`. Replace all `[PLACEHOLDER]` values with codebase-specific content before placing the file in `client-harness/.claude/commands/`.

---

Generate an architecture overview and Mermaid diagram for a subsystem or cluster of related modules.

## Usage

Provide the subsystem name or a list of module/directory paths to include. This command will:

1. Read the documentation for the named modules
2. Identify key integration points, data flows, and dependencies
3. Produce a Mermaid diagram and a narrative overview

## Context to load

Before starting, load:

- @[PLACEHOLDER: path/to/architecture-docs/] (if it exists)
- @[PLACEHOLDER: path/to/module-docs/ for each module in the subsystem]
- @CLAUDE.md

## Steps

1. Read the documentation for each module in the subsystem
2. Identify across all modules:
   - **Data flows**: what data enters the subsystem, what exits, how it transforms as it moves through
   - **Module dependencies**: which modules call which (direction matters)
   - **Integration points**: external systems, shared database tables, message queues, APIs
   - **Key business rules**: the invariants and decisions that govern the subsystem's behavior
3. Produce a Mermaid flowchart or sequence diagram capturing the main flows — use `flowchart LR` for dependency graphs, `sequenceDiagram` for request/response flows
4. Write a short narrative: what this subsystem does, why it exists, where the complexity lives
5. If the subsystem spans both legacy and newer architecture, note the architectural boundary and how the two interact

## Output format

```markdown
## [Subsystem Name] Architecture

### Overview

[Short narrative: what this subsystem does, why it exists as a distinct unit, where the significant complexity lives.]

### Architecture diagram

```mermaid
flowchart LR
    [nodes and edges]
```

### Key integration points

- **[External system or table]**: [one-line description of the relationship]

### Complexity areas

- **[Area name]**: [one-line description of what makes it complex or ambiguous]

### Requires clarification

- [Any `[REQUIRES CLARIFICATION]` markers from the source documentation, surfaced here for visibility]
```

## Guardrails

- If a module's documentation has `[REQUIRES CLARIFICATION]` markers, surface them in the "Requires clarification" section — do not silently omit them from the overview
- Do not infer integration points that are not documented — mark them as `[undocumented — verify with team]`
- Keep diagrams readable: 8–12 nodes maximum per diagram; split large subsystems into multiple diagrams
