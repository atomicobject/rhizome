---
type: EffortNote
id: EFF-2026-10-04-10-22
aliases: [EFF-2026-10-04-10-22]
name: Persistent diagnostics
created-at: 2026-10-04T14:22:05Z
status: complete
summary: "Deliver durable indexing reports and useful structured runtime logs with bounded local retention and an offline agent-friendly reader."
governing-specs:
  - "[[persistent-diagnostics]]"
---

# Persistent diagnostics

## Scope

Implement phases 1 and 2 of the approved observability recommendation: durable indexing diagnostics and consolidated, improved operational logging. Use plain files as the authoritative store. Evaluate the usefulness of each touched subsystem's evidence, including causes, outcomes, waits, reuse, and physical provider work. Open a pull request after integration and verification.

Excluded: the separately proposed fallback/resync and embedding optimization phases. Existing indexing correctness and cache behavior remain governing constraints.

## Spec Set (Frozen)

- [[persistent-diagnostics|SPEC-0116]], all requirements in the initial 2026-10-04 revision.

## Stories In Scope (Frozen)

Requirements-only delivery of SPEC-0116. [[indexing-observability-and-maintenance-policy|SPEC-0047]] supplies existing timing and maintenance constraints without expanding this effort into maintenance changes.

## Spec Coverage Checklist

- [x] Bounded local persistence, retention, atomic reports, and offline reading.
- [x] Operation-scoped indexing metrics, outcomes, waits, and fallback reasons.
- [x] Useful runtime, CLI, MCP, HTTP, provider, and background evidence.
- [x] Reader commands and generated agent guidance.
- [x] Behavioral, overhead, failure-path, concurrency, and integration verification.
- [x] Independent review resolved and pull request opened.

## Plan

1. Fix the shared event/report and bounded metric contracts, using independent design analysis. Keep record persistence generic and indexing report publication with the operation owner.
2. Implement five coordinated work areas: persistence/readers; indexing collector and providers; lane/indexing/watcher lifecycle; runtime/request/logging integration; offline command and agent guidance. Workers own disjoint files and review the usefulness of their evidence.
3. Integrate, run focused tests, compare a repeatable baseline workload, exercise offline commands against real persisted reports, and run the required full gate, build, documentation validation, and template regeneration checks.
4. Obtain independent review, fix accepted findings, reconcile the delivery record, and open and link a pull request. No merge or release is included.

## Plan Approval

Drew approved phases 1 and 2 in chat on 2026-10-04: "Let's just do 1 and 2. 3/4 can be a separate thing once the new diagnostic system is in place." He requested a Sol 6.1 team, useful subsystem data as well as consolidated persistence, phased execution, and a pull request. The current-user configuration is absent, so `plan-approved-by` is omitted rather than inferred. This plan records that authorized scope.

## Original Intended Delivery

An integrated diagnostics system that explains recent indexing and runtime work after processes exit, with bounded storage, useful measurements, agent guidance, and a verified pull request.

## Actual Delivered

Versioned per-process JSONL events and atomic JSON reports now retain bounded operational evidence under `.rhizome/diagnostics`. The latest completed index attempt has a protected copy. Indexing reports include bounded aggregate metrics, phase durations, queue/lock waits, discovery and fallback reasons, embedding reuse, physical provider requests and retries. Runtime, CLI, HTTP, MCP, search, and background boundaries retain correlated outcomes with safe attributes.

`rzm diagnostics index|reports|report|logs` reads the files offline with filters, JSON output, measured summaries, and explicit coverage warnings. Repository configuration controls persistence, retention, budget, and level. The managed Rhizome skills and public guide document recovery and comparison. Existing indexing and fallback behavior remains unchanged.

## Execution Notes

