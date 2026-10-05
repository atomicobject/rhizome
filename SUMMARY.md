# Rhizome — What It Is and Why It Exists

## The Problem

AI coding agents are good at implementing what you ask in a session. The problem is that **intent evaporates between sessions**. Decisions, invariants, and architecture live in Slack threads and people's heads. The next agent rediscovers them — or violates them. Every session starts from scratch.

The standard workaround is to re-paste context at the top of each conversation. That doesn't scale, drifts from the truth, and puts the burden on the human to know what context matters right now.

## What Rhizome Does

Rhizome reifies human intent into the codebase itself — making knowledge a first-class artifact that agents can retrieve automatically, at the moment of work, without being told where to look.

It does this by building a **unified knowledge graph over code and documentation**, binding notes to code (and vice versa), and exposing that graph through a CLI agents can call as tools.

When an agent opens a file, it can ask Rhizome: _what docs, constraints, decisions, and architecture apply here?_ Rhizome returns the right 1–3 documents — the ones that contain the invariants, edge cases, and ownership boundaries that keep changes safe.

## The Core Mechanism: Bidirectional Binding

The distinctive design choice is binding code and docs in both directions:

| Direction | Mechanism | What it means |
|-----------|-----------|---------------|
| Code → Note | **Coderefs** — wikilinks and `@mentions` in comments/docstrings | "this code follows this contract" |
| Note → Code | **Code anchors** — frontmatter rules on symbols or paths | "this contract applies to any caller of this symbol" |

Bound docs surface automatically through file context and search. Unbound docs don't. This makes binding a deliberate, high-signal act: if a doc doesn't explain something an agent needs to know before touching the code, it doesn't need to be bound.

## Key Components

### `rzm` — the CLI

The core tool. It indexes, searches, and exposes retrieval through subcommands:

- `rzm index` — builds or refreshes the unified note+code index (stored locally in `.rhizome/db.sqlite`)
- `rzm search` — answer-oriented retrieval across notes and code
- `rzm agent ...` — agent-facing retrieval tools (start, file-context, semantic-query, validate, etc.)
- `rzm start` / `rzm serve` — long-running server; keeps the index fresh as files change and serves the web UI
- `rzm note ...` — note lifecycle: create, move, rename, tag, properties (with backlink rewrites)
- `rzm init` — repo setup: generates `.rhizome/config.yml`, managed agent docs, shared skill bundles

### The Index

Everything lives in a single SQLite database per vault/repo:

- **Notes** — Markdown files with their frontmatter, tags, wikilinks, and backlinks
- **Code** — source files indexed by language (Go, TypeScript, Python, C#, and more), with symbol tables, call graphs, and code references to notes
- **Embeddings** — optional semantic vectors (OpenAI, Voyage, or Ollama) for similarity search
- **Graph signals** — PageRank on code, HITS on notes, community clustering across the unified graph
- **Ontology** — typed note families defined in GraphQL SDL, with relations, primary semantic chunks, and query recipes

### The Web UI

Served by `rzm start`, accessible at `localhost:8787` by default:

- Graph view with cluster coloring and authority-weighted node sizing
- Type browser: navigate typed notes by ontology type, see their relations and local graph
- Note pane: view note content, structural nodes, backlinks, and ontology metadata
- Search and validation panels
- Ontology edit sessions (staged changes before commit)

### MCP Server

`rzm mcp serve --tools <allowlist>` exposes selected shared Rhizome tools over stdio MCP, so clients can call the existing handlers without shell scripting.

### The Ontology System

Rhizome supports a typed note model defined in `.rhizome/ontology/*.graphql`. This lets you define structured note families — Spec, Effort, UserStory, Requirement, Person, etc. — with typed fields, relations, and identifier schemes.

Typed notes unlock:
- GraphQL queries over the note graph (`rzm agent ontology-query`)
- Source-owned semantic chunks enriched with typed identity and ancestry
- Configured table views (`.rhizome/views/*.yaml`) surfaced in the web UI
- Schema-driven id allocation (`rzm agent next-id`)
- Validation checks for ontology conformance, broken links, and drift

## The Operating Loop

```
Humans state intent
        ↓
Capture as notes + CONTEXT.md
        ↓
Bind code ↔ docs (coderefs + code anchors)
        ↓
Index (Rhizome builds the unified graph)
        ↓
Agent retrieves constraints before touching code
        ↓
Agent acts within those constraints
        ↓
Agent updates docs when behavior changes
        ↓
Updated docs inform the next session
```

The loop is the value. Retrieval and maintenance must both happen — if docs aren't updated when behavior changes, the cache rots and agents start working with stale contracts.

## The `rzm agent` Retrieval Surface

These are the commands agents call from skills and tool hooks:

| Command | Purpose |
|---------|---------|
| `rzm agent start` | Session bootstrap: surface + vault context in one call |
| `rzm agent file-context` | Relevant notes and docs for a given file or directory |
| `rzm agent semantic-query` | Hybrid search: answer packet with must-read evidence, coverage, confidence |
| `rzm agent files` | Exact note lookup by path, tag, property, or backlinks |
| `rzm agent validate` | Validation sweep: broken links, ontology, frozen-scope drift, etc. |
| `rzm agent ontology-query` | GraphQL queries over the typed note graph |
| `rzm agent next-id` | Allocate the next identifier in a typed note family |
| `rzm agent report` | Intelligence reports: doc coverage, complexity, hotspots, code similarity |

## Who It's For

Rhizome fits teams doing complex, long-horizon work where agents need to stay aligned with human decisions over time — not just "write me a function" tasks. It works best when:

- The codebase has real architecture that isn't obvious from code alone
- Multiple people (and multiple agents) work on overlapping areas
- Decisions get made that constrain future work and shouldn't be rediscovered on every session
- Documentation is alive and changes alongside code

The workflow templates bundled with `rzm init` — `agentic-engineering` and `complex-domain` — define typed note families, query recipes, and agent skill bundles for common team structures. A team using the spec-driven starter gets Spec, Effort, and UserStory typed notes, with lifecycle validation, traceability queries, and a delivery loop baked in.

## Getting Started

See [README.md](README.md) for full installation, configuration, and command reference.

Short version:
```bash
brew tap atomicobject/homebrew-tap && brew install rhizome
cd your-repo
rzm init          # detects your setup, asks a few questions, writes config + agent docs
rzm start              # starts the index watcher + web UI
```
