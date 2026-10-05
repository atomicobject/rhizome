## Ontology schema authoring

Ontology files live in `.rhizome/ontology/*.graphql`. They are committed to git.

### Required conventions

- One object type per note type.
- Instance notes may use `type: <TypeName>` as author intent / disambiguation, but actual type resolution comes from `@node`.
- Use `@node(paths: [...])`, `@node(matches: [...])`, or both on every concrete ontology type, except for one optional concrete `@node(default: true)` type.
- Use GraphQL `interface` / `implements` for shared contracts across concrete note types. Interfaces stay abstract and do not get `@node`.
- Use `interface Note` only for shared general note fields. Those fields must be nullable and apply to every file-backed note read through `NoteNode`. Do not declare `type Note`.
- Prefer frontmatter-backed scalar fields unless inline properties are already the repo norm.
- Use `@contains(...)` when body structure itself should be queried or validated as a typed subtree. Sections are not note families.
- Prefer nullable fields by default. Use GraphQL non-null only when query-time strictness is worth the failure surface.
- Add GraphQL triple-quoted descriptions on note types and important fields. Those descriptions are load-bearing — they feed `rzm ontology reference`, `rzm ontology authoring-guide`, and the `file_context` type summary.
- Choose and adapt a starter ontology when possible. Prefer evolving a bundled example over inventing an ontology from scratch for each repo.

### Directives

- `@node(paths:, matches:, label:, color:, keyField:, locator:, default:)`
  - defines which notes belong to a type and how the type should be presented.
  - `paths` is best when the type is folder-shaped.
  - `matches` is best when the type is semantic and should follow Rhizome list-style selectors.
  - `locator: FILE` (default) means a standalone note/file-backed node.
  - `locator: EMBEDDED` means an embedded node persisted inside a parent note's body; embedded nodes must `implement Section`.
  - `default: true` is allowed on at most one concrete file-backed type and cannot be combined with `paths` or `matches`; it resolves otherwise-untyped notes after explicit selector/frontmatter resolution fails.
  - use plain `implements Section` without `locator: EMBEDDED` for structural-only sections that shape a document but are not first-class graph nodes.
  - `matches` accepts selectors like:
    - `notes/projects`
    - `tag:type/decision`
    - `classification:project`
- `interface` / `implements`
  - defines shared query contracts across concrete note types.
  - use when several concrete types share the same authored fields or query shape.
  - `interface Note` is special: fields must be nullable and are injected into every file-backed note type; `NoteNode` exposes them on typed and otherwise-untyped note reads.
  - keep interfaces abstract; they do not get `@node` or query roots.
- `@field(source:, sourceKind:)`
  - maps scalar/enum fields to frontmatter or inline properties.
- `@identifier(preferred:, strategy:, prefix:, pad:, separator:, derivable:, derivedSuffix:, populate:)`
  - marks a scalar `String` or `ID` field as a stable identifier for the note (for example `id: SPEC-0001`).
  - `strategy: SEQUENTIAL` allocates the next monotonic number and may use `pad:`. `strategy: DATETIME` derives `YYYY-MM-DD-HH-MM` from an ordered prospective vault-relative path and uses unpadded `-2`, `-3`, ... collision suffixes; `pad:` is invalid for datetime.
  - the value must also appear in the note's `aliases:` frontmatter list — `rzm agent validate identifiers` enforces this. Unique aliases resolve as author-facing wikilinks, so prefer `\[\[SPEC-0001\]\]` for stable whole-note identifier links and use `\[\[path/to/note|SPEC-0001\]\]` only when disambiguation is needed.
  - `preferred: true` marks the canonical identifier. At most one per note type, and preferred values must be unique across the vault. The preferred identifier is the recommended display text for links to that note, not the wikilink target.
  - only valid on scalar, non-list `String` or `ID` fields. A file-backed note's identifier must come from frontmatter; section and embedded-node identifiers may come from frontmatter or inline properties.
