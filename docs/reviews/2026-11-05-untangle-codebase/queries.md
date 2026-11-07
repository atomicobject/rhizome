# Query, read-model, validation, and configured-view structural audit

Baseline: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. Read-only inspection. No production changes and no test/build runs. Ownership covered `pkg/ontology/{query,pushdown,queryrecipe,noderead,readmodel,schemadoc,viewconfig}`, `pkg/validate`, and `pkg/app/{validationrun,validationproduct,queryengine,views}`. Cross-boundary section-presentation findings were coordinated with the web/core auditors. Source scans covered 333 production Go files, 67,225 lines; that is scan coverage, not a claim to have manually read every line.

Read governing GraphQL, noderead, and validation skills/notes and package AGENTS. Existing shared seams are generally sound: query/views use `pushdown.Planner`; query and browser reads converge through request-scoped `noderead.Scope`; recipes share discovery/execution; validation has a descriptor registry and neutral result pipeline. Do not replace these with another generic layer.

## Q1. One section-field matcher should own `@contains` binding

**CONFIRMED, worthwhile; hidden link.** The same section-binding algorithm exists at:

- `pkg/ontology/build_assessment.go:302` (`matchSectionsForAssessment`).
- `pkg/ontology/query/sections.go:303` (`matchSectionNodesForField`).
- `pkg/app/web/ontology.go:2802` (`matchWebSectionNodesForField`), whose comment explicitly says it mirrors query.

All choose explicit-heading recursive matches, otherwise direct children at the requested level after unwrapping a lone H1. A behavior change currently requires synchronized edits across assessment, query, and web. Assessment/projection callers are `build_assessment.go:238`, `projection_resolver.go:257`, and `projection_bindings.go:27,36`; query callers are `sections.go:85,117`; web caller is `ontology.go:2504`.

**Smallest redesign:** put exported `MatchSectionNodesForField` beside ontology's existing section helpers and migrate every caller, deleting all three private copies. No new package. Preserve explicit-heading versus list-without-heading behavior, document order, nil nodes, H1 unwrap, nested starting-slice semantics, and singular-field rejection. Assessment currently checks raw nonempty heading while the other two trim; verify compiler normalization at `schema/schema_compile_fields.go:117` and cover a whitespace-only programmatic field before choosing shared behavior.

**Verification:** existing ontology section assessment/projection tests, query section list/nested tests, and web section binding tests; one direct table of literal expected section identities covering heading, level-only list, nested children, nil and singular-no-heading. Owning docs: ontology and GraphQL section contracts, web section presentation if an entry point changes. Web/core auditors agree with canonical ontology ownership.

## Q2. Reuse schemadoc's existing guidance and enum projection

**CONFIRMED, worthwhile; hidden links and surplus.** In the same package:

- `metadata.go:25` and `guide_guidance.go:70` duplicate summary fallback.
- The adjacent meaning, authoring, and agent-implications helpers duplicate each other.
- `metadata.go:53` and `guide_guidance.go:118` duplicate policy copying.
- `reference_render.go:237` and `guide_guidance.go:98` duplicate policy rendering byte for byte.
- `enums.go:24` and `guide_guidance.go:35` independently construct the same `EnumDoc`/`EnumValueDoc` semantic fields. The reference path additionally carries display/lifecycle metadata.

**Smallest redesign:** use existing `guidance*`, `clonePolicy`, and `formatPolicy` functions from both renderers and delete the `guide*` copies. Replace `ontologyEnumDoc` calls with `enumDoc` where preserving guide output is proven; the guide need not print the reference-only fields. Do not merge the guide and reference products or force their formatting to match. Keep `guideType`'s authoring-only structural data until a separate need justifies changing it.

Callers: `guide_types.go`, `guide_fields.go`, `guide_guidance.go`, `project.go`, `enums.go`, `reference_render.go`. No public API/config change.

**Verification:** `go test -tags fts5 ./pkg/ontology/schemadoc`; preserve `TestRenderMarkdown_IncludesGuidanceAndEnumPolicyForAuthoring`, `TestRenderMarkdown_IncludesLayeredGuidanceAndEnumPolicy`, `TestSchemaDocs_IncludeGuidanceAndEnumValuePolicy`, enum display tests and generated text. These tests cover different published products and must not be deleted as duplicates.

## Q3. Configured views retain obsolete pushdown adapter layers

