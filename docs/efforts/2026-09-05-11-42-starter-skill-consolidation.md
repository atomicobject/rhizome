---
type: EffortNote
id: EFF-2026-09-05-11-42
name: Agent experience integration
created-at: 2026-09-05T15:42:54Z
plan-approved-by:
status: active
summary: "Coordinate the base Rhizome, Agentic Engineering, Complex Domain, behavioral evaluation, and progressive code-mode efforts in one staging PR."
aliases:
  - EFF-2026-09-05-11-42
---

# Agent experience integration

## Scope

Coordinate the program defined in the [agent experience preparation brief](../reference/analysis/agent-experience-preparation.md): improve base Rhizome without starters, the primary Agentic Engineering starter, and Complex Domain as its extension. Preserve the engineering process, introduce proactive proportional base usage, and evaluate targeted tool changes including progressive code mode. Project-KB, engagement-tooling consolidation, and broad maintenance infrastructure are excluded.

This unfrozen draft is narrowed from its 2026-09-05 inventory to the direction Drew selected on 2026-09-07. Earlier proposals to fold foundation review, unify ingestion, or target a fixed skill count are design options to evaluate, not accepted requirements.

The coordinator owns shared specifications, composition decisions, init migration/topology changes and shared tests, generated-surface regeneration, cross-effort review, and integration evidence. Independent workers own the paths named in their effort. Every child PR targets `codex/agent-experience-integration`; the draft staging PR is #243.

## Spec Set (Frozen)

Not frozen. Candidate governing specifications are [[../specs/product/base-rhizome-agent-guidance|SPEC-0080]], [[../specs/product/init-starter-workflow|SPEC-0038]], [[../specs/technical/init-template-architecture|SPEC-0039]], [[../specs/product/agent-surface-integration-modes]], [[../specs/process/development-loop]], [[../specs/product/complex-domain-starter|SPEC-0062]], and [[../specs/technical/skill-template-overlays|SPEC-0063]]. Each child plan proposes exact criteria and any required spec amendments before scope freezes.

## Stories In Scope (Frozen)

Not frozen; selected during the specify pass.

## Spec Coverage Checklist

- [ ] Filled in when scope freezes.

## Plan

Current execution authority is recorded in the [delivery coordination record](../reference/analysis/agent-experience-delivery-coordination.md). Drew directed the idle supervisors to drive through delivery on 2026-09-07. This supersedes the planning-only checkpoint below; accepted scope, ownership, evaluation limits, and review duties are recorded there.

### Authorized preparation and planning

1. Create the five bounded planning efforts below, validate their structure and links, and push them to draft PR #243.
2. Start independent worktree chats from the pushed integration branch. Each completes its own effort plan and analysis note, proposes governing spec changes, and opens a draft child PR. No production edits or evaluation model runs during this first planning pass.
3. Reconcile the base composition contract, engineering phase/overlay boundaries, and progressive-discovery proposal. Integrate reviewed planning PRs and present one concrete implementation plan with any material remaining decisions to Drew.

### Independent efforts

- A. Behavioral evaluations
- B. Base Rhizome experience
- C. Agentic Engineering experience
- D. Complex Domain experience
- E. Progressive code mode

### Proposed implementation sequence

Before Complex Domain implementation, reconcile and close the original active delivery effort against its original scope; its remaining closure work is a coordinator prerequisite, not part of the redesign. After approval, establish the evaluation baseline and implement the base composition contract before starter changes that depend on it. Run bounded runtime work independently where file ownership permits. Review child PRs at their current heads, complete required gates, and merge only into staging. Regenerate installed surfaces centrally and verify a second regeneration has no updates. Evaluate the combined result, reconcile documentation, and present evidence for a separate main-merge decision.

Evaluation policy: no Astra runs. Use Luna extra high as the sole evaluation configuration for this plan. Do not infer stronger-model success solely from Luna success.

## Original Intended Delivery

An integrated improvement to the three prioritized agent experiences with observable evidence and bounded runtime changes. Detailed implementation scope remains unfrozen.

## Actual Delivered

Draft staging PR and planning handoffs only. No production redesign has shipped.

## Execution Notes

