# Booking AO KB fixture

Illustrative clinic-booking knowledge only. The AO KB root is `booking/`, which is also the Rhizome vault root. It pins AO schemaVersion 8, `productMode: single`, product `booking`, and toolkit commit `e9dce951ab4cd6aa9ccc734df96590bde25ccde9`. The fixture has 22 assertions, nine sources, seven concepts, four artifacts, two conflict entries, and one ordinary non-AO note. Raw evidence is text and Markdown; there is no product-model YAML.

This is a convention and compatibility fixture, not a clean KB. Every intentional AO finding is listed below. The knowledge adapter installs through ordinary `rzm init --addons ao-kb`. Installation also writes `.rhizome/source-ownership.yml`, so Rhizome refuses native edits to the AO files; the remaining adapter limits are listed below.

## Consumer contract and evidenced limits

The five root types are `Assertion`, `Source`, `Concept`, `Artifact`, and `ConflictRegistry`. Every type belongs to the Knowledge display group. Scalar fields are nullable strings or string lists so authored vocabulary and dates are not replaced by calculated defaults. `aoId` reads frontmatter `id`; `nodeId` and native `path` retain physical identity. `aoId` is deliberately not a Rhizome `@identifier`: identifiers require the value in `aliases`, AO notes carry none, and the resulting alias-append repairs would target AO-owned files. Look a record up by an exact `aoId` filter, for example `assertion(first: 2, filters: [{field: "aoId", op: eq, value: "a-000000000001"}])`. AO-owned fields carry `@policy` guidance; the starter's source ownership declaration is what enforces read-only access.

The current SDL cannot faithfully provide the requested flat `Assertion.sources`, direct assertion-to-assertion relations over body plus frontmatter, or flat `Concept.relatedConcepts`. Those fields are deliberately not declared with misleading whole-note neighbors or fictitious frontmatter sources. These are the usable bindings:

```graphql
{
  assertion(path: "assertions/clinic-patient-books-own-slot.md") {
    aoId nodeId path headline content frontmatter
    sourcesSection {
      citations { recordedLink content sources { aoId path } }
    }
    connectionsSection {
      supports { aoId path }
      supportedBy { aoId path }
      corroboratedBy { aoId path }
      content
    }
    supportedBy { notePath }
    concepts { aoId path }
  }
  concept(path: "concepts/self-service.md") {
    relatedConceptsSection { relatedConcepts { aoId path } }
  }
  conflictRegistry(first: 5) {
    groups { entries {
      title locator { sourceLocator }
      assertions { aoId path }
      fields { label value }
    } }
  }
}
```