- Embedded preferred identifiers (`@node(locator: EMBEDDED)`) may use an inline `id` field with `@identifier(preferred: true, derivedSuffix: "<TAG>", populate: ON_CREATE|ON_LINK)`. The `derivable:` arg defaults to `true` for embedded preferred-identifier fields: the semantic id is derivable from `${parent.id}-${derivedSuffix}${n}`. The `populate:` arg controls markdown materialization. Use `populate: ON_CREATE` when authors must write `- id:: ^...` as soon as the node is created. Use `populate: ON_LINK` when the field should stay derived until an external citation needs a durable target. If the embedded type has no `@identifier`, do not invent `id::`; link it with a plain standalone `^block-id` minted by `rzm agent node-link --ensure plan`.
- Choosing `derivedSuffix:` — short single-word type names get cap'd (e.g. `Spec` → `"SPEC"`); multiword names get acronymized (e.g. `UserStory` → `"US"`, `AcceptanceCriterion` → `"AC"`). Stacked derivations preserve the no-separator convention: `SPEC-0023-US1-AC2`, never `SPEC-0023-US-1-AC-2`.
- When a section-backed embedded node has a preferred identifier field and carries a durable external link, prefer a metadata bullet like `- id:: ^BLOCK-SAFE-ID` over a separate standalone anchor line; projection strips the caret from the field value and exposes `#^BLOCK-SAFE-ID` as the block target. When the embedded node has no identifier field, the durable target is a standalone `^block-id` line.
- `@link(source:, sourceKind:, inverse:, includeBodyLinks:, includeBacklinks:, contextInclude:)`
  - defines structural relations. Structural links drive validation and typed traversal.
  - `contextInclude: true` auto-includes the target note in `file_context` whenever the source note is opened.
- `@neighbors(direction:, type:, scope:, contextInclude:)`
  - defines ambient typed neighbor sets from links/backlinks. Use for concepts like `decisions`, `discussions`, or other "linked to/from" collections.
  - `contextInclude: true` auto-includes the target notes in `file_context` whenever the source note is opened.
- `@contains(level:, heading:, shape:, marker:, required:, min:, max:)`
  - binds a field to embedded body structure that should be typed and queryable.
  - `shape: SECTION` is the default and binds to an exact markdown heading plus exact heading level.
  - `shape: LIST_ITEM` and `shape: CHECKBOX_ITEM` declare lighter embedded nodes sourced from markdown list items. Note-level item sources require `marker:` so ordinary bullets do not become ontology nodes. Section-scoped item collections may omit `marker:` when every direct child item in that section should become that typed node.
  - `min:` and `max:` validate collection cardinality on list-valued `@contains` fields. Use them to make required child content explicit, for example "a user story has at least one acceptance criterion".
  - target a concrete section type or the built-in `Section` contract; arbitrary interfaces are not supported here.
  - pair with specialized section object types when a heading subsection needs its own fields.
  - if the target type uses `@node(locator: EMBEDDED)`, the contained items are embedded graph nodes; otherwise they are structural sections only.
- `@title(pattern:, notPattern:)`
  - validates note or embedded-node titles with regular expressions.
  - use this for heading contracts that agents can follow mechanically, such as `US1 - Outcome-oriented story title`.
  - keep patterns clear and anchored; use `notPattern:` to reject known bad legacy phrasings without over-constraining good titles.
- `@authoring(style:)`
  - validates how an embedded scalar/enum field is authored in markdown.
  - `LIST_METADATA` means unordered metadata bullets such as `- id:: ^SPEC-0001-US1`.
  - `INLINE_PROPERTY` means ordinary Dataview-style inline properties such as `due:: 2026-06-02`.
  - `CHECKBOX` means the value comes from a checkbox-item token.
  - prefer this for default authoring style, not for semantic meaning.
- `@format(pattern:, notPattern:)`
  - validates scalar/enum values with regular expressions.
  - use it for durable identifier shapes, short lifecycle guardrails, or migration rejection; avoid opaque regexes that agents cannot explain.
- `@requiresWhen(field:, equals:, require:)`
  - validates object-level conditional requirements.
  - `field:` names a scalar/enum trigger field on the same type; `equals:` is the trigger value.
  - `require:` lists fields that must be present, optionally with an expected value.
  - use this for schema-owned lifecycle constraints, such as superseded notes requiring a successor link.