- 2026-10-04 [design] Independent persistence and instrumentation analyses converged on per-process JSONL segments, atomic operation reports, and a bounded extension of the existing timing collector. Plain files remain authoritative; no diagnostic database is introduced.
- 2026-10-04 [implementation] Five Sol 6.1 agents own the coordinated work areas. Indexing/lane work uses extra-high effort; other areas use high effort.
- 2026-10-04 [baseline] The unchanged checkout built successfully. Its binary is retained in ignored local work storage for matched workload comparison.
- 2026-10-04 [measurement] Seven baseline trials used fresh temporary vaults with 250 linked Markdown notes and 25 Go files, embeddings disabled, and `index --in-process`. Median wall times were 395.971 ms cold, 97.976 ms unchanged, 129.153 ms after one note edit, and 110.726 ms after one code edit. macOS boot identity is blocked by the sandbox, so the local fixture benchmark ran outside it. The same workload will be repeated after integration; these figures do not measure network/provider overhead.
- 2026-10-04 [environment] Building restored the project launcher and Rhizome retrieval. The configured current user is absent; this does not revoke the explicit chat approval.
- 2026-10-04 [integration] Config round-trip and init preservation tests pass. Offline diagnostics skips dotenv loading and repository executable selection before it reads evidence, so broken config and missing trust cannot prevent recovery.
- 2026-10-04 [integration] Existing CLI handlers that call `log.Fatal` must return errors through Cobra instead. A process exit inside a handler bypasses recorder teardown and can suppress its error after the standard logger is bridged. This integration repair preserves the command's operation and centralizes terminal reporting.
- 2026-10-04 [verification] First `make check-full` passed vet, web lint/typechecks, credential checks, benchmark contracts, and all Go race packages except init template expectations and a headless-to-attached replacement race. The template route list and line budget are corrected. The race came from reading mutable `os.Stderr` while capture teardown restored it; diagnostics now retains its initial default stderr handle.
- 2026-10-04 [review] Independent persistence review found stale rolling-window percentiles after bounded sample retention filled, and a Windows exclusive byte-zero lock that prevented reads of active segments. Separate bounded window samples plus explicit text coverage notices fix the first. A lock sentinel beyond event data fixes the second. The reviewer reran race tests for persistence, metrics, lanes, indexing, and watchers successfully.
- 2026-10-04 [verification] `rzm validate` and `rzm validate frozen-scope-drift` pass. The initial sandbox validation was blocked by repository lock/store prerequisites and succeeded outside the sandbox.


- 2026-10-04 [review] Final lifecycle review reproduced a canceled queued job whose report could publish after lane shutdown returned. Shutdown now waits for removed queued finalizers and concurrent close callers wait for complete shutdown. The focused regression passed ten race-enabled runs; the reviewer confirmed the locking and completion order.
- 2026-10-04 [review] Runtime review reproduced blocked output after a raw log sink failure and cross-facet search measurements in a shared timing display. Failed sinks now continue draining. Search timing bindings are immutable per context while display events remain shared; the original gated two-facet reproduction now reports one total span per facet. Focused search race regressions passed three runs. Both review scopes have no unresolved findings.
- 2026-10-04 [verification] The built binary passed a real temporary-vault smoke: headless runtime startup, runtime-dispatched `index --timings`, immediately readable successful index report, correlated request/job events, watcher edit, and stop. A synthetic query marker was absent from structured events. With malformed configuration, explicit-path offline reading returned the same report and left every fixture file unchanged.
- 2026-10-04 [verification] `make build`, template regeneration with `rzm init`, and `rzm init --check` pass. Generated Codex and Claude guidance matches the source templates. `rzm validate` reports zero issues.
- 2026-10-04 [measurement] Recorder microbenchmarks measured 4.8 to 5.6 microseconds per enabled event and 2.7 nanoseconds with zero allocations for disabled INFO events. These isolate event recording and do not describe end-to-end indexing cost.

