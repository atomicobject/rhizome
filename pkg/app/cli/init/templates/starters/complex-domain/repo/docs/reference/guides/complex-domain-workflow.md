---
type: ReferenceDoc
summary: "Guide for using source-backed domain knowledge and requirements as the upstream constraint layer for spec-driven delivery."
reference-kind: guide
status: active
---

# Complex-domain workflow

## Purpose and ownership

Complex-domain notes preserve sources, domain concepts, processes, actor workflows, and requirements that explain what constrains delivery. Specs remain the delivery contract.

The `complex-domain` skills own domain distinctions, provenance, candidate and accepted meaning, structural traceability, and domain reconciliation. The installed `rhizome` skill owns sessions, exact reads, live schema and authoring discovery, safe mutation, readiness, and proportionate validation. Agentic Engineering owns specs, efforts, approved plans, implementation evidence, alignment, reconciliation, and completion truth.

## Note families

- `FeatureArea`: product or capability area used for planning and coverage.
- `DomainContext`: bounded business, regulatory, or system context with shared vocabulary and rules.
- `DomainType`: domain object, value, stateful entity, vocabulary term, or external concept.
- `DomainProcess`: business or system operation, states, decisions, handoffs, invariants, and rule points.
- `UserWorkflow`: actor-facing goal, task flow, interaction path, variant, or friction.
- `RequirementSource`: source document, transcript, workshop, recording, spreadsheet, regulation, or stakeholder evidence.
- `Requirement`: one atomic obligation, capability, constraint, data rule, workflow expectation, quality expectation, or compliance expectation.

## Evidence and authority

Keep these separate:

- A source and exact location support a claim; preserve its identity, version/date, provenance, and review state.
- A `candidate` requirement is extracted or proposed context.
- An `accepted` requirement records an authorized human decision.
- An authored link records a relationship.
- A readable story or acceptance-criterion target confirms that the locator resolves.
- Delivery evidence can demonstrate some or all of an obligation.
- An `implemented` lifecycle change requires full-obligation evidence and authorization.

Confidence, complete metadata, graph proximity, or semantic similarity does not promote a candidate. Existing authorization carried by an approved workflow remains applicable without routine reapproval.

Use `ActionItem` for an unresolved accountable decision. Do not invent an assignee or silently turn uncertainty into scope.

## Retrieval

Reuse a known anchor and choose the smallest structural pack:

- `domain-context-pack` for one known domain note.
- `requirement-trace-pack` for one requirement and its source/domain/delivery context.
- `spec-domain-context-pack` for a spec and upstream requirement/domain candidates.
- `changed-domain-impact-pack` for a known changed source, requirement, process, workflow, feature area, context, or type.
- `feature-area-backlog-pack`, `coverage-gap-pack`, and `sources-needing-review-pack` for bounded triage rows.

For a topic-only request, run `domain-inventory-pack` for each relevant live type. It is deterministic and bounded. Use optional `find` only when supported by the current question. Run `domain-topic-survey` afterward when semantic ranking would help identify remaining candidates; its scores do not prove authority, duplication, coverage, or satisfaction.

Qualify results precisely:

- An unresolved anchor proves only that the requested note did not resolve.
- A resolved note with absent authored links supports a missing-authored-coverage finding for that note.
- A degraded or failed capability leaves an evidence gap.
- A capped inventory or warnings prevent a completeness claim. Narrow with a supported typed/property selector or a specific anchor; filtering only returned rows is insufficient.
- A bounded linked/backlinked list that reaches its limit may be truncated. Confirm named targets with focused packs.
- Stale source evidence requires source/version evidence. Code disagreement alone is insufficient.

Reuse still-applicable context across engineering phases. Refresh only affected context when an anchor, source revision, selected scope, delivery evidence, or capability state changes.

## Source ingestion and refresh

Create or reuse one `RequirementSource` for an artifact. Extract each distinct obligation as one candidate Requirement with exact source locations. Rerunning an unchanged extraction reuses the source and matching candidates.

When a known source changes:

1. Preserve the prior source version/date and cited evidence.
2. Record the new version/date and exact changed evidence.
3. Load `changed-domain-impact-pack` and focused requirement traces.
4. Distinguish observed source changes, potentially affected links, verified delivery implications, conflicts, and decisions still needed.
5. Create only genuinely new atomic candidates; keep accepted scope intact until an authorized curation decision.

Unknown prior content remains an explicit evidence gap.

## Modeling and curation

Use `DomainProcess` for business/system operation and `UserWorkflow` for actor-facing tasks and variants. Link them when a user journey participates in the process, while preserving the distinct models.

Curation presents atomic wording, source evidence, duplicate/conflict links, affected targets, and a recommended disposition. Apply acceptance, rejection, supersession, priority, confidence, or other lifecycle changes only under explicit or already-carried authorization. Preserve rejected and superseded history.

## Spec and delivery handoff

Before specification, supply accepted constraints, candidate context, process rules, workflow variants, source qualifications, conflicts, and action items. Link the spec to the smallest durable upstream notes.

During effort setup and planning, preserve the selected domain context with scope so implementation can reuse it. Candidate and conflicting requirements inform hypotheses; they do not become unreviewed behavior changes.

During alignment, invoke `traceability-review` for missing authored links, invalid locators, uncovered obligations, partial delivery, stale evidence, conflicts, and retrieval limits. During reconciliation, invoke `domain-backport` for supported domain updates. Agentic Engineering retains normative spec and completion decisions.

## Delivery examples

### Partial delivery

Requirement R needs criteria A and B to demonstrate its full obligation. Delivery evidence proves A while B remains open. Record the evidence and partial coverage, repair only authorized links, and leave R short of `implemented`. A later reconciliation may apply the lifecycle update after evidence demonstrates the full obligation and that update is authorized.

### Source and code disagreement

Source version 1 supports accepted requirement R. Version 2 changes the rule while another source disagrees, and current code follows a third behavior. Preserve version 1 and its citations, record version 2 and the conflicting evidence, identify R and its delivery targets, and recommend a disposition. Do not rewrite accepted scope or declare a source stale merely because code differs.

## Skills and views

Keep the eight workflow entrypoints: `complex-domain`, `requirements-ingest`, `requirements-curation`, `domain-modeling`, `workflow-mapping`, `spec-from-domain`, `traceability-review`, and `domain-backport`.

Use the configured views as workbenches:

- `complex-domain.requirements-by-feature-area`
- `complex-domain.uncovered-requirements`
- `complex-domain.requirements-by-process`
- `complex-domain.sources-needing-review`

Open the underlying notes before lifecycle, freshness, coverage, or satisfaction claims. After mutations, use the Rhizome validation route to select checks in proportion to the changed note family and links. Read-only reviews need no blanket validation.
