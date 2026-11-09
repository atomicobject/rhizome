---
type: EffortNote
id: EFF-2026-11-08-19-38
aliases: [EFF-2026-11-08-19-38]
name: "AO KB Knowledge site"
created-at: "2026-11-09T00:38:28Z"
status: planned
summary: "Deliver SPEC-0135 on the SPEC-0136 experiences platform: complete legacy-aware data and setup, the site library, the Knowledge experience (KB overview, Review, lists, record pages on RecordPage, the panel), and the custom-first view switcher, in one pull request with EFF-2026-11-09-09-16."
governing-specs:
  - "[[docs/specs/experience/ao-kb-knowledge-site]]"
---

# AO KB Knowledge site

## Scope

Deliver [[ao-kb-knowledge-site|SPEC-0135]] as an experience on [[experiences|SPEC-0136]], in one pull request with [[2026-11-09-09-16-experiences|EFF-2026-11-09-09-16]], which delivers the platform. This effort owns complete and legacy-aware data reads, the knowledge-only setup route for legacy KBs, the site library, the Knowledge experience's pages and panel content, and the custom-first view switcher. Tab-retained view params (R6a) are delivered by the platform effort's host lane, which owns those files. The effort closes in that pull request.

Excluded, as the spec's Non-Goals state: KB writes, AO judgments, inferred links or dates, prose concept-to-concept edges, `CLAUDE.md` identity, scheduled or saved analysis, 3D, people views, product-model pages and model bindings below schemaVersion 8, legacy product-mode accounts, archived submissions, editing, export, a site router or module manifest, and migrating legacy KBs.

## Spec Set (Frozen)

