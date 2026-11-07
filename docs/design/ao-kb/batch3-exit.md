# SPEC-0133 Batch 3 delivery

Lane Y2, branch `t3/yaml-json-record-surfaces`. Base: `e278153a1`. The final handoff reports the resulting head SHA. The coordinator-authorized merge includes `t3/evaluate-ao-prompt-toolkit` at `a6d68a2c6b3ef528c46cae49ac3f5e0f405171f5`; merge commit `952b07332` preserves both lanes' materialization changes at version **21**. Database schema remains **80**.

## Completed steps

| Step | Commit | Result |
| --- | --- | --- |
| 1 | `2287d36af` | Built-in YAML/YML and JSON providers; explicit-extension discovery; broad globs and classic discovery do not activate structured notes; no mutation capabilities. |
| 2 | `5d08bdb8a` | Thin adapter over Lane Y's parser; one owner and value per occurrence, RFC 6901 addresses, native kinds/order, source-located diagnostics, absent unproven ranges. |
| 3 | `0e806a9d5` | Mixed-format catalog, summary/rich queries, content, children, workspace, references and semantic chunks; S2, S3 and S4 fixes. |
| 4 | `04ca30e24` | Native standard reader, fields/child collections/raw source, structured paths and copy links, locator stability/staleness, ownership refusals, standard-view and search coverage. |
| 5 | `d9016d3e8` | Operation-local sealed source/projection reuse, existing metadata/writer publication order, cross-format target refresh, materialization invalidation and convergence tests. |
| 6 | Final delivery commit | Checked AO rows/duplicate rows/edges oracle, real-provider operation counts, integrated verification and visual evidence. |

All changes are local commits. No push, PR, deployment, desktop installation, toolkit modification, or live-vault mutation was performed. Lane K and M2 owned sources match the merged coordinator branch byte-for-byte.

## Observable behavior and regressions

Structured records preserve nested objects/lists, missing/null/empty values, list order, repeated references, scalar rows, integer keys, same-title identities, composite keys, mapping groups, positional children, membership and account-first numbering. Unbound values remain readable in native content and contribute to the record's semantic search evidence. Standard views retain distinct physical refs for same-title records. Typed references from YAML to exact Markdown document paths and authored Markdown body links to YAML records are covered.

The standard reader reuses existing identity, field and collection components. Native values bypass Markdown and heading projection. Source shows the original complete host bytes; record fragments select the intended value without caret anchors or fabricated ranges. YAML/JSON suffixes survive shared web routing. Copy link uses the ordinary record URL when a wikilink is unavailable. Read-only reasons remain visible while global editing is enabled.

| Review issue | Failing-first evidence | Result |
| --- | --- | --- |
| S2 | `TestRichRecordReferencesPreserveGlobalAmbiguity`; `/private/tmp/y2-coordinator-red.log` | Adding `content` preserves the globally indexed reference outcome, including ambiguity. |
| S3 | `TestPresentEmptyRecordReferencesMatchExists`; same red log | Authored `refs: []` persists a presence/value-kind row and matches `exists` in summary and rich queries. |
| S4 | `TestGlobalRecordChildGetsOwnChunkWithoutContainsField`; same red log | Every unvisited selected RECORD receives its own chunk and semantic parent, including global children without a parent's `@contains` field. Child evidence remains excluded from the parent's chunk. |

The green review regression run is `/private/tmp/y2-coordinator-green.log`. The reader E2E also exposed and reproduced incorrect `.md` suffixing, Markdown-only reader gating, and a copy-link action that accepted only wikilinks. Their shared fixes retain the existing Markdown routes.

## Convergence and operation counts

`TestStructuredFullAndIncrementalPublicationConverge` compares durable source fingerprints/status, catalog nodes, field values, edges/occurrences and diagnostics after modify, add, delete, keyed reorder, positional reorder, rename, malformed YAML/JSON, recovery, binding changes, provider version changes and restart. Every incremental state equals a fresh full index. `TestStructuredReferencesRefreshWhenMarkdownTargetsChange` separately proves that adding/deleting a Markdown evidence target repairs unchanged YAML record edges and converges with full publication. A materialization-version refresh replans all bodies through the existing catch-up owner while retaining reusable unchanged vectors.

