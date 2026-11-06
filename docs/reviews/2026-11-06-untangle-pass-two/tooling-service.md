---
summary: "Second untangle pass: tooling-service audit and concrete candidate evidence."
reference-kind: analysis
---

# Second untangle audit: tooling and team service

Baseline: code revision `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`; inspected HEAD `2a9541828116bada06127cb80ca1575c2ef4c03a` has the parent's documentation follow-up. Read the first-pass delivery dispositions before evaluating candidates. This is a read-only source audit; no repository files, tests, builds, provider calls, or release actions were run.

## T2-TOOL-1: Put stale command removal through the existing ownership plan

**CONFIRMED, high priority. Hidden deletion path violates the existing user-file ownership boundary.**

### Evidence and scenario

- `pkg/app/cli/init/agent_surfaces.go:115-140` selects `.claude/commands`, `.claude/prompts`, `.codex/prompts`, `.codex/commands`, and `.cursor/commands` for enabled harnesses. It puts those directories and current command names into separate `filePlan.commandDirs`/`currentCommands` fields.
- `pkg/app/cli/init/file_plan.go:437-442` sweeps those directories after the normal `applyRemovals` operation. The return list is discarded.
- `pkg/app/cli/init/agent_surface_artifacts.go:117-146` deletes any non-directory entry whose name starts with `rhizome-` or `rhizome.` and is absent from the current template names. It does not inspect `generated-files.yml`, compare current bytes with the recorded fingerprint, ask about edited files, or add the removal to `syncReport.Removed`.
- A user-created `.claude/commands/rhizome-my-custom.md`, or a user-edited retired command with a recorded earlier version, is therefore deleted whenever an init apply reaches this sweep. The removal is outside the classified preview and the returned report.
- Current `pkg/app/cli/init/AGENTS.md` says a `rhizome` prefix alone grants no cleanup authority. The ownership path already implements this: `file_apply.go:117-150` classifies recorded, edited, unknown, and declined removals; `:179-237` obtains decisions; `:348-389` removes accepted candidates and updates the record/report.
- `agent_surfaces_test.go:551-634` tests only the broad prefix-based helper and therefore endorses the unsafe separate path; these are not proof of the current ownership contract.

### Smallest preserving fix

Generate stale command `generatedRemoval` entries while building the ordinary file plan. Feed them through existing `classifyPlan`, `resolveChanges`, and `applyRemovals`; remove the post-apply directory sweep and its extra plan state. Do not set `generatedRemoval.retired=true` for arbitrary prefix matches: that bypasses unknown-file protection. Group mirrored command copies consistently with existing `command:<name>` groups.

Required behavior follows the existing ownership contract:

1. Recorded, unmodified retired commands can be removed and appear in both preview and result.
2. Recorded but edited commands remain on noninteractive runs; interactive removal needs the existing edited-file decision.
3. Arbitrary unrecorded prefixed files remain. If the implementation elects to offer a confirmed historical command for removal, use the existing unknown-removal decision, whose default is keep. A prefix does not establish that historical identity.
4. Declined removals remain declined, unrelated files and symlink targets are preserved, and current command templates still update normally.

### Exact regression fixture

Use an init-package test and the existing `firstRunRepo(t)`/`initTestEnv(t)` helpers. Initial setup:

```go
Run(RunOptions{
    Env: env, Dir: root, Workflow: "none", Addons: "none",
    Agents: "claude,codex,cursor", Search: "off",
    Stdout: &out, Stderr: &out,
})
```

Then use `writeTestFile` in the five directories above for (a) an unrecorded custom `rhizome-*` file, (b) a recorded retired file with matching bytes, and (c) a recorded retired file edited after recording. The existing `setWritten(t, root, rel, originalBytes)` helper writes the real generated-files record for (b)/(c); perform the edit after it for (c). Rerun `Run` with Env/Dir/output buffers only, since first-run flags are rejected on a configured repository. Assert file bytes, not only counters. Before apply, exercise `Check:true` and assert it makes no changes; recorded deletions must be represented in the preview. Use `syncForTest` plus `scriptedUI` for the per-file accept/keep/decline matrix if driving terminal input would obscure the scenario. Keep at least one regression through `Run`, which owns the original failure.

Root owns red/green verification. No probe was executed by this audit. Tests must prove preservation first, then legitimate recorded cleanup and repeated-init idempotence. Run focused init tests and a disposable real init/`init --check` sequence before the full gate. No template or installed skill-byte changes are required by this fix.

## T2-TOOL-2: Reuse the existing compression-provider selection

**CONFIRMED, low priority. Duplicated feature activation/default policy.**

