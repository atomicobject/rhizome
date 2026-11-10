---
type: EffortNote
id: EFF-2026-11-10-08-34
name: Starter readiness and recovery
created-at: 2026-11-10T13:34:58Z
status: complete
summary: Make installed starters complete their setup and indexing with visible readiness and recovery, and refresh ontology data after folder-only changes.
aliases: [EFF-2026-11-10-08-34]
governing-specs:
  - "[[starter-readiness]]"
---

# Starter readiness and recovery

## Scope

Deliver [[starter-readiness|SPEC-0138]] using the existing module-action and runtime-indexing owners. The motivating incident is an AO KB installation in `agentic-process` that collided with an existing lowercase schema and exposed no complete setup or recovery flow. Existing per-file automatic ontology refresh remains in place.

## Spec Set (Frozen)

- [[starter-readiness|SPEC-0138]], initial revision on this branch dated 2026-11-10, all requirements and US1 through US3.

## Stories In Scope (Frozen)

- [[starter-readiness#^SPEC-0138-US1]]
- [[starter-readiness#^SPEC-0138-US2]]
- [[starter-readiness#^SPEC-0138-US3]]

## Spec Coverage Checklist

- [x] Shared readiness and automatic eligible setup, including CLI installation and runtime reopen.
- [x] Persistent visible status, concrete recovery actions, conflict and input handling.
- [x] Ontology folder-only event refresh and client invalidation.
- [x] Regression tests, required gates, and real fixture UI verification.

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

All three stories are implemented. Installed starters reconcile setup and indexing through the runtime and expose persistent recovery through one HTTP/UI contract. AO KB handles product selection, schema conflicts, dependencies, source admission, interrupted work, and unchanged reopen. Folder-only ontology changes refresh typed data. The shell uses the compact notice reviewed and implemented by Opus. Full source, browser, and packaged-app verification passed, and the final implementation review returned 5/5 with no remaining findings.

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

- [x] Approved scope delivered or deviations recorded.
- [x] Required gates and UI evidence recorded.
- [x] Independent review completed and accepted findings fixed.
- [x] Delivery branch and commit recorded.

## Status

Complete on `feat/starter-readiness`. [PR #125](https://github.com/atomicobject/rhizome/pull/125) carries the delivery. Implementation through `42a67c3e7d93a26fc20458eb15e08fd65524ea6c` passed the final review. Drew authorized squash merge after the required checks pass. Rhizome Dev installation remains a separate release action.

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
- Independent Sol correctness review found four defects: populated knowledge verification used the wrong node-batch shape, failures during input changes could persist against the new inputs, ignore files did not invalidate readiness, and killed binding actions left an unrecoverable lock. All four were corrected. The real query uses `items`, explicit batch size, per-item errors, and `pageInfo.truncated`. The runtime reassesses changed inputs and observes all three repository ignore files. Binding lock ownership is published atomically, and only the elected serial lifecycle runner may reclaim a stopped owner. Live owners and unknown legacy locks remain protected.
- The init include helper now accepts a declared source before its file exists while rejecting paths through an external symlink. This preserves the default Markdown include and permits compiler-declared optional sources without creating them.
- Corrected runtime verification passed with `GOCACHE=$PWD/.gocache go test -mod=vendor -tags 'fts5 integration' ./tests/integration/aokb -run 'TestInstalledStarterAutomatically|TestAOKBLegacyKnowledgeOnlySetup' -count=1 -v`. It covers real populated batches above 100 notes, empty setup, CLI install while open, unchanged reopen, and automatic failure/recovery after each of `.rhizome/ignore`, `.obsidianignore`, and `.gitignore` changes. Node regressions also exercise a real killed lock owner and refusal to reclaim a live owner. Full integrated and packaged checks follow this correction.
- The first full verification stopped at the existing format-neutral architecture gate. The include helper's Markdown branch now consumes the live runtime's immutable registry and each provider's ownership policy. Focused architecture and admission regressions passed, including broad Markdown admission without activating YAML accidentally. Full verification was restarted against this correction.
- The fresh Sol review identified a duplicate JavaScript include check that rejected valid brace patterns. Source admission now has one owner: the runtime invokes init's format-aware helper before accepting ready or indexing. AO assessments supply source paths without interpreting include patterns. A new coordinator regression proves changed admission indexes before ready; Node and Go regressions cover `*.{yaml,yml}` without a redundant include or retry loop. The second full run passed the race-enabled Go suite before being stopped to apply this correction.
- Packaged-app verification found that trust recovery opened Settings without offering a trust action for a configured worktree using an external executable. Repository Settings now exposes the existing worktree-scoped trust command explicitly; it does not grant trust merely by opening Settings. Its regression passed with the full desktop frontend suite (165 tests). The executable-schema regression also now uses valid node path declarations and asserts the generated-root collision, separately from authored SDL failure.
- Following the trust button through the real bridge exposed a second issue: its existing trust operation did nothing when the selected executable required no checkout trust. The explicit operation now records the selected worktree's trust for module actions as well, while opening a repository with an external/global executable remains independent of action trust. Focused desktop-service tests passed.
- Drew rejected the readiness banner's visual design and requested Opus review and improvement. An isolated Claude Opus 5.5 design pass owns the readiness component and styling, with the supplied screenshot and the existing design system as evidence. Backend verification continues independently; final UI acceptance will use the integrated redesign.

- Full parent verification passed with `make build` and `make check-full`, including race-enabled Go tests, all 2157 web tests, all integration packages, and mixed integration tests. The new main commit `cd607d89` allocated SPEC-0137 concurrently; this effort's new specification was mechanically renumbered to SPEC-0138 before integration, with its frozen story references updated and scope unchanged.
- The delayed maintainability review repeated the already-fixed admission and ignore findings and identified a guidance preflight gap. Source-include updates now reuse init's prepared file plan instead of writing managed blocks sequentially. The regression proves malformed CLAUDE.md leaves configuration, AGENTS.md, CLAUDE.md, and generated ownership records unchanged, and that repairing the fence then updates both blocks with matching records. Focused tests passed.
- Integrated the requested Opus design pass (`378ed048`, carried here as `d9c40f27`): a compact themed notice, existing status marks and controls, one main recovery action, and expandable diagnostics. Parent applied its desktop recommendations: no duplicate trust control, an inline trusted/untrusted state, dedicated Trusting progress, and a disabled pending trust action. The desktop frontend suite passed all 166 tests. Native and final combined verification follow integration.
- Final automated verification: the build, race-enabled Go suite, all 2172 web unit tests, all integration packages, and mixed integration tests passed in `make verify`. Its Chromium/HTML browser projects passed 147 tests. The WebKit project initially could not start because its browser binary was absent. After `npx playwright install webkit`, `RHIZOME_E2E_SKIP_WEB_BUILD=1 npx playwright test --project=webkit` passed all 14 tests, including the starter recovery journey added to that project. This was a missing test prerequisite, not a test retry or timeout change.
- Native verification: `make desktop-deps`, `make desktop-check`, and `make desktop-build` passed on macOS arm64; the native suite passed 145 tests and the final desktop frontend suite passed 166 tests. A separately launched `Rhizome.app` used isolated app data and a disposable legacy AO KB. Installing the add-on through Settings showed the compact trust notice. Its Settings action recorded worktree trust and automatically advanced to the `artifact`/`Artifact` schema conflict. The notice survived navigation and reload, Details showed the schema paths, Escape returned focus, and Retry kept the unresolved conflict visible. Renaming only the fixture's conflicting type resumed setup and indexing without a command. The overview loaded 43 assertions, 4 concepts, 7 sources, and 5 artifacts. Stopping the runtime and reopening the app restored ready state with the same data and an unchanged index-receipt timestamp. Drew's installed Rhizome Dev app and actual KB were not replaced by the test build.
- Generated surfaces and documentation: built-CLI `init --workflow none --add-addon ao-kb --agents none --binary-manager external`, then `init --check --agents none`, reported everything up to date in a disposable repository. `./scripts/rzm validate` checked 633 identifiers with zero issues and errors; `./scripts/rzm validate frozen-scope-drift` also passed with no drift. The original unrelated `AGENTS.md` and generated index HTML changes remain preserved.
- The committed-branch Greptile review identified three further issues in automatic setup: receipt writes followed repository-supplied symlinks, descriptor reads allowed unbounded or blocking files before trust, and background include reconciliation could overwrite a concurrent Settings save. Receipt reads and publication now use bounded, repository-confined handles and exclusive temporary files. Descriptor and fingerprint reads reject non-regular files and enforce byte limits. Include reconciliation now loads and edits under the ordinary configuration writer's cross-process lock, preserving unchanged authored bytes. Focused race regressions passed for unsafe receipt paths, oversized/symlinked/FIFO descriptors, concurrent Settings saves, and the existing include/guidance behavior.
- Final review fixes shipped in `42a67c3e`. `make check-full` passed again after those changes, including the Go race suite, 2172 web tests, all integration packages, and mixed integration tests. `make desktop-build` rebuilt the packaged app; cold reopening the same isolated fixture restored 43 assertions, 4 concepts, 7 sources, and 5 artifacts with no setup notice and an unchanged index receipt. `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` passed with zero issues and errors. `greptile review --json --branch cd607d89473015aa92f4b2e49c71da6655e62ed0` returned confidence 5/5, no comments, and confirmed all three prior findings resolved.
