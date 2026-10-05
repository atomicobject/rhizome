## Skill ontology recipes

Use ontology surfaces to learn the repo's note operating model before you bake retrieval guidance into a skill.

### Inspection order

1. `rzm agent ontology-query-schema`
2. `rzm agent ontology-reference`
3. `rzm agent ontology-authoring-guide`
4. `rzm agent ontology-inspect --input <representative-note-or-finder>`
5. `rzm agent list-properties` plus `rzm agent files --include-frontmatter`

This sequence tells you:

- which typed roots exist,
- which fields/relations they expose,
- which shared interfaces and section contracts the schema models,
- which note families have companion docs,
- and how the repo actually authors those notes today.

### Query-building pattern

Never guess root or field names. Inspect the SDL first, then write the smallest query that answers the workflow.

```graphql
{
  <typedRoot>(find: "<activity or entity>", first: 5) {
    path
    title
    linked(type: "<NeighborType>", first: 10) {
      path
      title
    }
  }
}
```

Common uses:

- Find the primary note for an activity plus the linked decisions/runbooks/specs it depends on.
- Pull the typed neighborhood a skill should read before editing a note family.
- Confirm whether the ontology already models a workflow before inventing path-based heuristics.
- Query a note root and then drill into a section subtree or interface-backed fragment when a note's body structure matters.

When interfaces or sections are present, prefer the exact SDL names from `rzm ontology query-schema` and keep the query shape as small as possible.

### Saved query recipes

When a skill needs the same ontology query shape more than once, save it as a query recipe instead of leaving only prose instructions. A saved recipe gives future agents:

- the workflow `problem` the query answers,
- whether it is broad, optional-anchor, required-anchor, or multi-anchor,
- the GraphQL shape to run,
- the output contract for empty, partial, and high-volume results,
- and adaptation guidance for schema evolution.

Use:

```bash
rzm agent query-recipe list
rzm agent query-recipe validate
rzm agent query-recipe run --id <recipe-id> --anchor <path-or-entity>
```

Use `--path <skill>/references/query-recipes.md` for a skill-local recipe file. Shared repo recipes belong under `.rhizome/query-recipes/` and are tracked source, not ignored runtime state.

Recipe GraphQL uses declared GraphQL variables. Recipe inputs bind to the query's variable definitions and are validated against the live query schema before execution.

Run `rzm agent validate query-recipes` after ontology schema changes. If a recipe breaks, adapt it against its `problem`, `inputSpec`, `outputContract`, and `adaptationGuidance`; ask the user before changing the workflow purpose.

### When drafting typed notes

- Use `rzm agent ontology-authoring-guide` as the drafting contract.
- Read surfaced companion docs when the guide points to them.
- Use `rzm agent ontology-inspect` on a real note when you need to see how the schema resolves in practice.
- If the guide shows section contracts, treat the heading order and subtree boundaries as part of the schema, not just prose style.

### Fallback when ontology coverage is thin

If typed roots or relations are missing, fall back to:

- `rzm agent files --include-frontmatter`
- `rzm agent list-properties`
- `rzm agent semantic-query`

Then decide whether the skill should stay generic or whether the repo first needs ontology work.
