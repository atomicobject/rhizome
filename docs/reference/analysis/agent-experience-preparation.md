---
type: ReferenceDoc
summary: "Settled scope, success scenarios, and proposed staging plan for the base Rhizome, Agentic Engineering, and Complex Domain agent experiences."
reference-kind: analysis
last-verified: 2026-09-07
status: draft
---

# Agent experience preparation

## Status and provenance

Drew Colthorp requested the assessment and staging setup in the Codex conversation on 2026-09-07. The decisions below record his explicit answers during grill-with-docs. They approve the direction and scope. Drew subsequently authorized effort preparation, draft PR updates, and independent planning chats. Drew later directed execution of the bounded plans; see Active delivery for the current authority.

The first assessment inspected revision `8d01660f`. The integration worktree starts from current main at `b978cc53`, on branch `codex/agent-experience-integration`. The original main checkout was fast-forwarded to the same revision. The staging worktree is `/private/tmp/rhizome-agent-experience-integration`.

## Settled decisions

1. Prioritize three experiences: base Rhizome without starters; Agentic Engineering as the primary development workflow; and Complex Domain as its extension for complex projects. The unused Project-KB starter is low priority and excluded from this program.
2. The first staging PR covers experience redesign, evaluations, and targeted tool improvements justified by workflow gaps. Broad source-change and knowledge-maintenance infrastructure is deferred.
3. Preserve the spec, bounded effort, and approved plan model, including the lightweight local-task path. Improve execution and remove unnecessary ceremony and repeated approval requests.
4. Starter-free agents should use Rhizome proactively and proportionally for substantive work, even when the user does not mention Rhizome. Trivial edits remain lightweight. Preserve durable knowledge when useful, without manufacturing documentation for every task.
5. Include a bounded progressive code-mode design and evaluation effort, using PR #157 as source material. Compare with efficient batched CLI/GraphQL and direct tools; avoid full-schema startup. Selected semantic writes must preserve current edit-session contracts.
6. Use Luna at extra high (`gpt-5.6-luna`, `xhigh`) as the primary evaluation configuration. Drew explicitly excluded Astra evaluation runs because of cost. Start with a bounded Luna pilot; no other evaluation model is included. Success on Luna is evidence, not proof of behavior on every stronger model.
7. The user authorized an isolated staging worktree, independent worker chats, child PRs, and merging reviewed child PRs into the staging branch. Final integration into main remains a separate decision.

## Ownership

| Layer | Responsibility |
| --- | --- |
| Base Rhizome | Discover capabilities and repository knowledge; retrieve relevant constraints and evidence; navigate the configured ontology; author safely; validate proportionally; handle unavailable or stale capabilities. |
| Agentic Engineering | Apply those capabilities during specification, planning, implementation, verification, alignment, and reconciliation; maintain the durable knowledge affected by development. |
| Complex Domain | Connect sources, domain concepts, requirements, processes, and workflows to engineering scope; preserve provenance and distinguish candidate from accepted meaning. |

The [root vocabulary](../../../CONTEXT.md) names these layers. Their boundaries should determine the skills and tools, rather than preserving the current command-by-command organization by default.

## Proposed success scenarios

| Scenario | Observable success |
| --- | --- |
| Starter-free local bug fix | Agent discovers relevant existing constraints, uses exact source evidence, fixes the bounded problem, and updates affected durable guidance only where useful. |
| Starter-free trivial edit | Agent finishes without unnecessary sessions, broad discovery, new process artifacts, or irrelevant gates. |
| Approved engineering feature | Agent retrieves frozen scope and the approved plan, completes the authorized work, collects meaningful evidence, and reconciles affected documentation without repeated permission requests for routine choices. |
| Resume engineering work | A fresh agent finds the current state, rationale, outstanding work, and evidence through durable records and links. |
| Complex-domain source change | Agent identifies affected domain knowledge and requirements, preserves conflicting or uncertain evidence, and identifies the implications for specifications without silently changing accepted scope. |
| Degraded retrieval | Agent distinguishes unavailable or incomplete retrieval from an empty result, gives accurate remediation, and preserves useful work that does not depend on the missing capability. |

Run scenarios against representative scratch repositories with no starter, Agentic Engineering, and Agentic Engineering plus Complex Domain. Compare the current and revised installed surfaces. Measure task correctness, missed constraints, inappropriate stops, redundant work, quality of durable updates, and fresh-agent retrieval. Routing self-reports supplement observed execution.

## Existing scope and integration dependencies

The unapproved, unfrozen [Agent experience integration effort](../../efforts/2026-09-05-11-42-starter-skill-consolidation.md) reuses and narrows the earlier Starter skill consolidation draft. Its original Project-KB and engagement-tooling scope is excluded. Its earlier proposals about folding foundation review and ingestion skills are candidates to evaluate, not settled decisions in this interview.

Existing governing specifications include [base Rhizome guidance](../../specs/product/base-rhizome-agent-guidance.md), [starter workflow](../../specs/product/init-starter-workflow.md), [agent-surface integration](../../specs/product/agent-surface-integration-modes.md), and [Complex Domain](../../specs/product/complex-domain-starter.md). The existing Complex Domain effort still has unfinished closure work. The coordinator must reconcile and close its original delivery before new implementation changes SPEC-0062 or the same starter assets; planning may proceed independently. Do not absorb unfinished original delivery into the redesign or silently refreeze the old record.

Code Mode PR #157 is open at `b1f971d7ef572179459c839eb0380d3da756afae` and currently conflicts with main. It overlaps the base skill, init, operation catalog, semantic writes, and agent runtime. Use selected ideas from it in the progressive code-mode effort; do not import the branch wholesale. Its one-shot complete discovery contract is a candidate to replace with progressive discovery. Its historical checks do not verify compatibility with the current staging baseline.