- [[docs/specs/experience/ao-kb-knowledge-site|SPEC-0135]] at the revision rebased on SPEC-0136 and reviewed by the council on 2026-11-09, approved with this plan. Drew decided D1 to D8 on 2026-11-08 (recommended options; his own wording for D6 and D7) and moved `principle` into Intent and rules on 2026-11-09. The spec moves from `proposed` to `active` at that approval.

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
- [ ] R6 the Knowledge experience and its panel: Batch C (`experience.yml`), Batch F (panel view); the platform is EFF-2026-11-09-09-16.
- [ ] R6a view params survive tabs: EFF-2026-11-09-09-16 Batch P6.
- [ ] R7 switcher order: Batch G.
- [ ] R8 accessibility and design: Batches C, F, G.
- [ ] R9 verification: every batch; real-KB pass in Batch D.
- [ ] US1 to US5: Batches B and C (US4's tab return with P6). US6: Batch A. US7: Batch F on the platform's P7. US8: Batch G.
- [ ] Amendments: SPEC-0133 (Batches A and C), SPEC-0105 (Batch A), SPEC-0110 (Batch G and P6). The SPEC-0128 amendment is the platform effort's.

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

- **The site is an experience.** `module/views/knowledge/experience.yml` declares it (SPEC-0136 R1); the panel is the view `ao-kb.panel` with `mount: { kind: panel }`. Which subjects show the panel, and every platform behaviour, is SPEC-0136's. Reference and policy documents are a separate section (D6); an untyped policy note shows Folder.
- **Panel draws its own locator.** The Agentic Engineering prototype's locator is an effort timeline and the AO KB's is a topic map, so the contents must be site-supplied. The panel is an ordinary framed custom view using the same site library as the pages, so positions match by construction.
- **File-name dates are plotted, marked, and counted.** A leading `YYYY-MM-DD` in the file name draws a hollow mark labelled "Date in file name"; recorded dates draw filled marks; title dates are not used; nothing is trimmed. Without them the largest KB's axis is empty (0 of 115 recorded dates; 68 file-name dates).
- **One orientation query plus per-record scope reads, both through SPEC-0136's read cache.** Putting citations and artifact references in the whole-KB query nearly triples its cost (291 ms to 807 ms on the largest KB); only record pages need them, and per-record reads cost 3 to 64 ms.
- **Membership is read from the concept side** for speed; per D1 the shared function takes both directions as one deduplicated set.
- **The site's home page is named "KB overview"**, because the bundled "Overview" already appears in the group's switcher.
- **Legacy KBs set up knowledge-only.** Below schemaVersion 8 the model stage is `not applicable`, the receipt carries the site source signature computed in Node, and no Python dependency is needed.
- **One receipt inventory for every schema.** The site source signature follows the types' and admission's path rules on both routes, so a new document or an underscore-named artifact stales the index. Existing schemaVersion 8 receipts expire once after upgrade.
- **Kind-group colours and lines.** Magenta `--type-10` replaces violet or indigo for Intent and rules, and relationship lines use `--link`, because the old choices failed R3's colour-vision rule and R8's 3:1 rule (computed in SPEC-0135 R3). Drew chose magenta on 2026-11-08.
- **Existing pages read completely.** Concepts, Assertions, and Attention lose their caps and read about 2 s and 6.2 MiB on the largest KB; complete counts are worth that on those pages, and Batch D reports it per KB.

### Batches

#### Batch A. Complete, legacy-aware knowledge reads and setup

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
  - Kit (`web/kit/index.tsx`, `web/src/api/publicGraphQLTypes.ts`): carry `extensions` through and expose typed-root coverage as an accessor keyed by the result's data object, as the kit's tolerated-errors pattern already does, so `useGraphQL`'s return shape is unchanged and coverage stays with the data it describes when structural sharing keeps an old object. This commit is integrated before the platform's P5 starts, and P5 owns `extensions` plumbing in `index.tsx` from then on. The kit reference documentation goes to the platform's P9.
  - Fixtures, all synthetic (see Fixtures): `testdata/ao-kb-legacy/` new; one map of content, one submission, and one curation document added to `testdata/ao-kb-product`.
- **Exit evidence:** `pkg/app/cli/init` ontology and starter tests for every R9 ontology case, including the knowledge-only refresh, check, index, and check sequence on the legacy fixture with uv absent from PATH, the site source signature cases on both schema routes, and the exported orientation query run on the legacy, booking, and product fixtures; `testdata/ao-kb/expected-knowledge.graphql` and the product fixture's expectations updated and reviewed as changed contracts, with `TestAOKBRecipesAndIndexParity` passing unchanged because booking's files do not change; a kit unit test for coverage; integration tests (`-tags=integration`) because fixtures index; `make build`, `rzm init`, and `rzm init --check` exiting 0.
- **Escalation:** a needed core change to `@field`, `@neighbors`, or matchers, or AO tooling that contradicts the retired-name table.

#### Batch B. Site library (parallel with A)

- **Outcome:** one pure module computes everything the site draws. Advances R3, R4, R5, and US1 to US5's data.
- **Ownership:** `module/views/knowledge/lib/site.ts` (under 500 lines; split kind groups into `lib/kinds.ts` if needed). No UI, no queries.
- **Work:** membership, the only implementation, which the Concepts and Assertions pages switch to in Batch C; kind groups with legacy names; stored-confidence buckets as an explicit set; the kind conflict; the Assertions list breakdown counts (US4); deterministic layout ported from the prototype's arithmetic with sorted inputs and estimated label widths, the height that grows with the concept count, the separation passes, shortened names and the unlabelled count, the 12px arc threshold, and the map's arrow order; timeline row labels that keep the last folder name whole; date placement with basis, including the minimum-area neutral mark for a dated source with no resolved extraction; source colour by strict majority; matrix counts with once-per-assertion totals and the linear tint scale from the 12% floor; scope sets for concept, source, and artifact from scope-read results, the artifact's sources being those its referenced and supporting assertions cite, the source set uniting `linkingAssertions` with assertions whose `provenanceSource` resolves to it; the scoped pair's "not enough to draw yet" states; support and extraction resolution through `makeResolver`, keeping unresolved and ambiguous values; the US3 citation states, including whether a source body links the assertion; gap lists with their rules and gap IDs; "not enough to draw yet" thresholds; completeness carried from coverage. Inputs are typed from the R2 and R5 field lists, so the batch starts against stubs before Batch A lands. The layout is global, so adding a concept moves every node; accept that for this effort and mark it with a `ponytail:` comment naming the upgrade (seed from the previous positions) if people lose their bearings. Charts are plain SVG: the kit's `Diagram` does not take fixed external coordinates.
- **Exit evidence:** `pkg/app/cli/init/testdata/ao-kb/site.test.mjs`, run by `TestAoKbScripts`, covering every R9 library case and printing layout timings at 21, 48, and 200 concepts.
- **Escalation:** none expected; a layout that is not stable under shuffled input is a defect, not a design question.

#### Batch C. Site pages (after B, the platform's P4 and P5, and the foundation review; its exit e2e after A, P3, and P6)

- **Outcome:** US1 to US5 work in the browser on the legacy fixture. Advances R1, R2a, R3, R4, R8.
- **Ownership:** the `--kind-*` tokens in `web/src/tokens.css` (R3), lines disjoint from P7's `--chrome-link`, with their palette and contrast results; starter views other than `data.tsx`, `lib/model.ts`, `trace.tsx`, and `lib/queries.ts` (Batch A's), `lib/site.ts` and `lib/kinds.ts` (Batch B's), and `panel.{yaml,tsx}` (Batch F's), including `lib/knowledge.ts`, `concepts.tsx`, and `assertions.tsx`; the AO KB e2e harness (`web/scripts/run-ao-kb-e2e-server.mjs`, `web/playwright.ao-kb.config.ts`) and `web/tests/e2e-ao-kb/site.spec.ts`.
- **Work:**
  - `module/views/knowledge/experience.yml` as SPEC-0135 R6 states but without the `panel` line, with `tailwind: false` (Batch F adds `panel` with its view and owns the file from then on); every Knowledge view exports its component as the module default; record pages use `RecordPage` and show the record before the orientation query finishes; batched scope reads use `readNodes`.
  - New group views: `ao-kb.overview`, named "KB overview" (default; `attention.yaml` loses `default` per D2), `ao-kb.review`, `ao-kb.sources`, and `ao-kb.artifacts`. New node views: `ao-kb.concept`, `ao-kb.artifact`. Params exactly as R2a.
  - Existing views: `concepts.tsx`, `filterAssertions` in `lib/knowledge.ts`, and the Assertions list's concept chips switch to the library's membership, and the unused `CEILING` export goes once Batch A lands; `assertions` and `concepts` gain their R2a params; the Assertions list gains the US4 breakdown line; `assertion.tsx` gets inline facts and the US3 citation table; `source.tsx` gets the scoped pair, the `assertion` param, the US3 mention states, and separate extracted and citing lists.
  - Charts in `views/knowledge/site/` (`map.tsx`, `timeline.tsx`, `matrix.tsx`, `scoped.tsx`), sharing one tooltip-on-focus pattern and roving focus. The matrix uses the ARIA grid pattern R4 names.
  - The `--kind-*` tokens in `web/src/tokens.css` with R3's light and dark values (light from `--type-1`, `--type-10`, `--type-6`, and `--type-8`; dark as R3 names them), used by `knowledge.css` and checked with the dataviz skill's palette validator; matrix fills at the R4 caps checked for AA. Record each pair's contrast ratio and its simulated colour-vision distances in Execution Notes. Relation lines in `--link`.
  - Every label in SPEC-0135 R1's table; type sizes from R8.
  - Review hand-offs: `/ao-kb:curate` for membership, confidence, and kind-conflict gaps, `/ao-kb:process` for extraction gaps, `/ao-kb:suggest-review` for submissions and findings; source dates go to a follow-up request to AO. Verify the commands against the pinned AO before shipping the copy.
  - E2E harness: the server takes a fixture argument (default `testdata/ao-kb/booking`); for the legacy fixture it skips the model intermediate and includes `**/*.md` and `.kb/curation/*.md`. The Playwright config runs `site.spec.ts` against a second server on the next port after the suite's (4175 in CI, where the suite uses 4174), and the existing specs against booking as today.
