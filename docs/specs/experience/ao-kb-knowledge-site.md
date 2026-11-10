---
type: ExperienceSpec
id: SPEC-0135
aliases:
  - SPEC-0135
  - AO KB Knowledge site
summary: "Turns the AO KB starter's Knowledge group into a navigable site for newcomers and maintainers: a KB overview with a topic map, sources by date, and a topic-by-kind matrix; record pages that show where a record sits; a Review page of live gaps; a sidebar panel; and a view switcher that puts custom views first. Every mark comes from authored AO data, labelled for what it is, and legacy KBs on AO schema 4 to 7 read and set up without migrating."
spec-status: proposed
last-updated: 2026-11-09
---

# AO KB Knowledge site

## Summary

The AO KB starter ([[format-neutral-embedded-nodes-and-ao-kb-adoption|SPEC-0133]]) gives an AO KB repository a Knowledge workspace with Attention, Concepts, Assertions, and Trace pages and Assertion and Source record pages. Drew asked for more: views that read as a site rather than a stack of tables, visuals through the whole site and not only on a home page, a sidebar panel carrying the site's navigation, and custom views that are not hidden in a dropdown. The audience is people working in a KB, maintaining it, or arriving from outside and trying to understand what it holds, across many KBs on different AO schema versions.

A prototype built from three real KBs and reviewed over three rounds by a four-model council (Sol, Astra, Opus, Fable) showed the shape works: one topic map, one time axis, one record template, and a panel. The council also found labels that claimed more than the data records, legacy KBs the starter cannot set up, data shapes the first draft missed, and platform rules that did not yet serve a second site. This spec keeps the shape and fixes each of those.

The site is one experience ([[experiences|SPEC-0136]]): the starter's Knowledge views and panel in one folder with an `experience.yml`, run in one page frame per window with a retained page per tab, with reads shared through SPEC-0136's index-generation cache. SPEC-0136 owns the platform (experiences, frame reuse, the panel slot, the read cache, the record-page shell); this spec owns the Knowledge pages, their data, and three smaller platform additions: typed-root coverage in the view kit, workbench tabs that keep a view's declared params, and custom-first ordering in the view switcher. It amends SPEC-0133, [[unified-view-contract|SPEC-0110]], and [[custom-view-apps|SPEC-0105]] as listed under Amendments.

## Terms

- **Membership**: an assertion belongs to a concept when either note links the other with an authored link. One shared function computes it, and every page, list, and count uses it. Today the Concepts view counts only assertion-to-concept links; this spec counts both directions (decision D1).
- **Related Concepts**: the links in a concept's `## Related Concepts` section, the only concept-to-concept relationship the starter projects. Links elsewhere in a concept's prose are not relatedness.
- **Kind group**: a display grouping of assertion kinds defined in the starter (R3). It is a Rhizome presentation convention, not an AO classification, and never a judgment of truth or strength.
- **Stored confidence**: the assertion's authored `confidence` value, shown verbatim.
- **Recorded date**: a source date read from the source's frontmatter. **File-name date**: a `YYYY-MM-DD` at the start of a source's file name. Neither says when something was learned.
- **Gap**: a structural observation Rhizome can prove from indexed records, stated with its rule, such as an assertion that no concept links and that links no concept. A gap is not a defect, a recommendation, or an AO verdict.
- **Orientation query**: the one body-free, whole-KB query every site page and the panel run (R5).
- **Scope read**: the small query a record page, or the panel on a record page, runs for the opened record only (R5).
- **Site**: the Knowledge experience (SPEC-0136): the pages, record presentations, and sidebar panel the starter declares in `module/views/knowledge/experience.yml`. It is presentation only, never ontology membership or an access boundary. Which subjects show its panel is defined by SPEC-0136 R6.

## Goals

- A newcomer can open a KB and, within a minute, see what it is about, how its topics connect, where its sources fall in time, and what it holds on each topic, then follow any of those into a record.
- A maintainer can see what is missing or unreviewed, with counts that match the records, and hand each item to the AO KB skill that acts on it.
- Every number, mark, and label describes authored data or a stated computation over it. Nothing claims an AO judgment, a passage that was not recorded, or a date meaning the KB does not record.
- The same KB reads correctly on AO schema versions 4 through 8 and with either kind vocabulary, and a single-product legacy KB completes setup through the documented route.
- Every visual has a keyboard path, a text equivalent, and an honest state when there is not enough data to draw it.
- Navigation from the sidebar panel, the KB overview, and record pages lands on the same records with the same counts, and a filtered list returned to through its tab shows the same filter.

## Non-Goals

- Writing to the KB. AO KB owns every authored file (SPEC-0133 R5).
- Computing AO judgments: confidence bands, weights, horizons, freshness, importance, or "shakiest" rankings.
- Inferring links, topics, dates, or relatedness from text, tags, similarity, or link centrality.
- Concept-to-concept edges from links outside `## Related Concepts`. A labelled "mentions" layer may follow in a later SPEC-0133 amendment.
- Parsing `CLAUDE.md` or other instruction files for KB identity.
- A scheduled task, saved analysis, or background job for the site's counts or layout.
- 3D views, people or speaker views, product-model pages, editing, export, or publishing. AO's own site engine owns publication.
- A site router or module manifest. The experience's `experience.yml` (SPEC-0136) is the only declaration.
- Migrating legacy KBs, and product-model bindings below schemaVersion 8. AO's own migration remains the way to upgrade a KB. Legacy product-mode accounts are not supported; none was observed.
- Listing archived submissions (`_improvements/_archive/`).

## User Stories

### US1 - Orient on a KB's overview

- id:: ^SPEC-0135-US1
- summary:: Open the Knowledge group and see what the KB is, where to start, how its topics connect, when its sources are dated, and what it holds on each topic.
- status:: ready

#### Acceptance Criteria

