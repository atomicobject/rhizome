---
type: ReferenceDoc
summary: "Proposed Complex Domain workflow, recipe, and AE overlay changes with ownership, acceptance amendments, and verification exits."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Complex Domain agent experience plan

## Decision and authority

Recommend retaining the eight domain skills, making their retrieval proportional, repairing bounded traceability recipes, and adding explicit AE alignment/reconciliation handoffs. Preserve the existing ontology and view contracts. This fixes identifiable workflow gaps without requiring new source-maintenance infrastructure or a skill-topology migration.

This analysis began as the planning deliverable for Complex Domain agent experience. Drew subsequently directed the tasks to drive through delivery. The coordinator authorized the retained eight skills, proportional mechanics, bounded recipe fixes, AE handoffs, SPEC-0062 amendments, focused behavior test, and additive original-effort reconciliation. The effort now records validated approval and real frozen targets. Main/release and view/runtime expansion remain excluded.

The base composition boundary is confirmed; AE owns the accepted alignment/reconciliation slot definitions and SPEC-0063 amendments, while Domain owns consumers. The original effort remains active with a concrete coverage-view gap assigned to coordinator follow-up. Its ceremonial closure is no longer a prerequisite for independent redesign. A's model pilot currently covers only bug/typo scenarios; domain fixtures are deterministically checked, with domain model-execution evidence explicitly deferred. The detailed design below is the implementation contract, with those refinements taking precedence over earlier planning-stage dependency language.

Inspected source: `3b93489c4e404981fc4bb193e75c0d70e9eac7a7`, which was both checkout HEAD and the requested integration ancestor before this planning change. Staging PR: #243. The first planning commit changed only this note and the owned effort; the authorized delivery now changes the named canonical assets and SPEC-0062.

Before publication, incorporated staging documentation commit `c534bfb89809ec7ac5fbe6ab97c83aa2e295f29f` by rebasing the child branch and read the [research and supervisor handoff](agent-experience-research-handoff.md). Its authority, Complex Domain, safe-write, and evidence boundaries agree with this proposal. Production-source findings remain grounded in the original revision; the later staging commit changed documentation only. No task model-setting change is claimed.

## Verified findings

Paths below are repository-relative. Canonical starter root is `pkg/app/cli/init/templates/starters/complex-domain/`.

| Finding | Source and implication |
| --- | --- |
| Universal mechanics are repeated and disagree with current base guidance. | All eight `skills/*/SKILL.md` files prescribe broad validation; the router starts with `--profile code --ontology`, and leaf skills request ontology-enabled sessions. Canonical base `pkg/app/cli/init/templates/skills/markdown/rhizome/SKILL.md` owns minimal session reuse and proportionate validation. Domain skills should name domain inputs and outcomes, then use that owner. |
| Recipe-first is being mistaken for deterministic-first. | `query-recipes/complex-domain.yaml`, `domain-topic-survey`, executes `notes(type: $type, semantic: $topics, first: $first)`. Its adaptation guidance correctly calls it semantic candidate discovery, but `skills/requirements-ingest/SKILL.md` and `skill-overlays/specify.yaml` use it as an initial inventory. A saved recipe is not necessarily a structural query. |
| Source impact is bounded but incomplete for some modeled families. | `changed-domain-impact-pack` expands source requirements and requirement specs, but process/workflow branches omit their typed requirements; context/type branches are absent. `domain-context-pack` lacks a source-specific requirements branch. Generic backlinks are impact candidates and do not prove satisfaction. |
| Spec context reads only outbound links. | `spec-domain-context-pack` uses `linked(type: "Requirement", first: 50)`. A requirement with an authored `specs` link to the spec can be missed if the spec has no reciprocal body link. Add incoming candidates separately and confirm their typed delivery relation. |
| Inventory completeness is not exposed by selected fields. | The three row recipes use `notes(... first: 200)` without selecting `pageInfo` or `warnings`. Their instructions suggest filtering or widening, but they accept no filter/limit inputs. `domain-topic-survey` has `first` but also omits completion fields. Anchored `linked`/`backlinked` lists use limits of 30–80 and expose no page metadata. |
| Closure handoffs are missing at the phase boundary. | Existing overlays target specification, effort setup, planning, implementation, compounding, and transcript ingestion. AE `references/alignment.md` and `references/reconciliation.md` have no slots or domain instructions. SPEC-0062 US5 AC3 explicitly requires the alignment resource to check linked requirements. Standalone `domain-backport` exists, but that does not ensure AE invokes it. |
| Curation and backport can imply more authority than evidence grants. | Curation allows candidate acceptance when provenance, ownership, and links are sufficient, without explicitly retaining the human decision. Backport says to mark stale sources when implementation differs. Adequate links do not authorize acceptance; code differing from a source does not establish that the source is obsolete. |
| Existing coverage views are triage surfaces. | `views/uncovered-requirements.yaml` is an `ontology_type: Requirement` table with accepted/review presets and trace columns. It does not calculate satisfaction or provide the spec/story-centered thin-coverage view described in SPEC-0062 US6 AC2. `sources-needing-review.yaml` exposes authored review state; it does not detect source updates. These are original-delivery reconciliation questions, not silently added redesign work. |
| A candidate process spec is archived. | `docs/specs/process/development-loop.md` has `spec-status: archived` and a retirement notice directing current policy to `docs/engineering/`. Do not freeze it as the execution authority for this effort. |

