---
type: EffortNote
id: EFF-2026-10-06-08-07
aliases: [EFF-2026-10-06-08-07]
name: Desktop repository setup
created-at: 2026-10-06T12:07:05Z
status: active
summary: "Replace the desktop app's one-button setup with a setup sheet backed by rzm init's JSON report, and add a What gets indexed page for .rhizome/ignore with inherited .gitignore rules."
governing-specs:
  - "[[desktop-repository-setup]]"
---

# Desktop repository setup

## Scope

Deliver [[desktop-repository-setup|SPEC-0118]]: the desktop setup sheet, the What gets indexed page before and after setup, and the `rzm init --json`, `--addons`, `--skip`, `--keep-indexed`, `--search-key-stdin`, and `rzm index scope` contracts they depend on. Open a pull request after integration and verification.

Excluded: the rerun maintenance from the app, the path tester, and the other exclusions SPEC-0118 lists as non-goals. No release is published.

## Spec Set (Frozen)

- [[desktop-repository-setup|SPEC-0118]], all requirements in the 2026-10-06 revision.

[[rhizome-desktop|SPEC-0113]] and [[init-starter-workflow|SPEC-0038]] constrain this work without entering its scope. They receive cross-references only, each recorded as a deviation on the active effort that froze it.

## Stories In Scope (Frozen)

