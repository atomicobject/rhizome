---
type: ProductSpec
summary: "Defines the ontology browser as a first-class workspace for browsing, triaging, and working typed notes through type-aware listings, readiness signals, neighborhoods, and semantic search."
id: SPEC-0014
spec-status: active
last-updated: 2026-10-02
aliases:
  - SPEC-0014
  - Ontology browser workspace
---

# Ontology browser workspace

## Summary

Rhizome should expose its ontology as a primary human-facing workspace, not only as backend infrastructure for agents and validation. The ontology browser exists to help users start from typed note families, schema-guided readiness, semantic search, and declared relationships, then move through a vault faster than a raw tree-plus-file-view workflow allows.

The ontology workspace complements the existing tree and graph views. It is the structured, high-signal surface for understanding a typed knowledge base, and its durable unit of focus is a node workspace rather than only a whole-note workspace. In the shipped browser contract, that workspace is exposed as a canonical node graph with additive derived views rather than a stack of separate note-only and structure-only payload families.

The typed note list is a workbench, not a separate product-management board. `/notes` remains the general note browser, while `/notes/<type-or-interface>` is the same browser with a type filter applied and a readiness-oriented default grouping. Users should be able to change filters, sorting, grouping, and search from there without leaving the general listing model.

Configured views extend that workbench without turning it into bespoke dashboards. A view is a saved collection experience defined by its source note set, where it is mounted, and which variants it offers. Type-mounted views belong to a note type or interface and power the direct list affordance from the type rail; standalone views mount as their own side-rail navigation entries. Both use the same listing engine, query model, and node-opening behavior.

## Goals

- make typed notes and declared relations legible to humans
- let users work against nodes, sections, and embedded entities through one coherent workspace model
- let users discover a vault from type families, neighborhoods, and note-focused search
- provide a clear starting surface when a repository has a meaningful ontology
- turn ontology health and gaps into something operational, not hidden backend state
- let users triage notes of any ontology type by readiness, issue state, required structure, key fields, and workflow-relevant signals
- keep readiness generic and schema-derived so project-kb, spec-driven, transcript, decision, and future ontologies all benefit
- let teams save reusable views as tracked repository configuration while still providing schema-informed defaults when no explicit config exists
- support type-mounted views and standalone rail views through one extensible source, mount, and variant model

## Non-Goals

- replacing the file tree or freeform graph views entirely
- requiring every repository to adopt a rich ontology before the workspace is useful
- specifying low-level node persistence or browser component architecture here
- treating the ontology workspace as an editor-first surface in this phase
- freezing the exact browser push transport or runtime plumbing in the product contract
- building a project-kb-specific dashboard, JIRA clone, or AO-KB-specific curation board
- hard-coding project management concepts such as sprint, assignee, priority, product, layer, lens, or status into the generic ontology browser
- replacing agents for judgment-heavy authoring; the browser should expose readiness and ambiguity so users and agents can align faster
- making Obsidian `.base` files the native Rhizome view configuration format in the first view-configuration slice
- shipping a full visual view-configuration editor before the durable view model and repo config contract are stable

## User Stories

### US1 - Start from note families, counts, type descriptions, and health signals instead of guessing from the file tree
- id:: ^SPEC-0014-US1
- summary:: Start from note families, counts, type descriptions, and health signals instead of guessing from the file tree.
- status:: ready

#### Acceptance Criteria

- The ontology home shows every note-role and embedded-node-role family that exists in the current schema, with role, label, description, count, and issue count.
- Each family summary includes a small set of starting node refs so a user can open representative notes or embedded nodes without doing path search first.
- Family ordering makes the most useful starting points obvious by preferring populated, issue-bearing, or otherwise high-signal families before empty or purely structural families.
- When no meaningful ontology is present, the workspace says so and falls back to ordinary note navigation rather than presenting empty ontology chrome as if it were complete.
- The per-type Overview and the type's native and custom views share one base-pane workspace per type, with a view selector that switches the base-pane content without disturbing any open floating note panes. [[unified-view-contract|SPEC-0110]] supersedes the former Home/Table toggle. ^SPEC-0014-US1-AC5
- The user's last view choice for a given type is remembered across reloads and across type switches; without a remembered choice or an authored default, a type opens its generated Table, as [[unified-view-contract|SPEC-0110]] specifies. ^SPEC-0014-US1-AC6

### US2 - Open a note, structural section, or embedded node and immediately see its typed identity, content, neighborhood, and structural context
- id:: ^SPEC-0014-US2
- summary:: Open a note, structural section, or embedded node and immediately see its typed identity, content, neighborhood, and structural context.
- status:: ready