Keyed links survive reorder; changing/removing the key makes the old locator unresolved. Duplicate keys preserve both source diagnostic addresses and refuse an arbitrary target. Positional locators reject every changed host hash, including insertion, reorder and unrelated whitespace. Ownership tests refuse scalar, locator, delete and file-transform requests before parsing valid or malformed YAML/JSON, do not invoke the transform callback, and preserve CRLF bytes exactly. Lane B's existing ownership transition tests remain part of the repository gates.

| Workload | Captures | Provider parses | Prepared resolver states |
| --- | ---: | ---: | ---: |
| Summary hydration, 3 real YAML records | 0 | 0 | 0 |
| Rich hydration and repeated projections, same 3 records | 1 | 1 | 1 |
| Summary/rich hydration, same real JSON fixture | 0 / 1 | 0 / 1 | 0 / 1 |
| One full publication operation per structured host | 1 | 1 | Existing prepared sync owner |

`TestStructuredRecordsShareOneCaptureParseAndResolver` asserts pointer identity for every record and absence of Markdown snapshots. `TestSourceScopeReusesCaptureAndDoesNotCrossOperations` proves the metadata-to-ontology handoff reuses the sealed capture, returns defensive copies, and refreshes the next operation. Adapter tests produce exactly **502** owners/values for 100 nested rows and **5,002** for 1,000 rows: one emitted value per source occurrence and linear traversal. These are deterministic operation/count assertions, not latency measurements. Logs: `/private/tmp/y2-final-counts-green.log`, `/private/tmp/y2-scope-green.log`, `/private/tmp/y2-step45-focused-final.log`.

## AO parity

The exact requested command passes:

```sh
AOKB_RUN_PROVIDER_PARITY=1 GOCACHE=$PWD/.gocache go test -mod=vendor -tags fts5 ./testdata/ao-kb-model -run TestProviderParity -v
```

M2's fixture compares **15 records across 10 kinds**, their complete normalized values and physical addresses, repeated references, qualifiers and indexed edges. Its AO expectation contains **14 resolved edge occurrences / 13 distinct edges**, plus one separately recorded unresolved edge. Account-first positions, selected-product isolation, containment and generated JSON all agree. The complete Go compiler fixture suite and all 10 Node compiler tests also pass. Logs: `/private/tmp/y2-m2-parity.log`, `/private/tmp/y2-m2-compiler-go.log`, `/private/tmp/y2-m2-compiler-node.log`.

`pkg/ontology/testdata/structured-ao/expected.json` adds a checked expectation from the pinned AO reader `e9dce951ab4cd6aa9ccc734df96590bde25ccde9` with PyYAML SafeLoader 6.0.3. It captures original synthetic YAML plus `rows`, `duplicate_rows`, and `edges`. The valid case preserves both repeated edge occurrences. Per foundation-review F7, an AO edge to a missing target is excluded from the comparison and requires Rhizome's `record_reference_unresolved`. The duplicate case proves the expected collision count and Rhizome's source diagnostics and refusal to select AO's last winner. The Python helper ran only under `/private/tmp`; CI consumes checked expectations and runs no Python AO helper. Lane Y's existing scalar oracle remains unchanged.

No provider-side or compiler-side mismatch remains in M2's parity harness.

## Verification

| Gate | Result | Evidence |
| --- | --- | --- |
| Focused FTS5 packages | Pass, including S2–S4, native read counts, AO oracle, locators, ownership and semantic search | `/private/tmp/y2-final-focused.log`; standard views also passed the dedicated `/private/tmp/y2-views.log` run |
| Focused `fts5 integration` Python-fixture ontology/edit/chunk tests | Pass | `/private/tmp/y2-focused-integration.log` |
| `make check-fast` | Pass | `/private/tmp/y2-integrated-fast.log`; final delivery rerun `/private/tmp/y2-final-fast.log` |
| `make check` | Pass | `/private/tmp/y2-integrated-check.log` |
| `make check-full` | Pass, including race-enabled units, all integration packages, mixed integration and benchmark contracts | `/private/tmp/y2-final-full.log` |
| Web lint, application/Node typechecks and unit tests | Pass; **245 files / 2,004 tests** | Included in the preceding gates |
| `RHIZOME_E2E_PORT=7921 npm run test:e2e` from `web/` | **130 passed**, including YAML records, Markdown editing/navigation/views and HTML | `/private/tmp/y2-final-e2e.log` |
| `make build` | Pass, final CLI, web and team API builds | `/private/tmp/y2-final-build.log` |
| M2 Node compiler / Go compiler + provider parity | Pass; 10 Node tests, generated SDL and provider oracle | Logs listed above |
| Impeccable detect for reader/CSS | No findings | `/private/tmp/y2-impeccable-detect.json` |
| Batch 1 golden files | Unchanged byte-for-byte | Empty diff against `e278153a1` for `pkg/ontology/testdata/structure-parity` |

