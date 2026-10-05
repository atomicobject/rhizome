---
type: TechnicalSpec
summary: "Defines source-shape support for embedded ontology nodes whose authored form is lighter than a markdown heading section, including list items and checkbox items."
id: SPEC-0059
spec-status: proposed
last-updated: 2026-07-11
aliases:
  - SPEC-0059
  - Non-section embedded node source shapes
---

# Non-section embedded node source shapes

## Summary

Rhizome's embedded ontology-node model should support authored spans that are smaller and lighter than heading-backed markdown sections. Some embedded nodes are substantial subdocuments with prose, fields, child sections, and linkable headings. Others are contextual facts, checklist items, action items, or compact criteria that belong naturally inside a list. The ontology should describe those nodes as typed embedded nodes without forcing authors to promote every item into a heading section.

This spec extends the embedded-node contract from "heading-derived section only" to "typed source span inside a parent note." A schema can declare that a contained embedded node is sourced from a markdown heading, list item, or checkbox item. Each source shape still produces canonical `NodeRef` identity, parent containment, typed fields, optional linkability, queryable rows, and edit-session ownership. The goal is to make list-style embedded nodes first-class without weakening the existing section-based model.

## Goals

- support embedded ontology nodes sourced from list items and checkbox items, not only markdown headings
- keep section-backed embedded nodes as the default for prose-heavy subdocuments
- let schema authors choose the source shape for each contained embedded-node collection
- expose checkbox checked state as a normal boolean field that views, queries, and edit sessions can use
- preserve canonical `NodeRef` identity, parent containment, graph relations, primary semantic retrieval, views, and link-target behavior for every embedded source shape
- support identifier-backed and fallback block locators for non-section embedded nodes when a node becomes externally cited
- keep authoring compact for contextual nodes while avoiding broad Dataview-style inline-property parsing inside every bullet by default

## Non-Goals

- changing every embedded-node authoring shape; spec-driven acceptance criteria are the first list-style adopter
- making list items the preferred shape for every embedded node
- treating every markdown list item or checkbox as an ontology node without an explicit schema match
- replacing heading-backed embedded nodes, structural sections, or current `@contains(level:)` behavior
- requiring every non-section embedded node to have an authored identifier or block locator
- defining the final UI for table-based completion workflows
- adding arbitrary Markdown table-row, callout, or HTML-span source shapes in the first slice
- changing canonical `NodeRef` identity semantics

## Requirements

### Source Shape Model

#### Must

- The ontology schema MUST distinguish an embedded node's source shape from its node role. `@node(locator: EMBEDDED)` continues to mean the node is contained inside a parent note; a separate containment/source declaration chooses whether children are discovered from headings, list items, checkbox items, or future source shapes.
- The existing heading-backed behavior MUST remain expressible and backward-compatible. Existing `@contains(level: H3|H4|H5, heading: ...)` declarations continue to discover section-backed embedded nodes exactly as today.
- A schema MUST be able to declare a contained embedded-node collection whose members are markdown list items under an owning section or parent node.
- A schema MUST be able to declare a contained embedded-node collection whose members are markdown checkbox items under an owning section or parent node.
- Non-section embedded nodes MUST have a source range covering the owning list item, including wrapped continuation lines and nested content that belongs to that item.
- Non-section embedded-node extraction MUST ignore list items in YAML frontmatter and fenced code blocks.
- A list or checkbox item MUST match a non-section embedded-node type only when the schema says how to recognize it, such as a required marker tag, an owning collection field, a property marker, or another explicit source-shape discriminator.
- Source shape names and source-shape matching MUST be stable enough for schema validation, generated authoring guides, query schemas, and saved query recipes.

#### Should

- The first source-shape vocabulary should include `SECTION`, `LIST_ITEM`, and `CHECKBOX_ITEM`.
- `CHECKBOX_ITEM` should be treated as a specialized list-item shape with a parsed checked token.
- The first runtime slice requires a marker such as `#action-item` for `LIST_ITEM` and `CHECKBOX_ITEM` containment. A later explicit section-scoped default may support "all list items in this collection section are this type" without marker omission becoming an implicit broad matcher.
- The schema surface should avoid overloading `@field(sourceKind: INLINE)` for bullet parsing. Bullet-owned fields need source-span-aware parsing so ordinary note-level inline-property discovery does not suddenly treat every list item as metadata.

