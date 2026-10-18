---
type: EffortNote
id: EFF-2026-10-17-14-00
aliases: [EFF-2026-10-17-14-00]
name: Critical subsystem refactor
created-at: 2026-10-17T17:58:43Z
status: complete
summary: Implement all nine subsystem audit lanes, verify shared contracts, and deliver an independently reviewed pull request with passing CI.
governing-specs: ["[[docs/specs/technical/subsystem-refactor]]"]
---

# Critical subsystem refactor

## Scope

Deliver the six-phase roadmap in [[docs/reviews/2026-10-17-subsystem-review/README]], including all nine lane reports and test findings. Baseline: `5251923fc1ac06ac6c9702eb3db25ca76da67d2f`, branch `t3/critical-subsystem-audit`.

## Spec Set (Frozen)

- [[docs/specs/technical/subsystem-refactor]] at its initial approved revision, derived from the audit roadmap and the user's instruction to implement it all.

## Stories In Scope (Frozen)

Requirements-only technical work. The full production and test finding inventory is the delivery checklist.

## Spec Coverage Checklist

Every audit finding is tracked in the [[docs/reviews/2026-10-17-subsystem-review/implementation-status|implementation record]] with its owner, result, and evidence. Scope includes correctness, semantic consolidation, dead-code removal, test cleanup, performance, and package structure.

## Plan

1. Verify findings and settle foundational contracts with Opus 5.5 High and Astra High. Add regression fixtures and record corrections to the audit.
2. Fix source corruption, destructive search opens, projection/query parity, graph and pagination defects, and bounded validation JSON.
3. Delete replaced implementations and consolidate Markdown syntax, scalar values, ontology builds, predicates, source transactions, validation orchestration, indexing controls, and search ranking.
4. Measure and remove per-row reads, whole-vault request work, duplicate resolution/configuration, and excessive open costs. Migrate consumers and improve shared test fixtures.
5. Simplify package ownership and upward dependencies after behavior is guarded. Update all callers and subsystem documentation without compatibility shims.
6. Run applicable full gates and live interface checks. Obtain independent integrated reviews, reconcile the effort, and deliver the PR with passing current-revision CI. The user subsequently removed the Greptile review requirement, as recorded below.

Each batch ends with focused observable evidence. The final gate sequence is `make build`, `make check-full`, then `make web-e2e`, without concurrent full builds or gates. Generated template changes also require real init and init check. Markdown and frozen scope validation run before closure.

## Plan Approval

The user approved the reported six-phase roadmap and recommendations in chat on 2026-10-17: "All right, do it all. Use opus 5.5 high and Astra high agents to review at critical points. Open a PR and use greptile CLI to force a review and iterate to 5 out of five."

Current-user configuration was absent. Approval is recorded here without fabricating a configured identity.

## Original Intended Delivery

Implement all audit findings that survive code verification, improve the tests and shared contracts, and provide a fully verified pull request with the requested independent reviews and Greptile score.

## Actual Delivered

