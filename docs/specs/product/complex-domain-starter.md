---
type: ProductSpec
summary: "Defines the complex-domain starter template as an Agentic Engineering extension for domain modeling, requirements traceability, saved query recipes, configured views, and workflow skills."
id: SPEC-0062
spec-status: active
last-updated: 2026-09-07
aliases:
  - SPEC-0062
  - Complex domain starter
  - complex-domain starter
---

# Complex domain starter

## Summary

`complex-domain` is a user-selected starter template for projects where product behavior is constrained by a large, evolving domain: domain types, domain processes, user workflows, source documents, and many requirements that must stay traceable into specs and implementation work.

The starter extends the Agentic Engineering workflow instead of replacing it. `agentic-engineering` remains the delivery router for specs, stories, efforts, planning, implementation, quality evidence, alignment, reconciliation, and compounding. `complex-domain` adds the upstream domain and requirement layer that feeds that router: source-backed requirements, feature-area organization, process/workflow modeling, rule traceability, coverage views, and skill instructions that make agents load the right domain context before drafting or changing specs.

Selecting `complex-domain` during `rzm init` requires `agentic-engineering`. If a user selects both `agentic-engineering` and `complex-domain`, init resolves one effective Agentic Engineering install and one effective `complex-domain` install. The result must compose cleanly with `core`, default activated addons such as `action-items`, and the existing starter asset families described by [[core-template-and-template-dependencies]].

## Goals

- make `complex-domain` an explicit starter template that users can select during init
- require `agentic-engineering` without breaking the case where users select both templates
- model feature areas, domain contexts, domain types, domain processes, user workflows, requirement sources, and atomic requirements as queryable typed notes
- let specs, stories, and acceptance criteria link to upstream requirements and the domain things those requirements constrain
- provide saved query recipes that pull the correct requirement/domain context for a feature area, domain concept, process, workflow, or spec
- provide configured views that help teams triage, filter, cover, and maintain requirements without hand-building dashboards
- provide starter skills and Agentic Engineering router/adapter overlays that teach agents exactly when and how to load domain context
- keep domain knowledge maintainable by separating source evidence, curated requirements, domain models, delivery specs, and implementation evidence
- support `domain-backport` and traceability review so implementation discoveries can update domain requirements after delivery

## Non-Goals

- replacing the Agentic Engineering delivery workflow, effort lifecycle, story lifecycle, or closure procedure
- replacing external product-management, issue-tracking, compliance, or ALM systems
- treating prose-only semantic search as sufficient proof of requirement coverage
- automatically generating final requirements from source documents without human review
- requiring every domain note to be perfect before a spec can be drafted
- forcing every requirement into a single monolithic requirements document
- implementing the skill-template overlay renderer; [[skill-template-overlays]] owns that technical mechanism
- shipping a full visual ontology editor before the starter can provide useful schema, recipes, views, and skills

## User Stories

### US1 - Select `complex-domain` and receive a coherent Agentic Engineering domain-requirements workflow without duplicate starter assets
- id:: ^SPEC-0062-US1
- summary:: Select `complex-domain` and receive a coherent Agentic Engineering domain-requirements workflow without duplicate starter assets.
- status:: ready

The template should feel like one deliberate workflow choice, not a manual checklist of dependent starter pieces.

#### Acceptance Criteria

- Complex-domain requires Agentic Engineering. ^SPEC-0062-US1-AC1
  - Given a user selects `complex-domain`, init resolves `agentic-engineering` as a required dependency of the selected workflow.
  - The resolved template explanation distinguishes `complex-domain` as explicitly selected and `agentic-engineering` as required when the user did not select `agentic-engineering` directly.
  - The generated repo contains the Agentic Engineering docs, ontology, query recipes, router, workflow skills, and managed docs needed for the normal delivery workflow.
- Selecting both templates dedupes cleanly. ^SPEC-0062-US1-AC2
  - Given a user selects both `agentic-engineering` and `complex-domain`, init installs one effective copy of each shared Agentic Engineering asset family.
  - Managed docs contain at most one `agentic-engineering` starter block and at most one `complex-domain` starter block.
  - Starter skills, query recipes, configured views, and ontology files fail fast on real collisions rather than silently overwriting one another.
