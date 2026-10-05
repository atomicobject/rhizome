---
type: TechnicalSpec
id: SPEC-0073
summary: "Make the `hotspots` and `doc_coverage` reports useful on sparse-call-graph languages by adding an auto-fallback signal (import-fan-in + raw symbol-ref counts via `intel_symbol_refs`) when a language's call-resolution rate is below a threshold, plus a `--lang` filter for explicit per-language selection. Drives readiness for a client legacy-codebase engagement: its LAMP stack (legacy procedural PHP + modern Laravel 8.3) will hit the same PHP-resolution-rate ceiling we saw on wp-tst (37.97% → 41.41% post-EFF-0037, still well below the ~60% threshold needed for the existing anchor-join-based scoring to surface PHP results)."
spec-status: active
last-updated: 2026-06-15
aliases:
  - SPEC-0073
  - sparse-graph-fallback-for-assessment-reports
---

# Sparse-Graph Fallback for Assessment Reports

## Summary

The four canonical assessment reports — `complexity`, `hotspots`, `doc_coverage`, `code_similarity` — feed the `legacy-codebase-assessor` skill's priority-files output. Two of them (`hotspots`, `doc_coverage`) score by **resolved call-graph connectivity** — they join `intel_edges(kind='calls')` through `intel_code_anchors` on both ends. When a language's call-resolution rate is low, both ends of the join fail (caller or callee lacks an anchor row), the language's authority scores collapse, and the report returns rows from higher-resolution languages only.

This is exactly what bit the wp-tst assessment (2026-06-15): WordPress dominated 98.4% of indexed symbols but `hotspots` and `doc_coverage` returned **JS-only**, surfacing 20–25 minified bundled JavaScript files instead of the PHP code that runs WordPress. Post-EFF-0037 the PHP resolution rate climbed 37.97% → 41.41%, but the threshold where the anchor-join scoring stops blanking PHP is closer to 60%. Without further work, the same blind spot will bite a client LAMP codebase whose legacy procedural PHP + modern Laravel 8.3 backend share the same shape (sparse call graph + many file-scope registrations).

This spec defines two changes to the two affected reports:

1. **Auto-fallback when sparse.** When a language's resolution rate (computed at query time, matching `rzm code stats`'s `callsResolved / callsExtracted` ratio) falls below a configurable threshold, the report supplements its scoring with `intel_symbol_refs`-based fan-in/fan-out signal (extracted-but-unresolved refs still carry path + dst-target information). Languages above threshold are unaffected.
2. **Explicit `--lang` filter.** Both reports gain a `--lang <code>` flag (repeatable) that constrains output to the named language(s) regardless of resolution rate. Useful when the user knows their corpus shape and wants to force a specific lens.

This is not a fix for the underlying resolution-rate problem — that requires the deferred method-resolution work (lightweight type inference for `$obj->method()` calls; tracked as an EFF-0035 compounding follow-up). This spec makes the assessor reports usable *now* against sparse-graph languages, so engagement starts don't hit the JS-only blind spot.

## Goals

- `rzm code hotspots` and `rzm code doc-coverage` surface non-trivial PHP rankings on the wp-tst corpus (currently both return JS/TS-only)
- The assessment skill produces a useful priority-files signal from `hotspots` + `doc_coverage` against both the legacy procedural PHP and the modern Laravel 8.3 layer
- A `--lang` flag on both reports lets users explicitly select one or more languages for output, decoupling report results from the implicit resolution-rate gating
- Existing rankings on dense-call-graph languages (TS in the rhizome repo, Go in any indexed Go corpus) do NOT change shape — fallback only kicks in below the threshold; above it the existing query path runs unchanged

## Non-Goals

- Does not close the underlying call-resolution gap. The wp-tst PHP resolution rate stays at ~41% after this work; only the report-surface behavior changes. Type-inference work for member-call resolution remains deferred.
- Does not redesign the `complexity` or `code_similarity` reports. Both already work on sparse graphs (`complexity` is line-span based; `code_similarity` is embedding-based) and are out of scope.
- Does not change the underlying schema or the indexer's extraction behavior. All changes are query-side and CLI-flag-side.
- Does not unify per-language threshold tuning into a single global configuration system; thresholds are simple defaults plus optional per-language override via the existing report-options structs.

