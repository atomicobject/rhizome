---
type: TechnicalSpec
summary: "Defines the composable action-items starter template, including ActionItem ontology, dependency on shared core identity, default init enablement, saved query recipes, configured views, skill routing, and date validation."
id: SPEC-0060
spec-status: proposed
last-updated: 2026-05-06
aliases:
  - SPEC-0060
  - Action items starter template
---

# Action items starter template

## Summary

Rhizome should ship action-item support as a composable starter template that is enabled by default during `rzm init` and remains compatible with `project-kb`, `spec-driven`, and future workflow starters. The template gives lightweight checkbox tasks a durable graph shape: an `ActionItem` embedded node with assignee ownership, checkbox completion state, optional due date, source-note context, saved query recipes, a configured view, and an agent skill that must be used whenever an agent creates, finds, updates, completes, assigns, or otherwise handles action items.

This spec builds on [[core-template-and-template-dependencies]], [[non-section-embedded-node-source-shapes]], [[saved-query-recipes]], [[configured-view-engine-and-repo-config]], and [[init-template-architecture]]. Current non-section embedded nodes can model checkbox items when a containing ontology field declares `@contains(shape: CHECKBOX_ITEM, marker: "#action-item")`. The action-items starter needs one additional productized capability: marker-gated action items must be discoverable across any in-scope note without requiring every other starter type to declare an `actionItems` containment field.

The action-items template does not define shared identity primitives itself. It depends on the `core` template for `Person`, optional team identity, and the private current-user setting. This keeps action items compatible with `project-kb`, `spec-driven`, and future starters that also need people without each starter inventing a different `Person` shape.

## Goals

- ship an `action-items` starter/addon template that composes with `project-kb`, `spec-driven`, and future workflow starters
- require the `core` template instead of duplicating shared identity schema
- enable the action-items template by default in the init workflow for new repos
- model action items as first-class embedded graph nodes that can be authored inside any in-scope note
- model assignees as `core.Person` graph nodes, not free-text guesses
- use the core current-user setting for personal action-item recipes and defaults
- let agents answer "what are my action items?" through a saved recipe that resolves the configured current user and queries `ActionItem` directly
- provide a configured action-items view over all open direct `ActionItem` rows, with a current-user canned filter
- validate the optional due date as a semantic `Date`, including item-local inline property values
- install a required action-items skill that routes every action-item operation through the ontology authoring guide, query recipes, and current-user setting

## Non-Goals

- duplicating action-item schema, recipes, or skills inside both `project-kb` and `spec-driven`
- defining shared identity primitives such as `Person`, `Team`, or current-user settings inside the action-items template
- making action items specific to meetings, conversations, efforts, projects, or specs
- replacing project-management tools, Linear, GitHub issues, or a full task-board workflow
- making every checkbox item an action item without an explicit marker
- making agents infer the current user from chat, Git config, host usernames, or prose
- requiring a due date on every action item
- defining recurring tasks, reminders, notifications, or calendar integration
- building the full browser task-management experience beyond the configured view and existing edit-session field toggles

## Requirements

### Template Packaging

#### Must

- `rzm init` MUST recognize an `action-items` workflow template.
- The `action-items` template MUST declare a required dependency on `core`.
- Init resolution MUST install `core` before `action-items` whenever action-items is selected directly or activated by another starter.
- New interactive init runs MUST include `action-items` in the recommended/default selected template set unless the user explicitly disables workflow templates.
- Non-interactive init MUST support selecting `action-items` by name and combining it with other templates, for example `spec-driven,action-items` and `project-kb,action-items`.
- The action-items starter MUST install its source assets from `pkg/app/cli/init/templates/starters/action-items/**`.
- The action-items starter MUST use template-owned target paths that do not collide with existing `project-kb` or `spec-driven` starter outputs.
- Starter ontology MUST install under a template-owned schema file such as `.rhizome/ontology/action-items.graphql`.
- Starter query recipes MUST install under `.rhizome/query-recipes/action-items.yaml`.
- Starter views MUST install under `.rhizome/views/action-items.yaml`.
- Starter skill source MUST install under `.agents/skills/action-items` and `.claude/skills/action-items` when those agent surfaces are enabled.
- Starter managed agent docs MUST route action-item work to the action-items skill without duplicating the full skill body.
- Init tests MUST prove default enablement, explicit selection, multi-template composition, installed ontology, installed recipes, installed view config, installed skill, managed docs routing, and collision-free behavior with `project-kb` and `spec-driven`.

#### Should