The inspected install fixture `TestRun_TemplateComplexDomainScaffoldsDocs` in `pkg/app/cli/init/run_test.go` creates a scratch project, checks ontology/recipe/view installation and compilation, and asserts rendered skill content. `TestLoadAllSkillTemplatesAppliesComplexDomainOverlays`, `TestAgenticEngineeringComplexDomainOverlaysCloseOverRetainedSkills`, and `TestApplyAgentSurfaces_KeepsComplexDomainRenderedSkillsInSync` cover composition. These are useful installation fixtures; they do not demonstrate a source-change or delivery-backport task over populated requirements.

The one authored RequirementSource at the time, since removed from the public tree, recorded a transcript path, source date, review status, and extraction date. The live `coverage-gap-pack` and deterministic Requirement inventory returned zero rows. No populated requirement/process/workflow fixture was found in the inspected starter tests or repository requirement folders. This limits behavioral evidence; it does not erase domain constraints.

## Live API evidence and limits

A usable worktree binary was absent. `NO_WEB=1 make build` successfully built this checkout with `-mod=vendor -tags "fts5"`; `./scripts/rzm --version` returned `v0.50.5`. One session was started with `agent start --intent ... --file <owned-effort>` and reused as `MYyrZeDEdkK1qSVn`.

Startup reported `indexed-context-missing` for optional indexed enrichment. Live schema, authoring guide, ontology inspection, and the following structural queries nevertheless executed successfully. This is not evidence that semantic search or code indexing was ready, and no semantic retrieval or model evaluation was run.

- `agent query-recipe run --id effort-execution-context` anchored on the owned effort resolved it. That effort has since been removed from the public tree, so this invocation is historical and cannot be rerun as written.
- `agent query-recipe run --id spec-domain-context-pack --anchor docs/specs/product/complex-domain-starter.md` resolved SPEC-0062 with empty linked requirements, feature areas, processes, and workflows. This confirms the supplied coverage gap.
- `agent query-recipe run --id coverage-gap-pack` returned an empty Requirement row list.
- `agent ontology-authoring-guide --type EffortNote,ReferenceDoc,Requirement,RequirementSource` and `agent ontology-inspect --input <owned-effort>` confirmed note shapes and blank approval semantics.
- `agent surface`, `agent ontology-query-schema`, `agent query-recipe run --help`, `validate --help`, `validate list`, and `init --help` supplied the current command contracts. There is no recipe `show` subcommand; use `list` or read the recipe source. No frozen-spec packs were required because this effort has no frozen selection; inspect those packs when the coordinator freezes approved criteria.

A temporary proposal recipe using `notes(type: $type, find: $find, first: $first)` also executed with a required string type, omitted optional find, and integer first input. It returned the authored RequirementSource with page metadata and no warnings. The temporary file was outside tracked sources; this was a read-only API check, not implementation or a model evaluation.

