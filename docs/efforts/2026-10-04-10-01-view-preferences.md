---
type: EffortNote
id: EFF-2026-10-04-10-01
aliases: [EFF-2026-10-04-10-01]
name: Persistent view preferences
created-at: 2026-10-04T14:01:47Z
status: active
summary: "Deliver instance-scoped SQLite preferences, native and custom view persistence, clear shared-save controls, updated authoring guidance, and a reviewed PR with Greptile 5/5."
governing-specs: ["[[view-preferences]]"]
---

# Persistent view preferences

## Scope

Deliver [[view-preferences|SPEC-0114]] using GPT-6.1-Sol for storage, client services, tests, and guidance; Opus 5.5 at high effort owns UI changes and the independent post-build review. Open a PR and address CI and Greptile findings until the current head has 5/5 with no unresolved actionable findings. Drew expanded this request on 2026-10-04 to seed user settings through `new-worktree` and squash merge when verification passes. Release publication remains outside this request.

## Spec Set (Frozen)

- [[view-preferences|SPEC-0114]], initial 2026-10-04 revision, all requirements. The specification records the evaluation and implementation plan approved in this conversation.

## Stories In Scope (Frozen)

Requirements-only scope. All storage, identity, interaction, authoring, and verification obligations in SPEC-0114 are included.

## Spec Coverage Checklist

- [x] Separate ignored store, schema migration, atomic updates, reset and legacy-import semantics.
- [x] Shared typed scope, native and kit APIs, hydration, synchronization, and visible failures.
- [x] Native and bundled controls remember preferences independently per instance.
- [x] Shared configuration save is explicit and preserves personal-only settings.
- [x] Canonical authoring guidance, examples, and generated surfaces agree.
- [ ] Integrated tests, browser evidence, independent review, and PR review loop complete.

## Plan

| Batch | Outcome and ownership | Dependencies | Exit evidence |
| --- | --- | --- | --- |
| 1. Contract and durable service | Parent records scope; Sol implements `pkg/app/userstate`, SQLite migration, REST contract, server lifecycle, and focused tests. | Existing view and storage contracts audited. | Concurrent updates preserve unrelated keys; reset and import survive reopen; API validation tests pass. |
| 2. Shared client and kit | Sol implements scoped preferences, native state/layout adapters, target selection, browser migration, public React/HTML APIs, and focused client tests. | Batch 1 DTO contract; implementation can proceed in parallel against contract fixtures. | Hydration, isolation, optimistic changes, errors, synchronization, and migration tests pass. |
| 3. View integration and controls | Opus integrates Table/Cards/Board, Trace/Briefing, Overview, reset, and explicit shared Save. | Batch 2 hook shape; parent integrates dependency patches before verification. | Existing behavior and new persistence tests pass; actual interface shows persistence and shared-save semantics. |
| 4. Guidance and integration | Sol updates canonical authoring guidance and examples; parent reconciles owning docs, regenerates skills, and integrates all lanes. | Public API and final UI behavior. | `make check-fast`, `make check`, `make check-full`, `make web-e2e`, `make build`, `rzm init`, `rzm init --check`, validation and frozen-scope drift checks. |
| 5. Review and delivery | Independent Opus 5.5 high review after build; parent and assigned workers repair findings, open/link PR, and use the app watcher for CI and Greptile. | Integrated implementation and gate evidence. | Current-head review findings addressed; CI green; Greptile 5/5; no unresolved actionable comments. |

Workers use isolated worktrees and exclusive file ownership. The parent integrates staged patches and runs the full gates once the combined result is coherent. The slowest dependencies are the common scope/API shape and UI integration; backend and client implementation run in parallel after the shape is fixed. Avoid redundant full builds in worker lanes. Rough scope is one new Go store/domain and REST surface, one shared client service, native and kit adapters, the existing interactive views, and focused documentation/tests.

Design alternatives considered: per-key rows with atomic patches, or whole-instance JSON documents with compare-and-swap. Per-key rows were selected because independent settings can change without replacing a stale full document. Revisions still fence reset and conflicting writes. The Model the Domain principle led to an explicit invocation scope; Separate Before Serializing Shared State led to per-key mutations and isolated worker worktrees.

### Authorization

Drew approved the evaluation's implementation plan in chat on 2026-10-04: "This sounds good," followed by instructions to build with GPT-6.1-Sol subagents, use Opus 5.5 high for UI and review, open a PR, and watch/greploop to 5/5. He then said "continue." This approval covers routine implementation choices, integration, verification, and review repairs. No current user is configured in this checkout; `plan-approved-by` is omitted rather than inferred.

