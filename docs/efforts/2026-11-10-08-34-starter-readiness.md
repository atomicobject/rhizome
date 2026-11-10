---
type: EffortNote
id: EFF-2026-11-10-08-34
name: Starter readiness and recovery
created-at: 2026-11-10T13:34:58Z
status: active
summary: Make installed starters complete their setup and indexing with visible readiness and recovery, and refresh ontology data after folder-only changes.
aliases: [EFF-2026-11-10-08-34]
governing-specs:
  - "[[starter-readiness]]"
---

# Starter readiness and recovery

## Scope

Deliver [[starter-readiness|SPEC-0137]] using the existing module-action and runtime-indexing owners. The motivating incident is an AO KB installation in `agentic-process` that collided with an existing lowercase schema and exposed no complete setup or recovery flow. Existing per-file automatic ontology refresh remains in place.

## Spec Set (Frozen)

- [[starter-readiness|SPEC-0137]], initial revision on this branch dated 2026-11-10, all requirements and US1 through US3.

## Stories In Scope (Frozen)

- [[starter-readiness#^SPEC-0137-US1]]
- [[starter-readiness#^SPEC-0137-US2]]
- [[starter-readiness#^SPEC-0137-US3]]

## Spec Coverage Checklist

- [x] Shared readiness and automatic eligible setup, including CLI installation and runtime reopen.
- [x] Persistent visible status, concrete recovery actions, conflict and input handling.
- [x] Ontology folder-only event refresh and client invalidation.
- [ ] Regression tests, required gates, and real fixture UI verification.

## Plan

1. Compare two Sol design sketches against one readiness truth, reuse of existing execution owners, unambiguous automatic work, and bounded recovery. Record the selected contract before implementation.
2. Implement and test the independent ontology-folder watcher fix in an isolated worktree.
3. Implement the shared starter readiness/setup owner and AO KB integration, then its desktop/API consumers. Reuse action trust and the existing index lane. Keep authored content unchanged.
4. Verify focused behavior, full repository gates, generated starter consistency, and the visible flow in a packaged fixture app. Independently review the integrated change and resolve accepted findings.
5. Record delivery evidence, deviations, and closure on the feature branch. Do not install the unmerged branch over Drew's Rhizome Dev app.

Throughput checkpoint: design and the shared contract precede implementation; watcher hardening is independent; writers use isolated worktrees; one owner holds the coupled readiness implementation to avoid competing state models.

## Original Intended Delivery

A starter installation reaches ready automatically when possible, otherwise remains visibly blocked with an appropriate recovery action. Schema conflicts do not look like empty content. Whole ontology folder changes refresh typed data.

## Actual Delivered

US1 and US2 are implemented in the shared runtime coordinator, installed AO lifecycle actions, public HTTP recovery API, and persistent shell notice. Parent-owned integrated verification, packaged-app validation, and independent review remain pending. The effort remains active.

## Execution Notes

- 2026-11-10T13:34:58Z: Drew approved the preceding recommendation with "OK. Make the changes. Use sol". All delegated design, implementation, and review use GPT-6.1 Sol. The current user is not configured in this checkout, so approval is recorded here and `plan-approved-by` remains absent.
- Existing unrelated changes in `AGENTS.md` and `pkg/app/web/assets/dist/index.html` remain untouched.
- Grounding verified that individual `.graphql` changes already force ontology republication. The reported `artifact` collision occurs while building the executable query schema, which must be checked separately from authored SDL validity.
- The Sol watcher implementation was reviewed and integrated as `323462c5`. Watch admission and change classification now share `assetroots.IsAssetRootOrAncestor`; the full live ownership test checks typed records and client invalidation after create, remove, and rename events for each ontology ancestor. The worker passed race-enabled tests and vet for assetroots, watchhub, and bootstrap, plus the file-size gate. Full integrated verification remains pending.
- Integrated verification: the folder regression passes with host permissions. The sandboxed run cannot acquire the macOS process-identity-backed lane lease; this is an environment constraint, not a flaky pass.
- Design decision: use a runtime-owned readiness flow declared by a module and executed through existing actions. Its states distinguish checking, working, required input, blocked, failed, and ready. Keep initial indexing and starter readiness separate. Reuse the runtime's existing index owner, reconstruct or persist unfinished work, and reject stale results. Reject a desktop-only checklist because it misses CLI installs and reopened repositories. The second Sol sketch remains an implementation review input.
- Both Sol sketches converged on module-owned typed assessments and a thin runtime adapter. Adopt installed descriptors and a small failure receipt, not a general workflow engine. The coordinator must run actions outside the index lane and use the selected executable explicitly. The implementation owner received this synthesis.
- Integration review found that `subscribeWatchHub` reduced directory events to resync before classification. Preserve ontology ancestor paths through that handler and strengthen the live ownership regression to feed `WatchEvent{IsDir:true}`. The focused directory and retry tests pass with host permissions.
- `make check` passed after the directory-event handoff correction, including Go tests, static analysis, web tests, type checks, content validation, and credential checks. Full runtime and packaged UI gates remain for the integrated readiness implementation.

## Deviations

None.

## Compounding Follow-ups

None identified yet.

## Closure Checklist

- [ ] Approved scope delivered or deviations recorded.
- [ ] Required gates and UI evidence recorded.
- [ ] Independent review completed and accepted findings fixed.
- [ ] Delivery branch and commit recorded.

## Status

Active on `feat/starter-readiness`.

## Implementation work log

- Design sketch for US1 and US2: a separate runtime-owned module coordinator consumes an optional installed lifecycle descriptor and typed module assessments. Extending startup readiness would be smaller initially, but would couple a monotonic startup gate to repeatable per-module recovery and miss installations after startup. The module owner instead serializes action execution outside the writer lane, fingerprints bounded declared inputs, rejects stale results, and retains terminal failure receipts under ignored local state. Existing actions retain trust and cancellation; the AO index action invokes the normal runtime index command and verifies its receipt. The public HTTP snapshot and shell notice share this owner, including when GraphQL is unavailable.
- Throughput checkpoint: implement the descriptor/coordinator and its boundary regressions first, then the AO assessment/actions and safe include helper, then HTTP and persistent shell recovery, followed by focused and fast gates. Parent retains integrated full, packaged-app, and independent-review gates. No effort completion is claimed here.
- Retrieval gap: compounding lookup ran before exploration but the semantic query failed because this worktree has no index database. Checked-in source and owning notes provide implementation evidence.

- US1/US2 delivery: optional installed lifecycle declarations drive one runtime worker outside the index lane. Typed assessments and recovery replace file-presence assumptions. Fingerprinted receipts retain unchanged failures; reopening rechecks ready modules against current queries and index evidence. AO owns product choices and uses existing compilation and index receipts. The shared action runner pins command and script subprocesses to the selected executable.
- Recovery delivery: a persistent shell notice consumes the same non-GraphQL snapshot after Settings or CLI installation. It reports source/schema/dependency reasons, offers product selection and Retry, and opens the current repository Settings through the verified desktop page bridge. Existing authored schemas and knowledge are preserved. Exact source includes and untouched managed guidance are refreshed through init-owned helpers.
- Verification: `make check` passed, including Go unit packages and 270 web test files / 2157 tests. `make check-fast` passed after regenerating the subsystem-derived Greptile configuration. Focused race tests passed for the coordinator, descriptor fingerprinting, cancellation/coalescing, and include/guidance helpers. The desktop `npm run check` passed 163 tests, and its added recovery-to-Settings regression passed in the 64-test App suite.
- Real fixture: `go test -tags 'fts5 integration' ./tests/integration/aokb -run TestInstalledStarterAutomatically -v` passed. An already-open runtime discovered CLI installation, selected single-product setup, verified an empty knowledge base, and reopened without rewriting the index receipt. `playwright test tests/e2e/starter-readiness.spec.ts --project workspace` passed the visible selection, failure, navigation/reload, and retry control path against controlled API states in the real shell. Packaged native and schemaVersion 8 dependency-backed journeys remain with the parent validation owner.
- Generated surfaces: `make build` passed; built-CLI `init --add-addon ao-kb --agents none --binary-manager external` followed by `init --check --agents none` in a disposable configured fixture reported everything up to date. Built-CLI default validation and `validate frozen-scope-drift` both passed with zero issues and errors.
- Verification corrections: sandboxed runtime tests could not read macOS process lock identity and were rerun with host permissions. A final path-helper change exposed macOS root aliases in the cancellation regression; the fingerprint owner now canonicalizes its root before converting paths, and the test bounds action startup. Raw AO evidence archives are excluded from readiness fingerprints to keep unrelated recordings outside the bounded input budget.
- Remaining gates: parent integrates the commits, runs full concurrency/integration/native/packaged checks, performs independent review, and records final closure. No PR, merge, or Rhizome Dev installation was performed by this implementation leaf.
- Final source verification: all six affected Go packages passed with host permissions, including the added AO script and schema regressions. The final `make check-fast`, source-only rebuild after the root-alias correction, and `cargo fmt --check` passed. The final targeted race suite passed for module readiness, descriptors, and init helpers. Build-generated web index HTML was restored to its pre-task bytes; source and generated API types carry the implementation.
