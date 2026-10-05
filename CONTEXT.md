# Rhizome

Platform for AI-forward development: connects Markdown knowledge base + codebase so agents operate within constraints without re-learning each session.

## Language

**Base agent experience**: How agents discover and use Rhizome capabilities in a repository without selecting a workflow starter.

**Agentic Engineering starter**: The development workflow connecting specifications, bounded efforts, approved plans, implementation, verification, and durable knowledge.

**Complex Domain starter**: The Agentic Engineering extension for source-backed domain documentation, requirements mapping, and traceability in complex projects. The repository calls the complex-project extension `complex-domain`.

## Key concepts

- **Docs alongside code**: `docs/notes` organized with links, tags, hub notes
- **Code-docs binding**: coderefs (code→note) and code anchors (note→code) make `file_context` surface the right docs
- **Unified index**: `rzm index` + MCP watcher maintain SQLite index of notes, embeddings, graph signals

## Agent workflow

1. `vault_context` once to orient
2. `file_context` on files/dirs you touch
3. `semantic_query` for "how/why/where" questions
4. Update nearest doc surface when behavior changes

### Deep docs

- [[Vision + Operating Model (Hub)]]
- [[documentation-binding-rationale]]
