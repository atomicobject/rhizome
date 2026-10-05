---
type: EffortNote
id: EFF-2026-10-02-14-44
name: Save and view freshness
created-at: 2026-10-02T18:44:34Z
status: active
summary: Implement the measured save latency and view lifecycle fixes, review the integrated change, and open a pull request above drag-reorder.
aliases:
  - EFF-2026-10-02-14-44
---

# Save and view freshness

## Scope

Deliver [[save-view-freshness|SPEC-0109]] on top of the open drag-reorder branch. Preserve the existing writer lease, validation contract, and synchronous exact-path projection.

## Spec Set (Frozen)

- [[save-view-freshness|SPEC-0109]], initial requirements authored in this effort from the investigation and approved five-step recommendation.
- [[view-manual-ordering|SPEC-0108]] supplies the existing reorder behavior from parent PR #1, initially `be58b5b` and updated through `2c3ce1a` during integration. This effort adds no requirements to that spec.

## Stories In Scope (Frozen)

Requirements-only delivery. All SPEC-0109 requirements are in scope.

## Spec Coverage Checklist

- [x] Retain displayed edits through canonical adoption and release Save promptly.
- [x] Share the lifecycle with custom view readers.
- [x] Publish committed read freshness without losing background processing.
- [x] Bound background yielding and optimize measured synchronous work.
- [x] Verify, independently review, and open a PR.

## Plan

1. View lane owns `web/`: shared save/read lifecycle, configured view retention, custom view parity, and regression checks.
2. Yield lane owns background indexing and its writer coordination: reproduce the long yield delay, fix its cause while draining accepted writes, and measure the same workload.
3. Projection lane owns save publication and exact-path projection/postcheck: publish readiness at the existing barrier, preserve semantic convergence, and optimize only measured redundant work.
4. Integrate isolated worker changes, inspect the diff, run repository gates and the disposable IP opportunities workload, and fix review findings.
5. Open a PR targeting `drag-reorder`, record verification and remaining limitations, and link it to the thread.

### Authorization

Drew approved the preceding five-step recommendation in chat on 2026-10-02: "Use sol fast medium workers to divide and conquer and do the work. When it's done, do a code review, then open a PR". The configured current user is absent, so `plan-approved-by` is omitted. This authorization covers implementation, review fixes, and PR publication, but not merging.

### Throughput checkpoint

Three Sol workers at medium effort run concurrently in isolated worktrees. Each owns a distinct source boundary and reports its design before implementation. The parent owns integration, documentation, shared benchmarking, final review, and publication. Shared fixture servers and final gates run serially to avoid measurement and test interference.

## Original Intended Delivery

Saving a reordered IP opportunities view keeps the visible order stable, completes promptly, and refreshes other readers without waiting for the filesystem watcher.

## Actual Delivered

Configured views retain saved field values and ordering until a request issued after commit acknowledgement succeeds. The shared query hook and custom-view kit use the same lifecycle; custom queries can supply a pure optimistic updater for their result shape. Save no longer waits for unrelated query refetches. Initial query data cannot acknowledge saved changes before an actual fetch. Discard removes newer dirty edits while preserving committed display values until canonical adoption. Verified committed reference mappings let subsequent edits target the same embedded items even when their byte offsets or fingerprints changed; only expected fields written by the successful submission advance to their new values.

Saves publish `node.changed` after successful projection, cache, and workspace refresh. Warning responses do not publish readiness. Watcher inputs survive foreground cache refreshes so semantic and graph work still converges. Identifier migration observes cancellation, and canceled validation cannot report indexing complete. Prepared ontology-only postchecks capture affected source bytes, loading the full source inventory only when a block-id migration must rewrite external references. Source read failures remain validation failures.

## Execution Notes