- `sourcesSection.citations` preserves section scope, authored order, and repeated occurrences. `recordedLink` preserves each target, including a missing source. Identical unanchored citation bullets currently make native target lookup ambiguous: both repeated interview occurrences have empty `sources`, while the unique operations-sync occurrence resolves. The interview itself resolves through `node(ref: "sources/patient-booking-interview.md")`. Consumers must distinguish occurrence ambiguity from a missing target using `recordedLink`; the fixture does not add AO-foreign anchors to hide this deficiency.
- Typed bullets inside `## Connections` bind on `AssertionConnections`, an embedded native section. Its canonical fields point from that section to the target claim. The containing assertion is its `notePath`. `supportedBy`, `refinedBy`, `contradictedBy`, `supersededBy`, `resolvedBy`, `addressedBy`, `blocks`, and `groundedBy` are declared inverse forms: the target claim is the source of the canonical edge. On `Assertion`, fields such as `supportedBy` and `supersededBy` are `@reverse` navigation to incoming canonical declaring sections. Do not treat an inverse declaration as canonical outgoing support.
- Map-shaped frontmatter `connections` (`{rel: [ids]}` and a block sequence under each relation key) bind as raw string lists through owner-local paths: `frontmatterSupports`, `frontmatterRefines`, `frontmatterDependsOn`, `frontmatterContradicts`, `frontmatterSupersedes`, `frontmatterResolves`, `frontmatterRelatesTo`, `frontmatterCorroboratedBy`, and the declared-inverse `frontmatterSupportedBy`, `frontmatterSupersededBy`, `frontmatterResolvedBy`. Values stay as authored (usually AO ids) and resolve by exact `aoId`. Sequence-shaped connections (`[{type, target}]`, the shape AO's PRD prompt documents, and a bare id list) are not projected in this release: the bounded selector grammar has no predicate to pick items by `type`, and a list of maps cannot be a string list. The starter's check action names every assertion that uses them. Merging body and frontmatter declarations, inverse-alias normalization, and deduplication happen in the views' relation module, so record, Trace, and Attention agree; typed queries expose the declared parts separately. Unknown relation names and dangling targets remain inspectable in `connectionsSection.content` or full source `content`.
- AO references by logical id (`extractedAssertionIds`, `provenanceSource`, frontmatter connections) stay raw strings. Indexed link fields resolve targets by path and alias only, so a link field over AO ids would raise false `link_target_missing` issues on every source; `Source.extractedAssertions` is therefore not declared. The views resolve ids by exact `aoId`. Native logical-id references arrive with the foundation's namespace-scoped reference bindings (structured-data batch).
- Source `path` is already a reserved native GraphQL address. The AO value is `sourcePaths`; `frontmatter.path` is authoritative for original list slots, including blank slots that normal scalar normalization would remove. The attachment fixture keeps two local text files in slots 1 and 2 and a URL in slot 3.
- `@node(paths:)` has no exclusion form. Source uses the existing prefix expression `@node(matches: ["sources AND NOT sources/_raw"])`. The raw `.md` file is discovered as ordinary Markdown but never projected as a `Source`.
- `.kb` directories were unconditionally pruned, even with an exact include. The shared discovery fix permits an explicitly named hidden-directory prefix and keeps other hidden content and excludes intact. Append exactly `.kb/curation/_conflicts.md` to `notes.include`, preserving existing includes. A broad `**/*.md` alone still excludes `.kb`; no underscore-file rule is needed.
- The registry matches AO's `conflicts.py` rendering (`type: registry`, header prose, `## Entries`, `### cf-NNN: a ↔ b`, fields in AO's order with unknown fields after). Conflict groups retain the authored H2 grouping; entries are H3 nodes below `groups[].entries[]`. Every `- **Field**: value` line binds through `ITEM_TITLE` and `ITEM_SUMMARY`, including the unknown `Clinic annotation`. Entry locators address their source section. `assertions` is entry-wide assertion navigation; the `Assertions` field row is authoritative for the exact pair if a Resolution mentions other claims. Label-specific typed pair binding still needs the adapter. Flat fields such as `state` are not invented from a partial fixed list.
- AO stores lowercase `type: assertion|source|concept|artifact`. A declared type that differs only in case from the selector-matched type resolves to it, so these records raise no `unknown_declared_type` issue. The registry's `type: registry` does not name `ConflictRegistry` and is still reported once; neither the fixture nor the adapter rewrites AO files.
- Nested provenance binds through owner-local paths: `provenanceSource`, `provenanceLocator`, `provenanceSpeaker`, `provenanceSpeakerRole`, `provenanceQuoteHash`, and `provenanceExtractedBy`. The nested form wins over the legacy top-level `speaker`, `speakerRole`, and `extractedBy`. The GraphQL `frontmatter` JSON still flattens nested objects to empty strings, so read the typed fields rather than `frontmatter.provenance`.
- Artifact `supportedBy` plus `additionalAssertionReferences` gives its assertion navigation. Native ambient references omit pairs already represented structurally by `supportedBy`; combining the two avoids double counting. Concept, source, and artifact references have separate fields.

## Named cases

