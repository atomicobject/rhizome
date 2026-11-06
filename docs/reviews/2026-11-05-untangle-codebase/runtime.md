# Runtime, desktop, harness, and provider structural audit

Audit date: 2026-11-05. Base: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`.

Read-only audit. Both optional cleanup passes are selected. No production files were changed, no broad build or test gate was run, and no credentials were read. Compounding lookup could not prepare the configured development binary because `bin/darwin/rzm` is missing. It reported no current context shards; this is a source-only review, not a successful Rhizome retrieval.

## Recommended fixes

### RUNTIME-1: Construct each Claude session in one place

**Confirmed hidden link.** `pkg/harness/claude/driver.go:74-92` and `pkg/harness/claude/status.go:38-44` each allocate the session lifetime, event stream, response/backlog/approval/item maps, shutdown defaults, and reader goroutine. A new mandatory field or a change to reader startup ordering must be copied into both paths. The status path is a real session with a different interaction mode, so this is shared lifecycle construction, not merely similar vendor protocol code.

**Smallest redesign:** introduce one private Claude `newSession` constructor, following the existing Codex constructor in `pkg/harness/codex/driver.go:117`. It initializes state and starts the reader. `StartSession` supplies its configured timeouts and interactive mode; `Status` remains noninteractive and performs its existing `status-init` handshake. Keep initialization requests, command arguments, and vendor-specific protocol handling with their current callers. Do not add a cross-vendor session abstraction.

**Reach:** Claude `driver.go`, `status.go`, their existing lifecycle/status/process tests, and the harness note if it explicitly lists construction ownership. No public API, CLI flag, persisted data, wire fixture, or provider behavior should change.

**Verification:** existing `pkg/harness/claude` tests including `TestStatusStates`, session boundary tests, stop-process tests, protocol-failure process tests, and cancellation tests; race-enabled harness tests at integration. Add only a behavioral regression if an existing test does not cover the status/session lifecycle distinction.

**Risk:** starting the reader before its state exists, accidentally making status interactive, or losing timeout defaults. Preserve the current ordering and values.

### RUNTIME-2: Reuse credential metadata in desktop key mutation

**Confirmed hidden link.** `pkg/app/desktop/provider_settings.go:13-18` independently lists Voyage and TypeSafe key aliases already owned by `credentials.NeedForProvider` in `pkg/app/credentials/credentials.go:45-58`. The adjacent desktop status implementation already derives aliases from that owner at `pkg/app/desktop/credentials.go:35-39`. Adding or changing an alias requires updating status, import validation, onboarding, and mutation consistently; today status and mutation use different mechanisms.

**Smallest redesign:** retain the explicit desktop allowlist of `voyage` and `typesafe`, then derive canonical key and aliases through `credentials.NeedForProvider`, just as `CredentialStatus` already does. Use `need.Key` as the persisted key and clear the complete `need.AltKeys` set. This removes the copied values without changing scope or key precedence.

**Reach:** `pkg/app/desktop/provider_settings.go`; existing `provider_settings_test.go` owns mutation, unrelated-value preservation, alias clearing, and invalid-provider cases. `credentials.go` need not change. No UI, Rust command, bridge JSON, configuration format, or release contract changes.

**Verification:** desktop package tests, especially the synthetic fixture containing both `VOYAGE_API_KEY` and `RHIZOME_VOYAGE_API_KEY` and the rejection of provider `openai`. Keep the canonical persisted key assertion exactly as it is.

**Retain:** `provideraccess.providerKeyNames` deliberately orders aliases for runtime resolution (`RHIZOME_VOYAGE_API_KEY` first), while onboarding and persistence use canonical `VOYAGE_API_KEY`. Do not blindly replace the resolver with canonical-first metadata or introduce a dependency from provideraccess back to credentials; credentials already imports provideraccess. A broader provider catalog is unnecessary for this fix.

### RUNTIME-3: Give bootstrap phase implementations their own files

**Confirmed needless reader coupling.** `pkg/app/bootstrap/live.go` contains 1,667 lines covering the central runtime and options, four initialization phases, capability waiting/snapshots, all accessors, leader background work, session cleanup, code embedding initialization, store opening, syncer construction, and diagnostics. These are separately described and tested concerns, but a contributor reading one startup phase receives the entire implementation and a very broad import list. Existing sibling files (`live_election.go`, `live_ownership.go`, `live_config_reload.go`, `live_health.go`) show the established way to separate these responsibilities without adding package boundaries.

**Smallest redesign:** mechanically move methods into cohesive sibling files in package bootstrap. Keep runtime/options/construction in `live.go`; move capability wait/snapshot/accessors together, phase initialization by search/semantic/code concern, leader/session cleanup together, and embedding/syncer setup together. Split to keep individual code files around the local 500-line guideline. Do not replace distinct capabilities with a generic initialization engine or change phase ordering.

**Reach:** `pkg/app/bootstrap/live.go` and new sibling files; update `pkg/app/bootstrap/CONTEXT.md` and the vault-runtime note only where entrypoint locations are named. All callers retain their exact Go symbols. Existing tests remain in their owning files unless moving a test materially improves ownership.

**Verification:** bootstrap unit tests, race tests and runtime integration gates. A body-preserving move needs no new mirrored tests. Confirm imports and function bodies by diff, then run existing behavioral suites.

**Risk:** accidental initialization ordering, store/provider closure, or goroutine-lifetime changes during a mechanical move. Keep bodies intact and review the move separately from logic changes. This is a packaging improvement, not evidence that the phase design itself is wrong.

### RUNTIME-4: Remove two small surplus helpers

**Confirmed surplus.** `pkg/llm/models.go:146-153` hand-writes slice membership for its only caller at line 124. Use standard-library `slices.Contains`; keep model filtering behavior unchanged. `pkg/app/credentials/session.go:194-197` only discards the boolean from `Env.Lookup`, and its only caller is `Resolve` at line 99. Inline that lookup into `Resolve` and remove the wrapper.

**Reach:** those two Go files only. The existing model-list and credential-resolution behavior tests cover them. No new abstraction, dependency, or test is needed.

## Optional cleanup pass

### Comments

- `pkg/app/bootstrap/live.go:43-68` contains a large duplicated LiveRuntime description inside the import block, immediately before the repoconfig import. It describes the type and repeats the package introduction and subsystem documentation. Move any unique ownership detail to the type's documentation, and delete the repeated narration.
- `pkg/app/bootstrap/live.go:851` says the runtime can “become leader later,” although the current constructor elects synchronously and a losing runtime starts no phases. Remove this stale explanation while retaining the actual cancellation/wait behavior.
- Remove the restating comments attached to RUNTIME-4 helpers when those helpers disappear. Preserve public API contract documentation and explanations of ordering, security, cancellation, and platform constraints.

### Tests

No test deletion is justified by the inspected evidence. The duplicated vendor version tests prove meaningful cancellation/cache behavior at distinct driver boundaries. The live-test skips require installed/logged-in provider CLIs; Windows/POSIX skips express real platform contracts. A passing source scan cannot show that these tests are redundant or flaky.

If the Claude constructor is consolidated, keep status, session and process behavior tests. If bootstrap methods move, do not replace their behavioral tests with tests asserting their new file layout.

## Important retained special cases

- Harness drivers keep their own vendor messages, version minimums, approvals, and session state. Shared transport/process/eventstream code already owns the cross-vendor mechanisms. Do not collapse the drivers merely because both run a CLI.
- Claude and Codex version caches are nearly identical, but the subsystem note explicitly keeps version caches in the drivers. Consolidating them would need a separate ownership decision and gives less immediate value than RUNTIME-1.
- The `teamaccess` per-origin Account cache retains refreshed tokens that could not yet persist. Its 30-second lookup cache and per-request gateway check solve different lifetime problems. Removing either as a superficial cache/wrapper cleanup would break recovery or sign-in propagation.
- `procrun.Run` owns arbitrary repository-command process trees and bounded stdout; harness `internal/command.OSRunner` owns short-lived vendor commands, stderr tails and harness phase reporting. Do not unify these runners without preserving their different contracts.
- Legacy runtime lock detection prevents an old running process from coexisting with the current writer. It is a migration safety boundary, not dead compatibility code that can be removed from this refactor without a support decision.
- Desktop independently validates runtime origins, vault identity and health at the boundary. Do not remove these checks because the client also probes; they prevent displaying or stopping a foreign runtime.
- Import-marker polling checks native cancellation during credential capture and again at commit. The marker bridges two process lifetimes; a timer alone is not evidence of an avoidable workaround.
- Provider-key focus restoration after edit/confirmation currently uses requestAnimationFrame. It may deserve an accessibility-specific review, but there is no demonstrated failure or existing general dialog primitive in the inspected desktop code, so it is not a redesign finding here.

## Coverage and limits

Inspected the harness and runtime subsystem guidance, relevant AGENTS and CONTEXT files; runtime, bootstrap, desktop bridge/client/provider/settings flows; provider access and credential/session ownership; team API account and process-boundary shapes; update and repo-executable guidance; and representative TypeScript UI/tests. Searched owned Go code for dead single-reference functions, lifecycle/special-case patterns, and repeated helpers. No single-reference function candidate remained after including comments/tests/callers in the token scan; that scan is only a lead generator and does not prove all code is live.

No broad test gate, live provider call, real desktop app launch, credential operation, or production mutation was performed. Remaining risk is runtime behavior not observed during this static audit.