#### Acceptance Criteria

- A selected note, section, or embedded node resolves to one canonical node workspace with authored format/provider, resolved type, title, canonical ref, author-facing locator, supported views, content, fields, collections, capabilities, status, and version. ^SPEC-0014-US2-AC1
- Relation groups are scoped to the focused node and preserve provenance clearly enough that users can tell structural ownership, ontology relations, ambient links, and generic relatedness apart.
- Structural tabs, outlines, and section cards are derived from provider-supplied structural data when present; relation rails and local graph views remain derived from the canonical node workspace, and root-only providers do not receive synthetic structure. ^SPEC-0014-US2-AC3
- Users can move from a focused node to related notes or embedded nodes without losing the pane stack they came from.

### US3 - Use note-focused semantic search, type filters, and NodeRef-aware results to discover the right node faster than path-only search
- id:: ^SPEC-0014-US3
- summary:: Use note-focused semantic search, type filters, and NodeRef-aware results to discover the right node faster than path-only search.
- status:: ready

#### Acceptance Criteria

- Search in the ontology workspace returns source notes or nodes, not flat file-hit rows as standalone destinations. ^SPEC-0014-US3-AC1
- Search results can be scoped by ontology type, current focused node, or current neighborhood when the backend supports that scope.
- Search results show bounded primary-chunk identity/ancestor context and enough provenance to choose the next node without opening every hit.
- Ontology-backed results include resolved NodeRef metadata or an explicit resolution warning so the browser can open them without client-side guessing. ^SPEC-0014-US3-AC4

### US4 - Keep open panes trustworthy when external file changes affect visible nodes
- id:: ^SPEC-0014-US4
- summary:: Keep open panes trustworthy when external file changes affect visible nodes.
- status:: ready

#### Acceptance Criteria

- When an external file change affects a node visible in an open pane, the workspace refreshes that node by canonical identity without forcing the user to rebuild the pane stack manually.
- Two panes anchored on different nodes in the same file can both stay current, keep separate titles and relation context, and avoid collapsing into one file-level refresh experience.
- Validation, dirty, rebased, conflicted, or stale state that becomes newly relevant after an update is surfaced at the affected node, field, or collection scope.
- If a node can no longer be resolved after a file change, the pane shows a scoped stale or conflict state rather than silently switching to the parent file.

### US5 - Copy a durable format-appropriate link target for the focused note or embedded node so implementation notes, code comments, and agent output can cite the precise contract
- id:: ^SPEC-0014-US5
- summary:: Copy a durable format-appropriate link target for the focused note or embedded node so implementation notes, code comments, and agent output can cite the precise contract.
- status:: ready

#### Acceptance Criteria

- Top-level note nodes expose an author-facing link target based on exact note path, alias, or preferred identifier, with a rendering appropriate to the source/target format.
- Structurally linkable Markdown embedded nodes expose a block-id link target when one exists, including Markdown and wikilink renderings; root-only HTML notes expose exact-path `href`-appropriate targets without creating block IDs.
- When the user invokes copy-link on a structurally linkable Markdown embedded node that has no block ID, the workspace runs `EnsureLinkTargetApply` for that single ref through the on-demand fix-up path so the copied target is durable. Workspaces MUST NOT auto-insert block IDs ambiently; insertion only happens on explicit user action. ^SPEC-0014-US5-AC3
- Copy-link, generated coderef, and agent-facing flows use the same link-target contract as the node workspace.

### US6 - Open a type-filtered workbench where notes are grouped by readiness by default, then filter, sort, and regroup without switching to a separate workflow page
- id:: ^SPEC-0014-US6
- summary:: Open a type-filtered workbench where notes are grouped by readiness by default, then filter, sort, and regroup without switching to a separate workflow page.
- status:: ready

#### Acceptance Criteria

- Opening a typed route such as `/notes/prompt-recipe`, `/notes/effort-note`, or `/notes/curation-finding` shows the general notes listing with the selected type or interface filter applied.
- Type-filtered routes default to grouping or sorting by readiness when readiness signals are available.
- Users can change the view to group by type, folder, enum/status field, validation issue state, modified state, or no grouping without leaving the listing.
- Users can filter by type/interface, readiness state, validation issue presence, missing required fields, missing required sections, enum values, relation presence/count, text search, and modified state when those dimensions are available.
- Each row shows enough schema-derived context to decide whether to open the note: title, path, resolved type, summary, readiness state, issue count, missing required fields/sections, key field chips, relation counts, and modification state when known.
- Selecting a row opens the same node workspace pane model used elsewhere, preserving the user’s listing filters and pane stack.
- The listing works for file-root note types and embedded-node types; embedded rows keep canonical node identity and link back to their parent note.
- When readiness cannot be computed for a note or type, the UI degrades to ordinary typed listing rather than inventing a misleading state.

