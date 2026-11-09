---
type: EffortNote
id: EFF-2026-11-08-19-38
aliases: [EFF-2026-11-08-19-38]
name: "AO KB Knowledge site"
created-at: "2026-11-09T00:38:28Z"
status: planned
summary: "Deliver SPEC-0135: the AO KB Knowledge site on existing view mounts first (complete legacy-aware data and setup, tab-retained view params, KB overview, record pages, Review), then the generic sidebar panel and custom-first view switcher."
governing-specs:
  - "[[docs/specs/experience/ao-kb-knowledge-site]]"
---

# AO KB Knowledge site

## Scope

Deliver [[ao-kb-knowledge-site|SPEC-0135]] in two pull requests. PR 1 proves the site on real KBs using the starter's existing custom-view mounts: complete and legacy-aware data reads, a knowledge-only setup route for legacy KBs, the site library, tabs that keep declared view params, the KB overview, Review, Sources, Artifacts, Concept and Artifact pages, and the reworked Assertion and Source pages. PR 2 adds the platform pieces: the `sidebar` mount and panel, the starter's panel view, and the custom-first view switcher. The effort closes in PR 2.

Excluded, as the spec's Non-Goals state: KB writes, AO judgments, inferred links or dates, prose concept-to-concept edges, `CLAUDE.md` identity, scheduled or saved analysis, 3D, people views, product-model pages and model bindings below schemaVersion 8, legacy product-mode accounts, archived submissions, editing, export, a site manifest, and migrating legacy KBs.

## Spec Set (Frozen)

- [[docs/specs/experience/ao-kb-knowledge-site|SPEC-0135]] at the revision Drew approves with this plan. Drew decided D1 to D8 on 2026-11-08 (recommended options; his own wording for D6 and D7). The spec moves from `proposed` to `active` at that approval. Base revision: `439890f79`.

## Stories In Scope (Frozen)

