---
summary: "Second untangle pass: query audit and concrete candidate evidence."
reference-kind: analysis
---

# Second-pass query and views audit

Baseline: `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`.

Read-only coverage: `pkg/ontology`, `pkg/app/views`, `pkg/app/queryengine`, `pkg/app/runtimeview`, `pkg/app/validationproduct`, `pkg/app/validationrun`, `pkg/validate`, and concrete callers of the candidates below. Read the first-pass delivery evidence and retained decisions. No repository file was edited; no tests/builds, subprocess probes, commits, or external provider requests were run.

Compounding lookup reached the repository launcher but retrieval failed with `Atomic Object server unavailable: unlock the system keychain or run rzm auth login`; it then printed `No current context shards match.` The latter is not evidence of a successful empty search. Findings below use direct source/caller inspection, not Rhizome retrieval.

## Q2-1. Delete the unused string-to-sort adapter

**Classification:** CONFIRMED surplus.

**Evidence:** `pkg/ontology/pushdown/scalars.go:104` defines `AggregateSortValues`; repository-wide identifier search finds its definition only. The active path is `AggregateSortRows` (`:120`) through `pushdown/evaluate.go:69` and `pkg/app/views/sort_text.go:25`. Query and noderead callers use the meaningful `PresentSortValues` helper, not the unused adapter. `AggregateSortValues` is also the sole import use of `pkg/ontology` in `scalars.go`.

**Why these pieces should change together:** the old adapter is a second entry point for exactly the conversion/aggregation already owned by the row pipeline. Nobody selects it, so its signature and dependency only create maintenance surface.

**Smallest fix:** delete `AggregateSortValues` and the now-unused ontology import from `scalars.go`. Preserve `ScalarFromRow`, `AggregateSortRows`, `PresentSortValues`, their scalar typing, null handling and sort-direction rules. No replacement wrapper.

**Callers/config/docs:** no caller, fixture, schema, config, public wire field or docs reference found. It is a Go export, so external Go consumers remain an unverified compatibility consideration, consistent with the first-pass deletion policy.

**Focused check:** package compilation plus existing pushdown scalar/sort tests and configured-view sorting tests. No new mirror test for deleted unused code.

## Q2-2. Retain GraphQL root policy, delegate interface inheritance to schema

**Classification:** CONFIRMED hidden link / duplicated domain rule; behavior-preserving scope is bounded to ordinary compiled schemas.

**Evidence:** `pkg/ontology/query/catalog_records.go:257-308` implements `executor.typeMatchesOrImplements` and its own recursive `executor.interfaceImplements`. `pkg/ontology/schema/type_match.go:22-89` already owns `TypeMatchesOrImplements` and the corresponding recursive traversal; its comment explicitly requires typed-reference consumers to use it. Concrete query callers are `roots_typed.go:77`, `read_plan.go:348`, `selectors.go:162`, `roots_embedded.go:205`, `section_neighbors.go:71/98`, and `relations.go:64/97/104/127/136/142/226`. Query schemas and discovery already call the canonical helper directly (`schema_nodes.go:363`).

**Why these pieces should change together:** object/interface inheritance is one compiled-schema contract. Today fragment matching, relation filtering and discovery can evolve via separate copies. The query method does have real additional GraphQL policy: `Node` accepts any nonempty typed record, whereas `Note` accepts only a concrete note-role type. The generic schema helper intentionally treats `Note` more broadly, so replacing the whole method directly would be wrong.

**Smallest fix:** keep the query method's trimmed/nonempty guard and its explicit `Node` and `Note` cases. Delegate the remaining type/interface matching to `ontoschema.TypeMatchesOrImplements(e.schema, typeName, expected)` and delete only the private recursive duplicate. Do not change `noteMatchesTypeCondition`, `sectionMatchesTypeCondition`, raw fallback identities, public GraphQL names, or the Store/QueryDeps boundary.

**Preserve:** exact match, transitive custom interfaces, unrelated types rejected, note-versus-embedded GraphQL `Note` distinction, `Node` behavior, and canonical `Section` semantics. The canonical helper normalizes recursive names; compiled interface identifiers already have that shape. Hand-constructed schemas with an impossible custom inheritance chain rooted at the built-in `Section` must not drive a new runtime policy.

**Focused check/probe:** before editing, characterize GraphQL execution over a note and an embedded type with inherited interfaces, selecting fields through matching and nonmatching fragments and checking relation filtering. Include explicit `Node`, `Note`, `Section`, exact concrete, unrelated and empty-name controls. Run query tests including `record_facts_test.go`, `roots_interfaces_test.go`, `reverse_relationships_test.go`, and the canonical `TestTypeMatchesOrImplementsUsesCanonicalTransitiveInterfaceSemantics`. A small differential table against the old method is useful if the implementation reveals any built-in difference.

**Related observation, deliberately not included:** `noderead/interface_instances.go:41` enumerates explicitly implemented interfaces. Loaded GraphQL schemas name transitive interfaces explicitly in current fixtures; changing that enumeration is outside this bounded duplicate removal without a supported counterexample. No request here to widen those results.