| Case | Fixture record(s) |
| --- | --- |
| Recorded lines, verified quote hash | `clinic-patient-books-own-slot`, operations sync `L12-L18` |
| Origin precedence, repeated Sources, incidental non-evidence link | Same claim: first/third source is the interview; recorded origin is the second source's logical ID; pricing-policy prose link is outside Sources |
| Recorded section | `pricing-tier-policy`, `Pricing > Tiers` |
| Ordered file selector and URL slot | `referral-file-selection`, `2::L3-L5` on `referral-attachments` |
| Recorded timestamp | `timestamp-arrival-buffer`, `01:10-01:30`, timestamp-scheme source |
| Document only | `same-day-video-question`, Sources but no locator |
| Unresolved source | `minors-need-guardian`, `missing-guardian-interview` |
| Unresolved locator | `unresolved-passage-range`, `L120-L125` |
| Malformed locator | `malformed-passage-locator`, `Lbanana` |
| No Sources and no stored attestation | `no-sources-eligibility-check`; legacy top-level speaker/role remain authored |
| Source-note fallback | `source-note-fallback`, `reception-policy-note` has no raw path |
| Corroboration, distinct from origin/extraction | Self-service claim points to external and generated sources with `corroborated-by::` |
| Supersession chain | `reminders-two-hours` supersedes `reminders-twenty-four-hours`, which supersedes `reminders-forty-eight-hours` |
| Expired and archived | `saturday-hours-through-october` expires 2026-10-31; `legacy-paper-booking` is archived |
| Open blocking conflict | Symmetric same-day cancellation pair; registry `cf-001`, Resolution, Impact override, and unknown field |
| Settled conflict | Symmetric 48h/24h reminder pair; registry `cf-002`, auto-superseded, Winner, and authored supersedes edge |
| Open, resolved, withdrawn, superseded questions | Specific eligibility / no-evidence questions; video question resolved by `video-visits-approved`; guardian question retracted with reason; old eligibility question superseded |
| Declared inverse | Self-service `supported-by:: [[video-visits-approved]]` |
| Frontmatter map to inline list | `video-visits-approved`: `connections.supports`, logical ID; duplicates its body declaration |
| Frontmatter map to block sequence | `eligibility-question-old` |
| Frontmatter sequence of typed maps | `eligibility-question-specific` |
| Frontmatter bare sequence | `minors-need-guardian` |
| Unknown relation and dangling connection | `malformed-passage-locator`: `teleports-to` and `missing-booking-rule` |
| Feedback row references, recorded review, nested speaker | Self-service claim has `contradicts_rows`, `supports_rows`, `reviewedBy`, `reviewedAt`, and nested attribution |
| Dense neighborhood and cycle | Operations source declares 14 extracted assertions; self-service and the late waitlist claim support each other |
| Artifact stale sync and unknown sync | PRD's graphSyncedAt predates the cancellation claim update; policy review has no graphSyncedAt |
| Saved assessment stale input | Weights snapshot is as of 2026-10-31; cancellation claim updated 2026-11-03 |
| Added after assessment | `new-waitlist-after-snapshot`, created 2026-11-04, absent from saved weights |
| Related Concepts section scope | Self-service links Reminders under Related Concepts and Cancellation under Notes |
| Raw exclusion and ordinary note | `_raw/meetings/patient-booking-interview.md` is not Source; `notes/team-notes.md` is not AO-owned |
| Canonical and legacy layer spelling | One assertion keeps `layer` alongside `phase`; one concept keeps `layers` alongside `phases`, deliberately pinning AO's retired-spelling diagnostics |

## AO output generation

The toolkit is read-only. All index/report writes ran against `/tmp/ao-kb-worker1/booking`; only the requested outputs were copied here. PyYAML 6.0.3 was installed into `/tmp/ao-kb-worker1/venv` so AO's YAML-backed verifier checks ran, rather than silently skipping them. `PYTHONDONTWRITEBYTECODE=1` prevented bytecode writes into the toolkit.

The source and passage hashes were produced by AO, including this exact successful invocation:

```sh
PYTHONDONTWRITEBYTECODE=1 /tmp/ao-kb-worker1/venv/bin/python3 \
  /Users/colthorp/Developer/atomic-ai-prompt-toolkit/plugins/ao-kb/skills/process/locators.py \
  hash /tmp/ao-kb-worker1/booking --source clinic-operations-sync --locator L12-L18
```

