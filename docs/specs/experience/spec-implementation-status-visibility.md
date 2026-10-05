---
type: ExperienceSpec
id: SPEC-0072
summary: "Surfaces a satisfaction rollup for any typed parent note whose schema declares a collection of embedded children with a status enum — visible at three densities (type-home card badge, parent-note pane strip, dedicated coverage view) sharing one schema-derived contract. Generalizes the spec-completion idea so it applies to spec-driven, complex-domain, and any custom Rhizome ontology with a parent/children/status shape; the spec-driven starter is one instance of the pattern, not the only one."
spec-status: proposed
last-updated: 2026-06-15
aliases:
  - SPEC-0072
---

# Parent-child status rollup visibility

## Summary

Rhizome's typed graph already encodes parent notes that own collections of embedded children, where each child carries a lifecycle enum (`satisfied | ready | draft | abandoned` in the spec-driven starter; `completed | in-progress | blocked` or similar in other ontologies). The data is read-only in the workspace today — to know how much of a parent is "done," a user opens the note and counts statuses by hand.

This spec adds a **read surface** that lifts that data into the Notes workspace at three escalating densities, so a typed knowledge base of code (or any other domain) becomes legible as commitments-with-progress instead of opaque definitions:

- a card-level badge on the type-home cards of any qualifying parent type — for scanning across many parents at once
- a per-status breakdown strip on the open parent-note pane — for reading one parent in detail
- a dedicated configured view listing all qualifying parents with their rollups — for triage across the whole corpus

All three surfaces share one schema-derived rollup contract so the headline number means the same thing everywhere.

This is the read-leverage complement of [[notes-workspace-context-signals|SPEC-0071]] — that spec surfaces a typed note's *lifecycle* (the identity-level enum, e.g., `spec-status`); this spec surfaces its *commitment progress* (rollup over its embedded children).

### Why generic over the ontology

Rhizome is a tool for many kinds of vaults — spec-driven repos, complex-domain knowledge bases, transcript libraries, custom schemas. [[ontology-browser-workspace|SPEC-0014]] is explicit that readiness signals MUST stay "generic and schema-derived so project-kb, spec-driven, transcript, decision, and future ontologies all benefit." So this spec treats the spec-driven starter (`TechnicalSpec.userStories[].status`) as **one instance** of a generic pattern (`<ParentType>.<childCollection>[].<statusEnum>`), not as the contract itself. A custom ontology that declares `Course.modules[].progress: ModuleProgress` lights up the same UX with no implementation changes.

## Goals

- make parent-child commitment progress visible without requiring the user to open each parent note
- match the mental model: a typed parent note represents committed scope; its embedded children carry the granular state; the workspace should read both layers
- share one schema-derived rollup contract across all three surfaces so the headline number is stable and learnable
- let users learn which surface (card / pane / coverage view) they actually reach for by shipping each as a separate effort rather than guessing in advance
- stay generic over the ontology so any Rhizome user benefits, not just spec-driven repos
- stay additive: types that don't fit the shape (no qualifying child collection, no status enum, no identifiable terminal value) render exactly as today — no empty placeholder
- give ontology authors a precise way to declare "done" semantics via a schema annotation while still working out-of-the-box for common English-language enums via a lexical fallback

## Non-Goals

- inventing new project-management taxonomies (sprint, assignee, priority, layer, lens) per [[ontology-browser-workspace|SPEC-0014]]'s non-goal list — this spec reads what the schema already declares
- requiring a forced ontology migration; the annotation is opt-in and the runtime falls back to a lexical heuristic
- rebuilding type-home, the note pane, or the configured-view engine; this spec is additive to all three
- adding a write surface — the rollup is a read-only computed projection
- rolling up effort-level statuses; this spec rolls up child statuses on the parent, not efforts that touch the parent
- a separate progress view for individual children; granular state already lives in the parent's child collection section
- handling booleans, free-text statuses, or non-enum lifecycle fields in v1 (e.g., `ActionItem.done: Boolean` will not light up); future spec if the need emerges

