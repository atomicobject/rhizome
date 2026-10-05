---
summary: "Ontology materialization persists has_issues/type_ambiguous next to each assessment row so whole-vault inventory reads stop decoding assessment JSON, with paired 1k/10k measurements."
reference-kind: guide
---

# Materialized assessment flags

`noderead.Scope.ensureNoteState` read and JSON-decoded every `ontology_note_assessments` row for the whole vault on each `ontologySummary` / `ontologyAtlas` / `TypeInstances` request, purely to derive two booleans per note: does the note have validation issues, and is its type ambiguous. At 1,000 notes that read and decode was roughly half of both handlers' CPU and 40% of their allocated bytes, and it grew linearly with vault size.

The ontology materializer now writes those two booleans into the assessment row, and the inventory path reads only them.

## Contract

- `ontology.FlagsForAssessment` is the single place either flag is derived: `HasIssues` is `ontology.AssessmentHasIssues` (a note-level, field-level, or relation-level issue), `TypeAmbiguous` is more than one candidate type. `setAssessmentRow` serializes the assessment and refreshes both flags in one call, replacing the old `mustAssessmentJSON`, so the columns and the JSON cannot disagree — including at the incremental finaliser in `sync.go`, which appends inverse-relation issues after the row was first serialized.
- Intel schema v64 adds `has_issues` and `type_ambiguous` to `ontology_note_assessments` (`INTEGER NOT NULL DEFAULT 0 CHECK (col IN (0,1))`, idempotent `ALTER TABLE`). The migration does not populate them.
- `OntologyMaterializationVersion` moves 3 → 4. That bump, not the migration, populates the columns: every existing index performs one full ontology rebuild. Between the schema migration and that rebuild the columns hold the migration's default 0, so `Scope.inventoryFlagsLocked` reads `ontology_schema_state` once per scope and trusts the columns only when the state is ready at the current `OntologyMaterializationVersion`; otherwise it decodes `assessment_json` through `FlagsForAssessment` for the whole vault, exactly the pre-change path. Once the rebuild lands the fast path is unchanged. The version check is the only fallback: a missing column or a failed rebuild is not papered over.
- `Store.OntologyAssessmentFlags` is one full-table scan of the two columns and the path — no `IN` chunking, no `assessment_json` in the projection. Measured at this row width, one scan beats a 900-parameter chunked lookup.
- Detail reads are unchanged: `Scope.AssessmentsByPaths`, `ensurePathStateLocked` (a requested-path read), node workspace validation, fix ops, identifier validation, and search eligibility all still decode the JSON.
- `Scope.issueCount` is deleted. It only ever held 0 or 1 per note; `issueCountForItems` counts distinct note paths flagged in `issueByPath`, which is the same number.
- The duplicated `assessmentHasIssues` copies in `pkg/ontology/noderead` and `pkg/app/web` are deleted in favour of the exported `ontology.AssessmentHasIssues`.

### Documented relaxation

A corrupt `assessment_json` row used to fail `ontologySummary` because the inventory path decoded it. It no longer does, so the summary succeeds and reports that note's materialized flags. Every detail read still fails on that row, including `ontologyType("__all__")`. `TestOntologySummary_CorruptAssessmentRowStaysInventoryReadable` pins both halves.

## Proof

