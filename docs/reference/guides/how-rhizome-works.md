---
type: ReferenceDoc
summary: "Conceptual guide explaining why Rhizome's core mechanisms — typed ontologies, code anchors, and starters — produce better agent retrieval than flat search or ad hoc context loading."
reference-kind: guide
status: active
last-verified: 2026-10-01
---

# How Rhizome Works

This guide explains the *why* behind Rhizome's design — why typed ontologies matter for agent retrieval, why code anchors outperform ad hoc LLM guessing, and why your starter choice shapes what agents can do. If you want to understand the tool before using it, start here.

---

## The Core Problem

AI agents working on code or domain problems need context. The naive approach is to dump files into the context window or let the agent search for relevant content ad hoc. Both approaches fail at scale:

- **Raw file dumps** flood the context window with irrelevant content, reduce precision, and re-teach constraints on every run
- **Ad hoc search** produces inconsistent results — agents guess at structure, miss domain rules, and surface different answers to the same question each time

Rhizome's answer: build a structured knowledge graph that knows the *shape* of your project's information and surfaces the right subset on demand.

---

## The Mental Model

Rhizome has three layers:

```
┌─────────────────────────────────────────┐
│  Agent Retrieval Surface                │
│  (rzm agent start / semantic-query /    │
│   file-context / ontology-query)        │
├─────────────────────────────────────────┤
│  Unified Knowledge Graph                │
│  (notes + code, typed, linked,          │
│   embeddings-indexed)                   │
├───────────────────┬─────────────────────┤
│  Vault            │  Code Index         │
│  (Markdown notes, │  (source files,     │
│   frontmatter,    │  coderefs, anchors, │
│   wikilinks)      │  CONTEXT.md)        │
└───────────────────┴─────────────────────┘
```

**Vault**: Markdown notes with typed frontmatter, wikilinks, and embedded properties. Rhizome indexes these into a graph and optionally creates semantic embeddings for similarity search.

**Code Index**: Source files scanned for double-bracket links and `@NotePath` references in comments and docstrings. Code anchor notes linked to specific files or directories. `CONTEXT.md` files that describe modules.

**Knowledge Graph**: Notes and code unified into a single graph with typed nodes, edges, community detection, and authority scoring. The typed layer is what separates Rhizome from a plain search index.

**Agent Retrieval Surface**: `rzm agent` commands that agents call to get scoped, budgeted, structured context — not raw search results.

---

## Why Typed Ontologies Matter

### The Problem With Flat Search

Without typed ontologies, your vault is a collection of Markdown files. An agent that searches for "authentication" might return a spec, a meeting note, a design decision, an action item, and a code comment — all equally weighted, all requiring the agent to figure out what kind of thing each one is.

This creates two problems:
1. **Precision loss**: relevant content gets buried in noise
2. **Repeated orientation**: every agent turn re-derives structure that the vault already encodes

### What Typed Ontologies Add

When notes have types — `ProductSpec`, `EffortNote`, `Requirement`, `UserStory`, `DomainConcept` — Rhizome can route retrieval by shape, not just keyword.

**Without ontology:**
```
Agent: "Find the authentication requirements"
Result: [5 notes mentioning 'authentication', in no particular order]
Agent: [reads all 5, figures out which are requirements vs. meeting notes vs. design docs]
```

**With ontology:**
```
Agent: runs requirements query filtered to FeatureArea: authentication
Result: [2 Requirement notes with provenance, linked to their source and spec]
Agent: [reads 2 structured notes, immediately knows what they are and where they came from]
```

The typed graph also enables structural queries: "give me all open stories linked to this spec", "show me all requirements not yet covered by a spec", "find all active efforts whose frozen spec set has drifted". These questions are only answerable when the vault has typed, linked nodes.

### How Rhizome Indexes Types

Types are defined in `.rhizome/ontology/*.graphql` — the starter you choose determines which types are available. When you run `rzm index`, Rhizome resolves each note's frontmatter `type:` field against the ontology and stores typed node records. `rzm agent ontology-query` executes structured queries against this typed graph.

---

## Why Code Anchors Beat Ad Hoc Context Loading

### The Problem With File Dumps

An agent working on `pkg/auth/session.go` needs context. The naive approach:
- Include the file itself
- Maybe include a few related files the agent guesses are relevant

What the agent often misses: the design decision note that explains *why* sessions use short-lived tokens, the spec that defines the session lifecycle, the requirement that mandates a specific expiration policy.

Without explicit links, the agent either misses this context or wastes tokens trying to find it.

### What Code Anchors Provide

Code anchors are explicit links from source code to documentation. A note with:

```yaml
code-anchors:
  go:
    - symbol: pkg/auth.SessionManager
    - dir: pkg/auth
```

...is automatically surfaced by `rzm agent file-context` whenever an agent asks about any file in `pkg/auth/` or any call to `SessionManager`. No guessing. No hallucinated context. The link is durable and indexed.