- 2026-09-07T12:21:46Z [planning] Drew authorized the five effort handoffs and planning chats. Narrowed the draft to the preparation brief and recorded Luna extra high as the evaluation model, with no Astra evaluation runs or other evaluation model in this plan.
- 2026-09-05T15:42:54Z [handoff] Opened by the agent that closed EFF-2026-09-04-17-52 so the follow-up starts with the inventory, the mechanisms, and the gate already in hand. Mechanisms available: overlay fragments may target a skill-relative Markdown file (`file:` in `skill-overlays/*.yaml`); retired skill names in `retiredStarterSkillNames` are cleaned up on rerun once the replacement router is accepted on disk; `scripts/routing-evals` runs the two-model routing eval and tabulates against expectations; `codex exec -s read-only` with the default model runs an Astra self-audit (the prompt shape that worked is in the 2026-09-04 analysis note; tell it not to delegate).
- 2026-09-05T15:42:54Z [learning] Things that bit last time: `--refresh-template-docs` is now safe for edited docs, but regenerating skill mirrors in this repo needs a temporary `updatePolicy: skills: always` in `.rhizome/workflows.yml` (remove it afterward); a second `rzm init --yes` must report no updates; the `rzm:skill-slot` markers must exist in whichever file an overlay targets or rendering fails closed; hubs, guides, README.md, and CHANGELOG.md all name shipped skills and need the same rename pass; closed efforts link retired skill template paths by mdlink, so `./scripts/rzm validate` will flag those to retarget.
- 2026-09-05T15:42:54Z [learning] Eval caveat: routing answers are self-reports under a read-only instruction, and Astra reports that instruction as its stop reason. Read `stop_reason` before counting a stop as a finding. An observed end-to-end run in a scratch repository is still the missing acceptance test.

## Deviations

- 2026-09-07 (frozen-scope-drift acknowledged via EFF-2026-09-07-08-21-3) SPEC-0038 US2 adds the reviewed local-task exit, authorization continuity, complete resume/closure evidence, proportional retrieval, and durable-maintenance criteria. The coordinator inspected the amendment at 98deb338 against the approved experience scope. Integrated behavioral evaluation remains outstanding.
- 2026-09-07 (frozen-scope-drift acknowledged via EFF-2026-09-07-08-21-3) SPEC-0063 documents the existing file-targeted overlay API and the two approved additive alignment/reconciliation hooks while preserving existing slots. The coordinator inspected the amendment at 98deb338; this acknowledges the shared input without expanding runtime scope or claiming integrated completion.
- 2026-09-07T12:58:37Z (frozen-scope-drift acknowledged via EFF-2026-09-07-08-21-2) SPEC-0080 is being amended under Drew's delivery authorization for proactive proportional base use, evidence authority, durable maintenance, authorization continuity, and starter composition. Its US6 update records the documented design review while preserving manual-only model runs and separately bounded run authority. The coordinator inspected the Base amendment against the reviewed plan; this acknowledges the integration input without claiming implementation or evaluation success.
- 2026-09-07T12:58:37Z (frozen-scope-drift acknowledged via EFF-2026-09-07-08-21-2) SPEC-0039 US5 AC2 now makes the base evidence/composition boundary explicit and leaves starter workflow semantics with their owner. This reviewed clarification preserves existing template and overlay topology; the Base child owns implementation and focused evidence.
- 2026-09-07T12:58:37Z (frozen-scope-drift acknowledged via EFF-2026-09-07-08-21-2) SPEC-0045 US4 AC1 is aligned by the coordinator with the authorized proactive managed-router behavior. Trivial work stays lightweight; existing risky-mutation routes, skill-authoring opt-in, and explicit integration opt-outs remain intact. US4 returns to ready pending delivery evidence; no completed historical effort is refrozen.
- 2026-09-07T12:57:03Z (frozen-scope-drift acknowledged via EFF-2026-09-07-08-21-4) SPEC-0062 is being amended under Drew's delivery authorization for the nine selected Complex Domain criteria: source/provenance preservation, explicit acceptance authority, conflict handling, proportional structural retrieval, result qualifications, context reuse, alignment/backport handoffs, shared mechanics, and bounded source-impact evidence. The coordinator inspected the amendment against the reviewed plan. This integration effort acknowledges that specific evolving spec input; it does not refreeze the original Domain effort, claim its missing coverage view is delivered, or expand the redesign into view/runtime work. The Domain child effort owns the verified criterion selection and delivery evidence.
- 2026-09-07: Drew narrowed this unapproved draft to three experiences, evaluations, and targeted tools. Removed Project-KB and engagement consolidation from the candidate scope; added progressive code-mode design using PR #157 as source material. Preserve the older execution notes as provenance, not current authorization.

## Closure Checklist

- [ ] Filled in when scope freezes.

## Compounding Follow-ups

None yet.

## Status

Active delivery coordination. All five supervisors were resumed under Drew's explicit delivery instruction. See the delivery coordination record for current decisions, thread/PR IDs, and bounded evaluation authority. Main integration remains a separate decision.