- `TestFlagsForAssessment`, `TestSetAssessmentRowKeepsFlagsInSyncWithJSON` (`pkg/ontology`) — the pure derivation and the row writer.
- `TestAssessmentRowsCarryMaterializedFlags` (`pkg/ontology`) — real vault through `EnsureFreshRuntimeWithStore` and then `SyncPaths`; every persisted row's columns equal `FlagsForAssessment(AssessmentFromJSON(row))`, including the note whose inverse-relation issue is appended in the incremental finaliser, plus a missing-required-field note and an ambiguous note.
- `TestEnsureIndexedRebuildsWhenMaterializationVersionIsStale` (`pkg/ontology`) — a v3 database with cleared flags rebuilds on `EnsureIndexed` and comes back with the flag set.
- `TestOpen_UpgradesV63WithOntologyAssessmentFlags`, `TestOntologyAssessmentFlagsScansEveryRow` (`pkg/anchors/sqlite`) — the migration adds both columns at 0 without touching `ontology_schema_state`, and the scan reader round-trips.
- `TestScopeInventoryReadsUseMaterializedFlags` (`pkg/ontology/noderead`) — `TypeInstances` issues exactly one `OntologyAssessmentFlags` call and zero `OntologyAssessmentsByPaths` calls, reports `HasIssues` and `IssueCount` from the columns, lists `__issues__` correctly, and still lazily decodes on `AssessmentsByPaths`.
- `TestScopeInventoryFallsBackToDecodeUntilMaterializationIsCurrent` (`pkg/ontology/noderead`) — an index shaped as the v64 migration leaves it (flags at 0, state one materialization version behind) reports issues and ambiguity from `assessment_json` with one `OntologyAssessmentsByPaths` call and zero flag scans; after `EnsureIndexed` rebuilds it, a fresh scope uses one flag scan and zero decodes.
- `TestOntologyReadHandlers_FlagParity` (`pkg/app/web`) — over a populated 200-note fixture every row's columns match the decoded JSON, and summary `IssueNotes` / `AmbiguousNotes` and `__issues__` count are unchanged.

## Paired measurements

Fixture: `pkg/app/web/ontology_read_bench_test.go`, `BenchmarkOntologyReadHandlers`. Before = `origin/codex/engine-follow-up` (`bf875585`), after = this change, same benchmark file compiled into both binaries, run alternately five times at `-benchtime=300ms`, `GOMAXPROCS=4`, on a shared host. Medians. Timings are indicative; allocation counts are deterministic.

| Handler | 1k before | 1k after | 1k allocs before | 1k allocs after |
| --- | ---: | ---: | ---: | ---: |
| `ontologyAtlas` | 6.29 ms | 3.11 ms (−51%) | 71,860 | 39,382 (−45%) |
| `ontologySummary` | 6.92 ms | 3.77 ms (−46%) | 73,835 | 41,344 (−44%) |
| `ontologyType("Decision")` | 6.64 ms | 5.11 ms (−23%) | 73,444 | 55,841 (−24%) |
| `ontologyType("__all__")` | 7.73 ms | 8.22 ms (+6%) | 77,125 | 81,179 (+5%) |

| Handler | 10k before | 10k after | 10k allocs before | 10k allocs after |
| --- | ---: | ---: | ---: | ---: |
| `ontologyAtlas` | 66.4 ms | 33.6 ms (−49%) | 715,741 | 391,227 (−45%) |
| `ontologySummary` | 73.7 ms | 39.7 ms (−46%) | 734,167 | 409,583 (−44%) |
| `ontologyType("Decision")` | 69.6 ms | 52.2 ms (−25%) | 732,011 | 555,830 (−24%) |
| `ontologyType("__all__")` | 80.3 ms | 83.8 ms (+4%) | 768,991 | 809,192 (+5%) |

Bytes/op fall by 30–36% on summary and atlas at both sizes.

`ontologyType("__all__")` is the one regression, and it is structural rather than accidental: that page decodes every assessment anyway to render per-note issue detail. It used to get that decode for free from `ensureNoteState`; now it pays the flag scan and then decodes the same rows itself, roughly 5% more work. Every other inventory read stops decoding entirely. `ontologyType("Decision")` improves because it now decodes only its own page instead of the whole vault.

## Follow-on

With the assessment decode gone, the largest remaining cost inside `ensureNoteState` is `Store.OntologyTypesByPaths` — a 900-parameter `IN`-chunked lookup of one narrow row per note, measured at 24% of atlas CPU at 1k on the pre-change branch. The same argument applies to it: a single-scan `OntologyTypes(ctx)` inventory reader should beat the chunked lookup at this row width. Not claimed here.
