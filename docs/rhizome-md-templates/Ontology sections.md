## Ontology sections

Use sections when a note's body structure needs to be queried, validated, or authored as a typed subtree.

- Sections are synthetic heading-derived nodes inside a note, not standalone notes.
- `@contains(level:, heading:, required:)` binds a section-shaped field to an exact heading and exact heading level.
- `@contains(shape: LIST_ITEM|CHECKBOX_ITEM, marker:)` is the lighter source-span form for contextual embedded nodes. Note-level item sources require `marker:` so ordinary bullets stay untyped; section-scoped item collections may omit `marker:` when every direct child item in that section belongs to the typed collection.
- `SectionLevel` is the heading-depth enum for `@contains` values: `H1` through `H6`.
- Use section fields for prose structure such as `requirements`, `decisionLog`, or `operatingNotes`.
- Specialized section types should `implement Section` when a subsection needs its own fields or section-local relations.
- Section fields should target a concrete section type or the built-in `Section` contract, not an arbitrary interface.
- `@neighbors(direction: OUTBOUND, scope: SUBTREE)` keeps body-link traversal inside the matched section subtree.
- Inbound or `BOTH` subtree neighbors are intentionally unsupported for now.
- V1 section matching should be exact heading text plus exact level only.
- Validation should catch missing required sections, duplicate singular matches, wrong heading level, and empty required content.

### Authoring rules

- Write the heading exactly as declared in `@contains`.
- Keep the section body contiguous until the next heading of the same or higher level.
- Use child section fields for nested structure instead of inferring substructure from prose.
- Query section fields through the owning note type; do not make sections standalone roots.

### Example

```graphql
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE, contextInclude: true)
}

type Spec @node(matches: ["tag:type/spec"]) {
  """Requirements section for this spec."""
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
```

`Section` builtins are implicit: `id`, `notePath`, `title`, `level`, `content`, and `children(first:)`.