This query executed successfully, returning no rows, `truncated: false`, `maxFirst: 5000`, and no warnings:

```graphql
{
  notes(type: "Requirement", first: 20) {
    nodes { path title }
    pageInfo { requestedFirst returnedCount truncated maxFirst }
    warnings { code message }
  }
}
```

The live `notes` root accepts `type`, `find`, `property`, `semantic`, and `first`; it does not expose `after` or `offset`. `PropertyFilterInput` accepts one `name`, `value`, and optional `source`. Do not prescribe cursor paging or compound filter arguments. `Requirement` exposes `sourceLocations`, `sources`, `specs`, `storyRefs`, `acceptanceCriterionRefs`, `conflicts`, `duplicates`, and singular `supersededBy`; `RequirementSource` exposes `sourceVersion`, `provenance`, `reviewStatus`, `lastExtracted`, and inbound `requirements`. New recipes must compile and execute against this schema before their syntax is advertised.

## Workflow contract

### Shared ownership

Base Rhizome owns session reuse, capability/readiness interpretation, exact source reads, live authoring/schema discovery, safe writes, identifiers, canonical locators, and validation selection. AE owns the spec/effort/approved-plan model, engineering phase transitions, implementation evidence, and delivery reconciliation. Complex Domain owns domain distinctions, source provenance, candidate-versus-accepted meaning, structural traceability, and domain reconciliation.

A leaf domain skill loads only the reference it needs. Add `skills/complex-domain/references/retrieval-and-evidence.md` for domain-specific recipe selection and result interpretation; it must link to installed Rhizome mechanics instead of cloning them. Keep all eight existing entrypoint names. No fixed skill-count target, retired-name migration, or replacement engineering workflow is proposed.

| Starting situation | Domain work and concrete output |
| --- | --- |
| Known source or changed source | `requirements-curation` loads the source and impact pack, compares the cited prior/current evidence, and identifies affected requirements and delivery targets. `requirements-ingest` creates only genuinely new atomic candidates. Output names source identity/version/locations, conflicts, affected notes, and proposed changes. |
| New source without a known note | Resolve exact source identity/provenance using a bounded deterministic typed inventory, then use semantic candidate discovery only for remaining duplicate questions. Preserve the source once and link extraction results to it. Rerunning unchanged extraction reuses candidates; no duplicate source or requirements. |
| Domain concept, process, or workflow | `domain-modeling` or `workflow-mapping` loads known context and the relevant live authoring guide. Reuse existing durable concepts. Processes describe business/system operation; workflows describe actor-facing tasks and variants. Output only the models and links justified by the source. |
| Candidate decision | `requirements-curation` presents atomic wording, source evidence, conflict/duplicate links, and a recommended disposition. Apply an explicitly authorized acceptance/rejection/supersession; sufficient metadata alone never means human acceptance. Existing approved decisions carry forward without another approval request. |
| Spec or approved engineering effort | `spec-from-domain` supplies accepted constraints, candidate context, unresolved conflicts, and source/coverage qualifications to `agentic-engineering specify`. AE's selected scope and plan remain authoritative during implementation. Refresh affected context when evidence changes; do not rerun an identical pack at every handoff. |
| Delivery completed or mismatch found | AE alignment invokes `traceability-review`; AE reconciliation invokes `domain-backport` for evidenced domain updates. A requirement becomes `implemented` only when its full obligation has delivery evidence and that lifecycle update is authorized. Partial story coverage remains partial. A source/code disagreement is recorded before deciding which is wrong. |

Every changed-source result distinguishes observed source changes, potentially affected links, verified delivery implications, and decisions still needed. Keep prior version/date and exact cited evidence in the existing source note's history or a linked durable artifact; an updated URL alone is insufficient provenance. Do not overwrite the evidence that justified an accepted requirement. Unknown old content remains an explicit evidence gap.

Use the installed `action-items` workflow for unresolved accountable decisions; do not invent assignees or silently promote uncertainty. Do not create a new note solely to narrate a routine tool call. The effort stores execution-specific reasoning; sources, requirements, domain notes, and specs store their respective durable truth.

### Retrieval and completeness

