# SPEC-0133 Batch 1 foundation exit

Lane A2 implements the shared structural model and Markdown parity on `t3/format-neutral-structural-model`. Lane B and Lane Y were integrated from `3e955f5aa` by merge `6b6ff0966`. The structured parser package remains unchanged from that integration commit. YAML/JSON envelope adaptation and AO starter adoption belong to later batches.

## Delivery sequence

| Step | Commit | Result |
| --- | --- | --- |
| 0 | `172e61c56` | Accepted review changes incorporated into the 364-line foundation design. |
| 1 | `8fbde8e93` | Literal Markdown structural goldens committed before production refactoring. |
| 2 | `6e115ab76` | Validated neutral owners/value tree, deep copies, structural-reading capability, synthetic provider. |
| 3 | `5ed003a65` | Existing Markdown parser moved into the syntax layer; provider facts drive the edit adapter. |
| 4 | `414721478` | Prepared traversal, RECORD compiler/execution, catalog/search owners, presence and occurrence persistence. |
| 5 | `78e83bc08` | Scoped native rich reads, current revisions, reserved locators and ownership-aware capabilities. |
| 6 | Foundation exit commit | Exit regression coverage, native transport adapters, incremental binding completeness, governing notes and final gates. |

## RECORD declaration

This spelling reuses Lane D's `@field(path:)`. `source:` remains a literal Markdown metadata key; it never changes the meaning of `path:`.

```graphql
type Model @node(default: true) {
  services: [Service!] @contains(shape: RECORD, select: "services")
}

type Service
  @node(locator: EMBEDDED)
  @source(
    shape: RECORD
    paths: ["models/*.native"]
    select: "services"
    key: {field: "id"}
    title: ["name", "$key"]
    space: "selected-product"
    logicalKind: "service"
    membership: {path: "products", includes: "booking", includeMissing: true}
    sourceOrder: 0
  ) {
  id: ID @field(path: "id")
  name: String @field(path: "name")
  group: String @field(path: "group", fallback: MAPPING_KEY)
  jobs: [Job!] @contains(shape: RECORD, select: "jobs", edge: {name: "contains"})
  uses: [Service!]
    @field(path: "uses")
    @references(to: ["Service"], mode: STRICT, values: BY_SHAPE, edge: {name: "uses"})
  reads: [Service!]
    @field(path: "reads")
    @references(
      to: ["Service"]
      mode: STRICT
      encoding: TYPED
      values: ELEMENTS
      targets: [{tag: "field", type: "Service", memberPath: "fields[].name"}]
      edge: {name: "reads"}
    )
}

type Job
  @node(locator: EMBEDDED)
  @source(
    shape: RECORD
    paths: ["models/*.native"]
    select: "services[].jobs"
    key: null
    title: ["$value", "$key"]
    scalarAs: "id"
    container: "Service"
    space: "selected-product"
    logicalKind: "job"
  ) {
  id: String @field(path: "id")
}
```

Keys also support `{mappingKey: true}` and `{template: "{from}>{to}"}`. An omitted key is an error; explicit `key: null` selects positional identity. References support `VALUE`, `ELEMENTS`, `MAPPING_KEYS`, and `BY_SHAPE`; `TOLERANT`, `matchName`, and `allowWildcard` are explicit arguments. A scalar field with `@references` validates and retains occurrences without requiring an edge. Containment and references emit edges only when `edge:` is supplied. Compiler tests cover unsupported selectors, mixed Markdown declarations, incomplete arguments, wrong roles, invalid keys/titles, incompatible targets and container cycles.

## Parity and operation evidence

The five structural golden files remain byte-identical to step 1. They cover 19 catalog nodes and 16 semantic chunks, including full refs, identities, fields, parents, spans, body output, link/locator evidence, title variants, embedding schema signatures and complete embedding input. HTML remains root-only. No golden regeneration path remains in the harness.

| Golden | SHA-256 |
| --- | --- |
| fallback | `225f1f020e713557cb762974af96e3bc9a8cfe1560bac56af9574f1f4a542851` |
| long-title | `d21ab95e9dcc063a5ae0e408168bb26a7a919e9d93860ce8c63a36ab89e5b558` |
| malformed | `42d338f0b7b49e762306d825a192f8b2a2f3a7ec4b8233505cc738e3175eb699` |
| nested | `c358926166de3b456ef6cf520f23abab164ba8c37b249f8f23c327881bf1164b` |
| unicode | `c36af33f2e0534c87e7d9a17bb53414aa2d0e0d2f266e61e33ccb006c14c52e7` |

`TestRecordProviderSummaryAndRichReadsShareOneCapture` indexes three records from one native host. It asserts zero source reads/projections for summary hydration, then exactly one source read, one provider projection and one cached prepared binding/resolver state for content and repeated projections. Distinct refs share that state. `TestRecordRichReadsRestoreIndexedCompositionWithoutOtherSourceReads` also restores the product row's account-first logical number/title from the catalog while opening only the product file; the account file is deliberately absent. A changed host uses its freshly captured hash and rejects an old positional locator before reindexing.

