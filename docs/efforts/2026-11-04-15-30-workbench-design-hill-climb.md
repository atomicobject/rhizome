---
type: EffortNote
id: EFF-2026-11-04-15-30
aliases: [EFF-2026-11-04-15-30]
name: "Workbench design hill-climb"
created-at: "2026-11-04T20:35:33Z"
status: complete
plan-approved-by: "[[docs/people/Drew Colthorp|Drew Colthorp]]"
summary: "Retroactive closure of the 2026-11-04 workbench design loop: baseline plus six rounds, banked at a 7.80 three-reviewer mean with graph redesign and backend follow-ups."
governing-specs:
  - "[[docs/specs/experience/workbench-sidebar]]"
  - "[[docs/specs/experience/workbench-redesign]]"
  - "[[docs/specs/experience/notes-workspace-shell]]"
  - "[[docs/specs/experience/scope-overview]]"
  - "[[docs/specs/experience/search-workspace]]"
  - "[[docs/specs/experience/validation-experience]]"
  - "[[docs/specs/experience/schema-guided-note-editing]]"
  - "[[docs/specs/experience/trusted-html-note-viewer]]"
  - "[[docs/specs/technical/type-collection-views]]"
  - "[[docs/specs/technical/group-views-and-view-platform]]"
  - "[[docs/specs/technical/unified-view-contract]]"
  - "[[docs/specs/technical/design-system-parity]]"
---

# Workbench design hill-climb

## Scope

Record the completed web-workbench design hill-climb directed by [[docs/people/Drew Colthorp|Drew Colthorp]] on 2026-11-04. This effort was opened at closure, after implementation, because the work grew from an in-session request. No effort or frozen spec selection existed before the loop; the sections below identify and reconcile the delivered slice retrospectively.

The delivered code is `1f1d0c9b..d91cd828`, covering the shell, native collections, bundled group views, graphs and ontology, notes and search, and local design-loop tooling. Graph-screen redesign, backend counting/truncation fixes, the unexplained re-index, and refreshing the design sidecar are follow-up work. This record closes the banked design slice; it does not certify the original score target or PR merge readiness.

## Spec Set (Frozen)

Selected at closure against integrated delivery head `d91cd828` (baseline `1f1d0c9b`). These note-level links are the retrospective contract boundary, not evidence of a pre-implementation freeze. Their contradictory statements are reconciled in this branch; the same twelve notes appear in `governing-specs`.

- [[docs/specs/experience/workbench-sidebar]]
- [[docs/specs/experience/workbench-redesign]]
- [[docs/specs/experience/notes-workspace-shell]]
- [[docs/specs/experience/scope-overview]]
- [[docs/specs/experience/search-workspace]]
- [[docs/specs/experience/validation-experience]]
- [[docs/specs/experience/schema-guided-note-editing]]
- [[docs/specs/experience/trusted-html-note-viewer]]
- [[docs/specs/technical/type-collection-views]]
- [[docs/specs/technical/group-views-and-view-platform]]
- [[docs/specs/technical/unified-view-contract]]
- [[docs/specs/technical/design-system-parity]]

## Stories In Scope (Frozen)

No stories or acceptance criteria were selected before execution. At closure the delivered concerns map to these existing story targets; only the visual/navigation slice described here is included, not every obligation in each story:

- [[docs/specs/experience/workbench-sidebar#^SPEC-0128-US1|SPEC-0128 US1]], [[docs/specs/experience/workbench-sidebar#^SPEC-0128-US2|US2]], and [[docs/specs/experience/workbench-sidebar#^SPEC-0128-US3|US3]]: shared GraphQL sidebar and Views/Types subheadings.
- [[docs/specs/experience/workbench-redesign#^SPEC-0126-US3|SPEC-0126 US3]], [[docs/specs/experience/workbench-redesign#^SPEC-0126-US5|US5]], [[docs/specs/experience/workbench-redesign#^SPEC-0126-US7|US7]], [[docs/specs/experience/workbench-redesign#^SPEC-0126-US8|US8]], and [[docs/specs/experience/workbench-redesign#^SPEC-0126-US11|US11]]: header, pane, reading measures, board tracks, finder, and editing presentation.
- [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US1|SPEC-0090 US1]], [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US3|US3]], and [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US6|US6]]: one replaceable page beside retained notes and conditional tab-strip visibility.
- [[docs/specs/experience/scope-overview#^SPEC-0117-US1|SPEC-0117 US1]], [[docs/specs/experience/scope-overview#^SPEC-0117-US3|US3]], and [[docs/specs/experience/scope-overview#^SPEC-0117-US6|US6]]: compact maps, shared hues, Members below the map, and quieter side blocks.
- [[docs/specs/experience/search-workspace#^SPEC-0098-US1|SPEC-0098 US1]] and [[docs/specs/experience/search-workspace#^SPEC-0098-US4|US4]]: replacement-page search with retained note context.
- [[docs/specs/experience/validation-experience#^SPEC-0100-US2|SPEC-0100 US2]], [[docs/specs/experience/schema-guided-note-editing#^SPEC-0099-US6|SPEC-0099 US6]], and [[docs/specs/experience/trusted-html-note-viewer#^SPEC-0088-US1|SPEC-0088 US1]]: quiet validation, editing geometry, and compact HTML host controls.

The technical specs' selected obligations are their native view, bundled view, navigation, kit-style, and type-palette requirements. No new story IDs were invented.

## Spec Coverage Checklist

- [x] Shared shell, controls, sidebar, finder, and type palette: `AppShell`, `NotesLeftRail`, `FinderPalette`, `typePalette.ts`, host tokens, and kit styles implement the conventions in `DESIGN.md`; Desktop's GraphQL toggle gap is recorded below.
- [x] Collections and group views: configured headers, columns, Record, Board, Cards, Briefing, Overview, Trace, and Sections received the delivered presentation changes; spec statements now describe them.
- [x] Notes and search: note-only tab-strip visibility, a single replaceable page, compact properties/HTML controls, editing states, and bounded result excerpts are reconciled with their experience contracts.
- [x] Graph and ontology presentation: folder-level Explorer, shared type hues, map labels/legends, and Atlas semantic zoom are delivered; the separate redesign remains open.
- [x] Design-loop tooling: frozen-vault capture, readiness checks, fixed clock and randomness, comparisons, before/after pairs, ledger generation, and the eight-category rubric are checked in.
- [x] The score target shortfall is explicit in Actual Delivered and Deviations. This checklist accounts for the banked slice and does not mark whole governing specs satisfied.

## Plan

Retrospective account of Drew's in-session direction on 2026-11-04:

1. Set up a repeatable screenshot and council-review loop against a frozen vault, with 24 screens and eight categories, and run it as a goal.
2. Review the baseline, route concrete findings into implementer lanes, integrate their changes, and repeat. Reviewers were GPT-6.1 Sol at xhigh, GPT-6 Astra at high, and Claude Opus 5.5 at high. Implementers used Opus 5.5 at high in rounds 1–4 and GPT-6.1 Sol at high in rounds 5–6.
3. After six rounds and a plateau, bank the gains in a PR and follow up with a separate graph-screen redesign driven by mock-ups.
4. This closure pass records the actual outcome, reconciles changed spec statements and owning web guidance, adds the changelog entry, and runs the requested documentation validation and `make check-fast`.

The per-round briefs in Execution Notes are contemporary instructions. This account was written at closure and is not a newly invented historical plan revision.

## Plan Approval

[[docs/people/Drew Colthorp|Drew Colthorp]] approved the work through his in-session direction on 2026-11-04 to set up the loop, run it as a goal, and then bank the results in a PR. He also approved giving GraphQL the standard sidebar on that date. The closing task explicitly supplies this approval evidence and authorizes the retrospective effort and reconciliation. The record was authored at closure; the original chat messages' exact times and revision identifiers are unavailable, so no timestamped approval event is fabricated.

## Original Intended Delivery

Reach 9 or more from every reviewer in every category, both for the app overall and for every screen. The council evaluates typography, hierarchy, spacing and density, colour and contrast, iconography, consistency and polish, clarity, and overall quality.

## Actual Delivered

- **Shell and system conventions:** shared headers, quieter buttons and banners, one disclosure chevron, Inter counts and readable dates, teal record titles with quieter secondary links, distinct green success and gold staged edits, clearer Finder rows, and calmer Agent and Tasks screens. GraphQL now uses the standard sidebar, an inset flat editor, session-tab underlines, visible query actions, and token-based syntax colours.
- **Collections and group views:** clearer headers and display controls, bounded supporting columns with room for titles, a Record overlay below the collection toolbar, aligned 260–360px Board tracks and 140px empty/collapsed strips, quieter cards, and in-place overflow. Briefing, Overview, Trace, and Sections share hierarchy, status, count, and link conventions; Briefing/Overview use centred measures, and Members sits below the compact Overview map.
- **Graphs and ontology:** one twelve-hue concrete-type palette across the host and kit, folder-level Explorer with simpler labels and legends, clearer Overview links and labels, and more legible Atlas cards, connector routes, and semantic zoom. Ontology detail uses human labels and shared presentation tabs. These are incremental improvements; the graph surfaces still need redesign.
- **Notes and search:** notes retain their tabs while one collection, view, Problems, Changes, or search page replaces the previous page. The tab strip appears only while notes are open. Notes use a centred reading measure, quieter properties and inspectors, shared empty-field disclosures in Edit, and a compact HTML host header. Search uses a shared centred header/results measure, one availability banner, and bounded source excerpts.
- **Tooling:** `web/scripts/design-loop/` contains snapshot setup, screenshot capture, comparisons, paired before/after output, ledger generation, and the council rubric. Round outputs remain Git-ignored; their evidence is summarized here.

The three-reviewer app-overall means were:

| Round | Sol | Astra | Opus | Mean |
| --- | --- | --- | --- | --- |
| 0, main `1f1d0c9b` | 6.80 | 6.70 | 6.30 | 6.60 |
| 1 | 7.40 | 7.10 | 6.50 | 7.00 |
| 2 | 7.80 | 7.50 | 6.90 | 7.40 |
| 3 | 7.60 | 7.60 | 6.90 | 7.37 |
| 4 | 7.80 | 8.10 | 7.10 | 7.67 |
| 5 | 8.00 | 8.00 | 7.30 | 7.77 |
| 6 | 8.10 | 8.00 | 7.30 | 7.80 |

The mean gained 1.20 points, with only 0.03 in the final round. No category reached the 9 target; round 6 records 0 of 576 per-screen category cells at 9 or above (24 screens × eight categories × three reviewers). Graphs remained weakest: group Overview 7.23, Explorer 7.27, All notes Overview 7.30, and Atlas 7.47. Drew chose to bank these gains and pursue the graph redesign separately.

Round 0's ledger also includes a Fable vote and reports a four-vote 6.72 mean, with two Sol runs averaged into one vote. The consistent Sol/Astra/Opus comparison above uses the three-vote 6.60 baseline, as the round-1 ledger does. These are screenshot judgments, not a usability-study result.

## Execution Notes

- 2026-11-04T20:35:33Z: Opened this effort retroactively at closure. Read the closure/setup/reconciliation rules, release/documentation policy, `DESIGN.md`, `git log --oneline 1f1d0c9b..HEAD`, and `git diff --stat 1f1d0c9b..HEAD`, then checked affected code and specs. All design-round history in this note is a retrospective summary of the supplied evidence.
- Evidence location: `/Users/colthorp/.t3/worktrees/rhizome/t3-a28051b5/web/scripts/design-loop/rounds/round-0/LEDGER.md` through `round-6/LEDGER.md`, and `round-1/LANE-BRIEF.md` through `round-6/LANE-BRIEF.md`. These ignored local files are not committed release evidence; the score table and delivered-scope summary above preserve their relevant results here.
- The later lane briefs retain Opus co-author wording after the actual implementer switch. The owner's supplied execution account establishes Sol high for rounds 5–6; the stale brief footer is not model-execution evidence.
- Closure build: `make build` first failed downloading team-api Go dependencies in the network-restricted sandbox, then passed outside it. Restored `pkg/app/web/assets/dist/index.html` after building.
- Rhizome discovery initially timed out; a restricted run reported `read kern.boottime: operation not permitted`, and the auto-started local runtime repeatedly ran embedding jobs. Stopped only this checkout's runtime and completed typed retrieval in process with `RZM_RUNTIME_AUTOSTART=0`. Used the built binary with `RZM_SKIP_REPO_DELEGATE=1` throughout.
- `make check-fast` passed (exit 0). Rhizome code mode ran `validate default` with `scopeNote` on all sixteen changed notes: ontology, identifiers, and broken-links all passed with zero findings. The closure-drift recipe resolved the EffortNote and its twelve frozen specs.
- 2026-11-04T20:37:37Z: The first `frozen-scope-drift` pass found two issues introduced by these spec date bumps, on EFF-2026-10-03-07-21 / SPEC-0100 and EFF-2026-10-03-18-33 / SPEC-0111. Added dated follow-on acknowledgments to those active efforts' Deviations, preserving their frozen sections, plans, lifecycle states, and delivery histories.

- 2026-11-04T20:39:06Z: `RZM_SKIP_REPO_DELEGATE=1 RZM_RUNTIME_AUTOSTART=0 ./bin/darwin/rzm agent code execute --session-id ZEMqt1wnGO_uNA-Y --timeout 2m` reran scoped default validation on the repaired notes and `frozen-scope-drift`: zero findings. The `action-items-in-note` recipe returned all eight open follow-ups assigned to Drew Colthorp with complete root coverage. Its readback also reproduced the known 80-byte title cut; shortened checkbox titles retain the full task details on continuation lines. Marked the banked slice complete with PR identity pending.

## Deviations

1. **Opened retroactively.** The effort and frozen links were created at closure, not before the work. Drew explicitly requested this honest retrospective record; no fictional execution timeline or prior freeze is asserted.
2. **Score target not reached.** The original universal 9 target is preserved. Drew directed banking the work at 7.80 after the sixth round and pursuing graphs separately.
3. **Implementer switch.** Implementers changed from Opus 5.5 high to GPT-6.1 Sol high in round 5, per the owner's execution account.
4. **GraphQL sidebar decision reversed.** On 2026-11-04 Drew approved the standard sidebar, reversing SPEC-0128's 2026-10-26 exclusion. The browser control works; Desktop still disables its toolbar and View-menu toggle on GraphQL (`desktop/src/App.tsx` and `desktop/src-tauri/src/page.rs`). That remaining integration gap is recorded as follow-up rather than described as delivered.
5. **Banked slice, with PR gates carried forward.** This task closes the completed design iteration with recorded limitations and the requested local documentation checks. Full integrated tests, independent final code review, Greptile, CI, and PR identity remain with the integrating PR owner; no unrun gate is claimed as passed. The backend issues and stale sidecar below are unresolved.

## Closure Checklist

- [x] Required closure checks passed: `make build`, Rhizome default validation (ontology, identifiers, broken-links) on changed notes, `frozen-scope-drift`, and `make check-fast`. The two introduced drift findings were resolved by follow-on acknowledgments.
- [x] Alignment and actual outcomes checked against integrated code, DESIGN.md, the seven score ledgers, and six lane briefs; target shortfall and Desktop gap are explicit.
- [x] Contradicted spec statements and owning web guidance reconciled; unchanged backend contracts remain intact.
- [x] Follow-ups triaged and assigned below, including the optional calibration.
- [x] PR handoff recorded: pending PR number, to be added once the integrating PR exists and before merge. Design delivery is `1f1d0c9b..d91cd828`; closure documentation travels with that work, not in a closure-only PR.
- [x] Full integrated review/test/CI gates remain with the PR owner as disclosed in Deviations; this effort does not assert merge readiness.

## Compounding Follow-ups

The rubric, fixed-vault capture, and before/after comparisons are reusable. The loop plateau suggests using mock-ups for the graph redesign, as Drew directed. Reference-app screenshot calibration is an optional experiment, not a completed evaluation. Refreshing the stale `.impeccable/design.json` should happen against the current DESIGN.md rather than treating the sidecar as authority.

## Follow-ups

- [ ] Open a graph redesign effort for Explorer, Overview maps, and Atlas. #action-item
  Base the redesign on mock-ups.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Fix the backend action-item embedded-title 80-byte cut. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Resolve SPEC-0117's 176 versus 177 record count. #action-item
  Investigate ambiguous-note counting under [[docs/specs/experience/scope-overview|SPEC-0117]].
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Fix Overview's 20-record outside-note read cap. #action-item
  Group records are being listed as outside notes.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Investigate the unexplained full re-index of unchanged files. #action-item
  Thousands of files were reported modified without changes.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Refresh the stale `.impeccable/design.json` sidecar from DESIGN.md. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Optionally calibrate council scores with reference-app screenshots. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Enable Desktop's GraphQL sidebar toggle in its toolbar and View menu. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]

## Status

Complete. Opened retroactively at closure and closed for the banked design slice. The original 9 target is unmet and covered by Drew's decision to bank the gains. PR number: pending, to be added by the integrating owner once the PR exists. The PR must carry this closure record and complete its review and CI gates before merge.
