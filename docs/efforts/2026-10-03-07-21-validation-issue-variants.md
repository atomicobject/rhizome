---
type: EffortNote
id: EFF-2026-10-03-07-21
aliases: [EFF-2026-10-03-07-21]
name: Validation issue variants and case-level resolution
created-at: 2026-10-03T11:21:30Z
status: active
summary: Add an optional variant to validation findings, group Problems by issue kind and variant in three columns, and resolve an ambiguous-type variant or every safe repair with one staged review.
---

# Validation issue variants and case-level resolution

## Scope

Validation findings gain an optional, general **variant**: the specific case of a finding within its issue code, such as `type_ambiguous` on the candidate set `CoachingSession, Meeting`. Findings have no variant by default. Every existing check whose findings share a cause that one decision resolves emits a variant. Problems becomes a three-column triage surface (kinds and variants, findings, detail). An ambiguous-type variant can be resolved for all of its notes by staging `type` edits into the edit session, and one control stages every safe repair into a repair review.

Out of scope: new checks, new repair-planner action kinds, alternative (mutually exclusive) repair actions, durable suppression, and changes to how repair review or edit-session save apply writes.

## Spec Set (Frozen)

- [[validation-experience|SPEC-0100]] at the revision that adds US6 in this effort.

## Stories In Scope (Frozen)

