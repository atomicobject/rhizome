---
type: TechnicalSpec
id: SPEC-0112
aliases: [SPEC-0112, Type collection views]
summary: "Every type and interface collection gets Table, Board, and Cards views whose defaults come from a schema-derived profile, enum values declare lifecycle stages that drive behavior while tone stays presentation, and a type Briefing replaces Overview."
spec-status: active
last-updated: 2026-10-07
---

# Type collection views

## Summary

[[view-preferences|SPEC-0114]] owns durable personal settings, concrete invocation identity, reset and migration, and explicit promotion into shared YAML. Its ignored repository-local SQLite store supersedes browser-only preferences and ephemeral expansion choices in this contract.

Selecting a type or interface opens its generated views. Those views work, but their defaults come from field names: a status field is found by matching `status`, `state`, or `done`, a summary by the name `summary`, a date by `date` or `due`, rows sort by title, and the same Issues and Updated columns appear everywhere. The result groups nothing for a `stage` enum, hides a meeting's date, shows Priority as "Unset" on every row, draws category enums with lifecycle marks, and offers a Board where one column holds most of the records. Overview shows the whole vault graph with the type highlighted.

This spec derives one **type profile** from schema metadata and uses it everywhere: generated sort, grouping, columns, and default view, the header summary, and each view's layout. It adds a **lifecycle stage** to enum values (`open`, `active`, `done`, `dropped`) so behavior such as "in motion", "stale", and "finished" reads a declared meaning instead of a color, with tone defaulting from the stage. It rebuilds the Table as the main working surface, offers the Board only where it works and gives it lanes, turns Cards into record briefs, and replaces Overview with a type Briefing built on the bundled-view platform from [[group-views-and-view-platform|SPEC-0111]].

It extends [[unified-view-contract|SPEC-0110]], which made generated Table the type default, and [[configured-view-engine-and-repo-config|SPEC-0058]], which owns native view execution and YAML configuration. Design rationale and comps: `web/prototypes/type-views/` in the authoring session (untracked), reviewed by Drew on 2026-10-03.

## Goals

- A type's default view, sort, grouping, and columns fit its shape without per-type configuration.
- Lifecycle behavior reads declared stages; tone only chooses color and icon.
- No generated default depends on a field's name.
- The Table supports reading, triage, and bulk edits without leaving the view.
- Board and Cards are offered only where they add something over the Table.
- The profile is computed once, on the server, and every consumer reads it.

## Non-Goals

- Shipping the type Briefing in this effort. Its requirements are recorded here; a later effort delivers them on SPEC-0111's bundled-view platform.
- Using git commit times as change times. Checkout and migration timestamps make change-based signals noisy in some repositories; that is follow-up work.
- Creating notes from a board column, aggregates such as sums, or editing ontology schema from the UI.
- Editing Drew's personal vault schema. Its enums keep working through inferred stages.

## Requirements

### Lifecycle stages

- `@view` on an enum value MUST accept `stage` with exactly one of `open`, `active`, `done`, or `dropped`; any other value is a fatal schema error.
- When any value of an enum declares a stage, every value of that enum MUST declare one; otherwise compilation fails naming the enum and the missing values.
- A value's effective tone MUST be its authored tone, else the default for its declared stage (`open` → `neutral`, `active` → `progress`, `done` → `success`, `dropped` → `muted`), else the existing fallback. Inferred stages never change tone or collapse, so enums without declared stages render as before.
- A value MUST be collapsed by default when it authors `collapsed: true`, or when it declares stage `dropped` and does not author `collapsed: false`.
- An enum without declared stages MUST infer stages only from authored metadata: authored tone `progress` → `active`, `success` → `done`, `muted` → `dropped`, any other authored tone → `open`; an untoned value that authors `collapsed: true` → `dropped`; any other value → `open`. An enum with no authored tone and no authored `collapsed` on any value has no stages.
- View capabilities MUST report each enum value's label, order, effective tone, collapsed default, and stage, marking whether the stage was declared or inferred. Type documentation MUST report each value's stage and whether it was declared beside SPEC-0111's `@view` label, order, tone, and collapsed fields; tone and collapsed there are the authored values, else the declared stage's defaults, and are omitted when only the existing fallback applies, so readers can still tell authored lifecycle metadata from a category enum.

### Type profile

The server MUST derive a profile for every type and interface from the compiled schema alone, and report it in type documentation and in the execution response of views sourced from that type or interface.

