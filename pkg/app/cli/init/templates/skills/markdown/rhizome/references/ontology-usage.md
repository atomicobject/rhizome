# Ontology usage

Use this route to understand, query, or diagnose the current ontology without changing its schema.

- `ontology-reference` explains live types, relations, directives, and companion guidance.
- `ontology-inspect` resolves the type and authoring context for a note or finder.
- `ontology-authoring-guide` gives the drafting/validation contract for a type.
- `query-recipe list/run/validate` exposes and checks saved, repeatable typed questions.
- `ontology-query-schema` is the authority before a one-off GraphQL query.
- `ontology-query` executes that schema-validated query.

Discover only the types and relationships needed for the current decision; reuse still-current schema and recipe knowledge. Valid shape and a successful query do not prove acceptance, truth, freshness, or complete coverage. Read relevant lifecycle and provenance semantics from the configured ontology without assuming starter vocabulary.

Prefer a saved recipe when it answers the same workflow question, accepts compatible inputs, bounds output, and documents adaptation. Otherwise inspect the schema and write a narrow query.

The public GraphQL layer supports typed note/relationship retrieval, a notes-only `search` root over the unified engine, `semantic:` note or node surveys, ontology metadata, authoring/schema metadata, and bounded known-path code/document/test evidence. It does not expose an `agent` root, does not replace broad mixed note/code `semantic-query`, and is not arbitrary code search. Use exact code tools for symbol/reference proof and `rzm agent surface` for agent-family capabilities.

Empty or partial typed results may mean the selector was too narrow, content is untyped, the index is stale, or the ontology does not model that relationship. Diagnose those separately before falling back to broad search.
