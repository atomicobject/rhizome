## Ontology interfaces

Use GraphQL `interface` and `implements` for shared contracts across concrete note types.

- Interfaces are abstract schema contracts, not note families.
- Do not put `@node` on an interface.
- Authored interfaces with a nonempty set of concrete implementors, all notes, get generated typed query roots with shared-field filters, sorting and pagination. Runtime interfaces (including `Note`), empty interfaces and interfaces with section or embedded implementors stay rootless.
- Use interfaces when multiple concrete note types should share the same authored fields or query shape.
- Keep interface fields small and stable so `query-schema`, `reference`, and `authoring-guide` can dedupe the shared contract cleanly.

### Authoring rules

- Keep field names, nullability, and list shape compatible across implementers.
- Prefer interfaces for shared authored contracts; prefer sections for body structure.
- Use inline fragments on interface-backed results for concrete fields; select runtime fields through `... on NoteNode { path resolvedType }`.
- Keep interface docstrings explicit about the shared contract and which concrete note families implement it.

### Example

```graphql
"""Shared summary-bearing contract for concrete note types."""
interface Summarized {
  """One-line note summary."""
  summary: String!
}

"""Decision note that implements the shared summary contract."""
type Decision implements Summarized @node(matches: ["tag:type/decision"]) {
  summary: String!
}
```