1. Reuse a known anchor. Load `domain-context-pack`, `requirement-trace-pack`, `spec-domain-context-pack`, or `changed-domain-impact-pack` according to the question. A topic-only request begins with typed deterministic inventory, then optionally `domain-topic-survey` as semantic candidate ranking.
2. Separate unresolved anchor, resolved note with absent authored links, runtime failure/degraded retrieval, capped inventory, and stale source evidence. Only the resolved-note case supports a statement about missing authored coverage. An unresolved anchor does not prove the domain is unmodeled.
3. For capped `notes` results, narrow using a supported typed/property selector or an explicit linked source/process/spec. Filtering only the already-returned rows cannot prove a full inventory. If no adequate partition exists, report the cap and stop the completeness claim while doing independent supported work.
4. Generic `linked`/`backlinked` lists have no page envelope. Reaching their explicit limit is a possible truncation signal, not a count of the entire neighborhood. Confirm named targets with focused packs. Do not label an entire domain complete from one bounded relation traversal.
5. Keep authored coverage, readable targets, evidence of satisfaction, and acceptance separate. Resolve story/criterion strings to actual targets and inspect relevant acceptance evidence; a nonempty string or semantic match does not establish any of those on its own.
6. On stale or unavailable capabilities, follow base recovery guidance and preserve the evidence qualification in the handoff. An unrelated indexing failure does not block source reading or a concrete planning proposal that does not depend on it.

### Exact recipe delta

All changes stay in `query-recipes/complex-domain.yaml`; retain existing ids and `rowPath: notes.nodes` for the three row recipes.

- Add `domain-inventory-pack`: deterministic `notes(type: $type, find: $find, first: $first)` with required type, optional find string and first; return path/title plus `pageInfo` and `warnings`. Default first 50. Document live type discovery and bounded inventory use. The inspected binder (`pkg/ontology/queryrecipe/bind.go`, `scalarRecipeVariableValue`) coerces lists, integers and booleans, otherwise strings; it does not bind an object input. Use current direct typed/property queries for property partitions rather than changing the recipe engine.
- `domain-topic-survey`: correct its problem/entrypoint guidance to semantic discovery after structural context; select page metadata and warnings. Keep score as candidate ranking only.
- `domain-context-pack`: add `RequirementSource` extraction/source identity and typed requirement links. Use `requirement-trace-pack` for detailed requirement context instead of duplicating it.
- `requirement-trace-pack`: preserve provenance/conflicts and authored story/criterion refs; return usable spec identities via concrete SpecLike type fragments including path/title. Do not turn string refs into a new ontology link type.
- `spec-domain-context-pack`: retain outbound `linked` shape; add a separately named inbound Requirement candidate list. Confirm the candidate's `specs` relation before classifying it as delivery coverage. Preserve absence/cap qualifications.
- `changed-domain-impact-pack`: include requirements for DomainProcess, UserWorkflow, DomainContext, and DomainType, and include their linked specs and authored story/criterion refs where the live schema permits. Add source location and review/version context needed to follow up. Keep traversal bounded; expand only affected requirements with the existing trace pack.
- `feature-area-backlog-pack`, `coverage-gap-pack`, `sources-needing-review-pack`: expose `notes.pageInfo` and `notes.warnings`; add optional `first` (default 200) and `find` string inputs. Keep row identity and output contract stable. Describe them as inventory rows requiring evidence review, not automatically calculated satisfaction or automatic source freshness.

The verified binding choice is a deterministic inventory recipe with `type`, optional `find`, and `first`, plus existing anchored packs. Do not make runtime filtering, pagination, or a new code-mode API a dependency of this guidance improvement.

## Ownership and composition contract