Escalate only for a material change to that scope or an unauthorized destructive action. Settled storage and personal/shared configuration decisions need no new approval. Review of the formative contract is part of batch 1 and does not create another human checkpoint.

## Original Intended Delivery

Personal view interactions survive reloads and application restarts without Git churn. Separate instances remain independent. Shared structure is intentionally saved to YAML. Custom-view authors get the same behavior through the public kit and shipped guidance. Deliver the reviewed change as an open PR at Greptile 5/5.

## Actual Delivered

Implementation is integrated and verified through all 92 browser tests. Full local code gates, the build, generated guidance, and independent Opus review pass. Final pre-commit verification passes; PR delivery remains in progress.

## Deviations

None.

## Closure Checklist

- [ ] Frozen acceptance obligations verified against the integrated result.
- [x] Required gates and live UI verification recorded.
- [x] Independent Opus review findings resolved.
- [x] Docs and generated guidance reconciled.
- [ ] PR opened, linked, and current-head CI/Greptile requirements met.

## Compounding Follow-ups

None identified yet.

## Status

Active implementation on `t3code/view-settings-evaluation`, starting from `0839691`. No unrelated changes were present before the initial build. The build regenerated the tracked asset entry; it will be handled with the final build output.

## Execution Notes

- 2026-10-04T14:01:47Z: Built the checkout to restore the repository launcher. Rhizome session `utjoLAWwG7sxtzV3` is working; current-user discovery reports no configured identity. Live authoring guidance and identifier allocation originally supplied SPEC-0113 (renumbered to SPEC-0114 after merging main) and EFF-2026-10-04-10-01.
- 2026-10-04T14:01:47Z: Two Sol design sketches compared per-key storage with a whole-document alternative. Selected per-key patches, explicit revisions, reset tombstones, and one-time legacy-source claims. Backend and client workers own separate worktrees; Opus completed the initial UI inventory.
- 2026-10-04: The initial shared client/kit foundation typechecked in its worker checkout and was integrated into the parent and UI checkouts. The canonical authoring guidance passed skill metadata, example transform, frontmatter, link, and fence checks before integration.
- 2026-10-04: Baseline `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` passed with zero findings before the combined implementation. These establish a clean documentation baseline, not final integration evidence.
- 2026-10-04: Parent review found that closing migration after the first imported source would discard a second legacy layout/query source. The store now permits distinct claimed sources to fill missing keys until the first personal patch or reset closes migration. Import never overwrites existing keys.
- 2026-10-04: Live baseline browser check reproduced lost expansion in the synthetic TvGuide Table. Collapsing Howto changed its `aria-expanded` to false and visible rows from 5 to 3; navigating to the same URL restored true and 5 rows. Collaborative preview reaches this fixture through the machine's LAN origin, with matching `--application-origin`; loopback addresses referred to the preview client instead of the server.
- 2026-10-04: Integrated backend, generated OpenAPI types, authoring guidance, and six new browser journeys. Backend worker passed normal userstate/migration/web tests, userstate/migration race tests, focused web race tests, vet, and OpenAPI validation. `.rhizome/user-state.sqlite` and its WAL/SHM/init-lock sidecars are ignored by Git.
- 2026-10-04: Parent `make build`, `./scripts/rzm init`, and `./scripts/rzm init --check` passed after backend/client-foundation integration. The managed Claude/Codex skill copies and hashes were regenerated from canonical templates. A final build remains after UI integration.
- 2026-10-04: Pre-review comment/style pass identified a hooks dependency suppression and unnecessary validator casts. Owners are removing those while preserving public API documentation. Backend audit also confirmed that a removed filter field may silently hide rows; native restoration will validate against capabilities from a successful default execution, never clear preferences on an ambiguous data-load error.
- 2026-10-04: Integrated documentation validation passed. Frozen-scope validation initially identified active EFF-2026-10-03-18-33's SPEC-0112 binding; its Deviations section now records this effort's superseding persistence contract. The focused frozen-scope check then passed. No closed effort was changed.
- 2026-10-04: Final client/kit lane integrated. Worker `npm run typecheck`, owned-path oxlint/oxfmt, and eight focused test files passed (52 tests), covering delayed hydration, multi-source and late imports, reset tombstones, target selection, null-vault mount-local controls, cross-vault events, obsolete-field sanitization, and invalid functional updates after a concurrent rebase. Shared test harness keeps preference state isolated while preserving strict unmatched-route failures.
- 2026-10-04: Integrated UI build passed. The first combined web unit run passed 1,374 tests and exposed two outdated test fixtures: the legacy selection assertion still expected browser storage, and the restored-edit test answered staged reads with disk content. Both fixtures were corrected; their 14 focused tests pass.
- 2026-10-04: The first browser gate failed. Its teardown disposed the HTTP client before preference reset handlers completed, and production selection requests included empty type identities before a concrete subject was available. The fixture now drains handlers and preserves widget identity; selection identity and visible failure feedback are being repaired before rerunning the gate.
- 2026-10-04: Live integrated Guide table verification retained the collapsed Howto group and three visible rows after both a page reload and a server restart. No preference error appeared. This reproduces the same workload that lost expansion in the baseline.
- 2026-10-04: Host reset semantics now include persisted child widgets. Host slots and widget slots have distinct identity; an atomic family reset clears existing widgets and prevents legacy import into unseen widgets. Focused HTTP tests, storage race tests, migration tests, and reset/write contention tests pass in the backend lane.
- 2026-10-04: Independent Opus 5.5 high review found unsafe stale-field deletion from partial or placeholder capabilities, repeated baseline queries, optional-store startup failure stopping the app, selection rendering before hydration, terminal migration failure recovery, expansion byte bounds, replaced-store revision handling, generated-view preference carryover, and explicit default values in Overview. Each finding is assigned for repair or reproduction. The review inspected the family-reset change before its integration; its reset concerns are now covered by the integrated identity, tombstone, event, and pending-write tests. Final review remains pending.
- 2026-10-04: Review repairs are integrated. Cleanup now checks fresh schema-backed results once per instance and definition, preserves row-observed dotted fields and selectors in current shared configuration, and leaves failed probes intact. Query-count tests verify one baseline execution across invalidation and staged edits. Expansion values have a 16 KiB UTF-8 budget per key. Generated saves carry non-promoted preferences forward, and Overview removes choices that return to defaults.
- 2026-10-04: Preference-store startup failures preserve database bytes and other web features while returning sanitized 503 diagnostics. Terminal invalid imports retain browser evidence without blocking controls; authoritative reads can recover after database replacement without overwriting newer in-flight results. Selection waits for hydration, exposes retry, normalizes in-memory node references, and migrates legacy note choices only after acknowledgment. Focused normal/race storage tests, 44 client recovery tests, 59 integrated selection UI tests, and the final 21-test selection suite pass.
- 2026-10-04: `make check-fast` and `make check` passed; the latter included 1,408 web tests. After the final authored-alias regression, `make check-full` passed all Go race, integration, and benchmark contracts plus 1,409 web tests. `make build`, `./scripts/rzm init`, and `./scripts/rzm init --check` passed. Final documentation and frozen-scope validation both returned zero findings.
- 2026-10-04: The next browser run passed 84 cases and exposed an incorrect entry point in the new shared-save test: it opened a type-mounted view as standalone, which correctly had no type view switcher. The test now opens its collection and selects its authored Table. Its focused browser rerun passes; the complete 92-case browser gate is running again. Independent Opus post-build review round 2 is also running.

