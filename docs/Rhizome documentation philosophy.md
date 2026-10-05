---
summary: "Core philosophy for Rhizome documentation: reify human intent into the codebase so agents can operate within constraints across sessions, without re-discovery."
tags: [type/reference, subsystem/documentation, subsystem/vision]
---

# Rhizome documentation philosophy

## The problem

Agents are good at implementing what users ask for in a session. But without durable context, each session starts from scratch:

- **Intent evaporates**: a user explains why a module should be fast, the agent optimizes it, but the next agent doesn't know performance matters and adds a blocking call.
- **Decisions get lost**: someone chose approach A over B for good reasons, but those reasons live in a Slack thread or someone's head—not in the codebase.
- **Constraints go unstated**: invariants that "everyone knows" aren't documented, so agents violate them and humans spend time fixing preventable mistakes.
- **Architecture becomes folklore**: data flow, subsystem boundaries, and extension points exist only in tribal knowledge.

The result: agents operate in a vacuum, humans repeat themselves, and the codebase accumulates decisions that no one can explain.

## The core idea

**Reify human intent into the codebase itself.**

Rhizome exists to capture and preserve what humans know—not just at the local implementation scale, but at the scale of subsystems, data flows, and architectural decisions. The goal is a codebase where:

- An agent can retrieve the constraints and intent that apply to any file it touches.
- Knowledge accumulates across sessions rather than being rediscovered.
- Humans express intent once; agents operate within it indefinitely.

This is **caching for human insight**: expensive-to-produce understanding gets persisted so it can be cheaply retrieved.

## Primary audience: agents

While documentation should remain useful for humans, **agents are the primary consumer**. We expect most coding to happen through agents, with humans focused on intent, direction, and review rather than direct implementation.

This means documentation should prioritize what agents need to operate safely and effectively:

1. **Knowledge and insights** — what has been learned about this code, its behavior, its edge cases
2. **Decisions** — what was chosen, what was rejected, and why
3. **Invariants and constraints** — what must remain true, what must never happen
4. **Goals** — what this code is trying to achieve, what success looks like
5. **Architecture and design** — how pieces fit together, where boundaries are, how to extend safely

These are the things agents cannot infer from code alone.

## The context graph

Rhizome builds a unified graph connecting code and documentation, where both notes and code files are nodes:

### Graph structure

- **Notes** linked via wikilinks form a navigable knowledge base
- **Code files** connected to notes via coderefs and mention edges
- **Symbols** (functions, types, modules) addressable as anchors with typed relationships (calls, defines, tests)

### Bidirectional binding (the key mechanism)

The distinctive feature of Rhizome is **bidirectional code-docs binding**:

| Direction | Mechanism | Effect |
|-----------|-----------|--------|
| Code → Note | **Coderefs**: wikilinks/mentions in comments | "This code follows this contract" |
| Note → Code | **Code anchors**: frontmatter rules targeting symbols | "This contract applies to all code using this symbol" |

This bidirectionality is what makes docs surface automatically:

- **Coderefs** ensure that when you're reading code, you see the linked notes.
- **Code anchors** ensure that when you're editing a *caller* of a symbol, you see the docs—even if the caller has no coderefs of its own.

The system also produces **mention edges** from doc sections to code anchors, so prose in notes gets attached to specific code entities. This is how "docs for code" works even when the code itself has no links.

### Additional context surfaces

- **Module docs** (`CONTEXT.md`): per-directory docs explaining scope, invariants, and how files fit together
- **Symbol indexing**: function/class/module comments, plus call graphs and type references
- **Graph-derived signals**: PageRank on anchors (high-fanin code is "important"), HITS on the doc graph (well-linked notes have authority)

When an agent runs `file_context`, it gets not just code but the contracts, design rationale, and operational hazards that apply—without having to search for them.

## Core principles

### 1. Capture intent as you go

Developers and agents should document **intent, decisions, invariants, and design rationale** during normal development—not as an afterthought.

- If a user explains why something matters (performance, security, ordering), that belongs in documentation.
- If a design decision has non-obvious tradeoffs, write them down near the code or in a linked note.
- Treat documentation as a by-product of thinking, not a separate task.

### 2. Docs are contracts

Documentation should be treated as a lightweight contract that agents consult and maintain:

- **Read before changing**: agents should check `file_context` and linked notes before modifying behavior.
- **Update when behavior changes**: if you change what code does, update the nearest docstring, comment, or linked note.
- **Ask before contradicting**: if a change conflicts with documented invariants, the agent should ask whether the intent is to change the contract.

### 3. The documentation feedback loop

The most powerful aspect of this approach is the virtuous cycle:

```
Human states intent → Agent documents it → Agent retrieves it next session
       ↑                                              ↓
       └──────── Agent updates when things change ←───┘
```

Each cycle makes the codebase smarter:
- Agents retrieve constraints before making changes
- Agents update docs when behavior changes
- Updated docs inform future agents

Without the loop, documentation rots. With it, documentation becomes a living cache of institutional knowledge.

### 4. Document what matters most

The system already knows what's central: PageRank identifies high-fanin code (called from many places), and HITS identifies authoritative notes (well-linked, frequently referenced).

Focus documentation effort where it has the highest leverage:

- **High-fanin functions and types**: many callers means many opportunities for misuse
- **Boundary surfaces**: APIs, entry points, and integration seams where mistakes are expensive
- **Non-obvious behavior**: caching, ordering, retries, failure modes—things that look simple but aren't

Don't document the obvious. Document what would hurt to get wrong.

### 5. Token-dense documentation

Because agents consume documentation in limited context windows, **every token should earn its place**.

This isn't just advice—it's how the system works mechanically:

- Notes are chunked into sections; each chunk has a budget (~1500 chars target)
- Frontmatter `summary` fields are included in chunks and boost retrieval
- Small, focused docs embed better than sprawling ones
- Large anchored docs consume budgets and crowd out subsystem-specific contracts