## User Stories

### US1 - Auto-fallback for sparse-call-graph languages

- id:: ^SPEC-0073-US1
- summary:: When a language's resolution rate (callsResolved / callsExtracted, computed at query time from the same data `rzm code stats` exposes) falls below a configurable threshold (default 0.60), the `hotspots` and `doc_coverage` queries supplement their scoring with rows derived from `intel_symbol_refs` + `intel_symbol_ref_files` + `intel_symbol_ref_targets`, surfacing files and packages that would otherwise be filtered by the anchor-join. Languages above threshold use the existing query path unchanged.
- status:: satisfied

#### Acceptance Criteria

- **`rzm code hotspots` on wp-tst surfaces PHP rows in the top-N**: With v1.7.0-stamped wp-tst index (PHP resolution ~41%), running the default `rzm code hotspots` returns a result set containing PHP packages/files in the top 25, not exclusively JS/TS.
  verification:: Direct invocation against the wp-tst DB shows PHP rows. Assertion: at least one PHP file in the top-10 of `HotspotFiles` and at least one PHP package in the top-10 of `HotspotPackages`.
- **`rzm code doc-coverage` on wp-tst surfaces PHP rows in the top-N**: Same shape as above — PHP entries appear in the default doc-coverage output.
  verification:: Direct invocation shows PHP `kind=class|method|func` entries in the top 25, not exclusively TS minified bundles.