- 2026-10-04: Independent Opus post-build review round 2 verified all ten prior findings and reported no blocking findings. The complete browser gate passed 91/92 cases; its remaining failure, Source selection after saving untyped HTML metadata, matches the review's optional null-scope hardening concern. The client owner is reproducing the actual identity gap before PR publication.
- 2026-10-04: The browser trace confirmed untyped HTML returns an empty resolved type and no reference type. Selection now uses the existing fallback node identity for durable preferences; temporarily unavailable identities retain isolated local interaction. A new regression failed before the fix; 26 focused selection tests, typecheck, lint/format, and the formerly failing HTML browser case pass afterward. Parent inspected the final change and started the complete browser and pre-commit gates.
- 2026-10-04: The final complete `make web-e2e` gate passed all 92 browser tests, including untyped HTML Source selection after save and every new preference journey.
- 2026-10-04: Final `make check` passed, including all 1,411 web tests. The previous `make check-full` remains valid for unchanged Go concurrency and storage code; the final frontend-only fix has focused and full web coverage.
- 2026-10-04: Merged main after desktop PR #75 landed. Preserved both Unreleased changelog entries. Validation found an independently allocated SPEC-0113 collision; the live allocator supplied SPEC-0114 for view preferences, and its owned references were updated without changing the delivered contract.
- 2026-10-04: PR #77 opened at `3606a299` and was linked to the T3 watcher. Main's desktop merge was integrated as `7b18c5d7`; the merged tree passed `make check` with 1,411 web tests and zero documentation/frozen-scope findings. Greptile's first review scored 4/5 and identified missing native validation feedback and an omitted normalized sort in the shared-save review. Both are assigned for reproduction and repair before the next review.
- 2026-10-04: Greptile round 1 repairs are integrated. Native query, layout, and expansion validation errors now reach the visible status, and Reset remains available beside Retry. Invalid widths use the same positive-width guard as validation. Shared Save uses the authored or explicitly chosen sort for both its review and request, preserving authored normalization differences. Red-first UI and Save regressions failed before repair; 25 validation/expansion tests, 11 Save request tests, and typecheck/lint/format pass. A new real-API browser regression checks visible invalid-density recovery; combined full gates are running.
- 2026-10-04: All 93 browser cases passed after Greptile round 1 repairs, including the real-API invalid-density recovery regression. Running the Go gate alongside the browser rebuild caused temporary missing embedded assets; the check/build/validation chain is being rerun sequentially against the completed assets.
- 2026-10-04: Final sequential verification of Greptile round 1 repairs passed `make check` (1,417 web tests), `make build`, `rzm validate`, and `rzm validate frozen-scope-drift`; the complete browser run passed all 93 tests. The only intervening changes were lint-required whitespace in the new browser test. Both findings are ready for a new review.
- 2026-10-04: Greptile review 2 scored `8df17717` at 5/5 and confirmed both findings resolved. Windows CI exposed an unrelated test expectation from the merged desktop change: LaunchTarget returns the shared normalized slash path, while the test compared an OS-native path. Both expectations now use `filepath.ToSlash`; production behavior is unchanged. Final CI confirmation is pending this test-only repair.