- [[desktop-repository-setup#US1 - Set up an unconfigured worktree from one review sheet|SPEC-0118 US1]]
- [[desktop-repository-setup#US2 - See and change what gets indexed|SPEC-0118 US2]]

## Spec Coverage Checklist

- [ ] `rzm init --json` report (with planned rules) and apply results, the JSON error contract, `--addons`, `--skip`, `--keep-indexed`, and `--search-key-stdin`.
- [ ] `rzm index scope` report across four layers for configured folders, and all-or-nothing edits.
- [ ] Bridge operations, capability probe, key handling, trust after success, and worktree serialization.
- [ ] Setup sheet: findings, choices, the Writes row, progress, summary, and errors.
- [ ] What gets indexed page before and after setup, opened from the sheet and the repository menu.
- [ ] Go, Rust, and shell tests; native verification on a fixture repository; documentation; independent review; pull request.

## Plan

The current gap: `Service.Initialize` (`pkg/app/desktop/open.go`) runs `rzm init --path <folder>` without options, discards its output, and reports only an exit status. `OpenStatus` (`desktop/src/Status.tsx`) offers one Set up button. No command reports init's findings or the ignore layers as data. The ignore-file editing (suggested skips, keep-indexed markers, included subtrees) lives in `pkg/app/cli/init/skips.go` and `include_subtrees.go` and runs only inside interactive init. Loading the `cli-subsystem` skill is required for Batch 1, and the `vault-runtime-subsystem` skill when Batch 2 touches runtime start.

### Decisions

Settled with Drew on 2026-10-06: one setup sheet, three workflows plus an Action items add-on, a key field in the app, trust as part of Set up, and a page available both before and after setup.

The following decisions are proposed. Batch 1 makes them concrete, and a foundation review confirms them before Batches 2 and 3 build on them:

1. **JSON shape.** Every `--json` document carries `"schema": 1` and uses camelCase keys. Display text, such as findings and summary lines, comes from the functions the terminal already uses, so wording stays in one place. Recommendation: version the documents even though the app requires a matching release, because scripts and agents also read them.
2. **Where `index scope` lives.** It is a subcommand of `rzm index`, for configured folders only, that calls helpers exported from `pkg/app/cli/init`; no package moves. The init check report embeds the same scope shape, with the rules setup would write marked as planned. Recommendation: keep one owner of the `.rhizome/ignore` sections and markers, rather than move them to a new package for a second caller.
3. **Edits made before setup.** Held edits become `rzm init` options (`--skip`, `--keep-indexed`, and the existing `--include-ignored`). They are written in the same `apply` pass as the configuration, through the `setup.skips` and `setup.keep` fields the settings menu already fills. Recommendation: one writer and one error path, and the result matches a terminal run byte for byte. A second `index scope` call after init would fail exactly when the pinned download fails, because after init that pinned executable is the selected one.
4. **Writes row freshness.** The app reruns `rzm init --check --json` with the current choices and held edits, debounced, instead of predicting files or planned rules itself. The row shows that it is refreshing. Each check runs `DetectLayout` and `git ls-files`.

The independent review on 2026-10-06 changed decision 3 and moved the planned rules from `index scope` into the check report. Its other findings are reflected in SPEC-0118: what `--remove-rule` matches, sending the key only on Set up, trust whenever configuration was written, the trust step first in a Rhizome source checkout, and readiness as the app's environment sees it.

### Batches

| Batch | Outcome | Work | Exit evidence |
| --- | --- | --- | --- |
| 1. Executable contracts | `rzm init --check --json`, `rzm init --json`, `--addons`, `--skip`, `--keep-indexed`, `--search-key-stdin`, and `rzm index scope` behave as SPEC-0118 requires. | `cmd/init.go`, `pkg/app/cli/init` (first-run report with an embedded scope and planned rules, apply result, the JSON error path, add-on choice recorded in `workflows.yml`, skip options mapped onto `setup.skips` and `setup.keep`, the stdin key through `saveKey`, and exported scope report and edit helpers), and a new `cmd/index_scope.go` for configured folders. Tests in `pkg/app/cli/init` and `cmd`. Update `pkg/app/cli/init/CONTEXT.md` and the `index-scope.md` skill template, and add SPEC-0038 cross-references with a deviation on EFF-2026-10-01-10-06. | Focused `go test ./pkg/app/cli/init/... ./cmd/...`. `make check-fast`. A fixture run of `rzm init --check --json`, then `rzm init --json` with non-default choices, then `rzm init --check` exits 0. `rzm index scope --json` before and after setup. The generated-surfaces gate. **Foundation review** of the four decisions with real JSON samples before Batches 2 and 3. |
| 2. Bridge and native layer | The app can request a setup report, apply setup with choices and a key, read and edit scope, and detect an unsupported executable. | `pkg/app/desktop`: `setup-report`; `initialize` with choices and held edits as options and the key on `cmd.Stdin`; trust whenever configuration was written; `scope` and `scope-edit`; the `setup_unsupported` probe; and `trust_required` before a report when the setup executable is repository-selected. `desktop/src-tauri`: new `Action` variants through `worktree_action`, with the key kept in a type that never prints. Update `pkg/app/desktop/CONTEXT.md`. | `go test ./pkg/app/desktop/...` with fixture executables: trust only after success, the key absent from arguments and responses, the probe, and serialization. `npm run test:native`. |
| 3. Shell experience | The setup sheet and the What gets indexed page work against the bridge, in both setup and configured modes. | New `desktop/src/SetupSheet.tsx` and `desktop/src/IndexScope.tsx`, with types in `api.ts`. Wiring in `App.tsx` stays thin, because that file is already over 600 lines: the setup step renders the sheet, the repository menu gains "What Gets Indexed…", and the page draws over the content area as Settings does. Styling in `style.css` follows the existing dense AO tokens. Load the `impeccable` skill for the layout and copy pass. | `npm run check` in `desktop/`, with tests for choice changes, held edits applied at setup, the summary, unsupported and failure states, and immediate edits in configured mode. |
| 4. Integration and verification | The packaged app sets up a real fixture repository and administers its scope. The documentation matches. | `make desktop-deps`, `make desktop-check`, `make desktop-build`, and `make check`. Native run with `RHIZOME_DESKTOP_DATA_DIR` set to a temporary directory: an unconfigured Git fixture with an ignored nested repository, non-default choices, edits before and after setup, a byte comparison with an equivalent terminal run, and the workspace opening and indexing without a trust prompt. Screenshots for Drew. Update `desktop/README.md`, and add the SPEC-0113 pointer with a deviation on EFF-2026-10-03-18-59. Independent review, accepted fixes, `./scripts/rzm validate`, and the frozen-scope-drift check. Open and link a pull request. | Every gate command and its result recorded below. Screenshots of the sheet, summary, and page. The review outcome and the pull request link. |

Batches 2 and 3 can run in parallel once the foundation review passes, because the bridge request and response types are their only shared contract. A batch does not imply a separate worker. Delegated work runs through T3 `delegate_task` in its own worktree.

Escalation: a change to the settled decisions, or to rerun behavior, needs Drew. Fixing defects in init's existing first-run output that this work exposes stays in scope. One example is today's silent `exit status 1` when the pinned download returns 404 after files are written.

## Plan Approval

Drew approved the plan in chat on 2026-10-06 (2026-10-06T19:57:15Z): "Ready to build this?", in reply to the request to approve the plan including the foundation-review pause after Batch 1. The current-user configuration is absent, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

Rhizome Desktop sets up an unconfigured worktree from one reviewed sheet with the terminal's choices, an in-app key, and trust, and it shows and edits the rules that decide what Rhizome indexes, before and after setup. `rzm` exposes the same contracts to scripts and agents.

## Actual Delivered

Not started.

## Execution Notes

- 2026-10-06: Recon. The current one-button setup fails opaquely when the pinned download 404s after writing, because no Rhizome release is published yet. `rzm init --check` already computes the findings and files, and `pkg/vault/ignore` already models the four layers with sources and lines (`Explain`, `RuleRef`). A strawman of the sheet was reviewed in chat. SPEC-0117 is claimed by the Scope Overview draft in another checkout, so this spec is SPEC-0118.
- 2026-10-06: An independent review (Claude Fable advisor) of the spec and plan returned "approve with edits", and the edits are applied. It confirmed that the key path has no logging in Rust or `pkg/app/desktop`, that `bridge::call` discards stderr, and that the protocol rejects unknown request fields. Follow-up outside this scope: SPEC-0038 US4-AC1 says the settings menu has no Codex row because Codex reads only shared files, but `--agents codex` writes `.codex/prompts` and `.codex/commands`. The sheet keeps a Codex checkbox, because the option is real.
- 2026-10-06: Batch 1 implemented. `pkg/vault/ignore` gained `LoadMatcherWithRhizomeLines` and `Matcher.Rules` (pruned walk for nested `.gitignore`). `pkg/app/cli/init` gained `Plan` and `Apply` (`first_run_json.go`), first-run options (`first_run_options.go`: `--addons`, `--skip`, `--keep-indexed`, `ErrNotFirstRun`), and the scope model (`scope.go`). `Run` now shares `start` and `prepareFirstRun` with them. The three `.rhizome/ignore` writers became pure transforms that `apply` and the plan share through `plannedIgnore`. `promptWorkflow` and `workflowLabel` read one `workflowChoices` table, `setupSummaryLines` and `commitPaths` came out of the printers, and the Action items starter gained a `description`. `cmd/init.go` added `--json`, `--addons`, `--skip`, `--keep-indexed`, and `--search-key-stdin` (pipe only, refused from a terminal), and `cmd/index_scope.go` added `rzm index scope`. Evidence: `go test ./cmd/... ./pkg/app/cli/... ./pkg/vault/ignore/...` passed; `make check-fast` passed after `npm ci` in `web/`; `make build`, then `rzm init` (updated only the two generated `index-scope.md` copies), then `rzm init --check` exited 0; `./scripts/rzm validate` and `frozen-scope-drift` were clean. On a fixture repository with a nested repository that `.gitignore` excludes, `rzm init --check --json`, then `rzm init --json` with non-default choices, then `rzm init --check` exited 0. `rzm index scope` read and edited the rules, and the error documents matched the spec. The pinned download still returns 404 because no release is published; it is reported in `pin.error` with exit 1.
- 2026-10-06: An independent review of the Batch 1 code (Claude Fable advisor) found no regressions in the terminal paths, no key exposure, and clean JSON stdout. Fixed: `--addons` ids are now checked before any write, including a piped key; the plan's Writes files and the result's created and updated lists name `.rhizome/config.yml`, `.rhizome/workflows.yml`, and `.rhizome/ignore`; and a `.rhizome/ignore` with comments but no rules keeps its comments when the built-in list is written ahead of the first rule. That last fix also corrects a pre-existing case where init's first skip or include in such a file silently dropped the built-in list. Recheck: init, ignore, and `cmd` tests and `make check-fast` passed.
- 2026-10-07T09:59:22Z: Foundation review. Drew confirmed the four Batch 1 decisions after reviewing real JSON samples of the setup plan, the apply result, the failure document, and the scope report ("yes"). Batches 2 and 3 start in parallel: a delegated worker builds the bridge and native layer in a separate worktree against the shared request contract, and the shell is built in this checkout.

## Deviations

- 2026-10-06, Batch 1 (frozen-scope-drift acknowledged for SPEC-0118): implementation settled three details in SPEC-0118's executable contracts without changing intent. `rzm init --check --json` exits 0 when it produced a report, since a first run always has changes. The apply result drops "whether configuration was written", because the bridge decides trust by inspecting the folder after the run, which also covers a run that fails partway. Trust follows "configuration exists after the setup run" for the same reason.
- 2026-10-07 (frozen-scope-drift acknowledged for SPEC-0113 and SPEC-0038): as planned, this effort added cross-references from SPEC-0113's unconfigured-worktree requirement and SPEC-0038 US1-AC5 to SPEC-0118, with matching deviations on EFF-2026-10-03-18-59 and EFF-2026-10-01-10-06. Neither spec's requirements changed.

## Closure Checklist

- [ ] Spec coverage checklist complete, with evidence in Execution Notes.
- [ ] `make check`, `make desktop-check`, `make desktop-build`, and the generated-surfaces gate pass.
- [ ] Native verification recorded with screenshots.
- [ ] `./scripts/rzm validate` and `frozen-scope-drift` clean, with deviations recorded for SPEC-0113 and SPEC-0038.
- [ ] Independent review resolved, and the pull request opened and linked.

## Compounding Follow-ups

None yet.

## Status

Active. Batch 1 committed (8d63d3d6) and confirmed in foundation review; Batches 2 and 3 in progress.