| Owner | Authorized paths and changes |
| --- | --- |
| Domain worker | `pkg/app/cli/init/templates/starters/complex-domain/skills/{complex-domain,requirements-ingest,requirements-curation,domain-modeling,workflow-mapping,spec-from-domain,traceability-review,domain-backport}/SKILL.md`; new `skills/complex-domain/references/retrieval-and-evidence.md`; apply the workflow above. |
| Domain worker | Existing `skill-overlays/{specify,effort-new,plan,implement,debugging,compound,ingest-transcript}.yaml`; remove duplicate retrieval and handoff loops, preserve conditional domain applicability. Add `skill-overlays/alignment.yaml` and `skill-overlays/reconciliation.yaml` only after C supplies agreed slots. |
| Domain worker | Also owns `docs/specs/product/complex-domain-starter.md`, original-effort reconciliation notes, and `pkg/app/cli/init/complex_domain_recipe_behavior_test.go`. Within the canonical starter: `query-recipes/complex-domain.yaml`, `managed-docs/AGENTS.md`, `docs/reference/guides/complex-domain-workflow.md`, `docs/reference/requirements/{sources,requirements}/README.md`, `docs/reference/domain/{contexts,types,processes,workflows}/README.md`, all beneath the canonical starter root. Document source-change and partial-delivery examples beside the workflow. |
| AE worker with coordinator agreement | `pkg/app/cli/init/templates/starters/agentic-engineering/skills/agentic-engineering/references/{alignment,reconciliation}.md`: add proposed extension slots `alignment.additional-context` and `reconciliation.additional-targets`. Keep existing slot names in other references unless C explicitly migrates every bundled consumer. |
| Coordinator | Shared specs other than delegated SPEC-0062; `pkg/app/cli/init/{run_test,helper_templates_test,skill_overlay_test,agent_surfaces_test}.go`; `README.md`, `CHANGELOG.md`, installed docs, managed blocks and all generated skill/config mirrors; integration gates and child merge. |
| Evaluation worker A | Scratch fixture/scenario assets under the exact runner root A selects, and observed task evidence; domain worker supplies fixture content and success assertions below. No competing runner or fixture directory is claimed here. |
| Base worker B | Canonical Rhizome mechanics and composition contract. Domain code-mode consumption is optional and must not change the approved domain behavior or write authority. |

Existing overlay contracts to preserve: specification `context.after-discovery` and `context.constraint-extraction`; effort setup `scope.additional-context`; planning `integration-map.additional-dimensions` and `architecture-decisions.additional-checks`; implementation `inputs.additional-context` and `docs.additional-traceability`; compounding `routing.additional-compounding`; transcript ingestion `source.additional-preflight` and `synthesis.additional-tracks`. All use `append`, order 10 today. `debugging.yaml` and `implement.yaml` both append to implementation context: have one consolidated instruction in `implement.yaml` and remove the redundant debugging fragment once tests confirm investigation remains covered. Retiring an overlay file does not retire a skill entrypoint.

Proposed alignment overlay: when selected scope depends on domain requirements, load its existing context or refresh changed anchors; invoke `traceability-review` to classify missing authored links, invalid locators, uncovered obligations, stale evidence, and conflicts against actual delivery evidence. Feed findings into AE's normal alignment result.

Proposed reconciliation overlay: for evidenced changes to requirements or domain knowledge, invoke `domain-backport` with the alignment finding, approved scope/deviation, affected targets, and verification evidence. Preserve source history and remaining gaps; keep normative spec decisions and effort completion with AE. Do not infer source obsolescence or requirement acceptance from code alone.

## Specification amendments

The Domain supervisor owns the SPEC-0062 amendments below; they have been applied and their real targets frozen in the effort. AE owns the SPEC-0063 amendments. Existing block targets were verified live before freezing. The original-delivery reconciliation is additive and does not falsely close its missing scope.

