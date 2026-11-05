---
type: EffortNote
id: EFF-2026-11-05-17-11
aliases: [EFF-2026-11-05-17-11]
name: "Parallel design hill climb"
created-at: "2026-11-05T22:11:38Z"
status: complete
plan-approved-by: "[[docs/people/Drew Colthorp|Drew Colthorp]]"
summary: "Retroactive closure of the 2026-11-04/05 divide-and-conquer design push: three slice leads (Collections, Notes and tools, Graphs), a shared integration pass, a rebuilt whole-vault Explorer, and full Action Item titles, closed after a saturated 7.83 council."
governing-specs:
  - "[[docs/specs/experience/workbench-redesign]]"
  - "[[docs/specs/technical/design-system-parity]]"
  - "[[docs/specs/technical/group-views-and-view-platform]]"
  - "[[docs/specs/technical/type-collection-views]]"
  - "[[docs/specs/experience/scope-overview]]"
  - "[[docs/specs/experience/search-workspace]]"
  - "[[docs/specs/experience/schema-guided-note-editing]]"
---

# Parallel design hill climb

## Scope

Record the parallel design hill climb that [[docs/people/Drew Colthorp|Drew Colthorp]] directed on 2026-11-04 and 2026-11-05, after [[docs/efforts/2026-11-04-15-30-workbench-design-hill-climb|EFF-2026-11-04-15-30]] closed in [PR #91](https://github.com/atomicobject/rhizome/pull/91). That effort is a historical record, and this push is new effort-driven work, so it gets its own effort. No effort was opened before the work. Like its predecessor, this one was opened at closure and identifies the delivered slice retrospectively.

The delivered change is `7e576548..4801ad49` on `t3/parallel-hill-climb-1`: 143 commits and 264 files, almost all under `web/`. The backend part is small: an Action Item display label in `pkg/ontology` and `pkg/embedindex`, with catalog materialization version 17. Atlas (the ontology view) was deliberately left out. This record closes the banked design slice. It does not certify the score target or PR merge readiness.

## Spec Set (Frozen)

Selected at closure against delivery head `4801ad49` (base `7e576548`). These note-level links are the retrospective contract boundary, not evidence of a freeze before implementation. The same seven notes appear in `governing-specs`.

- [[docs/specs/experience/workbench-redesign]]
- [[docs/specs/technical/design-system-parity]]
- [[docs/specs/technical/group-views-and-view-platform]]
- [[docs/specs/technical/type-collection-views]]
- [[docs/specs/experience/scope-overview]]
- [[docs/specs/experience/search-workspace]]
- [[docs/specs/experience/schema-guided-note-editing]]

No spec owns Explorer's behaviour: aggregation, expanding in place, layout stability, or code and note links. The delivered Explorer is described under Actual Delivered, and an owning contract is a follow-up.

## Stories In Scope (Frozen)

No stories or acceptance criteria were selected before execution. At closure, the delivered concerns map to these existing story targets. Only the visual and navigation slice described here is included, not every obligation in each story:

- [[docs/specs/experience/workbench-redesign#^SPEC-0126-US3|SPEC-0126 US3]], [[docs/specs/experience/workbench-redesign#^SPEC-0126-US7|US7]], and [[docs/specs/experience/workbench-redesign#^SPEC-0126-US11|US11]]: collection headers, wide-screen composition, and note reading and editing presentation.
- [[docs/specs/experience/scope-overview#^SPEC-0117-US1|SPEC-0117 US1]] and [[docs/specs/experience/scope-overview#^SPEC-0117-US3|US3]]: group and All notes Overview maps, legends, and link coverage.
- [[docs/specs/experience/search-workspace#^SPEC-0098-US1|SPEC-0098 US1]] and [[docs/specs/experience/search-workspace#^SPEC-0098-US2|US2]]: the search results column and its excerpts.
- [[docs/specs/experience/schema-guided-note-editing#^SPEC-0099-US4|SPEC-0099 US4]] and [[docs/specs/experience/schema-guided-note-editing#^SPEC-0099-US6|US6]]: Discard confirmation and the editing layout.

The technical specs' selected obligations are their type palette, status mark, Board and Cards layout, and bundled Briefing, Trace, and Sections presentation requirements. No new story IDs were invented.

## Spec Coverage Checklist

- [x] Type colours: one token per type across the sidebar, Finder, Atlas, ontology pages, chips, Explorer, and Overview legends, through `web/src/lib/typePalette.ts`, which the kit also uses. SPEC-0125 and SPEC-0126 now say that an interface shares its leading implementor's token.
- [x] Status marks: each status word keeps one mark everywhere (`235fb766`, with Relation chips and Record reverse links in `9d9f1660`). SPEC-0111's lifecycle-mark rule now says so.
- [x] Collections and group views: Board, Cards, Record, Action Items, Briefing, Trace, and Sections received Lead A's and the integration presentation changes; DESIGN.md records the conventions.
- [x] Notes, search, and tools: the note reading and editing presentation, Search at 960px with 720px excerpts (already the SPEC-0098 contract), and Discard confirming drafts as SPEC-0099 US4 requires.
- [x] Overview maps: legends, labels, and link coverage. "106 not linked" sits beside the Link coverage heading.
- [x] Explorer: rebuilt within the existing folder-containment concept. No spec owns it; see Follow-ups.
- [x] The score target shortfall is explicit in Actual Delivered and Deviations. This checklist accounts for the banked slice and does not mark whole governing specs satisfied.

## Plan

Retrospective account of Drew's in-session direction, which he approved on 2026-11-04:

1. Divide the whole-app loop, which plateaued at 7.93 after round 7, into three slices with their own hill climbs, each run by an Opus 5.5 lead: A Collections, B Notes and tools, C Graphs. An integrator owns the shared design system (tokens, base styles, kit, shell, DESIGN.md) and merges the leads.
2. In each slice, GPT-6.1 Sol implements, and Sol, GPT-6 Astra, and Opus 5.5 review fresh each round against the shared rubric. The goal is 9 or more from every reviewer in every category on every screen. A lead stops at that target, at a plateau (two rounds rising less than 0.15), or after five rounds.
3. Graphs follow Drew's revised direction. Keep the folder-containment Explorer and make it smarter: aggregate and never truncate, keep the layout stable when a folder expands, make code first-class, and use the vivid palette. Atlas stays untouched.
4. Integrate the shared items the leads hand over, run a whole-app council, fix its findings, then review code quality with sol-loop, pass local gates, and open a PR.

The shared lead brief (`LEAD-BRIEF.md`), the three slice tasks (`task-a.md`, `task-b.md`, `task-c.md`), and `INTEGRATION-TODO.md` in `web/scripts/design-loop/rounds/parallel-1/` are the contemporary instructions. That folder is ignored by Git. This account was written at closure and is not a new historical plan revision.

## Plan Approval

[[docs/people/Drew Colthorp|Drew Colthorp]] approved the divide-and-conquer plan in-session on 2026-11-04, including the revised Graphs direction. On 2026-11-05 he approved each lead's judge picks and decided the integration questions listed under Execution Notes. The closing task supplies this approval evidence and authorizes the retrospective effort. The original messages' exact times are unavailable, so no timestamped approval event is fabricated.

## Original Intended Delivery

Every reviewer scores 9 or more in every rubric category on every screen of every slice, and the integrated app keeps those gains in a whole-app council. The rubric's categories are typography, hierarchy, spacing and density, colour and contrast, iconography, consistency and polish, clarity, and overall quality.

## Actual Delivered

- **Explorer (Lead C):** the folder-containment root graph now covers the whole vault. It counts all 655 notes and 4,572 code files and aggregates instead of truncating. Folders expand in place with a stable layout that follows containment. Code is first-class, with its own colour and code and note links drawn, and the vivid type palette is retained. Capture states show an expanded folder, a deep folder after two expansions, and a focused file.
- **Overview maps (Lead C and integration):** clearer legends, labels, rings, and member tables on the group and All notes Overviews. "106 not linked" sits beside Link coverage.
- **Collections (Lead A):** Board density is one Display setting; cards keep a fixed line budget; empty values show "—" everywhere; the Trace header is a two-line band; status-strip counts appear only where the header has none; the collection, Briefing, Trace, and Sections layouts are refined.
- **Notes and tools (Lead B):** a calmer note reader and editor, Search, Finder, Agent, Tasks, and Problems.
- **Shared system (integration):** one colour per type; one mark per status word; dark-mode panel and card edges; the Finder scrim; edit-mode dates that show words at rest and the ISO field only while focused; ages in hours up to 23h, then days; 2px inline-code padding; 16px lucide arrows in rails and legends; and DESIGN.md conventions for these. Search is capped at 960px with 720px excerpts.
- **Behaviour fixes Drew decided on 2026-11-05:** Discard confirms before dropping local drafts as well as staged edits. Action Items show the full title through a new catalog `display_label` (`pkg/ontology/display_title.go`), and `firstLineTitle` and `ITEM_SUMMARY`, the embedding text, are unchanged, so nothing is re-embedded. The three tests that failed only in shuffled order are fixed.

Slice results, as the mean across the slice's screens of Sol, Astra, and Opus scores:

| Slice | Baseline | Final | Rounds | Stop |
| --- | --- | --- | --- | --- |
| A Collections | 7.64 (a-0) | 8.04 (a-4) | 4 | Plateau rule and round limit |
| B Notes and tools | 8.20 (b-0) | 8.23 (b-2), peak 8.33 at b-1 | 2 | Plateau |
| C Graphs | 5.73 (c-0) | 7.37 (c-5) | 5 | Round limit |

Lead C's root Explorer screen rose from 7.00 to 7.40.

The whole-app council after integration:

| Round | Sol | Astra | Opus | Mean |
| --- | --- | --- | --- | --- |
| Round 7 (PR #91) | 8.20 | 8.00 | 7.60 | 7.93 |
| int-1 | 8.20 | 8.10 | 7.00 | 7.77 |
| int-2 | 8.10 | 8.10 | 7.30 | 7.83 |

A Fable 5.1 plateau judge found the council saturated. Most int-2 consensus findings were regressions from int-1 fixes or reversals of them, because fresh reviewers never see earlier ledgers. One unscored regression-repair round followed (int-3), and int-4 carried out Drew's decisions. These are screenshot judgments, not a usability-study result.

## Execution Notes

- 2026-11-05T22:11:38Z: Opened this effort retroactively at closure. Read the closure and setup rules, release policy, the closed EFF-2026-11-04-15-30 record, the lead brief, the three tasks, `INTEGRATION-TODO.md`, and the round ledgers, then checked the governing specs against `git diff 7e576548`. All the history in this note is a retrospective summary of the supplied evidence and those ignored local files.
- Evidence locations, all under `web/scripts/design-loop/rounds/` and ignored by Git: `a-0`…`a-4` and `a-final`; `b-0`…`b-2` and `b-final`; `lead-c/c-0`…`lead-c/c-5` and `lead-c/final`. Each scored lead round has `LEDGER.md`, `DECLINED.md`, and `scores/`. Integration: `int-1` (with `DECLINED.md` and `WORKER-COMMON.md`), `int-2` (`LEDGER.md` and `scores/`; it has no `DECLINED.md`), and the `int-2-*`, `int-3-*`, and `int-4-*` worker folders, some with `REPORT.md` or `DECLINED.md`. These are not committed release evidence; the tables above keep their results.
- Drew's decisions on 2026-11-05: one colour per type; Search capped narrower, at 960px; and "whatever is recommended is fine", which covered Discard confirming before dropping drafts, full Action Item titles through `display_label` under his never-re-embed rule, and fixing the shuffle-order tests. He left "106 not linked" to the integrator, who placed it beside Link coverage. He had already approved the leads' judge picks: cards keep a fixed line budget, the Board gets one Display setting for card density, and empty values show "—".
- Code review: sol-loop, two rounds with GPT-6.1 Sol at high effort, verdict approved. Its one `should` finding was that Relation chips and Record reverse links bypassed the status-word mapping. That was fixed in `9d9f1660`, with a regression test.
- Local gates, as reported by the integrator: `make check` passed (Go lint, vet, 142 Go packages, web lint, both typechecks, and 1,911 web unit tests). The full Playwright suite had three failures in `app.spec.ts`, all stale tests. They were fixed in `802a16f3`, and a rerun passed 121 of 121. One later commit, `4801ad49` (staged cards marked with a dot), landed after those runs. The integrator reruns both suites before the PR, and the Greptile CLI runs after the PR opens.
- Reconciled contradicted spec statements on this branch: SPEC-0126 and SPEC-0125 said interfaces and code use the neutral type slot. They now say that an interface shares the token of its implementor with the most records and that Explorer gives code its own colour. SPEC-0111 said lifecycle marks always encode position and tone. It now says that a recognized status word keeps one mark everywhere. The SPEC-0098 960px cap and the SPEC-0099 US4 discard decision already matched the delivery.
- Completed two of EFF-2026-11-04-15-30's follow-ups that this branch delivers, by checking each box and adding one line that cites this effort, with no other change to the closed record: the graph redesign (delivered here under Drew's revised direction, which keeps Explorer's concept and leaves Atlas as it is) and the action-item title cut (resolved by `display_label`; Drew decided to keep the parser title and embedding text capped). Its other follow-ups remain open. Of its six oversized web files, only `web/src/lib/ontologyGraph.ts` (931 lines) is still over 500.
- 2026-11-05: `RZM_RUNTIME_AUTOSTART=0 RZM_SKIP_REPO_DELEGATE=1 ./bin/darwin/rzm agent code execute --session-id y7Qc57ecjG_4o5G1` ran `validate default` scoped to each changed file: this note, EFF-2026-11-04-15-30, SPEC-0111, SPEC-0125, SPEC-0126, and `CHANGELOG.md`. Ontology, identifiers, and broken-links all passed with zero findings. Repository-wide `frozen-scope-drift` found nothing. `closure-drift-pack` resolved this EffortNote, its approver, and its seven frozen specs. `action-items-in-note` read back nine open follow-ups here and the two completed items in EFF-2026-11-04-15-30, all assigned to a resolved `Person`.

## Deviations

1. **Opened retroactively.** The effort and frozen links were created at closure, not before the work. No fictional timeline or earlier freeze is asserted.
2. **Score target not reached.** No slice reached 9 in every category, and the integrated council ended at 7.83, below round 7's 7.93. The Fable judge found the council saturated: fresh reviewers reversed earlier fixes. The work was banked after one regression-repair round.
3. **"Pilot active" pill unchanged.** `INTEGRATION-TODO.md` planned to move the red "Pilot active" pill to the shared Active mark. The pill is authored content in a closed effort note, so it was left unchanged.
4. **Catalog reindex required.** Catalog materialization version 17 refreshes Action Item display labels, so each repository rematerializes its catalog once after upgrading. Embedding text is unchanged, and nothing is re-embedded.
5. **Gates predate the last commit.** The recorded `make check` and Playwright runs predate `4801ad49`. The integrator reruns them before the PR.

## Closure Checklist

- [x] Rhizome default validation (ontology, identifiers, broken-links) passed with zero findings on the six changed files, and `frozen-scope-drift` found none.
- [x] Alignment checked against `git diff 7e576548`, DESIGN.md, the lead and integration ledgers, and the lead brief and tasks. The target shortfall and the gates' timing are explicit.
- [x] Contradicted statements in SPEC-0111, SPEC-0125, and SPEC-0126 reconciled on this branch.
- [x] The closed effort's delivered follow-ups completed, and new follow-ups triaged and assigned below.
- [x] Changelog line added under `## [Unreleased]`, including the catalog refresh.
- [ ] PR handoff: the PR number is pending. Add it here and in Status once the PR exists.
- [x] Local gates and sol-loop review passed on the branch (Execution Notes). The integrator reruns gates before the PR; Greptile and CI run on the PR.

## Compounding Follow-ups

Parallel slice leads worked: each slice climbed on its own, and Lead C moved the weakest screens most. The integrated council then saturated, because reviewers who never see earlier ledgers reverse fixes that other reviewers asked for. The Fable judge's lesson for the loop: give reviewers the earlier ledger, or require three votes to reverse a fix.

## Follow-ups

- [ ] Split `web/src/ontology.css` (5,361 lines) by surface. #action-item
  It holds rules for other surfaces, such as the note edit bar (`.ontology-edit-bar`). Fold the `note-reader.css` edit-bar override and the Notes Info toggle rule back into their owning rules.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Refresh the stale `.impeccable/design.json` sidecar with `impeccable document`. #action-item
  This carries forward the still-open sidecar item in EFF-2026-11-04-15-30.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Reconcile the top bar's "346 typed notes" with the 655 All notes count. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Decide whether Explorer collapses context around an opened folder. #action-item
  Lead C declined it because it conflicts with the rule that outside nodes never move. Drew's call.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Re-measure Explorer's folder-click redraw time. #action-item
  It measured 193 ms at c-5, against 76–79 ms at c-3 and c-4.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Check whether `TestGeneratedClientBoundsUnresponsiveServer` is flaky. #action-item
  It failed once in a worker's `make check` and passed alone.
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Reinstall Rhizome Dev from `main` after merge with `make desktop-install-dev`. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Give design-loop reviewers the earlier ledger, or require three votes to reverse a fix. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]
- [ ] Give Explorer an owning spec for aggregation, in-place expansion, layout stability, and code and note links. #action-item
  assignee:: [[docs/people/Drew Colthorp|Drew Colthorp]]

## Status

Complete. Opened retroactively at closure and closed for the banked design slice. The 9 target is unmet; the council saturated, and the work was banked. The PR number is pending; add it once the PR exists.