- Opening the Knowledge group shows the "KB overview" page by default. It is named "KB overview" because the group already offers the bundled "Overview". Its header names the KB with the title of `_moc.md`, dropping a leading "Map of Content" and the separator after it, or the vault name when there is none. The recorded AO `profile`, when the knowledge manifest carries one (R2), and the counts below join the header's description, separated by " · ", because the house header allows no second line of facts.
- The header shows whole-KB counts of assertions, concepts, sources, and artifacts from the orientation query. When any root's coverage is not `complete`, the count says so and names what was not read.
- "Start here" lists `_moc.md` first, then each map of content under `MOCs/` by title, and each opens its record.
- The topic map, sources by date, and topic-by-kind matrix appear in that order, each with a caption that states its inputs (R4).
- Beside the map, the page states how many assertions belong to at least one concept, out of the total, and links the remainder to the Assertions list.
- A KB with too little data for any visual shows that visual's "not enough to draw yet" state instead of an empty or hidden area.
  verification:: library tests for every threshold on generated sparse inputs; e2e on the legacy fixture; dogfood on the three real KB copies

### US2 - See where a concept, source, or artifact sits

- id:: ^SPEC-0135-US2
- summary:: Open a concept, source, or artifact and see it placed on the same topic map and time axis as the KB overview, with its own content first in reading order.
- status:: ready

#### Acceptance Criteria

- Concept, source, and artifact pages open with their title, headline, and authored body, followed by a scoped pair: a neighbourhood cropped from the KB overview's map at the same positions, and a one-row strip on the overview's time axis.
- Highlighting follows R4's highlight rule. A concept's strip highlights the sources cited by its member assertions. A source's map highlights the concepts of the assertions that cite it, and its strip rings the source. An artifact's map highlights the concepts it references and the concepts of the assertions it references or records as support, and its strip highlights the sources those assertions cite. Each page gets these sets from its scope read (R5).
- Each strip says how many dated sources it is drawn against and how many sources in its scope have no date, and lists the undated ones as links.
- A concept page lists its member assertions under "What the KB says about it", grouped by kind group, showing each assertion's kind and stored confidence.
- An artifact page lists the assertions recorded in `supported_by` (or the legacy `supportingAssertions`) as "Supported by (recorded)", separately from other assertions it links, listed as "Assertions it links". Recorded support values that resolve to no assertion, or to more than one, are listed with their raw value under "Recorded but not resolved". A list ordered by stored confidence says so above the list.

### US3 - Read an assertion and its sources without overclaiming

- id:: ^SPEC-0135-US3
- summary:: Read an assertion with its kind, confidence, concepts, and sources inline, and open each source at the precision the KB actually records.
- status:: ready

#### Acceptance Criteria

- The assertion page shows the claim first, then inline facts: raw kind with its kind group, stored confidence, concept chips, and each cited source with its date and the date's basis. It has no scoped pair, by Drew's decision D7: a single assertion needs no visual. Orientation across many assertions comes from the Assertions list's breakdown (US4) and the KB overview's matrix, and once the panel ships, its locator shows where the assertion sits (US7).
- Each citation shows one state and offers "Open in source" as follows. The states in the first, second, and last rows are SPEC-0133's existing evidence states.

| Citation state | Shown inline | "Open in source" |
| --- | --- | --- |
| A recorded locator resolves | "Recorded passage", with the passage | Opens the source at that passage |
| A recorded locator does not resolve (unsupported, malformed, or out of range) | SPEC-0133's state for that case, unchanged | Opens the source at its top; nothing is highlighted |
| No locator; the source body links the assertion | "No passage locator recorded" | Opens the source at the first paragraph holding an authored link to the assertion, highlighted and labelled "This source links to the assertion here" |
| No locator; the source body never links the assertion | "No passage locator recorded" | Opens the source at its top with "This source does not link to the assertion"; nothing is highlighted |
| The cited source does not resolve | The unresolved state, with the raw value | Not offered |

- An authored link to the assertion is a wikilink or Markdown link in the source body that the shared resolver (R2) maps to the assertion's record. The highlight is never labelled a passage or evidence, never moves to a nearby paragraph, and never uses extraction IDs. "Open in source" passes the assertion as the source view's `assertion` param (R2), so the source's node reference and fragment are unchanged and the target survives tab switches and reloads (R6a).
- A source page lists "Assertions extracted from it" (from `extractedAssertionIds`) separately from "Assertions that cite it". Extraction IDs that resolve to no assertion are listed with their raw value.

### US4 - Answer a question from the matrix

- id:: ^SPEC-0135-US4
- summary:: Pick a topic and a kind of assertion in the matrix and get the list of exactly those assertions, in a list that says what it holds.
- status:: ready

#### Acceptance Criteria

- Activating a matrix cell opens the Assertions list with its `concept` and `assertionKind` params set; the list's count equals the cell's number.
- The Assertions list opens with a one-line text breakdown of the rows it currently shows: a count per kind group, each naming its raw kinds with their counts, then the "Stored confidence" counts for `hypothesis` and `guess`. Kind-group counts are text, because a group spans several kinds and `assertionKind` takes one value; each raw-kind count sets `assertionKind`, each stored-confidence count sets `confidence`, and the counts follow the list's other filters. It is text, not a chart (D7).
- Activating the stored-confidence cell of a row opens the list with `concept` set and `confidence` set to `hypothesis,guess`.
- The matrix header shows each column's total counting every assertion once, even when it belongs to several concepts, and states that rows can share assertions.
- Concepts with no members are listed under the matrix by name with a link, not dropped silently.
- Returning to the filtered list through its tab, after opening one of its assertions, shows the same filter and count (R6a).

### US5 - Keep the KB in shape from Review

- id:: ^SPEC-0135-US5
- summary:: See the KB's live gaps, submissions and findings, curation documents, and stored confidence on one page, and hand each to the AO KB skill that acts on it.
- status:: ready

#### Acceptance Criteria

- Review shows a Gaps block with each gap's count, the rule it applies, a link to the matching records, and a copyable AO KB command. Counts come from the current index on every load.
- The Gaps block covers at least: assertions with no concept membership; concepts with no member assertions; sources with no recorded date; sources with no recorded extraction; extraction IDs that resolve to no assertion; assertions whose stored confidence is outside `validated`, `hypothesis`, and `guess`, with each such value named; and assertions that record both `kind` and `assertionKind` with different values.
- "Submissions" lists the `Submission` records (R2) that are not findings, grouped by recorded `status`; the headline count is `status: pending` only. "Findings" lists records whose recorded `type` is `finding`, grouped by status, with no headline count. A status AO does not document is shown verbatim, not mapped. A line under the lists says archived submissions are not listed.
- "Curation registries and reports" lists every Markdown document directly under `.kb/curation/`, the conflict registry included, with its authored `updatedAt` when present and "No recorded date" otherwise. The index's file modification time is never shown as a curation date.
- Stored confidence shows counts for `hypothesis` and `guess`, labelled "Stored confidence", each linking to the filtered list.
- A KB with no submissions, no findings, no curation documents, or no gaps says so in place; a missing document is never reported as healthy.