## Q2-3. Own unordered metadata bullet prefix recognition in Markdown syntax

**Classification:** CONFIRMED hidden link.

**Evidence:** `pkg/ontology/projection_inline.go:182-192` and `pkg/validate/identifier_block_ids.go:291-301` define byte-identical `unorderedListItemContentStart` helpers. Both recognize exactly `-`, `*` or `+` followed by ASCII space or tab and return byte offset 2. Ontology uses this while extracting metadata spans (`projection_inline.go:118`), identifying list metadata (`source_span_content.go:127`) and editing inline fields. Validation uses it at `identifier_block_ids.go:260` when deciding whether a block-id field is already authored.

**Why these pieces should change together:** the projector and repair detector must recognize the same authored metadata bullet prefix. If one accepts a changed prefix while the other does not, reads and repair planning disagree about an existing identifier. This is the lexical shared boundary assigned by policy to `pkg/markdownsyntax`; it is not a request to unify all Markdown list parsers.

**Smallest fix:** move this exact small lexical predicate to `markdownsyntax.UnorderedListItemContentStart`, use it at the three call sites, and delete both private copies. Keep trimming at the current caller; do not make the helper trim or consume variable whitespace. Do not merge `sectionparse.sectionListItemContent`, which additionally supports ordered lists, or `ontology.listMarkerEnd`, which has different marker/offset behavior.

**Affected files:** new or existing lexical helper file in `pkg/markdownsyntax`, `pkg/ontology/projection_inline.go`, `pkg/ontology/source_span_content.go`, `pkg/validate/identifier_block_ids.go`; any same-package test call must migrate. Add an owning note sentence only if needed to name the exported helper, retaining the existing lexical boundary.

**Focused check/probe:** literal helper cases for all three marker bytes, space/tab, missing whitespace, ordered markers, empty input and pre-trimmed versus untrimmed input; retain existing public metadata projection/edit and block-id repair tests. Run focused `pkg/markdownsyntax`, `pkg/ontology`, `pkg/validate` tests. This is movement of accepted grammar, not an opportunity to improve it.

## Q2-4. Remove an impossible error return from asset-presence probing

**Classification:** CONFIRMED surplus and misleading local contract.

**Evidence:** `pkg/app/validationrun/prepare.go:98-101` defines `assetsPresent` returning `(bool, error)`, but always returns a nil error. It intentionally turns an `assetroots.Roots` error into presence so the owning check reports the malformed/unreadable folder. Call sites at `:102-113` nevertheless repeat three unreachable error branches for ontology/views/tasks. `present` at `:87-93` is different and can genuinely fail for modules/efforts.

**Why these pieces should change together:** the helper's type should express its actual triage policy. The dead branches obscure the deliberate rule that a bad asset root must not stop unrelated validation.

**Smallest fix:** return only bool from the local `assetsPresent` closure; initialize the three flags directly and remove their dead branches. Retain the explanatory policy comment, retain `err != nil` as presence, and retain genuine errors from `present` for modules/efforts. Do not weaken source acquisition or validation error reporting.

**Callers/config/docs:** only this function's local call sites change. `vaultFeatureFacts` still returns `(VaultFeatureFacts, error)` because other probes fail. No config schema or runtime authority changes.

**Focused check:** existing `TestPreparedVaultFeatureFactsKeepABrokenAssetFolderForItsOwnCheck` directly guards the important behavior; also run module-asset and selected-recipe tests in `prepare_test.go`. No new test needed solely for the removed impossible branch.

## Retained decisions and negative findings

- Keep the first-pass Q4 constructor and Store/QueryDeps authority split. Plain indexed type sources need no broad GraphQL store; no blanket mismatch guard is proposed.
- Keep raw-note fallback, canonical and historical untyped-section identities, and scope-owned overlays. No new path-only cache key or cross-request Scope reuse.
- Keep query and web preview missing-value policies distinct. Shared parser ownership already exists from the first pass.
- Keep view `isDateField` distinct from schema profile `isDateField`: the profile deliberately excludes lists, while the view role classifies date-valued fields. They are not equivalent copies.
- Keep all observed OS capability skips, self-reexecuting helper-test skips, and the one deliberate long-running child in a cancellation fixture. No failing or invalid test was demonstrated in this read-only pass, so no test is nominated for deletion.
- Keep shallow map/slice copy helpers local where they serve distinct DTO ownership; exporting a generic cloning abstraction would add dependency surface without a demonstrated shared domain rule.
- `runtimeview` remains a narrow snapshot/wait contract; the scan found no worthwhile structural change there.
- `validationproduct` and `validationrun` retain linear repair authority, exact-path postapply refresh and held-lease prepared postchecks. No retry, transaction or lease behavior is proposed for removal.

## Outcome

Four bounded cleanup/refactor candidates remain in this lane. No size-only recommendation, new data/wire contract, schema migration, provider behavior change, or re-litigation of a first-pass retained limit is proposed. Runtime checks are pending the parent's implementation/verification phase; this report does not claim execution evidence.