- Complex-domain assets install as tracked repo workflow assets. ^SPEC-0062-US1-AC3
  - Init installs complex-domain ontology under a starter-owned path such as `.rhizome/ontology/complex-domain.graphql`.
  - Init installs complex-domain recipes under `.rhizome/query-recipes/`.
  - Init installs complex-domain configured views under `.rhizome/views/`.
  - Init installs complex-domain skills under the selected agent skill surfaces, and any overlays are rendered into the affected skill templates through [[skill-template-overlays]].

### US2 - Capture domain types, processes, workflows, and feature areas as durable typed notes that requirements can cite
- id:: ^SPEC-0062-US2
- summary:: Capture domain types, processes, workflows, and feature areas as durable typed notes that requirements can cite.
- status:: ready

Complex projects need more than a flat glossary. The starter should help the team preserve the structure that makes requirements understandable.

#### Acceptance Criteria

- Domain model notes have typed homes and authoring guidance. ^SPEC-0062-US2-AC1
  - The ontology defines note families for feature areas, domain contexts, domain types, domain processes, and user workflows.
  - Each family has enough required fields to make notes filterable and traceable without making early modeling painfully heavyweight.
  - The starter docs explain where each family belongs and when to create a note versus link an existing one.
- Domain entities link to the requirements they constrain. ^SPEC-0062-US2-AC2
  - Requirements can link to one or more feature areas, domain contexts, domain types, domain processes, and user workflows.
  - Domain notes can show inbound requirements through the ontology read model rather than duplicating every requirement in prose.
  - Rules are captured as atomic `Requirement` notes using the closest `requirement-kind`, so source-backed constraints stay traceable through the same lifecycle as other requirements.
- Processes and workflows remain distinct. ^SPEC-0062-US2-AC3
  - Domain process notes describe business/system processes, decisions, states, handoffs, invariants, and rule points.
  - User workflow notes describe actor-facing journeys, tasks, screens, transitions, exceptions, and pain points.
  - Requirements can connect to both when a process constraint and an actor workflow both matter.

### US3 - Turn source documents, transcripts, workshops, and domain notes into reviewable candidate requirements with provenance
- id:: ^SPEC-0062-US3
- summary:: Turn source documents, transcripts, workshops, and domain notes into reviewable candidate requirements with provenance.
- status:: ready

The starter should make evidence extraction disciplined enough for complex work without pretending that extraction is the same as acceptance.

#### Acceptance Criteria

- Source-backed requirement capture is explicit. ^SPEC-0062-US3-AC1
  - A source refresh preserves the prior cited evidence/version and identifies the new evidence used for extraction. An unchanged extraction rerun reuses the source and matching candidates instead of duplicating them. Unknown prior evidence is explicitly qualified.
  - The ontology includes a source family such as `RequirementSource` for source documents, transcripts, workshops, recordings, imported spreadsheets, regulatory references, and stakeholder notes.
  - Each source records source kind, date or version when known, provenance, confidence, and review status.
  - Extracted requirements cite their source locations or source notes so agents can return to evidence during disputes.
- Requirements are atomic and reviewable. ^SPEC-0062-US3-AC2
  - Candidate acceptance records an authorized human decision; provenance quality, confidence, and structural links alone do not constitute acceptance. Existing authorization carries across the workflow without routine reapproval.
  - A curated `Requirement` captures one requirement-level obligation, constraint, capability, rule, or quality expectation.
  - Requirements have stable identifiers, lifecycle status, priority or criticality, confidence, owner or reviewer when known, verification hints, and source links.
  - Ambiguous or conflicting extracted statements become action items or provisional requirements rather than being silently merged into final requirements.
- Duplicate and conflict review is supported. ^SPEC-0062-US3-AC3
  - Source changes surface conflicting candidates and affected accepted requirements without rewriting accepted scope or suppressing earlier evidence.
  - The starter includes a recipe and skill flow for checking candidate requirements against existing requirements before creating new ones.
  - The flow surfaces likely duplicates, conflicts, weaker/stronger variants, superseded sources, and source disagreements.
  - The skill output asks for human judgment when requirements conflict or when a source appears authoritative but obsolete.

### US4 - Draft or update specs from the relevant domain model and requirement neighborhood instead of rediscovering context from scratch
- id:: ^SPEC-0062-US4
- summary:: Draft or update specs from the relevant domain model and requirement neighborhood instead of rediscovering context from scratch.
- status:: ready

Specs should remain focused delivery contracts, but they should know what upstream domain material they are satisfying.

#### Acceptance Criteria