### US7 - Use readiness grouping to identify which notes need human judgment, which can be delegated to an agent, and which are structurally ready
- id:: ^SPEC-0014-US7
- summary:: Use readiness grouping to identify which notes need human judgment, which can be delegated to an agent, and which are structurally ready.
- status:: draft

#### Acceptance Criteria

- The workbench distinguishes mechanical incompleteness from user-judgment gaps when the schema, validation result, or companion workflow can identify that distinction.
- Users can copy or reference the current filtered view as an agent handoff target, such as "finish incomplete PromptRecipe notes" or "review open high-severity curation findings."
- The product makes it clear which readiness gaps are directly fixable structure and which require partnership before delegation.
- Agents can use the same listing/readiness data through an API or CLI surface without scraping browser-only state.

### US8 - Open type-mounted and standalone views that share one source, mount, and variant model
- id:: ^SPEC-0014-US8
- summary:: Open type-mounted and standalone views that share one source, mount, and variant model.
- status:: ready

#### Acceptance Criteria

- View configs distinguish source, mount, and variants. ^SPEC-0014-US8-AC1
  Type-mounted views, standalone rail views, and future query UI recipes use the same saved view model instead of separate dashboard-specific contracts.
- Type-mounted views are discoverable from the matching type or interface row. ^SPEC-0014-US8-AC2
  The type row still opens the type home or overview. A hover or inline list affordance opens the configured default type-mounted view directly.
- Standalone views appear as independent side-rail navigation entries. ^SPEC-0014-US8-AC3
  Standalone views can use ontology type, query recipe, GraphQL, saved search, tag, or canonical-root-metadata-backed sources as those source adapters become supported.
- Types without explicit configs receive generated defaults. ^SPEC-0014-US8-AC4
  When a type or interface has no checked-in view file, Rhizome can generate a schema-informed default table view from the ontology summary, required fields, key fields, readiness inputs, and count data.
- View variants share one base note set.
  Table, card, and kanban presentations can be alternatives over the same source query, with variant-specific visible fields, grouping, and interaction rules.
- Opening a view result preserves node identity. ^SPEC-0014-US8-AC6
  Rows and cards open through the existing node workspace pane and edit-session model, including embedded nodes and sections.