- [[validation-experience#^SPEC-0100-US6|SPEC-0100 US6 — Triage by specific case and resolve a case at once]], all criteria AC1–AC6.

## Spec Coverage Checklist

- [x] AC1 variant contract and producers — Phase 1.
- [x] AC2 grouped counts and variant filter — Phase 1.
- [x] AC3 three-column Problems layout — Phase 2.
- [x] AC4 ambiguous-type choice staged into the edit session — Phase 3.
- [x] AC5 stage every safe repair — Phase 3.
- [x] AC6 stable refresh control — delivered before this effort was opened; verify in Phase 4.

## Plan

### Current state

- `validate.Issue` (`pkg/validate/types.go:87`) has no case-level field. `type_ambiguous` (`pkg/ontology/build_assessment.go:70`) carries its candidate types only in the message, and the web client recovers them with a regex (`web/src/components/validation/issueLabels.ts:112`).
- Diagnostics persist in SQLite `validation_diagnostics` (`pkg/anchors/sqlite/store.go:2112`, schema v69). Per-kind counts fan out one summaries request per code (`web/src/components/useValidationScopeSummaries.ts:63`); there is no grouped read.
- `type_ambiguous` is agent-only (`pkg/validate/remediation_registry.go:57`). It cannot become a set of alternative repair actions: repair transactions are computed over the whole plan (`pkg/validate/repair_review_selection.go:92`), so alternatives touching one note would form one connected transaction that requires and then conflicts with itself.
- The edit session already stages `setFrontmatter` operations (`pkg/app/web/ontology_sessions.go:1091`) in batches (`stageOps`, `web/src/components/useOntologyEditSession.ts:306`), and its save goes through `ApplyRepairSession`.

### Decisions

1. **Name: variant, not subtype.** "Subtype" already means type inheritance in the ontology. A variant is a case of an issue code.
2. **Shape.** `validate.Issue.Variant *IssueVariant` with `IssueVariant{Key, Label string}`. `Key` is stable and machine-comparable within `(check, code)`. `Label` is producer text that clients may format. A nil variant means the code has no cases. `ontology.ValidationIssue` gets matching `VariantKey`/`VariantLabel` fields because `pkg/ontology` cannot import `pkg/validate`; `ontologyValidationIssue` converts them.
3. **Issue identity is unchanged.** `StableIssueKey` excludes the variant. `type_ambiguous` adds `candidateTypes` to its `OntologyIssueData` evidence for the resolution choices, which changes those issue keys once.
4. **Storage and API.** Schema v70 adds `variant_key` and `variant_label` to `validation_diagnostics` with an index on `(generation, code, variant_key)`. `ValidationDiagnostic` gains `variant {key, label}`. The diagnostics and summaries filters gain `variant`, which applies within a `code`. A new grouped read, `POST /api/v1/validation/groups`, returns `{check, code, variant?, issueCount, affectedFileCount, applicableRepairCount}` rows for one generation, scope, and filter set with a single `GROUP BY`. It replaces the per-code fan-out.
5. **Who emits variants.** The rule is that one decision resolves every finding in the variant.

   | Codes | Variant key |
   | --- | --- |
   | `type_ambiguous` | Sorted candidate types |
   | `declared_type_mismatch` | Declared type → sorted candidates |
   | `unknown_declared_type`, views `unknown_ontology_type` / `unknown_ontology_interface` | The unknown name |
   | Field- and section-level ontology codes (`missing_required_field`, `field_shape_mismatch`, `field_type_mismatch`, `missing_required_section`, `empty_required_section`, `duplicate_section`, `wrong_section_level`, `contains_min_not_met`, `contains_max_exceeded`, `field_authoring_style_mismatch`, `field_format_mismatch`, `field_forbidden_pattern`, `conditional_required_*`, `link_target_missing`, `inverse_mismatch`, `identifier_not_in_aliases`) | Type and field |
   | `wrong_target_type` | Type, field, and expected target type |
   | `title_pattern_mismatch`, `title_forbidden_pattern` | Type |
   | Broken-link and placeholder-link codes | Missing target |
   | `identifier_strategy_migration_required` | Identifier pool |

   Per-instance codes stay variant-free: identifier collisions, link hygiene, code anchors, query recipes, code frontmatter, and companion docs.
6. **Resolution staging.** Choosing a type on an ambiguous-type variant pages through that variant's findings and stages one `setFrontmatter` operation per note (property `type`, value the chosen type) into the active edit session. "Stage N safe fixes" sends every `safe` action ID in the snapshot to the existing repair review. Both reuse current writers and review flows.

### Authorization

Drew approved this plan in chat on 2026-10-03 ("I approve. Execute"), including the repair review for safe fixes and opening a PR at the end. The configured current user is absent, so `plan-approved-by` is omitted. This covers implementation, review fixes, and PR publication, not merging.

### Phases

1. **Contract and producers.** The `IssueVariant` type, the ontology producer fields and conversion, variant producers for the codes above, `candidateTypes` evidence, the v70 migration plus insert, select, and filter SQL, the grouped-read store query and endpoint, the OpenAPI schema, `make web-generate`, and the subsystem notes for validation and stores. Tests cover producer variants per code family, key stability without a variant, migration, filter and cursor identity, and grouped counts that match summaries. An independent foundation review of the public shape comes before Phase 2 builds on it.
2. **Three-column Problems.** The first column lists kinds with nested variants (single-variant kinds collapse into one row) and keeps the By file mode. The second column holds the paged findings for the selected kind or variant, with the kind's explanation. The third column holds detail. Narrow widths keep the detail-only mode. The grouped read replaces `useValidationIssueCodeCounts`. Component tests cover selection, filter interplay, and paging.
3. **Case-level resolution.** Ambiguous-type type choices that stage edit-session operations, the global "Stage N safe fixes" control, and progress and failure states. Tests cover op construction, the staged count, and the safe-action selection.
4. **Verify and publish.** Docs (SPEC-0100 US6 status, validate, stores, and web subsystem notes), `make check`, a browser check at 1440x900 and 1024x768 against a synthetic vault containing overlapping types, an independent review, then a PR against `main`.

## Original Intended Delivery

Problems shows issue kinds with their specific cases and counts. Ambiguous notes resolve with one type choice staged for review.

## Actual Delivered

Validation findings carry an optional `variant {key, label}`, excluded from issue identity. Ontology type and field codes, broken and placeholder links, unknown view targets, and identifier strategy migrations emit variants; per-instance codes do not. `type_ambiguous` records sorted `candidateTypes` evidence. Intel schema v70 persists the variant, the diagnostics and summaries filters accept `variant` with `code`, and `POST /api/v1/validation/groups` returns per-variant counts from one grouped query that match the narrowed summaries.

Problems is a three-column surface: kinds with nested variants (the server returns at most 25 per kind and rolls the rest into one row), the paged findings for the selection, and detail. Filter edits keep the previous counts on screen until new ones arrive. An ambiguous-type variant offers "Set type: X on N notes", which stages one `setFrontmatter` edit per note into the edit session; "Stage N safe fixes" sends every safe action to one repair review. The refresh control stays put while checks run.

Known limits: a code's variant-free findings are reachable only through the code row; broken-link variants split on authored case; a whole variant stages in one edit-session request; the frontmatter setter rewrites flow lists in block style; `NotesIssuesHome.tsx` is 597 lines, above the roughly 500-line guideline.

## Execution Notes

- 2026-10-03T11:21:30Z — The refresh-control fix (AC6) was made and visually verified in this worktree before the effort opened.
- 2026-10-03T11:30:00Z — Contract committed (0050e6c). Backend (Opus 5.5 xhigh) and web (Opus 5.5 high) workers ran in parallel on disjoint files against the regenerated types.
- 2026-10-03T12:20:00Z — Browser check on a synthetic vault (12 CoachingSession/Meeting and 3 Decision/Incident overlaps): grouped endpoint returned both variants with counts 12 and 3; the variant filter returned 3; no page-level horizontal scroll at 1440x900 or 1024x768; "Set type: CoachingSession on 12 notes" staged 12 edit-session changes and Save wrote `type: CoachingSession` to each note, dropping validation from 15 to 3 issues. Narrow finding rows now give the path its own line.
- 2026-10-03T12:45:00Z — Independent review (Fable) found no correctness bugs. Fixed: added the v70 upgrade test, capped variant rows per kind, documented the unbounded grouped read and the unreachable variant-free remainder, kept previous counts during refetch, gave nested rows kind-qualified accessible names, corrected the stores note, and extracted repair notices from `NotesIssuesHome.tsx`. Deferred: case-folding broken-link variant keys and batching very large type staging.
- 2026-10-03T13:05:00Z — Greptile (4/5) flagged a stale case staying actionable after a filter change and the unbounded grouped read. Changing the check or kind filter now clears the selected case and hides type staging while counts reload; the server caps variant rows at 25 per code with an `otherVariants` roll-up row.
- 2026-10-03T12:20:00Z — Observed: the existing frontmatter setter rewrites flow lists such as `tags: [coaching]` into block style when it writes `type`. Pre-existing behavior of `obsidian.SetFrontmatterProperty`, outside this effort's scope.

## Deviations

- The ontology materialization version moves from 11 to 12 so existing indexes rebuild their persisted issues with variants and `candidateTypes`. Authority: required for Decision 3 to reach existing vaults.
- `ontology.ValidationIssue` carries `CandidateTypes` as well as the variant fields, because the conversion needs a source for the `candidateTypes` evidence.
- The `(generation, code, variant_key)` index is created only by the v70 migration step. `ResetDomain` keeps `validation_*` tables, so a baseline index could reference columns an old table lacks.
- Problems adds an "All issues" row at the top of the kinds column so deep links and a restored selection resolve without knowing the issue's kind.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

None yet.

## Status

Active. Delivered in PR #6 (https://github.com/atomicobject/rhizome/pull/6), awaiting review and merge. Closure follows merge.
