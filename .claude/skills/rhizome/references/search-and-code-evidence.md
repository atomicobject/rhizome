# Search and code evidence

Choose the retrieval surface by the unanswered question. Reuse current evidence; stop when it supports the next decision. Treat search rank and graph relationships as relevance signals, applying the core evidence-authority rule before relying on retrieved claims.

## Broad discovery

Use `semantic-query` for question-shaped discovery across rationale, docs, notes, and representative code:

```bash
rzm agent semantic-query --session-id <id> \
  --query "how does request authorization work and why?"
```

Use a precise `--path` seed when the subsystem is known. Omit `--mode` normally; routing infers common overview and docs/code bridge cases. Use an explicit mode only as an advanced override discovered from `rzm agent surface`.

Warnings about ambiguous paths, degraded lanes, or timeouts are recall gaps. Narrow the request or switch to an exact surface instead of treating a broad fallback as proof.

## Exact note discovery

Use `rzm agent files` for literal paths, filenames, tags, frontmatter properties, backlinks, and a small graph expansion. When the candidate set could be large, discover with `--include-content false`, then request `--include-content true --input "<literal-path>"` only for selected paths.

## Typed and repeatable retrieval

Use a saved `query-recipe` or schema-validated `ontology-query` for typed notes, typed relationships, the notes-only `search(query:, type:)` root, `semantic:` note/node surveys, ontology metadata, and bounded known-path document/code/test projections. This GraphQL layer is not a unified arbitrary code-search API.

Inspect `query-recipe list` before inventing a query. If no recipe fits, inspect the live `ontology-query-schema` before writing GraphQL; never guess root or field names.

## Exact code proof

Use the code tools when the question names a definition, symbol, caller, callee, usage, or test proof:

- `code-symbol` for a definition or exact symbol lookup
- `code-references` for definitions, callers, and callees
- `code-symbol-context` for a compact proof packet around a symbol

Use `semantic-query --scope code` only for a broad implementation survey. It complements exact symbol evidence; it does not replace it.