- `pkg/app/cli/init/configure_compression.go:20-41` separately checks nil configuration, explicit enablement, provider normalization, and the `cerebras` default.
- `pkg/app/credentials/credentials.go:153-162` already implements exactly that activation/selection policy as `ActiveCompressionProvider` and is used for credential onboarding. The init package already depends on credentials.
- Both must change together when compression activation or default provider changes. The first-pass kept explicit compression activation as a requirement; this fix retains it.

Use `credentials.ActiveCompressionProvider(&cfg)` in the warning function and return when it is empty. Keep the current `llm.APIKeyForProvider` lookup and existing warning formatting. Do not replace the warning's provider-to-env-name fallback with `NeedForProvider`: the supported sets differ (for example Gemini), so that would change behavior.

Reach: `configure_compression.go` and a small warning-output test, if existing coverage does not already exercise it. Verify no warning for nil/unset/false enablement, and the same warning/provider selection for explicit default and normalized explicit providers with synthetic environments. No credentials need to be read from the host. Remove the now-redundant local default-provider constant as part of this change.

## T2-TOOL-3: Delete two unreferenced default declarations

**CONFIRMED, low priority. Surplus only.**

- `pkg/app/teamdev/teamdev.go:26-27`, exported `DefaultOrigin`, has no consumers, including tests. The actual development selector and all consumers use `session.DevelopmentOrigin`; release/default origin injection remains `session.DefaultOrigin`. Delete this declaration and its comment, without moving origin policy or touching release flags.
- `pkg/app/cli/init/configure_compression.go:16`, private `defaultCompressionModel`, has no consumers. The runtime compressor's actual model lives in `pkg/app/contextpack/compress/llm.go`; delete the unused init declaration.

Caller searches covered the repository's source and tests, excluding vendor/generated browser assets. No new test is needed for declaration deletion; package compile/test checks suffice. External Go consumers of `teamdev.DefaultOrigin` are outside this repository and would need to use the real session constant; parent decides whether the project's no-shim policy requires mentioning this in delivery notes.

## T2-TOOL-4: Delete unused anti-slop object-expression helper

**CONFIRMED, low priority. Separate disposition requested by parent.**

- `web/tools/oxlint/anti-slop/shared/dictionary-types.ts:480-492` exports `isPopulatedObjectExpression`.
- A repository-wide symbol search finds exactly this definition. Neither of the two rule modules importing `dictionary-types.ts` imports it, and no test or barrel re-export references it.
- Remove only that function. Keep `isKnownEvidenceExpression` and the type-alias/dictionary classifiers: they have live rule consumers and distinct behavior.

Verification: existing anti-slop rule tests and web tooling typecheck/lint. This helper is internal lint tooling, not the browser kit API. No new test or rule behavior change is needed.

## Reviewed and retained

- First-pass SVC-1 through SVC-4 are present. Identity receives the ledger directly; receipt replay and settlement decisions are shared; Voyage bounds use the wire constant; dead admin/traffic helpers are gone. No second accounting extraction is warranted.
- Keep the explicitly retained runtime serialization location and model-list synchronization test from the first pass. This audit did not establish a new reason to change either.
- Preserve SQLite/DynamoDB transaction mechanisms, lease fencing, no-resend semantics, currency and credit holds, and shared contract tests. Similar adapter bodies implement storage-specific atomicity.
- Preserve the session client's per-origin account retention, keychain-only secret storage, 30-second refresh visibility, exact origin checks, one pre-admission replay, and generation guards. These are requirements rather than generalization targets.
- Preserve updater logical-destination identity versus resolved filesystem ownership, app handoff independence, all-target prepare-before-write ordering, partial result reporting, pinned-binary locking, and test-injected clock. No update abstraction was justified.
- `teamaccess`'s small transport decorator performs a live-safety check on every request; it is not a pass-through wrapper.
- Preserve release public/private separation, trusted-main source correlation, gateway receipts, signing boundaries, and generated-source ownership. Private legacy release orchestration is still intentionally supported; it is not dead because the public release path changed.
- Preserve `ensureDirExists`'s symlink cases and errors; they encode filesystem requirements. Its branch-narrating comments may be reduced opportunistically, but there is no structural rewrite justified by the comments alone.
- No test failure or flaky case was reproduced in this pass. Do not remove tests merely because their assertions read source; release/ownership tests often encode an actual boundary.

## Coverage and gaps

Inspected init's planning/apply/removal chain and representative config helpers, shared team-client/session boundaries, team gateway first-pass changes and billing contracts, team-access/development gateway composition, updater/app-bundle ownership, release orchestration/CI ownership, Makefile wiring, and the requested lint helper. This is a bounded source audit, not exhaustive runtime execution of every script.

Compounding lookup (`--files` across owned paths) completed with `No current context shards match.` No further runtime retrieval was needed for the concrete local findings. No host credential values, `.env` files, user-global config, live providers, or deployments were read or accessed.
