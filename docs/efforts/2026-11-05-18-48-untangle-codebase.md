---
type: EffortNote
id: EFF-2026-11-05-18-48
aliases: [EFF-2026-11-05-18-48]
name: Untangle shared policy and obsolete code
created-at: 2026-11-05T23:53:07Z
status: complete
summary: "Completed the approved 46-group untangle plan, preserving supported behavior while removing duplicated policy, dead paths and hidden dependencies."
governing-specs: ["[[codebase-untangling]]"]
---

# Untangle shared policy and obsolete code

## Scope

The user-approved [execution plan](../reviews/2026-11-05-untangle-codebase/plan.md) covers all 46 candidate groups in the [audit inventory](../reviews/2026-11-05-untangle-codebase/README.md), with the corrections from the [independent review](../reviews/2026-11-05-untangle-codebase/inventory-review.md). Both comment and test cleanup passes are included. Implementation, caller migration, focused verification, documentation, integrated gates and fresh review are in scope. Publishing, merging and deployment are excluded.

## Spec Set (Frozen)

- [[codebase-untangling|SPEC-0132]], SHA-256 `1eae73369214ead3ab5e64f669219cb1b7ab629ccddda2db21e0758272db8384`.
- Implementation-plan approval snapshot SHA-256 `955cf36d56944e9524bbe4281d4867ea1968c2290bfc8d47488fe19a6a04045e`.
- Starting code revision: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`.

## Stories In Scope (Frozen)

Requirements-only technical work: [[codebase-untangling#Requirements|SPEC-0132 requirements R1-R12]]. No invented user stories or acceptance-criterion identifiers.

## Plan Approval

The user explicitly wrote "Approves" in the working conversation before implementation. Approval covers the recorded plan, its narrowed view-store fix, preserved graph/preview policies, characterized Markdown parser work, API retirements, and verification obligations. Approval was recorded in the plan before this effort was created. The current-user configuration is absent, so no configured Person identity or `plan-approved-by` metadata is inferred.

## Spec Coverage Checklist

- [x] R1 shared rules: batch 1.
- [x] R2 retirements: batch 2.
- [x] R3 indexing: batch 3.
- [x] R4 views: batch 4.
- [x] R5 application boundaries: batch 5.
- [x] R6 browser contracts: batch 6.
- [x] R7 runtime: batch 7.
- [x] R8 tooling: batch 8.
- [x] R9 team service: batch 9.
- [x] R10 test/comment cleanup: batch 10 and each owning batch.
- [x] R11 final evidence: focused checks, full integration and independent review.
- [x] R12 documentation and authority: owning documents, bindings, validated effort and final delivery record.

## Plan

Execute the [approved ten-batch plan](../reviews/2026-11-05-untangle-codebase/plan.md#batches-and-ownership). Eight workers own core/cache, search/code intelligence, queries/views, agent/graph surfaces, browser code, runtime/harness, tooling, and the team service. The lead owns validation composition (Q5), global subsystem documentation, integration, test execution and final review. Shared files have one owner. Workers must request root execution of focused tests, especially red-before behavior tests; no concurrent independent build or verification commands.

Preserve all decisions in the plan. Review found the large view-constructor redesign unnecessary; fix the actual store gate. Share graph and preview mechanisms without standardizing currently distinct behavior. Test Markdown protection differences before changing semantics. Share only pure billing decisions, with both backend contracts executed.

At each batch exit, record files, candidate IDs, actual verification and remaining risks. Run `make check-fast` and `make check` as appropriate, then `make verify`, desktop checks/package fixture, disposable init convergence and DynamoDB Local contracts. Fresh review must account for all candidates and integration boundaries. Complete closure only after those obligations are proven or explicitly recorded as unresolved.

## Original Intended Delivery

Complete the approved structural refactor and both cleanup passes in this branch, with preserved supported behavior, current owning documentation, meaningful regression coverage, passing required checks and an independent final review. No external publication is authorized.

## Actual Delivered

All 46 approved candidate groups and both selected cleanup passes are implemented. Shared alias, section, language, ranking, preview, graph, trust, validation and billing rules have one appropriate owner; obsolete APIs/state are removed; cache/bootstrap/view source files are split by responsibility. The required packaged test additionally exposed a pre-existing fallback heading identity mismatch, now fixed with shared construction and lookup plus typed and historical-selector controls. See the [delivery evidence](../reviews/2026-11-05-untangle-codebase/delivery-evidence.md) for every candidate and retained exception.

The final code passed the full build, race, integration, web and browser gate. SQLite and DynamoDB Local contracts executed, desktop checks and packaging passed, init converged in a disposable fixture and this checkout, and the actual packaged app passed heading navigation/reload and appearance persistence. The final `make check` passed. Code and its review/verification evidence are carried by local commit `ed628b75c7845345e5624e495a07f51a40de011b` on `t3/untangle-codebase`. This closure record ships on the same branch; no pull request, push, merge or deployment was performed.

## Deviations

No material change to the approved scope, wire schemas, persistence or provider policy. The packaged W4/R6 node-identity check revealed that untyped headings emitted a different structural fingerprint from the one lookup accepted. That additional shared-rule defect was reproduced, fixed, independently reviewed and covered by a fresh full gate. Existing selectors remain readable; typed section identity is unchanged.

The narrowed Q4 constructor choice, distinct graph/preview policies, characterized Markdown syntax and optional SVC/test retentions remain as approved. Slow typed authoring was resolved through supported direct identifier commands; [baseline evidence](../reviews/2026-11-05-untangle-codebase/baseline-evidence.md) records those attempts. A flaky new shutdown fixture was replaced with deterministic policy coverage while preserving real process tests, rather than increasing its timeout. The final pre-commit pass also exposed an existing wall-clock redaction guard under concurrent host load. A controlled process pause reproduced the false failure; the test now measures CPU work against the same two-second budget, with production redaction unchanged.

## Closure Checklist

- [x] All 46 candidates fixed or retained with reviewed evidence.
- [x] Baseline harness failure diagnosed and resolved without hiding it.
- [x] Required code, generated-surface, desktop, DynamoDB and user-interface checks recorded.
- [x] Full integrated diff and fresh structural review completed.
- [x] Spec, subsystem notes, code bindings and effort validation pass.
- [x] Actual delivery and any remaining limitations recorded with carrying commit(s).

## Compounding Follow-ups

Shared-rule lessons are encoded in the owning subsystem notes and regression tests. No memory or context-shard publication was requested or performed. Remaining intentional limits are listed in the delivery evidence; none is unfinished approved implementation.

## Status

Complete. All approved candidates, selected cleanup, independent review and applicable local verification are delivered. Carrying code commit: `ed628b75c7845345e5624e495a07f51a40de011b`. Final documentation and frozen-scope validation accompany closure. Publication remains outside this task.

## Execution Notes

- 2026-11-05T23:53:07Z [decision] Recorded explicit chat approval. The spec identifier allocator returned SPEC-0132 and the effort allocator returned EFF-2026-11-05-18-48 for this exact path. No configured current user was fabricated.
- 2026-11-05T23:53:07Z [validation] Baseline evidence: CLI/web build and team-service build completed; `make check` failed in the unchanged immediate-decode shutdown test; 20 focused reruns passed without resolving the cause. Web tests passed except sandbox-bound socket cases, whose complete file passed with local socket access. Final integrated validation remains pending.

- 2026-11-06T01:01:55Z [delivery] Implemented all approved candidates. Five integrated review passes are resolved; the packaged heading failure was reproduced before the supplemental identity fix. Full `make verify` passed after that fix, including 112 browser tests. Final local commit/closure remains in progress.

- 2026-11-06T01:11:53Z [closure] Final `make check` passed after replacing the redaction wall-clock assertion with the same CPU budget. Full `make verify`, explicit SQLite/DynamoDB contracts, desktop checks/package/interaction and init convergence passed. The approved spec and plan hashes still match. Code committed as `ed628b75c7845345e5624e495a07f51a40de011b`; closing documentation records the completed result on this branch.


## Publication Follow-through

After closure, the user requested a Sol review loop and pull request. Two GPT-6.1 Sol xhigh rounds approved the first-pass behavior and its integration with current main, with no findings. The integrated code is carried by `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`; [PR #101](https://github.com/atomicobject/rhizome/pull/101) carries the work and review evidence. The merged branch passed `make verify`, `make check`, generated-file convergence, documentation validation and frozen-scope validation. The [Sol ledger](../reviews/2026-11-05-untangle-codebase/sol-loop.md) records the exact review and verification scope.

The user also requested a new untangle pass with both cleanup options retained. That follow-on work is separate from this completed effort's frozen scope and delivery history. No merge or deployment has been performed.
