# Structured Markdown

The live ontology defines valid structure and declared semantics; representative notes show usage, not automatic authority. Do not infer a note type, root, field, relation, identifier strategy, heading shape, or lifecycle value from memory or bundled conventions; discover the configured contract. Reuse still-current discovery rather than repeating it for every edit.

## Authoring sequence

1. Inspect the existing note or intended finder with `rzm agent ontology-inspect --input <note-or-finder>` to resolve its type.
2. Load `rzm agent ontology-authoring-guide --type <TypeName>` and read its companion docs and validation annotations.
3. When relationships affect the edit, list saved recipes and use one that returns the relevant typed neighborhood. Reuse known, current recipe contracts and results when sufficient.
4. If needed context is still missing and no recipe fits, inspect `rzm agent ontology-query-schema`, then run a bounded `ontology-query` using only live roots and fields.
5. Inspect one or two representative notes of the resolved type.
6. Draft or revise without destroying unrelated prose or frontmatter.
7. Validate the note type, identifiers, links, and any relevant recipe/view contract.

Typed roots require a supported selector such as a path, a `find:` title/filename selector, a property, or semantic input as shown by the live schema. Treat ontology docstrings as concise semantics and authoring-guide/companion output as the deeper drafting contract.

Treat required heading order and subtree boundaries as schema, not prose style. Write note-level inline properties as `key:: value` and embedded-node metadata as one `- key:: value` bullet per field; omit unknown nullable values rather than inventing placeholders.

When a type requires an allocated identifier, run `rzm agent next-id --type <Type>` before drafting; allocate batches with `--count <N>`. If the type does not support or require allocation, do not invent an id. Mirror an identifier into `aliases:` only when the type contract requires it, and make durable display links target the real note path or block target—not the alias alone.

If no configured type fits, inspect the permitted ordinary Markdown/documentation path; do not invent a type or change the ontology without authority. Choose an existing useful home before creating a note, and preserve attribution and uncertainty.

If the task is owned by a more-specific writing or workflow skill, let that skill decide the deliverable and use this reference only for Rhizome mechanics. Schema changes belong to the higher-risk ontology-authoring route.
