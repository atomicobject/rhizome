---
type: EffortNote
id: EFF-2026-09-19-18-09
aliases:
  - EFF-2026-09-19-18-09
name: Note Link Hover Preview
created-at: 2026-09-19T22:09:06Z
status: active
plan-approved-by: Drew Colthorp
plan-approved-at: 2026-09-19T22:09:06Z
summary: "Ship index-backed hover preview cards for note links in the web workspace, shaped by a new field-level @display annotation, with the starter ontologies annotated and configured-view labels moved to a shared humanizer."
---

# Note Link Hover Preview

## Scope

Drew asked on 2026-09-19 for an Obsidian-style link hover preview that uses Rhizome's structural understanding of notes. The design was worked out in one conversation through three throwaway prototype rounds, each driven against this repository's own vault in the built web UI:

1. A client-only card fed by the node workspace query. It proved the interaction (dwell, placement, pointer travel) and showed the workspace payload was too heavy for a tooltip.
2. An index-backed endpoint plus a field-level `@display` annotation, with the starter ontologies annotated by educated guess. Server time dropped to 0.2-5ms per card.
3. Link-valued properties rendered as links that open nested cards.

This effort turns that prototype into production code. It is an effort rather than a local task because it changes ontology schema semantics (a new field-level directive surface), generated init behavior (the starter ontologies), and the public REST contract.

Excluded: body excerpts, section previews for fragment links, editor and graph hover, edit-session overlays, and the other intended consumers of `@display` importance (properties panel, configured-view default columns). [[note-link-hover-preview|SPEC-0106]] records these as non-goals and open questions.

## Spec Set (Frozen)

- [[note-link-hover-preview|SPEC-0106]] Note link hover preview (new in this effort).

## Stories In Scope (Frozen)