Current main includes edit-session identity changes made after the first assessment. Any selected semantic-write work must preserve [preview lineage and replay conflicts](../../specs/technical/ontology-edit-replay-conflict-contract.md), current canonical references, and durable citation requirements. A planned block locator must resolve to a usable target before it becomes frozen scope.

## Proposed independent work packages

### A. Behavioral evaluation foundation

Own scratch-repository fixtures, scenario execution, evidence capture, and result comparison. Establish the current baseline before changing expected outcomes. The [existing routing evaluation](agentic-engineering-routing-evals-2026-09-04.md) provides prior evidence and explicitly identifies the need for observed execution.

### B. Base Rhizome guidance and composition contract

Own the canonical base skill and its references, corresponding managed instruction templates, and their tests. Define proactive proportional use, context selection, evidence interpretation, authoring, validation, and recovery. Give shared mechanics one owner. Publish the composition contract consumed by C and D before those workers change their instructions.

### C. Agentic Engineering experience

Own the starter's canonical skills, phase references, and starter documentation. Preserve the process model while improving retrieval during work, authorization continuity, documentation maintenance, reconciliation, and handoff. Consume B's contract instead of duplicating mechanics.

### D. Complex Domain extension

Own the extension's canonical skills, overlays, recipes, and documentation. Preserve source provenance and requirement traceability. Compose with B and C, with focused retrieval and validation instead of repeated universal procedures.

### E. Progressive code mode and targeted runtime improvements

Design compact discovery and composable typed operations against current main, using PR #157 as source material. Evaluate the smallest useful read workflows against efficient CLI/GraphQL and direct tools. Selected preview/diff/apply writes must use current edit-session identity contracts. Precise readiness reporting and compact authoring context remain candidates justified by scenario failures, not approval to implement every item.

### Integration ownership

The coordinator owns cross-package decisions, shared spec records, the integration effort, staging status, shared files, generated-surface regeneration, final scenario runs, and integration review. Worker ownership must name exact paths before dispatch. Workers must preserve unrelated changes and avoid independently editing shared generated surfaces.

## Dependency and dispatch plan

1. Prepare independent effort drafts and dispatch planning chats from the pushed integration branch. Each returns a concrete implementation plan and proposed spec changes in a draft child PR.
2. A defines the evaluation protocol while B develops the shared composition contract and E designs progressive discovery. C and D can inspect and plan in parallel against the provisional boundaries. Run A's baseline only after its protocol and bounded run configuration are approved.
3. Once the shared contract is reviewed, run C and D as bounded independent efforts with explicit dependencies. Start selected E efforts only after their contracts are concrete.
4. Open child PRs targeting `codex/agent-experience-integration`. Review and verify each current PR head before merging into staging.
5. Regenerate the composed installed surfaces centrally and prove idempotence. Run end-to-end scenarios against the complete staging result; reconcile and rerun affected evidence when integration changes invalidate an earlier result.
6. Present the staging PR, actual evidence, and remaining limitations for the final main-merge decision.

## Gates and working agreements

Follow [review and approval](../../engineering/review-and-approval.md), [quality gates](../../engineering/quality-gates.md), [documentation policy](../../engineering/documentation.md), and [release policy](../../engineering/release.md).

Each effort needs an approved implementation plan before production work. Schema, public API, or shared ownership decisions that constrain later work get an explicit foundation-review exit when the plan requires it. A worker's completion report is evidence to review, not proof that integration or release is complete.

Child PRs receive focused checks and required repository gates. Generated template changes require a build, regeneration, and a second regeneration with no updates. The coordinator reruns integration-sensitive checks against the combined staging head.

No new architectural ADR is warranted by the scope decisions alone. Record one if a consequential, costly-to-reverse tradeoff is resolved during design.

## Planning dispatch

The [research and supervisor handoff](agent-experience-research-handoff.md) preserves assessment rationale, evidence limits, failure examples, and the development-team model policy. Supervisors read its relevant sections before finalizing their plans.

All five chats begin from the pushed staging branch and own their effort plus one analysis note during the first pass. They inspect production sources and propose changes without editing shared specs or generated files. The coordinator reconciles their proposals into a coherent implementation plan for Drew's approval. This allows concrete review before production changes while keeping independent planning moving.

| Package | Effort | Planning artifact |
| --- | --- | --- |
| A. Evaluations | Behavioral evaluations | `docs/reference/analysis/agent-experience-evaluation-plan.md` |
| B. Base | Base experience | `docs/reference/analysis/base-agent-experience-contract.md` |
| C. Engineering | Agentic Engineering | `docs/reference/analysis/agentic-engineering-experience-plan.md` |
| D. Domain | Complex Domain | `docs/reference/analysis/complex-domain-experience-plan.md` |
| E. Code mode | Progressive code mode | `docs/reference/analysis/progressive-code-mode-plan.md` |

The older manual routing evaluation effort remains frozen to SPEC-0080 US6. Package A must propose reuse or reconciliation explicitly; observed execution and code-mode comparisons do not silently replace that record.

## Active delivery

Drew subsequently directed the idle chats to drive through delivery. The [delivery coordination record](agent-experience-delivery-coordination.md) supersedes the earlier planning-only dispatch and pending-startup status. It records real thread IDs, implementation authority, delegated shared-file ownership, reviewed scope decisions, the authorized four-launch Luna xhigh pilot, and current review/merge responsibilities. Earlier planning descriptions above remain preparation history; they are not a new stop before routine implementation.