- **Exit evidence:** `site.spec.ts` in `make web-e2e` covering R9's journey, keyboard through each chart, and no console errors; the existing AO KB specs unchanged and passing; view validation passes.
- **Escalation:** a page that cannot meet R5 without bodies in the orientation query.

#### Batch D. Real-KB proof (with the platform's P10, as one pass over the same disposable copies)

- **Outcome:** the site is shown to be true on three real KBs before review. Advances R1, R5, R9.
- **Work:** follow the AGENTS.md dogfood route with this checkout's built binary on disposable copies of a large legacy KB, a medium legacy KB, and a current-schema KB (the one with 48 concepts, no Related Concepts, mostly `principle` assertions, and nested source folders that share a long parent), each in a scratch directory with embeddings off and provider keys absent. Run the legacy copies through the setup skill's knowledge-only route. Check every caption's numbers against the library's output, every R1 label, and the matrix-to-list agreement; edit and delete one record in a copy and confirm the KB overview's counts follow after reindex; review screenshots locally in both appearances, never committed. Delete the copies afterward. Then the pull request gates and the SPEC-0133, SPEC-0105, and SPEC-0110 amendments.
- **Exit evidence:** in Execution Notes, per KB: the exact orientation query and scope-read strings (from `lib/queries.ts`), the record inventory, root coverage statuses, the five samples and medians, the uncapped evidence read's median for Concepts, Assertions, and Attention, and the setup commands' results; orientation query under one second and scope reads under 250 ms median on each KB; the pull request merge-ready under review-and-approval policy.
- **Escalation:** a read over its threshold, or a label that cannot be made true from the data. Both return to Drew with measurements.