It returned `sha256:23a4144b5227c15ab9e62552c7b0c49df9aab1e8e9a1f21df4de984e50da2c2d`. All 17 valid located claims carry AO-generated hashes. The two deliberately invalid locators retain that valid operations passage hash as an intentionally unresolvable recorded reference.

For regeneration, copy `booking/` to the scratch root. Before generating historical weights, set the scratch cancellation claim's updatedAt back to `2026-10-20T10:00:00Z`, remove the late waitlist claim from scratch, and remove the self-service claim's link to that later claim. Those are the only historical changes. Run:

```sh
TOOL=/Users/colthorp/Developer/atomic-ai-prompt-toolkit/plugins/ao-kb/skills/process
PY=/tmp/ao-kb-worker1/venv/bin/python3
SCRATCH=/tmp/ao-kb-worker1/booking
PYTHONDONTWRITEBYTECODE=1 "$PY" "$TOOL/claim_weight.py" "$SCRATCH" \
  --build-index --as-of 2026-10-31 --out "$SCRATCH/.kb/index/kb-weights.json"
cp "$SCRATCH/.kb/index/kb-weights.json" testdata/ao-kb/booking/.kb/index/kb-weights.json
```

Restore those three assertion files from the final fixture into scratch, then run:

```sh
node "$TOOL/build_kb_index.js" "$SCRATCH" --out testdata/ao-kb/parity/booking.kb-index.json
node "$TOOL/build_kb_index.js" "$SCRATCH"
PYTHONDONTWRITEBYTECODE=1 "$PY" "$TOOL/verify_kb.py" "$SCRATCH" --max-detail 100
PYTHONDONTWRITEBYTECODE=1 "$PY" "$TOOL/locators.py" check "$SCRATCH" --json
PYTHONDONTWRITEBYTECODE=1 "$PY" "$TOOL/conflicts.py" "$SCRATCH" --check-first
cp "$SCRATCH/.kb/curation/_check_first.md" testdata/ao-kb/booking/.kb/curation/_check_first.md
```

`kb-weights.json` is the historical 21-assertion output, with its asOf/generatedAt/builder metadata retained. `parity/booking.kb-index.json` is the current 42-record output. `_check_first.md` is AO's generated dated report, not a Rhizome ranking. No site engine, ingestion, curation apply, or publication ran.

Expected AO results:

- Weights: exit 0, 21 assertions; 2 contested, 9 firm, 2 thin, 3 history, 2 open, 3 resolved. One claim predates the attestation field.
- Index builder: exit 0, 22 assertions / 9 sources / 7 concepts / 4 artifacts. Three unresolved graph declarations correspond to the two deliberately missing records (the missing source creates both an incidental relation and an evidence relation). `teleports-to` is the single noncanonical relation.
- Verifier: exit 1, exactly two broken wikilinks, three schema findings (legacy `layer`, legacy `layers`, archived vocabulary), one missing Sources section, and two typed-label findings (`supported-by` and `teleports-to`). Zero asymmetric contradictions, invalid YAML, unpaired artifact references, unresolved supported_by, confidence-ceiling violations, or origin warnings. AO also emits two stylistic headline warnings for the artifact names; these descriptive artifact headlines deliberately match their names. Site/trace/metamodel checks report unavailable because this Markdown-only fixture has no site engine.
- Locators: exit 1, 22 checked, 19 located, 17 verified, exactly `locator-malformed` and `locator-unresolved`.
- Conflicts: exit 1 because one blocking pair remains open; one pair is auto-superseded. `_check_first.md` contains four items. It uses the configured layer section order; the fixture keeps AO's reported direction and does not derive another ranking.

## GraphQL field map

`FM` means authored frontmatter. Grouped fields have the same binding rule. Native `nodeId`, `path`, `title`, `content`, `locator`, `frontmatter`, and `neighborhood` remain available alongside these fields.