- Existing repos with no workflow template selection should be offered action-items on rerun instead of having it silently added without review.
- Existing repos with explicit workflow template config should preserve user choice, while the settings menu makes the new default visible.
- Template docs should explain that action-items is an addon template and can be removed from the workflow template list if a repo does not want local task tracking.

### Ontology Model

#### Must

- The starter MUST define an `ActionItem` embedded node type.
- `ActionItem` MUST be sourced from marker-gated checkbox items using `#action-item`.
- `ActionItem` MUST expose `done: Boolean!` from `sourceKind: CHECKBOX`.
- `ActionItem` MUST expose an assignee field that links to the `Person` node type supplied by the `core` template.
- `ActionItem` MUST expose an optional `due` field of type `Date`.
- The `due` value MUST be authored as an item-local inline property and validated as a Date value, not treated as an opaque string.
- `ActionItem` MUST preserve canonical `NodeRef`, source note path, parent context when available, checkbox source range, content text, and source locator.
- `ActionItem` MUST expose source-note context sufficient for recipes and views to show where the item came from and open the source span.
- `ActionItem` MUST be queryable through a direct GraphQL root such as `actionItem(first: ...)` without requiring callers to query a containing `Conversation`, meeting, spec, effort, project, or generic note type first.
- Action-item discovery MUST work for any in-scope note that contains a marker-gated checkbox item, including notes whose primary ontology type comes from another starter.
- Action-item discovery MUST NOT reclassify every note as a generic action-item container in a way that hides its more specific ontology type.
- The starter MUST NOT define duplicate `Person` or `Team` node types.
- `core.Person` MUST use the person note title as its primary author-facing identity; action-items MUST NOT require synthetic person ids.
- `ActionItem.assignee` MUST resolve to `Person` through normal link resolution and produce a graph edge.
- Ontology validation MUST flag unresolved assignee links, missing required assignees, invalid due dates, invalid checkbox field bindings, and malformed action-item source shapes.

#### Should

- The authoring guide should recommend an assignee wikilink to a `Person` note, `due:: YYYY-MM-DD`, and `#action-item` inside the same checkbox item.
- The action item display text should omit the marker and item-local metadata when clean display text is available, while preserving raw content for source review.
- `ActionItem` should expose an optional source-context relation to the primary ontology node for the source note when that note resolves to a typed node such as `Spec`, `Effort`, or `Project`.
- The first source-context implementation should treat nearest heading or section context as display/locator metadata, not as a separate graph filter contract.
- The implementation should prefer a schema declaration that lets an embedded type declare a global source shape, marker, and note scope, rather than forcing every concrete note type to duplicate an `actionItems` field.

### Date Support

#### Must

- `Date` MUST be a first-class ontology scalar for ActionItem due dates.
- Validation MUST reject non-date due values such as `next week`, `soon`, `5/6`, or arbitrary prose when they are authored into the `due` field.
- Validation MUST accept ISO `YYYY-MM-DD` date values for `due`.
- Item-local inline fields MUST use the same scalar validation path as note frontmatter and section inline fields.
- GraphQL results MUST expose `due` as a typed date-compatible value that views, recipes, filters, sorts, and clients can treat as a date.
- Configured views MUST be able to sort and filter action items by `due`.
- Default action-item sorting MUST order open items by due date ascending with undated items last, then by source-note/source-span order.
- Query recipes MUST be able to select `due` and preserve it in their output contract.

#### Should

- The implementation should reuse existing date analysis where it already validates `Date` and `DateTime`.
- Date validation diagnostics should identify the owning action item and source note, not only the containing file.
- Future date-range filters should support overdue, due today, due this week, and no due date without changing the underlying `ActionItem.due` field.

### Core Current User Identity Dependency

#### Must

- The `core` template MUST provide a private per-vault current-user setting that action-items can consume.
- The setting MUST point to a `Person` node by author-facing title, alias, wikilink target, or path that Rhizome canonicalizes.
- The setting MUST live in ignored local state, not tracked repo config.
- Init MUST provide a path to configure the current user when action-items is enabled by default.
- The action-items skill MUST read the configured current-user setting before answering "my action items" or creating a default-assigned action item.
- Agents MUST NOT infer the current user from chat context, OS usernames, Git authors, vault names, or surrounding prose when the setting is missing.
- If the current-user setting is missing or points at an unresolved/non-Person node, the skill MUST stop and guide the user to configure or create a `Person` node instead of running an unreliable "my action items" query.
- Query recipe execution for "my action items" MUST use the configured current user as the assignee input.
- The ontology GraphQL/runtime surface MUST expose the configured current user through a queryable value such as `currentUser { person { nodeRef displayName path } }` so recipes and views can bind filters without guessing identity.

