## Ontology overview

Rhizome ontologies live in `.rhizome/ontology/*.graphql`. They are repository config, not vault notes.

- **Purpose**: define a repo's typed note operating model: note families, structural relations, ambient neighbor conventions, retrieval defaults, and reusable schema docs for downstream tools/agents.
- **Authoring format**: GraphQL SDL plus Rhizome directives.
- **Runtime model**: Rhizome compiles SDL into an internal schema, assesses notes at index time, persists best-effort resolved types + relations in SQLite, and generates both a read-only GraphQL query schema and schema docs.
- **Type membership**: define with `@node(paths: [...])`, `@node(matches: [...])`, or both. `matches` uses the same note-selector shapes Rhizome list supports: path selectors, `tag:...`, and property checks like `classification:decision`.
- **Shared contracts**: use GraphQL `interface` / `implements` when multiple concrete note types share the same authored fields or query shape.

### Starter patterns

Use a starter ontology that `rzm init` offers as a base when a repo is still designing its note model; run `rzm init` to see the current list.

Adapt the starter to the repo. Do not treat the starter as a mandatory taxonomy.

### Core concepts

- **Typed note**: note with a single resolved ontology type. `type:` is an optional author-intent hint/disambiguator, not the only source of truth.
- **Abstract interface**: GraphQL shared contract for concrete note types. Interfaces are not note families, do not get `@node`, and do not create query roots.
- **Structural relation**: declared with `@link`; backed by frontmatter or inline properties; used for validation/inverses.
- **Ambient relation**: declared with `@neighbors` or discovered from body links/backlinks; used for exploration and query convenience, not structural validation.
- **Section**: synthetic heading-derived node inside a note body. Use `@contains(...)` when body structure itself needs to be queried or validated.
- **Section-local relation**: `@neighbors(direction: OUTBOUND, scope: SUBTREE)` keeps body-link traversal inside a matched section subtree. Inbound section-local neighbors are not supported yet.
- **`contextInclude: true`**: on `@link` or `@neighbors`, marks the relation as auto-include in `file_context`. Use sparingly — every flagged edge spends context budget across all callers.
- **`@companionDocs(paths:, purpose:)`**: attaches workflow/reference notes that `rzm ontology authoring-guide` should surface with the type or field contract.
- **Typed query root**: generated singular root per concrete ontology type, returning a typed list, e.g. `project(path: "...") { ... }`.
- **Schema docs**: GraphQL descriptions plus `@semantics(kind: ...)` metadata, surfaced to query clients and docs renderers.
- **Docstrings are the workflow contract**: each type docstring should explain what the note is, when to create it, body structure, what must be read with it, when to update it, and what agents must preserve.

### Modeling defaults

- Structure key semantics; keep explanatory context in prose.
- Use `@link` for canonical authored relations.
- Use `@neighbors` for typed discovery from ordinary links/backlinks.
- Embedded nodes should be first-class when they represent real conceptual objects rather than mere prose sections.
- Prefer a shared `id` field across important top-level types.

### Key commands

- `rzm ontology validate`
- `rzm ontology query-schema`
- `rzm ontology reference`
- `rzm ontology authoring-guide`
- `rzm ontology inspect <path-or-finder...>`
- `rzm ontology query --query '...' --variables-json '{"path":"docs/specs/example.md"}'`
- `rzm query-recipe validate --path <recipe-file-or-dir>`
- `rzm query-recipe run --path <recipe-file-or-dir> --id <recipe-id>`
- `rzm agent ontology-query-schema`
- `rzm agent ontology-reference`
- `rzm agent ontology-authoring-guide`
- `rzm agent ontology-inspect --input <path-or-finder>`
- `rzm agent ontology-query --json '{"query":"query($path: String!) { note(path: $path) { path title } }","variables":{"path":"docs/specs/example.md"}}'`
- `rzm agent query-recipe validate --path <recipe-file-or-dir>`

### Related docs

- [[Ontology interfaces]]
- [[Ontology sections]]
- [[Ontology schema authoring]]
- [[Ontology query guide]]