- **Languages above threshold are unaffected**: On a dense-call-graph corpus (the rhizome repo's own Go index), the report output matches the v1.7.0 baseline exactly (or within deterministic ordering-on-ties variations). No silent re-ranking.
  verification:: Unit test asserts that on a synthetic dense-graph fixture (resolution rate ≥ 0.80) the new code path is NOT taken; query plan matches the pre-EFF-0038 path.
- **Threshold + fallback signal are visible in the JSON output**: The report's JSON output gains a per-row `fallback bool` field (true when the row was surfaced via the fallback path) so downstream tooling (assessor skill, dashboards) can distinguish "scored from resolved edges" from "scored from raw refs".
  verification:: JSON contract: each row in `HotspotPackages`, `HotspotFiles`, and `DocCoverage` carries a `fallback` field. When the resolution rate is above threshold, all rows have `fallback=false`. When below, the fallback-derived rows have `fallback=true`.
- **Default threshold value documented**: The threshold ships at 0.60 (chosen empirically — wp-tst PHP at 0.41 needs fallback; TS at 0.0036 needs fallback; rhizome Go and TS at near-1.0 do not). Threshold is captured as a constant in the analytics code with a `// WHY:` comment citing wp-tst measurement, and surfaced in `docs/reference/guides/php-indexer-coverage.md`.
  verification:: `grep` finds the constant + rationale comment; the guide doc mentions the default.

---

### US2 - `--lang` filter on hotspots + doc_coverage

- id:: ^SPEC-0073-US2
- summary:: Both reports accept a `--lang <code>` flag (repeatable, e.g. `--lang php --lang ts`) that constrains output to the named language(s). When set, the resolution-rate auto-fallback decision still applies per-language; the filter only changes which rows are returned, not which scoring path runs.
- status:: satisfied

#### Acceptance Criteria

- **`--lang` flag accepted by both reports**: `rzm code hotspots --lang php` and `rzm code doc-coverage --lang php` are valid invocations and return only PHP rows.
  verification:: `rzm code hotspots --help` lists `--lang`; `rzm code doc-coverage --help` lists `--lang`; both flags are repeatable.
- **`--lang` is honored in the JSON contract**: The report's options structs (`DocCoverageOptions`, new `HotspotPackagesOptions` / `HotspotFilesOptions`) gain a `Langs []string` field. Empty means "all languages" (current behavior); non-empty filters via SQL `WHERE lang IN (...)`.
  verification:: Unit test asserts the filter applies at the query level (not post-filter in Go), so paging + limits work correctly under the filter.
- **Filter composes with auto-fallback**: When `--lang php` is supplied on wp-tst (PHP at 0.41 resolution rate), the result set includes ONLY PHP rows AND the fallback path runs (so the result set is populated rather than empty).
  verification:: Integration assertion: filtered output is non-empty AND `fallback=true` on rows.

---

### US3 - Documentation + assessor skill update

- id:: ^SPEC-0073-US3
- summary:: The new fallback behavior + `--lang` flag are documented in the durable PHP coverage reference and surfaced to the `legacy-codebase-assessor` skill so engagement starts pick up the new capability.
- status:: satisfied

#### Acceptance Criteria

- **`docs/reference/guides/php-indexer-coverage.md` updated**: The Known Gaps table loses the row about "hotspots + doc_coverage reports return JS-only on sparse-PHP corpora" — that row moves to a new "Closed by EFF-0038" entry under "Recent improvements" or similar. The Operational notes section describes the threshold (0.60), the fallback signal (intel_symbol_refs-based), and how to use `--lang` for explicit selection.
  verification:: `grep` finds the threshold value + `--lang` flag description.
- **`legacy-codebase-assessor` SKILL.md (template at `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/SKILL.md`) updated**: Step that runs `hotspots` and `doc_coverage` notes that PHP-heavy corpora now surface in those reports thanks to the auto-fallback, and mentions `--lang` as an explicit-selection escape hatch. Edit applied only to the template — `.codex/`, `.claude/`, `.agents/` copies are regenerated by `rzm init`.
  verification:: The SKILL.md template references SPEC-0073 + the new `--lang` flag.
- **AGENTS.md Recent Changes entry**: Brief note prepended summarizing EFF-0038's deliverables + the client engagement-readiness framing.
  verification:: The entry exists and links to SPEC-0073 + EFF-0038.

## Requirements

- The fallback decision is computed per-language at query time from the same data exposed by `rzm code stats` (`callsResolved / callsExtracted`). No new schema columns.
- The fallback signal MUST be reachable via existing tables (`intel_symbol_refs` + `intel_symbol_ref_files` + `intel_symbol_ref_targets`) — no new tables introduced.
- All new behavior MUST land with unit tests in `pkg/anchors/sqlite/analytics_test.go` (or the appropriate test file) using a synthetic-DB fixture pattern. Integration tests where they add value.
- The JSON output contract gains the `fallback` field as a non-breaking addition (downstream consumers that ignore the field continue to work).
- The `--lang` flag MUST be repeatable (`--lang php --lang ts`) per Cobra convention.
- Existing rankings for languages above threshold MUST NOT shift due to this change. A regression test confirms this on a dense-call-graph fixture.

## Open Questions

- **Should `complexity` also get a `--lang` filter?** Symmetry argues yes (cheap to add). Out of immediate scope (complexity already works on sparse graphs), but worth deciding during plan.
- **Threshold tunability via CLI flag?** A `--fallback-threshold 0.5` flag would let users override the default per-invocation. Probably yes — small surface, increases usefulness in edge cases.
- **Fallback signal shape**: pure import-fan-in (file-level "who imports this file") vs. symbol-ref counts ("how many refs point at this symbol regardless of resolution") vs. a blend. The plan should decide based on what surfaces meaningful PHP rankings on wp-tst.

## Documentation Plan

- **New** acceptance: `docs/reference/guides/php-indexer-coverage.md` Known Gaps table loses the hotspots/doc_coverage row; gains a "Closed by EFF-0038" mention under Recent improvements (or analogous section)
- **Update** `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/SKILL.md` (template — generated copies regenerate via `rzm init`)
- **Update** `AGENTS.md::Recent Changes` with EFF-0038 summary
- **Update** `cmd/code.go` flag definitions for `--lang` + `--fallback-threshold` (if adopted)
- **New** unit + integration tests in `pkg/anchors/sqlite/analytics_test.go` (or the right test file) covering: auto-fallback fires below threshold; existing path runs above threshold; `--lang` filter at SQL level; `fallback` field shape in JSON
- **Update** `docs/reference/coverage-catalogs/php-coverage-2026-06-12.md` if the catalog notes the report-blind-spot anywhere