**Prefer:**
- Summaries over exhaustive explanations
- Frontmatter `summary` fields (they're retrieval multipliers)
- Bullet points over prose when listing constraints
- Links to depth rather than embedding everything

**Avoid:**
- Redundant explanations of things obvious from code
- Long historical narratives (summarize the decision, not the journey)
- Embedding large "background" docs in anchored notes

**Heuristic**: if a doc section wouldn't change an agent's behavior, it probably doesn't need to exist.

### 5a. Two-tier note model (anchor/coderef notes vs. reference docs)

Notes surfaced via coderefs and code anchors are **context-window real estate**. They're retrieved automatically when agents work on related code, so they must be extremely high-signal.

#### Tier 1: Anchor/coderef notes (optimized for automatic retrieval)

These notes are pulled into context automatically. Every token must change agent behavior.

**Include:**
- Invariants, constraints, "don't do X because Y"
- Non-obvious behavior, edge cases, failure modes
- Extension patterns ("how to add Y safely")
- Key vocabulary and definitions

**Exclude:**
- Background, history, rationale prose
- Exhaustive API documentation
- Anything inferrable from code
- General explanations or tutorials

**Target size**: Under 2k chars. If longer, split into hub + reference notes and link out.

#### Tier 2: Reference docs (pulled on demand)

Broader documentation lives in reference notes, README files, and design docs. These are:
- **Linked from** tier-1 notes rather than embedded
- **Pulled explicitly** via `semantic_query` or `files` when depth is needed
- **Not automatically retrieved** — agents follow links when they need more

This separation keeps automatic retrieval lean while preserving depth for when it's needed.

### 6. Tests as executable documentation

Tests encode intent in a form that's verified on every run. They're a powerful documentation surface because:

- **Test names document invariants**: `TestCacheInvalidatesOnWrite` tells you what must remain true
- **Assertions reveal expected behavior**: the test body shows what outputs are correct for given inputs
- **Test setup shows preconditions**: fixtures demonstrate what state is required for code to work

When reading code with sparse docs, tests are often the most reliable source of truth for expected behavior and edge cases. When writing tests:

- **Name tests for the behavior they verify**, not the function they call
- **Add comments for non-obvious setup** — explain why the fixture is shaped a certain way
- **Treat test failures as doc drift** — if a test breaks, either the code or the documented intent changed

### 7. Right layer, right scope

Documentation belongs at the layer closest to the behavior it describes:

| Layer | Use when... | Example |
|-------|-------------|---------|
| Docstrings / comments | Behavior is local to a function/type | "Returns nil if cache miss; never blocks" |
| File headers | File is an entry point or orchestrator | "Docs: [[Search (Hub)]]" |
| `CONTEXT.md` | Explaining how files in a directory fit together | Scope, invariants, public surface |
| Notes + hubs | Cross-cutting design, runbooks, operational hazards | Architecture decisions, failure modes |

See [[Rhizome documentation - Layering + authoring workflow]] for the full model.

### 8. Bind docs to code explicitly

Docs that aren't bound to code don't get retrieved. Rhizome provides two binding mechanisms:

- **Coderefs** for "this code implements/follows this note"
- **Code anchors** for "this note applies to callers/dependents of this symbol"

The choice matters:

- Use **coderefs** when the code is the natural home for the link (entry points, non-obvious behavior)
- Use **code anchors** when you want docs to surface for *all users* of a symbol, not just the definition site

See [[documentation-binding-rationale]] for design rules that keep bindings high-signal.

## What this philosophy is not

- **Not "document everything"**: document what matters for safe changes. Code that's obvious from reading doesn't need prose.
- **Not "AI-only docs"**: docs should remain useful for humans, but optimize for agent consumption first.
- **Not "replace reading code"**: the goal is to make "what to read, and why it matters" obvious—not to substitute for understanding.
- **Not "documentation as afterthought"**: capture intent during development, not in a separate documentation phase.

## Agent behavior expectations

When an agent works in a Rhizome repository:

1. **Build context proactively**: run `file_context` on files you'll touch before reasoning about changes.
2. **Follow conventions automatically**: even if the user doesn't mention documentation, consult and respect it.
3. **Surface conflicts**: if a request contradicts documented design, ask whether the intent is to change the architecture.
4. **Maintain proactively**: update comments and linked notes when behavior changes, even if not explicitly asked.
5. **Confirm alignment**: for significant changes, verify that updated documentation reflects the user's goals.

## Examples

### Example: Performance-sensitive module

A user asks an agent to optimize a database sync module for throughput.

**What should happen**:
1. Agent documents in `CONTEXT.md` or a linked note: "This module is performance-critical. Inserts are batched and parallelized. Avoid blocking operations."
2. Agent adds invariants: "Batch size is tuned for connection pool limits. Don't reduce without load testing."
3. Next session, a different agent sees the constraints before adding a "convenient" synchronous logging call.

**Without Rhizome**: the next agent has no idea performance matters and adds blocking I/O. The optimization is silently undone.

### Example: Code anchors surfacing for callers

A note documents that `Service.Authenticate()` must be called before any operation that accesses user data. The security implications are non-obvious—callers might skip auth for "internal" operations without realizing the risk.

**What should happen**:
1. The note has a code anchor attached to the `Authenticate` symbol.
2. Any agent editing code that *calls* `Authenticate`—or should call it—sees the security constraint automatically.
3. The constraint surfaces even though the caller file has no coderefs of its own.

**Without code anchors**: only agents reading the `Authenticate` definition see the docs. Callers operate blind.

### Example: Subtle ordering invariant

A cache invalidation system requires that writes complete before reads are served.

**What should happen**:
1. The invariant is documented near the code: "Write must complete before read path is unlocked. Violating this causes stale reads."
2. A coderef links to a note explaining why: the original bug, the fix, and what would happen if violated.
3. An agent modifying the write path sees the constraint and preserves ordering.

**Without Rhizome**: the agent refactors for "cleaner" async patterns, breaks ordering, and introduces a subtle bug that only manifests under load.

## Enabling infrastructure

Rhizome operationalizes this philosophy through:

- **RHIZOME.md**: per-repo guidance for agents (tool usage, conventions, retrieval patterns)
- **Agent skills** ([[Agent Skills (Hub)]]): packaged workflows for common tasks
- **`rzm agent` commands**: programmatic access to the context graph (`vault-context`, `file-context`, `semantic-query`, `files`)

These should be internally consistent, encourage parallel tool calls for low latency, and guide agents to proactively build context from the knowledge graph.

## Related

- [[Rhizome Codebase Documentation Best Practices (Hub)]]
- [[Rhizome documentation - Layering + authoring workflow]]
- [[documentation-binding-rationale]]
- [[Rhizome documentation - Tool guide (agent CLI tools + tradeoffs)]]
- [[Vision + Operating Model (Hub)]]
