## Ontology revision workflow

When changing an ontology, follow a small, safe workflow:

1. **Inspect first.** Pull the current schema and example notes before editing.
2. **Choose or adapt a starter shape.** If the repo is still designing its note model, start from a bundled example and adapt it to the repo's actual entities/workflows.
3. **Edit minimally.** Smallest coherent schema change; avoid speculative fields.
4. **Validate.** Typecheck schema + notes; spot-check agent-facing rendering.
5. **Plan migrations.** If existing notes will become invalid, call the migration out explicitly.

### Inspect first

- `rzm ontology query-schema` — current executable SDL (with descriptions).
- `rzm ontology reference` / `rzm ontology authoring-guide` — how the type surfaces for agents.
- `rzm ontology inspect <path-or-finder>` — per-note assessment, fields, and current relations.
- `rzm agent files --input "notes/**/*.md" --include-frontmatter` — real frontmatter usage.
- `rzm agent list-properties` — frontmatter key frequency.
- Bundled starter ontologies: compare the starters `rzm init` offers with the repo's needs before inventing new note families from scratch.
- [[Ontology interfaces]] — shared contract and `implements` guidance.
- [[Ontology sections]] — heading-derived subsection contracts and query shape.

### Validate after edits

- `rzm ontology validate` — schema + typed-note validation, unresolved internal note-link checks, and per-type note inventory.
- `rzm ontology query-schema` — confirm the executable SDL still compiles.
- `rzm ontology reference` — confirm descriptions, `contextInclude` flags, and companion-doc links surface correctly.
- `rzm ontology inspect <representative-note>` — spot-check the authoring contract for a real note.
- `rzm ontology query --query '{ ... }'` — smoke-test typed roots if query ergonomics changed.
- `rzm ontology query --query '{ ... on InterfaceName { ... } }'` — smoke-test interface fragments if you added shared contracts.
- `rzm ontology inspect <representative-section-note>` — confirm section contracts, heading levels, and required subtree structure render as expected.
- If you introduced a new note family, verify the companion guidance notes still match the type docstring contract.

### Migration questions

- Are there typed notes in the wrong paths?
- Do notes still match the type's `paths` / `matches` selectors?
- Will any required field become missing?
- Are relation targets still the expected types?
- Did a structural relation become ambient, or vice versa?
- If you added an interface, do all implementers still expose the shared field shape?
- If you added sections, do the exact heading text and level still match real markdown?
- If you added section-local traversal, are body links staying inside the intended subtree?
- Are type descriptions and field descriptions still accurate for downstream docs/agents?
- Are any notes affected by a `contextInclude` flip (added/removed context load)?
- Do any `@companionDocs` paths now point at stale, missing, or wrong note families?

### Agent guidance

- Preserve the single `type:` model on instance notes.
- Treat `type:` as optional intent/disambiguation, not as the sole membership rule.
- Prefer `matches:` for tag/property-defined types; `paths:` for folder-defined; combine when both hold.
- Prefer nullable-first authoring unless query-time strictness is explicitly wanted.
- Prefer adding new fields over renaming in place unless the migration is explicit.
- When introducing `@neighbors`, make sure the name reflects what callers will actually ask for in queries.
- When introducing section fields, keep the heading contract exact and document the subtree boundary clearly.
- When flipping `contextInclude` on, sanity-check that the target notes are token-dense — every flagged edge costs context budget.
- After adding a typed root or relation, add at least one example query or docs assertion to tests/fixtures.
- When changing interfaces or sections, update their owning companion/human references in the same change. Update the base `rhizome` ontology references only when the universal authoring or diagnosis procedure changes.