### Field Extraction

#### Must

- Non-section embedded nodes MUST expose a content/text field derived from the authored item body when the schema requests it.
- Checkbox-source nodes MUST expose the checkbox token as a boolean field through a schema-declared field source, rather than through ad hoc status inference.
- The boolean checkbox field MUST preserve the distinction between checked and unchecked markdown tokens.
- Link fields on non-section embedded nodes MUST be able to resolve wiki links or markdown links inside the item span.
- Inline properties inside a non-section item MUST be scoped to that item span, not lifted to the containing note or sibling items.
- Field extraction MUST keep prose text, marker tags, assignee links, and locator tokens separable so views can display clean item text while still filtering by fields.

#### Should

- A field source like `CHECKBOX` or equivalent should be available for boolean fields whose value comes from the checkbox marker.
- Schema authors should be able to choose whether marker tags remain part of the display text or are treated as hidden classification markers.
- The parser should support common item-local field syntax such as `assigned-to:: Some Person` only within a matched item span, without changing global inline-property behavior.
- Checkbox completion should be representable as a boolean field even when a specific ontology later derives a domain enum such as `open` or `closed`.

### Identity And Linkability

#### Must

- Non-section embedded nodes MUST receive canonical `NodeRef` identity with note path, node kind, resolved type, parent containment, source locator, structural fingerprint, and source byte range.
- Non-section embedded nodes MUST participate in type-instance listing, GraphQL ontology query results, views, primary semantic retrieval, and graph/read-model outputs the same way section-backed embedded nodes do.
- Non-section embedded nodes MUST preserve parent containment so a result can navigate back to the source meeting, spec, or owning note context.
- The link-target service MUST support non-section embedded nodes. It MUST be able to report an existing durable target, preview a non-durable target, or plan/apply a locator fix when the caller explicitly requests linkability.
- Identifier-backed linkability MUST work for non-section embedded nodes whose schema declares a preferred identifier field.
- Identifierless non-section embedded nodes MUST be able to receive plain fallback block locators on demand when an external citation requires a durable target.
- Default validation MUST NOT require identifiers or block locators for uncited non-section embedded nodes.

#### Should

- Fallback locators for list-style nodes should be inserted at a source-shape-appropriate location, such as a trailing Obsidian block identifier on the same item or an item-owned continuation line, as long as the shape remains compatible with Obsidian block links.
- Identifier-backed locators for list-style nodes should prefer the declared identifier field when that field exists and can be written without obscuring the item text.
- Structural fingerprints for non-section nodes should avoid depending solely on sibling ordinal when stronger authored material is available, but they must remain robust when an uncited item has no durable locator yet.
- The authoring guide should explain that list-style embedded nodes can be contextual and need not carry identifiers until the schema or citation lifecycle requires one.

### Editing And Replay

#### Must

- Edit replay MUST be able to target a non-section embedded node by canonical identity and source range.
- Editing the checkbox boolean field MUST toggle only the markdown checkbox token owned by the target node.
- Editing item-local fields MUST preserve surrounding prose, marker tags, links, continuation lines, sibling list items, and parent section structure.
- Linkability fix-up for non-section nodes MUST route through the same source-preserving edit-session path as section-backed block-ID insertion.
- Replay MUST fail closed when the target item cannot be matched to the current source range, when sibling membership/order drift invalidates the operation, or when the requested source-shape edit would consume sibling content.

#### Should

- Checkbox toggles should be idempotent: setting a checked item to checked again produces no source change.
- Multi-item edits in one file should apply from bottom to top or through an equivalent span-safe strategy.
- Conflict diagnostics should name the source shape so callers can explain whether a section, list item, or checkbox item drifted.

### Views, Queries, And Agents

#### Must

