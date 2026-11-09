---
type: EffortNote
id: EFF-2026-11-09-08-24
name: Desktop repository settings
created-at: 2026-11-09T13:24:44Z
status: complete
summary: Let Rhizome Desktop change a set-up repository's workflow, add-ons, agents, and search provider from a repository tab in Settings, backed by a rerun mode of rzm init's JSON contract.
aliases: [EFF-2026-11-09-08-24]
governing-specs:
  - "[[desktop-repository-setup]]"
---

# Desktop repository settings

## Scope

After setup, Rhizome Desktop offers no way to change a repository's workflow, add-ons, agents, or search provider; only What gets indexed is reachable, from the repository menu. This effort adds a repository tab to Settings, opened from that menu, and the rerun JSON contract it needs from `rzm init`. Drew chose the direction (tabs, one Apply, the repository menu entry) from a mockup on 2026-11-09 and said "OK, do it" with a full-stop handoff: build, prove, review, and leave the pull request ready for him to merge.

Out of scope: deciding about edited files, ejecting or restoring workflows, rerun suggestions, and key entry from the repository tab (see the revised non-goals in [[desktop-repository-setup|SPEC-0118]]).

## Spec Set (Frozen)

- [[desktop-repository-setup|SPEC-0118]] as revised on this branch (2026-11-09): US3, the configured-folder `rzm init --json` requirements, the settings bridge requirement, and the revised non-goals.

## Stories In Scope (Frozen)