- [[note-link-hover-preview#^SPEC-0106-US1|SPEC-0106.US1]] Preview a linked note by hovering its link.
- [[note-link-hover-preview#^SPEC-0106-US2|SPEC-0106.US2]] A preview that stays out of the way.
- [[note-link-hover-preview#^SPEC-0106-US3|SPEC-0106.US3]] Follow and preview links inside a card.
- [[note-link-hover-preview#^SPEC-0106-US4|SPEC-0106.US4]] Shape previews from the schema.
- [[note-link-hover-preview#^SPEC-0106-US5|SPEC-0106.US5]] Importance shapes the properties panel and view defaults (added 2026-09-19, see Deviations).

## Spec Coverage Checklist

- [ ] SPEC-0106.US1: typed, untyped, HTML, issue, unresolved-fragment, and unresolvable states render; link seams are wired.
- [ ] SPEC-0106.US2: dwell, open timing, placement, pointer travel, dismissal, keyboard, and coarse-pointer behavior hold under test.
- [ ] SPEC-0106.US3: link values render as links, nested cards open beside the parent, and closing is scoped.
- [ ] SPEC-0106.US4: `@display` defaults, KEY, DETAIL, hover opt-out, SUMMARY role and convention, compile errors, and starter annotations.
- [ ] SPEC-0106.US5: panel order, detail fold, never-hidden rules, unannotated types unchanged, view defaults by importance, authored views unchanged.
- [ ] SPEC-0106 requirements: index-only reads, bounded lookups, public contract surfaces, caps, date rendering, shared humanizer.

## Decisions and boundaries

- **REST, not GraphQL.** The preview is one small cacheable GET keyed by a ref. The public API already carries sibling REST reads (`/api/v1/suggest`, `/api/v1/views`), and a REST route keeps the GraphQL node type free of a presentation concern.
- **Extend `@display` rather than add a directive.** `@preview` already means a section's collapsed-line template. `@display` already means presentation on types, so field presentation belongs there. Type arguments on a field, and field arguments on a type, are compile errors so the two uses cannot blur.
- **No `label` argument.** Guessing labels across 67 starter fields produced five, all renames by taste. The real defect was three separate humanizers, one of which did not split camel case. One shared humanizer replaces the need.
- **`summary` holds the SUMMARY role by convention.** Every starter type already names its summary field `summary`, so no starter needs the explicit role.
- **Metadata only.** Drew chose index-only content over body excerpts; the thin card for plain notes is recorded as an open question in the spec.
- **Request on dwell, not on render.** No prefetching of links the reader has not paused on.
- **SPEC-0106, not SPEC-0105.** The allocator offered SPEC-0105, which an unmerged branch (custom view apps) already uses. Skipping it avoids a collision at merge.

## Plan

### Batch 1 - Ontology annotation (Go)

Typed `FieldDisplay` on compiled fields, compile-time validation, SUMMARY role resolution with the `summary` convention, the shared `HumanizeFieldName`, configured-view labels moved onto it, and authoring documentation. Exit: `go test ./pkg/ontology/... ./pkg/app/views/...` pass with table-driven coverage of parsing, defaults, every compile error, and the humanizer.

### Batch 2 - Preview endpoint (Go)

`GET /api/v1/nodes/preview` from the summary hydrate profile, a pure record-to-preview function under table-driven tests, one resolve for the target and one batched resolve for link values, OpenAPI and capabilities entries, generated web types, and public API docs. Root-cause the two prototype findings: the empty record after a bare-path resolve, and the missing type on subsystem reference docs. Exit: handler tests for 200, 400, 404, 405, fragment fallback, and link resolution pass; a smoke run against this vault returns typed cards.

### Batch 3 - Web card (TypeScript)

Production component under `web/src/components/notePreview/` with the hover-intent timing separated for fake-timer tests, contract-shaped card rendering, nested cards placed beside the parent, keyboard and coarse-pointer behavior, and the link seams wired. Exit: `make web-typecheck`, `make web-lint`, `make web-test` pass; Playwright screenshots show the card and a nested card that does not cover its parent's links.

### Batch 4 - Starters, docs, integration

Starter ontology annotations with `.rhizome/ontology` kept identical, the generated-surface gate (`rzm init --yes` twice), `make check`, `rzm validate`, prototype scratch removed, PR opened, review loop run.

### Batch 5 - Follow-ups folded in (added 2026-09-19)

Heading and block links name their fragment in the card from indexed fragment targets. The related-notes rail and configured-view relation values become preview triggers through one reusable trigger rather than a second state machine. `@display` importance orders the properties panel, folds DETAIL properties, and picks configured-view default columns; the workspace and view field capabilities expose importance. Exit: `make check` passes, a live run shows a rail card, a relation-value card, a heading card, the folded properties panel, and an importance-ordered default view.

## Original Intended Delivery

All four stories of SPEC-0106 in one pull request.

## Actual Delivered

- Field-level `@display(role, importance, hover)` with typed constants, positioned compile errors, SUMMARY role resolution through interface inheritance, and the hints surfaced in the ontology guide and reference output next to policy hints.
- `ontology.HumanizeFieldName` (camel, kebab, snake, digits, acronyms, idempotent), used by configured-view labels and the preview.
- `GET /api/v1/nodes/preview` served from the summary hydrate profile: one resolve for the target, one batched resolve for every link value, `IndexOnly` so a fragment missing from the catalog is never projected from source. OpenAPI, capabilities, generated web types, and `docs/api/public-api.md` updated.
- Two read-path fixes in `noderead` found by the prototype: `Resolve` now returns the summary record for a bare note path in one call, and summary records take their type from indexed type rows, so notes typed by `@node(matches:)` rather than authored `type:` frontmatter are no longer untyped on the summary path.
- Web: `web/src/components/notePreview/` with the hover-intent state machine separated from rendering, measured placement, nested cards beside the parent, an Escape stack, keyboard focus, and no behavior without hover support. Wired into narrative bodies and the ontology note pane.
- Starter ontologies annotated (core, action-items, complex-domain, agentic-engineering) with `.rhizome/ontology` kept identical, and `@display` documented in the ontology authoring reference.
- Batch 5: fragment links name their heading or block from indexed fragment targets (`fragment` on the response, no file read). One reusable preview trigger (`useNotePreviewTrigger`) serves anchors, related-notes rail buttons (cards open left of the rail), and per-value relation links in configured-view tables, cards, and boards; view rows carry `relationValues` with index-hydrated titles and refs. `@display` importance orders the properties panel and folds DETAIL properties, the workspace capability exposes `displayImportance`, the view capability exposes `importance`, and generated view defaults choose KEY fields while Issues and Updated keep their columns. Generated column labels are humanized. SPEC-0058's default-derivation sentence was revised to match.

### Verification evidence

- After Batch 5: `make check` pass (788 web unit tests); GraphQL schema golden covered by `go test -tags "fts5 integration" ./tests/integration/python/...`; live run shows `fragment: {kind: heading, text: "Where to start for a task"}` for a heading link, a rail card opening left of the rail, a relation-value card in a configured view, the folded properties panel, and the generated ProductSpec view with columns Title, ID, Spec status, Summary, Issues, Updated.

- `make check`: pass (Go unit tests, web lint and typechecks, 772 web unit tests).
- `make check-fast` after scratch removal: pass.
- `./scripts/rzm validate`: 0 issues.
- Live run against this vault on the built binary: typed, untyped, HTML, and unresolved-fragment previews return in 1-8ms server time; 400, 404, and 405 use the public error envelope; Playwright drove a card and a nested card, and the nested card opened beside its parent (parent rect right edge 677, nested left edge 683) with pointer travel and leave behaving as specified.
- `rzm init --yes` twice: the second run reports no updates. The first run also rewrote generated surfaces unrelated to this effort (agentic-engineering skill references, `.rhizome/workflows.yml`, a migration manifest fingerprint, the AGENTS.md notes line, `docs/efforts/templates/`). That drift exists on `main` and was left out of this change set.
- Not run: `make web-e2e` (one scenario was added to `web/tests/e2e/tabs.spec.ts`); CI runs the full gate.

## Execution Notes

- 2026-09-19: Plan approval is Drew's chat direction to "convert this into actual implementation and productionize it" after the third prototype round. The written plan above was composed after that direction from the design settled in the conversation; the pull request is where he confirms it reads as intended.
- 2026-09-19: Batches 1-2 and batch 3 were delegated to two implementers working in disjoint directories against a shared written contract, then reviewed by the orchestrator.

## Deviations

- 2026-09-19, scope widened by Drew after reviewing PR #286: fold the listed follow-ups into this effort instead of deferring them. In: heading shown for fragment links, rail previews, configured-view relation value previews, and importance in the properties panel and view defaults (new story SPEC-0106.US5). Still out by his decision: body excerpts, including section content; a thin card with title and path is acceptable. SPEC-0106 was revised to match and the plan gained Batch 5.
- The coverage criterion in SPEC-0106.US1 was first narrowed to the two Markdown link seams, because the related-notes rail renders buttons and configured-view relation cells rendered one joined string. Batch 5 then delivered both.

## Compounding Follow-ups

- Generated surfaces on `main` are stale against their templates; `rzm init --yes` rewrites several files unrelated to any one change. Worth a small cleanup so the generated-surface gate is quiet again.
- Relation cells that are editable render edit widgets, so the relation-value preview is reachable only where a relation field is read-only in that view. A read-mode presentation for editable relation cells would make it reachable everywhere.
- The first importance-ordered default let NORMAL fields displace the Issues and Updated columns; the rule now reserves their slots. Worth remembering when other surfaces adopt importance: fill leftover room, never displace operational columns.

## Closure Checklist

- [ ] Spec coverage checklist complete
- [ ] Quality gates recorded
- [ ] Subsystem notes updated where invariants moved
- [ ] SPEC-0106 status and story statuses reconciled

## Status

Active. Implementation in progress on 2026-09-19.
