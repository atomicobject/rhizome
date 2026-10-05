---
type: ReferenceDoc
summary: "Agentic Engineering phase contracts, recipe repairs, overlay boundaries, and implementation evidence for the agent experience redesign."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Agentic Engineering experience implementation plan

## Decision and authority

Retain the `agentic-engineering` phase router, `foundation-review`, and `ingest-transcript`. Improve the existing workflow through explicit local-task exits, authorization continuity, complete resume/closure context, proportional retrieval, and conditional durable updates. No new workflow phase, lifecycle field, orchestration runtime, or skill retirement is needed.

This is package C's delivery contract for the owned effort, under the [preparation brief](agent-experience-preparation.md). Drew explicitly directed the tasks to drive through delivery, superseding the initial planning-only dispatch. The coordinator assigned C the bounded implementation and SPEC-0038/SPEC-0063 amendments. The effort records validated identity, approval, and real frozen targets. Main/release and staging merges remain excluded.

Source baseline: `3b93489c4e404981fc4bb193e75c0d70e9eac7a7`, verified as this worktree's initial HEAD and an ancestor. Staging PR #243 was open, draft, and at that revision during inspection. Findings below distinguish source inspection and live tool evidence from proposed agent behavior. No model evaluation ran.

## Current findings and three task traces

Canonical starter sources are under `pkg/app/cli/init/templates/starters/agentic-engineering/`. Paths below that prefix are named explicitly in the ownership section.

| Trace | Current source path and behavior | Gap and proposed result |
| --- | --- | --- |
| Clear local bug fix | `references/workflow-state.md` permits a local pass without an effort. `references/implementation.md` accepts either a local task or approved slice, but ends with “continue to quality gates and closure.” Alignment, reconciliation, and closure then require effort-anchored packs. | Add a local exit after focused checks and a durable-knowledge assessment. Do not require an effort, spec delta, closure pack, or new doc unless the actual change warrants one under local policy. Reproduce bugs and keep meaningful regression evidence. |
| Approved multi-phase feature | The router and managed block already say approved plans authorize routine decisions. Planning protects approved plans from unconfirmed revision; implementation checks off items. Alignment, reconciliation, compounding, and closure each prescribe `closure-drift-pack`; the query-bundle guide additionally mandates semantic discovery and file context before spec-linked implementation. | Preserve the authority rule. Distinguish routine execution notes from material plan changes, reuse current scope/evidence across phase transitions, and refresh affected context after edits. Close the effort only when its actual gates are met. |
| Fresh-agent resume | `effort-execution-context` selects plan, execution notes, and delivery sections, but omits `planApprovedBy` and `closureChecklist`. `closure-drift-pack` omits those fields plus plan and execution notes, despite promising checklist/closure state. The workflow-state reference has no concrete resume procedure. | Retrieve approval and checklist explicitly; verify current checkout, unfinished items, and relevant evidence before continuing. A changed session alone is no reason to re-plan or seek approval again. Missing or conflicting authority blocks only dependent implementation. |

These are instruction and data-contract gaps, not measured task failures. The [September 4 routing evaluation](agentic-engineering-routing-evals-2026-09-04.md) measured self-reports and noted excessive concern-document reads; it did not execute tasks. Its historical model choices are not the evaluation configuration for this effort.

Additional inspected evidence:

- [SPEC-0001](../../specs/process/development-loop.md) is archived and explicitly retired. It still says interruption requires explicit resume confirmation. It cannot govern this redesign over current [review and approval policy](../../engineering/review-and-approval.md).
- [SPEC-0038 US2](../../specs/product/init-starter-workflow.md#^SPEC-0038-US2) already owns the coherent starter bundle, thin phase router, separate workflows, and team-owned concern docs. Extend that active contract rather than restoring retired process guidance.
- `docs/reference/guides/harness-capabilities.md` in the starter still mandates degradation banners for absent subagents, while the current closure reference permits direct closure without degradation ceremony. Its runtime-capability assertions can also become stale.
- The starter's `docs/reference/guides/spec-driven-query-bundle.md` mandates a semantic/file-context itinerary even for known paths. Its recipe summaries promise fields absent from the queries. Its missing-recipe advice prescribes init without expressing current refresh authority.
- The ingestion guide starts with full query schema and ontology reference, then a broad survey. This is stronger than the skill's need for likely destination types and duplicates base discovery mechanics. Source preservation and candidate/accepted distinctions remain necessary.
- [CLI subsystem guidance](../subsystems/cli.md), [init module context](../../../pkg/app/cli/init/CONTEXT.md), and `skill_overlay.go` establish embedded source ownership, file-relative overlay targets, fail-before-write validation, and refresh authority. `--yes` alone does not authorize replacement of existing starter assets.
- [GraphQL subsystem guidance](../subsystems/graphql-query.md) requires bounded roots, live SDL, validated deterministic recipes, and explicit unavailable results. The proposed recipe changes preserve those invariants.

## Final behavior contract

Read each phase's reference only when needed. The user request outranks team policy; `docs/engineering/` outranks starter defaults. Reuse an already loaded concern document while it remains current. The phase names are entrypoints into one task, not mandatory stops or a requirement to run every phase.

| Phase | Inputs and decision | Deliverable and verification | Continue or seek judgment |
| --- | --- | --- | --- |
| classify / resume | User intent, current work, applicable risk policy; known effort if present. On resume inspect current approval, frozen scope, plan, completed items, deviations, and checkout. | Select local work, specification/planning, remaining approved implementation, or remaining closure. A closed effort is history. | Resume approved unfinished work directly when context agrees. Ask only about conflicting scope/authority or an unknown intended outcome; do independent authorized work meanwhile. |
| specify | Existing authoritative behavior and selected prior art; source-backed material goes through ingestion when needed. | Observable commitments, non-goals, uncertainty, and valid targets. Check affected frozen efforts before changing a held contract. | Settle routine details. Obtain material behavior/scope decisions before committing them; proceed to already-authorized planning. |
| effort / plan | Candidate slice, selected durable criteria, bounded effort context, policy relevant to architecture/testing/docs. | Exact ownership, phases/exits, tests, dependencies, exclusions, and remaining decisions. Keep unapproved scope labeled and approval blank. | Approval covers all named implementation steps. Add a foundation-review exit only for a formative decision on which later work depends. |
| implement | Local scope or approved effort; relevant file constraints and exact source proof through base capabilities. | Fix/feature, focused evidence, completed plan items, deviations when material, affected durable truth updated where useful. | A local task finishes after its checks and report. An effort continues through required gates and closure. Routine refactoring/test fixes/review feedback within approved behavior do not restart planning. |
| gates / align | Changed revision, current test results, selected criteria for effort work, local gate policy. | Exact outcomes, missing criteria, stale claims, conflicts, and evidence gaps. Repeat checks only when changes or missing coverage invalidate evidence. | Fix authorized failures. Ask about product/acceptance tradeoffs or scope expansion, not permission to run required local checks. |
| reconcile / compound | Verified findings and changed durable knowledge. Reuse current closure context; refresh sections changed since retrieval. | Smallest affected spec/reference/code rationale/policy update; effort-only rationale stays in execution notes. Repeated friction may justify a bounded follow-up. | No durable change means no new documentation artifact. New product/policy commitments require authority. Domain reconciliation consumes the agreed extension hook. |
| finish / handoff | Required checks, alignment/reconciliation results, actual delivery, deviations, checklist, remaining work. | Truthful effort state plus revision-bound evidence and a usable next step. Interrupted work records the next unfinished item and blocker in the existing effort. | Mark complete only under repository policy. Handoff alone does not complete an effort or authorize merge/release. Local tasks need only a concise result/evidence report. |

Review feedback is handled inside implementation/alignment: reproduce and resolve in-scope defects, retain valid current checks, rerun affected verification, and route changed intended behavior back to specification. Do not add a `review` phase.

Retain foundation review as the distinct human decision workflow. Replace the implied target of three to six unresolved seams with “only unresolved formative decisions”; already-settled decisions are evidence, not questions to ask again. Retain ingestion as the distinct provenance workflow; explicit save/update requests authorize in-scope writes, while uncertain meaning remains candidate context. Neither requires skill consolidation.

## Composition contract with packages B and D

Base Rhizome owns session lifecycle, capability discovery, file/semantic/typed retrieval mechanics, availability interpretation, ontology authoring, durable locators, mutations, and validation mechanics. AE owns when those capabilities protect an engineering decision and what an effort/spec must record. Complex Domain owns source-backed meaning, accepted versus candidate requirements, domain coverage, and domain reconciliation.

C consumes B's reviewed rules for these operations instead of copying command catalogs or imposing a second startup sequence. A substantive local task should retrieve relevant existing constraints; a typo does not automatically need a session. Prefer one bounded context read for the decision at hand, then deepen only for missing or conflicting evidence. Do not retry the same degraded operation in a loop or interpret an unavailable lane as empty. Explicit Rhizome operations still require working integration; independent source inspection may continue with the limitation stated.

Preserve every existing overlay target and extension slot:

| Target skill / relative file | Slot ids | Current D overlay filenames |
| --- | --- | --- |
| `agentic-engineering/references/specification.md` | `context.after-discovery`, `context.constraint-extraction` | `specify.yaml` |
| `agentic-engineering/references/effort-setup.md` | `scope.additional-context` | `effort-new.yaml` |
| `agentic-engineering/references/planning.md` | `integration-map.additional-dimensions`, `architecture-decisions.additional-checks` | `plan.yaml` |
| `agentic-engineering/references/implementation.md` | `inputs.additional-context`, `docs.additional-traceability` | `implement.yaml`, `debugging.yaml` |
| `agentic-engineering/references/compounding.md` | `routing.additional-compounding` | `compound.yaml` |
| `ingest-transcript/SKILL.md` | `source.additional-preflight`, `synthesis.additional-tracks` | `ingest-transcript.yaml` |

Two additive `extension` slots implement the coordinator/Domain agreement:

- `alignment.additional-context` in `agentic-engineering/references/alignment.md`, after findings classification, allows affected domain evidence to join the audit.
- `reconciliation.additional-targets` in `agentic-engineering/references/reconciliation.md`, after selection of the smallest durable home and before authority boundaries, allows affected domain targets to reach `domain-backport`.

D owns `pkg/app/cli/init/templates/starters/complex-domain/skill-overlays/alignment.yaml` and `reconciliation.yaml`. Its incoming coordination message confirmed these names, target files, and the full preserved slot inventory. Empty hooks strip cleanly for AE alone. Consumers add affected evidence/targets without replaying universal discovery, repeating approval, or making every AE task domain work. Existing `implement.yaml` and `debugging.yaml` share a slot; D owns their current-context reuse. No public API rename or legacy cleanup is included.

## Exact recipe repair

Owner: `pkg/app/cli/init/templates/starters/agentic-engineering/query-recipes/spec-driven.yaml`. Preserve recipe ids, required `path` anchor, bound variables, and unrelated recipes.

| Recipe | Add selections on `effortNote` | Purpose |
| --- | --- | --- |
| `effort-execution-context` | `planApprovedBy`, `closureChecklist { content }` | Distinguish missing approval from omitted data and expose incomplete closure work on resume. |
| `closure-drift-pack` | `planApprovedBy`, `plan { content }`, `executionNotes { content }`, `closureChecklist { content }` | Include the approved work and recorded decisions/evidence that qualify delivery truth. |

Update each recipe's expected paths and empty/partial guidance to match these fields. Require `effortNote.closureChecklist.content` as a selected output path, but do not classify nullable `planApprovedBy` as a schema error: blank approval is meaningful pending authority. Approval text is a record to reconcile with actual authorization, not permission that overrides the current user request. Do not add removed `auditStatus`, `backportStatus`, or `compoundStatus` fields.

The live executable SDL exposes every selected field. A bounded direct query against this effort returned its blank approval and unchecked closure checklist. No ontology change is required. Keep the two existing recipe purposes; collapsing them or introducing persistent workflow state adds unnecessary migration cost. Reuse equivalent current results across phases, refreshing after changes to their source notes, scope, or execution evidence.

## Governing amendments

The assigned amendments are authored in the active specs. `node-link --ensure plan` verified all 13 selected criterion targets as linkable with existing block ids before the effort froze them; the effort records exact snapshot hashes. The text below records the rationale and criterion mapping.

1. In [SPEC-0038](../../specs/product/init-starter-workflow.md), retain US2 AC1–AC3. Add under US2:
   - AC4, **Proportional execution**: “A clear local task proceeds through implementation and the repository's applicable checks without requiring a new spec, effort, effort-closure query, or documentation artifact. It escalates when the actual work meets the repository's effort-driven risk criteria.”
   - AC5, **Authorization continuity**: “An approved effort plan authorizes all covered steps, including routine implementation choices and in-scope review fixes. A phase transition or fresh session does not require renewed approval; missing authority, conflicting scope, and material decisions outside that authority hold only dependent work.”
   - AC6, **Complete resume context**: “Effort execution and closure retrieval expose approval, frozen scope, plan, recorded execution evidence, actual delivery, deviations, and the closure checklist. A fresh agent verifies unfinished work and current revision before resuming; it preserves completed or archived effort history.”
   - AC7, **Proportional context and evidence**: “Phase guidance consumes shared Rhizome mechanics and relevant local policy, reuses current results, and refreshes evidence when affected inputs change. Missing, partial, or stale capability results remain explicit evidence gaps.”
   - AC8, **Durable maintenance and handoff**: “Delivery updates the smallest durable home of changed reusable knowledge and records effort-local rationale in the effort. With no durable knowledge change, no additional document is required. Handoff records current revision, verified outcomes, remaining work, and unresolved decisions without implying completion or merge authority.”
   - AC9, **Distinct judgment and provenance workflows**: “Foundation review presents only unresolved formative decisions requested by the plan. Ingestion preserves provenance and uncertain meaning while honoring authorized writes. Ordinary review feedback stays within implementation or alignment.”
2. In [SPEC-0063](../../specs/technical/skill-template-overlays.md), retain US1 AC3, US2 AC1–AC3, US3 AC1–AC3, and US5 AC2–AC3 as composition constraints. Amend “Complex-Domain Overlay Contract” to refer to the compounding **reference**, not the router body, and add: “Alignment and reconciliation expose the agreed extension slots for affected domain evidence and targets; domain-backport owns domain reconciliation and consumes current findings without duplicating base mechanics or changing accepted scope.” Amend “Overlay File Format” to describe the implemented optional skill-relative Markdown `file` field (default `SKILL.md`), and US2 AC1 to include that file target. This records an existing API plus the additive hooks, not a renderer rewrite.
3. Keep SPEC-0001 archived and exclude it from this effort's governing set. Current team policy remains authoritative. No new process spec is recommended. B owns any SPEC-0080 amendments; D/coordinator own SPEC-0062.

Shared-spec inbound active/frozen efforts were inspected. The coordinator owns required deviations or refreeze on its integration record; C does not rewrite other efforts. Existing SPEC-0038 US2 is already referenced by other efforts; this is not permission to rewrite their scope. The old Complex Domain delivery still has `status: active`, pending audit/backport/compound fields, and an unchecked closure item. Its status names alignment, backport, and compounding as remaining work despite delivered phases. The coordinator owns reconciliation/closure of that original scope before Domain delivery changes SPEC-0062 or its starter assets. The latest dispatch explicitly allows C to implement base-compatible AE hooks and the assigned SPEC-0063 amendments now. C preserves old slot semantics and reports historical reconciliation without modifying the old record.

## Implementation ownership and exclusions

The initial planning commit changed only this note and the effort. C now owns these authorized production files:

- `pkg/app/cli/init/templates/starters/agentic-engineering/skills/agentic-engineering/SKILL.md`
- In that skill's `references/`: `workflow-state.md`, `implementation.md`, `planning.md`, `effort-setup.md`, `specification.md`, `quality-gates.md`, `alignment.md`, `reconciliation.md`, `compounding.md`, `closure.md`, and `closure-report-contract.md`. Keep `spec-template.md`, `strawman.md`, and `traceability.md` unless review identifies an actual conflict; no speculative rewrite.
- `pkg/app/cli/init/templates/starters/agentic-engineering/skills/foundation-review/SKILL.md`
- `pkg/app/cli/init/templates/starters/agentic-engineering/skills/ingest-transcript/SKILL.md`
- `pkg/app/cli/init/templates/starters/agentic-engineering/managed-docs/AGENTS.md`
- `pkg/app/cli/init/templates/starters/agentic-engineering/query-recipes/spec-driven.yaml` for the two named recipes only.
- `pkg/app/cli/init/templates/starters/agentic-engineering/docs/reference/guides/harness-capabilities.md`, `spec-driven-query-bundle.md`, and `ontology-driven-transcript-ingestion.md` to reconcile stale procedures and recipe promises.
- `pkg/app/cli/init/templates/starters/agentic-engineering/docs/efforts/README.md` for a concise resume/handoff contract using existing effort sections and a valid current-policy link.
- `pkg/app/cli/init/templates/starters/agentic-engineering/docs/reference/README.md`, `docs/reference/guides/README.md`, and `docs/reference/analysis/README.md` under the same starter root: required ReferenceDoc metadata, repairing four reproduced fresh-install findings together with the effort link.

C owns `docs/specs/product/init-starter-workflow.md`, `docs/specs/technical/skill-template-overlays.md`, and the new `pkg/app/cli/init/agentic_engineering_experience_test.go` for recipe-result and hook-rendering assertions. A two-entry expected-slot addition in `skill_overlay_test.go` is required by the additive hooks. B owns the reference-inventory hunk in `helper_templates_test.go`; other shared test changes are limited to contradictory existing assertions if needed. Coordinator owns init topology/migration/runtime code, root README/CHANGELOG and hubs, and generated repository surfaces. No query-runtime changes are included.

D owns its overlays and domain docs. B owns the base skill and shared mechanics. A owns scratch fixtures, runner, scores, and comparison artifacts. E owns progressive code mode; this plan does not depend on E shipping. Team-edited `docs/engineering/` are preserved; no policy rewrite or forced refresh is part of C's change.

Exclude Project-KB, engagement tooling, new lifecycle/schema fields, automatic policy changes, generic knowledge-maintenance infrastructure, model runner implementation, code-mode runtime, fixed skill-count goals, historical effort repair, model evaluation runs in this child, merging, and release.

## Delivery phases and exits

| Phase | Work | Exit evidence |
| --- | --- | --- |
| 1 — Contract agreement | Coordinator dispatch settles B/C/D hook boundaries and C ownership. Freeze approved, resolved criteria; report coordinator-owned historical drift separately. | The delivery dispatch and incoming Domain/Base confirmations settle the formative interfaces. Approval and verified targets are recorded once in the effort; implementation proceeds. |
| 2 — AE behavior and data | C changes named sources and two recipe selections. Keep current skill names/slots; add only the agreed reconciliation slot. Reconcile starter guides with phase contracts and base ownership. | Focused tests compile and execute recipes, source references resolve, AE alone renders cleanly, and source review finds no local-task closure trap or mandatory no-change docs. |
| 3 — Composed installation | D supplies the agreed domain fragments after its prerequisite. Coordinator updates shared tests and regenerates installed surfaces with authorized refresh policy. | Fresh AE-only, domain-only, and explicit combined installs; equivalent combined skill bodies; no leftover markers; retained team docs/rejections; second authorized regeneration reports no updates. |
| 4 — Behavioral evidence | After A's protocol and bounded pilot approval, A runs current versus revised installed guidance using only `gpt-5.6-luna` with `xhigh`. C resolves in-scope findings and supplies source revisions. | Observable results for scenarios below. No unsupported correctness/completion claims, unnecessary scope/approval stops, or fabricated documentation. Costs/redundancy are compared to baseline after correctness. |
| 5 — Integration and handoff | Required gates, current-head review, smallest durable reconciliation, revision-bound evidence; coordinator integrates reviewed child changes into staging. | Delivered criteria accounted for, remaining limitations owned, effort closure justified by actual gates. Main merge remains a separate decision. |

No additional foundation pause is prescribed after Phase 1 unless a new material decision appears. Shared contract agreement can be part of the combined plan review; do not ask again merely because implementation has reached its first phase.

## Meaningful verification

Repository commands come from [quality gates](../../engineering/quality-gates.md) and [testing policy](../../engineering/testing-policy.md). After production changes: `go test -mod=vendor -tags fts5 ./pkg/app/cli/init`, `make build`, launcher-based documentation/recipe/overlay validation, authorized `./scripts/rzm init --yes` regeneration and a second clean run. The coordinator runs `make check` for shared Go test changes before commit, and before merge/effort closure. `--yes` is prompt handling; existing-asset refresh needs the existing saved policy or explicitly reviewed update flow. Apply this in disposable fixtures and central regeneration, never by quietly changing team policy.

Tests should prove observable contracts, not exact paragraphs:

- Execute each repaired recipe against a disposable effort with blank and populated approval, an unfinished checklist, and distinctive plan/execution evidence. Assert those results and lifecycle meaning; compile against the effective installed schema. Preserve valid pending approval.
- Reuse `TestAgenticEngineeringSkillTopology`, `TestSkillTemplateReferencesAndRecipesAreReachable`, `TestBundledStarterQueryRecipesValidateAgainstEffectiveSchema`, and composed rendering tests. Coordinator adjusts prose-coupled assertions only when needed; do not add a test per sentence.
- Render all three starter combinations and ensure the additive slot appears only as resolved prose, once, in the reconciliation reference. Invalid target files/slots must still fail before writes.
- Existing update-authority tests and real reruns must preserve team edits, ejection, `never` policy, and rejected updates. No skill retirement means no new cleanup migration is necessary.

A's proposed observed-execution coverage, using actual task effects and tool traces:

| Scenario | Observable pass condition |
| --- | --- |
| Typo and local bug | Typo needs no invented process artifacts or irrelevant tests; bug is reproduced/fixed with focused evidence and relevant existing constraints. No effort-closure query for a task without an effort. |
| Approved feature with review fix | All covered steps complete through applicable checks; a routine in-scope correction does not trigger plan approval again. New scope is surfaced precisely. |
| Fresh resume, then handoff | Approval and checklist are visible; current revision and remaining plan item are verified; valid evidence is reused. Closed effort remains historical. |
| No durable change / real durable change | First case produces no extra doc; second updates the existing owning guidance and valid links, with rationale retrievable by a fresh agent. |
| Foundation / ingestion | Only unresolved formative decisions are presented; authorized source writes retain provenance and candidates are not silently accepted. |
| AE plus domain | Relevant accepted constraints affect implementation; conflicts remain explicit; domain changes reach domain-backport through the agreed hook without duplicate universal retrieval. |
| Degraded retrieval | Tool warnings remain visible; agent does not treat missing context as empty, claim verification, or loop on the same failure. Useful independent work continues. |

Record fixture revision, installed template revision, model/reasoning, transcript, actual diff, checks, human-question count and reason, redundant calls, durable updates, and fresh-agent retrieval result. Routing self-reports supplement execution. No numerical success threshold or model budget is invented here; A's reviewed pilot protocol owns sample sizes and cost limits. Luna results do not prove behavior on other models.

## Evidence and integration dependencies

The checkout-built CLI reports `v0.50.5`; `NO_WEB=1 make build` used vendored dependencies and the `fts5` build tag. The project launcher successfully ran `agent start`, `agent surface`, ontology inspection/authoring guides, effort and spec recipes, and the bounded resume-field query. Startup warned `indexed-context-missing`; no semantic/code-index coverage claim is made. Direct source inspection plus successfully returned typed note data support this plan.

`./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` passed with zero issues on the unmodified baseline. Final changed-note results are recorded in the effort. Current implementation gates and scratch-install idempotence results are recorded in the effort as they complete; central repository regeneration remains coordinator-owned.

The remaining dependencies are operational: coordinator reconciliation of its two spec-drift findings, Domain consumers and combined regeneration, package A's bounded Luna xhigh evaluation, and current-head review/gates. None requires reopening the approved implementation plan. Results and limitations are maintained in the effort; no behavioral model outcome or central integration is inferred from source tests.