No failing requested gate remains. Early failing implementation tests were fixed and followed by the green runs above. Initial sandbox-only socket/cache restrictions were superseded by successful authorized runs outside the sandbox. Live AO site builds and downstream deployment are outside this provider batch and were not run; AO reader parity uses the pinned checked fixtures rather than a claim of whole-toolkit compatibility.

### Screenshots

All six images were opened at original size and inspected. The YAML screenshots show the native value, properties, children, normal read mode and ownership reason. The Markdown comparison serves frontend assets from baseline `e278153a1` and the final frontend against the **same disposable current backend/vault**, isolating the presentation change. `cmp` confirms that both before/after PNG pairs are byte-for-byte identical.

| Reader | Light | Dark |
| --- | --- | --- |
| YAML record | [Light](batch3-screens/yaml-record-light.png) | [Dark](batch3-screens/yaml-record-dark.png) |
| Markdown before | [Light](batch3-screens/markdown-before-light.png) | [Dark](batch3-screens/markdown-before-dark.png) |
| Markdown after | [Light](batch3-screens/markdown-after-light.png) | [Dark](batch3-screens/markdown-after-dark.png) |

The disposable visual server used port **7922** and was stopped. All nine synthetic E2E fixture directories identified in this lane's logs, the AO helper/environment and the baseline frontend scratch directory were removed with `rm -rf`. Logs and the checked-in evidence remain. The generated `pkg/app/web/assets/dist/index.html` build artifact was restored.

## Contract notes and limits

- **Lane K:** Standard-reader navigation accepts YAML/YML/JSON refs. Use the existing native `source_value` payload and public record locator. Fields and collections honor ownership/capability reasons; the native reader does not use Markdown. `RECORD` types do not inherit authored `Note` fields.
- **Lane M2:** Built-in provider registration passes the compiled SDL oracle without fixture/compiler edits. Include the selected original host paths explicitly. Materialization version 21 includes both lanes' changes; database schema is 80. Keep the named generated-field exclusion and account/product merge diagnostics in the compiler's own contract.
- YAML/JSON remain read-only, with no structural edit planner or draft overlay. Explicit include patterns are required. HTML remains root-only.
- The pinned YAML profile rejects tags, anchors, aliases, multiple documents, unsupported numeric or temporal spellings, merge syntax, and duplicate or complex mapping keys. Source diagnostics identify the rejected host, which has no current partial typed model. Markdown frontmatter keeps its existing parser and scalar profile.
- Positional locators are revision-bound and expire after every host edit. They do not promise stable identity across insertions or reorder. Duplicate identities produce diagnostics rather than AO's convenience last-winner lookup.
- Cross-format evidence coverage uses authored Markdown body links to records and YAML `@references` to exact Markdown document paths. This delivery does not expand Markdown `@link` syntax to embedded RECORD targets.

## Files by area

The following appendix lists Lane Y2 files only; the authorized merge also imports the coordinator's K/M2 work.

<!-- lane-files -->

### Shared surface and fixture integration

- `cmd/projected_note_reader_test.go`
- `internal/testkit/recordfixture/fixture.go`
- `internal/testkit/vault.go`
- `pkg/app/indexing/ownership_transition_test_helpers_test.go`
- `pkg/app/noteownership/transition_regression_test.go`
- `pkg/app/views/structured_records_test.go`
- `pkg/app/web/html_viewer_test.go`
- `pkg/embedindex/CONTEXT.md`
- `pkg/embedindex/ontology_node_chunks.go`
- `pkg/embedindex/record_global_child_test.go`
- `pkg/embedindex/structured_search_test.go`