- The configured view occupies the unified base-pane workspace, sharing the same view selector and floating-pane stack that the type Overview uses; clicking a row opens the row's note as a floating pane on top of the table without replacing the base pane. ^SPEC-0014-US8-AC7
- Single-clicking a different type swaps the base-pane content (Home or Table, per the destination type's sticky preference) without clearing the floating pane stack; double-clicking a type clears the floating panes. ^SPEC-0014-US8-AC8

### US9 - Choose visible fields, filters, sort and group rules, and supported variants from schema-informed options
- id:: ^SPEC-0014-US9
- summary:: Choose visible fields, filters, sort and group rules, and supported variants from schema-informed options.
- status:: draft

#### Acceptance Criteria

- The configuration UI proposes fields from known metadata.
  Schema fields, canonical root metadata, Markdown frontmatter properties, tags, validation/readiness signals, relation counts, path metadata, and modified state appear as selectable columns, card fields, filters, sorts, and grouping dimensions when available.
- Users can configure table and card variants without editing YAML.
  The UI lets users pick visible table columns, card properties, default sort, default filters, and grouping dimensions, then persists the durable configuration to the repo-backed view file.
- Kanban columns are derived from explicit fields.
  Kanban variants use enum/status-like schema fields, canonical root-metadata fields, or tag-derived dimensions for columns; Rhizome does not infer workflow columns from prose alone.
- Dragging between kanban columns stages an explicit field update.
  Moving a card between columns updates the configured column field through a provider-capability-compatible, reviewable edit-session path rather than mutating authored source invisibly.
- Ephemeral state and durable config stay separate.
  Temporary browser state, ad hoc search text, and unsaved column widths may stay in the URL or browser-local state; named view definitions are repo-tracked.

### US10 - Make safe row edits directly from a configured view without leaving the edit-session workflow
- id:: ^SPEC-0014-US10
- summary:: Make safe row edits directly from a configured view without leaving the edit-session workflow.
- status:: ready

#### Acceptance Criteria

- Enum fields are selectable from valid values. ^SPEC-0014-US10-AC1
  When a configured table column represents an editable enum field, the cell lets the user choose from the schema's valid enum options rather than typing freeform text.
- Checkbox-backed boolean fields can be toggled. ^SPEC-0014-US10-AC2
  When a configured table column represents an editable checkbox-backed boolean field, the cell exposes a toggle that stages completion changes for action-item-like nodes.
- Relation fields can be assigned from valid nodes. ^SPEC-0014-US10-AC3
  When a configured table column represents an editable single-node relation, the cell offers a filterable picker over valid target nodes such as Person notes for an assignee field.
- Edits use the shared session controls. ^SPEC-0014-US10-AC4
  The first view edit automatically starts an edit session, and all view edits appear in the same preview, dirty, discard, and commit controls as node-pane edits.
- Editor availability follows provider and node capabilities.
  A row exposes an editor only when its provider and node capabilities advertise the compatible web edit-session operation. HTML v1 rows remain readable, searchable, and filterable but do not expose web-originated metadata or link mutation.

## Requirements

### Must

- The ontology workspace MUST exist as a first-class product surface when a schema is present.
- The ontology workspace MUST let users start from note families, semantic search, or a selected note's typed neighborhood.
- The product MUST expose note type counts and enough type descriptions to make the ontology legible to humans.
- The product MUST provide a typed note explorer that can browse notes by family and show health or issue signals when available.
- The typed note explorer MUST be a general filter/sort/group listing surface, not a set of bespoke per-template dashboards.
- Type-specific routes MUST be prefiltered views of the same listing surface used by the all-notes browser.
- Type-specific routes MUST use readiness as the default sort/group criterion when readiness data exists.
- The main ontology workspace MUST support panes anchored on a whole note, a structural section, or an embedded node through one shared workspace model.
- The main ontology workspace MUST surface canonical ref, author-facing locator, authored format/provider, resolved type, key metadata, supported views, rendered or authored content, grouped relations, capabilities, and node-scoped status for the selected pane anchor.
- Derived browser views MUST stay projections over the canonical node workspace rather than separate primary payload families.
- The product MUST support note-focused semantic search rather than only path-first file search.
- Ontology-backed search results MUST preserve enough NodeRef/link-target metadata for the browser to open the returned node directly.
- The workspace MUST expose durable format-appropriate targets for linkable focused nodes. On explicit copy-link or "make linkable" action, structurally linkable Markdown nodes may run on-demand block-ID fix-up through the link-target service; root-only formats use their exact-path target and MUST NOT synthesize block IDs. The workspace MUST NOT eagerly flag absence of a block ID as a workspace-level warning; see [SPEC-0023](../technical/linkable-embedded-node-identifiers.md) for the usage-driven block-ID lifecycle.
- Assessment or health views MUST make ontology issues discoverable enough that the ontology can guide cleanup work.
- Open panes affected by external file changes MUST refresh without destroying the surrounding pane context.
- Readiness MUST be derived first from generic ontology and validation signals: required canonical root fields, unresolved validation issues, dirty/modified state, and declared relation/field metadata; required sections and embedded-node fields apply only when the provider exposes structural capabilities.
- Readiness MUST NOT require project-kb, spec-driven, or any other template-specific logic to be useful.
- Listing rows MUST expose schema-derived missing-field detail and, for structurally capable providers, missing-section detail without requiring the user to open every note.
- Listing rows MUST preserve canonical node identity for embedded nodes and sections so opening a result can focus the exact node.
- Filters MUST support at least type/interface, readiness, validation issue presence, text search, and modified/dirty state.
- Sorting MUST support at least readiness, title, modified time when known, issue count, and relation count when available.
- Grouping MUST support at least readiness, type/interface, folder/path prefix, and enum/status-like fields when available.
- Search, filters, sort, and grouping MUST compose predictably; applying one must not silently clear the others unless the user explicitly resets the view.
- The browser MUST clearly distinguish validation errors from readiness gaps. A note can be valid but incomplete for a workflow, or invalid regardless of readiness.
- Configured views MUST distinguish source, mount, and variants as separate concepts.
- Repo-authored view definitions MUST be tracked repository configuration, not ignored transient runtime state.
- A type-mounted view action MUST NOT replace the type home semantics; the type home remains the overview, while the direct view affordance opens the default configured list view.
- Standalone views MUST appear independently in the side rail instead of being hidden under a type row.
- Configured views MUST preserve the same node identity and pane-opening behavior as ordinary typed listings.
- View execution MUST compose search, structured filters, sort, grouping, and variant selection without silently dropping state.
- Safe configured-view field edits MUST be capability-gated and stage through the compatible shared edit-session workflow; view cells MUST NOT commit directly to authored source.
- Configured-view enum editors MUST use schema-valid options, not only values observed in the currently filtered rows.
- Configured-view relation editors MUST limit picker candidates to nodes that satisfy the schema-declared target type or interface.
- Kanban drag updates MUST go through an explicit, reviewable edit-session write path.

### Should

- The ontology workspace should offer a lightweight ontology summary or home view that answers where to start, what note types exist, and where the noise is.
- The product should allow users to pivot between the ontology workspace and graph/tree views rather than forcing one navigation model.
- The product should provide compare or split-note workflows for reading related notes side by side.
- The workspace should make inline expansion or preview of related note content possible when that can be done without losing context.
- The workspace should expose validation, dirty, and freshness state in the same node-centric affordance model instead of forcing users to infer state from file-level reloads.
- The listing should offer saved or shareable view presets once the filter model stabilizes.
- The listing should show a compact type-contract affordance near type-filtered views so users can jump to the full ontology type detail or authoring guide.
- The listing should support quick actions for safe structural affordances such as opening the missing field/section, adding a missing required section scaffold, or opening the relevant validation issue.
- Readiness should include a "needs decision" or similar state only when the source signal is explicit enough to avoid guesswork.
- Type home pages should summarize readiness counts, validation issue counts, and modified counts for the selected type or interface.
- Agents should be able to request the same readiness/listing data through an agent-safe read API so browser workbench state can become an execution target.
- View configs should support ontology type/interface and query recipe sources first, with GraphQL, saved search, tag, and canonical-root-metadata-backed sources added through the same source adapter shape.
- Native Rhizome view configs should own Rhizome-specific concepts such as NodeRef identity, ontology-aware readiness, query recipe sources, and edit-session actions.
- Obsidian `.base` compatibility should be treated as import/export or adapter support after the native Rhizome view contract is stable.
- The configuration UI should use schema, canonical root metadata, Markdown frontmatter adapters, tags, and indexed metadata to suggest fields rather than requiring users to type property names from memory.

### May

- The ontology workspace may surface richer authoring or repair guidance later once the note-family migration is farther along.
- The listing may later support board-like presentation for status-heavy note types, but the underlying model remains filters, grouping, sorting, and schema-derived readiness.
- Workflow-specific templates may ship saved view presets, but those presets must be declarative inputs to the general listing model.
- A single source note set may power multiple components or alternate variants, such as a recent subset, readiness queue, backlog board, or compact review card wall.

## Readiness Model

Readiness is a user-facing triage signal over the current note state. It is not the same thing as validation.

- `ready`: required ontology fields and any capability-applicable structural requirements are present, validation has no blocking issue for the node, and no known readiness rule marks the note incomplete.
- `incomplete`: required fields or capability-applicable sections, embedded fields, or structural blocks are missing or empty.
- `has issues`: validation reports unresolved issues that affect the note or node.
- `modified`: the note has uncommitted edit-session changes or known dirty state.
- `needs decision`: a workflow or companion-derived rule has identified a human judgment gap. This state is optional until the rule source is explicit and explainable.
- `unknown`: Rhizome cannot compute readiness for this note/type; the row should still be listable.

The first implementation should focus on `ready`, `incomplete`, `has issues`, `modified`, and `unknown`. `needs decision` should wait until the product has a declarative rule or explicit workflow signal strong enough to avoid implying human ambiguity from ordinary missing content.

Readiness inputs should come from existing or generalizable surfaces:

- ontology root-field requirements plus section requirements for structurally capable providers
- ontology assessment and validation issues
- embedded node assessment for repeated section entities
- edit-session dirty/modified state
- node workspace status and capability metadata
- optional future readiness rules declared through ontology guidance, companion docs, or a separate schema-owned configuration surface

## Listing UX Contract

The general note listing should behave like a data workbench:

- `/notes` starts as the all-notes browser.
- `/notes/<type-or-interface>` opens the type or interface home with the type/interface context applied.
- A type-mounted configured view opens from a type row action or explicit view route, such as `/notes/<type-or-interface>/views/<view-id>` if that route shape is adopted during implementation.
- A standalone configured view opens from the side rail, such as `/views/<view-id>` if that route shape is adopted during implementation.
- A type-filtered route defaults to readiness grouping when readiness data exists.
- Users can remove the type filter, add additional filters, change grouping, and change sorting without switching pages.
- View state should be URL-addressable enough that a user can share or hand off a useful filtered view.
- A row click opens the node workspace pane for the selected note, section, or embedded node.
- The listing must remain useful on small ontologies, partial ontologies, and mixed typed/untyped vaults.
- The listing should not introduce a second editing model; any edits flow through the existing ontology edit-session and node workspace capability model.

## Configured Views Contract

Configured views are saved collection experiences over notes or nodes. They are not a separate dashboard feature.

- `source` defines the base note or node set. Initial sources should include ontology type/interface and query recipe sources. Later sources may include GraphQL, saved search, tag, canonical root-metadata predicate, or composed subsets of another source.
- `mount` defines where the view appears. `type` mounts attach to a type or interface and power the direct list affordance from that type row. `rail` mounts appear as standalone side-rail navigation.
- `variants` define presentations over the source. Table is the default; card and kanban variants add display and interaction choices without changing the source identity model.
- `defaults` define initial search, filters, sort, grouping, visible fields, and selected variant.
- `schemaHints` or equivalent metadata may pin labels, key fields, card properties, kanban column field, and safe edit actions when schema inference is not enough.
- Config should live in tracked repo settings, preferably separate files such as `.rhizome/views/*.yaml` rather than hidden in ignored runtime state.
- Generated defaults are allowed when no view file exists; generated defaults should be explainable and should not create files until a user saves or customizes them.

## Constraints from Existing Specs

- `SPEC-0019` (`docs/specs/technical/node-workspace-capability-pipeline.md`) defines the canonical node workspace and `NodeRef` identity model. Listing rows that point at embedded nodes or sections must use canonical node identity rather than path-only identity.
- `SPEC-0030` (`docs/specs/experience/ontology-browser-navigation-model.md`) defines the pane-stack navigation model. Opening a row must enter that same pane model rather than replacing it with a separate list-specific detail screen.
- `SPEC-0017` (`docs/specs/product/validation-fixes-workspace.md`) defines grouped validation repair and review/apply boundaries. Readiness may surface validation issues, but safe repair and grouped repair application stay governed by the validation fixes workspace.
- `docs/reference/decisions/Keep skills generic and push note semantics into ontology surfaces.md` means readiness must stay schema-derived and generic; template semantics belong in ontology, companion docs, or saved views rather than hard-coded browser logic.
- `SPEC-0058` (`docs/specs/technical/configured-view-engine-and-repo-config.md`) defines the configured view engine, repo-tracked view config, source adapters, mount model, variants, validation, and safe write boundaries for reusable workbench views.
- `SPEC-0086` (`docs/specs/technical/format-aware-root-ontology-projection.md`) defines format-neutral typed/fallback roots and the root-only HTML boundary.
- `SPEC-0087` (`docs/specs/technical/format-aware-note-maintenance-mutations.md`) defines non-editor maintenance capabilities and keeps HTML web mutation read-only in v1.
- `SPEC-0088` (`docs/specs/experience/trusted-html-note-viewer.md`) defines the provider-selected reading view for trusted root-only HTML notes.

## Documentation Plan

- Update the ontology browser user-facing docs or hub note once the listing/readiness workbench is implemented.
- Update the web/API technical plan to describe the read model for readiness summaries, filters, grouping, and embedded-node rows.
- Add companion guidance for schema authors that explains which ontology fields, required sections, and future readiness-rule hooks affect the listing.
- Add examples that show project-kb `PromptRecipe`, spec-driven `EffortNote`, and validation `CurationFinding`-style queues as presets over the same generic listing model rather than bespoke dashboards.
- Document the native Rhizome view config format once the first implementation slice defines the exact YAML shape and supported source adapters.
- Document Obsidian `.base` compatibility only when import/export or adapter support exists.

## Open Questions

- which ontology health signals are most useful as default product affordances versus secondary debugging tools
- how aggressively the workspace should bias toward ontology-first navigation when a repo has only partial ontology coverage
- whether readiness should be exposed first through the existing `/api/ontology/types/{name}` and notes query surfaces or through a dedicated listing endpoint
- what minimum declarative rule surface is needed before the product can safely show `needs decision`
- exact route shape for type-mounted and standalone views
- whether the first implementation should support only ontology type/interface and query recipe sources, or include simple tag/frontmatter sources from the start
