---
type: EffortNote
id: EFF-2026-11-09-21-28
aliases: [EFF-2026-11-09-21-28]
name: Agentic Engineering delivery experience
created-at: 2026-11-10T02:35:34Z
status: active
summary: "Deliver the complete refined Agentic Engineering starter experience with exact authored scope, installed UI proof, and an open unmerged PR."
governing-specs:
  - "[[docs/specs/experience/agentic-engineering-delivery-experience.md]]"
---

# Agentic Engineering delivery experience

## Scope

Deliver the complete refined Delivery experience for designers and leads shaping specifications and developers and agents delivering work. Include the home/workflow map, selectable work lists with one composed prompt, inline spec criteria, both-form effort briefs, contextual panel, all data and UI states, and supported install/update/eject behavior. Reuse the experience platform merged at base `019a59ea128b900b57dfbcf565481f10ea4e9553`.

The PR must remain open and unmerged. Agent invocation, automatic approval or acceptance, lifecycle writes, policy/reference browsing, a generic site framework, and unrelated platform changes are excluded. Minimal source-link pagination and explicit HTML workspace frozen-scope selection are necessary dependencies.

## Spec Set (Frozen)

- [[docs/specs/experience/agentic-engineering-delivery-experience.md|SPEC-0137]] at SHA-256 `deb5932b187c0cd25335b8261e00f85704b183f324686712dd17c126d6cd5302`. This is the exact uncommitted source snapshot, not an invented Git revision.

## Stories In Scope (Frozen)

- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US1|SPEC-0137.US1]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US2|SPEC-0137.US2]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US3|SPEC-0137.US3]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US4|SPEC-0137.US4]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US5|SPEC-0137.US5]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US6|SPEC-0137.US6]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US7|SPEC-0137.US7]]
- [[docs/specs/experience/agentic-engineering-delivery-experience.md#^SPEC-0137-US8|SPEC-0137.US8]]

The complete Requirements section of SPEC-0137 also applies. The eight story targets were returned as linkable with existing block targets by `rzm agent node-link`; see [locator evidence](2026-11-09-agentic-engineering-experience/evidence/story-locators.json).

## Spec Coverage Checklist

| Scope | Batch | Exit evidence |
| --- | --- | --- |
| SPEC-0137.US1, US2, US6 | 2 | Home/list rules, exact scope selection, prompt and review counterexamples |
| SPEC-0137.US3, US4, US5 | 3 | Full criteria, both effort forms, authored provenance, panel and navigation |
| SPEC-0137.US7 | 4 | Real light/dark, loading/error/partial, long/narrow/zoomed and keyboard proof |
| SPEC-0137.US8 | 4 | Fresh/update/eject fixture, native alternatives, clean init --check |
| Requirements | 1 through 5 | Source-link and generation proofs, integrated gates and final review |

## Plan

### Authority and reviewed decisions

Drew directed, "Start a new chat with GPT 6.1 extra high using a Team of 6.1 high subagents to spec and implement this entire thing full stop." The implementation brief expressly authorizes specification, implementation, necessary minimal shared fixes, real UI proof, commits, push, PR, and current-head checks, with no merge. This is task-wide execution authority. It is not a claim that Drew reviewed the plan written afterward. `plan-approved-by` is absent because current-user is unconfigured. No additional discretionary human approval pause is introduced.

Independent plan review raised P1 about HTML frozen-scope attribution. Its recheck approved the explicit source selector. The [foundation contract](2026-11-09-agentic-engineering-experience/evidence/foundation-contract.md) is the final API/data/ownership contract and supersedes the initial draft's page-info and HTML assumptions. [Initial review](2026-11-09-agentic-engineering-experience/evidence/plan-review.md) and [approved recheck](2026-11-09-agentic-engineering-experience/evidence/plan-recheck.md) distinguish plan completeness from implementation acceptance.

Five factual primary views preserve Shape, Build, Settle orientation without an exclusive synthetic lifecycle. Shaping specs and Ready stories may overlap. Planned and Active use authored effort status; checked boxes do not move an active effort. Delivery history distinguishes complete and archived. Current contracts and Record review are supporting views. Every count names its scope and unit.

The exact shared extension is sourceLinks(first: Int = 100, offset: Int = 0) plus sourceLinkCount. Keep existing resolver, first semantics, bounded pages and authored occurrence order. Aggregate only matching source revision and graph generation. Export the existing kit responseGeneration accessor. Add one optional metadata-only EffortWorkspace.frozenScope material selector. New templates explicitly select their existing log; old records retain unknown authority rather than silently adopting log scope.

### Batches and ownership

| Batch | Usable outcome and ownership | Dependencies and exit |
| --- | --- | --- |
| 0. Contract | Root owns SPEC-0137, this effort, evidence, final architecture selection and review. | Live guides and IDs, linkable story targets, docs validation and independent plan approval. |
| 1. Foundation | Query owner implements bounded occurrence pagination/count and kit export, tests and owning docs. Starter-contract owner implements frozenScope schema/templates/recipes/guidance and tests. Domain owner implements canonical facts, coverage, grouping, prompts and adapters in delivery/lib and data.tsx with tests. | Follow foundation-contract.md. Prove exact HTML A/B/C sources, missing/broken/duplicate/same-source/criterion-only cases, page limits and generation coherence. Independent implemented-foundation review must pass before dependent UI integration. |
| 2. Home and lists | UI owner implements home, workflow map, selectable lists, exact composed prompt, first-day and truthful record review with shared facts. | Foundation interfaces and rules are accepted. Real installed home/list behavior and paired screenshots, clipboard success/failure and overlap counts. |
| 3. Records and panel | UI owner implements spec, both-form effort brief, shared presentation and CSS, panel, canonical find, actual Recent, routes and history. | Same foundation and existing kit. Full criteria and linked plan/log/material proof, source/coverage failures, deep links and keyboard navigation. |
| 4. Installed acceptance | Packaging/proof owner implements registration YAML, disposable fixture, installer lifecycle tests and E2E wiring. Root inspects all actual screenshots and exercises the installed UI. | Integrated source and one serial build. Fresh/update/eject correctness, retained native IDs, init --check zero, all major states/themes and reasonable narrow/zoomed layouts. |
| 5. Finish | Root integrates, reviews full diff, fixes findings, runs policy gates, aligns/backports docs, closes effort on branch, commits, pushes and files/links PR. | Independent implementation review, Greptile current-head 5/5 without unresolved comments, current-head CI, live PR open/non-draft/mergeable, no auto-merge or queue. Never merge or install Rhizome Dev. |

Writers use T3-managed isolated worktrees from committed contracts with exclusive file ownership. Preserve unrelated edits. Root integrates and inspects their actual diffs. Root owns changelog and final records; workers do not edit this effort or the new spec. Shared source/query/type interfaces, builds, gates and closure are serial integration points.

### Verification

Use repository commands, not invented substitutes. Focused query/init/model tests cover changed behavior. Run make check-fast in the edit loop, make check before Go/TypeScript commits, make test-content for shipped contracts, make build with disposable init/init --check, and make verify for final integrated full and browser gates. Run ./scripts/rzm validate and ./scripts/rzm validate frozen-scope-drift. Never run concurrent build or verification commands in one checkout. A failed or unavailable check remains a gap.

The fixture matrix includes every lifecycle value, mixed draft/ready/satisfied stories, overlapping groups, incomplete inventories, more than 200 selected refs, 101/201 source occurrences, changed source or target generations, exact criterion-only scope, mentions outside frozen scope, differing governing/frozen/log sources, absent approver, all-checked active effort, missing and duplicate sections, broken/unsupported components, no selection/mixed/multi-spec prompt scope, clipboard failure, long/empty content, direct links, tabs/history, panel Find/Recent, both themes, narrow/zoomed layouts, keyboard and focus.

The baseline empty installed starter was captured in both themes under /private/tmp/ae-experience-evidence/before. The original six comps are visual references only. Final proof must be the actual installed branch starter in disposable repositories. Keep live vaults untouched.

### Throughput checkpoint

Units are one small shared query extension, one starter scope contract, one pure domain/read adapter, five page families plus a panel, four retained native views, and three install lifecycle scenarios. Parallelize query, starter-contract and pure domain work after this contract commit. Gate UI integration on accepted foundation. Packaging fixtures can be prepared independently. Batch compatible reads and canonical hydration; avoid per-row detail calls. Reuse existing kit freshness and cache behavior. No speedup or unmeasured latency budget is claimed.

## Original Intended Delivery

A complete accurate Agentic Engineering starter experience for humans and agents, preserving the comps' workflow visual language and every authored authority boundary, delivered as one ready open unmerged PR.

## Actual Delivered

The complete starter-owned Delivery experience is implemented: home and five-group workflow map, seven factual work lists, selected prompts, inline spec criteria, both effort forms' resume briefs, contextual Find and Recent, first-day and incomplete-data states, themes and narrow layouts. Source-link pagination/count, explicit workspace frozen-scope selection, canonical ref navigation, source-aware Markdown links and provider-aware HTML commit reads preserve authored evidence.

All eight stories are backported as satisfied. Local `make check` and `make verify` pass, including 158 native, 90 Knowledge and 54 Delivery browser cases. Fresh/update/eject verification passes. [Installed UI proof](2026-11-09-agentic-engineering-experience/evidence/ui-proof.md) and [integration evidence](2026-11-09-agentic-engineering-experience/evidence/integration-notes.md) retain the results and limitations. [PR #123](https://github.com/atomicobject/rhizome/pull/123) is open and unmerged. Its five required checks passed on implementation head `46e792283d3781057f247056ed12a247ce0b3460`; remaining CI and a passing manual Greptile review remain required.

## Execution Notes

- 2026-11-10T02:35:34Z [decision] Opened this effort with live allocated ID EFF-2026-11-09-21-28. The earlier local filename stamp is independent of this UTC creation timestamp. Current user is unconfigured; approval metadata remains absent.
- 2026-11-10T02:35:34Z [validation] Typed specification and repository documentation validation passed with zero issues/errors, as did frozen-scope-drift before this effort was written. These are authoring checks, not implementation proof.
- 2026-11-10T02:35:34Z [decision] Recorded exact scope snapshot and verified eight story locators. Independent plan recheck approved the explicit HTML frozen-scope selector. Implemented foundation acceptance remains required.

- 2026-11-10T02:38:18Z [validation] The story-acceptance-pack CLI attempt ended with publish-current-projection context cancellation. Exact story targets remain verified through node-link and typed docs validation. Recipe execution must be proven against installed fixtures; no successful pack result is claimed.
- 2026-11-10T02:38:18Z [decision] Starting isolated implementation lanes from the committed contract. Registration IDs and stage parameters are fixed in the foundation contract.

- 2026-11-10T04:33:44Z [validation] The 48-case installed suite passed before final presentation refinements. Expanded acceptance passed 50 cases and failed two HTML-plan text assertions because the expected fixture wording was wrong; that assertion is corrected without retries or timeout changes. The final 52-case run is in progress.
- 2026-11-10T04:33:44Z [review] Root inspected all 44 screenshots from the expanded run at original size and identified final loading, source-link, scope-message, criterion-formatting and phone-sidebar proof refinements. Those changes are implemented and await the final accepted captures.
- 2026-11-10T04:33:44Z [decision] Comment review removed six redundant blocks. Root accepted the remaining three test-comment deletions and used local names for selector-generation agreement and untrusted fixture build tags, preserving their behavior.

- 2026-11-10T06:11:02Z [validation] Final make check and make verify passed: 2,169 web unit tests, 158 native browser cases, 90 Knowledge cases and 54 installed Delivery cases. Fresh/update/eject and init --check passed. All 47 accepted captures were inspected; 23 are committed with their manifest. No retry, timeout or skip was added.
- 2026-11-10T06:11:02Z [review] Integrated Sol-loop approved in three rounds; the diagnosed native preference fixture correction received a separate approved causal review. The HTML cache correction has cached/uncached red-before-green HTTP proof. Eight SPEC-0137 story states are backported to satisfied. External PR readiness remains active.

- 2026-11-10 [review] Manual Greptile returned 4/5 on `46e792283` with two confirmed findings: ambiguous spec-section coverage and recipe-presence assertions. Root fixed both, with red-before-green scope proof and all 16 named recipe cases passing. [The review ledger](2026-11-09-agentic-engineering-experience/evidence/greptile-review.md) records the evidence; strict template TypeScript, make check, make build, all 55 installed Delivery cases and documentation/frozen-scope validation pass. A new committed-head review remains pending.

## Deviations

The initial seven-stage comp becomes five factual primary views plus separate Current contracts and Record review. This is an authorized accuracy refinement that avoids inventing readiness, acceptance, or reconciliation. P1 added explicit frozen-scope source selection for HTML workspaces as a necessary dependency. Existing records are not rewritten. No implementation scope is deferred.

## Closure Checklist

- [x] Implemented foundation and independent technical review pass.
- [x] All eight stories and Requirements are verified in the installed interface.
- [x] Required local gates, documentation validation and frozen-scope checks pass.
- [ ] Independent implementation review and Greptile 5/5 cover final behavior.
- [x] Specs, owning docs, changelog, actual outcomes and deviations are reconciled.
- [ ] PR URL, current head, passing CI and open unmerged state are recorded.

## Compounding Follow-ups

Record recurring friction with its evidence. Fresh-worktree authoring needed OS/runtime access and delayed projection publication. It eventually succeeded; no indexing refactor is included in this effort. Compounding lookup was degraded by provider/authentication failures, so no-match output did not establish complete retrieval.

## Status

Active. Implementation, local verification, independent reviews and installed UI proof are complete. Current-head CI, manual Greptile 5/5 review and the ready open unmerged PR remain required.
