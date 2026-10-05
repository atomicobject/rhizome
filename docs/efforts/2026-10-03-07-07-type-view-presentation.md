---
type: EffortNote
id: EFF-2026-10-03-07-07
aliases: [EFF-2026-10-03-07-07]
name: Type view presentation polish
created-at: 2026-10-03T11:07:37Z
status: complete
summary: "Show link values as target titles, let link fields group views, let an authored type view replace the generated layouts, and replace the view select with a segmented switcher."
---

# Type view presentation polish

## Scope

Fix four rough edges Drew found while using the DAI IP views in his vault on 2026-10-03:

1. Link values display raw wiki-link text, for example a group header `[[IP opportunity - AI in the products we build]]`. Everywhere a link-typed field or Rhizome link value is displayed, show the target note's title.
2. Configured views can group by a link field through configuration, but the toolbar's Group control neither offers link fields nor shows the selected one.
3. Views sourced from one type appear as separate sidebar entries beside the type, and a type-mounted authored view only adds more choices beside the generated Table, Cards, and Board. An authored view must be able to take the generated view's place, and authoring guidance must mount type views on their type by default.
4. The view switcher is a native select with a redundant "Use configured default" button. Replace it with a segmented button group of the standard presentations, one icon segment for a single custom view or a menu for several, and a default marker.

Excluded: removing standalone mounts, which remain the way to create a dedicated sidebar entry; changes to Overview, Structured, or Source behavior; publishing.

## Spec Set (Frozen)

- [[unified-view-contract|SPEC-0110]], revised by this effort for `mount.replaceGenerated` and the `custom` choice flag.
- [[configured-view-engine-and-repo-config|SPEC-0058]], revised for the `replaceGenerated` mount field.

## Stories In Scope (Frozen)

Requirements-only delivery of the SPEC-0110 and SPEC-0058 revisions above, plus the link-title and grouping fixes, which restore existing intent (titled relation values) rather than add requirements.

## Spec Coverage Checklist

- [x] Link fields are groupable; group buckets key on the canonical target and label with its title.
- [x] Link values display as target titles across configured views and other surfaces.
- [x] `mount.replaceGenerated` replaces the generated layouts and inherits the default role; other authored choices are flagged `custom`.
- [x] Segmented switcher with custom-view icon or menu and a default marker; the reset button is gone.
- [x] Authoring guidance mounts type views on their type by default.
- [x] Gates, browser verification against Drew's vault, and independent review recorded.

## Plan

The orchestrator first committed the shared contract fields (`MountSpec.ReplaceGenerated`, `ViewChoice.Custom`, OpenAPI, generated TypeScript) as `ca97fb6`, so four workers could branch from one contract. Each worker owns an isolated worktree and runs `make check` and `make web-e2e` on its own port:

1. Links lane: link groupability, canonical bucketing, titled group labels, and every configured-view surface (group headers, board columns, filter values, cards).
2. Catalog lane: `replaceGenerated` validation and choice composition, `custom` flags, consumers of generated IDs, stored-selection fallback, skill templates, SPEC-0110, SPEC-0058, subsystem notes, and the changelog.
3. Switcher lane: the segmented switcher and its callers, unit tests, and browser specs.
4. Sweep lane: link-title audit and fixes outside configured views.

The orchestrator merges the branches, resolves conflicts, runs `make check`, `make web-e2e`, `make build`, documentation validation, and template idempotence. It then moves the two DAI IP views in Drew's vault to type mounts with `replaceGenerated: true`, verifies them in a browser against a disposable copy of the vault, and obtains an independent review of the integrated diff.

## Plan Approval

Drew directed this work in chat on 2026-10-03 and asked for Opus 5.5 high subagents to divide it. The local Rhizome current user is unconfigured, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

All four rough edges fixed on one local branch, with Drew's DAI IP views showing once per type, using their authored layouts as the type's Table, Cards, and Board, and grouping ideas under titled opportunity headers.