**CONFIRMED, worthwhile; surplus and caller vocabulary.** `source_ontology.go:414` (`typeInstancesPushdown`) takes a receiver, context, and note type but only returns three fields from `SourceResult`. Callers at 192 and 265 falsely imply a second planning step. `interfaceTypeInstancesPushdown` at 446 takes unused receiver/context and only selects the first already-proven-equivalent plan; its caller already proves nonempty/equivalent at 154. `nodereadScopeForResolver` at 526 takes an unused context.

`viewLinkResolver` at 310 wraps `CatalogLinkResolver` even though the wrapped type already satisfies the exact `ResolveLinkTarget` interface. Both type assertions to `noderead.ReadStore` at 35 and 320 assert an interface onto its already declared type (`ServiceOptions.Store`, types.go:34). The fallback can recreate a candidate resolver instead of making request-scoped ownership explicit.

**Smallest redesign:** read the planned fields directly and remove no-op methods; construct one `CatalogLinkResolver` per `ResolveSource` and hand it directly to the shared planner; retain its existing structured nil/unavailable behavior. Rename/rehome the scope constructor without a fictitious context if still helpful. Factor the 1,155-line `source_ontology.go` by actual responsibility (source enumeration, planner mapping, row hydration/capabilities) as part of these edits rather than introducing service abstractions. Do not change cap semantics, warning codes or cached target candidate lifetime.

**Verification:** configured views source/filter/sort tests, especially `link_filter_test.go` (both collapsed and per-type interface execution) and `source_filter_consistency_test.go`/`source_sort_consistency_test.go`. Public `Service.Execute` output is the contract. Update `pkg/app/views/CONTEXT.md` only for entry-point organization, not a new behavior claim.

## Q4. Configured views require callers to duplicate one source authority

**CONFIRMED structure; broader redesign needs a contract decision.** `ServiceOptions` (`types.go:28-38`) separately carries VaultPath, VaultDef, NoteReader, Store, ReadOverlay, and `QueryDeps`, which repeats most of these. `New` only synchronizes a nonnil overlay (`service.go:20`). Query-recipe execution uses QueryDeps (`source_query_recipe.go:39`), while hydration uses standalone options (`source_ontology.go:526`). `resolveIndexedEmbeddedOntologySource` at 255 checks `QueryDeps.Store` merely to enable a path whose actual reads use standalone `Store` through Scope. Thus unrelated GraphQL wiring controls ordinary configured type-view planning.

Production callers manually keep both copies aligned in `cmd/view.go:252-286` and `pkg/app/web/views.go:230-250`; fixture construction does the same in `type_views_fixture_test.go:106-116`. Custom source resolvers and catalog-only callers legitimately omit the query runtime, so blindly requiring it would break supported use.

**Redesign recommendation:** give the default resolver one canonical read-dependency bundle, with explicitly separate optional GraphQL execution dependencies. Derive shared vault/reader/store/overlay values once at construction, and gate indexed type execution on its actual read capabilities. Reuse `queryengine`'s existing schema pairing validation; do not add a second engine. First decide which constructor field owns shared authority and the failure behavior for mismatched existing inputs. Preserve custom SourceResolver and catalog-only construction.

**Verification:** literal rows and warning/plan output from `Service.Execute` for (a) type view with no recipe engine, (b) recipe view, (c) overlay, and (d) mismatched options rejected rather than mixed. Run cmd and web view-service tests. This is an internal constructor/ownership change, not a reason to alter public view JSON or YAML.

## Q5. Neutral validation code depends on the CLI initializer for bundled overlays

**CONFIRMED, worthwhile after choosing composition seam; needless dependency.** `validationrun/prepare.go:12,66` and `validationproduct/projection.go:11,229` import `pkg/app/cli/init` solely to select `ValidateBundledSkillOverlays`. This pulls the initializer and embedded templates into neutral validation orchestration. The domain already accepts the exact capability as `validate.RunContext.SkillOverlays`, so a new framework is unnecessary. Tooling auditor independently confirmed this and agrees to injection rather than moving template sources.

**Smallest redesign:** select the bundled overlay checker in the application composition layer, pass the existing callback into preparation/default context construction, and keep templates in their current source-of-truth package. There must be one composed checker shared by command, live, indexed, and fallback projection contexts. Do not merely remove it: missing checker would silently discard current validation coverage.

All affected routes:

- `cmd/validation_product_runner.go:57` → Prepare and its projection request.
- `validationrun/index.go:25` → Prepare, called by `pkg/app/indexing/unified.go:454`.
- `validationproduct/live.go:40` → Prepare; invoked by coordinator.go:74 and `pkg/app/cli/serve/readiness.go:382`.
- `validationproduct/projection.go:63,214` creates fallback RunContext.
- Tests directly create Prepare / RunLive / IndexValidationSnapshot / projection contexts and need capability fixtures.

