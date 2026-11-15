---
type: ReferenceDoc
summary: "A worked, tested scenario for the usage skill: a sales discovery pipeline built as a team module with its own types, validation, a compatibility rename, a conditional requirement, a recipe, a board view, and the path on to group views, custom views, and an experience."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Schema evolution and views for custom workflows

This note walks one realistic request through Rhizome's schema and view layers, so the council can see what the usage skill must teach and where an agent would stumble. The request is: "I run sales discovery calls and want a pipeline board." Everything marked **verified** ran against a scratch vault in the session scratchpad with a fresh v0.52.1 build of this checkout (`make build NO_WEB=1`), with runtime auto-start disabled. Nothing in the repository was changed.

## 1. Decide whether the schema needs to change

The ontology guidance says to design from use: start with the questions people ask and add a structured field only for a concrete query, filter, sort, validation, relation, or editing need (`pkg/app/cli/init/templates/skills/markdown/rhizome/references/ontology-authoring.md`, "Design from use"). For a pipeline board the concrete needs are a stage to group by, an account to lane by, and a next-call date to sort by. Prose about the call stays in the note body.

If the repository already has a type that fits, rung 4 (a view) is enough and the schema does not change. The skill should check that first with `rzm agent ontology-reference` and `rzm agent view list`.

## 2. Choose where the schema lives

- **Verified:** schema files load from top-level `*.graphql` in `.rhizome/ontology/` and in every `.rhizome/modules/<module>/ontology/` (`docs/reference/subsystems/ontology.md`). The scratch vault used `.rhizome/modules/sales/ontology/sales.graphql`, and `rzm ontology validate` listed it under `Files:`.
- **Recommendation (inference):** a team module (`.rhizome/modules/<name>/`) keeps the schema, recipes, views, tasks, scripts, and actions for one workflow together, and only a module can have actions. The flat folders also work and suit a single type. The guide `docs/reference/guides/build-an-experience.md` uses a team module for the same reason.
- **Stale guidance:** `references/ontology-authoring.md` and `docs/rhizome-md-templates/Ontology schema authoring.md` still name only `.rhizome/ontology/*.graphql`.

## 3. Write the smallest schema

This SDL compiled and resolved notes by path (**verified**):

```graphql
"""A company we sell to."""
type Account @node(paths: ["sales/accounts/*.md"]) @display(group: "Sales") {
  summary: String
  industry: String @display(importance: KEY)
}

"""One potential deal with one account, from first call to close."""
type Opportunity @node(paths: ["sales/opportunities/*.md"]) @display(group: "Sales") {
  summary: String
  stage: OpportunityStage! @display(importance: KEY)
  account: Account! @link @display(importance: KEY)
  nextCall: Date @display(importance: KEY)
  value: Int
}

enum OpportunityStage {
  discovery @view(label: "Discovery", order: 10, stage: "open")
  qualified @view(label: "Qualified", order: 20, stage: "active")
  proposal @view(label: "Proposal", order: 30, stage: "active")
  won @view(label: "Won", order: 90, stage: "done")
  lost @view(label: "Lost", order: 100, stage: "dropped")
}
```

What each choice buys, per the views reference and the type profile Rhizome derived (**verified** with `rzm.ontologyReference({ type: "Opportunity" })`):

| Schema choice | Effect |
| --- | --- |
| `@view(stage:)` on every enum value, with an `active` stage | Profile `shape: workflow`, `lifecycleField: stage`; generated views group by lifecycle and offer Table, Cards, and Board |
| `summary: String` | Profile `summaryField: summary`; Cards appear and tables default to two lines |
| `nextCall: Date` marked `KEY` | Profile `primaryDateField: nextCall` |
| `account: Account! @link` | Profile `relationFields: [account]`; a candidate board lane |
| `@display(group: "Sales")` | A "Sales" display group in the Notes rail, which gets the bundled group views |
| Default `propertyCase: KEBAB` | Field `nextCall` reads frontmatter key `next-call` |

The profile also reported `gapFields: [nextCall]`, which the Briefing view and the skill-script example use to show records missing that field.

A note that satisfies it:

```markdown
---
summary: Route planning pilot for two depots.
stage: qualified
account: "[[sales/accounts/acme]]"
next-call: 2026-11-20
value: 40000
---
# Acme routing pilot
```