#### Batch F. Knowledge panel view (after A, B, C, and the platform's P4, P5, and P7)

- **Outcome:** US7 complete for AO KBs.
- **Work:** `views/knowledge/panel.{yaml,tsx}` (`ao-kb.panel`, `mount: { kind: panel }`) with the KB title, Find by title (Enter opens the first match; "Search all text" sends open-search with no folder), the site links and counted lists, the locator from the site library with the record's scope read, the `panel` line in `experience.yml`, and the Recent list built from `useViewLocation` and kept in `sessionStorage` keyed by vault and experience (R6), so it survives eviction within the window.
- **Exit evidence:** its own `web/tests/e2e-ao-kb/panel.spec.ts` through the panel on Batch C's harness, including Recent after the panel frame is evicted and recreated and two windows keeping separate Recent lists; view validation passes with the `panel` line; the Batch D copies rechecked for the panel.

#### Batch G. Custom-first view switcher (independent; can start any time)

- **Outcome:** US8 and R7.
- **Ownership:** `web/src/views/ViewSelector.tsx`, its test, its CSS, and its own e2e spec `web/tests/e2e/view-switcher.spec.ts`. The SPEC-0110 and related order sentences go to the platform's P9.
- **Work:** order custom then standard, ranking by the choice's `custom` flag, not its renderer (bundled and ejected standard views also render as custom); measure with a ResizeObserver including trailing controls; overflow into "More"; keep the selected view visible; keep default marking, reset, and keyboard behaviour. The platform's P9 amends SPEC-0110's order sentence and any restating text in SPEC-0111, SPEC-0112, SPEC-0117, and SPEC-0126.
- **Exit evidence:** unit tests with stubbed widths for order, overflow, selection in overflow, focus after a resize moves the focused choice into overflow; one e2e at wide and narrow widths.

### Fixtures

All synthetic and never containing real client text.

