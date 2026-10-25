---
type: EffortNote
id: EFF-2026-10-25-08-29
aliases: [EFF-2026-10-25-08-29]
name: Design system parity
created-at: 2026-10-25T12:29:51Z
status: complete
summary: "Fix the 2026-10-25 Impeccable audit findings: shared light and dark tokens, kit defaults and primitives that match DESIGN.md, host contrast and theming, calmer toolbars and designed loading states, accessibility, render performance, and Drew's custom views."
governing-specs: ["[[design-system-parity]]"]
---

# Design system parity

## Scope

Implement [[design-system-parity|SPEC-0125]], which records the findings of the 2026-10-25 Impeccable audit of the web UI (Notes workspace, bundled and custom views, Ontology, Explorer, Agent, GraphQL) and Drew's follow-up feedback on the collection toolbar, kit loading placeholders, and "Reset to shared". Drew's personal views in `drews-vault/.rhizome/views/` are rebuilt on the new kit as part of this effort but stay outside the product contract.

Excluded: phone and touch layouts, an in-app appearance override, identity work, view/preference/API contract changes, server-side Briefing aggregation, custom-view frame keep-alive, an ELK worker, and lucide splitting (SPEC-0125 Non-Goals).

## Spec Set (Frozen)

- [[design-system-parity|SPEC-0125]], 2026-10-25 revision, all requirements.

## Stories In Scope (Frozen)

Requirements-only scope; all requirements in the governing spec are included.

## Spec Coverage Checklist