## 4. Validate, and know what each failure does

With one deliberately bad note (`stage: negotiating`, no `account`), **verified** results:

- `rzm ontology validate` reported `field_type_mismatch` for the enum value and `missing_required_field` for the link, and exited non-zero. It needed no index.
- `rzm ontology query --query '{ opportunity(first: 10) { title stage nextCall value account { title } } }'` returned the good record's data, nulled the bad record's fields, listed both errors, and exited non-zero. One bad note makes every query over the type fail at the operation level, so scripts and views see an error unless they opt into partial data (the kit's `useGraphQL` has `options.partial`).
- `rzm agent view run --id <view> --omit-capabilities` still listed the bad record as a row with its raw `negotiating` value. Views tolerate invalid values that GraphQL rejects.
- `rzm validate fix ontology` classified both issues as `agent_required`, with no safe or confirmable action. Deterministic ontology repairs exist only for a schema-derived scalar or link suggestion, an empty required list field, and a missing required section (`pkg/validate/fix_ontology.go`). Fixing a wrong enum value or a missing link is agent work.

**Implication (inference):** non-null fields (`!`) are strict at query time, which is why the schema guidance says to prefer nullable fields. A conditional rule (below) gives validation pressure without breaking reads.

## 5. Evolve the schema safely

Each change below was **verified** in the scratch vault unless marked otherwise.

- **Add an optional field.** Adding `channel: String` and writing `channel: referral` in a note showed up in the next `rzm ontology query` without a manual `rzm index`. The views reference says reindexing is needed only when the schema or index contract requires it; when results look stale, `rzm index --status` and `rzm index` are the fallback.
- **Require a field only in some stages.** `@requiresWhen(field: "stage", equals: "proposal", require: [{field: "value"}])` on `Opportunity` made `rzm ontology validate` report `conditional_required_field_missing` for a proposal with no value, while the query still succeeded with `value: null`. This is the right tool for "a proposal must have a deal size."
- **Rename a property without breaking old notes.** `nextCall: Date @field(sources: ["next-meeting", "next-call"])` read both spellings: an old note with `next-call` and a new note with `next-meeting` both returned `nextCall`. The guidance allows alternate spellings only as an explicit migration or compatibility input (`ontology-authoring.md`).
- **Migrate the notes.** `rzm note properties rename next-call --to next-meeting --inputs sales/opportunities --dry-run` previewed the rewrite ("Would rename properties in 1 note(s)"). `rzm note properties set` and `delete`, and `rzm note tags rename`, cover other bulk frontmatter changes, all with `--dry-run`. After migrating, drop the old spelling from `sources`. No shipped skill reference mentions these commands.
- **Change identifier strategy.** This is the one schema migration with a dedicated planner: `rzm validate identifiers`, then `rzm validate fix identifiers` to preview, then `--apply` only after approval, then rerun until the plan is empty (`ontology-authoring.md`, "Identifier strategy migration"). Not exercised here.
- **Other evolution moves (inference, from the directive list in `pkg/ontology/schema/schema_sdl.go`):** `@node(matches:)` or `@admit` to type notes by tag or property rather than folder; `interface` plus `implements` for shared fields, which also gives an interface query root and interface views; `@contains` for typed body sections and embedded list items; `@display(role: PARENT)` for trees; `@display(role: RANK)` for manual order; `@identifier` for allocated ids; `@guidance` and `@companionDocs` for authoring rules agents read.

### Evolution checklist the skill could carry

1. Inspect the live schema, representative notes, and the views and recipes that read the affected types.
2. Propose the smallest change and say which existing notes it invalidates.
3. Edit the SDL, then run `rzm ontology validate` and a representative `rzm ontology query`.
4. If existing notes need changes, preview them (`rzm note properties ... --dry-run`, `rzm validate fix <check>`) and get approval before applying.
5. Run `rzm validate views`, `rzm validate query-recipes`, and the type's authoring guide (`rzm agent ontology-authoring-guide --type <T>`).
6. Open the type in the app and confirm the generated view, board, and hover previews look right.

## 6. A shared board view

With the schema in place, the generated view already offers a Board. In the scratch vault the default was Table, because a workflow type opens as a Board only when at least two `open` or `active` values have records and at most 60 records are open or active (`views.md`, "Generated defaults by shape"). That rule surprises users with one or two test records.

To make the board the team default, either use the workspace's save to repository from the generated view (no YAML authoring), or write the YAML. This definition **validated** with `rzm validate views` and ran as a kanban with `rzm agent view run --id sales.pipeline --omit-capabilities`:

```yaml
apiVersion: rhizome.view.v1
id: sales.pipeline
name: Pipeline
source: { kind: ontology_type, type: Opportunity }
mount: { kind: type, type: Opportunity, replaceGenerated: true }
defaults:
  variant: kanban
  sort: [{ field: nextCall, direction: asc }]
variants:
  kanban:
    columnField: stage
    laneField: account
    card: { title: title, fields: [{ field: account }, { field: nextCall }, { field: value }] }
  table:
    columns: [{ field: title }, { field: stage }, { field: account }, { field: nextCall }, { field: value }]
```

`replaceGenerated: true` makes this the collection's standard Table and Board rather than an extra choice (`views.md`, "Applicability and defaults"). Filter presets can use `valueFrom: ontology.currentUser.resolvedRef` for a "Mine" preset, as the shipped action-items view does (`.rhizome/modules/action-items/views/action-items.yaml`); that needs a `Person` link field and a configured current user.

## 7. A recipe for agents and the command line

A recipe in the module became a command (**verified**): `rzm sales` listed `upcoming-calls` with its `problem` text, and `rzm sales upcoming-calls` ran it. The file format is in `pkg/ontology/queryrecipe/types.go` and in the ladder note. A native view can also use the recipe as its source (`source: { kind: query_recipe, queryRecipe: <id>, resultPath: ... }`), which suits a view whose rows come from a nested query (`pkg/app/views/CONTEXT.md`, "Query recipes").

## 8. Group views, custom views, and an experience

- **Group views:** `@display(group: "Sales")` puts both types under a Sales rail group. Without an authored default, a group with two or more member roots opens the bundled Briefing; Overview, Trace, and Sections are also offered, and Trace appears only when two member types link through a forward `@link` whose target is a member, which `Opportunity.account` satisfies (`views.md`, "Bundled views"). A `NO_WEB=1` build has no bundled views, so this was not visible in the scratch vault.
- **Custom page:** a TSX entry with `source: { kind: custom, entry: home.tsx }` and `mount: { kind: group, group: Sales, default: true }` replaces the group's home. The `custom-views` skill owns the loop: discover targets, prove the query with `rzm agent ontology-query`, write, `rzm validate views`, serve, and verify in the real UI.
- **Experience:** an `experience.yml` (`title: Sales`, `group: Sales`, `panel: sales.panel`) in the same views folder makes the home, an `Opportunity` record page (`mount: { kind: node, type: Opportunity }`), and a panel (`mount: { kind: panel }`) run as one site with a sidebar panel. `docs/reference/guides/build-an-experience.md` has a complete worked example with a `Policy` type. That guide does not install downstream; `custom-views/references/experiences.md` does.

## Pitfalls collected from the walkthrough

- Required (`!`) fields turn one sloppy note into a failed query for the whole type. Use nullable fields plus `@requiresWhen` unless strict reads are worth it.
- The schema has no UI editor; every ontology HTTP route is read-only (`pkg/app/web/server_http.go`). Schema work happens in text through an agent.
- `rzm validate fix ontology` rarely fixes schema drift by itself. Plan for agent edits plus validation reruns.
- `rzm agent view run` takes `--id`; a positional id fails with `view not found: `.
- `views.md` says a one-type view belongs on a `type` mount, yet the shipped action-items view is `standalone`. A user who copies the starter gets a second rail entry beside the type.
- Display groups are shared across modules. Another module's types in a group named "Sales" also get a Sales experience's panel unless that module declares its own experience with `types` (`build-an-experience.md`, "How Rhizome chooses the panel").

## What the usage skill needs from this (inference)

- A short decision path: existing type plus a view, or a new type. Most pipeline, tracker, and board requests need one small type and one view.
- The evolution checklist above, including the compatibility-rename pattern and the bulk property commands.
- A pointer to `rzm ontology validate` as the quick, index-free check, and an honest statement that queries fail when records violate non-null fields.
- A route into `custom-views` for anything past native layouts, and a statement that experiences are just a views folder with `experience.yml`.