#### Should

- Interactive init should use the core template's person setup path when no suitable `Person` exists.
- Non-interactive init should accept the core current-user ref or leave a validation/actionable setup issue rather than fabricating a user.
- Action-items should not hard-code the current-user storage path; it should use the core identity API or resolver surface.

### Query Recipes

#### Must

- The starter MUST ship saved query recipes for action-item workflows.
- A recipe for "my action items" MUST query `ActionItem` directly and filter to action items where `assignee` is the configured current user.
- A recipe for open assigned action items MUST exclude checked/completed action items by default.
- Recipes MUST include `problem`, `inputSpec`, `outputContract`, and `adaptationGuidance`.
- Recipes MUST select canonical node identity, title/display text, `done`, `assignee`, `due`, source note path/title, and locator fields needed to open or edit the item.
- Recipes MUST select source-context fields when the source note has a typed primary ontology node.
- Recipes MUST be valid against the live ontology query schema after the starter ontology is installed.
- Recipe validation tests MUST run the action-items recipe bundle against the combined schema with `project-kb`, with `spec-driven`, and with both starters together.

#### Should

- The starter should include at least these recipes:
  - `my-action-items`: current-user assigned, open by default
  - `assigned-action-items`: explicit assignee input
  - `all-action-items`: bounded inventory/debug query
  - `action-items-in-note`: source-note scoped query
  - `action-items-for-context`: source-context scoped query for a spec, effort, project, or other typed source note
- Recipes should expose optional filters for `done`, `dueBefore`, `dueAfter`, and source note path once the query runtime supports those inputs.
- Recipes should expose optional filters for source-context node ref once globally sourced action items preserve typed source-note context.
- Empty results should distinguish "no matching action items" from "current user is not configured."

### Configured View

#### Must

- The starter MUST ship a configured view for action items.
- The view MUST use the configured-view engine and MUST NOT introduce a separate task dashboard data path.
- The view source MUST be `ontology_type`, `query_recipe`, or explicit `graphql` that compiles to the shared GraphQL-backed source plan.
- The default view MUST show open and completed action items grouped by `done`, with open items expanded and completed items collapsed by default.
- The view MUST preserve row `NodeRef` identity so selecting a row opens the source action item, not just the containing note.
- The table columns MUST include display text, done, assignee, due, and source note.
- The default sort MUST order by done state, due date ascending with undated items last, then by source-note/source-span order.
- Filtering by done state, assignee, and due date MUST be available where the view engine supports the field capabilities.
- The view MUST provide a canned "mine" filter control that filters `assignee` to the configured current user.
- The canned current-user filter MUST bind its filter value from the GraphQL/runtime current-user value rather than from chat context, OS usernames, Git authors, vault names, or prose.
- Default filters, canned filters, user filters, and sort MUST be normalized into the view source plan and pushed into ontology GraphQL/read-model execution before pagination when the fields are supported.
- In-memory filtering and sorting MUST be treated as a residual fallback, not the primary execution model, and MUST report warnings when a configured filter or sort cannot be pushed down.
- When no current user is configured or the configured user does not resolve to a `Person`, the default all-open view MUST still work and the canned "mine" filter MUST be disabled or return an actionable setup warning.

#### Should

- Completed action items should stay visible but collapsed by default so recent closure remains reviewable without taking over the work queue.
- The view may also mount as a standalone rail entry named "Action Items."
- Future variants may add card or kanban views, but the first template should stay table-first.

### Skill Behavior

#### Must

- The starter MUST install an `action-items` skill.
- The skill description MUST trigger whenever an agent wants to create, find, query, summarize, update, complete, assign, review, or otherwise handle action items.
- For create or modify operations, the skill MUST run or instruct the agent to run `rzm agent ontology-authoring-guide --type ActionItem` before editing.
- For find/query/list operations, the skill MUST prefer the bundled action-item query recipes over ad hoc ontology queries.
- The skill MUST explain how to add an action item to any relevant note as a checkbox item with `#action-item`, an `assignee::` wikilink to a `Person` note, and optional `due:: YYYY-MM-DD`.
- The skill MUST default the assignee to the configured current user only when the user asks for a personal action item and the setting resolves to a `Person`.
- The skill MUST require explicit assignee input when the requested action item is not clearly for the configured current user.
- The skill MUST never silently create a free-text assignee that does not resolve to a `Person` node.
- The skill MUST route completion/toggle operations through the ontology edit-session path so only the checkbox token changes.
- The skill MUST route due-date, assignee, and action-item text updates through source-span-aware edit paths rather than whole-note rewrites when the edit can be localized.