**Without code anchors:**
```
Agent: "I'm working on pkg/auth/session.go. What's the design intent?"
→ Agent scans file, reads comments, maybe finds a linked note if it was mentioned in a comment
→ Often misses the governing spec or design decision note
```

**With code anchors:**
```
Agent: runs rzm agent file-context --file pkg/auth/session.go
→ Retrieves: SessionManager design note, auth spec, session lifecycle requirement
→ Agent works inside the documented constraints
```

Coderefs (wikilinks in code comments/docstrings) work similarly: a double-bracket link to an auth spec in a comment becomes a backlink that Rhizome tracks and surfaces through file-context.

---

## Why Starter Choice Shapes What Agents Can Do

### Skills Are The Agent Interface

When `rzm init` installs the Agentic Engineering workflow, it adds a lean workflow router whose phases are arguments. `agentic-engineering` routes an unclear delivery request; `agentic-engineering specify` creates typed spec notes using the live ontology, `plan` maps approved scope to implementation phases, and `implement` executes against that scope.

Without a starter, you have a Rhizome index but no workflow instructions. The agent has retrieval capability but no structured procedure for using it.

### Ontology Shapes Retrieval Quality

The starter's ontology determines which types exist in your graph. An `agentic-engineering` vault can answer "what specs are unimplemented?" — a plain vault cannot. A `complex-domain` vault can answer "which requirements have no spec coverage?" — even Agentic Engineering alone cannot.

Choose your starter based on what questions you need your agents to be able to answer structurally, not just by keyword.

### What Installs Where (abbreviated)

When `rzm init` installs the Agentic Engineering workflow (the default choice, or `rzm init --workflow agentic-engineering`):

This tree highlights the primary installed surfaces and omits supporting reference files and sibling skills for readability.

```
.rhizome/
  ontology/
    spec-driven.graphql    ← types: ProductSpec, EffortNote, UserStory, ...
  query-recipes/
    spec-driven.yaml       ← named queries: effort-execution-context, spec-neighborhood, ...
  views/
    *.yaml                 ← configured table views for effort status, story status, ...

.agents/skills/
  agentic-engineering/SKILL.md ← router for delivery work
  agentic-engineering/references/specification.md ← specify phase
  agentic-engineering/references/planning.md       ← plan phase
  agentic-engineering/references/implementation.md ← implement phase
  agentic-engineering/references/effort-setup.md  ← phase text for creating efforts
  agentic-engineering/references/closure.md       ← phase text for evidence and closure

docs/engineering/
  README.md                ← policy index and precedence
  review-and-approval.md    ← task classification and approval
  quality-gates.md         ← repository command slots
  testing-policy.md        ← team test expectations
  documentation.md         ← retrieval leverage policy
  architecture.md          ← ownership and boundary rules
  release.md               ← branching, changelog and publication
```

The starter is the difference between "Rhizome is installed" and "agents know how to use Rhizome correctly for this project's workflow."

---

## The Retrieval Loop in Practice

Here's what happens when an agent starts a new session on an `agentic-engineering` repo:

1. **`rzm agent start --profile code --ontology`** — Returns session ID, vault context (note counts by type, community summary, recent activity), and ontology type counts. Agent now knows the shape of the vault without reading individual files.

2. **Structural query** — Agent runs `rzm agent query-recipe run --id effort-execution-context --anchor <effort-path>`. Gets back: frozen spec set, plan, stories in scope, checklist state, deviations — all in one bounded response.

3. **Code evidence** — Before editing a file, agent runs `rzm agent file-context --file <path>`. Gets back: governing notes, coderefs, code anchors, CONTEXT.md. Agent works inside documented constraints.

4. **Validation** — After changes, agent runs `rzm agent validate`. Checks broken links, ontology compliance, query recipe validity, frozen-scope drift. Surfaces fix suggestions.

At no point does the agent guess at structure, re-derive intent from scratch, or miss context that was explicitly documented. The typed graph makes the vault queryable in the shape the workflow needs.

---

## Linking It Together

The mechanisms reinforce each other:

- **Typed ontology** makes the vault structurally queryable
- **Code anchors + coderefs** bind code to the vault so file-context retrieval is precise
- **Starters** install the right ontology + skills so agents have structured procedures
- **Query recipes** save complex structural queries so agents can run them by name without constructing GraphQL

A vault with all four is qualitatively different from a flat note collection. Agents can onboard in one command, execute within documented constraints, and leave behind durable context for the next agent — rather than rediscovering structure from scratch each turn.

---

## Next Steps

- [Choosing Your Starter](choosing-your-starter.md) — pick the right starter for your use case
- [Playbook: Agentic Harness Setup](playbook-agentic-harness-setup.md) — set up a new or existing repo end-to-end
- [Playbook: Legacy Codebase Assessment](playbook-legacy-codebase-assessment.md) — run a structured assessment on an existing codebase