### Documentation and visual evidence

- `docs/design/ao-kb/batch3-exit.md`
- `docs/design/ao-kb/batch3-screens/markdown-after-dark.png`
- `docs/design/ao-kb/batch3-screens/markdown-after-light.png`
- `docs/design/ao-kb/batch3-screens/markdown-before-dark.png`
- `docs/design/ao-kb/batch3-screens/markdown-before-light.png`
- `docs/design/ao-kb/batch3-screens/yaml-record-dark.png`
- `docs/design/ao-kb/batch3-screens/yaml-record-light.png`
- `docs/reference/subsystems/graphql-query.md`
- `docs/reference/subsystems/indexing.md`
- `docs/reference/subsystems/notemeta.md`
- `docs/reference/subsystems/ontology.md`
- `docs/reference/subsystems/vault-core.md`

### Source capture and publication

- `pkg/app/indexcore/CONTEXT.md`
- `pkg/app/indexcore/publish.go`
- `pkg/app/indexcore/publish_test.go`
- `pkg/notemeta/CONTEXT.md`
- `pkg/notemeta/indexer.go`
- `pkg/notemeta/projection_loader.go`
- `pkg/notemeta/published_metadata.go`
- `pkg/notemeta/published_metadata_test.go`
- `pkg/notemeta/source_scope.go`
- `pkg/notemeta/source_scope_test.go`
- `pkg/notemeta/source_snapshot.go`

### Providers and explicit discovery

- `pkg/noteformat/CONTEXT.md`
- `pkg/noteformat/architecture_test.go`
- `pkg/noteformat/builtin/registry.go`
- `pkg/noteformat/builtin/registry_test.go`
- `pkg/noteformat/provider_selection_test.go`
- `pkg/noteformat/structured/diagnostics.go`
- `pkg/noteformat/structured/facts.go`
- `pkg/noteformat/structured/facts_test.go`
- `pkg/noteformat/structured/provider.go`
- `pkg/noteformat/structured/provider_test.go`
- `pkg/vault/notediscovery/CONTEXT.md`
- `pkg/vault/notediscovery/structured_test.go`

### Ontology, query and read scopes

- `pkg/ontology/CONTEXT.md`
- `pkg/ontology/materialization.go`
- `pkg/ontology/node_catalog.go`
- `pkg/ontology/noderead/structured_scope_test.go`
- `pkg/ontology/publication_fixture_test.go`
- `pkg/ontology/query/CONTEXT.md`
- `pkg/ontology/query/query_test.go`
- `pkg/ontology/query/record_references.go`
- `pkg/ontology/query/structured_reads_test.go`
- `pkg/ontology/query/structured_regressions_test.go`
- `pkg/ontology/record_reference_index.go`
- `pkg/ontology/record_references.go`
- `pkg/ontology/schema/record_note_interface_test.go`
- `pkg/ontology/schema/schema_sdl.go`
- `pkg/ontology/schema/type_match.go`
- `pkg/ontology/structured_ao_oracle_test.go`
- `pkg/ontology/structured_convergence_test.go`
- `pkg/ontology/structured_locator_test.go`
- `pkg/ontology/structured_references_refresh_test.go`
- `pkg/ontology/sync_record_plan.go`
- `pkg/ontology/testdata/structured-ao/README.md`
- `pkg/ontology/testdata/structured-ao/expected.json`

### Standard reader and web fixtures

- `web/scripts/run-e2e-server.mjs`
- `web/src/api/nodeWorkspaceAdapter.test.ts`
- `web/src/api/nodeWorkspaceAdapter.ts`
- `web/src/components/OntologyNotePane.test.tsx`
- `web/src/components/OntologyNotePane.tsx`
- `web/src/components/body/CollectionView.tsx`
- `web/src/lib/content.test.ts`
- `web/src/lib/content.ts`
- `web/src/note-reader.css`
- `web/tests/e2e/fixtures/structured-records/.rhizome/source-ownership.yml`
- `web/tests/e2e/fixtures/structured-records/notes/structured/services.yaml`
- `web/tests/e2e/fixtures/structured-records/schema.graphql`
- `web/tests/e2e/structured-records.spec.ts`
