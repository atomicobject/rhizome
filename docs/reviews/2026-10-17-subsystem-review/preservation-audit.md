---
summary: Portable evidence for the DTO import-pruning incident, repairs, forensic dispositions and unavailable original baselines.
---

# Preservation audit appendix

This appendix records the import-pruning incident and its follow-up audit against baseline `5251923f`. The faulty helper removed 31 literal-return statements and one quoted GraphQL fixture line. The owners restored those statements and the fixture line. Within the available baselines, the subsequent quoted-line census left no unresolved proven or plausible accidental deletion among its enumerated candidates. This bounded result does not establish exact preservation of the whole refactor.

Historical source paths and declaration names below remain literal, even when a later approved extraction removed or renamed them. Links point to current substantive files. The [delivery record](delivery-evidence.md) owns broader implementation and gate status.

## Failure and repair method

The original regular-expression helper mistook standalone quoted lines, including nonempty literal returns, for import entries. It also guessed imported package names from path basenames and confused local selector bases with package references. The helper was retired. Its replacement uses Go AST import-spec spans, imported package clauses and unresolved selector identifiers, with explicit checks for a shadowed local `sql` and a preserved literal return. It edits import spans rather than deleting matching lines throughout a file.

Recorded import corrections included restoring `doublestar` in `anchor_code_files.go` and `anchor_match.go`, restoring `sqlite3` in `ontology_readiness_test.go`, and removing an unused `database/sql` import from `note_path_search.go`, where `sql` names a local builder. Those corrections are distinct from the restored return statements.

## Exact 31-return repair inventory

The repair record names nine functions in six files. Counts refer to removed statements, including repeated returns of the same value. The inventory records the repair checkpoint; later deliberate consolidation may replace a formerly repaired declaration.

| Historical file | Function | Restored literal values | Statements |
| --- | --- | --- | ---: |
| `pkg/store/ontology_field_predicates.go` | `ontologyFieldValueColumn` | `value_bool`, `value_int`, `value_real`, `value_date`, `value_datetime`, `value_norm` | 6 |
| `pkg/ontology/query/workspace_source_links.go` | `workspaceSourceLinkKind` | `mdlink`, `wikilink` | 2 |
| `pkg/ontology/query/workspace_source_links.go` | `workspaceSourceLinkTargetKind` | `attachment`, `file` | 2 |
| `pkg/ontology/pushdown/scalars.go` | `PredicateValueKind` | `bool`, `int`, `real`, `date`, `datetime`, `string` | 6 |
| `pkg/ontology/idalloc/allocator.go` | `ErrorCode` | `type_not_found`, `no_preferred_identifier`, `unsupported_for_type`, `invalid_input`, `identifier_exhausted`, `runtime_unavailable`, `internal_error` | 7 |
| `pkg/ontology/index.go` | `ambientRelationName` | `related` in three branches | 3 |
| `pkg/app/web/ontology.go` | `pathKind` | `file` | 1 |
| `pkg/app/web/ontology.go` | `fragmentTypeFromResolvedTarget` | `block`, `heading` | 2 |
| `pkg/app/web/ontology.go` | `slugifyHeading` | `section` in two branches | 2 |
| Total | 9 functions, 6 files | | 31 |

Current [field predicate selection](../../../pkg/store/ontology_field_predicates.go), [workspace link classification](../../../pkg/ontology/query/workspace_source_links.go), [scalar kind selection](../../../pkg/ontology/pushdown/scalars.go), [allocator errors](../../../pkg/ontology/idalloc/allocator.go), [ambient relation naming](../../../pkg/ontology/index.go) and [web path/fragment classification](../../../pkg/app/web/ontology.go) retain those returns. The old web `slugifyHeading` duplicate was deliberately removed after its two returns were repaired. [HeadingSlug](../../../pkg/markdownsyntax/heading_slug.go) and its callers now own the empty-heading behavior; an absent old function is not a remaining return deletion.

## Restored GraphQL fixture line

The affected test is `TestExecute_PublicConnectionBatchSearchAndValidationRoots`. Its historical location was `pkg/ontology/query/query_test.go:4517`. The missing line was exactly four spaces, `"notes/missing.md"`, and a newline. It belongs to the requested-ref list:

```graphql
  batch: nodes(refs: [
    "notes/projects/roadmap-refresh.md",
    "notes/missing.md"
  ]) {
```

The query owner restored the line in [query_search_roots_test.go](../../../pkg/ontology/query/query_search_roots_test.go). The complete current Go raw-string token, including its backtick delimiters, is byte-identical to the recorded HEAD token. Its SHA-256 is:

```text
ee3ff283b7f5ad80353549381f391096ab9212f35858f25781e085a5af77f32c
```

Removing only the exact missing line from that token reproduces the damaged token and this SHA-256:

```text
e2c137f6fa17893ebe2a4108d84832fdd85cce971d3ce43d55e62c84706669c9
```

Three real captured intermediate snapshots contain that damaged token, each at literal line 4515. The forensic comparison established exact equality to the original token minus the one line, rather than inferring damage from a later failing assertion:

| Captured snapshot | Recorded source |
| --- | --- |
| Query test split, `rhizome-query-test-structure-before-1792265371093153000` | `query_test.go:4515` |
| Ontology extraction, `rhizome-ontology-structure-before-20261017T1` | `pkg/ontology/query/query_test.go:4515` |
| Adapter split, `rhizome-adapter-size-before-20261017T191802199924Z` | `pkg/ontology/query/query_test.go:4515` |

This documentation pass rechecked the current token and the one-line subtraction against both recorded hashes. The query owner retained the original literal assertions and reported a passing full query package after repair. The forensic and documentation passes did not run product tests or make this source repair.

## Quoted-line forensic census

The follow-up audit reconstructed the retired discovery/deletion expressions and examined matches outside actual AST import declarations. It covered raw literals, quoted continuation lines, return statements and other code in the 123 initial DTO paths, documented pruning/splitting waves, non-vendor HEAD Go sources and captured intermediate snapshots. Declaration names and known package/file moves located current owners. Only explicitly authorized package-path and DTO-symbol substitutions were normalized; literal values remained significant.

The recorded census contains 6,972 baseline/move snapshots and 3,468 current Go files. It excluded 45,862 actual-import matches and evaluated 1,336 non-import spans, grouped into 31 raw-literal groups and 492 code groups. All 31 raw-literal groups match a current owning literal after only the stated substitutions. Baseline parsing reported zero errors. An executable self-check reproduced deletion of a quoted fixture line while retaining a used actual import.

The first comparison produced 16 code-difference rows, dispositioned below. A whole declaration's removal or rename cannot be caused by a pattern that removes only standalone quoted lines. That distinction identifies deliberate ownership changes without proving every aspect of those broader refactors correct.

## Original 16 candidate dispositions