| Type | Field | Source | Direction | Status |
| --- | --- | --- | --- | --- |
| Assertion, Source, Concept, Artifact | aoId | FM id | Scalar logical identity; look up by exact filter | bound (not a Rhizome identifier) |
| Assertion | headline, abstract, kind, confidence, attestation, status, horizon, priority, sensitivity, phase, validFrom, validUntil, reviewedBy, reviewedAt, withdrawnAt, withdrawnReason, createdAt, updatedAt, createdVia, speaker | Same-named FM key | Scalar | bound |
| Assertion | layer | FM layer, then phase spelling | Scalar | bound |
| Assertion | lenses, products | Same-named FM key | Ordered scalar list | bound |
| Assertion | extractedBy, speakerRole | FM extracted_by, speaker_role | Scalar | bound |
| Assertion | contradictsRows, supportsRows | FM contradicts_rows, supports_rows | Raw kind:id lists, no edges | bound |
| Assertion | provenanceSource, provenanceLocator, provenanceSpeaker, provenanceSpeakerRole, provenanceQuoteHash, provenanceExtractedBy | FM provenance object (owner-local paths) | Recorded attribution and locator | bound; source is the raw authored id or slug |
| Assertion | frontmatterSupports ... frontmatterResolvedBy | FM map-shaped connections (owner-local paths) | Raw declared targets, canonical or declared inverse | bound as raw strings; sequence shapes not projected, named by the check action |
| Assertion | sourcesSection | H2 Sources | Claim contains citation section | bound |
| AssertionSources | citations | Sources list items | Authored occurrence order, including duplicate links | bound |
| SourceCitation | recordedLink | ITEM_SUMMARY | Raw citation occurrence | bound |
| SourceCitation | sources | Link in citation bullet | Citation occurrence to Source | deficiency for repeated unanchored occurrences; unique occurrences bind |
| Assertion | sources | Desired flattening of Sources occurrences | Claim to Source, ordered duplicates | deficiency; not declared |
| Assertion | connectionsSection | H2 Connections | Claim contains declarations | bound |
| AssertionConnections | supports, refines, dependsOn, contradicts, supersedes, resolves, duplicate, relatesTo, tracesTo, grounds, exemplifies, implements, actorOf, elementDependsOn | Canonical typed bullets, kebab keys | Declaring section to Assertion | bound for body declarations; frontmatter merge deficient |
| AssertionConnections | supportedBy, refinedBy, contradictedBy, supersededBy, resolvedBy, addressedBy, blocks, groundedBy | Inverse typed bullets | Target Assertion to declaring assertion semantically; stored as declaration | bound as declared inverse; canonical normalization deficient |
| AssertionConnections | corroboratedBy | corroborated-by typed bullets | Declaring section to Source, distinct from extraction/origin | bound for body; frontmatter deficient |
| Assertion | supportedBy, refinedBy, dependedOnBy, contradictedBy, supersededBy, resolvedBy, duplicatedBy, relatedFrom, tracedFrom, groundedBy, exemplifiedBy, implementedBy, hasActor, elementDependedOnBy | @reverse of matching canonical Connections field | Target Assertion to incoming declaring section | bound navigation |
| Assertion | concepts | Every outgoing concept link | Claim to Concept | bound |
| Assertion | Direct canonical/inverse/corroboration fields | Connections body plus four FM connections shapes and alias registry | Canonical claim endpoints | deficiency; raw declarations retained in content |
| Source | name, headline, abstract, kind, sourceKind, status, ingestedAt, convertedFrom, hash, sensitivity | Same-named FM key | Scalar | bound |
| Source | topics, products, extractedAssertionIds | Same-named FM key | Ordered scalar list | bound |
| Source | sourcePaths | FM path | Scalar coerced to list; list order retained | bound for nonblank values; original slots in frontmatter |
| Source | path (AO list) | FM path | Raw files, not note identity | deficiency: reserved native field; use sourcePaths/frontmatter.path |
| Source | locatorScheme, speakerRole, extractedBy | FM locator_scheme, speaker_role, extracted_by | Scalar | bound |
| Source | extractedAssertions | FM extractedAssertionIds | Source to Assertion extraction | not declared; ids resolve by exact aoId until reference bindings land |
| Concept | name, headline, abstract, aliases, tags, products, createdAt, updatedAt | Same-named FM key | Scalar or list as authored | bound |
| Concept | layers | FM layers, then phases spelling | Ordered scalar list | bound |
| Concept | relatedConceptsSection | H2 Related Concepts | Concept contains explicit relatedness section | bound |
| RelatedConcepts | relatedConcepts | Links in Related Concepts only | Section to Concept | bound |
| Concept | relatedConcepts | Desired flattening of explicit section links | Concept to Concept | deficiency; not declared |
| Artifact | name, headline, abstract, status, artifactType, graphSyncedAt, products, sensitivity | Same-named FM key | Scalar or list as authored | bound |
| Artifact | layer, lastReviewed | FM layer/phase, last-reviewed | Scalar | bound |
| Artifact | supportedBy | FM supported_by | Artifact to Assertion | bound for slug refs; logical-ID indexing deficient |
| Artifact | additionalAssertionReferences, conceptReferences, sourceReferences, artifactReferences | Outgoing ambient body links | Artifact to target; add supportedBy for structurally represented assertion pairs | bound with explicit structural-pair rule |
| ConflictRegistry | groups | H2 groups | Registry to group | bound with exact hidden-file include |
| ConflictGroup | entries | H3 cf-NNN headings | Group to ConflictEntry | bound |
| ConflictEntry | assertions | All Assertion links in this entry; exact pair in the Assertions field row | Entry to Assertion navigation | bound navigation; exact pair scoping deficient |
| ConflictEntry | fields | Bold-label list items, in authored order | Entry to ConflictField | bound |
| ConflictField | label, value | ITEM_TITLE, ITEM_SUMMARY | Authored label and value, including unknown labels | bound |