- 2026-10-04 [integration] The next full gate passed the complete Go race suite and all 1,341 web tests, then exposed protocol and lifecycle regressions in integration tests. Static agent discovery must remain write-free, protocol-owned errors must not gain terminal prose, skipped capabilities need an accepted report outcome, and final diagnostics must drain before the runtime ownership lock is released. These are compatibility repairs within the approved scope; the existing integration assertions remain authoritative.

- 2026-10-04 [review] A synthetic twelve-writer report burst lost eleven reports because the first busy guard immediately failed publication. The first repair retried report acquisition for at most 250 ms. The later retained-history stress review below superseded that bound and also repaired event admission. The subprocess test now exercises the production publication call directly, without a test-only retry loop.

- 2026-10-04 [review] Drew requested an independent Opus 5.5 extra-high review. It found eight issues despite the passing full gate: contention with retained history, protocol error prose, metadata command overhead, oversized CLI reservations, inherited raw-output handles, oldest-first bounded reads, rebuild finalization order, and invalid config losing an explicit opt-out. The accepted repairs preserve protocol output, prioritize recent bounded log tails, finalize rebuild reports before runtime restart, and retain disabled persistence on malformed settings. Sol 6.1 workers repaired and verified the storage and protocol paths; a fresh independent Opus review verified the integrated repair, with its two remaining minor findings repaired and checked afterwards.

- 2026-10-04 [verification] Final repaired source passed `make check-full`, including Go race tests, all 1,341 web tests, integration packages, mixed integration tests, and benchmark contracts. `rzm validate`, `rzm validate frozen-scope-drift`, and `rzm init --check` also pass. The rebuilt binary passed runtime-dispatched indexing, trace correlation, shutdown, query-payload exclusion, and unchanged offline reading with broken configuration. The smoke selects the explicit index attempt from history because startup catch-up may legitimately complete afterwards and replace latest-index.
- 2026-10-04 [review] Retained-history storage now uses bounded two-second guard acquisition, 64 KiB CLI reservations, and cheap scans of cleanly closed segments. Twelve simultaneous processes retain every start and report against 4,000 retained files; the full diagnostics package passed twice with the race detector. Fast healthy searches retain events; detailed reports cover at least 250 ms, failures, cancellation, panic, and degraded or fallback outcomes. Search selection passed an independent real-service fallback reproduction.

- 2026-10-04 [measurement] The matched comparison after desktop integration rotated baseline/enabled/disabled order across seven trials, each with a fresh 250-note, 25-Go-file vault and embeddings disabled. All arms produced identical indexed counts: 25 files, 25 symbols, 250 notes, and 500 graph document edges. Paired median persistence overhead was 31.6 ms cold, 24.5 ms unchanged, 24.9 ms after a note edit, and 25.2 ms after a code edit. This measures local small-vault instrumentation cost, not network/provider work or an indexing speedup. Enabled cold runs ranged from 401 to 456 ms; machine activity and filesystem caches remain sources of variation. Those rotated results superseded the earlier isolated baseline figures; the final review-repair measurements below supersede this snapshot.

Final median wall time after all review repairs, in milliseconds:

| Workload | Baseline | Candidate enabled | Candidate disabled |
| --- | ---: | ---: | ---: |
| Cold index | 400.9 | 433.3 | 390.4 |
| Unchanged index | 98.9 | 126.3 | 99.0 |
| One note edit | 161.2 | 193.9 | 157.5 |
| One code edit | 110.0 | 145.4 | 113.7 |

The workload runs `index --in-process` on 250 linked Markdown notes and 25 Go files, then repeats unchanged, replaces one note, and changes one Go function. Baseline uses shared-pipeline main commit `e496f8d1`; enabled and disabled use the same rebuilt candidate after integrating the shared live indexing pipeline and applying all review repairs. Enabled-versus-disabled comparisons isolate persistence cost, with the same indexing implementation in all three arms. Each trial has fresh content and database state; the three binaries/settings rotate order between trials. No embedding requests occur. Indexed table counts are compared after all four steps.