### US6 - Read and set up a KB written on an older AO schema

- id:: ^SPEC-0135-US6
- summary:: Install the starter in a single-product KB at AO schemaVersion 4 through 7, complete setup through the documented route, and browse its knowledge with the same site, without migrating it.
- status:: ready

#### Acceptance Criteria

- The starter's check reports knowledge as supported through a read-only legacy spelling adapter for schemaVersion 4 through 7, and product-model bindings as not applicable below schemaVersion 8, naming AO's migration as the upgrade path.
- Setup completes for a single-product legacy KB through the setup skill's route: refresh publishes knowledge bindings; the check reports `bindingsReady` with the model stage `not applicable`; the starter's index command issues a receipt; the check then reports `indexReady`; and a typed knowledge query returns every assertion. No model dependency (uv or Python) is required.
- A legacy KB whose `.kb/config/manifest.json` records no `siteEngine` entry, as AO's older scaffolds write, is admitted as a KB that never enabled AO's site engine. A recorded `siteEngine` that does not match the pin still fails, and a legacy product-mode account reports `ao_schema_unsupported` with AO's migration named.
- Kind, source kind, speaker, and supporting-assertion values read from their legacy keys when the current key is absent. The raw kind value is shown as stored; grouping uses AO's retired-name table.
- No file in the KB changes.

### US7 - Navigate the site from the sidebar panel

- id:: ^SPEC-0135-US7
- summary:: While inside the Knowledge group, use a resizable panel at the bottom of the sidebar to find records by title, move between site pages and lists, and see where the current record sits.
- status:: ready

#### Acceptance Criteria

- The panel appears at the bottom of the Notes sidebar while the Knowledge experience presents the active tab's subject (SPEC-0136 R1 and R6), and gives way to the panel that fits any other subject. Returning to the group, by history or by tab, shows the same panel with its Recent list, find text, and scroll intact; if the panel frame was evicted (SPEC-0136 R3), the Recent list survives and find text and scroll start fresh.
- The panel holds: the KB title; "Find by title", which matches titles and concept aliases, opens the first match on Enter, and offers "Search all text" to hand the query to Rhizome search across the whole vault; links to KB overview and Review; concepts with member counts; source folders and artifact folders with counts; assertion kinds with counts; a locator on record pages; and a "Recent" list of the site pages visited in this window, newest first, kept in `sessionStorage` keyed by vault and experience.
- On a record page the locator draws the map by R4's highlight rule with R6's panel colours (in-scope concepts filled with the panel highlight, the rest faint, the current one ringed), without labels, and lists the in-scope concepts below it as links. On other pages the locator is collapsed to one line saying what it shows on record pages.
- The panel has three states: split, with a draggable and keyboard-operable divider; minimised to its title bar; and cover, filling the sidebar's contents. Escape returns from cover to split. Height, minimised state, and every other panel behaviour follow SPEC-0136 R6.
- Arriving at the site never shows a sidebar the person hid, changes its width, or moves its toggle.

### US8 - See custom views first in the view switcher

- id:: ^SPEC-0135-US8
- summary:: See a subject's custom views first and inline when the width allows, with built-in views still one step away.
- status:: ready

#### Acceptance Criteria

- The switcher shows custom choices first, by order then ID, then standard choices. As many as fit the measured width are inline; the rest are in a "More" menu.
- The selected view is always visible: inline, or named on the "More" trigger.
- The default marker, the reset to the configured default, and the keyboard model (one tab stop, arrows, Home and End, menu focus return) behave as before at every width.
- Selection precedence and the catalog's order in the API are unchanged.

## Requirements

### R1. Authored data only, labelled for what it is

- MUST draw every mark and number from indexed authored records or a computation over them that the caption names. MUST NOT infer links, topics, dates, kinds, or confidence.
- MUST use these labels or equivalents that claim no more. The prototype label on the left MUST NOT ship.

| Prototype label | Required label | Why |
| --- | --- | --- |
| Known, Decided, Open | Statements, Intent and rules, Risks and questions, Other kinds (R3) | The old names judged truth and resolution; the groups only collect kinds |
| How the KB was built | Sources by date | It plots sources, not the KB's construction history |
| placed on the date it happened | the basis of each date (recorded, or from the file name) | The KB records neither event dates nor learning dates in general |
| When this was learned | When its sources are dated | Same |
| against every source in the KB | against the N dated sources | Undated sources are not on the axis |
| What we know about it | What the KB says about it | Many member assertions are hypotheses or guesses |
| Evidence it cites | Supported by (recorded) and Assertions it links | Only recorded support values are support; other links are references |
| A highlighted source passage | This source links to the assertion here | A backlink is not a recorded passage locator (US3) |
| Lines are links people wrote between concepts | Lines are the Related Concepts people recorded | Only that section is relatedness |
| Pending submissions (all statuses) | Submissions by status; pending count only | Ready and applied are not pending |
| Curation reports | Curation registries and reports | The folder also holds registries and logs |
| Find in this KB | Find by title | It does not search text |
| not validated | Stored confidence: hypothesis or guess | Other confidence values are neither validated nor unvalidated |

- MUST show missing values as missing ("Kind not recorded", "No recorded date") and MUST NOT substitute defaults, ingestion dates, file modification times, or validity dates.

### R2. Data contract

The starter's knowledge-convention adapter (SPEC-0133 R6) gains these read-only fields. Field names are fixed here so the data, library, and page work can proceed in parallel. Every field below that takes several source keys reads the first key present; for a list field, an explicitly empty current list counts as present (verified on the built binary for `supportValues`).

