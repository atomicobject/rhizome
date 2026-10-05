---
type: ExperienceSpec
id: SPEC-0100
aliases:
  - SPEC-0100
summary: "Defines trustworthy validation state, quiet contextual diagnostics, and safe repair review in the Notes workspace."
spec-status: active
last-updated: 2026-10-03
---

# Validation experience

## Summary

Validation should help a user understand and reconcile real problems without making healthy work feel broken. A clean vault looks calm. Running, stale, incomplete, and failed checks remain visibly different from a completed clean result. Findings stay available through bounded detail pages, appear quietly in relevant browsing contexts, and lead to server-authorized repair reviews.

This spec follows the Problems experience deferred by [[notes-workspace-shell|SPEC-0090]]. It composes with [[validation-fixes-workspace|SPEC-0017]], [[validation-fix-plan-and-apply-session|SPEC-0018]], and [[frontend-data-lifecycle|SPEC-0074]]. The separate schema-guided editing effort owns field controls, drafts, source authoring, replay, conflicts, recovery, and save outcomes.

## Goals

- Report what validation actually established, including scope, selected checks, freshness, completion, and known findings.
- Preserve complete diagnostic detail behind bounded, stable, generation-specific reads.
- Fit Problems into the Instrument Notes design with a compact clean state and dense, usable triage.
- Show quiet issue presence and counts in relevant notes, lists, views, search results, and graphs.
- Let users review and apply exact canonical repairs without recreating authority in the browser.
- Keep contextual reads cheap and avoid relayout, full-vault hydration, or validation work during ordinary browsing.

## Non-Goals

- Field editors, narrative buffering, source editing, local draft recovery, or manual-edit conflict resolution.
- A second write engine or edit-session protocol.
- Durable suppression or accepted-baseline policy.
- Automatic agent execution or generation of substantive replacement content.
- A new graph layout, type-color scheme, dark theme, or mobile redesign.

## User Stories

### US1 - Know what validation established

- id:: ^SPEC-0100-US1
- summary:: See accurate scope, findings, completion, and freshness without false clean results.
- status:: ready

#### Acceptance Criteria

- Clean appears only for a completed, current, successful selected scope with zero findings and no failed or blocked applicable checks. ^SPEC-0100-US1-AC1
- Never checked, running, complete, incomplete, failed, and stale states have distinct wording. A refresh retains and labels the last completed snapshot. ^SPEC-0100-US1-AC2
- Counts distinguish issue instances, affected files, affected notes, and repair actions and come from one diagnostic generation. ^SPEC-0100-US1-AC3
- Aliases resolve through the validation registry. Unknown or unavailable requested checks return typed errors instead of successful empty results. ^SPEC-0100-US1-AC4
- Complete findings remain inspectable through stable bounded pages. Filters or generation changes cannot mix pages, totals, or selected identity. ^SPEC-0100-US1-AC5

### US2 - Triage problems in a quiet workspace

- id:: ^SPEC-0100-US2
- summary:: Inspect individual findings and grouped repairs in a compact Problems surface.
- status:: ready

#### Acceptance Criteria

- The clean view is a compact neutral status with optional check details. It has no alert hero, red zero, fix instructions, empty filter, or empty-list settings. ^SPEC-0100-US2-AC1
- A nonempty view shows an issue and file summary, file grouping, optional check grouping, and server-side filters for check, code, repair availability, and text. An empty filtered result offers Clear filters and never claims the vault is clean. ^SPEC-0100-US2-AC2
- Selecting a finding reveals its explanation, source, exact location, related instances, and actions. Selection survives a refresh when the same finding remains. ^SPEC-0100-US2-AC3
- Repair rows remain distinct from findings and show safety, affected paths, transaction membership, confirmation requirements, and a preview before apply. ^SPEC-0100-US2-AC4
- At 1440x900 and 1024x768, triage, detail, and primary actions remain usable without page-level horizontal scrolling. ^SPEC-0100-US2-AC5

### US3 - Discover issues in context

- id:: ^SPEC-0100-US3
- summary:: See restrained issue indicators while browsing and open the matching detail without losing context.
- status:: ready

#### Acceptance Criteria

- Global navigation, type and file lists, search results, note identity, and configured views use generation-matched scoped aggregates. Zero badges disappear; unknown and stale values do not render as zero. ^SPEC-0100-US3-AC1
- A note count opens its complete scoped findings. Fields, relations, links, and source locations can disclose local findings through accessible controls without filling the page with warning color. ^SPEC-0100-US3-AC2
- Graphs preserve type colors and layout. Selected-node detail may show a count, and an explicit Problems overlay may reveal affected nodes through batched reads. ^SPEC-0100-US3-AC3
- Opening a finding targets its canonical note, node, field, relation, or source range. Missing targets fall back to the file and finding detail. ^SPEC-0100-US3-AC4

### US4 - Review the exact authorized repair