- **Lifecycle field:** the first single-valued enum field, KEY fields first and then declaration order, whose enum declares stages; otherwise the first single-valued KEY enum field with inferred stages. A Boolean or list field is never a lifecycle.
- **Ordered fields:** other enum fields whose enum has stages (declared or inferred). **Category fields:** enum fields with no stages.
- **Summary field:** the field with `@display(role: SUMMARY)`, else the conventional `summary` field.
- **Rank field:** the field with `@display(role: RANK)`, a singular Int or Float field, at most one per type. It is the type's own record order: ascending, missing values last.
- **Primary date field:** the first KEY Date or DateTime field, else the first required one; otherwise, for a type with no lifecycle field, its only Date or DateTime field that is not DETAIL importance.
- **People fields:** link fields whose target is the core identity type (`identity.CurrentUserType`).
- **Key text fields:** KEY scalar String fields other than the title, identifier, and summary.
- **Relation fields:** other forward link fields, KEY fields first. **Reverse fields:** `@reverse` fields and inbound `@neighbors` list fields.
- **Gap fields:** authored (scalar, enum, or link) KEY fields other than required fields, the lifecycle field, and fields `@requiresWhen` makes conditionally required. View capabilities MUST carry each field's `@policy` reason.
- **Shape:** `workflow` when the lifecycle has an `active` value; `contract` when it has a lifecycle without one; `dated` when it has a primary date and no lifecycle; `catalog` when it has a category field; otherwise `reference`.
- For ontology-sourced views, field roles MUST come from the profile. Name-based role guesses MAY remain only for sources without schema fields.

### Generated defaults

- Generated sort MUST be: the rank field ascending, then title, when the type has one; otherwise primary date descending for `dated`; changed descending within groups for `workflow` and `contract`; title ascending otherwise.
- Generated grouping MUST be: the lifecycle field for `workflow` and `contract`, with groups in stage order (`active`, `open`, `done`, `dropped`) and declaration order within a stage; the primary date by calendar month, newest first, for `dated`; the first category field for `catalog`; none for `reference`.
- Date and DateTime fields MUST be groupable by calendar month in any native view.
- Groups and board columns over a link field MUST list targets by their type's rank field, ranked targets first, then the rest by title. Authored `group.values` order still wins.
- Generated table columns MUST be, up to ten: title, identifier, implementing type for interfaces, lifecycle, KEY ordered and category fields, key text fields, people fields, relation fields, reverse fields as labeled counts, the primary or first date field, and Changed. Issues MUST remain available as a column and as a row marker but MUST NOT be a generated default column.
- Table MUST always be offered. Board MUST be offered when a lifecycle field exists, using it as the column field. Cards MUST be offered when a summary field exists.
- The generated default variant MUST be Board when the shape is `workflow`, at least two `open` or `active` lifecycle values have records, and at most 60 records are `open` or `active`; otherwise Table. The catalog's default choice for the type MUST make the same decision.
- An authored view, including one that replaces the generated layouts, keeps its own configuration; profile-derived defaults apply only where it leaves a setting unspecified.

### Execution statistics

The execution response for a type or interface source MUST include statistics computed over every matching row before paging:

- Total, issue count, and stale count, where a stale record holds an `active` lifecycle value and has not changed for 30 days.
- Record counts per lifecycle value, and per implementing type for an interface.
- Filled counts for every generated column field and every KEY field.
- For a primary date field, the first and last dates and record counts per calendar month.

### Header

- The collection header MUST replace the record-count and mean-links line with a facet line: total; implementing-type counts for an interface; one facet per populated lifecycle value with its status mark and count; for a primary date, the date span, a per-month spark of the last 24 months, and this and last month's counts; gap fields empty on some records, as "<field> empty n/N" with the field's policy reason as a tooltip; the stale count; and issues or "no issues".
- The header's gap facets MUST come from the profile's gap fields.
- Lifecycle, implementing-type, gap, stale, and issue facets MUST apply the matching filter when activated, and show as active filter chips. Native views MUST support a `missing` filter operator that matches rows where a scalar, enum, or link field has no value.
- While a type or interface collection view is showing, the Notes rail's list of that collection's records MUST start collapsed; the reader can expand it.

### Table

- When the type has a summary field, rows MUST show it as a clamped second line under the title; a Rows control MUST switch between two-line and one-line rows and persist with the other view state.
- Columns whose field is empty on every matching row MUST be hidden, except title and lifecycle, and listed as hidden-empty in the Columns menu, from which the reader can show them.
- Reverse fields MUST render as counts under a header named by the target's plural label. Changed MUST render as a relative age with the absolute time on hover.
- Activating a row MUST show that record in the workspace's right rail: title, summary, editable lifecycle, ordered, category, and relation fields, its reverse relations with status marks, and an Open action. Enter or double-click MUST open the note in a tab.
- The reader MUST be able to select several rows (checkbox, shift-range, `x`) and set an enum field or add a link value on all of them. Bulk edits MUST stage through the edit session exactly like single-cell edits. Escape clears the selection.
- The Table MUST load further pages as the reader scrolls instead of showing a pager. Grouped tables keep group headers and counts across loaded pages.
- The Table SHOULD support column resizing and multi-key sort with shift-activation, persisted with the view state.

### Board

- Lifecycle values with no records that are not collapsed MUST render as narrow labeled strips that still accept drops. Collapsed values MUST render as narrow strips with counts.
- A Lanes control MUST offer ordered fields, people fields, and relation fields. The default lane field MUST be the first KEY ordered field with values, else the first KEY relation field with values whose filled records average at most 1.25 targets, else none. A record whose lane field holds several targets MUST appear in each of those lanes and say how many others. Lanes for a relation MUST be ordered by the target's rank, ranked targets first, then by its lifecycle stage and then record count; enum lanes by enum order; the lane for records without a value comes last.
- A card MUST show the title, a two-line summary, the first filled key text field with its label, ordered-field tags, the first filled relation field other than the lane field, reverse fields as labeled counts, people as initials, and Changed. A card in an `active` value unchanged for 30 days MUST show a stale marker, and the column header MUST count them.
- A column holding more than 24 cards MUST render compact cards showing the title and, for an interface, the implementing type.
- The board's column field MUST be switchable among the lifecycle and ordered fields.

