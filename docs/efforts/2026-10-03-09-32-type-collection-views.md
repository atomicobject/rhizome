---
type: EffortNote
id: EFF-2026-10-03-09-32
aliases: [EFF-2026-10-03-09-32]
name: Type collection views
created-at: 2026-10-03T13:35:22Z
status: complete
summary: "Deliver SPEC-0112's lifecycle stages, schema-derived type profile, shape-based generated defaults, rebuilt Table, Board, and Cards, and Save view as separate pull requests independent of SPEC-0111."
---

# Type collection views

## Scope

Deliver [[type-collection-views|SPEC-0112]] except its Briefing section, as four pull requests built from `main` on branch `feat/type-views` and its children, independent of the SPEC-0111 group-views branch.

Excluded: the type Briefing (waits for SPEC-0111's bundled-view platform on `main`; a later effort), switching SPEC-0111's views to read stages, git commit times as change times, note creation from board columns, and Drew's personal vault schema.

## Spec Set (Frozen)

- [[type-collection-views|SPEC-0112]], authored in this effort, all sections except "Briefing (delivered after SPEC-0111 lands)".
- [[unified-view-contract|SPEC-0110]] and [[configured-view-engine-and-repo-config|SPEC-0058]] supply the generated-view and native-execution contracts this effort extends; reconciled in batch 1 and batch 4.

## Stories In Scope (Frozen)

Requirements-only delivery. Every SPEC-0112 requirement outside the Briefing section is in scope.

## Spec Coverage Checklist

- [x] Lifecycle stages: parsing, all-or-none, tone and collapsed defaults, inference, reporting (#8).
- [x] Type profile derived in Go and reported in type documentation, the kit, and execution responses (#8).
- [x] Generated defaults by shape: sort, grouping, month buckets, columns, offered variants, default variant with catalog agreement (#8).
- [x] Execution statistics over all matching rows, with a truncation flag (#8).
- [x] Header facet line with filters; Notes rail record list collapsed for collections (#9).
- [x] Table: two-line rows, hidden-empty columns, reverse counts, relative Changed, right-rail record, bulk edits, scroll loading, column resize and multi-key sort (#9).
- [x] Board: strips, lanes, card content, stale markers, compact cards, switchable column field (#10).
- [x] Cards: record briefs with editable tags and status marks on linked records (#10).
- [x] Save view: create and in-place update with validation, writing YAML directly as Drew accepted (#8 server, #10 client).
- [x] Starter stages and tone cleanup; generated copies refreshed (#8).
- [x] Documentation plan items (#8–#10).
- [ ] Visual check against a copy of Drew's real vault. Not performed; Drew is evaluating the merged views in his vault directly. Worker screenshots against this repository and E2E fixtures were reviewed.
- [x] Gates, browser tests, independent reviews, pull requests (see Closure Checklist).

## Plan

### Current gap

Generated views are built in `pkg/app/views/defaults.go` from name heuristics (`generatedGroupField`, `generatedNamedField(fields, "summary")`, `semanticRoleForKey`), sort by title, and always add Issues and Updated. Enum `@view` carries label, order, collapsed, and tone (`pkg/ontology/schema_directives.go`), and tone falls back to `progress` for every untoned value after the first (`EnumType.Tones`). The web Table (`web/src/components/ConfiguredView*.tsx`) clips every cell to one line, pages at 200 rows, and has no selection or right-rail record. The Board renders every value as a full-width column with no lanes. Cards are read-only label grids. Execution responses (`pkg/app/views/types.go`) carry no profile or whole-result statistics. No endpoint writes view YAML. PR #7 already delivered the segmented view switcher and `mount.replaceGenerated`.

### Contract (batch 0, parent-owned)

Before workers start, the parent commits the shared contract to `feat/type-views` so batches can run in parallel:

- `ontology.EnumValueView.Stage` and the stage constants; `ontology.TypeProfile` and `ontology.DeriveTypeProfile(schema, typeName, personType)` with a stub body.
- `views.ExecuteResponse.Profile` and `.Stats`, `views.FieldEnumValue.Stage` and `.StageDeclared`, board lanes, the month bucket on `viewconfig.GroupSpec`, `viewconfig.KanbanVariant.LaneField`.
- The same shapes in `pkg/app/web/openapi.yaml`, regenerated into `web/src/api/generated.ts`.

### Batches

| Batch | Outcome | Owner and scope | Exit evidence |
| --- | --- | --- | --- |
| 1. Schema (PR 1) | Stages compile, validate, default tone and collapse, infer, and report; the profile is derived for every type and interface and served in type documentation; starters declare stages. | Opus 5.5 high worker, `tv/schema`: `pkg/ontology` (directives, schema types, profile, reference docs), starter ontologies and refreshed repo copies, ontology subsystem note, authoring template and skill reference. | Focused `go test ./pkg/ontology/...`, `make check`, `make build && rzm init && rzm init --check`, `rzm validate`. |
| 2. Engine (PR 1) | Generated defaults by shape, month grouping, default variant with catalog agreement, execution statistics, lanes, profile roles replacing name guesses for ontology sources, and the Save view endpoint. | Opus 5.5 xhigh worker, `tv/engine`: `pkg/app/views`, `pkg/ontology/viewconfig`, `pkg/app/web` view routes and OpenAPI, views skill reference. Consumes the batch 0 profile type; does not edit the profile derivation. | Focused `go test ./pkg/app/views/... ./pkg/ontology/viewconfig/... ./pkg/app/web/...`, `make check`, `make integration` if the compiled GraphQL schema changes. |
| 3. Table and header (PR 2) | Facet line with filters, collapsed rail list, two-line rows, hidden-empty columns, reverse counts, relative Changed, right-rail record, bulk edits, scroll loading, column resize and multi-key sort. | Opus 5.5 high worker, `tv/table`: `web/src/components/ConfiguredView*.tsx` table paths, header, Notes shell rail, `web/src/configured-view.css`. Develops against contract fakes, then merges the integrated backend. | Web unit tests, `make check-fast`, `RHIZOME_E2E_PORT=4183 make web-e2e`, screenshots. |
| 4. Board, Cards, Save (PR 3 and PR 4) | Board strips, lanes, cards, stale markers, compact cards, column-field switch; record briefs with tag edits; Save view UI. | Opus 5.5 high worker, `tv/board`: board and card components and their CSS, Save action in the toolbar. | Web unit tests, `make check-fast`, `RHIZOME_E2E_PORT=4184 make web-e2e`, screenshots. |
| 5. Integrate and review | Batches merged into `feat/type-views` children per PR, E2E fixtures for workflow, dated, and catalog types, visual check against this repository and a copy of Drew's vault, independent Opus 5.5 review, fixes, SPEC-0110 and SPEC-0058 reconciliation, CHANGELOG. | Parent, with one Opus 5.5 xhigh reviewer. | `make check`, `make web-e2e`, `rzm validate`, review findings resolved, PRs open. |

Pull requests: PR 1 (schema and engine) targets `main`; PR 2 (table and header), PR 3 (board and cards), and PR 4 (Save view) stack on PR 1 so each reviews only its own changes. Workers commit after every deliverable so a restart loses little.

### Decisions and escalation

Settled: the decisions recorded in SPEC-0112. Batch 1 fixes the stage semantics and profile that batches 2 to 4 depend on; the parent reviews the batch 1 contract before batch 2 relies on its derivation, without pausing for Drew. Escalate to Drew only for a material change to SPEC-0112's behavior, the open questions it lists, or a destructive action.

### Authorization

Drew approved this plan in chat on 2026-10-03, after reviewing the comps, the four decisions, and the proposed pull-request split: "Ok. Let's do it. Use Opus 5.5 sub-agents to divide and conquer at appropriate reasoning levels." This covers implementation, review fixes, and opening the pull requests, not merging. No current user is configured in this checkout, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

Opening a type shows a view that fits its shape: IP ideas and requirements as boards with lanes, specs and efforts as tables grouped by lifecycle, meetings as a month-grouped log, reference docs grouped by kind. The header summarizes the collection and filters on click; the Table supports reading, triage, and bulk edits; Board and Cards appear only where useful; lifecycle behavior reads declared stages.

## Actual Delivered

Delivered as three squash merges to `main`, each with Greptile 5/5, zero open review threads, and green CI on its final head:

- `9267a7e` (#8): lifecycle stages, the schema-derived type profile in Go, profile and stages in type docs and the kit, shape-based generated defaults with catalog agreement, month grouping, execution statistics, board lanes and column-field switch, `missing` and `stale` filters, policy reasons, titles and status on linked records, the Save view endpoint, starter stages, and root-cause fixes for seven pre-existing flakes (note-cache refresh after saves, Overview graph remounts, four unit-test races, and E2E network and locator dependencies).
- `0827d47` (#9): the facet header, two-line table, hidden-empty columns, right-rail record, bulk edits, scroll loading, column resize and multi-key sort, and type-views E2E fixtures.
- `64ea0ef` (#10): board strips, lanes, cards, stale markers, compact mode, record briefs, and the Save view client.

Before merging, the stack integrated #11 (SPEC-0111 group views); type docs keep SPEC-0111's flat enum fields plus `stage` and `stageDeclared`, and bundled group views keep their tone rules because tone defaults from declared stages.

## Deviations

- 2026-10-03T14:44:05Z: Pull requests are three stacked branches instead of four. The board worker merged the in-progress engine and table branches to test against live data, so board, Cards, and the Save view client ship together as PR 3 on top of PR 2 (table and header), which stacks on PR 1 (schema and engine). Scope is unchanged; this is packaging within the approved plan.
- 2026-10-03T14:44:05Z: SPEC-0112 was clarified during execution without changing intent: tone and collapse defaults come from declared stages only; list enums are never a lifecycle; the primary date prefers a KEY date over a required one; gap fields are authored KEY fields; the profile carries gap fields and capabilities carry policy reasons; native views gain a `missing` filter operator; reverse-relation status marks in record briefs moved to follow-up.

## Execution Notes

- 2026-10-03T13:35:22Z: SPEC-0112 and this effort authored on `feat/type-views` from `main` at `213b51d` (PR #7). SPEC-0111 is open on another branch and allocated SPEC-0111 there, so this spec takes SPEC-0112.
- 2026-10-03T13:41:00Z: Batch 0 contract committed (`8449c30`, `3852300`, `7499cf8`); four Opus 5.5 workers launched in isolated worktrees (`tv/schema` high, `tv/engine` xhigh, `tv/table` xhigh, `tv/board` high).
- 2026-10-03T14:02:00Z: Batch 1 complete and fast-forwarded (`68055dd`); parent reviewed stage inference, the all-or-none rule, collapse defaults, and profile derivation against SPEC-0112. Noted for review: interface gap fields ignore `@requiresWhen` declared on implementing types.
- 2026-10-03T14:30:00Z: A reported list-link filter defect was the table worker's fixture writing multi-target lists as one entry; retracted, test-only coverage kept (`cb72e7a`).
- 2026-10-03T14:42:00Z: Batch 2 complete (`3acfa87`) and merged into `feat/type-views`; batch 4 complete on `tv/board`. Independent Opus 5.5 xhigh review of PR 1 started.
- 2026-10-03T15:40:00Z: Independent Opus 5.5 reviews of PR 1 and PRs 2–3 reported one blocker each (duplicate replacing views on Save; Save deleting authored density and lane settings) plus should-fix items. PR 1 fixes landed `ef298d9`..`316c03a`, adding a server stale filter, status on linked records, explicit no-grouping saves, and Save semantics where omitted settings stay unchanged. Stage group order now applies only to generated views and declared stages, so vaults without stages render as before.
- 2026-10-03T16:05:00Z: Two pre-existing E2E flakes fixed at the root and carried on PR 1: saves did not refresh the live note cache that serves reads (`9b28623`; about 1 in 600 runs), and the Overview graph remounted on count-only summary refreshes or when the graph beat the summary (`8c5d707`, `a5460f8`). Evidence: 200/200, 40/40, and 60/60 targeted repeats and three consecutive full-suite passes.
- 2026-10-03T21:05:00Z: Merged `main` at `b21900e` (#11, [[group-views-and-view-platform|SPEC-0111]]) into `feat/type-views`. Type documentation now uses SPEC-0111's flat enum value fields plus `stage` and `stageDeclared`; the nested `view` object is gone. Tone and collapsed there are authored, else a declared stage's defaults, so the bundled group views' tone rules keep working; spec `active` keeps `stage: done` with no tone, which satisfies SPEC-0111's no-`progress` rule.

## Closure Checklist

- [x] Required quality gates pass: `make check`, `make integration`, and full `make web-e2e` on every branch before each merge (final runs 74, 78, and 80 browser tests), `rzm init --check`, `rzm validate`, and CI green on all three PRs.
- [x] Alignment and outcomes verified: two independent Opus 5.5 reviews (one blocker each, all findings fixed) and Greptile loops to 5/5 on each PR. Visual check against a real-vault copy is carried forward to Drew's own evaluation.
- [x] Specs and documentation reconciled: SPEC-0112's open questions resolved, SPEC-0110 and SPEC-0058 cite it, subsystem, CONTEXT, skill, and kit references updated.
- [x] Follow-ups triaged below; the Briefing and stage-reading group views move to EFF-2026-10-03-18-33.

## Compounding Follow-ups

- Workers testing against each other's in-progress branches found real integration issues early but blurred PR boundaries; next time, publish a shared fixture and backend snapshot from the parent instead of letting workers merge siblings.
- A generated E2E fixture produced a false engine defect; fixture generators should be validated by indexing them once before tests rely on them.

## Follow-ups

- Type Briefing replacing Overview, and group views and the kit reading server stages and profiles: EFF-2026-10-03-18-33.
- Git commit times as change times for stale and recent-change signals.
- Suggest stages for Drew's vault enums that have no `@view` metadata, such as `IPInitiativeStatus`.
- CLI flags for the board lane and column fields.
- Commit-time recovery of pending repair journals writes files without refreshing the live note cache.
- `NotesLeftRail.tsx` and `NotesRightRail.tsx` remain over the file-size rule.

## Status

Complete. Drew decided on 2026-10-03 to keep the right rail open on table views and evaluate it in use, keep runtime fonts, and accept spec `active` as a done checkmark. Next work: EFF-2026-10-03-18-33.