| # | Historical source and declaration | Disposition and current evidence |
| --- | --- | --- |
| 1 | `pkg/app/cli/init/agentic_engineering_experience_test.go`: `agenticEngineeringEffortNote` | The native transaction owner confirmed an intentional TEST-4 change to a synthetic Status sentence; no assertion consumed Status. The owner then restored the exact HEAD sentence to remove unnecessary fixture churn. This was separate from the accidental GraphQL deletion. [Current source](../../../pkg/app/cli/init/agentic_engineering_experience_test.go). |
| 2 | `pkg/app/cli/validation_product.go`: `shellQuoteValidationProductArg` | The old wrapper and CLI-side renderer were removed. Shared POSIX quoting retains the exact empty-argument return. No obsolete wrapper was recreated. [Current source](../../../pkg/shellquote/quote.go). |
| 3 | `pkg/app/semanticruntime/runtime.go`: `*Runtime.intentMetricPhase` | The old application runtime and intent-exemplar lifecycle were removed during semantic ownership extraction. The missing method signature is an ownership/feature removal, not a dropped quoted line. [Current source](../../../pkg/embedindex/runtime.go). |
| 4 | `pkg/app/unifiedsearch/run.go`: `roleForResult` | The old function and answer-input assembly call were removed. The current application classifier owns supported roles; the former declaration was not restored. [Current source](../../../pkg/app/unifiedsearch/application/assessment.go). |
| 5 | `pkg/app/web/ontology.go`: `slugifyHeading` | The duplicate function was removed after the two repaired web returns. Shared heading slug and section identity policy now own empty-heading behavior. [Current source](../../../pkg/markdownsyntax/heading_slug.go). |
| 6 | `pkg/ontology/field_capabilities.go`: `indexedScalarValueKind` | The old helper was replaced by schema.ScalarKindForType in FieldQueryCapabilityForField. The bool/int/real/date/datetime/url/id/string kind values remain. [Current source](../../../pkg/ontology/schema/scalar_values.go). |
| 7 | `pkg/ontology/query/execute.go`: `slugifySectionHeading` | The duplicate query function was removed. Section-neighbor callers use the shared heading slug with explicit section fallback where required. [Current source](../../../pkg/ontology/query/section_neighbors.go). |
| 8 | `pkg/ontology/sections.go`: `slugifyHeading` | The duplicate ontology function was removed. Shared heading slug and section identity policy own empty-heading behavior. This declaration removal is distinct from the repaired web returns in the inventory. [Current source](../../../pkg/markdownsyntax/heading_slug.go). |
| 9 | `pkg/search/retrieval/timings.go`: `timingStatus` | The retrieval timing duplicate was removed. Current callers use shared TimingStatus with the same ok/timeout/canceled/error strings. [Current source](../../../pkg/search/timings.go). |
| 10 | `pkg/search/semantic/embed_pipeline_core.go`: `run` | The moved provider-slot closure returns typed MetricName constants. The metric registry retains the original provider.slots.* strings. [Current source](../../../pkg/indexingperf/metrics.go). |
| 11 | `pkg/search/semantic/search.go`: `displayOntologyNodeType` | The complete function assigning the removed Result.NodeType shadow field was deleted. Current semantic identity uses the canonical source/node reference; fallback labels remain in ontology chunk metadata. [Current source](../../../pkg/search/source_ref.go). |
| 12 | `pkg/search/semantic/timings.go`: `timingStatus` | The semantic timing duplicate was removed. Current callers use the same shared TimingStatus strings as candidate 9. [Current source](../../../pkg/search/timings.go). |
| 13 | `pkg/search/service.go`: `statusFromContext` | The old context wrapper was removed in timing consolidation. TimingStatus and searchResultStatus retain the same status vocabulary. This and candidate 16 record one logical candidate with different snapshot provenance. [Current source](../../../pkg/search/diagnostic_metrics.go). |
| 14 | `pkg/search/service.go`: `statusFromErr` | The duplicate error-status wrapper was removed. Shared TimingStatus owns its unchanged status branches. [Current source](../../../pkg/search/timings.go). |
| 15 | `pkg/validate/next_actions.go`: `shellQuoteCommandArg` | The caller now uses shared POSIX quoting, including the exact empty-argument value and escaping behavior. The duplicate wrapper was removed. [Current source](../../../pkg/validate/next_actions.go). |
| 16 | `pkg/search/service.go`: `statusFromContext` | This is the second provenance row for candidate 13, not another deleted function or another accident. The current shared status owners retain its values. [Current source](../../../pkg/search/diagnostic_metrics.go). |

### Later receiver rename

A later extraction renamed historical `skillOverlayError.Error` in `pkg/app/cli/init/skill_overlay.go` to `skilloverlay.Error.Error` in [model.go](../../../pkg/skilloverlay/model.go). Its exact `"skill overlay error"` return remains. HEAD and a captured intermediate snapshot produced two comparison rows for this one rename. After candidate 1's synthetic sentence was restored, the live comparison had 17 code differences rather than the original 16. All were dispositioned; the count does not mean 17 accidental deletions.

## Declaration audit and baseline limits

The earlier strict DTO audit inventoried 123 original paths. After expected DTO substitutions and ignoring imports, comments and optional commas, 1,416 declarations matched. Another 749 discrepancies, including 159 declarations absent from their old package, remained for concurrent extraction review. The matched-function token differences contained no remaining literal-return deletion. These numbers are checkpoint evidence; the quoted-line census does not certify every unrelated difference.