- Investigation built parent PR #1 and reproduced display snapback in a component probe. Against a disposable copy of 3,635 notes with embeddings disabled, warm reads took 52 to 79 ms and an uninstrumented warm save took 1.84 s. Instrumented save phases included 791 ms projection and 482 ms postcheck/cleanup. A separate contended save waited 51.3 s for boot catch-up. Small-vault commits took 131 to 161 ms while freshness notifications could lag 14.85 s or remain absent after 17 s.
- Primary branch fast-forwarded to the parent reorder commit. Worker branches start at the same commit; no changes to the parent PR are authorized or needed.
- Controlled cancellation against the same copied 3,635-note workload, with cancellation requested 20 seconds into boot indexing, improved from 48.440 seconds between cancellation and return to 13.529 milliseconds. A goroutine sample located the delay in recursive identifier migration projection. This measures cancellation responsiveness, not total indexing speed.
- The repeat HTTP workload against the same disposable IP opportunities copy measured a contended save at 52,304.82 ms before and 2,106.53 ms after. The following warm saves were 2,065.22 ms and 2,095.39 ms, effectively unchanged; committed queries remained 53 to 57 ms and returned the saved ranks. Background scheduling makes these individual timings scenario evidence rather than a statistical warm-save speedup claim.
- A final repeat on the same copied vault measured saves at 896.85 and 862.85 ms, with no warnings. Immediate queries took 56.16 and 57.81 ms and returned the saved ranks. Warm timing varies across these runs; the controlled benchmark, not the lowest HTTP sample, quantifies the isolated source-capture improvement.
- `BenchmarkOntologyEditSessionSave` uses a synthetic 3,600-note vault. With only scoped source capture disabled for the before run, three repetitions of three saves yielded median 333.69 ms/op before and 186.10 ms/op on the final backend, a 44.2% reduction. Reproduce the current benchmark with `go test -tags=fts5 ./pkg/app/web -run '^$' -bench '^BenchmarkOntologyEditSessionSave$' -benchtime=3x -count=3`.
- A live small-vault HTTP and event-stream check measured commits at 119.83, 104.34, and 110.87 ms. Each `node.changed` event arrived before its commit response, at 119.63, 104.13, and 110.68 ms. Immediate queries took 0.89 to 0.92 ms and returned the saved ranks.
- Worker checks passed 34 focused web tests, lint and both typechecks, plus race-enabled bootstrap, indexing, and validation package tests. The integrated build passed. Generated custom-view API guidance updated through `rzm init --yes`; the second run reported no changes. Documentation and frozen-scope validation report zero issues.
- Independent correctness and standards reviewers found canceled validation could still report successful completion, a reference-source capture failure could appear valid, and the web package needed its publication contract documented. All findings were fixed, checked by the reviewers, and covered by focused tests where behavioral. Comment review found no actionable issues.
- The final `make check-full` passed race-enabled Go tests, integration tests, benchmark contracts, and all 950 web tests. The final `make check` also passed; after the last web-only parent update it passed all 951 web tests. Verification also consolidated clean-save success classification across two UI callers.
- The focused browser run (`cd web && npm run test:e2e -- tests/e2e/view-variants.spec.ts`) passed both tests. It holds view reads during save, checks stable saved ordering and an immediate new edit, then confirms discard and reload preserve the saved value. Enum and boolean sort ties defer to canonical ordering after refresh; the numeric component regression asserts exact ordering.
- Final review also covered committed-reference handoff, queued field witnesses, shifted item identities, and discard during pending reads. It corrected empty relation witnesses and kept source-reference lineage available on refresh warnings while still suppressing readiness events.
- Final `make web-e2e`: all 53 browser tests passed. An earlier run had one transient page-load timeout in the unchanged delayed-Spec navigation test; the full rerun passed without changing that test or production code.
- Parent PR #1 advanced to `9b38bab` during publication. Integrated its review fixes, preserving its enum-based browser setup with this effort's save/read assertions and updating the numeric regression fixture to declare indexed sorting. The merged focused run passed 29 component/ordering tests and both browser tests. Final verification also includes the subsequent reference-handoff and discard fixes. Two later parent commits through `2c3ce1a` align enum equality with server sorting and establish differing statuses for the browser test; both are integrated with the same save/read assertions.