`TestRecordQueriesReadNativeContentWithoutMutationSupport` exercises typed summaries, repeated reference targets, child collections, native content, workspace bodies and two different same-file source-link targets. Rich selection asserts one host capture/project; summary selection asserts zero. Native records have no Section level, caret identifier or Markdown body. Web adapter tests retain native values while excluding Markdown/heading projections.

`TestStructuralProviderRefusesUnsupportedMutationsAndPreservesBytes` covers scalar/list/link/root metadata, body insertion/replacement, child insertion/deletion/reorder, identifier operations, rewrites and transforms through the existing edit-session preview boundary. Every unsupported operation fails and leaves exact source bytes intact.

Cross-host index tests retain ordered reference occurrence evidence, deduplicate graph edges and remove unresolved edges after target deletion. Schema 78-to-80 migration/restart tests preserve source presence, value kind, member qualifiers and composition-dependent RECORD identity. Materialization version 19 invalidates earlier derived state. Native chunk dispatch and source-link attribution exclude sibling evidence. A changed or deleted RECORD host forces rebinding of the current declared RECORD source set, including unresolved dependents; unrelated Markdown changes retain their existing plan.

`TestPositionalLocatorsDistinguishBindingsOfTheSameOwner` reproduced a collision when two positional bindings selected one native value. Positional fragments now use `record-pos:v1:<source SHA-256>:<physical identity digest>` so binding identity distinguishes them. The source hash still rejects every captured positional locator after a host edit; positional NodeIDs remain independent of that hash.

The existing public GraphQL schema golden has only additive workspace source, locator durability/revision, graph occurrence and Lane D field source-path fields. The frozen Markdown structural goldens are untouched.

## Integration boundaries

Lane B's existing `ApplySourceOwnership(policy, workspace)` remains intact. HTTP workspace construction and GraphQL workspace/body construction apply it after executable capabilities are built. RECORD reads expose no mutation operation; readable child collections independently enable navigation. Lane B continues to own `.rhizome/source-ownership.yml`, staging/commit admission and refusal reasons. No replacement ownership input was introduced.

Lane Y's `pkg/noteformat/structured` requires a later adapter to `StructureFacts`. No YAML/JSON provider was registered in this batch. Web API transport carries `workspace.source` and `bodies.source` as validated native values with text/media/representation/address/provenance. Lane K's views and starter paths remain outside this lane's edits.

## Changed areas

| Area | Files and responsibility |
| --- | --- |
| Syntax providers | `pkg/noteformat/structure*.go`, `source_value.go`, runtime contracts, Markdown provider, and the existing parser moved under `pkg/markdownsyntax/{sectionparse,structure}`. |
| Ontology | `pkg/ontology/prepared_document.go`, `record*.go`, schema compiler/model, projection/catalog/traversal, source links, capabilities and incremental RECORD binding plan. |
| Rich reads and query | `pkg/ontology/{noderead,query,readmodel}` for scoped native reads, indexed context, references, source payloads, locators and occurrence evidence. |
| Persistence and search | `pkg/store` schema migrations 79/80, row readers/writers and SQL predicates; `pkg/embedindex` native owner chunks and Markdown parity fixtures. |
| Transport and compatibility | `pkg/app/web` native workspace/preview/OpenAPI contracts; `web/src/api` source decoding and adapters; descriptor/import migrations in code-intel and agent consumers. |
| Evidence and guidance | Structural goldens, synthetic fixtures, mutation/operation-count tests, eight governing subsystem notes and owning package `CONTEXT.md` files. |

## Verification

The final required sequence passed: focused FTS5 package tests, focused integration, `make check-fast`, then `make check`. The final full gate includes Go unit tests, web unit tests, content/credential checks and team API tests. No failing required check remains.

Focused packages pass:

```sh
GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/noteformat/... ./pkg/markdownsyntax/... ./pkg/ontology/... ./pkg/store ./pkg/embedindex ./pkg/app/web ./pkg/search/planner ./pkg/app/agentapi ./pkg/notemeta ./scripts/release/contract ./cmd
```

Focused integration passes, covering Markdown typed queries, the public schema, edit/reindex behavior and primary chunks:

```sh
GOCACHE=$PWD/.gocache go test -tags 'fts5 integration' ./tests/integration/python -run 'TestPythonFixture_Ontology|TestOntologyEditSession_ReindexesAndUpdatesGraphQLQuery|TestPythonFixture_PrimaryOntologyChunksReplaceGeneratedCards'
```

The first full gate exposed four compatibility failures. Its descriptor-only Markdown test fixture now registers an executable provider; the raw-metadata provenance assertion expects projection v6; the agent API fingerprints explicitly include additive locator durability/revision fields; and the root projection constructor again accepts a snapshot without a populated root ref. All four focused regressions pass. No unrelated failure was suppressed or repaired.

`./scripts/rzm validate` passes with zero issues and zero errors. The design remains 364 lines. Frozen structural golden diffs and the Lane B/Y package diffs are empty.

`make check-full` is excluded by the lane brief. Browser screens, native YAML/JSON execution and AO starter adoption require their later lanes. Independent implementation foundation review remains the coordinator's integration gate before dependent work. No push, PR, desktop installation or live-vault mutation is part of this delivery.