| Need | Field | Mapping | Rule |
| --- | --- | --- | --- |
| Assertion kind | `Assertion.kind` (existing), `Assertion.legacyKind` (new) | `kind`: sources `["kind", "assertionKind"]`; `legacyKind`: `assertionKind` only | Raw value preserved. When both keys are present and differ, `kind` wins and Review reports the conflict (US5). |
| Source kind | `Source.kind` (existing) | sources `["kind", "format"]` | Labelled "Source kind"; folder groupings are labelled "Source folder". |
| Assertion speaker | `Assertion.speaker` (existing) | sources `["speaker", "attestedBy"]` | Absent stays absent; the legacy `origin` key is not read as a role. |
| Artifact support | `Artifact.supportValues: [String!]` (new) | sources `["supported_by", "supportingAssertions"]` | Raw values, resolved by the shared resolver below. Only these are "Supported by (recorded)". The existing `supportedBy` link field stays for the existing views. |
| Source recorded date | `Source.recordedDate` (new) | sources `["sourceDate", "meetingDate", "date"]` | Only an ISO calendar date, optionally with a time, is a date; any other value counts as "unreadable recorded date". |
| Concept membership, both directions | `Concept.linkedAssertions` and `Concept.linkingAssertions` (new) | `@neighbors(direction: OUTBOUND, type: "Assertion")` and `@neighbors(direction: INBOUND, type: "Assertion")` | One shared function forms the deduplicated union by canonical identity (D1). |
| Assertions that link a source | `Source.linkingAssertions` (new) | `@neighbors(direction: INBOUND, type: "Assertion")` | Candidates for "Assertions that cite it" (R5). `Source` declares no other relation field, and the generic `neighborhood` field is never used for this set (R2c). |
| Maps of content | `MapOfContent` (new type) | `_moc.md` and `MOCs/**/*.md` | Title; `abstract` is bound as an ordinary field. |
| Submissions and findings | `Submission` (new type) with `status` and `recordType` (from `type`) | Markdown under `_improvements/` whose frontmatter has `status`, excluding `_improvements/_archive/` and `_improvements/_pending-attachments/` (`@node(matches:)` with `has:status`) | Covers AO's nested `<slug>/<slug>.md` submissions, its flat `fnd-*.md` findings, and the flat submissions legacy KBs keep; status-less guides and attachments are excluded. `status` and `type` are verbatim. |
| Curation documents | `CurationDocument` (new type) and `ConflictRegistry` (existing), each with `recordedUpdatedAt` (new) | `CurationDocument` matches `.kb/curation/*.md` except `_conflicts.md`, which keeps its existing type; `recordedUpdatedAt` reads the authored `updatedAt` key | Review lists both types as one inventory. The generated `updatedAt` record fact is a file modification time and is never shown as a curation date. |
| KB profile | `context.profile` in the knowledge manifest (no new type) | `profile` in `.kb/config/vault.md`, read by the binding compiler's existing scalar reader | Absent stays absent; `vault.md` is already a fingerprint input. A value the scalar reader refuses is omitted with a note and never fails refresh, since it is only displayed. |

#### R2a. View parameters

Names avoid the workspace URL keys that `pkg/ontology/viewconfig/params.go` reserves, including `kind` and `folder`.

| View | Params (all `string`) | Meaning |
| --- | --- | --- |
| `ao-kb.assertions` | `concept` (existing), `assertionKind`, `confidence`, `gap` | Concept note path; raw kind; comma-separated raw confidence values; a gap ID |
| `ao-kb.concepts` | `gap` (new) | A gap ID, such as concepts with no members |
| `ao-kb.sources` (new) | `recordFolder`, `gap` | A source folder path; a gap ID |
| `ao-kb.artifacts` (new) | `recordFolder` | An artifact folder path |
| `ao-kb.source` | `assertion` (new) | The note path of the assertion "Open in source" targets (US3) |

Gap IDs are defined once in the site library, with each gap's rule.

#### R2b. Legacy admission and setup

- MUST admit knowledge for single-product KBs at AO schemaVersion 4 through 8. This adapter reads another tool's authored files that are still in use and that current AO tooling cannot migrate without an older AO release; it is not a compatibility shim over Rhizome's own contracts. It covers exactly AO's documented key renames and retired kind names and nothing else.
- For knowledge admission, a `.kb/config/manifest.json` with no `siteEngine` entry is read like an absent manifest: a KB that never enabled AO's site engine. Its hash stays a fingerprint input. A present `siteEngine` must still match the pin. The model stage keeps its own checks.
- Below schemaVersion 8 the model stage is `not applicable`:
  - Refresh publishes knowledge bindings, does not run the model exporter, publishes no model generation, and records the model manifest with status `not applicable` and the reason. Its output reports the model state the check computes, never a fixed `modelReady: true`.
  - The check reports `bindings.model.status: "not applicable"` and `modelReady: false`, and sets `bindingsReady` when knowledge is ready and the model is ready or not applicable. Model dependencies are not required, and uv is not looked up.
  - The index command runs indexing, skips model-record verification, and writes a receipt holding the knowledge fingerprint and the site source signature below. `indexReady` compares the receipt with both.
  - The check's named rows (`bindings-check.mjs`): `schemaVersion` says knowledge is supported and the model is not applicable, naming AO's migration; `model compiler dependencies`, `model bindings`, and `model source ownership` report `not applicable` with that reason, never `dependency missing`, `ok`, or `pending`; `model index` reports the knowledge receipt. Knowledge-root ownership is still enforced at refresh.
- **Site source signature, every supported schema.** The receipt's signature covers the site inventory, which Node computes from the same path rules as the starter's types and the generated admission (reusing the path atoms `knowledge.mjs` builds), not the adapter's underscore rule: every Markdown file under `assertions/`, `concepts/`, `artifacts/`, and `sources/` except `sources/_raw/`; `_moc.md` and `MOCs/**`; `_improvements/**` except `_archive/` and `_pending-attachments/`; and `.kb/curation/*.md`; at the account root and, in product mode, the selected product's scope. Frontmatter conditions (`has:status`, `products:`) are not reproduced, so the inventory is a deliberate superset: a file that gains or loses a status or a product is still caught. At schemaVersion 8 the signature covers the model sources plus this inventory, which replaces the adapter's `knowledgeSources` as its knowledge input; `knowledgeSources` keeps its ownership role. The index command computes the signature before and after indexing and the check recomputes it. Existing schemaVersion 8 receipts therefore expire once after upgrade, and the check asks for one reindex.
- Product-model compilation keeps its schemaVersion 8 baseline. In a multi-product account (schemaVersion 8 only), `MapOfContent`, `Submission`, and `CurationDocument` select the account root and every `products/*/` root, and the generated admission keeps the account root and the selected product's root, never another product's, as it does for the four knowledge types.