## Schema-Derived Rollup Contract

The rollup engine evaluates each typed parent note through this cascade. At every step, a failure means "render nothing" — never "render an empty placeholder":

1. **Parent shape**: Does the parent's typed schema declare a collection of embedded children (e.g., a `[ChildType!]` field on the parent type)?
2. **Child status field**: Does the child type carry an enum field whose lowercased name ends in `status`? (Same heuristic [[notes-workspace-context-signals|SPEC-0071]] uses for the identity pill.)
3. **Terminal value identifiable**: Can the engine identify a terminal-positive enum value via schema annotation OR lexical match?
   - **Schema annotation** (precise path): a new `@status(terminal: true, polarity: positive | neutral | negative)` directive declared on enum values. Spec-driven starter annotates `satisfied` as `terminal: positive` and `abandoned` as `terminal: neutral`.
   - **Lexical fallback** (no-migration path): if no annotation is present, match the enum value's lowercased name against a known progress lexicon: `satisfied | complete | completed | done | delivered | shipped | resolved`. First match becomes the terminal-positive value.
4. **Committed scope present**: Does the parent have at least one child whose status is NOT the terminal-positive value AND NOT the terminal-neutral value (i.e., still-in-flight) OR IS the terminal-positive value (already done)? In spec-driven terms: at least one `ready` or `satisfied` story.

When all four pass, the rollup renders:

- **Numerator**: count of children whose status equals the terminal-positive value (spec-driven: `satisfied`)
- **Denominator**: count of children whose status is NOT terminal-neutral AND NOT non-committed-draft. In the spec-driven starter that's `ready + satisfied`. In a generic ontology this means: every enum value that is neither annotated as terminal-neutral (or lexically `abandoned | cancelled | won't-fix | wontfix`) nor identified as a non-committed-draft state (lexically: `draft | pending | unscoped`; schema annotation TBD if needed)
- **Draft indicator**: count of children whose status matches a non-committed-draft state, surfaced as a `+N draft` suffix when greater than zero
- **Terminal-neutral indicator**: count of children with a terminal-neutral status (spec-driven: `abandoned`), shown only on US2 pane strip and US3 coverage view — never on US1 card

When the parent's own lifecycle status (e.g., `spec-status`, picked via SPEC-0071's identity-status heuristic) is in a historical/terminal state (`archived`, `superseded`, or any enum value lexically matching `archived | superseded | retired | deprecated`), the rollup ratio is hidden because the parent is no longer the live contract — the identity-level pill still renders.

## User Stories

### US1 - See a satisfaction badge on type-home cards so I can scan parent progress across many parents at once

- id:: ^SPEC-0072-US1
- summary:: See a satisfaction rollup badge on each card in the type-home for any parent type whose schema fits the rollup shape, alongside the existing identity-status pill from SPEC-0071.
- status:: ready

The smallest, most discoverable surface. A user browsing `/notes/technical-spec` (or any qualifying parent type home — `/notes/domain-process`, `/notes/course`, etc.) sees at a glance which parents are mostly delivered, which are barely started, and which have unfinished drafts attached. Same card layout as today; one new badge slot.

#### Acceptance Criteria

- **Rollup badge renders next to the identity-status pill on type-home cards for qualifying parent types**: On a type-home page whose parent type passes the four-step cascade above, each note card renders a small rollup badge (e.g., `3/5`) adjacent to the identity-status pill added by [[notes-workspace-context-signals#^SPEC-0071-US1|SPEC-0071.US1]].
  verification:: Open `/notes/technical-spec` in a spec-driven repo; each card with at least one committed child story shows a numeric badge.
- **Rollup uses the schema-derived contract**: Numerator equals the count of children whose status equals the terminal-positive enum value (schema-annotated or lexically matched). Denominator equals the count of children whose status is neither terminal-neutral nor non-committed-draft. Terminal-neutral and draft children are excluded from both.
  verification:: A test fixture parent with 3 satisfied, 2 ready, 1 draft, 1 abandoned child renders `3/5`.
- **Draft indicator surfaces as a small suffix when at least one draft child exists**: When the parent has one or more children whose status matches a non-committed-draft state, the badge appends `+N draft` (or visually equivalent) to the rollup; no draft suffix is rendered when the count is zero.
  verification:: Same fixture as above renders `3/5 +1 draft`; a fixture with no draft children renders `3/5` alone.
- **Identity-status pill and rollup are both visible when the parent is in a live state**: When the parent's identity-status enum value is NOT in a historical/terminal state (not `archived | superseded | retired | deprecated` or lexically equivalent), both the identity pill (from SPEC-0071) and the rollup badge render. Neither replaces the other.
  verification:: A `proposed` or `active` parent card shows the identity pill AND the rollup badge side by side.
- **Rollup badge is hidden when the parent is in a historical/terminal state**: When the parent's identity-status enum value matches a historical/terminal state (schema-annotated or lexically: `archived | superseded | retired | deprecated`), the rollup badge is omitted. The identity pill still renders.
  verification:: An `archived` parent card shows the `archived` pill and NO rollup badge.
- **No badge when the parent type does not fit the rollup shape**: For types with no child collection, no enum status field on children, or no identifiable terminal value (no schema annotation AND no lexical match), no rollup badge slot renders. Cards are visually identical to today.
  verification:: Open `/notes/process-spec` (no `userStories` field) — cards show no rollup badge and no empty placeholder.
- **No badge when the parent has zero committed children**: When numerator and denominator are both zero (a parent with only draft children or no children at all), the rollup badge is omitted; if draft children exist, the draft indicator still renders alone (`+3 draft`). Mid-authoring parents do not display a misleading `0/0`.
  verification:: A parent with 0 satisfied, 0 ready, 2 draft children renders `+2 draft` and no ratio. A parent with 0 children at all renders no badge.
- **Badge is read-only**: Clicking the badge on a card does not open an editor or pop-up. Selecting the card itself still opens the parent note as today.
  verification:: Clicking the badge produces no interaction; clicking elsewhere on the card opens the parent.

---

### US2 - See a status breakdown strip on the open parent note so I can see what's where in detail

- id:: ^SPEC-0072-US2
- summary:: See a per-status child breakdown (`3 satisfied · 2 ready · 1 draft · 1 abandoned`) on the parent note pane when reading a parent, scoped to whatever enum values the schema declares.
- status:: ready

When a user opens a parent note, they already see the identity strip (lifecycle pill, id, title, last-updated). This story adds a small horizontal strip directly under (or beside) the identity strip showing the per-status breakdown — more detail than the card badge, scoped to the parent the user is currently reading. The labels come from the schema (not hardcoded English) so a custom ontology with `enum ModuleProgress { not-started in-progress completed }` shows those exact labels.

#### Acceptance Criteria

- **Strip renders on the parent note pane for qualifying types with at least one child**: When the user opens a parent whose type passes the rollup-shape cascade and has at least one child of any status, the pane renders a status breakdown strip near the identity strip.
  verification:: Open any spec in the Notes workspace; a strip showing per-status counts is visible above or beside the child collection section.
- **Strip surfaces every non-zero status separately, using the schema's enum value labels**: For each enum value in the child status field with a count greater than zero, the strip renders the count with the schema's label for that value. Enum values with zero count are omitted.
  verification:: A spec with 3 satisfied, 2 ready, 1 draft, 1 abandoned renders four cells using the spec-driven labels; a custom-ontology parent with `completed` and `in-progress` children renders those labels.
- **Headline rollup matches the card contract**: A summary number on the strip (e.g., `3/5 committed`) follows the same numerator/denominator rule as US1 (numerator = terminal-positive; denominator = committed scope; terminal-neutral and draft excluded).
  verification:: The same fixture used in US1.AC2 renders `3/5` in the headline portion of the strip.
- **Terminal-neutral children surface here even though they don't affect the rollup**: The strip shows the terminal-neutral count when greater than zero so a user reading the parent can see what was deliberately removed from scope, even though those children are excluded from the headline ratio per the locked contract.
  verification:: A spec with at least one abandoned story shows `· 1 abandoned` on the strip.
- **No strip when the parent has no children**: When the parent has zero children of any status (or the type does not fit the rollup shape), no strip renders.
  verification:: A parent note with no children renders the identity strip alone, with no status breakdown strip beneath it.
- **Strip is hidden when the parent is in a historical/terminal state**: Same lifecycle carve-out as the card badge — historical parents do not show progress against them.
  verification:: Open an `archived` parent; the strip is not rendered, but the identity strip's identity-status pill still shows `archived`.

---

### US3 - See a dedicated coverage view listing all qualifying parents with their rollups so I can triage progress across the whole corpus

- id:: ^SPEC-0072-US3
- summary:: See a sortable, filterable configured view in the Notes workspace listing every parent note that fits the rollup shape, with columns for id, title, type, identity-status, rollup, draft count, and last-updated.
- status:: ready

The highest information density. A user can sort by completion percentage, filter to one type or one lifecycle state, and find parents that are stuck (e.g., `active` lifecycle but 0% terminal-positive) or parents ready to flip to a historical state (e.g., 100% terminal-positive but still `active`). Lives in the Notes workspace as a configured view per [[ontology-browser-workspace|SPEC-0014]], alongside the existing domain navigation entries. Naming should reflect the rollup, not the spec-driven instance — e.g., "Progress" or "Coverage," not "Spec coverage."

#### Acceptance Criteria

- **A coverage view appears in the Notes workspace navigation**: A configured view (titled "Coverage" or equivalent) is reachable from the Notes workspace left rail under the existing navigation structure.
  verification:: Open the Notes workspace; "Coverage" appears as a clickable entry in the left rail.
- **The view lists every parent note across all types whose schema fits the rollup shape**: All typed notes whose parent type passes the four-step cascade appear as rows. Types that don't fit (no qualifying child collection, no enum status field, no identifiable terminal value) are NOT listed.
  verification:: In a spec-driven repo, the row count equals TechnicalSpec + ProductSpec + ExperienceSpec + OperationsSpec counts; ProcessSpec notes do NOT appear because the type has no `userStories` field. In a complex-domain repo, parent types with qualifying child collections appear; types without don't.
- **Columns include id, title, type, identity-status, rollup, draft count, last-updated**: Each row carries: parent id (if the type has an identifier), title, resolved type, identity-status enum value, rollup ratio, draft child count, `last-updated` date.
  verification:: The first row's cells map to those seven fields.
- **Rollup column uses the schema-derived contract**: Numerator and denominator follow the same rule as US1 and US2. Parents with denominator == 0 show a blank or `—` in the rollup column (not `0/0`); their draft-count column still shows the count if non-zero.
  verification:: A row for a parent with no committed children shows `—` in the rollup column and `3` in the draft column if it has 3 draft children.
- **Rows are sortable by rollup percentage**: Clicking the rollup column header sorts rows by the satisfaction percentage (terminal-positive / committed) ascending or descending. Rows with no denominator sort to one end consistently.
  verification:: Click the rollup header; rows reorder by computed percentage; click again to reverse.
- **Rows are filterable by identity-status and by type**: The view exposes filter controls (or a configured-view filter equivalent) for the parent's identity-status enum and resolved type.
  verification:: Filter to identity-status `active` and one type; the row count drops to only that combination.
- **Clicking a row opens the parent note in a new pane**: Row click opens the parent note as today (matching the configured-view click behavior for other typed views), not an inline expansion.
  verification:: Clicking a row opens the parent in a pane on the right, leaving the view as the root context.

## Requirements

- MUST compute the rollup server-side (as part of the typed-graph read path or as a derived projection on the configured-view engine) so all three surfaces read the same number. Client-side computation per surface would invite drift.
- MUST evaluate parent-type qualification via the four-step cascade documented in `Schema-Derived Rollup Contract`. Failure at any step renders nothing — never an empty placeholder.
- MUST identify the terminal-positive enum value via the schema annotation when present, falling back to the lexical match list (`satisfied | complete | completed | done | delivered | shipped | resolved`) only when no annotation is found. Identify the terminal-neutral value the same way (annotation, falling back to lexical match: `abandoned | cancelled | won't-fix | wontfix`).
- MUST exclude terminal-neutral and non-committed-draft children from both numerator and denominator of the rollup ratio. The draft count surfaces as a separate signal in all three surfaces; the terminal-neutral count surfaces only on the pane strip (US2) and the coverage view (US3), never on the card-level summary (US1).
- MUST hide the rollup ratio (US1 badge, US2 headline, US3 column value) when the parent's identity-status matches a historical/terminal state (schema-annotated or lexically: `archived | superseded | retired | deprecated`).
- MUST source child status from the typed graph's child status field (e.g., `UserStory.status` in spec-driven), not from a parallel parsing of frontmatter or markdown.
- MUST add a `@status(terminal: Bool, polarity: positive | neutral | negative)` directive to the ontology DSL and parser. Annotate the spec-driven starter's `UserStoryStatus` and `SpecStatus` enums, and the complex-domain starter's equivalent enums, as part of the first effort that ships any of these stories. Existing user vaults retain their own `.rhizome/ontology/*.graphql` files — `rzm init` re-run paths must not silently overwrite custom schema edits.
- SHOULD use the existing configured-view engine for US3 rather than building a parallel list renderer per [[ontology-browser-workspace|SPEC-0014]].
- SHOULD compose with [[notes-workspace-context-signals|SPEC-0071]] visually — the identity-status pill (SPEC-0071.US1) and the rollup (this spec) sit next to each other on cards; their styling should feel like one row of signals, not two competing systems.
- MAY ship as three separate efforts (one per user story) so the team learns which surface users actually reach for first.