Only one actual pre-rewrite root-store snapshot, at schema version 76, and 86 HEAD versions were available. HEAD predates other lanes' concurrent changes. A snapshot captured after the faulty rewrite can prove a subsequent move or the damaged fixture, but cannot reconstruct an earlier missing version. The following 36 files were already new and have no saved pre-DTO original. Names are the literal gap inventory, including any paths later moved or deleted:

- `pkg/store/ontology_readiness_snapshot.go`.
- `pkg/store/ontology_readiness.go`.
- `pkg/store/ontology_readiness_test.go`.
- `pkg/store/ontology_readiness_outputs_test.go`.
- `pkg/store/ontology_readiness_outputs.go`.
- `pkg/ontology/sync_completeness.go`.
- `pkg/ontology/query/roots_identity.go`.
- `pkg/ontology/query/roots_embedded.go`.
- `pkg/ontology/query/catalog_records.go`.
- `pkg/ontology/catalog_readiness.go`.
- `pkg/ontology/sync_catalog.go`.
- `pkg/app/indexwriter/handler_safety_test.go`.
- `pkg/app/indexwriter/write_queue_ontology_test.go`.
- `pkg/ontology/noderead/indexed_fields.go`.
- `pkg/ontology/noderead/neighborhood_inbound_test.go`.
- `pkg/ontology/noderead/scope_type_instances_test.go`.
- `pkg/ontology/noderead/hydrate_embedded.go`.
- `pkg/ontology/noderead/interface_overlay_paging_test.go`.
- `pkg/ontology/noderead/resolve_paths.go`.
- `pkg/ontology/noderead/scope_overlay_test.go`.
- `pkg/ontology/noderead/interface_instances.go`.
- `pkg/ontology/noderead/type_instances_overlay_links_test.go`.
- `pkg/ontology/noderead/catalog_items.go`.
- `pkg/ontology/noderead/scope_fixture_test.go`.
- `pkg/ontology/noderead/field_values.go`.
- `pkg/ontology/noderead/graph_contract_test.go`.
- `pkg/ontology/noderead/service.go`.
- `pkg/ontology/noderead/resolve_helpers.go`.
- `pkg/ontology/noderead/catalog_records.go`.
- `pkg/ontology/noderead/type_instances_overlay.go`.
- `pkg/ontology/scalar_values.go`.
- `pkg/ontology/sync_materialization_test.go`.
- `pkg/ontology/scalar_values_test.go`.
- `pkg/ontology/pushdown/evaluate.go`.
- `pkg/ontology/pushdown/equivalence_test.go`.
- `pkg/app/cli/init/spec_driven_schema_test.go`.

Inspection of the no-original files checked string-return branch completeness, including provider hashes, catalog keys, comparable scalar rendering, target unwrapping and overlay list keys. The recorded AST inspection found no empty if blocks; seven empty switch cases were checked as intentional allowed-kind/direction branches. Store tests and focused statistics/trigger tests passed at their checkpoints. Such inspections and tests support current behavior; they do not establish exact equality to unavailable versions.

No missing original was invented and no blanket source restore was used. There is no exact whole-refactor preservation claim, and no claim that all concurrent discrepancies received an independent review. The 36-file provenance gap remains after source delivery and final test acceptance.

## Evidence provenance and execution limits

The primary incident records are `rhizome-dto-return-repairs.md` and `rhizome-rawstring-preservation-audit.md` with its JSON counterpart, captured under `/tmp` during this work. The JSON records original/current contexts, candidate dispositions, the two literal hashes, the three damaged snapshot witnesses and the 36 baseline gaps. This appendix carries the material inventory, decisions, proof values and limits so PR review does not depend on access to those temporary files.

Supplementary temporary artifacts include the AST/import-span extractor, exact-pattern census/self-check, baseline manifest and comparison JSON. Their presence is not a substitute for missing pre-DTO sources. The forensic follow-up reported no source repair of its own, no unresolved ambiguity among enumerated candidates and no additional proven accidental deletion. The source owners performed the repairs and focused tests. This appendix pass changed documentation only and ran no product tests, builds or gates. Broader gate and review results remain in the delivery record.