### Cards

- Cards MUST render record briefs grouped into sections by lifecycle value in the generated grouping order. A brief shows: the type or implementing-type label, ordered-field tags, people, Changed, the title, the summary clamped at six lines, each filled key text field as a labeled callout, each filled relation field as labeled chips, and each filled reverse field as a list of up to four records with status marks and a count of the rest.
- Lifecycle and ordered-field tags on a brief MUST be editable through the edit session.

### Save view

- An explicit shared-configuration Save action MUST make the settings being published reviewable and write the selected filters, sort, grouping, visible columns, row density, board column and lane fields, and variant to view YAML under `.rhizome/views/`, directly and without a staged review, because view YAML is trusted repository configuration reviewed through version control. From a generated view, it creates a native view mounted on the type or interface with `mount.replaceGenerated: true`. From an authored view, it updates that file's `defaults` and `variants` in place, preserving comments and keys it does not own. Alternative considered: staging the YAML change through the edit session's review flow.
- Temporary search MUST be excluded unless explicitly included for publication. After Save succeeds, only promoted personal overrides clear; widths and expansion choices remain, including across creation of an authored view ID ([[view-preferences|SPEC-0114]]).
- Save MUST validate the result with the same checks as `rzm validate views` and refuse to write a definition that fails them.

### Briefing (delivered after SPEC-0111 lands)

- A type Briefing MUST replace Overview as a selectable view for every type and interface, built as a bundled kit view mounted on `type: "*"` and `interface: "*"` that shares modules with SPEC-0111's group Briefing.
- It MUST show: needs attention (validation issues; warning and risk enum values; stale records; KEY fields empty on some records with policy reasons; reverse fields that more than half the records fill but some do not); in motion (`active` lifecycle values, newest first, with the first key text field); recent changes with bursts of four or more same-minute changes collapsed; a distribution bar per lifecycle, ordered, and category field; a per-month chart of the primary date; per relation and reverse field, how many records fill it and its most common targets; notes of other types that link in; and the type's companion guide.
- Overview MUST remain selectable until the Briefing ships.

### Starters

- The Agentic Engineering starter MUST declare stages: SpecStatus `proposed` open, `active` done, `superseded` and `archived` dropped; EffortStatus `planned` open, `active` active, `complete` done, `archived` dropped; UserStoryStatus `draft` and `ready` open, `satisfied` done. The complex-domain starter MUST declare stages for RequirementStatus (`candidate` open, `accepted` active, `implemented` done, `superseded` and `rejected` dropped) and DomainLifecycleStatus (`draft` open, `active` done, `superseded` and `archived` dropped).
- Authored tones that equal their stage default MUST be removed from the starters; tones that differ, such as UserStoryStatus `ready` as `info`, stay.

### Verification

- Go tests MUST cover stage parsing and the all-or-none rule, effective tone and collapsed defaults, stage inference, profile derivation for each shape including interfaces and inferred stages, generated defaults per shape, month grouping, the default-variant rule and catalog agreement, execution statistics over paged results, board lanes including multi-target records, and Save view creation, in-place update, and refusal.
- Web unit tests MUST cover the facet line and its filters, hidden-empty columns, two-line rows, the right-rail record, bulk edits through the edit session, scroll loading, board strips, lanes, stale markers, compact cards, and record briefs with tag edits.
- Browser tests MUST cover one `workflow`, one `dated`, and one `catalog` fixture type opening with the right default, a bulk edit, a board lane switch, and Save view.
- The views MUST be checked visually against this repository and a copy of a real vault before delivery.

## Decisions

Drew made these decisions on 2026-10-03 after reviewing the comps:

- **Defaults follow the type's shape.** Board for workflow types whose open work spans at least two columns; Table elsewhere.
- **Cards become record briefs** rather than being retired.
- **Stages are primary; tone defaults from stage** and can be overridden for color only.
- **The profile is computed once in Go** and served to every consumer, rather than recomputed in browser code.
- **This work is independent of SPEC-0111's branch** and ships as separate pull requests; the Briefing waits for SPEC-0111's platform.

## Open questions

None. Drew accepted on 2026-10-03 that Save view writes YAML directly and that spec `active` shows the `done` checkmark.

## Documentation plan

- Ontology subsystem note and the ontology schema authoring template: `@view(stage:)`, the all-or-none rule, defaults, and inference.
- Rhizome skill references for ontology authoring and views: stages, the type profile, generated defaults by shape, month grouping, lanes, and Save view.
- Views and web context notes: execution statistics, the right-rail record, and bulk edits.
- Starter READMEs where they describe status enums.
- CHANGELOG.
- Reconcile SPEC-0110 and SPEC-0058 so they cite this spec where it changes generated defaults and view configuration.