- `testdata/ao-kb-legacy/` (new): `.kb/config/vault.md` at schemaVersion 5 with `profile` and `productMode: single` (tests copy it with 4); `.kb/config/manifest.json` with a toolkit stamp and no `siteEngine`; assertions with `assertionKind` across every legacy name, one `principle` (Intent and rules), and one `lesson` (Other kinds); confidence values `validated`, `hypothesis`, `guess`, and `high`; one assertion with both `kind` and `assertionKind` differing; sources in three folders with `sourceDate`, `meetingDate`, `date`, an integer `date` (expected to read as an unreadable recorded date, never as absent), a file-name date only, and none; one source with `format`; extraction IDs including one that resolves to nothing, and one dated source whose extractions all fail to resolve; citations covering each US3 state, including a source whose body links its citing assertion and one whose body never does; one assertion that cites a source only through `provenance.source` (its AO ID), with no link to it; one source linked from the Sources sections of 21 assertions; concepts with Related Concepts, one prose-only concept link that must not draw, one concept body linking an assertion that does not link back, and one concept with no members; one assertion in no concept; artifacts in two folders with `supportingAssertions` as AO IDs that differ from their file names, one ambiguous and one unresolved value, and one artifact with both keys disagreeing; two artifacts with the same basename in different folders; one underscore-named artifact; `_moc.md` and two `MOCs/` notes; under `_improvements/`, flat legacy submissions with statuses `pending`, `ready`, and `applied`, a nested `<slug>/<slug>.md` with `pending` and one undocumented status, a flat `fnd-*.md` finding with `status: open`, a status-less guide, an `_archive/` file, a `_pending-attachments/` file, and an `attachments/` file; `.kb/curation/` with `_conflicts.md`, one document with `updatedAt`, and one without.
- `testdata/ao-kb/booking` (existing, schemaVersion 8): files unchanged, so AO's index oracle and `TestAOKBRecipesAndIndexParity` stand. `testdata/ao-kb-model` and `testdata/ao-kb-product` keep passing; the product fixture gains one account-root and one product-root map of content, a submission in each, and a curation document, and the exported orientation query runs there to check product admission.
- Generated in each test's temporary directory: sparse inputs for every "not enough to draw yet" state (library tests only) and a 600-assertion KB for completeness.

### Parallelism and ownership

| Batch | Starts | Blocks | Lane |
| --- | --- | --- | --- |
| A | after approval; its kit coverage commit lands first, before the platform's P5 | D | starter ontology, setup route, query module, kit coverage |
| B | after approval | C | starter library |
| C | after B, P4, P5, and the foundation review; its exit e2e after A, P3, and P6 | D | starter views except A's and F's files, AO KB e2e harness and `site.spec.ts` |
| F | after A, B, C, P4, P5, and P7 | D | `panel.{yaml,tsx}` and `panel.spec.ts` |
| G | any time | D | web selector |
| D | with P10, after A, C, F, and G | the pull request | coordinator |

Lanes own disjoint files: in the starter's views, Batch A owns `data.tsx`, `lib/model.ts`, `trace.tsx`, and `lib/queries.ts`, Batch B owns `lib/site.ts` (and `lib/kinds.ts`), Batch F owns `panel.{yaml,tsx}`, `panel.spec.ts`, and the `panel` line of `experience.yml`, and Batch C owns the rest. Each lane runs web E2E on its own port and cleans up its worktree and scratch copies when done. The coordinator inspects integrated changes before each pull request.

### Review gates

- Plan approval by Drew in chat, recorded in `plan-approved-by`. D1 to D8 and the panel-spanning question were decided on 2026-11-08; D8 shapes the Agentic Engineering effort and does not block this plan.
- The platform effort's foundation review before Batch C builds on the experience catalog and kit protocol.
- The pull request: `make check` in the loop; `make check-full` and `make web-e2e` (the AO KB suite included); `make build`, `rzm init`, and `rzm init --check` exiting 0; `make test-content` for changed starter guidance; `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift`; `greptile review --json` at 5/5 after every push; a full-diff self-review; CI green. One `CHANGELOG.md` line per user-visible change.

## Plan Approval

- plan-approved-by:: Drew Colthorp, 2026-11-09, in chat: "OK. Do it all /full-stop", after "Once the council is satisfied and you feel good about it, use subagents to build the whole thing". Approved scope: SPEC-0135 as rebased on SPEC-0136 and this plan, delivered with EFF-2026-11-09-09-16 in one pull request left for Drew to merge.

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

Planned and approved. Next: the council's review of the revised specs, then Batches A, B, and G alongside the platform's P1 to P5.