- 2026-10-04: Drew authorized the worktree preference-seeding extension and squash merge when good. SPEC-0114 is refrozen with the one-time independent snapshot requirement (frozen-scope-drift acknowledged). The existing per-instance store shape remains unchanged. Seed through the user-state owner under its initialization lock, preserve existing destination state, and use the existing clone/snapshot machinery. Focused tests cover WAL content, independent edits, reset protection, existing/missing state, initialization cancellation, failed-copy cleanup, and command ordering. This is one small storage/CLI extension; the bottleneck is final CI, so focused verification precedes one full local gate.
- 2026-10-04: Worktree extension passed focused store/command tests, `make check` (1,417 web tests), `make build`, and both documentation validation selectors. An independent Sol review returned PASS with no blocking findings. The built CLI copied fixture preferences, preserved source state after a destination edit, and preserved that edit when run again. Full verification requires a fresh run after a test-name edit during compilation and integration of the newly advanced main.
- 2026-10-04: Integrated main after PR #76 landed. Preserved both additions in the changelog and store guidance. Verification exposed an upstream integration mismatch between PR #78 runtime read synchronization and PR #76 indexing: synchronization still referenced removed throttling state. A local repair passed focused race tests, but Drew identified the indexing thread as already owning this fix with an additional regression test. The duplicate patch is parked locally; integrate that thread's merged repair and verify the combined branch before squash merge.
- 2026-10-04: Windows rest CI additionally exposed Claude/Codex shutdown fixture failures under a synthetic 300 ms budget. Both tests now wait for the sleeping child's readiness frame and use the documented three-second shutdown default, retaining blocked pipe writes, the elapsed bound, and immediate child-exit assertions. Ten repeated race runs per package passed on macOS; both Windows test packages cross-compiled. Independent review returned PASS. Actual Windows execution remains a CI check after integrating PR #80.
- 2026-10-04: PR #80 landed as `e496f8d1` and is integrated. Preserved both changelog additions; the runtime repair also supplies the watcher formatting correction. The local duplicate runtime patch remains unused. Final build, full verification, documentation checks, and fresh PR #77 CI cover this integrated branch and the shutdown fixture repairs.
- 2026-10-04: Final integrated build and built-command preference preservation check passed. Both documentation selectors and `rzm init --check` passed. `make check-full` passed formatting, lint, type checks, credential checks, Go race tests, benchmark contracts, and all 1,420 web unit tests; integration phases are still running. Publish this integrated head for fresh Windows and browser CI before squash merge.
- 2026-10-04: Windows anchors/app CI on `85775ee7` exposed a fixture startup race: cache warmup could consume the initial discovery diff while leader work and polling were disabled, leaving no nodes or derived work. Explicitly warming the cache reproduced the exact failure twice on macOS (30.493 s and 30.442 s). The fixture now retains an initial full watcher resync after warming. Ten repeated workload/shared-provider race runs passed; Windows bootstrap tests cross-compiled with MinGW/CGO. Independent review returned PASS. No production behavior or latency limit changed; actual Windows execution remains a fresh CI gate.
- 2026-10-04: The workload fixture repair passed `make check` with all 1,420 web tests, plus documentation and frozen-scope validation. Publish the two-file test/evidence correction and require fresh Windows anchors/app CI before squash merge.
