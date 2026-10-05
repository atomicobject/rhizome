# Ontology authoring

Changing `.rhizome/ontology/*.graphql` changes the project's typed-note contract and may require content migration. Treat it as a high-risk route and use any repo-local ontology workflow or subsystem guidance first.

## Design from use

Start with the questions people and agents need to answer and the actions they are authorized to take. Inspect representative notes and the screens or scripts that will consume them before designing fields. Keep prose readable in the body; add a structured property only for a concrete query, filter, sort, validation, relationship, or editing need. Preserve exact relationships for retrieval without turning every useful detail into metadata.

Missing optional information is context, not an obligation. An opportunity describes a problem, an idea describes an approach, and accepted work belongs in the project’s work records. Do not invent next steps, owners, deadlines, or review duties to fill a schema. Test the proposed model with realistic records and its actual consumers before migrating existing content.

## Before editing

1. Start/reuse a session and inspect `ontology-reference`, `ontology-query-schema`, and representative notes.
2. Identify affected types, interfaces, relations, directives, recipes, views, validations, and generated/installed template copies.
3. Decide whether existing content needs a migration or compatibility is intentionally being removed.
4. Preserve human intent in docstrings and companion docs at the appropriate layer; do not turn the base skill into a shadow ontology.

Use a docstring for concise "what is this?" semantics, `@guidance` for deeper authoring or preservation rules, `@policy` only for simple edit-behavior hints, and companion docs for longer procedures or examples. Treat meaningful embedded nodes as first-class graph/query objects. Use `contextInclude: true` only for relations needed in almost every source read or draft: each one consumes context budget.

Use field-level `@display` to shape presentation. A tooltip should help someone decide whether to open the note: keep the summary and a few identifying facts; use `hover: false` for ordering fields, bookkeeping, long explanations, and relationship lists that add noise. `role: SUMMARY` selects the hover preview's lead text; a field named `summary` supplies it by convention unless another field explicitly declares the role. Importance defaults to `NORMAL` and affects these surfaces:

- Hover previews render `KEY` fields as leading chips, then `NORMAL` fields, and omit `DETAIL` fields.
- The properties panel orders `KEY` before `NORMAL`, preserves the existing order within each group, and folds `DETAIL` fields behind a read-mode disclosure. Edit mode, validation issues, and missing required values reveal details.
- Generated views pick columns from the type profile (see the views reference). Native views without an authored column list default to title, identifier, and `KEY` fields in schema order, then Changed; `NORMAL` fields fill only leftover room under the default limit. `DETAIL` fields remain available in the column picker but are not initially visible.

`role: PARENT` makes a type's records a tree, which group views nest and roll up. Declare it on one single-valued `@link` field per note type whose target is the declaring type or an interface it implements, for example `parent: FeatureArea @link @display(role: PARENT)`. An interface may declare it for its implementors. Every type the target admits must be a note type, so section and embedded types cannot form parent trees. Any other placement is a schema compile error. Validation reports a parent chain that repeats as `parent_cycle`; fixing one means choosing which link to change, so ask rather than guess. Leave the field empty on top-level records.

`hover: false` suppresses a field only in hover previews; it does not affect the properties panel or configured views. A singular section field declared with `@contains` may use `@display(role: SUMMARY)` to supply readable body prose to indexed hover previews and native views. Query it as `summary { content }`; use `@contains(level: H2, heading: "Summary", display: INLINE)` when that prose should also appear directly in the note’s Structure view. Other section display roles and list-valued summaries are unsupported. Section summaries are edited through the section workspace, not as scalar properties.

Give lifecycle enums declared stages with `@view(stage:)`: `open`, `active`, `done`, or `dropped`, on every value or none. Stages drive view behavior (Board columns, stale and finished signals, collapsed `dropped` values); `tone:` only overrides color and defaults from the stage (`open` neutral, `active` progress, `done` success, `dropped` muted), so omit a tone that equals the default:

```graphql
enum TaskStatus {
  todo @view(label: "To do", order: 10, stage: "open")
  ready @view(label: "Ready", order: 20, stage: "open", tone: "info")
  doing @view(label: "Doing", order: 30, stage: "active")
  done @view(label: "Done", order: 90, stage: "done")
  cut @view(label: "Cut", order: 100, stage: "dropped")
}
```

Generated views derive a type profile from schema metadata, not field names: the lifecycle is the first enum field with declared stages (KEY fields first), else the first KEY enum whose authored tones or `collapsed:` infer stages. A lifecycle with an `active` value makes the type a workflow; one without makes it a contract; a KEY or required `Date`/`DateTime` makes it dated; an enum without stages makes it a catalog. Mark the fields that define a type `@display(importance: KEY)` and declare `role: SUMMARY` when the summary field is not named `summary`. Links to the core `Person` type count as people. Read a type's profile from its type documentation.

Use the default canonical authored property naming unless the live schema says otherwise. Add alternate property spellings only as explicit migration or compatibility inputs, never as an unexamined second contract.

## Identifier strategy migration

A user can request the complete workflow in plain language, for example: "Migrate the selected type's identifiers to its approved strategy. Show the plan before applying it. Do not hand-edit identifiers or references."

Treat that as authority to inspect and edit the schema, then preview the supported repair. It is not authority to apply the plan. After the schema change, run `rzm validate identifiers`, then run `rzm validate fix identifiers` without `--apply`. Report the complete mapping, edits, moves, warnings, and fingerprint. Apply with `rzm validate fix identifiers --apply` only after the user approves the preview and any required `--allow-historical` authority. Rerun the same plan after apply; an empty second plan is the idempotence check.

## After editing

- Run the ontology's dedicated validation command advertised by the installed version.
- Reindex or rebuild only when the schema/index contract requires it.
- Validate affected notes, recipes, and views.
- Run representative queries and authoring-guide output for changed types.
- Update the nearest normative documentation and any canonical template source in the same change.
- Sweep skills, canonical prompt/template sources, generated consistency, recipes, tests, and prose for changed roots, types, fields, relations, headings, and companion paths. Validate affected recipes; ask before changing a recipe's workflow purpose.

Never hand-edit generated agent-surface copies. Never guess directive or root syntax from an older schema. Keep schema authoring separate from ordinary structured-note authoring so mutation authority and migration impact remain explicit.