## Open Questions

None at draft time. The schema-generic framing, the four-step cascade, the rollup contract, and the no-forced-migration approach (annotation + lexical fallback) were all locked in conversation before drafting.

## Documentation Plan

- **`.rhizome/ontology/spec-driven.graphql` + starter template**: Add the `@status` directive declaration and annotate `UserStoryStatus.satisfied` (`terminal: true, polarity: positive`), `UserStoryStatus.abandoned` (`terminal: true, polarity: neutral`), and the historical-state values of `SpecStatus` (`archived`, `superseded` → `terminal: true, polarity: negative`).
- **`.rhizome/ontology/complex-domain.graphql` + starter template**: Annotate equivalent terminal values on `RequirementStatus` and `DomainLifecycleStatus`.
- **`pkg/ontology/schema.go` (or wherever directives parse)**: Register `@status` as a known directive. Surface the parsed flags on `EnumValueDoc` so the rollup engine reads them through the existing reference-view contract.
- **`pkg/app/web/CONTEXT.md`**: Once US1 lands, extend the "Type/list seam" bullet to mention the rollup alongside `identityStatus` (the doc already covers `identityStatus` after the SPEC-0071 backport). Both are derived projections on `OntologyNoteListItem`.
- **`pkg/app/web/openapi.yaml`**: Each user story adds API surface — a `statusRollup` object on `OntologyNoteListItem` for US1, a similar field on the parent workspace payload for US2, and a configured-view definition for US3. Document each surface in the OpenAPI schema as it ships.
- **No new Hub note required**; this is additive UI complementary to SPEC-0071's coverage.
- **`pkg/app/mcp/resources.go`** (agent guide): Mention the rollup field on the type-home list response once US1 ships so agents can read commitment progress directly without enumerating children themselves.
- **Tests**: Backend integration tests covering the rollup cascade on the spec-driven fixture AND a generic-ontology fixture (to lock in the schema-genericity); Vitest tests for card / pane / view rendering. Reuse the polyglot fixture pattern from `pkg/app/web/server_integration_test.go::prepareIdentityStatusFixtureVault`.