#### Should

- The skill should include short command examples for listing recipes, running the current-user recipe, inspecting the authoring guide, and validating action-item ontology/query recipes.
- The skill should treat "what are my action items?", "what do I owe?", "show my todos", and "find open tasks assigned to me" as current-user action-item queries.
- The skill should tell agents to add action items near the source context that created them, not into a central catch-all file unless the user asks for an inbox.

### Validation And Test Data

#### Must

- Integration testdata MUST include example notes with action items authored in ordinary notes, not only in conversation/meeting-specific notes.
- Testdata MUST include checked and unchecked action items.
- Testdata MUST include action items assigned to at least two different `Person` nodes.
- Testdata MUST include at least one action item with a valid `due` date.
- Testdata MUST include at least one invalid due date case in focused validation tests.
- Tests MUST prove direct `actionItem` query roots return rows without querying through a containing note root.
- Tests MUST prove "my action items" recipe behavior resolves the configured current user and filters by assignee.
- Tests MUST prove the default configured view returns open and completed action items with completed groups collapsed, not only current-user items.
- Tests MUST prove the configured view's canned current-user filter binds from the GraphQL/runtime current-user value and filters by assignee.
- Tests MUST prove missing current-user settings produce an actionable error or validation issue.
- Tests MUST prove starter view config validates and executes against the action-items ontology.
- Tests MUST prove init default installation and multi-template composition.

#### Should

- Integration tests should cover action items embedded in a spec-driven note and a project-kb note when both starters are installed.
- Integration tests should cover action items whose source note resolves to a typed source context such as a spec, effort, or project.
- Validation tests should report invalid due dates at the embedded node/source span level.
- Query recipe tests should include a source note with multiple action items assigned to different people so assignee filtering is observable.

## Related Contracts

- [[non-section-embedded-node-source-shapes]] owns marker-gated checkbox source spans, checkbox field binding, item-local inline fields, direct embedded-node query behavior, and source-span edit ownership.
- [[core-template-and-template-dependencies]] owns the `core` template, `Person` identity schema, current-user setting, and dependency/activation resolution.
- [[configured-view-engine-and-repo-config]] owns repo-tracked `.rhizome/views/*.yaml`, GraphQL-backed source planning, table columns, filters, sort, grouping, and row identity.
- [[saved-query-recipes]] owns durable recipe metadata, recipe validation, and recipe execution through ontology GraphQL.
- [[init-template-architecture]] owns starter source families, managed docs, skills, ontology, query recipes, collision behavior, and init refresh semantics.
- [[public-api-graphql-rest-contract]] owns public GraphQL/REST boundaries for custom apps and future external action-item consumers.

## Example Authoring Shape

```md
## Follow-ups

- [ ] Update the rollout note with the agreed message assignee:: <Person wikilink> due:: 2026-05-12 #action-item
- [x] Confirm query recipe validation assignee:: <Person wikilink> #action-item
```

The first item should project as an `ActionItem` with:

- `done = false`
- `assignee = Person(<person-note>.md)`
- `due = 2026-05-12`
- source note and source span pointing at the checkbox item
- direct availability through `actionItem(first: ...)`

## Open Questions

- [TODO: Confirm implementation path] Should global marker-gated embedded sources be declared through a new directive on `ActionItem`, for example `@source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["**/*.md"])`, or through a schema-level declaration that maps source shapes to embedded types?
- [TODO: Confirm UX] Should action-items trigger the core person setup prompt during init, or should it report a setup issue and let the user create/select their `Person` later?
- [TODO: Confirm query filters] Should the first implementation extend direct typed roots such as `actionItem` with structured relation/date filters, or should query recipes initially use `notes(type:)`/GraphQL source plans that can express the required assignee and due filters?

## Documentation Plan

- Update init starter docs and managed `AGENTS.md` blocks so agents know action-items is enabled by default and routed through the `action-items` skill.
- Update core template docs when action-items consumes new or renamed identity fields.
- Update ontology schema authoring docs if global embedded-node source declarations require new SDL syntax.
- Update the action-items skill whenever recipe ids, current-user setting shape, or ActionItem authoring fields change.
- Update public docs and MCP/agent guidance if action-item recipes or view execution become recommended general-purpose workflow examples.