## Deviations

A deterministic browser check held reads before the first stage and exposed stale embedded-item references on the next edit after Save. Commit now returns the existing replay's verified field-reference lineage; the shared controller and staging helpers use it without weakening structural or expected-field checks. No extra replay or renderer-specific identity map was added.

The source-read optimization required a lazy full-inventory fallback for block-id reference rewrites. Review also extended migration error propagation so failed source reads cannot authorize an incomplete repair. No persistence schema or writer ownership changes were needed.

## Closure Checklist

- [x] Implementation plan authorized in chat.
- [x] Changes integrated and verified.
- [x] Independent code review completed and findings resolved.
- [x] PR opened and linked: [PR #3](https://github.com/atomicobject/rhizome/pull/3), initially targeting `drag-reorder` above [PR #1](https://github.com/atomicobject/rhizome/pull/1). After that parent merged, the latest-main integration targets `main`.

## Compounding Follow-ups

Greptile's first review scored 3/5 and found two regressions. Home tables now receive the save/read lifecycle and retain their saved display through canonical adoption. Reference mappings belong to each retained reader instead of accumulating in the controller; a fresh reference after an external restore stays unchanged. Queued edits retain only the mappings from commits that completed after capture. Custom-view mutations can bind to their query's display session. Each behavior has a regression check, including independent readers adopting at different times.

The follow-up uses three workers with separate ownership for Home/View integration, controller queue handling, and the custom-view kit. The parent owns integration, independent review, gates, and CI monitoring. Drew explicitly authorized addressing all feedback and making CI green; the PR remains open for maintainer review.

Follow-up verification: Home snapback and restored-reference regressions failed before the fixes and pass afterward. The independent reviewer found no blockers in the integrated controller, configured views, or kit adapter. `make build` and generated-guide refresh passed, with no changes on the second initialization. The first `make check` found new-file spacing errors; after correction, the final `make check` passed all 958 UI tests and its Go, lint, type, and credential checks.

The follow-up `make web-e2e` passed all 53 browser tests. Documentation and frozen-scope validation each report zero issues. The next Greptile review and CI run will verify the published follow-up commit.

## Status

Active. Implementation, latest-main integration, local verification, and independent review are complete. PR #3 targets `main` and is open for maintainer review; merging is outside this effort's authorization.

## Latest main integration

Drew authorized merging latest `main`, resolving conflicts, and pushing on 2026-10-02. Integrated `838c52a`, including the unified view contract and merged manual-ordering and preview work. Resolution preserves the new catalog, contexts, selection, and navigation while moving save retention and reader-scoped reference translation into the shared native view host. Home, standalone, group/type, and node presentations carry the lifecycle into native or custom renderers. Kit `useViewRows` now uses the same retained-read lifecycle and optional optimistic updater as GraphQL reads.

Independent review found no blockers. `make build`, generated-guide refresh, and a second unchanged initialization passed. `make check` and `make check-full` passed all 1,025 UI tests plus Go, race, integration, benchmark-contract, lint, type, and credential checks. The first browser run rejected a duplicate reorder test created by automatic merge; consolidation preserves both readiness waits and the held-read save/discard assertions.

The final `make web-e2e` passed all 66 browser tests. Documentation and frozen-scope validation each report zero issues.

The fresh review after main integration scored 4/5 and identified an existing-main navigation issue: reopening a note without a presentation cleared its explicitly requested presentation. A red regression reproduced it. The shared tab helper now distinguishes an omitted presentation from explicit replacement or clearing, while plain URL hydration still clears the presentation. Independent review found no blockers. An existing title-callback assertion now waits for its effect to finish, preserving its exact call count and arguments. Final `make check` passed all 1,026 UI tests and standard checks; `make web-e2e` passed all 66 browser tests.