## Actual Delivered

All four rough edges are fixed on `t3code/link-view-navigation` (local, unpushed), verified against a disposable copy of Drew's vault and through the full local gate.

- Link fields are groupable. Groups, board columns, and filter options key on the canonical target link and show the target's title; unresolved links show their alias or text without brackets.
- Link values show target titles on note pages (properties, Parent, read-only editors) and in hover previews, through indexed target refs.
- `mount.replaceGenerated` lets a native type or interface view replace the generated Table, Cards, and Board, inheriting the default role; other authored choices are flagged `custom`. Skill guidance mounts single-type views on their type.
- The segmented switcher replaces the select: standard presentations as segments, one custom view as an icon segment or several in a menu, a default dot, and no reset button. Picking the default clears the remembered choice.
- Beyond the original scope, at Drew's direction to use the database and indexes: note-type views read link targets in two batched catalog queries instead of one resolution per note, relation-count columns no longer walk the vault, and the note page carries the parent title instead of a separate preview request. Both IP views on the vault copy dropped from ~75 ms to ~30 ms per execution.
- Drew's two DAI IP view files now mount on their types with `replaceGenerated: true`.

## Execution Notes

- 2026-10-03T11:07:37Z [decision] `replaceGenerated` is an explicit opt-in rather than implied by `mount.default`, because SPEC-0110 keeps generated views selectable beside authored views that may filter the collection. A replacing view takes the generated slot, including its default role unless another authored view is the explicit default.
- 2026-10-03T11:07:37Z [environment] Native agent definitions do not offer Opus 5.5 at high effort mid-session; workers run through T3 delegation (`claude-opus-5-5`, effort high) in orchestrator-created worktrees `views-polish/{links,catalog,switcher,sweep}`.
- 2026-10-03T11:40:00Z [implementation] Merged the four lanes: catalog `d3ab0db` (replacement, `custom` flags, guidance, SPEC-0110/0058), links `e25477e` (link groupability, titled groups, board columns, filter titles), sweep `02da304` (titled `NodeFieldState.links`, property panel, read-only editors, preview aliases), switcher `65ef544` (segmented switcher, default marker, standalone header). Only `CHANGELOG.md` conflicted. Picking the default now clears the remembered choice (`f17ab5e`) so later default changes apply.
- 2026-10-03T11:40:00Z [validation] Integrated `make check` passes (Go units, lint, typechecks, 1039 web tests). `RHIZOME_E2E_PORT=4220 make web-e2e`: 59 passed, 1 failed (`app.spec.ts:298` Markdown save commit returned non-OK while a 2,486-note vault copy indexed concurrently), 7 did not run; the failing test passes 3/3 in isolation. A full rerun is required after review fixes.
- 2026-10-03T11:40:00Z [validation] Browser check against a disposable copy of Drew's vault (embeddings disabled, fresh index) with both IP views moved to `kind: type` plus `replaceGenerated: true`: the DAI IP group lists each type once; IP ideas opens the authored table as the type's Table, grouped under opportunity titles in title order, with Group showing "Opportunities" and the switcher showing Overview, Table (default), Cards, Board; IP opportunities shows titled Impact links; an idea's properties show titled links.
- 2026-10-03T11:40:00Z [review] Independent review (Opus 5.5 high) confirmed choice composition, defaults, keyboard behavior, and batched view hydration. Findings being fixed: link group value/key taken from the first sorted row; Parent title fetched by a separate preview request repeated on every staged edit; note-page field links re-resolving text instead of reading indexed target refs, twice when bodies are selected; stale "Use configured default" guidance; unresolved alias labels; duplicate facet titles; fallback default order; standalone label prefix stripped by string in the web.
- 2026-10-03T12:20:00Z [implementation] Review fixes merged: `26cae9a` keys link groups, overrides, kanban columns, and facets on the canonical target link `[[<vault path>]]` (the spelling relation edits already write) and shares one alias-preferring unresolved-link label; `0707e3e` names standalone layouts plainly, removes stale guidance, and marks the custom-view menu as pressed; `eb7140e` adds `NodeWorkspaceProjection.parentTitle` and reads note-page link targets from indexed field rows in one batched read shared by `fields` and `bodies`.
- 2026-10-03T12:20:00Z [learning] Drew asked that the work use the database and indexes. Measuring the vault copy showed views with link columns cost far more per row (IP ideas, 14 rows: ~75 ms; Meeting, 200 rows without links: ~40 ms). Note-root view rows carried no indexed field values, so each note's link text was resolved separately, and relation-count columns walked the vault. `551ace0` reads note link targets in two batched catalog queries per execution and stops the count-column walk. A read-count test pins 200 rows to one locator read and one field-value read; `BenchmarkExecuteNoteLinkFields` (300 notes, 3 link fields) improved from a 55.5 ms to a 23.4 ms median. The vault copy now executes both IP views in ~30 ms.
- 2026-10-03T12:45:00Z [validation] Final branch `551ace0`: `make check-full` exit 0 (race-enabled Go units, integration, benchmark contracts, 1039 web tests); `RHIZOME_E2E_PORT=4250 make web-e2e` 67 passed; `./scripts/rzm validate` and `frozen-scope-drift` 0 issues; `rzm init --check` reports everything up to date.
- 2026-10-03T12:20:00Z [handoff] Drew's vault `.rhizome/views/dai-ip-ideas.yaml` and `dai-ip-opportunities.yaml` now mount on `IPIdea` and `IPOpportunity` with `replaceGenerated: true` (mount block only, uncommitted in that repo). The dev binary in Drew's main checkout validates the new files with 0 issues and ignores the new field until rebuilt.

