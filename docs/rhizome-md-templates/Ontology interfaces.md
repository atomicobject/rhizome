## Ontology interfaces

Use GraphQL `interface` and `implements` for shared contracts across concrete note types.

- Interfaces are abstract schema contracts, not note families.
- Do not put `@node` on an interface.
- Do not create query roots for interfaces; roots stay concrete note types.
- Use interfaces when multiple concrete note types should share the same authored fields or query shape.
- Keep interface fields small and stable so `query-schema`, `reference`, and `authoring-guide` can dedupe the shared contract cleanly.

### Authoring rules

- Keep field names, nullability, and list shape compatible across implementers.
- Prefer interfaces for shared authored contracts; prefer sections for body structure.
- Use inline fragments on interface-backed results when the selected value can be one of several implementing types.
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
