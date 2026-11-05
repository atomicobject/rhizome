---
type: EffortNote
id: EFF-2026-11-03-11-17
aliases: [EFF-2026-11-03-11-17]
name: Evidence verification for every unsettled compounding candidate
created-at: 2026-11-03T16:16:14Z
status: complete
plan-approved-by: Drew Colthorp
summary: "Remove Jev's verification veto, resume deferred candidates, and prove real dispositions and workspace status against an isolated vault."
governing-specs: ["[[session-memory-starter]]"]
---

# Evidence verification for every unsettled compounding candidate

## Scope

Deliver the smallest complete admission fix, offline stage regressions, visible resumable status, the saved 24-case replay plus postcard control, and a focused reviewable PR. Preserve passage screening, model/version routing, evidence/provenance safeguards, settled decisions and human publication approval. Work only in the assigned checkout and disposable fixtures. No live-state replacement, global installation or merge.

## Spec Set (Frozen)

- [[session-memory-starter|SPEC-0124]], amended by this authorized scope at main base `fd58aaa8`; current source SHA-256 `5eec48a85b642d31c13462c64e78c94d47edf129c6d2e6e39b1f9c35faa3acf2`.

## Stories In Scope (Frozen)

- [[session-memory-starter#^SPEC-0124-US1|SPEC-0124 US1]]: admit all unsettled candidates to evidence verification while preserving rejection controls.
- [[session-memory-starter#^SPEC-0124-US4|SPEC-0124 US4]]: resume count/spend backlog without duplicate proposals or lost suppression.
- [[session-memory-starter#^SPEC-0124-US8|SPEC-0124 US8]]: keep isolated state separate and notice pending work in scheduled checks.
- [[session-memory-starter#^SPEC-0124-US9|SPEC-0124 US9]]: show actual run coverage and exact store in the shipped workspace.

## Plan Approval

Drew's November 3 task authorizes implementation and ordinary design choices: "Implement the smallest complete fix, run meaningful offline tests and a bounded real replay, and make the result reviewable in a PR." The task authorizes the existing stronger verifier for all extracted candidates, visible budget/count deferral, private source preservation, and the 24-case replay plus controls. It explicitly excludes merge and desktop installation. This effort records that supplied plan rather than requesting it again.

## Spec Coverage Checklist

- [x] Low-score normal Jev and supported fallback candidates reach real evidence-verification stages.
- [x] Duplicate, obsolete, generalized-category and provenance controls remain enforced; a synthetic supported gap proposes.
- [x] Count/spend resume, no duplicates, legacy C eligibility and settled-decision suppression are covered.
- [x] All 24 held cases plus postcard control receive real Terra dispositions against frozen disposable source.
- [x] Installed workspace shows real deferral and completion in its actual isolated store.
- [x] Required local checks and independent review corrections recorded; carrying PR adds final-head review and CI evidence.

## Plan

1. Preserve current main and private evidence. Freeze disposable source and seed only the 24 held candidates plus one negative control, keeping scores and chronology.
2. Replace estimate-based exclusion with an eligible queue. Keep priority, evidence verification, safe scopes and human provenance. Resume older count-deferred work automatically; expose exact coverage and store through existing Health/report surfaces.
3. Exercise actual mine stages with anonymized deterministic fixtures, then run only the bounded saved-pool Terra replay. Verify real installed UI during deferral and after completion; compare live-state hashes.
4. Run repository gates, inspect the full diff and obtain independent/Greptile review. Record exact outcomes and limits, close this implementation effort on the branch and file a focused PR. Do not merge or install globally.

The escalation boundary is a new model/passage-recall policy, publication, or live-state mutation. None is needed.

## Original Intended Delivery

One focused PR that lets the existing evidence-reading verifier inspect unsettled learnings and makes unfinished verification visible and resumable, proved on the saved vault candidates without touching live mining state.

## Actual Delivered

Core queue and Health reporting implemented. All 25 saved cases reached Terra verification and were rejected with zero proposals or contract failures for $0.5517428. Browser proof demonstrated count deferral followed by completion using the installed disposable module. Full detail and limitations: [[compounding-verification-gate]]. Local gates, independent corrections and the private evidence archive are complete. Carrying PR [#80](https://github.com/atomicobject/rhizome/pull/80) records final-head Greptile and CI state.

## Execution Notes

- Refreshed origin/main and confirmed base `fd58aaa8b15b81ace7b70d18c632052c0fdfb38f`. No primary-checkout changes.
- First worktree build hit sandbox DNS restrictions for team API dependencies; the authorized outside-sandbox build passed. Generated module installation and `init --check` passed in the disposable vault.
- Full offline compounding suite passed 184 tests. Independent review found a scheduled-check field mismatch and noncanonical API/CLI checkout identity; both were corrected. The new scheduler regression uses an actual mining dry-run.
- Initial `make check` failed only the newly exposed macOS report-path comparison; canonicalization corrected it. Final gate rerun is pending.
- Real runs `20261103T160821-70f81c` and `20261103T161156-2d2642` spent $0.032633 and $0.5191098. An unchanged third invocation returned `nothing-new` without a new run or model calls. All 24 former holds and the postcard control were checked; no manually staged UI examples were created.
- The assigned checkout's metadata refresh repeatedly failed with `note projection is not current`. Effort DATETIME allocation succeeded in a metadata-only disposable fixture containing this checkout's ontology and existing effort claims. This does not claim the failed checkout retrieval succeeded. Temporary local index/embedding config changes were restored byte-for-byte.

- Final `make check` passed (including 1,774 web tests); `make web-e2e` passed 110 browser tests. Default validation passed with zero issues after supplying the required Compounding Follow-ups section. Frozen-scope validation flagged only unrelated pre-existing EFF-2026-10-03-18-33 / SPEC-0111 drift; this effort was not flagged. All 2,309 live state files remained byte-identical.
- Greptile reviewed `2c8b6a84` at 4/5 and found the unbound dry-run path omitted pending counts when both harnesses were signed in. The actual-mine task test reproduced it red, then passed green with verification status included before harness selection. The budget-stop command also now requests a larger approved cumulative budget instead of repeating the exhausted amount. Final-head review is enforced in the carrying PR.
- Real browser proof used the installed module's real store. After the review corrections, rebuilt module/native Health checks retained the same actual results; no additional model replay was needed. The full private packet is retained in an ignored worktree archive.

## Deviations

The task was already a detailed approved execution brief; no extra approval round was introduced. The replay reuses frozen extraction/grouping outputs, so it directly tests admission and verification rather than repeating full-month extraction. Source and outside-service uncertainties remain explicit in the analysis.

## Closure Checklist

- [x] Authorized behavior, private replay and installed workspace proof delivered.
- [x] Local gates and independent/Greptile correction evidence recorded; final-head Greptile and green CI remain enforced before any future merge.
- [x] Implementation closure committed before publication, and carrying PR [#80](https://github.com/atomicobject/rhizome/pull/80) recorded immediately after creation.

## Compounding Follow-ups

Invalid cached verifier responses remain visibly pending and need evidence/response repair. General screening recall and outside-service claims are outside this bounded replay.

## Status

Implementation, offline verification, bounded real replay and installed workspace proof are complete. PR [#80](https://github.com/atomicobject/rhizome/pull/80) owns final-head review and CI evidence; no merge or desktop installation is authorized.