| Existing durable target | Proposed added acceptance text |
| --- | --- |
| [SPEC-0062 US3 AC1](../../specs/product/complex-domain-starter.md#^SPEC-0062-US3-AC1) | A source refresh preserves the prior cited evidence/version and identifies the new evidence used for extraction. An unchanged extraction rerun reuses the source and matching candidates instead of duplicating them. Unknown prior evidence is explicitly qualified. |
| [SPEC-0062 US3 AC2](../../specs/product/complex-domain-starter.md#^SPEC-0062-US3-AC2) | Candidate acceptance records an authorized human decision; provenance quality, confidence, and structural links alone do not constitute acceptance. Existing authorization carries across the workflow without routine reapproval. |
| [SPEC-0062 US3 AC3](../../specs/product/complex-domain-starter.md#^SPEC-0062-US3-AC3) | Source changes surface conflicting candidates and affected accepted requirements without rewriting accepted scope or suppressing earlier evidence. |
| [SPEC-0062 US4 AC1](../../specs/product/complex-domain-starter.md#^SPEC-0062-US4-AC1) | A known anchor uses structural context first; topic discovery begins with bounded typed inventory. Semantic recipes are explicitly candidate discovery. Required inputs are limited to the current question and reused when still valid. |
| [SPEC-0062 US4 AC3](../../specs/product/complex-domain-starter.md#^SPEC-0062-US4-AC3) | Results distinguish unresolved anchor, missing authored coverage, unavailable/degraded retrieval, stale source evidence, and capped traversal. Agents do not claim complete coverage from a partial inventory. |
| [SPEC-0062 US5 AC1](../../specs/product/complex-domain-starter.md#^SPEC-0062-US5-AC1) | Context may be reused across engineering phases when targets, source revisions, and scope remain applicable; refresh only affected evidence when those inputs change. |
| [SPEC-0062 US5 AC3](../../specs/product/complex-domain-starter.md#^SPEC-0062-US5-AC3) | AE alignment explicitly routes domain findings to traceability review; reconciliation passes supported updates to domain-backport. Authored links, resolved targets, obligation satisfaction, and authorized lifecycle changes are distinct. Partial delivery does not mark an entire requirement implemented. Source/code disagreement requires evidence before declaring the source stale. |
| [SPEC-0062 US7 AC2](../../specs/product/complex-domain-starter.md#^SPEC-0062-US7-AC2) | Domain skills consume base session, authoring, mutation, and validation mechanics and AE delivery authority; they prescribe only domain-specific retrieval and outputs. Read-only review does not require a blanket mutation-validation suite. |
| [SPEC-0062 US7 AC3](../../specs/product/complex-domain-starter.md#^SPEC-0062-US7-AC3) | Source-impact retrieval returns bounded paths to affected requirements and delivery targets, with source locations and review/version qualifications; it does not claim automatic freshness detection. |
| [SPEC-0063 US1 AC3](../../specs/technical/skill-template-overlays.md#^SPEC-0063-US1-AC3) | Document the reviewed AE alignment and reconciliation extension points as part of the bundled slot contract; any rename migrates all bundled overlays together. |
| [SPEC-0063 US3 AC1](../../specs/technical/skill-template-overlays.md#^SPEC-0063-US3-AC1) | Composed AE alignment and reconciliation include the domain handoffs, while AE alone remains useful without domain prose or overlay markers. |

Also update SPEC-0062 `Requirements / Saved Query Recipes` with the deterministic inventory and result qualification contract, and `Skills And Workflows` with retained entrypoints and shared mechanics ownership. Update SPEC-0063 `Complex-Domain Overlay Contract` to name alignment/reconciliation reference targets and correct its stale compounding-router wording to `references/compounding.md`. No schema/view acceptance amendment is proposed. Preserve US2 process/workflow distinctions as regression constraints.

Do not select archived SPEC-0001 for freezing. Use current engineering policy and the coordinator's current AE specification decision. Existing SPEC-0062 US1 and SPEC-0063 rendering criteria remain regression constraints, not an excuse to absorb the full old delivery scope.

## Verification design

For approved development, preserve the research handoff allocation: bounded fixture/mechanical work may use Luna `max`; moderate implementation Sol `medium`; subtle authority/traceability work Astra `low`, with the supervisor inspecting diffs and evidence. Use a fresh independent review for contract-sensitive implementation. This is separate from the evaluated model and does not authorize evaluation runs during planning.

No model evaluations run in this planning pass. A owns the approved baseline/revised comparison using only `gpt-5.6-luna` with `xhigh` reasoning. Neither Astra nor Sol is an evaluation option. Evaluate installed surfaces and actual edits/tool evidence; routing self-reports are supplementary.

| Fixture/scenario | Meaningful observable assertion |
| --- | --- |
| Source v1/v2 conflict | A dated source v1 supports an accepted atomic requirement linked to a process, workflow, and one spec criterion. V2 changes the rule while a second source disagrees. The agent preserves v1 provenance, captures v2 evidence, identifies the affected requirement/spec/criterion and conflicting candidate, and proposes a disposition without changing accepted scope. |
| Idempotent ingestion | Run the same source extraction twice. The second run creates no duplicate source or candidate and retains exact source locations. Distinct obligations remain separate requirements. |
| Incoming-only trace | Requirement authors a `specs` link; spec has no reciprocal body link. Context returns the incoming candidate and confirms its typed relation. An incidental backlink is not counted as satisfaction. |
| Partial delivery/backport | One atomic requirement needs two acceptance criteria to demonstrate its obligation, with only one delivered. The agent reports partial evidence, repairs only authorized links/docs, and leaves the requirement unimplemented. A later full-evidence case can perform the authorized lifecycle update. |
| Source/code disagreement | Delivered code contradicts the cited rule. Agent records a mismatch and recommendation; it does not relabel the source obsolete merely because code differs. |
| Empty/degraded/capped | Separate resolved-but-unlinked spec, unresolved path, unavailable semantic retrieval, and inventory over the selected cap. Each produces the correct qualification. The over-cap case contains a relevant later requirement so local filtering cannot falsely pass completeness. |
| Domain model distinction | A business process exception and an actor task variant use distinct typed homes and link to the requirements they constrain. No replacement BusinessRule/DomainQuestion/CoverageReview schema is introduced. |
| Fresh-agent handoff | A new agent can recover source versions/locations, selected constraints, remaining decisions, delivery evidence, and partial coverage from durable notes without the prior chat. |

Coordinator's deterministic tests should extend the existing temp-install fixture pattern with populated schema-valid source, requirement, domain, and spec notes. Test executed recipe results, caps/warnings, incoming-only links, missing targets, and no semantic-coverage substitution. Compile recipes against the installed merged schema; preserve row contracts. String assertions alone are insufficient evidence for the workflow.

Required implementation checks: focused init tests with `go test -mod=vendor -tags fts5 ./pkg/app/cli/init`; recipe/query tests per [GraphQL subsystem guidance](../subsystems/graphql-query.md); `make check` before merge/closure and before committing Go/TypeScript changes per policy. Generated template changes require `make build`, real temp installs with `--template agentic-engineering`, `--template complex-domain`, and `--template agentic-engineering,complex-domain`, equality of effective rendered skills and agent mirrors (selected-template configuration remains distinct), then a second `init --yes` with no updates. Enable the intended agent surfaces and authorized skills refresh policy in disposable projects; `--yes` alone does not authorize overwriting existing managed assets. The coordinator regenerates repository mirrors centrally.

Changed Markdown uses `./scripts/rzm validate`; changed efforts/specs also use `./scripts/rzm validate frozen-scope-drift`; `git diff --check` is required. The live `--scope-note` flag scopes fragile-external checks only, so do not advertise it as a general changed-file validation filter. Classify existing failures and directly assess both changed notes rather than silently broadening edits. PR CI content validation is not a substitute for runtime/full gates during implementation.

## Phases and exits

1. **Resolve prerequisites and shared contract — complete.** Coordinator accepted B's mechanics boundary and C's two closure slots; Domain owns SPEC-0062 amendments and frozen verified targets. Validated Drew's Person identity before recording approval. Original delivery is reconciled with the missing coverage view deferred to coordinator ownership and status preserved. Fresh implementation review remains required; no routine approval pause remains.
2. **Establish deterministic fixtures.** Domain adds failing populated recipe behavior fixtures. A owns the scenario corpus and four-launch Luna pilot; domain model scenarios are explicitly outside that pilot. Exit: current failures and corrected outcomes are reproducible, fixtures are schema-valid, and evidence labels distinguish installation/recipe checks from model task outcomes.
3. **Implement domain guidance and recipe changes.** Domain worker edits only its agreed canonical sources; C/coordinator supply the two closure slots and shared tests. Keep all existing entrypoint names and recipe row identities. Exit: targeted tests and recipe compilation/execution pass; examples use current APIs; no unapproved schema, view, runtime, or write-contract expansion.
4. **Compose and evaluate.** Coordinator builds/regenerates and proves idempotence; A validates domain fixtures deterministically; its authorized model launches cover bug/typo scenarios only. Exit: install and recipe behavior evidence passes, source guidance is independently reviewed, and domain model/fresh-agent task results remain explicitly deferred rather than claimed. A failed scenario is investigated and rerun only after a relevant change; a model result is not generalized to other models.
5. **Reconcile and hand off.** Domain worker updates owned workflow docs and effort evidence; coordinator integrates shared specs/changelog, performs review at the actual combined head, runs applicable gates, and merges only under staging authority. Exit: evidence names revision and remaining limitations, frozen scope aligns with actual delivery, and integration status is accurate. Main merge/release remains a separate decision.

## Coordination dispositions

| Decision | Recommendation and reason |
| --- | --- |
| Original delivery truth and closure | Additive reconciliation is recorded; missing spec/story coverage view and remaining formal closure are coordinator-owned follow-up. Original status/history remain intact and redesign proceeds. |
| B/C/D composition boundary | Accepted: base owns mechanics and AE supplies the two closure slots; retain the eight domain entrypoints. This repairs the concrete gap with the smallest topology change. If C changes phase files, migrate domain fragments in the same integration change. |
| Spec amendments and plan approval | Drew authorized delivery; Domain applied/froze SPEC-0062 clauses and AE owns SPEC-0063. Archived development-loop is excluded. |
| Coverage view shortfall discovered during reconciliation | Treat it as a coordinator disposition against original SPEC-0062 US6, with explicit follow-up or original-scope repair. Keep view/runtime redesign outside this effort unless Drew deliberately expands its scope. |

Routine defaults needing no separate user decision: use existing lifecycle/provenance fields and authored string refs; retain row contracts; use bounded CLI/GraphQL rather than requiring code mode; keep package A as the sole evaluation runner owner. The inspected binder supports the proposed string/integer inputs; no object-input runtime change is included.

## Planning verification result

On 2026-09-07, `./scripts/rzm validate` passed ontology, identifiers, and broken links with zero issues/errors; `./scripts/rzm validate frozen-scope-drift` passed with zero issues/errors. Direct `agent ontology-inspect` resolved the changed effort as EffortNote and this analysis as ReferenceDoc, each with no assessment issues. `git diff --check` passed. Baseline validation before editing was also clean. No full implementation gates or model evaluations are claimed.

## Exclusions

No Project-KB work, broad source-change monitoring, automatic requirement acceptance, new provenance/versioning service, ontology family migration, automatic stale-source detection, computed satisfaction engine, view/UI redesign, code-mode implementation, new semantic write operation, historical-effort rewrite, shared generated edits in this child, unapproved model evaluations, or main/release merge.

## Delivery verification — 2026-09-07

The planned canonical changes and populated recipe fixture are implemented. Independent review found a filtered-empty response contract that could imply global absence; guidance now says no rows matched the selector. Tests cover filtered and unfiltered inventories, caps, incoming typed spec links versus incidental backlinks, real criterion locators, unresolved versus unlinked anchors, and source provenance/conflicts. Six domain hubs now carry required ReferenceDoc metadata and are checked in the fixture. These are deterministic retrieval and installation results, not model workflow evaluations.

Full `make check CHECK_JOBS=3 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4` passed after the final implementation correction, as did full build, focused recipe/init checks, eight skill validators, repository validation, frozen-scope drift, and diff checks. Actual disposable installs of AE, Complex Domain, and explicit combined templates confirmed equal rendered domain skills, equal agent mirrors, no leaked slot markers, and unchanged skill hashes on second init.

Fresh-install recipe/view/overlay/broken-link validation passed. Ontology validation still reports four AE-owned documentation errors: the efforts hub links to missing `../engineering/efforts.md`, and the reference, analysis, and guides hubs omit `referenceKind`. Those are assigned to AE and the coordinator. The local gated tree also includes AE's two closure hooks and shared manifest expectations; AE publishes those separately. Repository mirrors and staging integration remain coordinator-owned. Domain model and fresh-agent scenarios remain deferred beyond the authorized bug/typo pilot.