## Deviations

- 2026-10-03: Efficiency work (indexed note link targets, relation-count canonicalization, `NodeWorkspaceProjection.parentTitle`) was added after Drew asked mid-effort that the implementation use the database and indexes.
- 2026-10-03: A standalone view's own layouts are now named plainly ("Table", "Cards", "Board") because its header shows the view name; only `custom` choices keep the "<View> · <Layout>" form.
- 2026-10-03: Link fields report `groupable: true` in ontology field capabilities, so GraphQL clients see the change too; the GraphQL schema shape is unchanged apart from the additive `NodeFieldState.links` and `NodeWorkspaceProjection.parentTitle`.
- 2026-10-03: The SPEC-0058 revision tripped frozen-scope drift on [[2026-10-02-12-13-view-manual-ordering|EFF-2026-10-02-12-13]], which holds that spec as context only; its Deviations record the acknowledgment.

## Closure Checklist

- [x] Required quality gates pass.
- [x] Alignment and actual outcomes are verified.
- [x] Specs and documentation are reconciled.
- [x] Follow-ups are triaged.

## Compounding Follow-ups

- `Scope.ensureNoteState` loads metadata, types, and issue flags for every vault note on each `TypeInstances` call and is now the largest cost in small views (~9 ms on a 2,000-note benchmark). Narrow it through the `ExactNoteMetadataRows` hook in a separate change.
- Inline body wikilinks (for example `assigned-to:: [[notes/people/alice]]` in item text) still read as link paths; changing narrative link text is Drew's decision, and the API already carries the data.
- Starter views (efforts, specs, user stories, action items) are candidates for type mounts with `replaceGenerated`; changing them affects downstream repositories, so it waits for Drew.
- Link filters without facet options use a text box; offering a note picker would be new work.
- `replaceGenerated` on a hidden view is ignored silently; a warning would help authors.
- `app.spec.ts:298` (Markdown save) failed once while a large vault indexed concurrently and passed in isolation and in the final suite; watch for recurrence.

## Status

Complete. Delivered and verified locally on `t3code/link-view-navigation`; pushing and opening a pull request await Drew's request.