- `@view(label:, order:, collapsed:, tone:, stage:)` on an enum value
  - presentation and lifecycle metadata for views; it never changes what a note authors.
  - `stage:` is `open`, `active`, `done`, or `dropped`. Views read it for behavior: `active` is in motion and can go stale, `done` is finished, `dropped` is collapsed by default. Declare it on every value of an enum or on none; a partial enum fails to compile.
  - `tone:` (`neutral`, `info`, `progress`, `success`, `warning`, `risk`, `muted`) only chooses color and icon. Omit it when it matches the stage default (`open` neutral, `active` progress, `done` success, `dropped` muted).
  - an enum without stages gets inferred ones from authored tone and `collapsed:`, but only declared stages change tone or collapse. Declare stages on workflow enums rather than relying on inference.
  - example: `ready @view(label: "Ready", order: 20, stage: "open", tone: "info")`.
- `SectionLevel`
  - enum for heading depth: `H1` through `H6`.
- `EmbeddedSourceShape`
  - enum for embedded source spans: `SECTION`, `LIST_ITEM`, `CHECKBOX_ITEM`.
- `FieldSource`
  - enum for authored field sources: `FRONTMATTER`, `INLINE`, `CHECKBOX`, `ITEM_TITLE`, `ITEM_SUMMARY`, `ITEM_DETAIL`.
  - use `@field(sourceKind: CHECKBOX)` only on a singular `Boolean` field inside a checkbox-item embedded type; edit replay toggles only the owned `[ ]` / `[x]` token.
  - use `ITEM_TITLE`, `ITEM_SUMMARY`, and `ITEM_DETAIL` only on singular `String` fields for list-item or checkbox-item embedded types. `ITEM_TITLE` reads an optional bold prefix such as `**Title**: summary`; `ITEM_SUMMARY` reads the first-line outcome without that prefix; `ITEM_DETAIL` reads indented continuation text such as Gherkin, callouts, or nested examples.
- `FieldAuthoringStyle`
  - enum for style validation: `ANY`, `FRONTMATTER`, `INLINE_PROPERTY`, `LIST_METADATA`, `ITEM_TEXT`, `CHECKBOX`.
- `@companionDocs(paths:, purpose:)`
  - attaches durable workflow/reference notes that `rzm ontology authoring-guide` should surface with the type or field.
  - use this when examples, vocabulary, or procedures would bloat the type docstring.
  - keep the list short and durable; prefer note paths with `summary:` frontmatter so the authoring guide can surface high-signal blurbs.
- `@semantics(kind:)`
  - lightweight structured metadata for field/type intent.
  - use `BEHAVIORAL` for fields that drive retrieval, dispatch, or tooling behavior.
  - use `DOCUMENTARY` for fields that mainly capture narrative/reference context.
- `@retrieval(intents:, boost:, relationBoost:, propertyBoost:)`
  - optional retrieval metadata for search/query orchestration.
  - `intents` narrows when the hint applies.
  - `boost` is a general score multiplier hint.
  - `relationBoost` and `propertyBoost` target relationship/property expansion specifically.
  - current status: preserved and rendered in ontology reference output, but not yet enforced by the ontology retriever.
- `@traversal(intents:, includeAmbient:, maxDepth:, minStructuralHits:)`
  - optional traversal-policy hints for ontology graph navigation.
  - use this for sparse-hit fallback controls, explicit ambient-link policy per intent, and bounded multi-hop traversal from typed seeds.
  - current status: `includeAmbient`, `minStructuralHits`, and `maxDepth` are operational in ontology retrieval.

### `contextInclude` is budgeted

`contextInclude: true` spends context budget on every caller that opens a note of the source type. Flip it on only when agents almost always need the targets to read or draft the source. Keep flagged targets token-dense — a bloated auto-included note hurts every caller.

### Type docstring contract

For project-operating-system ontologies, type docstrings should answer:

