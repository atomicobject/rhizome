---
summary: "Web ontology summary/type handlers read the vault inventory through the noderead Scope instead of re-reading and re-decoding it, with paired 1k/10k measurements."
reference-kind: guide
---

# Summary/type inventory reuse

`GET /api/v1/ontology/summary` and `GET /api/v1/ontology/types/{name}` used to pay twice for the same full-vault inventory. `ontologySummary` read `CurrentNoteMetadataRows`, `OntologyTypesByPaths`, and `OntologyAssessmentsByPaths` directly, decoding every assessment JSON row, and then created a `noderead.Scope` whose `ensureNoteState` read and decoded the same rows again. `ontologyType` repeated the assessment decode for every path on the page after the scope had already decoded and memoized it.

Both handlers now read note state through the `noderead.Scope` they already hold: `TypeInstances(TypeScopeAll)` supplies the inventory and total, `Scope.TypesByPaths` the typed count, and `Scope.AssessmentsByPaths` the decoded assessments. `Scope.AssessmentsByPaths` also serves as the fix-target loader, so assessments outside the requested slice are still hydrated into the counted population. `pkg/app/web/search_notes.go` issue filtering uses a request-local scope over the search hits only. The store-reading helpers `ontologyIssueDetails` and `ontologyIssueCounts` are deleted, including the dead `ontology.Service.Assessment` fallback and the "indexed assessment read failed → zero counts" branch.

## Contract

Counts and issue totals are unchanged: `TotalNotes` is the number of current metadata rows (`TypeInstances(TypeScopeAll).Count` enumerates exactly those rows), `TypedNotes` the rows with a type row, `AmbiguousNotes` the assessments with more than one candidate type, `IssueNotes` the assessments with issues, and per-type `Count`/`IssueCount`/`StartingNotes` come from `TypeInstances` as before. Sort orders, the type-list item fields (`HasIssues`, `Issues`, `RelationCount`, `Tags`, `IdentityStatus`), and schema-less degradation to indexed metadata are untouched. One scope per request; no cross-request cache; no noderead, store, or materialization change.

Error behaviour is unchanged in practice. The old helper skipped rows that failed to decode and turned a store read error into zero counts, but the scope created a few lines later decoded the same rows and returned the error, so the request already failed. `TestOntologySummary_CorruptAssessmentRowIsAnError` passes before and after.

## Fixture

`pkg/app/web/ontology_read_bench_test.go`, `BenchmarkOntologyReadHandlers`: a generated vault of 1,000 and 10,000 notes across four note types (`Decision` 40%, `Project` 20%, `Person` 20%, `Meeting` 20%), one interface `SummaryDoc` with two implementors, and a 10% issue rate produced by omitting the required `summary` field on every tenth `Decision`. Indexed with `ontology.EnsureFreshRuntimeWithStore` and served by `NewServer` with `Runtime{IntelStore: store}`. Handler functions are called directly; HTTP and JSON encoding are outside timing. Each sub-benchmark asserts the expected counts before timing and reports allocations. `atlas` is the unchanged control.

## Paired measurements, 1,000 notes

Before = branch base `b3832c28`, after = this change, same benchmark file in both binaries, run alternately five times at `-benchtime=300ms`, `GOMAXPROCS=4`, on a shared host. Medians. Timings are indicative; allocation counts are deterministic.

| Handler | before time | after time | before B/op | after B/op | before allocs/op | after allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `ontologySummary` | 12.25 ms | 7.04 ms (−43%) | 11.93 MB | 8.07 MB (−32%) | 141,346 | 73,834 (−48%) |
| `ontologyAtlas` (control) | 6.41 ms | 6.33 ms (−1%) | 6.49 MB | 6.49 MB (0%) | 71,860 | 71,860 (0%) |
| `ontologyType("Decision")` | 7.94 ms | 6.72 ms (−15%) | 8.35 MB | 7.47 MB (−11%) | 88,283 | 73,443 (−17%) |
| `ontologyType("__all__")` | 10.56 ms | 7.77 ms (−26%) | 12.43 MB | 10.23 MB (−18%) | 113,601 | 77,124 (−32%) |

Raw samples (ms): summary before 12.56/12.25/12.43/12.16/12.25, after 6.96/7.07/7.05/7.04/7.01; atlas before 6.41/6.25/6.60/6.44/6.32, after 6.62/6.33/6.33/6.33/6.35.

## 10,000 notes

Single `-benchtime=3x` run per side, same binaries.

| Handler | before time | after time | before allocs/op | after allocs/op |
| --- | ---: | ---: | ---: | ---: |
| `ontologySummary` | 128.0 ms | 72.4 ms (−43%) | 1,410,418 | 734,164 (−48%) |
| `ontologyAtlas` (control) | 64.8 ms | 65.1 ms (+1%) | 715,741 | 715,740 (0%) |
| `ontologyType("Decision")` | 82.4 ms | 69.7 ms (−15%) | 880,201 | 732,014 (−17%) |
| `ontologyType("__all__")` | 108.8 ms | 78.8 ms (−28%) | 1,133,428 | 768,990 (−32%) |

## Candidates measured and rejected

Schema/recipe reuse on this repo (4 SDL files, 60 KB; 121 candidate recipe files): `ontology.LoadSchema` 2.08 ms / 1.81 MB, `BuildExecutableSchema` 0.85 ms / 1.08 MB, `queryrecipe.LoadDefaultSources` 4.42 ms / 2.54 MB, `Validate` 1.56 ms, `Compile` one recipe 12 µs. The web server already caches schema and executable schema for the server lifetime; per-call loading happens only in MCP ontology tools and recipe views. 3–6 ms per call is below the bar, and a reuse policy needs an owner for file/schema freshness plus global duplicate-ID validation. Broad `find`/interface selectors cost 2.8 ms at 1k and 26.8 ms at 10k for `notes(find:"common", first:10)`; the remaining cost is the inventory scan and all-candidate type read that the find contract requires, which B14 already deferred.

## Follow-on

Roughly the remaining half of summary and atlas time was `Scope.ensureNoteState` decoding every assessment JSON purely to derive a has-issues flag — the atlas control shows that cost unchanged here, because this change removes only the duplicate. It is now removed by the store-owned `has_issues`/`type_ambiguous` columns in [materialized assessment flags](engine-assessment-flags.md), which carries the migration, the `OntologyMaterializationVersion` bump, and the follow-on paired measurements.
