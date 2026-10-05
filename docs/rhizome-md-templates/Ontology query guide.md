## Ontology query guide

Rhizome generates a read-only GraphQL query schema from the ontology.

### Root queries

- Generic:
  - `note(path: ...)`
  - `notes(type:, find:, property:, semantic:, first:)`
- Typed:
  - one singular root per ontology type, returning a typed list
  - example: `project(path:, find:, property:, semantic:, first:)`

### Why typed roots exist

Typed roots avoid `... on Project` in the common case:

```graphql
{
  project(path: "notes/projects/roadmap-refresh.md") {
    name
    decisions { title }
  }
}
```

### Interfaces

Use interfaces for shared contracts across concrete note types. The query schema should expose them once, then let implementing types reuse the shared field surface.

- Interfaces are not root queries.
- Concrete typed roots still return the concrete note type.
- Use inline fragments on interface-backed results when the selected value can be one of several implementing types.

Example:

```graphql
{
  note(path: "notes/projects/roadmap-refresh.md") {
    ... on Summarized {
      summary
    }
  }
}
```

### Retrieval rules

- For typed roots, use exactly one of:
  - `path`
  - `find`
  - `property`
  - `semantic`
- `first` defaults to `20`, max `200`.
- `notes(...)` still supports generic dynamic retrieval when the type is not known in advance.

### Built-ins on every type

- `path`
- `title`
- `content`
- `frontmatter`
- `tags`
- `linked(type:)`
- `backlinked(type:)`
- `connected(type:)`

### Sections

Sections are synthetic, heading-derived nodes nested under a note or another section. Query them through the owning field, not a standalone root.

- A section field should target a concrete section type or the built-in `Section` contract.
- Arbitrary interfaces are not valid section targets yet.

Section fields should expose:

- `title`
- `content`
- `children(first:)`
- any specialized fields declared on the section subtype

Example:

```graphql
{
  spec(path: "notes/specs/search-rewrite.md") {
    title
    requirements {
      title
      content
      children(first: 10) {
        title
      }
      decisions {
        title
      }
    }
  }
}
```

### Semantics

- Generated `@link` fields resolve structural ontology edges only.
- Generated `@neighbors` fields resolve ambient typed neighbors only.
- `@neighbors(direction: OUTBOUND, scope: SUBTREE)` keeps body-link traversal inside a section subtree when the section contract needs to stay self-contained.
- Inbound or `BOTH` subtree neighbors are not supported yet because sections are not directly addressable link targets.
- `linked/backlinked/connected` are generic ambient escapes.