- what this note is,
- when to create one,
- required body structure,
- what should be read with it,
- when it must be updated,
- what agents should preserve.

Field docstrings should explain why the field exists and what discipline it enforces. Use prose for workflow guidance before inventing new directives.

### Modeling guidance

- Structure the key semantics; keep supporting explanation and rationale in prose plus links unless the relationship is canonical enough to deserve a field.
- Use descriptions for prose semantics. Prefer natural language there over inventing many custom directives.
- Use interfaces for shared authored contracts and sections for body structure. Do not model body structure as frontmatter or inline props when the heading hierarchy matters.
- Use plain section types for tabs/prose structure, and `@node(locator: EMBEDDED)` only for embedded objects that should behave like first-class graph/query/edit nodes.
- When a section-backed embedded node uses inline properties, teach metadata bullets by default: one `- key:: value` line per field. This keeps embedded node metadata visually separate from prose while still parsing as scoped inline fields.
- Keep note-wide inline properties as plain `key:: value` lines. Bullets outside a typed embedded-node property block should still mean prose/list structure unless a schema field explicitly uses `@contains(shape: LIST_ITEM|CHECKBOX_ITEM, marker: ...)` for note-level embedded nodes or markerless `@contains(shape: LIST_ITEM|CHECKBOX_ITEM)` inside a containing section.
- Use `@companionDocs` for linked workflow/reference notes; do not overload `contextInclude` just to make authoring docs visible.
- Use `@link` when the relation is part of the note's canonical authored structure.
- Use `@neighbors` when the relation should mean "all typed notes linked or backlinked here via ordinary note prose".
- Use `@neighbors(direction: OUTBOUND, scope: SUBTREE)` for section-local body-link traversal when that subtree should stay self-contained.
- Do not use `INBOUND` or `BOTH` on section-local neighbors yet; sections are not link-addressable enough for inbound section semantics to be reliable.
- Do not mix structural and ambient meaning into one field.
- Name relations by what callers will ask for (`decisions`, `runbooks`, `owners`).
- Prefer adding new fields over renaming unless the migration is explicit.
- In v1, keep workflow behavior lightweight: use docstrings, `contextInclude`, and existing retrieval/traversal hints instead of adding a second workflow language to the schema.

### Example

```graphql
"""Decision record that captures a durable project choice."""
type Decision @node(matches: ["tag:type/decision"]) {
  """Short decision title."""
  name: String
}

"""
Project hub note.

Collects authoritative design notes, runbooks, and entry points for one project.
Agents should read the owner and any context-include decisions first.
"""
type Project @node(paths: ["notes/projects/*.md"]) @semantics(kind: BEHAVIORAL) {
  """Human-friendly project name."""
  name: String
  """Owning person — pulled automatically when reading the project."""
  owner: Person! @link(inverse: "projects", contextInclude: true)
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}

"""
Structural-only section for a note's requirements block.

Use this shape for a section that collects requirements, decision links, or
other section-local structure. Keep links inside the section subtree when the
subtree is meant to stand on its own.
"""
type RequirementsSection implements Section {
  """Section-local decisions gathered from the subtree."""
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE, contextInclude: true)
}

"""
Embedded user story node persisted inside a spec note.

Use this when a heading-derived subtree should behave like a first-class graph
object even though it lives inside another note.
"""
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
  effort: Effort @link
}

"""
Embedded action item sourced from a lightweight checkbox list item.

Use this pattern for contextual commitments that should be queryable without
turning every item into a heading-backed subsection.
"""
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  text: String @field
  assignee: Person @link
}

"""
Spec note with a typed requirements section.

Create one when the repo needs a durable document that treats prose structure as
part of the contract. The section heading, level, and subtree boundaries are
load-bearing and should be kept in sync with the query/authoring guide.
"""
type Spec @node(matches: ["tag:type/spec"]) {
  """Requirements section for this spec."""
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
```

Section builtins are implicit. The compiler provides `Section`, `title`, `content`, `children`, `id`, `notePath`, and `level`, so do not redeclare them. Query a section through its owning note, and keep its heading text and level exact.