- Spec authoring loads domain context first. ^SPEC-0062-US4-AC1
  - A known anchor uses structural context first; topic discovery begins with bounded typed inventory. Semantic recipes are explicitly candidate discovery. Required inputs are limited to the current question and reused when still valid.
  - Before creating or changing a spec, the `agentic-engineering specify` phase loads related requirements, feature areas, domain types, processes, workflows, and sources through saved query recipes.
  - The skill treats deterministic recipe results as the primary inventory and uses semantic search only to refine or explain unresolved context.
  - The skill summarizes the loaded domain context and highlights gaps before drafting a new spec.
- Specs link upstream requirements without copying the whole domain. ^SPEC-0062-US4-AC2
  - Specs can link to requirements, feature areas, processes, and workflows that govern the desired behavior.
  - User stories and acceptance criteria can cite requirement identifiers or domain nodes when the trace matters at story granularity.
  - The spec body keeps user-facing behavior, goals, non-goals, stories, and requirements clear instead of becoming a full domain encyclopedia.
- Coverage gaps are visible before planning. ^SPEC-0062-US4-AC3
  - Results distinguish unresolved anchor, missing authored coverage, unavailable/degraded retrieval, stale source evidence, and capped traversal. Agents do not claim complete coverage from a partial inventory.
  - When a spec covers only part of a feature area, process, workflow, or requirement set, the skill names uncovered requirements or unresolved action items.
  - The skill recommends whether to broaden the spec, create a separate spec, defer requirements intentionally, or ask for domain clarification.
  - Planning does not treat unreviewed domain gaps as silently accepted scope.

### US5 - Plan, implement, align, and reconcile with the domain requirements that constrain the selected spec and stories
- id:: ^SPEC-0062-US5
- summary:: Plan, implement, align, and reconcile with the domain requirements that constrain the selected spec and stories.
- status:: ready

Domain traceability should survive the handoff from spec authoring into implementation and closure.

#### Acceptance Criteria

- Effort and plan skills preserve upstream traceability. ^SPEC-0062-US5-AC1
  - Context may be reused across engineering phases when targets, source revisions, and scope remain applicable; refresh only affected evidence when those inputs change.
  - Effort creation records the frozen spec/story scope plus the upstream requirement/domain context that materially constrains that scope.
  - Planning loads requirement trace packs for the selected specs and stories before choosing architecture, tests, and implementation phases.
  - Foundation phases call out domain schema, rule, workflow, and traceability decisions that later implementation phases will rely on.
- Implementation skills check impacted domain requirements. ^SPEC-0062-US5-AC2
  - Before implementation, the skill loads requirements tied to the frozen specs, stories, acceptance criteria, and feature areas.
  - When code changes reveal a requirement mismatch, missing rule, new exception, or changed workflow, the skill records the finding for `domain-backport` rather than burying it in implementation notes.
  - Tests, docs, and acceptance evidence can reference requirement ids when that improves auditability.
- Alignment and reconciliation close the loop. ^SPEC-0062-US5-AC3
  - AE alignment explicitly routes domain findings to traceability review; reconciliation passes supported updates to domain-backport. Authored links, resolved targets, obligation satisfaction, and authorized lifecycle changes are distinct. Partial delivery does not mark an entire requirement implemented. Source/code disagreement requires evidence before declaring the source stale.
  - The `agentic-engineering` alignment resource checks whether delivered behavior still satisfies linked requirements and whether uncovered or stale requirements remain.
  - Its reconciliation resource updates specs and delivery artifacts, while `domain-backport` updates requirements, domain notes, process notes, and workflow notes when implementation taught the team something durable.
  - Its compounding resource captures repeated traceability or context-loading friction as future skill, recipe, view, validation, or starter improvement work.

### US6 - Use configured views to inspect, filter, group, and maintain complex-domain requirements and domain knowledge
- id:: ^SPEC-0062-US6
- summary:: Use configured views to inspect, filter, group, and maintain complex-domain requirements and domain knowledge.
- status:: ready

The starter should make the knowledge base usable as a workbench, not just as files agents know how to query.

#### Acceptance Criteria

- Requirements-by-feature-area is a first-class view. ^SPEC-0062-US6-AC1
  - A standalone configured view lists requirements with feature area, status, priority or criticality, confidence, source, coverage, and linked spec/story indicators.
  - Users can filter by feature area, status, source, priority, confidence, requirement kind, and review state.
  - Users can group by feature area, process, workflow, or status without losing row identity.