#### R2c. Completeness and identity

- MUST read whole-KB data completely. Every whole-KB root MUST request up to the public root maximum (5000) and read typed-root coverage; any root whose status is not `complete` MUST be disclosed where its numbers appear. Declared relation fields (`@neighbors`, `@contains`) MUST NOT pass `first`, since on them an omitted `first` returns every row and typed-root coverage does not report nested truncation. The generic `neighborhood` field is different: it returns 20 rows by default and at most 200, reporting `truncated`, so no whole set (a page's list, count, or scope set) may be read through it. Array length alone MUST NOT be presented as a whole-KB total. This replaces the existing views' 500-record ceilings, at root and nested levels.
- MUST use canonical node identity for every record, link, and count. Authored references (extraction IDs, support values, and source-body links to an assertion) MUST resolve through the starter's shared `makeResolver` (`lib/relations.ts`): an exact AO ID first, then a unique file name. A value that matches no record, or several, stays unresolved and visible with its raw text. File basenames MUST NOT otherwise identify records. Product admission from SPEC-0133 MUST hold on every page.

### R3. Kind groups and colour

| Kind group | Current kinds | Legacy spellings (AO retired names) |
| --- | --- | --- |
| Statements | `fact`, `observation`, `need` | `tribal-knowledge`, `customer-voice` |
| Intent and rules | `decision`, `rule`, `preference`, `goal`, `principle` | `constraint`, `trade-off` |
| Risks and questions | `risk`, `hypothesis`, `question` | `assumption` |
| Other kinds | any other value, named with its count | |

- `principle` is not an AO kind. AO KBs record it as a custom kind for a durable belief or design principle, and in one current-schema KB it is 112 of 190 assertions, so leaving it under Other kinds left most of that KB grey. Drew placed it in Intent and rules on 2026-11-09. Other custom kinds stay under Other kinds.
- The table lives once in the starter's presentation code. It MUST NOT affect confidence, currentness, resolution, or any AO-owned value, and the legend MUST name the kinds in each group.
- A kind is a kind and a confidence is a confidence: kind `hypothesis` and confidence `hypothesis` MUST appear under different headings.
- Kind groups use three hues plus the neutral for Other kinds, as new tokens in `web/src/tokens.css`: `--kind-statements` (light `#1264df`, dark `#4b92f5`), `--kind-intent` (light `#9c168e`, dark `#c65ab9`), `--kind-risks` (light `#d05200`, dark `#e36e1d`), and `--kind-other` (the neutral `--type-8`). The light values are the house type hues blue `--type-1`, magenta `--type-10`, and orange `--type-6`. The house dark values fail the palette checker named below (blue and magenta 5.2 apart under simulated protanopia, and outside its lightness band), so the dark values are those hues mixed toward their light values, which pass: worst simulated separation 10.1 in dark and 13.3 in light, contrast against the dark surface 5.51, 4.56, and 5.33, and ink numerals at the matrix's 50% dark cap 6.22, 7.01, 6.38, and 5.58 for the neutral. They MUST NOT use connection teal, success green, AO red, or warning gold. The values for each appearance MUST pass the palette checker (the dataviz skill's validator) for colour-vision deficiency and 3:1 non-text contrast against the surface, with results recorded. Violet `--type-7` and indigo `--type-4` cannot pass beside blue: simulated deuteranopia puts violet 3.0 CIELAB units from blue in light, and protanopia puts indigo 0.4 from blue in dark. Measured as CIELAB distance, magenta keeps at least 16.7 from blue, orange, and neutral under all three simulations in both appearances, and stays 31 or more from AO red; the palette checker's own measure is the one recorded for acceptance. This replaces the earlier violet-or-indigo choice; Drew chose magenta on 2026-11-08.
- Relationship lines, including highlighted neighbours, MUST use the house link teal `--link` everywhere except inside the sidebar panel, which uses `--chrome-link` (R6). `--link` is the colour `DESIGN.md` names for essential edges (6.43:1 on the light surface, 7.67:1 on the dark). Connection teal (`--series-link`) measures 2.02:1 on the light surface and MUST NOT draw lines.

### R4. Visuals

Every visual is deterministic for the same index, has a text or table equivalent, and has a "not enough to draw yet" state that names what is missing and links to the matching gap.