- id:: ^SPEC-0100-US4
- summary:: Review and apply canonical repairs while preserving their generation and source preconditions.
- status:: ready

#### Acceptance Criteria

- Staging creates an opaque server-owned review from selected action IDs, generation, and plan fingerprint. The browser never rebuilds operations from hints. ^SPEC-0100-US4-AC1
- Safe, confirmation-needed, agent-required, and unavailable actions have explicit paths. Connected transaction expansion requires consent. ^SPEC-0100-US4-AC2
- Reviews expose diffs, source preconditions, expiry, and overlapping manual paths. Changed or expired authority requires revalidation and a new review. ^SPEC-0100-US4-AC3
- Apply uses the existing validation transaction engine, reports each independent transaction honestly, and returns the recorded result on an idempotent retry. ^SPEC-0100-US4-AC4
- Manual editing retains ownership of its drafts and save flow. Validation reserves overlapping paths and triggers a validation refresh after successful publication through narrow integration seams. ^SPEC-0100-US4-AC5

### US5 - Reconcile efficiently and accessibly

- id:: ^SPEC-0100-US5
- summary:: Navigate, filter, and repair findings with bounded work and predictable assistive behavior.
- status:: ready

#### Acceptance Criteria

- Disclosures expose names, expanded state, and controlled regions. Focus is visible; Escape restores focus; next and previous navigation work without a pointer. ^SPEC-0100-US5-AC1
- Failures use alert semantics and status progress uses polite announcements without announcing every refresh or keystroke. Reduced motion disables transient animation. ^SPEC-0100-US5-AC2
- Cached summaries and badges use indexed aggregates and requests of at most 200 scopes. Paging has honest totals and stable ordering; graph counts do not replace or relayout the canvas. ^SPEC-0100-US5-AC3
- Delivery evidence records comparable 1,000 and 10,000-file reads, large diagnostic paging, request counts, transferred bytes, and browser long tasks. ^SPEC-0100-US5-AC4

### US6 - Triage by specific case and resolve a case at once

- id:: ^SPEC-0100-US6
- summary:: Group findings of one kind by their specific case and resolve every note in a case with one reviewed decision.
- status:: ready

#### Acceptance Criteria

- A finding may carry an optional variant that names its specific case within its issue code, with a stable key and a readable label. Codes without variants behave as before. ^SPEC-0100-US6-AC1
- Variant counts come from one grouped read for the current generation, scope, and filters, and the diagnostics filter accepts a variant within a code. ^SPEC-0100-US6-AC2
- Problems lists issue kinds with nested variants and counts in its first column, the findings for the selected kind or variant in its second column, and the selected finding's detail in its third column. ^SPEC-0100-US6-AC3
- An ambiguous-type variant offers one choice per candidate type that stages a `type` frontmatter edit for every note in the variant into the active edit session, which the user reviews and saves through the existing save flow. ^SPEC-0100-US6-AC4
- One control stages every browser-applicable safe repair in the current generation into a single repair review. ^SPEC-0100-US6-AC5
- The validation refresh control keeps its position while checks run. ^SPEC-0100-US6-AC6

## Requirements

- The snapshot REST envelope is versioned at `/api/v2/validate`; the retired v1 result endpoint returns an explicit 410 migration error. New diagnostics and repair endpoints retain their v1 routes.
- Published snapshot code facets cover the whole generation. Retained previous-generation detail is read-only, and selected repairs are bound to both generation and plan fingerprint.
- Capacity pressure cannot evict unexpired terminal repair receipts; new reviews fail with an actionable capacity error until space becomes available.

- One published diagnostic generation owns check outcomes, complete sanitized findings, scope memberships, action memberships, and aggregate counts.
- Run lifecycle and the last published snapshot are separate. Reads never trigger a validation run.
- The store retains the current and immediately previous published generations so an in-flight page remains coherent during refresh.
- Diagnostic cursors bind generation, normalized filters, and sort order. The default page is 100 and the maximum is 200.
- Repair authority remains in memory with the original validation result and run context. Persisted diagnostics and client state are presentation only.
- Repair apply reuses the existing validation transaction, locking, recovery, refresh, and postcheck machinery. Validation does not introduce another physical writer.
- Manual edit overlap protection and post-save refresh are narrow shared seams. The editing subsystem owns all other manual-edit behavior.
- Draft validation is deferred until it can consume the editing subsystem's acknowledged revision and immutable overlay without introducing a parallel queue or session protocol.
- Diagnostic location units explicitly distinguish UTF-8 source bytes from browser offsets.
- Non-note and untyped-file findings remain visible globally and in file scope.

## Documentation Plan

- Update validation, stores, indexing, GraphQL, and web subsystem notes for changed contracts.
- Record the repair-review lifecycle and the boundary with schema-guided editing.
- Keep implementation evidence and known limitations in the delivery effort.