- Coverage-gap views highlight missing delivery links. ^SPEC-0062-US6-AC2
  - A configured view lists requirements with no linked spec/story/acceptance criterion where the requirement status implies it should be considered for delivery.
  - A configured view lists specs or stories whose upstream requirement coverage is thin, stale, or only semantically inferred.
  - The view surfaces warnings when results are capped, recipe-backed, or partially degraded.
- Domain maintenance views are actionable. ^SPEC-0062-US6-AC3
  - A requirements-by-process view groups requirements by process and shows kind, status, priority, confidence, affected workflows, sources, and linked specs.
  - Unresolved ambiguity is tracked through action items and surfaced through the action-item views installed by the default addon path.
  - A source-review view shows sources with unreviewed candidate requirements, stale extraction dates, conflicts, or missing provenance.

### US7 - Keep requirements, domain models, recipes, views, and skill instructions current as the project changes
- id:: ^SPEC-0062-US7
- summary:: Keep requirements, domain models, recipes, views, and skill instructions current as the project changes.
- status:: ready

Complex-domain support should turn repeated friction into durable leverage rather than decay into a second stale documentation layer.

#### Acceptance Criteria

- Validation catches broken starter contracts. ^SPEC-0062-US7-AC1
  - Starter validation covers ontology schema, aliases, query recipes, configured views, skill overlays, managed docs, and asset collisions.
  - Query recipes fail validation when their roots, fields, row paths, required inputs, or output contracts no longer match the ontology query schema.
  - Views fail validation when their source adapters, fields, filters, grouping, or recipe row paths no longer resolve.
- Skills make maintenance work routine. ^SPEC-0062-US7-AC2
  - Domain skills consume base session, authoring, mutation, and validation mechanics and AE delivery authority; they prescribe only domain-specific retrieval and outputs. Read-only review does not require a blanket mutation-validation suite.
  - Domain-backport and traceability-review skills describe exactly how to update requirements and domain notes after delivery, discovery, or review.
  - Skills name the recipes and views to consult before editing requirements.
  - Skills avoid hard-coding ontology field names when they can rediscover the live schema or authoring guide.
- Staleness and scope drift are visible. ^SPEC-0062-US7-AC3
  - Source-impact retrieval returns bounded paths to affected requirements and delivery targets, with source locations and review/version qualifications; it does not claim automatic freshness detection.
  - Requirements can record review date, source version, supersession, or status so stale knowledge can be filtered.
  - When a source changes, recipes can surface impacted requirements, domain notes, specs, and stories.
  - When a spec changes, recipes can surface upstream requirements that may need re-review.

## Requirements

### Template Behavior

- `complex-domain` MUST be a named starter template selectable through the same init surfaces that select `agentic-engineering` and `project-kb`.
- `complex-domain` MUST declare `requires: [agentic-engineering]` in starter metadata.
- Template resolution MUST be transitive and deduplicated: selecting `complex-domain` resolves `agentic-engineering` once even if `agentic-engineering` is also explicitly selected.
- Resolver output MUST explain which templates were explicit, required, and default-activated before init writes assets.
- Init MUST validate the resolved asset set for docs, ontology, query recipes, configured views, skills, managed docs, and overlays before writing partial output.
- `complex-domain` MUST NOT duplicate Agentic Engineering ontology types, router, engineering docs, or managed-doc blocks.
- `complex-domain` SHOULD activate `action-items` only through the dependency/default-addon behavior already owned by `agentic-engineering`; it should not independently force a second action-item path.
- Starter docs SHOULD include a short README that explains that complex-domain is upstream of Agentic Engineering delivery, not a replacement for specs.

### Ontology Model

- The starter MUST define typed note families for at least `FeatureArea`, `DomainContext`, `DomainType`, `DomainProcess`, `UserWorkflow`, `RequirementSource`, and `Requirement`.
- The starter SHOULD define `RequirementSet` or an equivalent grouping type when teams need curated batches such as "MVP scheduling requirements" or "claims adjudication regulatory requirements."
- The starter MAY define `CoverageReview` when traceability review needs a durable review note rather than only a configured view.
- `Requirement` MUST have a stable preferred identifier, with a recommended prefix such as `REQ-`.
- Domain note families MUST expose enough structured fields to support status, owner/reviewer, review state, priority or criticality, confidence, source provenance, and stale review filtering.
- Requirements MUST link to source evidence directly or through a requirement source/candidate relationship.
- Requirements SHOULD link structurally to feature areas, domain contexts, domain types, processes, workflows, specs, stories, and acceptance criteria when those relations are authored.
- The ontology SHOULD distinguish curated requirements from source-extracted candidates so agents do not treat unreviewed extraction as accepted scope.
- The ontology MUST use neighbor-derived backlinks for ambient visibility rather than requiring duplicate authored lists on both sides of every relation.
- The starter MUST provide authoring guidance that keeps requirements atomic, testable, source-backed, and reviewable.