## Verification evidence

- `make build` and `make build NO_WEB=1`: passed. The design comp was opened with local Playwright at its file URL and the browser was closed afterward.
- `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/cli/init -run TestAOKBKnowledgeFixture -count=1`: passed. The test installs through ordinary init with addon `ao-kb`, runs init check, publishes the fixture, and asserts the typed bindings and named deficiencies.
- `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/cli/init/`: fails only at `TestBundledStarterQueryRecipesValidateAgainstEffectiveSchema/ao-kb/defaults`, because the skeleton declares query recipes but that worker's assets are absent. The ontology declaration now has assets and compiles.
- `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/ontology/query ./pkg/ontology ./pkg/vault/obsidian -count=1`: passed, including both shared regressions. Explicit hidden inclusion and list-item first-line links each failed before their fixes.
- `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/web -run 'TestExactGraphQLHydratesOneDurableFactSnapshot|TestPublicQueryRecipeHydratesOneDurableFactSnapshot' -count=1`: passed after removing the unsuccessful raw-frontmatter experiment. The full query package's staged-value tests also passed.
- `./scripts/rzm validate`: exit 1 for one pre-existing issue in SPEC-0133 (`spec-status: approved` is not a member of the repository SpecStatus enum); no identifier or broken-link findings. This worker did not alter that separately owned spec.
- `make check-fast`: passed. `make check-full`: fails only for the missing AO KB recipes; the other race-enabled unit packages, fast checks, and benchmark contracts pass. That stops the full gate before its web-unit and integration targets. An earlier `make check` exposed the rejected frontmatter experiment as well as the recipe gap; those snapshot/staged regressions were removed and verified separately.
- The built binary ran `init --path <scratch> --workflow none --addons ao-kb --agents none --hooks off --search off --binary-manager external`, then `init --path <scratch> --check`, then `index` after appending the exact conflict-registry include. All three exited 0 and all 61 original fixture files retained identical bytes.

The two shared fixes have separate commits. Hidden-source discovery now honors explicit include prefixes and excludes. Embedded list neighbors now retain headline links in both typed queries and native neighborhoods; ontology materialization version 18 forces existing read models to rebuild. The lead owns the corresponding subsystem-note updates (`vault-core` and `ontology`); this worker did not edit the separately owned documentation, templates, recipes, views, actions, or scripts.
