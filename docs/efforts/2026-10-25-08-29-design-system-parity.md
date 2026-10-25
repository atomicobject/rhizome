---
type: EffortNote
id: EFF-2026-10-25-08-29
aliases: [EFF-2026-10-25-08-29]
name: Design system parity
created-at: 2026-10-25T12:29:51Z
status: active
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

- [ ] Tokens and themes, including dark mode, palettes, and self-hosted fonts.
- [ ] Color and type rules across host surfaces.
- [ ] Kit defaults, new primitives, `NoteLink`, `StatusMark`, `Diagram` width.
- [ ] Loading and empty states.
- [ ] Host chrome: reset button, collection toolbar, issue labels, edit mode, Atlas and Type Detail.
- [ ] Accessibility requirements.
- [ ] Performance requirements.
- [ ] Author guidance and regenerated skill copies.
- [ ] Drew's custom views rebuilt on the kit.

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

Pending.

## Execution Notes

- 2026-10-25: Audit evidence (screenshots, detector output, four review reports) gathered in the planning session; the shell's Impeccable update landed first as `1f17987b` and `c593d2f2` on this branch.
- 2026-10-25T12:33:57Z: Drew approved this plan revision and its frozen scope in chat ("Yes"), including the branch, PR, squash-merge, and vault-view authority listed under Plan. No current Person is configured in this checkout, so `plan-approved-by` is omitted; the chat approval stands.
- Drew's planning decisions (2026-10-25): darker filled primary `#c8313a`; add dark tokens now for host and kit; self-host fonts and keep every current weight; parent PR with squash-merged phase PRs.

## Deviations

None yet.

## Closure Checklist

- [ ] Spec coverage checklist complete or deviations recorded.
- [ ] Quality gates recorded with exact commands and results.
- [ ] SPEC-0125 set to `active` and reconciled with delivery; DESIGN.md updated.
- [ ] Parent PR open with Greptile 5/5 and green CI.
- [ ] Follow-ups recorded.

## Compounding Follow-ups

Pending.

## Status

Active; Phase 1 (foundation) in progress.