### Saved Query Recipes

`domain-inventory-pack` provides bounded deterministic typed discovery with optional `find` and `first` inputs. `domain-topic-survey` is semantic candidate discovery after structural context, never a coverage inventory. Row recipes expose page metadata and warnings while preserving `notes.nodes` identity. A cap, absent authored links, unresolved anchor, unavailable retrieval, and stale source evidence are distinct outcomes. Incoming requirement links are candidates until their authored delivery relation is checked; links alone never prove satisfaction.

- Complex-domain recipes MUST follow [[saved-query-recipes]] and execute through the ontology GraphQL query surface.
- Recipes that answer inventory, coverage, or traceability questions MUST use deterministic typed roots or indexed relation traversal first.
- Semantic search MAY be used as a refinement step for discovery, explanation, and candidate matching, but MUST NOT be the only proof that a requirement is covered or uncovered.
- Recipes used by views MUST declare a row path or output contract that lets the configured view engine resolve each row to canonical node identity.
- Recipes MUST include interpretation guidance for empty, partial, capped, high-volume, and degraded results.
- Recipes MUST include adaptation guidance that preserves the workflow question when ontology field names or relation names change.
- The starter SHOULD ship at least these recipes:
  - `domain-topic-survey`: broad typed survey for existing domain notes, requirements, specs, decisions, and references around a topic.
  - `domain-context-pack`: anchored pack for a feature area, domain type, process, or workflow, returning related requirements, sources, specs, and stories.
  - `requirement-trace-pack`: anchored pack for one or more requirements, returning source evidence, domain links, specs, stories, acceptance criteria, efforts, and implementation evidence links when available.
  - `spec-domain-context-pack`: anchored pack for a spec/story/acceptance criterion, returning upstream requirements and domain nodes plus uncovered siblings from the same feature/process/workflow.
  - `feature-area-backlog-pack`: feature-area inventory for requirements grouped by status, priority, review state, source, and delivery coverage.
  - `coverage-gap-pack`: deterministic list of requirements that appear ready for delivery consideration but lack spec/story/acceptance links or have stale coverage.
  - `changed-domain-impact-pack`: impact pack from a changed requirement, source, process, or workflow to affected specs, stories, efforts, and views.
  - `requirements-ingest-duplicate-pack`: candidate-deduplication pack for comparing extracted statements against existing requirements.
- Recipe validation MUST run in starter tests and through `rzm agent validate query-recipes`.

### Configured Views

- Complex-domain configured views MUST use the general view engine from [[configured-view-engine-and-repo-config]].
- Views MUST preserve canonical `NodeRef` identity for every row/card so users can open the node workspace from any result.
- Views SHOULD be standalone rail views by default unless a view is clearly type-mounted to a domain type or interface.
- The starter SHOULD ship at least these views:
  - `complex-domain.requirements-by-feature-area`: requirements grouped and filtered by feature area, status, priority, source, confidence, and coverage.
  - `complex-domain.uncovered-requirements`: requirements ready for delivery consideration but missing spec/story/acceptance links.
  - `complex-domain.requirements-by-process`: requirements grouped by process, kind, status, priority, confidence, source, affected workflows, and linked specs.
  - `complex-domain.sources-needing-review`: sources with unreviewed extractions, stale review dates, conflicts, or missing provenance.
- Views MUST expose field labels, filters, sort defaults, grouping defaults, source warnings, and bounded pagination.
- Views SHOULD make status/priority/review fields safely editable only when the configured view source adapter declares them write-capable and the edit-session model can stage the change.
- Views MUST NOT infer coverage solely from semantic similarity; coverage indicators must come from authored structural links, derived backlinks, or recipe output contracts that identify explicit trace relations.

### Skills And Workflows

Retain the eight domain entrypoints and compose with base Rhizome for sessions, readiness, authoring, identifiers, locators, safe mutations, and proportional validation. AE owns delivery authority and phase transitions. Its alignment and reconciliation references hand domain findings and authorized durable updates to `traceability-review` and `domain-backport`. Known-source refresh preserves prior evidence and candidate uncertainty; code differences alone do not establish source obsolescence.