- **Topic map.** One node per concept, area by member count: radius `4 + 18·√(n / n_max)` px at the overview's natural width for a concept with n members, and a 5px hollow node at zero members. Arcs show kind-group shares only on nodes of at least 12px radius; smaller nodes are solid neutral, and the caption MUST say that a solid neutral node is too small for arcs, not a node of Other kinds. The map's height grows with the concept count, from 430px to at most 680px (`max(430, min(680, 250 + 9·concepts))`), and after the force layout a fixed number of separation passes push apart any two nodes closer than their radii plus 14px, so no two nodes overlap. Concept names are drawn at rest on the KB overview, cut at a word boundary with an ellipsis past 30 characters, and placed in member-count order by R5's estimated label widths at the first of eight candidate positions that overlaps no node or earlier label. A name with no free position is left off at rest, and the caption counts those concepts and says to hover or focus a circle for its name or use the concept list. The tooltip and the concept list always give the full title. The scoped neighbourhood labels only in-scope nodes, at their placed position or, when the overview left them unlabelled, at the default position to the node's right; the locator draws no labels. Edges are undirected Related Concepts pairs, deduplicated. Hovering or focusing a node highlights its neighbours and shows a tooltip with its name, member count, and Related Concepts count. Activating a node opens the concept. Arrow keys move through nodes by member count descending, then title. Fewer than three concepts: not enough to draw. No Related Concepts recorded: the nodes draw and the caption says no lines are recorded. The concept list on the KB overview, with member and Related Concepts counts, is the map's text equivalent.
- **Sources by date.** One row per source folder. A row label that does not fit 24 characters keeps the last folder name whole and shortens the folders before it with an ellipsis, so sibling folders never read the same; the full path is the label's tooltip. A recorded date draws a filled mark; a file-name date draws a hollow mark labelled "Date in file name". The legend and each mark's tooltip state the basis. The caption counts sources by basis, undated sources, and unreadable recorded dates, and links each set. Dates from titles are not used. Every dated source is plotted; nothing is trimmed as an outlier. Mark area follows the number of extraction IDs that resolve, labelled as such: radius `3.2 + 1.45·√n` px for n resolved extractions, and 3.2px at none. Arrow keys move through marks by date, then folder, then path. A dated source with no resolved extraction draws at the minimum area in the neutral colour, and its tooltip says "No resolved extraction", so every dated source stays visible. A mark takes a kind group's colour only when more than half of its resolved extracted assertions fall in that group; otherwise it is neutral, labelled "mixed or none". Fewer than three dated sources: not enough to draw, with the counts.
- **Matrix.** Rows are concepts with at least one member; columns are the raw kind values present in the KB, grouped under kind groups, then "Stored confidence: hypothesis or guess". Cells show counts with ink numerals on fills capped at 60% tint in light appearance and 50% in dark; where R8's recorded contrast check disagrees, R8 governs. Tint scales linearly with the count, from a 12% floor at a count of one to the cap at the table's largest cell, so darker always means more and a count of one is visibly filled against a blank cell. The fill repeats the numeral, which carries the count and meets 4.5:1, so R8's 3:1 rule for meaning-bearing marks does not apply to it. The stored-confidence column uses the neutral tint under the same caps and scale, with an ink header, never a kind-group hue or warning gold. Empty cells stay blank. It uses the ARIA grid pattern: `role=grid` on a table with row and column headers that are announced, one tab stop, arrow keys moving by cell, and Enter or Space activating the cell. No concept with members: not enough to draw.
- **Highlight rule.** Wherever a visual "highlights" or "colours" a scope (US2, the scoped pair, US7's locator), in-scope nodes and marks keep the KB overview's encoding (size, arcs or kind colour, edges in the link colour), out-of-scope ones are drawn faint in the neutral colour without labels, the caption says what is in scope, and the current record is ringed in ink. The scoped neighbourhood draws only the out-of-scope nodes inside its crop and only the lines whose two ends it draws, and its caption names only what it draws and counts the concepts outside the crop; a scope with no concept, or whose crop would draw a single node, shows the "not enough to draw yet" state with the record's concepts and links instead of a map. Inside the sidebar panel, R6's panel colours replace the kind encoding.
- **Scoped pair.** Concept, source, and artifact pages only (US2). The neighbourhood crops and scales the KB overview's positions and never reruns the layout. The strip shares the overview's time domain. When the overview's map or axis is not enough to draw (the thresholds above), that half of the pair shows the "not enough to draw yet" state with the record's own counts and the same gap links.
- **Locator.** Panel only (US7).

### R5. Computation

- One pure library in the starter MUST compute membership, kind groups, counts, the topic layout, date placement, scope sets, and gaps, and every site page and the panel MUST use it. Its whole-KB input is the orientation query and the Related Concepts read; a record page, and the panel on a record page, add one scope read for the opened record (for an artifact, its record read plus a batched citation read; batched reads chunk through `readNodes`).
- The orientation query MUST carry exactly these fields, with roots at `first: 5000` and nested lists without `first`, and nothing else:
  - every root: `ref`, `path`, `aoId`, `title`;
  - `assertion`: `headline`, `kind`, `legacyKind`, `confidence`, `provenanceSource`;
  - `source`: `name`, `headline`, `recordedDate`, `extractedAssertionIds`;
  - `concept`: `name`, `headline`, `aliases`, `linkedAssertions { path }`, `linkingAssertions { path }`;
  - `artifact`: `name`, `headline`;
  - `mapOfContent`: no further fields.
- The Related Concepts pairs MUST come from their own read beside it, `concept` at `first: 5000` with `path` and `relatedConceptsSection { relatedConcepts { path } }` and nothing else, and its `concept` coverage counts toward R2c. Every orientation field reads only the index, so the read cache serves the orientation query (SPEC-0136 R4); the section is projected from each concept's note file, which the cache never serves, so it stays out of the orientation query. On the largest real KB the Related Concepts read took 22 ms.
- `provenanceSource` is a raw scalar (`provenance.source`), not a link, so an assertion can record a source as its origin with no link to it. AO and the existing citation reader (`citations()` in `lib/knowledge.ts`) count such an assertion as citing the source, so the site does too.
- Scope reads, by page:
  - Concept: the Sources-section citations (`sourcesSection { citations { recordedLink sources { path } } }`) of its member assertions, read with the kit's `readNodes` (SPEC-0136 R7), which chunks at the batch maximum and merges coverage, so a concept with any number of members is read completely.
  - Source: the candidate set is the source's `linkingAssertions { path }`, read without `first`, united with every assertion whose `provenanceSource` from the orientation query resolves to the source through `makeResolver`. The scope read confirms each candidate as a citation by reading its Sources-section citations, and `citations()` decides, so a provenance-only citation is included and a link outside the Sources section is not.
  - Artifact: its `supportValues`, `additionalAssertionReferences { path sourcesSection { citations { recordedLink sources { path } } } }`, and `conceptReferences { path }`, then, as one batched node read, the Sources-section citations of support values that resolve through `makeResolver` to assertions not already read. `citations()` decides, as for a concept. The artifact's source set is the sources those assertions cite.
- The orientation query and Related Concepts strings and the scope-read builders MUST be exported from one starter module, so the views and the Go integration tests run the same text.
- Record pages MUST use SPEC-0136's `RecordPage` and MUST show the record's own title, headline, and body without waiting for the orientation query; the scoped pair and other whole-KB parts load beside them.
- Bodies, content, workspace data, and passages load only for the opened record, with one exception: an assertion page reads the text of the sources it cites, on demand and only those sources, to resolve each recorded passage and to find whether a source's body links the assertion (US3). That read is part of the assertion page's scope read and its budget. Review's submission and curation lists use their own body-free query.
- The Concepts, Assertions, and Attention pages keep their SPEC-0133 evidence reads, because SPEC-0133 R7's locator-coverage categories need source text. This effort removes their caps (R2c) and adds the concept-side membership fields (`linkedAssertions { path }`, `linkingAssertions { path }`) so they use the shared membership (D1); nothing else in that read changes. New pages and the panel MUST NOT use that query. Without caps that read measured 2,039 ms and 6.2 MiB on the largest KB; this effort accepts that cost on those three existing pages in exchange for complete counts, and Batch D reports it per KB.
- The layout MUST be a function of the sorted canonical inputs alone: no randomness, a fixed iteration count, and estimated label widths. The same index MUST give the same positions in every frame, page, and browser. It is computed in each frame that needs it, once per index generation (SPEC-0136 R4), and stored nowhere.
- Measured on 2026-11-08 on a disposable copy of the largest real KB (878 assertions, 115 sources, 21 concepts, 181 artifacts), using this checkout's built binary at `439890f79` with the R2 fields applied to the copy's starter, embeddings off, median of five warm runs:

| Read | Median | Payload | Coverage |
| --- | ---: | ---: | --- |
| Orientation query as specified above, before `provenanceSource` was added | 291 ms | 501 KiB | complete on every root |
| The same plus citations and artifact references in one query | 807 ms | 833 KiB | complete |
| Today's Concepts and Attention query with roots at 5000 and nested `first` removed | 2,039 ms | 6.2 MiB | complete |
| Scope read, largest concept (48 members) | 64 ms | 12 KiB | |
| Scope read, most-linked source (66 assertion links), through the generic neighbourhood with an explicit `first` | 62 ms in two reads | 23 KiB | |
| Scope read, one artifact | 3 ms | under 1 KiB | |

- Two inputs changed after these measurements. `provenanceSource` adds one short scalar per assertion; none of the three real KBs records `provenance.source`, so it reads null throughout. `Source.linkingAssertions` replaces the neighbourhood read with a declared field of the same shape as `Concept.linkingAssertions`. Batch D re-measures both exact strings.
- Citations and artifact references cost about 360 ms and 340 ms across the whole KB, which is why they are scope reads. Whole-KB reads go through SPEC-0136's read cache, so the panel, every page of the experience, and reloads share one execution of the orientation query per index generation (the Related Concepts read executes per frame); the budgets below are for a cache miss. The prototype's layout took 0.5 ms for 21 concepts and 29 ms for 200; Batch B re-measures it.
- If the orientation query exceeds one second median, or a scope read exceeds 250 ms median, on any supported real KB at release, that is a decision to bring back with measurements, not a cache to add quietly.

### R6. The Knowledge experience and its panel

- The starter's Knowledge views MUST form one experience: `module/views/knowledge/experience.yml` with `title: Knowledge`, `group: Knowledge`, `types: [Assertion, Concept, Source, Artifact, MapOfContent, Submission, CurationDocument, ConflictRegistry]` (listed so these types resolve at SPEC-0136 R6's type level, ahead of any group panel), `panel: ao-kb.panel` (a view with `mount: { kind: panel }`, added together with that view), and `tailwind: false`. Recent is window-scoped by design, so it uses `sessionStorage`, not a view preference. Every page in this spec is a view in that folder and exports its component as the module default. The utilities the views use come from SPEC-0136's precompiled stylesheet or `knowledge.css`.
- The panel's platform behaviour (which subjects show it, its frame lifetime, suspension, states, divider, persistence, shortcuts, motion, and the outer sidebar's independence) is SPEC-0136 R3 and R6. This spec defines only what the panel holds (US7).
- **Shared group names.** No module owns a group name. Among this repository's starters alone, "Knowledge" holds the AO KB types, the Agentic Engineering starter's `ReferenceDoc`, and the compounding starter's `ContextShard` and `AgentFinding`, and a vault may put its own types there too. Because the experience presents the Knowledge group, its panel shows for those records as well. It draws its locator only for records it knows and otherwise shows the locator collapsed, keeping its other navigation. A module that wants its own panel for its types declares an experience with those `types`, which outranks the group (SPEC-0136 R6).
- **Reference and policy documents (D6, decided).** Rhizome is the home for engineering, reference, and policy documents, and they may be separate sections rather than one site. A reference doc's home is the group its type declares: the Agentic Engineering starter's `ReferenceDoc` is in "Knowledge", a section apart from Delivery, so leaving Delivery for a reference doc hides the Delivery panel and shows that group's panel, if any. An untyped policy note under `docs/engineering/` shows the built-in Folder panel (SPEC-0136 R6); it gets a section when the Engineering starter gives policy documents a type, which belongs to the Agentic Engineering effort (D8). That starter may also give `ReferenceDoc` a group of its own, so that a vault with the AO KB starter keeps reference docs apart from the KB. Neither needs a platform change.
- **Locator colours.** The panel draws on the chrome surface, where `--link` fails R8's 3:1 rule, so the locator uses SPEC-0136 R6's `--chrome-link` for in-scope concepts and relationship lines, faint `--chrome-muted` for the rest, and a `--chrome-text` ring for the current concept. It draws no kind hues, because no kind palette passes both the colour-vision check and 3:1 on every chrome background. R8's contrast check records each pair on every chrome background.
- Open-search's `folder` becomes optional in the kit, and omitting it searches the whole vault, so "Search all text" can hand a query to Rhizome search.