**Verification:** retain real bundled-checker integration assertions at command/application composition; neutral package tests should inject a synthetic checker and prove it ran and propagated issues/errors. Existing `validationrun/skill_overlays_test.go` and `validationproduct/skill_overlays_test.go` currently duplicate the bundled wiring test at the wrong layer; move the distinct command/index/live assertions to their owners rather than delete coverage. Run validationrun, validationproduct, indexing focused tests and full gate due indexing involvement. Update both CONTEXT.md documents and the init context's injection description.

## Q6. Shared workspace display semantics are implemented twice

**CONFIRMED duplication; preview policy decision needed.** `query/workspace_bodies.go:199` and `web/ontology.go:2783` choose a preferred identifier then first identifier. `query/workspace_bodies.go:214` and `web/ontology.go:2878` independently parse the same preview placeholders with the same regex (`workspace_bodies.go:15` and web's `previewPlaceholderInterpolator`). The implementations already disagree: absent property becomes empty text in GraphQL but remains literal `{{name}}` in web. This can occur for optional fields; compiler recognition of a field does not prove a value exists.

**Smallest redesign:** canonicalize identifier selection alongside the schema/ontology presentation facts. For preview expansion, first record the intended missing-value behavior, then use one ontology-owned pure interpolator with caller-produced properties/title. If preserving both public behaviors is required now, retain an explicit missing-value policy at the boundary, not two parsers. Do not merge web raw-section extraction with query projection hydration; those have different input authority and must remain separated.

Callers: query workspace body binding at 162-163; web renderSectionBodies at 2525-2531. Coordinate web audit. Verification: query workspace content tests plus web ontology section binding/preview tests with a known missing optional field, whitespace around placeholders, title and identifier preference. Needs a documented policy decision before changing any output.

## Q7. Local query field lookup recomputes presence separately from value

**CONFIRMED, small local cleanup; hidden link/surplus.** `query/types.go:662-724` has four near-identical case-insensitive map scans: frontmatter value/presence and inline value/presence. Callers in `noteRecord.propertyValues` need both value and presence, but fetch them independently, so matching/canonicalization policy can drift and the same map can be scanned twice.

**Smallest redesign:** one private generic `caseInsensitiveLookup[T](map[string]T, string) (T, bool)` with exact-key precedence followed by the current normalized fallback. Consume returned presence at the caller. Delete all four specialized functions. Keep this helper package-local; do not build a repo-wide collection utility. Preserve authored-empty/null behavior and alias order exactly.

**Verification:** existing `root_scalar_parity_test.go`, `authored_scalar_parity_test.go`, `overlay_scalar_selection_test.go`, and source alias tests; add a small literal table only if current cases miss exact-case precedence or empty present values. Do not turn SQL/residual parity into self-comparison-only tests.

## Retain and do not broaden

- Do not merge pushdown's ViewResolver and SchemaResolver spelling rules; their differences are public and intentional.
- Keep request-scoped caches, canonical ref+profile keys, overlay merging before page/filter semantics, late source-deletion checks, result caps/coverage warnings, and validation publication/repair locks.
- Keep platform/filesystem skips for unsupported symlinks, case behavior, permissions, Git hooks. No disabled test found here was proven unjustified.
- No test was proven incapable of catching a real bug. Keep both guide/reference output tests, graph endpoint/store versus assembled-scope tests, scalar parity tests with literal expected rows, and real bundled overlay checks until replacement coverage exists.
- Small repeated clone helpers across packages are not by themselves worth a new shared utility; similar syntax is not proof of shared reason to change.
- Do not collapse SourceResult's public JSON plan fields or alter validation issue IDs as a cleanup.
- The explicit Markdown view-path compatibility function at source_ontology.go:424 is documented behavior, not safe dead code.

## Optional comments pass

The three duplicate section-matcher explanations disappear with Q1. At `source_ontology.go:109-111`, the WHY comment restates that noderead is the shared source; after consolidation, keep only any external contract reference that remains useful. At 289-292 the explanation for why a real Scope is necessary to retain edit target candidates is useful architectural rationale and should stay. Public type/function documentation and safety-contract comments in noderead/repair code should not be deleted just because names overlap.

## Evidence limits

This is source inspection and caller tracing, not runtime failure reproduction or a performance benchmark. Q1, Q2, Q3, Q5, Q7 are concrete reduction opportunities; Q4 and Q6 require an explicit authority/output decision before implementation. No broad gates were run to avoid racing other work.