- The starter MUST include a router skill named `complex-domain` that explains when to use complex-domain workflows and when to stay in ordinary Agentic Engineering work.
- Starter skill descriptions MUST be short routing contracts; detailed procedure belongs in the skill body.
- Skills MUST rediscover live ontology contracts through `runtime-authoring-context`, `ontology-authoring-guide`, `ontology-query-schema`, or saved query recipes rather than hard-coding stale field lists.
- Skills MUST name the exact query recipes to run at phase boundaries where missing context would change the decision.
- Skills MUST summarize loaded recipe results before drafting or editing when the user needs to confirm requirement scope.
- Skills MUST ask for human judgment before accepting conflicting requirements, changing requirement lifecycle status, or treating an unreviewed source extraction as final.
- Skills SHOULD be authored as small phase-specific workflows:
  - `requirements-ingest`: turn source material into source-backed candidate requirements, action items, and duplicate/conflict review notes.
  - `requirements-curation`: promote, merge, split, supersede, or reject candidate requirements with provenance preserved.
  - `domain-modeling`: create and maintain feature areas, domain types, processes, and workflows.
  - `workflow-mapping`: model actor-facing workflows and connect them to processes, requirements, and specs.
  - `traceability-review`: inspect requirement/spec/story/acceptance coverage and record gaps.
  - `spec-from-domain`: coordinate with `agentic-engineering specify` to draft or update specs from a domain context pack.
  - `domain-backport`: update requirements, rules, processes, workflows, and sources after delivery or discovery changes.
- Complex-domain overlays SHOULD augment these current Agentic Engineering router/adapter slots when the overlay mechanism exists:
  - `agentic-engineering specify`: load domain-context and requirement-trace packs before drafting or changing specs.
  - `agentic-engineering effort`: record material upstream domain/requirement context alongside frozen spec/story scope.
  - `agentic-engineering plan`: load requirement trace packs before selecting architecture, tests, and implementation phases.
  - `agentic-engineering implement`: re-check linked requirements before coding, investigation, or tests and record domain mismatches for `domain-backport`.
  - `agentic-engineering`: augment the compounding resource to capture repeated domain/traceability friction as recipe, view, validation, or skill improvement work.
  - `ingest-transcript`: route domain-heavy transcripts into requirement-source extraction instead of generic meeting notes when appropriate.
- `domain-backport` owns domain reconciliation after delivery or discovery: it updates requirements, rules, processes, workflows, feature areas, sources, and related action items while the Agentic Engineering reconciliation resource owns specs and delivery artifacts.

### Managed Docs

- The complex-domain managed-doc block MUST be written only when `complex-domain` is active in the resolved template set.
- The block MUST state that `complex-domain` extends Agentic Engineering with domain and requirement traceability.
- The block MUST name the primary skills and the views/recipes agents should use, but it MUST NOT duplicate the full ontology or skill procedures.
- The block SHOULD include a short reminder that semantic matches are discovery aids and structural requirement links are the coverage contract.

### Validation And Tests

- Init resolver tests MUST cover selecting only `complex-domain`, selecting both `agentic-engineering` and `complex-domain`, dependency dedupe, default addon composition, and asset collision failure.
- Init integration tests MUST prove the installed ontology, recipes, views, skills, managed docs, and overlays validate together.
- Query recipe tests MUST compile every complex-domain recipe against the installed ontology query schema.
- View validation tests MUST prove every starter view resolves its source, row path, fields, filters, grouping, and mount.
- Skill rendering tests MUST prove complex-domain overlays are applied to current Agentic Engineering router/adapter skills only when `complex-domain` is active and that final installed skills contain no overlay markers.
- Ontology validation tests MUST cover representative notes for feature area, domain type, process, workflow, requirement source, and requirement.
- End-to-end fixture tests SHOULD initialize a repo with `complex-domain`, create a small domain/requirement/spec graph, run traceability recipes, and execute the configured views.

## Open Questions

- Should v1 store source-extracted candidates as embedded nodes inside `RequirementSource`, as draft `Requirement` notes, or both with an explicit promotion path?
- Should `Requirement` support direct links to acceptance-criterion block targets in v1, or should trace initially stop at spec/story links until the linkable embedded-node UI is smoother?