- [x] Tokens and themes, including dark mode, palettes, and self-hosted fonts (#38, #44).
- [x] Color and type rules across host surfaces (#44).
- [x] Kit defaults, new primitives, `NoteLink`, `StatusMark`, `Diagram` width (#38; chart-only width recorded as a deviation).
- [x] Loading and empty states (#38).
- [x] Host chrome: reset button, collection toolbar, issue labels, edit mode, Atlas and Type Detail (#38, #42, #44).
- [x] Accessibility requirements (#42).
- [x] Performance requirements (#45).
- [x] Author guidance and regenerated skill copies (#38).
- [x] Drew's custom views rebuilt on the kit (applied to `drews-vault/.rhizome/views/` on Drew's instruction).

## Plan

Delivery: a parent branch `feat/design-system-parity` with one parent PR to `main`. Each phase is a child PR into the parent, squash-merged so the parent carries one commit per phase. Drew merges the parent PR.

1. **Foundation: tokens and kit (child PR 1).** Add dark values, filled primary, missing semantic tokens, `--type-*` and `--series-*` palettes, and remove undefined tokens and the kit `--muted` collision in `base.css`; make the kit build carry every theme block. Retune `kit/theme.css`; restyle `@rhizome/ui` (`Button`, `Badge`, `Card`, `Tabs`, `Table`, `Skeleton`, `Input`, `Select`, focus); style `NoteLink`; give `StatusMark` the host shapes; make `Diagram` fill its width. Promote `Section`, `Facts`, `Count`, `Dash`, `MoreToggle`, `Segmented`, and `BarList` into `@rhizome/kit` with delayed neutral loading placeholders, and move the bundled group views onto them, deleting the duplicated `group.css` rules. Show "Reset to shared" only with overrides, in the selector row. Rewrite the custom-views skill guidance and examples, then `make build`, `rzm init`, `rzm init --check`. **Foundation review pause:** this phase fixes public `/kit/v1` exports and token names that every later phase uses; run `foundation-review` before phases 2–5 start.
2. **Host theme sweep and dark mode (child PR 2).** Apply the tokens across host CSS: Atlas and Type Detail rewrite, `ontology.css` literals and fallbacks, Explorer and typed-section links, red side stripes and red hovers, uppercase eyebrows, Merriweather limits, the type scale, syntax-highlight roles, graph palettes on `--type-*`, GraphiQL theme, desktop shell styles. Self-host fonts with generated notices. Verify every route in light and dark.
3. **Notes workspace UX and accessibility (child PR 3).** Collection toolbar with "View options" disclosure and the Issues facet absorbing "Problems on this page"; distinct issue-count labels; edit-mode disabled primary, growing textareas, and "Empty" wording; Agent empty state; every accessibility requirement in SPEC-0125.
4. **Performance (child PR 4).** Memo boundaries and stable props below `NotesShell`, memoized Markdown and hoisted heading components, debounced search scroll commits, graph layout off the main thread or Barnes-Hut above 1,000 nodes with incremental updates, precomputed staged-edit lookups and memoized table rows, lazy local graph and highlighting, stable validation-summary keys. Record before/after measurements.
5. **Drew's custom views (vault).** Rebuild IP Desk, Postcard prep, Home base, and Quarter slate on the kit primitives: no duplicate titles, facts strips instead of stat cards, hairline rows, one-line empty states, series tokens, `useViewPreference` instead of `localStorage`, and no per-view dark overrides. Verify each in light and dark against a copied vault.
6. **Integration and closure.** On the parent branch: `make verify`, desktop checks, light/dark screenshots at 1280 and 1440, contrast computations, the Impeccable detector, a full-diff self-review, and `greptile review --json` until 5/5. Close the effort in the parent PR and hand it to Drew to merge.

Phases 2, 3, and 4 run in parallel after the foundation review, each by a delegated Opus 5.5 High worker in its own worktree branched from the parent, with file ownership split so phase 2 owns stylesheet sweeps and Type Detail, phase 3 owns toolbar, rail, and component behavior, and phase 4 owns hooks and data paths. Each worker runs `make check`, `make web-e2e` on a unique port, and commits after every deliverable; I review each diff before squash-merging it into the parent. Phase 5 runs alongside them.

Authority requested with this plan: create and push the parent and child branches, open the parent and child PRs, squash-merge child PRs into the parent, and edit the view files in `drews-vault/.rhizome/views/`. Merging to `main` stays with Drew.

## Original Intended Delivery

One parent PR that makes custom views look like the bundled views by default, gives host and kit a matching dark theme, clears the audit's P1 and P2 findings, and leaves Drew's four custom views rebuilt on the new kit.

## Actual Delivered

Delivered on `feat/design-system-parity` for parent [PR #37](https://github.com/atomicobject/rhizome/pull/37), one squashed commit per phase:

- Phase 1, [#38](https://github.com/atomicobject/rhizome/pull/38) (`e406c460`): `web/src/tokens.css` with light and dark values, filled primary, focus ring, type, series, and syntax palettes; `@rhizome/ui` restyled to DESIGN.md under the same names; kit primitives `Section`, `SectionBody`, `sectionStatus`, `Facts`, `Fact`, `Count`, `Dash`, `MoreToggle`, `Segmented`, `BarList`, `NodeLink`; styled `NoteLink`; shape-based `StatusMark`; chart width; bundled group views on the primitives; "Reset to shared" only with overrides, beside the selector, backed by the additive `familyOverrides` field; custom-views skill "Look like Rhizome".
- Phase 3, [#42](https://github.com/atomicobject/rhizome/pull/42) (`ce26f7e1`): collection toolbar with "View options", distinct issue counts, labeled header count, growing text fields, compact Agent empty state, and the accessibility requirements.
- Phase 2, [#44](https://github.com/atomicobject/rhizome/pull/44) (`10ee675f`): host and desktop on the tokens with dark mode, Atlas and Type Detail rewrite, one type palette with runtime-resolved canvas colors, red and typography rules, about 1,000 lines of dead CSS removed, token-based syntax and GraphiQL themes, self-hosted fonts with OFL notices.
- Phase 4, [#45](https://github.com/atomicobject/rhizome/pull/45) (`a9f7f082`): memo boundaries below the shell (inactive tab renders and parses 4/10 → 0/0), debounced search-scroll commits, worker graph layout (581 → 9 ms main-thread block at 2,000 nodes), memoized table rows and batched staged lookups, lazy rail graph and highlighting (entry 1,308 → 932 kB), per-batch validation summaries, one Overview members read.
- Integration: changelog (`e87e11b9`) and Greptile review fixes (`eaa50a08`, `f498be21`).
- Phase 5: IP Desk, Postcard prep, Home base, and Quarter slate rebuilt on the kit and copied into Drew's vault on 2026-10-25 at his instruction; they load once Rhizome Dev runs a build with this branch.

## Execution Notes

- 2026-10-25: Audit evidence (screenshots, detector output, four review reports) gathered in the planning session; the shell's Impeccable update landed first as `1f17987b` and `c593d2f2` on this branch.
- 2026-10-25T12:33:57Z: Drew approved this plan revision and its frozen scope in chat ("Yes"), including the branch, PR, squash-merge, and vault-view authority listed under Plan. No current Person is configured in this checkout, so `plan-approved-by` is omitted; the chat approval stands.
- 2026-10-25 foundation review after phase 1 (PR #38), decided by Drew: restyle `@rhizome/ui` defaults in place under `/kit/v1` (visual defaults may change within a kit version; names and props stay); lock the public token names (`--type-1..8` with `-ink`, `--series-1..8`, `--series-link`, `--focus-ring`, `--accent-fill`, and the `text-faint`, `text-link`, `text-risk`, `bg-sunk`, `bg-tint` utilities); keep the additive `familyOverrides` preference field. Phases 2–5 may start.
- Phase 1 evidence: `make check` passed; `make web-e2e` first run 92 passed, 3 failed (Matrix toggle now a radio, Diagram re-rendering flowcharts on resize, the note toolbar's reset overlapping Copy link); fixed, and the three specs rerun pass (19 passed). `rzm init --check` exit 0; `rzm validate` 0 issues.
- Phase 5 (2026-10-25): rebuilt IP Desk, Postcard prep, Home base, and Quarter slate on the kit primitives in a copy of the vault and verified them in light and dark against this branch's build. Staged at `/tmp/rzm-phase5-views/` and applied to `drews-vault/.rhizome/views/` only once the parent PR is ready, because Drew's installed Rhizome Dev predates the new kit exports and would fail to load the rebuilt views. Postcard prep's per-view dark stylesheet is deleted. Home base keeps its bucket colors, which deliberately match the leadership site, rather than the series palette.
- Phase 1 follow-up fixes found while rebuilding the views: chart width now survives xyChart frontmatter (init directive), and kit component styles sit in `@layer components` so view utilities override them.
- Integration gates (2026-10-25): each phase was rebased onto the parent and tested as combined trees before merging. Phase 2+3: `make check` exit 0; `RHIZOME_E2E_PORT=7871 npm run test:e2e` 102 passed. Phase 2+3+4: `make check` exit 0; `RHIZOME_E2E_PORT=7872 npm run test:e2e` 102 passed. Rebased trees were identical to the tested trees. Child PR CI: #38 20 passed/1 skipped, #42 20 passed/1 skipped, #44 21 passed, #45 web jobs passed (Go jobs skipped for web-only changes). Review fixes: `make check` exit 0; web unit tests 1,618 passed.
- Greptile (`greptile review --json`): the whole branch is too large for one review, so each squashed phase was reviewed against its parent: phase 1 3/5, phase 2 4/5 (its one finding, missing fonts, was a false positive from excluded binary files), phase 3 4/5, phase 4 4/5. All seven real findings were fixed in `eaa50a08`; Greptile's review of that commit raised a failed-save pin and two coverage gaps, fixed in `f498be21`; the fix range then scored 5/5.
- Documentation: `./scripts/rzm validate` 0 issues. `./scripts/rzm validate frozen-scope-drift` reports three findings, all on other active efforts (EFF-2026-10-01-10-06, EFF-2026-10-03-18-33, EFF-2026-10-11-15-21): unrelated and pre-existing.
- Visual verification: light and dark screenshots of Notes, type tables, Edit mode, Problems, group views, the four custom views, Ontology, Explorer, GraphiQL, and Agent against a copy of Drew's vault served by this branch's build.
- Drew's planning decisions (2026-10-25): darker filled primary `#c8313a`; add dark tokens now for host and kit; self-host fonts and keep every current weight; parent PR with squash-merged phase PRs.

## Deviations

- Phase 1, 2026-10-25: the spec's preference non-goal admitted no REST change, but hiding "Reset to shared" when nothing is saved needs to know whether a custom view's widget slots hold values. Added an additive `familyOverrides` field to host preference snapshots (read-only SQL in `userstate.readSnapshot`, no schema change) and refreshed hosts on widget events; SPEC-0125 updated in place. Within the approved plan's scope (the reset rule); recorded because it touches an API the spec had excluded.
- Phase 1: kept the kit's 14px root (spacing density) and pinned text sizes instead of dropping the root; `Diagram` fills width for charts only, since scaling flowcharts enlarges their text; node presentations keep a Barlow title because the note tab does not title custom presentations. SPEC-0125 updated in place.
- Phase 1: undefined host tokens (`--line`, `--r-full`, `--surface-1/2`, and others) are replaced in phase 2's stylesheet sweep, which owns those files, rather than in phase 1.

## Closure Checklist

- [x] Spec coverage checklist complete or deviations recorded.
- [x] Quality gates recorded with exact commands and results (Execution Notes).
- [x] SPEC-0125 `active` and reconciled with delivery; DESIGN.md and `web/CONTEXT.md` updated.
- [x] Parent PR open; Greptile per phase and on the fix range (5/5), child PR CI green; parent PR CI runs on the final head.
- [x] Follow-ups recorded.

## Compounding Follow-ups

- GraphiQL's Monaco editor widgets (suggestions, hover) keep the library's own colors; build Monaco themes from the token values.
- Rhizome Desktop's shell still falls back to system fonts; serve the self-hosted fonts to it too.
- Atlas field rows are very small at default zoom.
- `--accent` on `--bg-tint` is 2.89:1 for selection edges (graphics minimum 3:1).
- Query-recipe views lost the client-side "Problems on this page" filter; decide whether they need a problems filter.
- Shared-save status is visible only inside View options; consider a status beside the closed button.
- `TypedSectionCard`, `FilePanel`, and `NarrativeView` pass inline Markdown components and still re-parse when they render.
- Re-measure the Explorer graph's first draw in a GPU-backed browser before tuning Sigma further.
- `make build` rewrites the tracked `pkg/app/web/assets/dist/index.html` and `third-party-licenses.txt`; regenerate them once on `main` or stop tracking them.
- Greptile cannot review the whole parent branch in one pass (too large); review per phase or keep parent branches smaller.
- `GroupFacts` keeps its own markup instead of the kit `Facts`; fold it in if the strip changes again.

## Status

Complete as of 2026-10-25. Delivery is carried by parent PR #37 for Drew to merge; after merging, run `make desktop-install-dev` so Rhizome Dev serves the kit his rebuilt views need.