- Configured views MUST be able to use non-section embedded-node types as ontology-backed row sources.
- Rows for non-section embedded nodes MUST preserve canonical `NodeRef`, source path, parent context, title/display text, resolved type, fields, and updated state.
- Filters and sorts MUST be able to target checkbox boolean fields, assignee/link fields, text fields, date fields, and locator state when the schema exposes them.
- Agent query recipes MUST be able to query non-section embedded-node types without knowing which aggregate file or source note contains them.

#### Should

- A view over action-item-like nodes should be able to filter by assignee, open/closed state, date range, and source conversation.
- Primary semantic chunks should carry enough bounded parent context to explain where a lightweight item came from without embedding the entire parent note in every result.
- Configured views over embedded-node types should work regardless of whether the source shape is section-backed, list-backed, or checkbox-backed; generated defaults may stay conservative until they intentionally include inline and checkbox source fields.

### Documentation And Authoring

#### Must

- Ontology schema authoring guidance MUST document the supported source shapes, matching rules, field-source options, locator lifecycle, and examples for list/checkbox embedded nodes.
- Authoring guides MUST make clear that section-backed embedded nodes remain the right shape for prose-heavy subdocuments.
- Authoring guides MUST warn that ordinary bullet lists are not indexed as embedded nodes unless a schema declaration and matching marker make them eligible.
- Starter template examples SHOULD include at least one non-section embedded-node type after the source-shape runtime exists.

## Related Contracts

- [Structural node model and ontology read path](structural-node-model-and-ontology-read-path.md) owns the shared node identity model across file-backed nodes, structural sections, and embedded nodes.
- [Linkable embedded node identifiers](linkable-embedded-node-identifiers.md) owns durable external locator lifecycle for embedded nodes. This spec extends the source shapes that may need that lifecycle.
- [Ontology edit replay conflict contract](ontology-edit-replay-conflict-contract.md) owns source-preserving mutation and conflict behavior.
- [Ontology browser workspace](../product/ontology-browser-workspace.md) owns the user-facing workspace and view behavior over canonical node identity.

## Examples

### Checkbox Action Item

This is an illustrative shape, not a requested ontology change:

```md
## Action Items

- [ ] Update SPEC-0042 to represent the new ETL constraints from 2026-05-05-sync-with-smes assigned-to:: Gabe #action-item
```

A schema could model the item as a `CHECKBOX_ITEM` embedded node with:

- `text`: display body with metadata markers stripped according to schema policy
- `done`: boolean from `[ ]` / `[x]`
- `assignee`: link resolved from `assigned-to::`
- `references`: outbound links in the item body
- parent relation: containing `Conversation` or source note

### List-Style Criterion

The spec-driven ontology now uses this shape for `AcceptanceCriterion` authoring:

```md
#### Acceptance Criteria

- The query returns open action items by assignee.
- The result links back to the source meeting.
```

A schema can model `AcceptanceCriterion` as markerless `LIST_ITEM` nodes under an acceptance-criteria collection, while preserving the same semantic type and locator lifecycle.

## Open Questions

- Resolved for the first runtime slice: source-shape declarations live on `@contains`; note-level item-shaped `@contains` declarations require an exact marker discriminator, while section-scoped item collections may omit the marker.
- Resolved for the first runtime slice: item-local scalar fields use Dataview-style `key:: value` inside the owned item content, and normal markdown/wiki links remain available to link extraction.
- Resolved for fallback locators: list-style nodes use a trailing `^id` on the owning item line when linkability is requested.
- Resolved for checkbox state: `CHECKBOX` is a `FieldSource` enum value for singular Boolean fields on checkbox-item embedded types.

## Documentation Plan

- Update ontology schema authoring docs and the `rhizome` ontology-authoring reference when the universal source-shape procedure changes.
- Update generated authoring guides so embedded-node types report their source shape and field-source rules.
- Update view/runtime docs if non-section embedded nodes require new generated default columns or field capabilities.
- Add code anchors or reference docs for the parser/source-shape implementation once the technical plan chooses modules.