#### R6a. View params survive tabs

- A workbench tab MUST retain the declared param values of the presentation it shows, and restore them when the tab is activated, after another tab closes, from persisted tabs, and on reload. This covers note tabs and the group's home location. Values are validated as view params and bounded like other persisted tab state. Browser Back and Forward keep working from the full URL as today.
- This is generic: the AO KB lists and source view need it, and so does any custom view with declared params.

### R7. View switcher order

- Visual order MUST be custom choices, then standard choices, with inline width measured including trailing controls, and the rest in "More" (US8). This replaces the rule in `ViewSelector.tsx:63-64` that two or more custom choices force a menu.
- The catalog order returned to callers and the selection precedence of SPEC-0110 MUST NOT change.

### R8. Accessibility and design

- Base text is 14px; captions and matrix body are 14px; legends, axis labels, and column heads are at least 13px; nothing is smaller than 12px, and only identifiers use 12px mono. Record titles follow the house document-title treatment.
- All text, including numerals on fills, MUST meet AA 4.5:1 in both appearances; non-text marks that carry meaning MUST meet 3:1 and are always paired with a text label. The check of every fill and numeral pair is recorded.
- Each chart is one tab stop with arrow-key movement inside it, a visible house focus ring, and the hover tooltip also shown on focus. Hover-only meaning is not allowed.
- Navigation history is not drawn as breadcrumbs. The panel's "Recent" list and browser Back serve that role.
- Every page works at the desktop working widths in light and dark appearance, and the panel works at the sidebar's 200–400px range.

