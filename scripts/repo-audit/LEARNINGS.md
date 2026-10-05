# Jev audit lessons and next experiments

Recorded 2026-09-19. These results describe the first Rhizome repository sweep and source review, not a general benchmark of Jev. See [README.md](README.md) for current commands and report semantics.

## What ran

The original `jev-1.13.0` sweep selected 14,990 focus chunks from 3,063 admitted Markdown/source files. Notes completed 7,160 of 7,361 calls in 10.3 minutes; code completed 7,301 of 7,629 in 13.3 minutes. All 529 failures were HTTP 402. Successful chunk coverage was 96.5%; bounded comparison shortlists mean relationship coverage was much smaller and is not measured.

Successful responses reported 146,351,214 input tokens and 9,361,364 output tokens. These are provider-reported units, not a dollar estimate. Failed attempts may incur additional usage. Separate diagnostic experiments used another 14 successful calls. Raw checkpoints and evaluation evidence remain local under `.rhizome/audits/`; they contain source excerpts and are not committed.

The original 44,676 signals were mostly suggestions to link related content. Context exclusions, probability thresholds, grouping and bounded evidence reduced these to 54 investigation candidates. Every candidate received a source review:

| Source-review outcome | Count |
| --- | ---: |
| Distinct actionable findings | 8 |
| Duplicate confirmations of an existing finding | 2 |
| Rejected | 41 |
| Unresolved | 3 |

All eight useful findings came from 33 disagreement candidates. Thirteen link suggestions and four consolidation suggestions produced no justified fixes. Of four documentation-gap candidates, three were rejected and one remained unresolved. One useful finding was reconciliation of the current Jev/code-mode work, leaving seven pre-existing maintenance findings. These counts measure precision in a selected queue; they say nothing about missed defects.

## What was worth fixing

| Finding | Source-review conclusion |
| --- | --- |
| Persistent code-mode spec still promises serial requests | Reconcile with the bounded concurrency introduced in this branch. |
| Starter installation tree lists retired standalone skills and workflow policy | Update the guide to the actual phase router and factored policy files. |
| Configured table views effort says Phase 3 is pending | Reconcile the status prose with recorded delivery through Phase 8. |
| Unified workspace effort says implementation is pending | Reconcile with its eleven delivered tasks while preserving outstanding closure work. |
| PHP indexer effort has a pending coverage table | Reconcile delivery rows against the existing delivered evidence. |
| Frontend permission-mode union includes unsupported `auto` | Remove the extra type member; the UI already uses server capabilities. No runtime failure was demonstrated. |
| CLI batching helpers have only test callers | Remove the unused duplicate and retain valuable behavior coverage against the production code-intel implementation. Different worker caps were not a demonstrated production bug. |
| Alias-cache comment claims first-writer resolution | Correct the comment; the shared resolver and regression test already preserve ambiguity. |

## Why candidates were noisy

- Historical plans, completed implementation and current contracts were compared as if all described the same moment.
- Fixed line chunks hid test setup, caller responsibilities and mutually exclusive build conditions.
- Strong topical relationships were mistaken for missing documentation. Existing CONTEXT files, coderefs and subsystem bindings were often sufficient.
- Empty headings and common reference lists inflated consolidation signals.
- Multiple section pairs described the same correction.
- A high probability for a local question did not establish that a repository edit was justified.

## Changes made after evaluation

The default audit now asks about contract drift and status consistency. Link, consolidation and documentation-need questions require explicit exploration mode. Focus screening removes empty/navigation material, marked generated content, historical research and tests/fixtures; excluded material can still supply comparison evidence where admitted by candidate selection. Exclusions are coverage tradeoffs, not proof that excluded files are correct.

Packets include bounded lifecycle, heading, build and declaration context. Same-note status checks prioritize delivery evidence; other comparisons prioritize ancestor guidance before lexical matches. A separate action question distinguishes a correction from an expected difference or insufficient evidence.

Only source-reviewed confirmations enter the main report. Review packets are bounded and deduplicated, decisions cite source hashes, and rebuilding invalidates changed or newly ignored evidence. Corrections group by edit target and change key. Rejected and unresolved decisions remain recorded. The scripts do not invoke another agent automatically.

Limited runs use deterministic sampling and expose request previews. HTTP 401/402/403 stops new batches. Successful checkpoints are reusable; ambiguous or failed attempts are not blindly replayed. The inventory excludes its own audit output to avoid recursively auditing reports.

Offline behavior tests, real code-mode dry runs and report interaction checks passed. Reusing the original source reviews produced eight distinct confirmed improvements without new Jev calls. This validates the reporting workflow, not the accuracy of the revised questions.

## Next experiments, in priority order

1. **Measure a small live pilot.** After API funding is available, evaluate reproducible 100-packet notes and code samples. Track distinct useful fixes, duplicate burden, reviewer time and usage. Review a separate sample of screened-out inputs for missed positives. Keep the original 54 judgments as development evidence, not a held-out test set.
2. **Improve comparison evidence before broadening questions.** Resolve canonical coderefs, code-anchor globs and graph bindings. Use enclosing symbols and caller context when lexical declaration hints are insufficient. A missing shortlist entry must never become proof of missing documentation.
3. **Make status audits cheaper.** Consider deterministic extraction of lifecycle/status/coverage records before Jev evaluation. Check implementation completion separately from review, merge and closure. Preserve frozen scope and historical rationale.
4. **Evaluate incremental audits.** Cache by question/model version and whole-source hashes, then invalidate related comparisons when a governing contract changes. Measure savings and missed findings against a paired full run before adopting this as the default.
5. **Experiment with targeted agent verification.** Feed only pending candidate packets to a stronger coding agent, bound its work and import explicit verdicts. Compare review cost and correctness with manual source review before automating fixes.
6. **Revisit exploratory questions only with stronger evidence.** Consolidation needs a demonstrated duplicated obligation and a concrete replacement plan. Link suggestions need proof that the relationship is useful and absent. Generic similarity is insufficient.

## Open investigations

Three original candidates remained unresolved. They are not confirmed defects: whether the July routing-evaluation effort was superseded; whether permissive `NormalizeDocPath` output can actually reach a persisted-key write; and whether semantic presentation weights lack necessary rationale. Trace concrete callers, existing bindings, history and evaluations before changing behavior or inventing explanations.

Repository fixes will invalidate some saved candidates. Keep original run evidence for evaluation, and rebuild a separate report when checking the corrected tree.

## Maintenance follow-through

The follow-up fix pass addressed all eight confirmed findings. It reconciled the code-mode spec and installation tree, corrected the three effort summaries/tables against their recorded delivery, removed the unsupported frontend permission-mode member, and corrected the alias-cache comment. The unused CLI batching implementation was removed; its idle-flush, capacity-flush and failure-cancellation tests now run against the production code-intel writer with production constants. Existing production worker-cap coverage remains in place.

The two active efforts still require closure, and the completed PHP effort records an explicit administrative correction. No frozen scope or lifecycle state was changed. The three unresolved investigations above remain open questions.
