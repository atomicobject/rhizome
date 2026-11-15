# Customizing a vault

Use this route when a person wants the vault shaped to their work: a tracker, a pipeline, a new kind of note, a field, a stage, or a view the team shares. Climb only as far as the question requires. Each step up changes files more people and agents depend on.

## 1. Start from the question and real notes

Ask what the person needs to see or decide, then read two or three notes that already hold that information. Discover what exists before designing anything: `rzm agent ontology-reference` for types and `rzm agent view list` for views and their targets.

Done when you can name the grouping, sorting, filtering, or validation the question needs, and which existing type, if any, already carries it.

## 2. Reuse existing structure

If an existing type has the fields, the answer is a view (step 3) or a typed note written to that type (`references/structured-markdown.md`). Adjust a team's process in its own policy documents and skills, not in the schema.

## 3. Choose a personal setting or a shared view

- **For one person:** they sort, filter, group, and pick columns in the app. Rhizome remembers those choices per person and writes nothing to the repository.
- **For the team:** the person arranges the view and uses the workspace's save-to-repository action, or you author the view YAML. Either way the result is a tracked file the team reviews. `references/views.md` owns mounts, defaults, and layouts.
- **Past tables, cards, and boards:** load the `custom-views` skill for HTML or TSX pages and multi-page experiences.

## 4. Change the schema only when a consumer needs it

Add a type or field for a concrete query, filter, sort, validation rule, relationship, or edit that steps 2 and 3 cannot serve. Keep explanation in the note body. `references/ontology-authoring.md` owns SDL mechanics, display roles, and lifecycle stages; these patterns keep a change safe:

- **Leave fields nullable and require them by condition.** A note that violates a non-null field (`!`) makes every query over its type report an error. `@requiresWhen(field: "stage", equals: "proposal", require: [{field: "value"}])` reports the gap in validation while queries keep working.
- **Give subtypes an interface.** When notes of one kind need different guidance, declare an `interface` for the shared fields and keep the specific types. Deleting a specific type to quiet ambiguity discards its rules.
- **Rename a property through `sources`.** `nextCall: Date @field(sources: ["next-call", "next-meeting"])` reads both spellings while notes migrate; remove the old spelling afterward.
- **Write docstrings as rules for agents.** A type's docstring and its `summary` field are what an agent scanning the vault reads; say what belongs in the type and what does not.
- **Declare stages on a lifecycle enum** with `@view(stage:)` so generated views offer a board without any view YAML.

Procedure:

1. Inspect the live schema, the representative notes, and the views and saved queries that read the affected types.
2. Propose the smallest change, the file it goes in, and which existing notes it would invalidate or require migrating.
3. Edit the SDL, then run `rzm ontology validate` and one representative `rzm agent ontology-query`.
4. **Stop for approval before changing existing notes.** Preview the migration with `rzm note properties rename <old> --to <new> --inputs <scope> --dry-run` (`set` and `delete` work the same way) or `rzm validate fix ontology`, and show the person the affected notes. Apply after they approve. Wrong values and missing links have no automatic repair; fix those notes by hand and rerun validation.
5. Run `rzm validate views` and `rzm validate query-recipes`, and read `rzm agent ontology-authoring-guide --type <Type>` to confirm an agent would draft the type correctly.
6. Ask the person to open the type in the app and confirm its views and hover previews.

Fix notes that drift from the schema; weaken the schema only when the evidence says the contract is wrong.

## A worked example: a sales pipeline

The request is "I run discovery calls and want a pipeline board." No existing type has a stage, so step 4 applies. The board needs a stage to group by, an account to lane by, and a next-call date; call notes stay in the body.

```graphql
"""A company we sell to."""
type Account @node(paths: ["pipeline/accounts/*.md"]) @display(group: "Pipeline") {
  summary: String
}

"""One potential sale to one account. Keep call notes in the body, not in fields."""
type Deal @node(paths: ["pipeline/deals/*.md"]) @display(group: "Pipeline")
  @requiresWhen(field: "stage", equals: "proposal", require: [{field: "value"}]) {
  summary: String
  stage: DealStage @display(importance: KEY)
  account: Account @link @display(importance: KEY)
  nextCall: Date @display(importance: KEY)
  value: Int
}

enum DealStage {
  discovery @view(label: "Discovery", order: 10, stage: "open")
  proposal @view(label: "Proposal", order: 20, stage: "active")
  won @view(label: "Won", order: 90, stage: "done")
  lost @view(label: "Lost", order: 100, stage: "dropped")
}
```

A note satisfies it with frontmatter; `nextCall` reads the key `next-call`:

```markdown
---
summary: Route planning pilot for two depots.
stage: proposal
account: "[[pipeline/accounts/acme]]"
next-call: 2026-11-20
---
# Acme routing pilot
```

`rzm ontology validate` reports this note until it has a `value`, because it is at `proposal`. With no view file, the Deal type now offers a Table, Cards, and a Board by stage, and the Pipeline group gets the bundled group pages. To make the board the team's default with account lanes, save it from the app or author the view (`references/views.md`). A weekly check for deals with no call booked is `references/automation.md`.

## Package it as a module

When a customization grows past one type and one view, keep its schema, views, saved queries, scripts, and actions together in `.rhizome/modules/<id>/`, as the example's `.rhizome/modules/pipeline/ontology/pipeline.graphql` would be. A single type or view can live in the flat `.rhizome/ontology/` and `.rhizome/views/` folders. `references/automation.md` has the module layout.