- [[ao-kb-knowledge-site#^SPEC-0135-US1|SPEC-0135.US1]] Orient on a KB's overview.
- [[ao-kb-knowledge-site#^SPEC-0135-US2|SPEC-0135.US2]] See where a concept, source, or artifact sits.
- [[ao-kb-knowledge-site#^SPEC-0135-US3|SPEC-0135.US3]] Read an assertion and its sources without overclaiming.
- [[ao-kb-knowledge-site#^SPEC-0135-US4|SPEC-0135.US4]] Answer a question from the matrix.
- [[ao-kb-knowledge-site#^SPEC-0135-US5|SPEC-0135.US5]] Keep the KB in shape from Review.
- [[ao-kb-knowledge-site#^SPEC-0135-US6|SPEC-0135.US6]] Read and set up a KB written on an older AO schema.
- [[ao-kb-knowledge-site#^SPEC-0135-US7|SPEC-0135.US7]] Navigate the site from the sidebar panel.
- [[ao-kb-knowledge-site#^SPEC-0135-US8|SPEC-0135.US8]] See custom views first in the view switcher.

## Spec Coverage Checklist

- [ ] R1 labels and missing values: Batch C copy table, checked in Batch D's real-KB pass.
- [ ] R2 fields, R2a params, R2b legacy admission and setup, R2c completeness and identity: Batch A (fields, setup, queries), Batch C (params).
- [ ] R3 kind groups and colour: Batch B (table), Batch C (tokens and palette check).
- [ ] R4 visuals and their "not enough to draw yet" states: Batches B and C.
- [ ] R5 orientation query, scope reads, and query budget: Batch A query module, Batch B library, Batch D measurement.
- [ ] R6 sidebar panel contract, including the catalog's `sidebarViewId`: Batch E, after its foundation review.
- [ ] R6a view params survive tabs: Batch H.
- [ ] R7 switcher order: Batch G.
- [ ] R8 accessibility and design: Batches C, E, F, G.
- [ ] R9 verification: every batch; real-KB pass in Batch D.
- [ ] US1 to US5: Batches B and C (US4's tab return with H). US6: Batch A. US7: Batches E and F. US8: Batch G.
- [ ] Amendments: SPEC-0133 (Batches A and C), SPEC-0105 (Batches A and E), SPEC-0110 (Batches E, G, and H), SPEC-0128 (Batch E).

## Plan

### Current gap

Verified on `439890f79` against the starter, AO KB `8383d033`, the three real KBs (counts and keys only), and a disposable copy of the largest real KB:

- Legacy KBs fail setup at four points. `module/bindings/knowledge.mjs:45` rejects any recorded `schemaVersion` other than 8. Both legacy KBs have a `.kb/config/manifest.json` with no `siteEngine`, which `knowledge.mjs:73-76` rejects as `ao_toolkit_pin` once a manifest exists. `publish.mjs:36` always runs the model exporter, whose `adapter.py:84` requires the current schema, and `publish.mjs:117` requires model readiness for `bindingsReady`. `cli.mjs:45` refuses to index without `bindingsReady`, and the setup skill requires model bindings `ok`. Current AO refuses to migrate anything below schemaVersion 7 without installing AO 6.2.0.
- `Assertion.kind` reads only `kind`; the legacy KBs write `assertionKind` on every assertion (878 of 878 read with the R2 sources on the copy).
- `Source` has no recorded-date field; `kind` ignores `format`; `Artifact.supportedBy` is a link field, and the medium legacy KB's three `supportingAssertions` values are all AO IDs that match no file name.
- `views/knowledge/data.tsx` reads `first: 500` per root and per nested list. On the copy it returned 500 of 878 assertions with root status `capped`; nested lists report no truncation.
- Membership is outbound only (`a.concepts` in `lib/knowledge.ts`, `concepts.tsx:83`, and the Assertions list's concept chips at `assertions.tsx:97`): 236 pairs and 208 member assertions on the copy, against 315 pairs and 269 assertions for the union. Related Concepts sections hold all 68 concept pairs. The Concepts, Assertions, and Attention pages share one evidence read (`data.tsx`), which has no concept-side membership field.
- `Source` declares no relation field, and the generic `neighborhood` field returns 20 rows by default and at most 200 (`pkg/ontology/query/roots_graph.go:16-22`), while a declared `@neighbors` field without `first` returns every row (`relations.go`). `Assertion.provenanceSource` is a raw scalar, and `citations()` (`lib/knowledge.ts:152-180`) counts an assertion whose `provenance.source` resolves to a source as citing it with no link; no inbound read finds that assertion. The three real KBs record no `provenance.source`, so their measurements cannot show the gap.
- The index receipt's signature (`runtime.mjs` `indexSignature`) hashes the model sources and the adapter's `knowledgeSources`, which skip underscore-named files (`adapter.py:136-142`) that the starter's static type selectors admit, and cover none of the new documents. The large KB has six underscore-named artifacts, three flat submissions, and ten curation documents.
- The check's rows (`scripts/lib/bindings-check.mjs`) would misreport a legacy KB once its bindings are ready: `model compiler dependencies` says `dependency missing` without uv, `model bindings` says `ok`, and `model source ownership` stays `pending` because the manifest lists no sources. `refresh-bindings` prints a fixed `modelReady: true` (`cli.mjs:34-35`). `docs/reference/subsystems/cli.md:52` records the current model states.
- The view catalog (`ViewCatalog`, `ViewTarget` in `pkg/app/web/openapi.yaml`, generated into `web/src/api/generated.ts`) has no place for a group's sidebar view.
- "Knowledge" is the display group of the AO KB types, the Agentic Engineering starter's `ReferenceDoc`, and the compounding starter's `ContextShard` and `AgentFinding`.
- The kit's `readGraphQL` drops `extensions`, so views cannot read typed-root coverage.
- The large legacy KB keeps three status-bearing submissions flat under `_improvements/` and none nested; AO documents nested submissions and flat `fnd-*.md` findings. Its ten curation documents record `updatedAt`, and the generated `updatedAt` record fact (file modification time) never equals it. The setup skill, check, and e2e server include only `.kb/curation/_conflicts.md`, so other curation documents are never indexed.
- `kind` and `folder` are reserved workspace URL keys (`viewconfig/params.go`), so the first draft's params fail validation.
- Workbench tabs (`useNoteTabs.ts`, `noteTabStorage.ts`) store no view params, so activating a tab rebuilds a URL without them.
- Open-search requires a folder (`CustomViewFrame.tsx`), so a panel cannot search the whole vault.
- The Knowledge group already offers the bundled "Overview" (`web/bundled-views/group/overview.yaml`).
- `viewconfig.MatchesMount` requires the context kind to equal the mount kind, so a sidebar view invoked with a group context is rejected at `custom_view_context.go:125` today.
- Embedded types with no display parent, such as Engineering's `UserStory` and AO's conflict entries, are in no group's navigation tree (`display_groups.go`), and the Engineering starter displays `ReferenceDoc` in "Knowledge".
- `ViewSelector.tsx` puts every custom choice in a menu once there are two or more. `PanelResizeHandle.tsx` resizes width only.

### Measurements

Taken on 2026-11-08 on a disposable copy of the largest real KB in the session scratchpad: this checkout's built binary at `439890f79`, the ao-kb starter installed with `--search off`, `noteEmbeddings.enabled: false`, and provider keys removed from the environment; the R2 fields applied to the copy's starter ontology. Medians of five warm runs through the local runtime's public GraphQL endpoint. The copy, its index, and its runtime were deleted afterward; nothing was sent to a provider.

| Read | Median | Payload | Rows | Coverage |
| --- | ---: | ---: | --- | --- |
| Orientation query (SPEC-0135 R5) | 291 ms | 501 KiB | 878 / 115 / 21 / 181 / 3 | complete |
| Orientation plus citations and artifact references | 807 ms | 833 KiB | same | complete |
| Citations section alone | 582 ms | 214 KiB | 878 | complete |
| Artifact references alone | 338 ms | 189 KiB | 181 | complete |
| Concept-side membership and Related Concepts alone | 96 ms | 32 KiB | 21 | complete |
| Today's whole-KB query, roots at 5000, nested `first` removed | 2,039 ms | 6.2 MiB | 878 / 115 / 21 / 181 | complete |
| Scope read, largest concept (48 members) | 64 ms | 12 KiB | | |
| Scope read, most-linked source (66 links): neighbourhood, then citations | 15 ms + 47 ms | 23 KiB | | |
| Scope read, one artifact | 3 ms | under 1 KiB | | |

The same copy reproduced the planning counts exactly: 181 concept-side outbound and 236 inbound pairs, 315 union pairs, 269 covered assertions, 68 Related Concepts pairs, and 0 recorded source dates. With synthetic notes added to the copy, `supportValues` returned `[]` for an explicitly empty `supported_by` beside a legacy value, the legacy value when only it was present, and the current value when both were; `Submission` selected the three flat records and a nested one while excluding an archived file, an attachment, and a status-less guide; `CurationDocument` selected all ten plus a synthetic one, with `recordedUpdatedAt` null where `updatedAt` was not authored. The planning-time figure of 1,374 ms and 4.5 MiB for today's capped query was not re-measured. Layout timings (0.5 ms for 21 concepts to 119 ms for 400) are the prototype's and are re-measured in Batch B. Two later changes are not measured: `provenanceSource` in the orientation query, null on all three real KBs, and `Source.linkingAssertions` in place of the neighbourhood read (the same directive shape as `Concept.linkingAssertions`, which returned the 236 inbound pairs above). Batch D measures both exact strings.

### Decisions settled in this plan

- **No site declaration.** A site is a display group plus its module's views; the panel attaches by a `sidebar` mount on that group. Activation follows the active tab's subject through the group's effective display tree, with embedded and section nodes judged by their owning note (SPEC-0135 R6). This covers the Agentic Engineering journey from spec to story to criterion and both effort forms. Reference and policy documents are a separate section from the Engineering site (D6, decided by Drew). No group name or starter combination is assumed, and a panel spanning groups is allowed where a journey needs it; none in scope does, so the mount names one group.
- **Panel draws its own locator.** The Agentic Engineering prototype's locator is an effort timeline and the AO KB's is a topic map, so the contents must be site-supplied. The panel is an ordinary framed custom view using the same site library as the pages, so positions match by construction.
- **File-name dates are plotted, marked, and counted.** A leading `YYYY-MM-DD` in the file name draws a hollow mark labelled "Date in file name"; recorded dates draw filled marks; title dates are not used; nothing is trimmed. Without them the largest KB's axis is empty (0 of 115 recorded dates; 68 file-name dates).
- **One orientation query plus per-record scope reads.** Putting citations and artifact references in the whole-KB query nearly triples its cost (291 ms to 807 ms on the largest KB); only record pages need them, and per-record reads cost 3 to 64 ms.
- **Membership is read from the concept side** for speed; per D1 the shared function takes both directions as one deduplicated set.
- **The site's home page is named "KB overview"**, because the bundled "Overview" already appears in the group's switcher.
- **Legacy KBs set up knowledge-only.** Below schemaVersion 8 the model stage is `not applicable`, the receipt carries the site source signature computed in Node, and no Python dependency is needed.
- **One receipt inventory for every schema.** The site source signature follows the types' and admission's path rules on both routes, so a new document or an underscore-named artifact stales the index. Existing schemaVersion 8 receipts expire once after upgrade.
- **Kind-group colours and lines.** Magenta `--type-10` replaces violet or indigo for Intent and rules, and relationship lines use `--link`, because the old choices failed R3's colour-vision rule and R8's 3:1 rule (computed in SPEC-0135 R3). Drew chose magenta on 2026-11-08.
- **Existing pages read completely.** Concepts, Assertions, and Attention lose their caps and read about 2 s and 6.2 MiB on the largest KB; complete counts are worth that on those pages, and Batch D reports it per KB.

### Batches

#### Batch A. Complete, legacy-aware knowledge reads and setup (PR 1)

- **Outcome:** every whole-KB read is complete or says what it missed, and a single-product schemaVersion 4 or 5 KB completes setup and browses read-only. Advances R2, R2b, R2c, R5's query module, US6, and the SPEC-0133 R6 amendment.
- **Ownership:** starter ontology; the binding compiler and setup route (`module/bindings/knowledge.mjs`, `publish.mjs`, `cli.mjs`, `runtime.mjs`, `README.md`); `module/scripts/check.mjs` and `module/scripts/lib/bindings-check.mjs`; the setup skill `agents/skills/ao-kb-setup/SKILL.md` and the guide `repo/docs/reference/guides/ao-kb.md`; `docs/reference/subsystems/cli.md`; the existing views' data module `views/knowledge/data.tsx` only; the legacy model copy in `views/knowledge/lib/model.ts` and `trace.tsx`; the query module `views/knowledge/lib/queries.ts` (new); two additive kit fields; the new and product fixtures. Batch A does not edit `lib/knowledge.ts`, `concepts.tsx`, or `assertions.tsx`.
- **Work:**
  - Ontology (`module/ontology/assertions.graphql`, `sources-concepts.graphql`, `artifacts-conflicts.graphql`, new `documents.graphql`): exactly the R2 fields and types, spelled as R2 names them. The spellings, `has:status` selector, and hidden-path selector compiled and selected the right files on the measurement copy.
  - Setup route (R2b): admission for single-product schemaVersion 4 through 8; the `siteEngine`-absent rule for knowledge admission; the `not applicable` model state through refresh, check, and index; `refresh-bindings` printing the computed model state; the check's named rows exactly as R2b lists them, in `bindings-check.mjs`; `ao_schema_unsupported` for legacy product mode. The model stage's own pin and schema checks stay as they are. The setup skill's steps 3 and 6 accept `not applicable` below schemaVersion 8 and drop uv there. Existing views say "not applicable below schemaVersion 8" where `readBindingFreshness` (`lib/model.ts`) and `trace.tsx:237` now say the bindings could not be read or are not ready.
  - Site source signature (R2b): one Node inventory, built from the path atoms `knowledge.mjs` already computes, used by `indexSignature` on both schema routes before and after indexing and by the check. Record in `cli.md` (with `last-verified` bumped) the `not applicable` state, the knowledge-only route, the signature, and the `siteEngine`-absent rule.
  - Includes: the check's include rule requires `.kb/curation/*.md` whenever curation documents other than `_conflicts.md` exist; the setup skill's step 4 and the guide name that include and describe the legacy route. The knowledge manifest's context gains `profile`, omitted with a note when the scalar reader refuses it.
  - Product mode: the generated admission scopes `MapOfContent`, `Submission`, and `CurationDocument` to the account root and the selected product's root.
  - Query module (`lib/queries.ts`): the R5 orientation query string (with `provenanceSource`), the three scope-read builders (the Source builder reads `linkingAssertions` without `first`), and Review's document query, exported for the views and for Go tests. The module imports nothing from `@rhizome/kit`, as `lib/model.ts` already does, so the Go tests read the strings by running `node --experimental-strip-types` on it.
  - Completeness (`views/knowledge/data.tsx` only): roots at `first: 5000`, nested `first` removed, the concept-side `linkedAssertions { path }` and `linkingAssertions { path }` added, coverage read through the kit, and the coverage status used instead of `CEILING`. Nothing else in the Concepts, Assertions, and Attention evidence read changes. The switch of those pages to the library's membership is Batch C's.
  - Kit (`web/kit/index.tsx`, `web/src/api/publicGraphQLTypes.ts`): carry `extensions` through and expose typed-root coverage as an accessor keyed by the result's data object, as the kit's tolerated-errors pattern already does, so `useGraphQL`'s return shape is unchanged and coverage stays with the data it describes when structural sharing keeps an old object. Document the shape in the custom-views kit reference under `pkg/app/cli/init/templates/skills/`.
  - Fixtures, all synthetic (see Fixtures): `testdata/ao-kb-legacy/` new; one map of content, one submission, and one curation document added to `testdata/ao-kb-product`.
- **Exit evidence:** `pkg/app/cli/init` ontology and starter tests for every R9 ontology case, including the knowledge-only refresh, check, index, and check sequence on the legacy fixture with uv absent from PATH, the site source signature cases on both schema routes, and the exported orientation query run on the legacy, booking, and product fixtures; `testdata/ao-kb/expected-knowledge.graphql` and the product fixture's expectations updated and reviewed as changed contracts, with `TestAOKBRecipesAndIndexParity` passing unchanged because booking's files do not change; a kit unit test for coverage; integration tests (`-tags=integration`) because fixtures index; `make build`, `rzm init`, and `rzm init --check` exiting 0.
- **Escalation:** a needed core change to `@field`, `@neighbors`, or matchers, or AO tooling that contradicts the retired-name table.

#### Batch B. Site library (PR 1, parallel with A)

- **Outcome:** one pure module computes everything the site draws. Advances R3, R4, R5, and US1 to US5's data.
- **Ownership:** `module/views/knowledge/lib/site.ts` (under 500 lines; split kind groups into `lib/kinds.ts` if needed). No UI, no queries.
- **Work:** membership, the only implementation, which the Concepts and Assertions pages switch to in Batch C; kind groups with legacy names; stored-confidence buckets as an explicit set; the kind conflict; the Assertions list breakdown counts (US4); deterministic layout ported from the prototype's arithmetic with sorted inputs and estimated label widths, the height that grows with the concept count, the separation passes, shortened names and the unlabelled count, the 12px arc threshold, and the map's arrow order; timeline row labels that keep the last folder name whole; date placement with basis, including the minimum-area neutral mark for a dated source with no resolved extraction; source colour by strict majority; matrix counts with once-per-assertion totals and the linear tint scale from the 12% floor; scope sets for concept, source, and artifact from scope-read results, the source set uniting `linkingAssertions` with assertions whose `provenanceSource` resolves to it; the scoped pair's "not enough to draw yet" states; support and extraction resolution through `makeResolver`, keeping unresolved and ambiguous values; the US3 citation states, including whether a source body links the assertion; gap lists with their rules and gap IDs; "not enough to draw yet" thresholds; completeness carried from coverage. Inputs are typed from the R2 and R5 field lists, so the batch starts against stubs before Batch A lands. The layout is global, so adding a concept moves every node; accept that for this effort and mark it with a `ponytail:` comment naming the upgrade (seed from the previous positions) if people lose their bearings. Charts are plain SVG: the kit's `Diagram` does not take fixed external coordinates.
- **Exit evidence:** `pkg/app/cli/init/testdata/ao-kb/site.test.mjs`, run by `TestAoKbScripts`, covering every R9 library case and printing layout timings at 21, 48, and 200 concepts.
- **Escalation:** none expected; a layout that is not stable under shuffled input is a defect, not a design question.

#### Batch H. View params survive tabs (PR 1, independent; can start any time)

- **Outcome:** R6a. A filtered list or a source's `assertion` target survives tab switches, tab close, persisted tabs, and reload.
- **Ownership:** `web/src/components/useNoteTabs.ts`, `noteTabStorage.ts`, the param hand-off in `NotesShell.tsx`, and their tests.
- **Work:** note tabs and the home location keep the active presentation's declared param values as read from the URL; `tabLocation`, `activate`, `close`, and tab restore apply them with `withViewParams`; persisted tabs store them, validated with `isViewParamValues` and bounded. Keep `NodeRef` and its fragment unchanged. Amend SPEC-0110.
- **Exit evidence:** unit tests for activation, close, restore, and persistence with and without params; Batch C's e2e covers filtered list to record tab and back, source with `assertion` to another tab and back, reload, copied URL, and Back and Forward.

#### Batch C. Site pages (PR 1, after B; its exit e2e after A and H)

- **Outcome:** US1 to US5 work in the browser on the legacy fixture. Advances R1, R2a, R3, R4, R8.
- **Ownership:** the `--kind-*` tokens in `web/src/tokens.css` (R3) with their palette and contrast results; starter views other than `data.tsx`, `lib/model.ts`, and `trace.tsx` (Batch A's), including `lib/knowledge.ts`, `concepts.tsx`, and `assertions.tsx`; the AO KB e2e harness (`web/scripts/run-ao-kb-e2e-server.mjs`, `web/playwright.ao-kb.config.ts`) and `web/tests/e2e-ao-kb/site.spec.ts`.
- **Work:**
  - New group views: `ao-kb.overview`, named "KB overview" (default; `attention.yaml` loses `default` per D2), `ao-kb.review`, `ao-kb.sources`, and `ao-kb.artifacts`. New node views: `ao-kb.concept`, `ao-kb.artifact`. Params exactly as R2a.
  - Existing views: `concepts.tsx`, `filterAssertions` in `lib/knowledge.ts`, and the Assertions list's concept chips switch to the library's membership, and the unused `CEILING` export goes once Batch A lands; `assertions` and `concepts` gain their R2a params; the Assertions list gains the US4 breakdown line; `assertion.tsx` gets inline facts and the US3 citation table; `source.tsx` gets the scoped pair, the `assertion` param, the US3 mention states, and separate extracted and citing lists.
  - Charts in `views/knowledge/site/` (`map.tsx`, `timeline.tsx`, `matrix.tsx`, `scoped.tsx`), sharing one tooltip-on-focus pattern and roving focus. The matrix uses the ARIA grid pattern R4 names.
  - The `--kind-*` tokens in `web/src/tokens.css` with R3's light and dark values (light from `--type-1`, `--type-10`, `--type-6`, and `--type-8`; dark as R3 names them), used by `knowledge.css` and checked with the dataviz skill's palette validator; matrix fills at the R4 caps checked for AA. Record each pair's contrast ratio and its simulated colour-vision distances in Execution Notes. Relation lines in `--link`.
  - Every label in SPEC-0135 R1's table; type sizes from R8.
  - Review hand-offs: `/ao-kb:curate` for membership, confidence, and kind-conflict gaps, `/ao-kb:process` for extraction gaps, `/ao-kb:suggest-review` for submissions and findings; source dates go to a follow-up request to AO. Verify the commands against the pinned AO before shipping the copy.
  - E2E harness: the server takes a fixture argument (default `testdata/ao-kb/booking`); for the legacy fixture it skips the model intermediate and includes `**/*.md` and `.kb/curation/*.md`. The Playwright config runs `site.spec.ts` against a second server on the next port after the suite's (4175 in CI, where the suite uses 4174), and the existing specs against booking as today.
- **Exit evidence:** `site.spec.ts` in `make web-e2e` covering R9's PR 1 journey, keyboard through each chart, and no console errors; the existing AO KB specs unchanged and passing; view validation passes.
- **Escalation:** a page that cannot meet R5 without bodies in the orientation query.

#### Batch D. Real-KB proof and PR 1 (after A, C, and H)

- **Outcome:** the site is shown to be true on three real KBs before review. Advances R1, R5, R9.
- **Work:** follow the AGENTS.md dogfood route with this checkout's built binary on disposable copies of a large legacy KB, a medium legacy KB, and a current-schema KB (the one with 48 concepts, no Related Concepts, mostly `principle` assertions, and nested source folders that share a long parent), each in a scratch directory with embeddings off and provider keys absent. Run the legacy copies through the setup skill's knowledge-only route. Check every caption's numbers against the library's output, every R1 label, and the matrix-to-list agreement; edit and delete one record in a copy and confirm the KB overview's counts follow after reindex; review screenshots locally in both appearances, never committed. Delete the copies afterward. Then the PR 1 gates and the SPEC-0133, SPEC-0105, and SPEC-0110 amendments.
- **Exit evidence:** in Execution Notes, per KB: the exact orientation query and scope-read strings (from `lib/queries.ts`), the record inventory, root coverage statuses, the five samples and medians, the uncapped evidence read's median for Concepts, Assertions, and Attention, and the setup commands' results; orientation query under one second and scope reads under 250 ms median on each KB; PR 1 merge-ready under review-and-approval policy.
- **Escalation:** a read over its threshold, or a label that cannot be made true from the data. Both return to Drew with measurements.

#### Batch E. Sidebar panel platform (PR 2)

- **Gate first:** a `foundation-review` of SPEC-0135 R6 covering mount spelling, the catalog's `sidebarViewId`, frame context, the activation rule (effective tree and owning-note fallback), frame lifetime, the location message, and states. It uses the Agentic Engineering site's journeys (spec, story, criterion, both effort forms, effort material, reference doc, policy note, unrelated record, Back) as test cases. Dependent work waits for it. It must show that:
  - nothing in R6 depends on a group name or a starter combination: activation, conflict resolution, persistence, and frame context use only the mount's group and the vault's effective display tree, over AO KB alone, AO KB with Agentic Engineering and compounding, a vault-local type in the panel's group, two sidebar views for one group, and none;
  - one group per mount serves both sites' journeys under D6's answer, with reference docs showing their own group's panel and untyped policy notes none;
  - a later `groups` list could be added without changing single-group frame identity, persistence keys, or activation.
- **Outcome:** any module can contribute one sidebar panel per display group. Advances R6, R8, US7's host half.
- **Ownership:** the `--chrome-link` token in `web/src/tokens.css` and `web/src/lib/themes.test.ts`, which checks it against every chrome background (R6); `pkg/ontology/viewconfig` (mount kind, `MatchesMount`, validation); `pkg/app/views` (catalog and targets exclude sidebar views from choices and set the group target's `sidebarViewId`); `pkg/app/web/openapi.yaml` (`ViewTarget.sidebarViewId`) and `web/src/api/generated.ts` through `make web-generate`; `pkg/app/web/custom_view_context.go` and a new file beside `custom_views.go` for group-context invocation of a sidebar view (`custom_views.go` is at 491 lines against the 500-line gate); `pkg/app/userstate/scope.go` (confirm the group-context preference scope needs no change); `web/src/views/context.ts`; `web/src/components/SidebarPanel.tsx` (new) inside `NotesLeftRail.tsx` under `WorkbenchSidebar.tsx`; `PanelResizeHandle.tsx` (axis); `CustomViewFrame.tsx` (location message, panel-frame acceptance, replaced-frame rejection, optional search folder); the kit location hook and `openSearch`; the shortcut bridge from the panel frame.
- **Exit evidence:** Go viewconfig, catalog (`sidebarViewId` and the two-view warning), and invocation tests; web unit tests for activation over the display trees of the AO KB starter alone and of the AO KB, Agentic Engineering, and compounding starters installed together (AO's four knowledge types and an embedded conflict entry; Engineering's spec, story, criterion, both effort forms, effort material, and reference doc; a context shard; a vault-local type in Knowledge; an untyped note; an unrelated type), persistence per vault and group, minimise, cover, Escape, focus moves before hiding, inert regions, divider keys on both axes, hidden-not-destroyed lifetime, and message origin and source checks; e2e for presence inside a group and absence outside, cover and restore, reload, history return, and an outer sidebar hidden by the person staying hidden. Amend SPEC-0128 and SPEC-0110, including the exception to SPEC-0110's "Inactive custom frames MUST unmount", and update `views.md`, `web/CONTEXT.md`, `pkg/ontology/viewconfig/CONTEXT.md`, and `DESIGN.md`.
- **Escalation:** a journey in scope that needs a panel across groups. Drew allowed one where needed (2026-11-08), so the foundation review may add the `groups` list with the precedence SPEC-0135 R6 states; record the journey that needs it.

#### Batch F. Starter panel view (PR 2, after E)

- **Outcome:** US7 complete for AO KBs.
- **Work:** `views/knowledge/sidebar.{yaml,tsx}` with the KB title, Find by title (Enter opens the first match; "Search all text" sends open-search with no folder), the site links and counted lists, the locator from the site library with the record's scope read, and the Recent list built from location messages and kept in the frame's `sessionStorage`.
- **Exit evidence:** e2e extending `site.spec.ts` through the panel; the Batch D copies rechecked for the panel.

#### Batch G. Custom-first view switcher (PR 2, independent; can start any time)

- **Outcome:** US8 and R7.
- **Ownership:** `web/src/views/ViewSelector.tsx`, its test, and CSS.
- **Work:** order custom then standard, ranking by the choice's `custom` flag, not its renderer (bundled and ejected standard views also render as custom); measure with a ResizeObserver including trailing controls; overflow into "More"; keep the selected view visible; keep default marking, reset, and keyboard behaviour. Amend SPEC-0110's order sentence and any restating text in SPEC-0111, SPEC-0112, SPEC-0117, and SPEC-0126.
- **Exit evidence:** unit tests with stubbed widths for order, overflow, selection in overflow, focus after a resize moves the focused choice into overflow; one e2e at wide and narrow widths.

### Fixtures

All synthetic and never containing real client text.

- `testdata/ao-kb-legacy/` (new): `.kb/config/vault.md` at schemaVersion 5 with `profile` and `productMode: single` (tests copy it with 4); `.kb/config/manifest.json` with a toolkit stamp and no `siteEngine`; assertions with `assertionKind` across every legacy name, one `principle` (Intent and rules), and one `lesson` (Other kinds); confidence values `validated`, `hypothesis`, `guess`, and `high`; one assertion with both `kind` and `assertionKind` differing; sources in three folders with `sourceDate`, `meetingDate`, `date`, an integer `date` (expected to read as an unreadable recorded date, never as absent), a file-name date only, and none; one source with `format`; extraction IDs including one that resolves to nothing, and one dated source whose extractions all fail to resolve; citations covering each US3 state, including a source whose body links its citing assertion and one whose body never does; one assertion that cites a source only through `provenance.source` (its AO ID), with no link to it; one source linked from the Sources sections of 21 assertions; concepts with Related Concepts, one prose-only concept link that must not draw, one concept body linking an assertion that does not link back, and one concept with no members; one assertion in no concept; artifacts in two folders with `supportingAssertions` as AO IDs that differ from their file names, one ambiguous and one unresolved value, and one artifact with both keys disagreeing; two artifacts with the same basename in different folders; one underscore-named artifact; `_moc.md` and two `MOCs/` notes; under `_improvements/`, flat legacy submissions with statuses `pending`, `ready`, and `applied`, a nested `<slug>/<slug>.md` with `pending` and one undocumented status, a flat `fnd-*.md` finding with `status: open`, a status-less guide, an `_archive/` file, a `_pending-attachments/` file, and an `attachments/` file; `.kb/curation/` with `_conflicts.md`, one document with `updatedAt`, and one without.
- `testdata/ao-kb/booking` (existing, schemaVersion 8): files unchanged, so AO's index oracle and `TestAOKBRecipesAndIndexParity` stand. `testdata/ao-kb-model` and `testdata/ao-kb-product` keep passing; the product fixture gains one account-root and one product-root map of content, a submission in each, and a curation document, and the exported orientation query runs there to check product admission.
- Generated in each test's temporary directory: sparse inputs for every "not enough to draw yet" state (library tests only) and a 600-assertion KB for completeness.

### Parallelism and ownership

| Batch | Starts | Blocks | Lane |
| --- | --- | --- | --- |
| A | after approval | D | starter ontology, setup route, query module, kit coverage |
| B | after approval | C | starter library |
| H | after approval | D | web tabs |
| C | after B; its exit e2e after A and H | D | starter views except A's three files, AO KB e2e harness |
| D | after A, C, and H | PR 1 | coordinator |
| E | after the foundation review | F | viewconfig, catalog and OpenAPI, web invocation, web shell, kit |
| F | after E | PR 2 | starter panel view |
| G | any time | PR 2 | web selector |

Lanes own disjoint files: in the starter's views, Batch A owns `data.tsx`, `lib/model.ts`, `trace.tsx`, and `lib/queries.ts`, Batch B owns `lib/site.ts` (and `lib/kinds.ts`), and Batch C owns the rest. Each lane runs web E2E on its own port and cleans up its worktree and scratch copies when done. The coordinator inspects integrated changes before each pull request.

### Review gates

- Plan approval by Drew in chat, recorded in `plan-approved-by`. D1 to D8 and the panel-spanning question were decided on 2026-11-08; D8 shapes the Agentic Engineering effort and does not block this plan.
- Foundation review before Batch E.
- Each pull request: `make check` in the loop; `make check-full` and `make web-e2e` (the AO KB suite included); `make build`, `rzm init`, and `rzm init --check` exiting 0; `make test-content` for changed starter guidance; `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift`; `greptile review --json` at 5/5 after every push; a full-diff self-review; CI green. One `CHANGELOG.md` line per user-visible change.

## Plan Approval

Not yet approved.

## Original Intended Delivery

Recorded at approval.

## Actual Delivered

Nothing delivered yet.

## Execution Notes

- 2026-11-09T00:38:28Z: Effort drafted from the council's four reviews of the prototype. The measurements were taken on a disposable copy of the largest real KB, which was then deleted; no KB was modified and nothing was sent to an embedding provider.
- 2026-11-09T01:20:00Z: Revised after the council's second round (Sol 7/8, Astra 8/7, Opus 7/7, Fable 8/8). Each blocker was checked in code or data before revision; the resolution ledger lives with the council's working files. The orientation query, scope reads, and today's uncapped query were re-measured on a new disposable copy of the largest real KB with this checkout's binary, embeddings off and provider keys absent, and the copy was deleted. Real KBs were read for counts and frontmatter keys only.
- 2026-11-09T01:53:36Z: Revised after the council's third round (Sol 8/9, Astra 8/9, Opus 8/8, Fable 8/9) and Drew's answers on D6, D7, and panels across groups. Each blocker was checked in code before revision: the neighbourhood default and declared-relation `first` semantics, provenance-only citations, the receipt inventory, the check's rows, the catalog schema, file ownership across batches, and Fable's contrast and colour-vision figures (recomputed and reproduced). No KB was read or measured in this round.

## Deviations

None yet.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

- Ask AO KB to document an optional source event-date field (the `sourceDate` spelling one legacy KB already uses) and to capture it during ingestion.
- A labelled "mentions" layer for concept-to-concept prose links, as a later SPEC-0133 amendment, if the current-schema KB's map without lines proves too thin.

## Status

Planned. Next: Drew approves the plan; then Batches A, B, H, and G can start in parallel.