- [[desktop-repository-setup#^SPEC-0118-US3|SPEC-0118 US3]] — change a set-up repository's settings.
- [[desktop-repository-setup#^SPEC-0118-US2|SPEC-0118 US2]] — only its entry point, which moves from "What Gets Indexed…" to the repository tab's Edit rules action.

## Spec Coverage Checklist

- [x] US3: Settings tabs, repository menu entry, current choices, staged rows with Undo, refreshing write list, Apply, summary, errors, unsupported executables.
- [x] Executable contracts: configured-folder `rzm init --check --json` and `rzm init --json`, first-run options still rejected.
- [x] Desktop requirement: settings through the selected executable after trust, with a probe and no key.
- [x] Verification: Go and desktop tests named by the spec. The real-app check ran the real shell, bridge, and executable in a browser; a packaged-app click-through is a follow-up (see Deviations).

## Plan

1. **CLI.** Split the rerun's option handling out of `rerun()` so the terminal and JSON paths share it. `Plan` and `Apply` stop rejecting configured folders: the report fills `SetupPlan` with `configured: true` from the rerun plan (change and file lines as writes, edited files as decisions, all add-ons), and the apply reuses the rerun's apply and past-tense lines in `SetupResult`. Update `--json` help so the desktop can probe for the capability. Go tests in `pkg/app/cli/init` and `cmd`.
2. **Bridge.** Add `settings-report` and `settings-apply` operations to `pkg/app/desktop` that run `rzm init --check --json` / `rzm init --json` with `--workflow`, `--agents`, `--search`, `--add-addon`, and `--remove-addon` on a configured, trusted worktree, probing `init --help` for the new capability. Add matching native actions in `desktop/src-tauri`. Tests on both sides.
3. **Shell.** Settings gets tabs; a `RepositorySettings` component renders the rows with the setup sheet's styles, stages changes, refreshes the report, and applies. The repository menu gets "Repository Settings…". Tests with Vitest.
4. **Docs.** Changelog, `pkg/app/desktop/CONTEXT.md`, `pkg/app/cli/init/CONTEXT.md`, `desktop/README.md`, and the CLI subsystem note where they describe first-run-only JSON.
5. **Proof and review.** `make check`, `make desktop-check`, the real executable against a fixture repository, and the shell rendered against real `rzm` JSON. Independent review (sol-loop), Greptile, then closure on the branch.

## Original Intended Delivery

A Settings tab in Rhizome Desktop that changes a set-up repository's workflow, add-ons, agents, and search provider through the worktree's own Rhizome, with the files listed before they are written.

## Actual Delivered

Delivered as planned on branch `t3/desktop-repo-settings`:

- `rzm init --check --json` and `rzm init --json` on a configured folder report and apply the workflow, add-ons (optional ones included), agents, and search provider through `applyRerunOptions`, which the terminal rerun now shares. The report lists the rerun's change lines, every file an apply writes (support files included), and edited files it would keep; the apply reports created, updated, and kept files, the past-tense lines, and paths to commit (removals included). First-run options there still fail `not_first_run`.
- The desktop bridge's `settings-report` and `settings-apply`, with a help-text probe and changed-only flags, and matching native actions.
- Settings tabs ("Rhizome on this Mac" and the repository), the repository menu's Repository Settings…, and `RepositorySettings`: staged rows with Undo and Discard, a pinned bar listing what a change writes, Apply with the terminal's summary, trust and unsupported-executable states, readiness refreshed after a key is saved, and staged changes kept through Edit rules.
- SPEC-0118 US3 and requirements, SPEC-0126's action list, changelog, README, `desktop/README.md`, both CONTEXT notes, and the CLI subsystem note.

## Execution Notes

- 2026-11-09T13:24:44Z [decision] Drew approved the direction and asked for the full-stop flow in chat ("OK, do it" after the recommendation and mockup). The current user is not configured in this checkout, so `plan-approved-by` stays absent; approval is recorded here.
- 2026-11-09T13:24:44Z [decision] A workflow change from the app follows `rzm init --workflow` (Rhizome's files for dropped starters are removed, docs stay) rather than the terminal menu's eject question; recorded as a SPEC-0118 non-goal.
- 2026-11-09T13:24:44Z [learning] A non-interactive rerun already applies `--workflow`, `--agents`, `--search`, and add-on changes; only the JSON rendering and the configured-folder refusal stood in the way.

- 2026-11-09T14:40:00Z [decision] A settings request carries only the settings the person changed. Passing `--agents` every time would rewrite agent preferences nobody touched, so `SettingsChoices.agents` is optional end to end and the bridge adds each flag only when its choice is present.
- 2026-11-09T14:40:00Z [decision] The repository tab loads when first shown and stays mounted while Settings is open, so opening Settings on the Mac tab starts no process and switching tabs keeps staged changes. Closing Settings discards unapplied changes.
- 2026-11-09T14:40:00Z [validation] `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/cli/init/ ./pkg/app/desktop/ ./cmd/` passed, including the new settings tests. `make desktop-check` passed: typecheck, oxlint (one existing `no-unused-vars` warning in `App.test.tsx`), oxfmt, 146 frontend tests, 145 Rust tests, clippy, and the app-instance checks. `make check` passed (Go unit tests and 2,026 web tests). `./scripts/rzm validate` and `validate frozen-scope-drift` reported 0 issues.
- 2026-11-09T14:40:00Z [validation] Real-executable proof on a fixture Git repository: `rzm init --json` set it up; `rzm init --check --json` reported `configured: true` with no writes; with `--agents claude,cursor --add-addon compounding --remove-addon action-items` it listed the change lines and every file and wrote nothing. The real `rzm desktop-bridge` answered `settings-report` with the same document. The real desktop shell, served by Vite with a Tauri stand-in that forwards settings and scope requests to that bridge, staged Cursor and Compounding, listed 60 files, and Apply wrote `.cursor/rules/rhizome.mdc` and the Compounding skill, which did not exist before. Playwright captures in light, dark, and 760px widths led to two style fixes (labels align to the top of their rows; the pinned bar's file list is capped).

- 2026-11-09T15:00:00Z [review] sol-loop with GPT-6.1 Sol (high), 3 rounds. Round 1 found two shell bugs, both fixed with tests: add-on toggles after a workflow change, and stale reports after Undo or Discard. Round 2 approved.
- 2026-11-09T15:00:00Z [review] `greptile review --json`: 3/5, then 4/5 twice, then 5/5 with no comments. Fixed: the file preview missed support files; Edit rules discarded staged changes; readiness went stale after a key was saved; Discard kept a failed report; edited-file notices were hidden on their own; the apply result missed kept files, removals to commit, and settings files; the README lacked the commands; a remote Ollama endpoint looked uninstalled. The remote Ollama readiness change has no test, since it needs a live Ollama probe.
- 2026-11-09T15:00:00Z [validation] After the review fixes: `go test -tags fts5 ./pkg/app/cli/init/ ./pkg/app/desktop/ ./cmd/` passed, and the desktop shell's 151 tests, lint, and format passed. CI's `make check-full` runs on the pull request.

## Deviations

- SPEC-0126 (workbench redesign, effort closed) listed "What Gets Indexed…" among the repository actions; it now names "Repository Settings…", and its `last-updated` moved to 2026-11-09.
- The packaged app was not clicked through: the shell was exercised in a browser against the real bridge and executable, and the native actions are covered by Rust tests. A packaged-app check of the tab is a follow-up for Drew.

## Compounding Follow-ups

- [ ] In the packaged Rhizome Desktop app, open Repository Settings… on a real repository, stage and apply one change, and use Edit rules and back (EFF-2026-11-09-08-24 checked the shell in a browser against the real bridge) #action-item
  assignee:: [[Drew Colthorp]]
- Proposed, not built: show the Mac tab's settings in the same label-column rows as the repository tab, as the mockup did.

## Closure Checklist

- [x] Plan steps delivered or recorded as deviations.
- [x] Gates recorded with commands and results.
- [x] Pull request linked (see Status).

## Status

Complete on the branch. The pull request carrying this work is linked here once opened; its CI runs `make check-full`. The packaged-app click-through is an action item for Drew and does not keep the effort open. Merging is Drew's.