- 2026-10-04 [review] Opus verified closure of its eight original findings using real CLI/runtime probes, including 48 simultaneous processes with no lost starts or reports. Two additional nonblocking findings were repaired: fair bounded log-tail allocation prevents an active runtime file from consuming every byte, and a read-only missing code index is INFO skipped/index_missing. Focused reader race tests and a real fresh-vault CLI probe pass.
- 2026-10-04 [integration] Merged desktop release commit `ed373c25` from main. Preserved shared repository executable authority, runtime UI keep-alive, and probe redirect protection. Offline diagnostics and the global desktop handoff avoid new vault resolution or writes. Focused integration review found no diagnostics issues.
- 2026-10-04 [identifiers] The desktop merge independently used SPEC-0113. Rhizome allocated SPEC-0114 and its deterministic reconciliation plan kept the desktop identifier. The repair apply step could not materialize an authored governing-specs link range, so the parent applied that exact mapping to this new spec, its alias, and its explicit references. No note path or requirement changed. Validation confirmed zero identifier collisions or broken links.

- 2026-10-04 [final verification] After merging main and repairing the final two findings, `make check-full`, the rebuilt-binary runtime/offline smoke, fresh-vault missing-index protocol probe, `rzm validate`, `rzm validate frozen-scope-drift`, and `rzm init --check` all pass. The final matched measurements above ran after tests and reviews finished.

- 2026-10-04 [merge authorization] Drew subsequently requested: "greploop to 5/5 then squash merge." This authorizes review repairs and squash merge after the current-head review and required checks pass.
- 2026-10-04 [integration] Integrated main commit `49edf106`, including runtime-backed code reads and script inputs. The stdio request boundary encloses both runtime and local reads; watcher freshness event counts and diagnostic resync reasons are both preserved.
- 2026-10-04 [Greptile review] The initial review scored 4/5 with two retention/recovery findings. Latest replacement now publishes the protected copy first and reserves the actual peak bytes and one additional slot. Reading by operation ID falls back to a matching protected copy after damaged or mismatched history, while retaining the original history error when recovery is unavailable.

- 2026-10-04 [review verification] After the main code-mode integration and Greptile repairs, `make check-full` passed, including the complete Go race suite, all 1,341 web tests, integration suites, and benchmark contracts. The rebuilt runtime/offline smoke retained 62 correlated events with query payload excluded and no offline file changes. Documentation, frozen-scope, and generated-guidance checks passed. New report admission/recovery and runtime structured-failure regressions pass; Windows diagnostics tests cross-compile, with native Windows execution still untested. The earlier concurrent build/test attempt was canceled after asset-rebuild races and sandbox identity/listener denials; the successful full gate ran after the completed build outside the sandbox.

- 2026-10-04 [final measurement] Repeated the same seven rotated trials after all review repairs and the successful full gate. All indexed counts still match. Paired median persistence overhead is 21.0 ms cold, 31.2 ms unchanged, 36.4 ms after a note edit, and 32.3 ms after a code edit. Enabled cold runs ranged from 416 to 590 ms, showing substantial machine/cache variation; paired medians differ from differences between the arm medians. Those results preceded the shared live indexing integration and are superseded by the table above. These figures measure local instrumentation overhead, not indexing speedup or provider work.

- 2026-10-04 [pipeline integration] Integrated main’s shared structural writer and derived scheduler (`b1d58b5d`) plus typed-read freshness repair (`e496f8d1`). Measurements now follow the owning core/writer phases and finite derived tickets. Shared embedding provider batches publish one bounded runtime-lifetime summary after node drain; an active or crashed runtime may lack that final summary, so absence is unknown coverage. This preserves main’s scheduling and avoids attributing a physical batch to one ticket.
- 2026-10-04 [pipeline review] Independent review found and verified repairs for an unbound shared provider collector and lost preemption causes. A final review confirmed that obsolete work is quiet INFO `skipped` while original handle errors, terminal outcomes, retry decisions, and scheduling remain intact. Three-run focused race tests covered real HTTP retry/backoff/dedupe retention, derived source fencing, preemption, shutdown, typed-read freshness, and protected latest-index evidence.