All 222 original findings have source-supported dispositions: 212 implemented and 10 resolved with rationale. The implementation record links the delivered contracts and regressions. Shared source transactions, provider-aware publication, typed reads, read-only index access, command environments, search and indexing ownership, and test/gate cleanup are delivered. The fresh local build and full gate passed, Opus r13 and Astra r16 approved the final CI corrections, and all 21 CI checks passed on delivered source revision `ec83c811c9c53616495216af6268966afff346ad` in [PR #21](https://github.com/atomicobject/rhizome/pull/21). The PR remains open; merge and release are outside this delivery.

## Execution Notes

- 2026-10-17T17:58:43Z: Baseline `make build` passed. Rhizome session `VIoa9vWx0XBTXriV` started through the project launcher. Seven implementation lanes have distinct ownership. Opus and Astra High foundation reviews were launched through T3 delegation.
- 2026-10-17T17:58:43Z: Astra corrected the audit's journal quarantine policy, legacy journal migration, third writable search open, fixed-width timestamp encoding, provider invalidation, identifier dependencies, folded collisions, cancellation, and view configuration placement. These corrections preserve approved intent and constrain implementation.
- 2026-10-17: Opus source acceptance r7 approved the coordinator after eleven fault probes, full source tests and real Git integration. Final full-suite checks then found additional consumer defects and stale fixtures. Their corrections retain exact source, authority, cancellation and publication assertions; fresh Opus and Astra acceptance rounds are running against those corrections.
- 2026-10-17: Documentation validation resolved all introduced link and context-marker issues. Three remaining findings concern an existing backlink fixture and two existing agentic-process heading references. Frozen-scope validation identifies one unchanged active effort, EFF-2026-10-03-18-33. These are classified as unrelated/pre-existing content; no historical effort or spec was rewritten to silence them.
- 2026-10-17: Root completed `make build` and `make check CHECK_JOBS=2 GO_TEST_PACKAGES=2 GO_TEST_PARALLEL=2` with repository-local Go caches. All Go packages and 1,511 web tests passed at that checkpoint. The following `make check-full` reproduced an operation-host file-descriptor race in agentcode; the foundation owner is repairing ownership before the final gate rerun. Fresh Astra r11 and Opus r10 reviews cover the final Markdown admission and zero-width edit-overlap corrections.
- 2026-10-17: Astra r11 approved authored Markdown admission. Full agentcode race verification passed after isolating descriptor ownership. The next full run passed the other package races but lost a temporary review probe during validate compilation; it is not a completed gate. Opus r10 accepted the overlap guard and found a compatible-frontmatter composition regression, now under producer repair. Default documentation validation passes with zero issues after scoping synthetic fixture vaults out of project indexing and retargeting two links to their actual Markdown headings. The unrelated frozen-effort drift remains recorded.

- 2026-10-17: Final production build passed. After fixture-only integration corrections, `make check-full CHECK_JOBS=2 GO_TEST_PACKAGES=2 GO_TEST_PARALLEL=2` exited zero with repository-local Go caches, race-enabled unit and integration tests, static/content/credential checks, benchmark contracts and all 1,511 web tests. Opus r11 approved frontmatter composition; Astra r11/r13 approved Markdown admission and descriptor ownership. `CI=1 make web-e2e` then passed all 96 browser tests in 1.5 minutes. PR/CI/Greptile acceptance remains pending.

- 2026-10-17: PR #21 opened at `c935e47a` after all local gates passed. CI exposed a default linter configuration schema error before the action reached the intended gate configuration. Moved the existing errcheck test exclusion to the v2 `linters.exclusions.rules` key; both configurations validate under golangci-lint 2.12.2 and the actual analysis gate reports zero issues. The Greptile CLI refused the full 16.8 MB diff at its 3 MB payload limit; a hosted review was requested while an honest complete-coverage CLI alternative is investigated. No 5/5 acceptance is claimed.

- 2026-10-17: Cross-platform CI exposed stale test assumptions about portable link identity, canonical Windows path spelling, and a companion-path OS error. Fixture corrections preserve fresh/incremental equality, ambiguity, exact vault identity, diagnostic keys/counts and truncation. Four byte-sensitive golden/corpus files are pinned to LF; a disposable autocrlf checkout reproduces and then removes the exact Windows hash discrepancy without changing data or expected hashes. Local full metadata/Obsidian race suites and focused command/validation races pass; `make check-fast CHECK_JOBS=2` passes. The Windows branch-ancestry failure remains unresolved and has failure-only provenance diagnostics for the next CI run; no unsupported fixture or production change is claimed.
- 2026-10-17: Windows diagnostics proved that Git interleaves unrelated path additions. Astra r14/r15 then reproduced unsafe certification from independent births and sibling modifications of one path. The correction validates the full raw-bounded stream and proves each adjacent owned event's ancestry before certification. Direct parents add no query; a gap adds one bounded graph query per path. The bound covers total ancestry, so a short gap in an older repository can discard Git evidence. Identifier keepers retain canonical-key fallback with a reason; ambiguous links remain unresolved. Focused race tests passed in 17.844 s; Astra r16 approved after rerunning the original probes and checking 1,024 graph shapes. Native Windows CI remains pending.
- 2026-10-17: Linux diagnostics lost evidence after a two-second guard wait near the retained-artifact cap. Profiling identified serialized metadata scans. Cache prototypes risked stale pruning or quota undercount; parallel variants did not justify their added complexity. The selected one-line production correction raises the retry budget to four seconds and adds a mixed event/report regression while retaining every original persistence assertion. Forty candidate repetitions retained 480 starts and 480 reports, and the full diagnostics race suite passed. Opus r13 approved with the longer per-attempt wait, recorder serialization and sustained-stall shutdown costs documented. No scan speedup is claimed. Fresh integrated gates and native CI remain pending.

- 2026-10-17: After both CI corrections and final Opus/Astra acceptance, fresh `make build` and `make check-full CHECK_JOBS=2 GO_TEST_PACKAGES=2 GO_TEST_PARALLEL=2` exited zero. The gate included all Go races, 1,511 web tests, both integration stages, static/content/credential checks and benchmark contracts. Diagnostics passed in 25.119 s and identifier reconciliation in 25.004 s. The ten changed Go files retained their frozen gate hashes. Existing 96-test browser E2E evidence remains applicable because these corrections change no web behavior. Native Linux/Windows CI on the next PR #21 revision remains required. Fresh default documentation validation returned zero issues and errors; frozen-scope validation reports only the unchanged unrelated EFF-2026-10-03-18-33 drift.

- 2026-10-17T23:11:51Z: Verified all 21 checks passed on `ec83c811c9c53616495216af6268966afff346ad` in [CI run 39077275008](https://github.com/atomicobject/rhizome/actions/runs/39077275008), including Linux/Windows unit and integration suites, compiled-runtime evidence, browser E2E, builds and macOS desktop checks. All 222 findings have dispositions, required independent reviews are accepted, and the user-approved Greptile waiver is recorded. Reconciled the finding inventory and delivery records and closed the effort in PR #21. This final documentation update changes no delivered source and will receive its normal PR CI checks.

## Deviations

All source-delivery work remains in scope. Evidence-backed corrections to an audit recommendation and changes to review requirements are recorded here and in the implementation record.

- The user approved keeping PR #21 and using a hosted full-PR Greptile review after CLI 3.6.1 refused the full diff at its 3 MB payload limit. The separate Apex CLI review of `c935e47a..2222276b` returned 5/5 with zero findings; it covers only the lint follow-up and does not establish full-PR acceptance. The complete change remains in review scope.
- The user subsequently instructed us to skip Greptile. Full-PR Greptile review and its 5/5 target are no longer acceptance requirements. Independent Opus/Astra review and current-revision CI remain required; no full-PR Greptile score is claimed.

- Opus verified that the existing schema deliberately gives plain frozen story identifiers no graph semantics. The implementation removes the hardcoded inference and uses the existing authored wikilink relation contract; a new schema directive would contradict that contract.
- Schema extraction keeps `viewconfig` in its current package and changes its dependency to the immutable schema module. Moving only its path would add migration work without improving the boundary. Provider tag handling receives its own parity regression before the extraction removes the old source-text fallback.
- Failed current-user bindings in views remain closed for every comparison operator. An unresolved identity cannot supply a meaningful operand, including for `neq`.
- Section-neighbor reads use the audit's bounded incoming-source alternative. They project only indexed incoming source documents and touched overlays, including provider-owned links. Persisting a second fragment-endpoint schema is unnecessary to remove the observed whole-vault scan and remains outside this implementation.
- Query workspace projection stays with the query executor in cohesive files. The audit permits file splits without a new resolver registry; a new package and pass-through interface would increase coupling without changing ownership.
- The corpus keeps versions 1 and 2 byte-for-byte. Version 3 records source-path and symbol migrations and retires two complete historical cases whose named implementation no longer exists. Cross-version scores are not evidence of a quality gain.
- A repair retry interrupted again after commit requires a fresh plan. Recovery verifies and refreshes the committed result, then refuses more writes from the old plan. Exact transaction-ID matching remains authoritative; automatic resume would require explicit attempt lineage. Opus and the source owner accepted this fail-safe alternative.

## Closure Checklist

- [x] All audit findings have a verified disposition.
- [x] Required local quality gates and live behavior checks pass on the final corrections.
- [x] Opus High and Astra High integrated reviews are addressed on the final corrections.
- [x] Specs and subsystem documentation match delivered behavior.
- [x] PR #21 is linked and all 21 checks pass on delivered source revision `ec83c811`.
- [x] Delivery and compounding records are reconciled.

## Compounding Follow-ups

The file-size and Go analysis gates enforce the new limits. Provider architecture guards, bounded-read counters, real-lease crash probes, deterministic scheduling fixtures, and isolated command environments protect the consolidated contracts. Import migrations now use parsed import spans. The source-preservation incident and its unavailable-baseline limits are recorded in delivery evidence.

## Status

Complete. Delivered in [PR #21](https://github.com/atomicobject/rhizome/pull/21) with all 222 findings dispositioned, accepted Opus/Astra reviews, passing local gates and all 21 CI checks green on source revision `ec83c811c9c53616495216af6268966afff346ad`. Greptile was skipped at the user's request. The PR remains open for human review; no merge or release is included.