### R9. Verification

- Synthetic fixtures only. They MUST be shaped like the observed real KBs (a schema-5 vocabulary legacy KB with a scaffold manifest that has no `siteEngine`, a schema-4 stamp variant, the current-schema booking fixture, and the product fixture) and never contain real client text.
- Library tests MUST cover: shuffled input gives identical layout and counts; both vocabularies group as R3 says; confidence `high` is never counted as `hypothesis` or `guess`; a file-name date is never reported as recorded; membership in both directions, deduplicated, including an assertion linked only from the concept side; every "not enough to draw yet" threshold on generated sparse inputs, including each half of the scoped pair; a root that is not complete is disclosed; support values resolving by AO ID, by file name, ambiguously, and not at all; each US3 citation state; a source's scope set including an assertion that cites it only through `provenanceSource`, with that assertion's concepts in the source's map; the Assertions list breakdown under filters; the matrix tint scale; the kind conflict gap; no two topic-map nodes overlapping, and the unlabelled-name count, at 48 concepts with no Related Concepts; distinct row labels for sibling source folders with a long shared parent; and layout timing at 21, 48, and 200 concepts.
- Ontology and starter tests MUST cover: each R2 field, including `Source.linkingAssertions` returning every row for a source linked by more than 20 assertions; legacy admission at schemaVersion 4 and 5; a scaffold manifest without `siteEngine` against a mismatched pin; the knowledge-only route from refresh to check to index to check, with uv absent from PATH, reporting `bindingsReady` and `indexReady` with the model `not applicable` and each named check row as R2b states; the site source signature going stale when a newly bound document (a map of content, a submission, a curation document) is edited, added, or deleted and when an underscore-named artifact changes, and reindexing restoring `indexReady`, on both schema routes; a both-keys kind conflict; `supportValues` precedence with an empty current list; submission and finding selection with archive, pending-attachment, and guide files present; curation documents with and without `updatedAt`, the registry included; product-mode document admission; the exported orientation query run against the legacy, booking, and product fixtures; and a generated 600-assertion KB read completely.
- E2E MUST run the site journey against the legacy fixture: KB overview to concept to assertion to source and Back; the matrix count agreeing with its list, keyboard included; a concept's count on the Concepts page agreeing with the Assertions list filtered by it; a breakdown count agreeing with the list it opens; a Review gap opening its list; each "Open in source" state; a filtered list and a source's `assertion` param surviving a tab switch, a reload, and a copied URL; light and dark. The booking suite keeps running unchanged. The journey also covers opening records in new tabs within one frame. The panel spec covers the panel inside and outside the experience (Folder on an untyped note); the switcher spec covers wide and narrow widths. Panel resolution over the AO KB starter alone, the three starters together, a vault-local type in Knowledge, and a team experience whose `types` outranks the group is covered by SPEC-0136's Go tests.
- Before the pull request that delivers the site, the site MUST be exercised on disposable copies of three real KBs (a large legacy KB, a medium legacy KB, and a current-schema KB) with the built binary. Screenshots stay on the local machine.

## Amendments

- **SPEC-0133.** R6: the knowledge-convention adapter adds the R2 fields, legacy knowledge admission for single-product schemaVersion 4 through 7 with the model stage `not applicable`, the `siteEngine`-absent admission rule, and the three read-only document types. R7: membership becomes the union in R2; KB overview becomes the Knowledge group's default; KB overview, Review, Sources, Artifacts, Concept, and Artifact pages join the workspace; whole-KB reads report coverage instead of a 500-record ceiling. The Related Concepts meaning and the Concepts page's locator-coverage categories are unchanged.
- **SPEC-0110.** Workbench tabs retain declared view params (R6a). The panel, frame reuse, and suspension are SPEC-0136's amendments. The presentation order sentence gains R7's visual order, keeping catalog order and selection precedence. Any ordering language in [[group-views-and-view-platform|SPEC-0111]], [[type-collection-views|SPEC-0112]], [[scope-overview|SPEC-0117]], and [[workbench-redesign|SPEC-0126]] that restates the old visual order is updated with it.
- **SPEC-0105.** The kit gains typed-root coverage on GraphQL results (R2c), and open-search's `folder` becomes optional (R6). Both are additive; the other kit additions are SPEC-0136's.

## Open Questions

- Drew accepted the recommended options for D1 to D5 and D8 on 2026-11-08: membership in either direction, KB overview as the Knowledge default, Attention kept beside Review, custom kinds under Other kinds (on 2026-11-09 Drew moved `principle` into Intent and rules, R3), the legacy adapter kept and reviewed each release, and a sized-down Agentic Engineering site in its own effort after this one (originally "after PR 1"). D6 (reference and policy documents have a home of their own, R6) and D7 (no visual on single assertion pages; the Assertions list breakdown, US4) were decided by Drew on 2026-11-08, as was a panel spanning groups where needed (R6). The kind-group hue changed from violet or indigo to magenta for colour-vision reasons (R3), which Drew chose.
- AO KB has no documented source event-date field. The Review page offers a hand-off; asking AO to document `sourceDate` is a follow-up outside Rhizome.

## Documentation plan

- This spec, and the amendments above in the change sets that deliver them.
- `docs/reference/subsystems/views.md` (tab params, switcher order; the experience and panel material is SPEC-0136's) and `web/CONTEXT.md` with `last-verified` bumped.
- `docs/reference/subsystems/cli.md`: the AO KB starter's `not applicable` model state, the knowledge-only route, the site source signature, and the `siteEngine`-absent rule, with `last-verified` bumped.
- `DESIGN.md`: the sidebar panel and its `--chrome-link` token, the `--kind-*` tokens with their checked dark values, and chart type sizes.
- The custom-views skill's kit reference under `pkg/app/cli/init/templates/skills/` (coverage, location hook, optional search folder), never the generated copies.
- The AO KB starter's setup skill (`agents/skills/ao-kb-setup/SKILL.md`), bindings README, and guide template (`repo/docs/reference/guides/ao-kb.md`): legacy setup, the `not applicable` model state, includes, the site, and what each label means.
- `CHANGELOG.md` under Unreleased.