- 2026-10-04 [integrated verification] `make check-full` passed on the shared-pipeline integration, including the complete Go race suite, all 1,344 web tests, integration suites, and benchmark contracts. The rebuilt runtime/offline smoke retained 151 correlated events, excluded query payloads, and changed no files during offline reads with broken configuration. Documentation validation, frozen-scope validation, and generated-guidance checks passed.
- 2026-10-04 [integrated measurement] Repeated seven rotated trials against a clean archived build of shared-pipeline main (`e496f8d1`) after all gates completed. All indexed counts match. Paired median persistence overhead was 36.7 ms cold, 26.8 ms unchanged, 27.2 ms after a note edit, and 24.3 ms after a code edit. Enabled cold runs ranged from 399 to 512 ms. The table above records the arm medians; paired medians differ from differences between those medians. This fixture measures local instrumentation cost with embeddings disabled.

- 2026-10-04 [main integration] Main’s view-preferences change (`9264ce09`) landed during final integration. Preserved both HTTP server imports and both changelog entries. The measured indexing implementation is unchanged; the performance baseline remains the shared-pipeline commit stated above.

- 2026-10-04 [identifier integration] The newly merged view-preferences spec independently used SPEC-0114. The live allocator supplied SPEC-0116 for this conversation’s diagnostics spec; updated its identifier, alias, and explicit references, preserving main’s spec and every requirement. The pre-commit reconciliation preview lacked provenance for the incoming merge, so it could not establish a historical keeper.

- 2026-10-04 [final merged verification] After integrating `9264ce09`, `make build` and `make check-full` passed, including all Go race tests, 1,420 web tests, integration suites, and benchmark contracts. The rebuilt runtime/offline smoke retained 176 correlated events and confirmed query-payload exclusion and unchanged broken-config offline reads. Final documentation, frozen-scope, and generated-guidance checks passed after identifier reconciliation.

- 2026-10-04 [final CI review] Greptile scored head `6c2b4a0f` 5/5 and confirmed both original findings resolved. Windows CI found a Unix-only sink-failure fixture that renamed an open log directory; replaced the setup with a portable real closed-handle failure while retaining the pipe-drain assertion. The review’s additional unbounded `--input-file` read was reproduced with oversized whitespace input and bounded to the existing 1 MiB script-input budget before trimming or parsing.

- 2026-10-04 [repair verification] The portable sink-failure regression passed ten race runs and Windows CGO cross-compilation. A temporary Go overlay removing the discard drain caused the repaired test to fail with blocked process output, confirming the regression remains effective. Bounded JSON input tests passed with inline/file exact-limit, oversized JSON, and oversized whitespace cases. The rebuilt CLI preserved one JSON error on its established error stream for rejected input and the exact successful result for valid file input. Independent review found no concerns. `make check` passed, including all 1,420 web tests; documentation and frozen-scope validation passed.

## Deviations

- SPEC-0104's headless log location is reconciled with SPEC-0116. The active reboot-recovery effort acknowledges this documentation change; its frozen lock-recovery contract is unchanged.

## Closure Checklist

- [x] Required quality gates pass.
- [x] Alignment and actual outcomes are verified.
- [x] Specs and documentation are reconciled.
- [x] Follow-ups are triaged.

## Compounding Follow-ups

Use the resulting diagnostic evidence to scope fallback/resync and embedding improvements separately, as Drew requested.

## Status

Complete. Implemented and verified the approved phases, resolved independent review findings, and opened [PR #79](https://github.com/atomicobject/rhizome/pull/79). Drew subsequently authorized Greptile review to 5/5 and squash merge; release remains separate.
