---
type: EffortNote
id: EFF-2026-10-25-14-06
aliases: [EFF-2026-10-25-14-06]
name: Scheduled tasks, phase 1
created-at: 2026-10-25T18:06:44Z
status: complete
summary: "Build phase 1 of scheduled tasks: repository task files, modes, the Git-history clock, the repository-wide runner with free checks and background prompt runs in Rhizome-created worktrees, interactive Start through the person's harness, the CLI inbox, and the runtime trigger."
governing-specs: ["[[scheduled-tasks]]"]
---

# Scheduled tasks, phase 1

## Scope

Implement phase 1 of [[scheduled-tasks|SPEC-0127]]: the task capability and its command line, triggered by `rzm tasks run-due` and the runtime interval. A synthetic task proves the whole path.

Excluded: the Rhizome Desktop inbox, badge, notifications, and Open worktree (the desktop part of US6, phase 2); launchd or systemd jobs; Windows behavior beyond compiling and failing clearly; diff views; starter-provided outcome views; real session-memory or dream tasks (SPEC-0127 Delivery Phases and Non-Goals).

## Spec Set (Frozen)

- [[scheduled-tasks|SPEC-0127]], 2026-10-25 revision.

## Stories In Scope (Frozen)

- [[scheduled-tasks#^SPEC-0127-US1|US1]] Declare a task once for the whole repository.
- [[scheduled-tasks#^SPEC-0127-US2|US2]] Choose whether tasks run automatically or notify.
- [[scheduled-tasks#^SPEC-0127-US3|US3]] Run due tasks once, whatever triggers them.
- [[scheduled-tasks#^SPEC-0127-US4|US4]] Check, then run the prompt only when needed.
- [[scheduled-tasks#^SPEC-0127-US5|US5]] Start a task in the person's harness, in its own worktree.
- [[scheduled-tasks#^SPEC-0127-US6|US6]] See and act on tasks in the inbox, CLI criteria only; the Rhizome Desktop criterion is phase 2.
- [[scheduled-tasks#^SPEC-0127-US7|US7]] Keep each task's clock.

## Contracts Fixed by This Plan

These are the public shapes later starters depend on. Approving this plan approves them, so no separate foundation review pause is planned.

**Task file:** `.rhizome/tasks/<id>.yml`, where the id is the file name without its extension.

```yaml
name: Update agent memory        # required, shown in the inbox
description: Mine recent sessions # optional
every: 7d                         # required: Go duration, plus d and w units
scope: person                     # person | team
check:                            # optional; exactly one of command or script
  command: [node, scripts/needs-mining.mjs]
  # script: .rhizome/tasks/memory-check.js   (run by rzm agent code execute)
prompt: |                         # optional; a task needs check, prompt, or both
  Run the session-memory mine workflow ...
timeout: 1h                       # optional, default 1h, applies to check and prompt each
```

**Check output:** one JSON object on stdout: `{"needed": false}` or `{"needed": true, "summary": "...", "context": "..."}`.

**Check environment:** `RZM_TASK_ID`, `RZM_TASK_MAIN_CHECKOUT`, `RZM_TASK_COMMIT`, `RZM_TASK_DATA_DIR`. The prompt preamble names the same locations.

**Repository configuration** in `.rhizome/config.yml`:

```yaml
tasks:
  primaryRef: origin/dev   # optional; default origin/HEAD
  defaultMode: auto        # auto | notify; default notify
```

**Task state** in the main checkout's `.rhizome/task-state/`, owner-only: `state.json` (mode overrides, pause, run records, usage, inbox items), `state.lock` (short, guards `state.json`), `run.lock` (held through a run), `checkout/` (detached check checkout), `worktrees/` (background runs), `data/<task id>/`, and `logs/<run id>.log`.

**Commands:** `rzm tasks status|inbox|run-due|run <id> [--file]|start <item> [--print]|snooze|skip|discard|done <item>|mode <id> auto|notify|off|default|pause|resume`, with `--json` on the read commands.

## Spec Coverage Checklist

- [x] US1: task files, starter and custom, read from the primary ref (`definition_test.go`); drafts with `rzm tasks run --file`; invalid files isolated in `status` and reported by `rzm validate tasks` (`pkg/validate/tasks_test.go`).
- [x] US2: repository default mode, per-person overrides, the trust gate, the first-seen announcement, pause and resume (`runner_test.go`, `review_fixes_test.go`).
- [x] US3: `run-due`, the runtime interval (`TestRunHeadlessAsksForTasksAndStillExitsWhenIdle`), shared state, the repository-wide lock (`TestConcurrentPassesRunATaskOnce`), one run after a missed cadence.
- [x] US4: checks in the reset checkout, shared locations, background worktree runs, Rhizome's tagged commit, usage, process cleanup (`pkg/proctree`, `runner_unix_test.go`), failure handling, live runs with Claude Code.
- [x] US5: Start through each harness's worktree, the branch preamble, the untrusted-folder relay, `--print`, reservation before launch, the open-pull-request check (`start_test.go`; live with Claude Code and Codex).
- [x] US6, CLI part: item states, no-change cleanup, the actions, done on tagged merge. The Rhizome Desktop criterion is phase 2.
- [x] US7: `person` and `team` clocks, whole-line tag matching, `status` with merge settings.
- [x] Requirements and edge cases: no push, definitions from the ref only, owner-only state, no fetch, login-shell PATH, bare repositories (state falls back to the per-user folder), missing ref, merge settings, no harness.

## Plan

Delivery: this branch (`t3/rhizome-scheduled-tasks`) carries the spec, this effort, and the implementation, and ends in one pull request to `main`. Drew merges it.

| Batch | Outcome | Work | Exit evidence |
| --- | --- | --- | --- |
| 0. Spikes | The three unverified behaviors are settled before code depends on them. | (a) Interactive `codex --worktree "<prompt>"` in a disposable repository, through `expect`. (b) A throwaway Go program on `pkg/harness` runs Claude Code and Codex in full-access mode with no person present and edits a file. (c) Stopping that program's process group mid-turn leaves no harness process behind. Limit: about an hour; if (a) fails, Start for Codex falls back to `codex` in the main checkout, with the preamble's `git worktree add` step, recorded as a deviation. | Each result, with versions, in Execution Notes. |
| 1. Foundation (US1, US2, US7) | Task files load from the primary ref, modes resolve, and the clock says what is due. | `repoconfig` gains `Tasks`. New `pkg/app/tasks`: a small Git runner (common directory, main checkout, ref resolution, `show` and `ls-tree` at a commit, tag search with `--grep`), definition parsing and validation, state file with atomic writes under `state.lock`, mode resolution (override, then repository default, then notify), first-seen announcement items, pause, and the clock for `person` and `team` scopes. Thin `cmd/tasks.go` with `status`, `mode`, `pause`, `resume`. | Package tests on temporary Git repositories with a fake clock: dirty branch versus primary ref, invalid sibling files, missing ref, `team` tag found in a squash-style body, merge settings untouched. `make check-fast`. |
| 2. Runner (US3, US4, US6 CLI) | `run-due` runs each due task once per repository and produces inbox items. | `run.lock` through `indexlock`, skip when held. Checks in the forcibly reset `checkout/`, with the login shell's PATH, stdin closed, a timeout, and their own process group. Background prompt runs: branch and worktree under `worktrees/`, `pkg/harness` session in full-access mode behind a small interface so tests use a fake, the preamble, usage recorded, Rhizome's commit with the task tag, empty-run cleanup. Failure and timeout create needs-attention items. Inbox commands: `inbox`, `run`, `snooze`, `skip`, `discard`, `done`, and done-on-tagged-merge. Shared harness resolution: export the `agent.harness` fallback now private to `pkg/app/mcp/tool_generate.go`. In `pkg/harness`, start JSON stdio children in their own process group and stop the whole group, reusing the pattern in `pkg/app/cli/hook/proc_unix.go`. | Package tests with fake harness and real Git: two concurrent `run-due` processes run a task once; a check timeout kills a grandchild; a failed check becomes needs attention without retrying; context reaches the prompt; a changed worktree becomes a ready item with a tagged commit; an unchanged one is removed. Harness test: a stopped session leaves no descendant. `make check`. |
| 3. Start (US5) | A person can take a due task into their own harness. | `rzm tasks start` builds the preamble and branch name, checks `gh` for an open pull request with the tag, and runs `claude --worktree` or `codex --worktree` interactively; `--print` writes the prompt and changes nothing. Claude's untrusted-folder error is relayed with the fix. | `cmd` tests for argv, preamble, suffixing, `--print` purity, and the started transition only after launch. One live run per harness in a disposable repository, recorded. |
| 4. Trigger, init, and the end-to-end task | Rhizome Desktop and headless runtimes run due tasks, and `rzm init` supports task files. | Serve runs `rzm tasks run-due` as a detached child on an interval, without touching the idle tracker, using a detached-spawn helper extracted from `pkg/app/runtime/spawn.go`. Init: `!tasks/` and `!tasks/**` in the `.rhizome/.gitignore` allowlist and a starter `tasks` family beside views and query recipes. A synthetic task fixture (a check that stores context and a prompt that edits one file) runs live once with each harness. | Serve test that the interval spawns the command and an idle headless runtime still exits. Init tests, then `make build`, `rzm init`, `rzm init --check` exit 0. Live end-to-end run recorded. |
| 5. Documentation and integration | The change is documented, verified, reviewed, and ready to merge. | Vault reference note for task files, modes, and commands; `CHANGELOG.md`; updates to the vault-runtime, harness, and CLI subsystem notes with `last-verified`. `make check-full` (the change touches the runtime and concurrency), `./scripts/rzm validate`, `frozen-scope-drift`, full-diff self-review, `greptile review --json` until 5/5, then the closure pass. | Commands and results in Execution Notes; pull request opened with Drew's go-ahead. |

Delegation: batches 0, 1, 2, and 3 are sequential and mine. Inside batch 2, the `pkg/harness` process-group change and, in batch 4, the init changes are independent, so each may go to a T3 `delegate_task` Opus 5.5 High worker in its own worktree. I review each diff before merging it into this branch.

Escalation boundary: a spike result that breaks a contract above, a change to the task file shape, or anything that pushes or opens a pull request comes back to Drew. Routine implementation choices do not.

## Original Intended Delivery

Phase 1 of SPEC-0127 on one branch: task files and modes, the Git clock, the serialized runner with checks and background prompt runs, Start, the CLI inbox, the runtime trigger, init support, and a synthetic task proving the path, with documentation and a pull request ready for Drew.

## Actual Delivered

Phase 1 on `t3/rhizome-scheduled-tasks`, from `b12e36e7` to `50981265`, squash-merged to `main` through one pull request:

- `rzm tasks status|inbox|run-due|run|start|snooze|skip|discard|done|mode|pause|resume` (`cmd/tasks.go`), over the new `pkg/app/tasks`:
  - task files read from the primary ref (`pkg/vault/taskdef`);
  - modes with a repository default (`tasks:` in `.rhizome/config.yml`);
  - the Git-history clock;
  - `run.lock` and `state.lock`;
  - checks in a reset checkout;
  - background prompt runs through the person's harness that commit tagged work on `rzm/<task>/<date>`;
  - the inbox;
  - Start through `claude --worktree` or `codex --worktree`.
- `rzm validate tasks` reports invalid task files (in the `all` suite).
- Runtimes start a detached `rzm tasks run-due` every 15 minutes for vaults inside a Git checkout (`pkg/app/cli/serve/tasks_tick.go`).
- `rzm init` installs starter task files and allowlists `.rhizome/tasks/`.
- `pkg/proctree` gives `pkg/harness` and task checks cleanup that reaches shells in other process groups and sessions, which fixes harness sessions leaving Claude Code's shells running after `Stop`.
- `agentchat.ResolveHarness` is shared by `generate` and tasks.
- Documentation:
  - the guide `docs/reference/guides/scheduled-tasks-guide.md`;
  - the config guide;
  - the README;
  - `CHANGELOG.md`;
  - the vault-runtime, harness, CLI, and validate subsystem notes;
  - `CONTEXT.md` files.

## Deviations

These are implementation choices that differ from SPEC-0127's wording. None changes a contract another phase depends on.

- **Drafts:** run with `rzm tasks run --file <path>`, with no item argument (US1 shows `run <id> --file`).
- **Script checks:** read at the resolved commit but run against the main checkout's vault, because the reset check checkout has no index (US4 says checks run in the reset checkout; command checks do).
- **Start's open pull request check:** matches pull requests whose branch starts with `rzm/<task>/` rather than searching commit messages (US5).
- **Runtime trigger:** fires for any vault inside a Git checkout, not only checkouts with `.rhizome/tasks`, because task files come from the primary ref.
- **Validation suite:** `rzm validate tasks` is in the `all` suite, like `views` and `query_recipes`, rather than the default suite.
- **Process cleanup:** a known gap remains. A process that starts its own session and whose parent exits within one two-second recording interval, as can happen when a harness crashes, can survive. The normal stop path records just before closing input.
- **Windows:** tests that need a shell skip on Windows. Scheduled tasks remain unsupported there, as planned.

## Closure Checklist

- [x] Every in-scope criterion traced to evidence (Spec Coverage Checklist).
- [x] `make check-full` exit 0 at `9b8ec3ec` and again at `50981265`; `./scripts/rzm validate` 0 issues; `rzm init --check` exit 0. `frozen-scope-drift` reports only three findings on other efforts (EFF-2026-10-01-10-06, EFF-2026-10-03-18-33, EFF-2026-10-11-15-21), which were there before this branch.
- [x] Live end-to-end runs recorded (Execution Notes).
- [x] Full diff reviewed by me, by GPT-6.1 Sol xhigh (two rounds), and by Greptile (four rounds); all confirmed findings fixed.
- [x] Merge review: Greptile scored 4, 3, and 3 as each round found narrower cases in new code. On 2026-10-25, Drew chose one more GPT-6.1 Sol xhigh full review in its place, as for PR #35. Its four findings are fixed in `50981265`.
- [x] Pull request [#49](https://github.com/atomicobject/rhizome/pull/49), squash-merged to `main` once CI is green.

## Compounding Follow-ups

- Phase 2: the Rhizome Desktop inbox, badge, notifications, and Open worktree (US6), in a new effort.
- Adopt tasks in session memory (SPEC-0124): mining as a task, its state in the task's data folder, and dropping its "Scheduled or background runs" non-goal.
- Later: a launchd or systemd job, Windows, a diff view, and starter-provided outcome views (SPEC-0127 Delivery Phases).

## Status

Complete. Phase 1 ships in [#49](https://github.com/atomicobject/rhizome/pull/49). Phase 2 (the Rhizome Desktop inbox) and session memory adopting tasks are follow-ups for new efforts.

## Execution Notes

- 2026-10-25T18:06:44Z: Effort created. Spec reviewed twice by GPT-6 Astra High (T3 delegated tasks spec-0126-astra-review-r1 and -r2). Both rounds' findings were resolved or made moot by Drew's later simplification (no caps or allowance, trusted tasks).
- 2026-10-25: Drew asked to "plan out the effort and start executing" in chat. That authorizes this plan and its contracts; he has not yet read the plan text, so it is reported to him with the first progress update. No current Person is configured in this checkout, so `plan-approved-by` is omitted.
- 2026-10-25 batch 0 spikes (macOS, Claude Code 2.1.302, Codex 0.163.0):
  - (a) Interactive `codex --worktree "<prompt>"` passes the prompt in as the first turn (the TUI showed it submitted and working) in `~/.codex/worktrees/<id>/<repo>`, detached. Like Claude, Codex first asks to trust a folder it has not seen, and that trust applies to the repository root. Under `expect`, the TUI waits for answers to terminal color and cursor queries, which a real terminal gives.
  - (b) A throwaway program on `pkg/harness` (`.spike/main.go`, never committed) ran Claude Code and Codex in `PermissionFullAccess` in a fresh Git repository. Both ran shell commands and wrote the file, with zero approval requests, and reported usage (Claude `CostUSD` is 0 on a subscription).
  - (c) Stopping a session does not stop everything it started. Claude Code runs shell commands in their own process group (the shell's group 9769, Claude's 9595), and a backgrounded `sleep 300` survived a normal `Stop()` after Claude exited. Codex left nothing behind. So stopping the harness's process group is not enough. The runner must collect the whole descendant tree while the harness is still running, then terminate it. This widens the harness change in batch 2; it is a routine correction within the plan, not a deviation.
- 2026-10-25 batches 1–3 (`pkg/app/tasks`, `cmd/tasks.go`, `cmd/tasks_render.go`, `repoconfig.LocalTasksConfig`, and `agentchat.ResolveHarness`, moved from `pkg/app/mcp` so tasks and `generate` share one harness choice):
  - `GOCACHE=$PWD/.gocache go test -race -count=3 -tags fts5 ./pkg/app/tasks/` passed. Covered: the primary ref only (branch and dirty edits ignored), invalid siblings, `origin/HEAD` default, team clock from a squash-style body with whole-line matching, an auto run that commits tagged work on a local branch with the check's context in the prompt, no-change cleanup, notify then Run, mode overrides and pause, a failed check that waits instead of retrying, a check timeout that kills a grandchild, two concurrent passes running a task once, a teammate's tagged merge closing the item, the untrusted skip, and Start planning, MarkStarted, and UndoStart. `make check-fast` exit 0.
  - Live, in a disposable repository with the built binary:
    - `run-due` in notify mode made an announcement and a due item and spent nothing.
    - `run <item>` ran Claude Code in about 5 seconds and committed `hello.txt` on `rzm/look/2026-10-25` with the task's name as subject, the agent's message, and `Rhizome-Task: look`; `main` was untouched.
    - `discard` removed the worktree and branch, and `start --print` printed the preamble without changing state.
    - `start --harness claude` in a folder Claude had not trusted returned the item to due with the fix in its reason.
    - `start --harness codex` opened Codex in `~/.codex/worktrees/<id>/`, where the agent ran the preamble's `git switch` onto `rzm/second/2026-10-25` at the primary commit, and the item became started.
    - The disposable repository, its trust record, and the Codex worktree were removed afterward.
  - Choices made during implementation:
    - Drafts run with `rzm tasks run --file <path>` and no item argument.
    - Start's open-pull-request check matches pull requests whose branch starts with `rzm/<task>/`, rather than searching commit messages.
    - Code-mode check scripts run against the main checkout's vault, read at the resolved commit, because the reset check checkout has no index.
    - A run where the agent committed everything itself gets an empty tagged commit, so the team clock still sees it.
- 2026-10-25 harness change (batch 2):
  - A T3 `delegate_task` Opus 5.5 High worker (`eff-2026-10-25-14-06-harness-pgroup`, branch `t3/fa9bd21a`) started JSON stdio children in their own process group and killed the group on `Close`, with grandchild tests (`8ca25135`, cherry-picked as `8a13054d`).
  - I extended it (`b0fb0fff`): `Close` snapshots the descendant tree before closing input and kills it with the group, because spike (c) showed Claude Code's shells in their own groups. The new `TestCloseKillsDescendantsInOtherProcessGroups` failed first, then passed.
  - `go test -race -count=2 ./pkg/harness/...` passed, and `GOOS=windows go vet ./pkg/harness/...` passed.
  - Rerunning spike (c) live with Claude Code left no `sleep 300` behind.
- 2026-10-25 batch 4 (`dd383b61`):
  - Serve runs a detached `rzm tasks run-due` through `runtime.StartDetached`, one minute after start and then every 15 minutes, when the checkout has `.rhizome/tasks`.
  - `TestRunHeadlessAsksForTasksAndStillExitsWhenIdle` shows the interval firing and a headless runtime still idle-exiting.
  - Init installs a starter `tasks` family and allowlists `tasks/`. `TestRhizomeGitIgnoreTracksTaskFilesButNotTaskState` checks this with `git check-ignore`.
  - `make build`, then `rzm init` (it updated only `.rhizome/.gitignore`), then `rzm init --check` exit 0.
  - Live end to end, in a disposable repository in auto mode, `run-due` took 7 seconds. The check counted a draft line and wrote `last-check.txt` to the data folder. Claude Code edited `notes/todo.md`, read `last-check.txt`, and quoted it in its message. Rhizome committed this as `chore(tasks): Finalize notes` with `Rhizome-Task: finalize` on `rzm/finalize/2026-10-25`. Cleaned up afterward.
- 2026-10-25 self-review fixes (`b08c8aee`):
  - An item left running by a crashed pass now becomes needs attention, because a pass holding the run lock means nothing else is running.
  - The login shell's PATH is read after a marker, so profile output can't corrupt it.
  - Unix-only process tests moved to `runner_unix_test.go`, and shell-based tests skip on Windows.
- 2026-10-25 `make check-full` exit 0 at `b08c8aee`. Its edits landed mid-run, so the gate reruns before merge.
- 2026-10-25 Greptile round 1 (`greptile review --json`) raised 18 findings, fixed in `8dff0234` with regression tests in `review_fixes_test.go`:
  - Start checks trust and reserves the item before launching.
  - Failed tasks run again at their next cadence, and pause is rechecked before each task.
  - Script checks get the task's timeout, and a check must state `needed` explicitly.
  - A retry keeps the failed attempt's worktree on its own item, and discard keeps the location when removal fails.
  - Each task's last run is kept outside the trimmed history.
  - Runtimes ask for tasks in any Git checkout, and `ps` snapshots are bounded.
  - `rzm tasks` uses the invocation's directory and reports a broken config.
  - `status` shows failed passes and squash settings that drop the tag (US7).
  - The README mentions tasks.
  - The validation check for task files (US1) went to a T3 Opus 5.5 High worker (`eff-2026-10-25-14-06-validate-tasks`).
- 2026-10-25 GPT-6.1 Sol xhigh review (`eff-2026-10-25-14-06-sol-review-r1`) of the branch through `8dff0234`: "ready after the listed fixes", with three findings, all reproduced and fixed in `d0b65728`:
  - P1: a timed-out check whose escaped descendant held the output pipes kept the run lock.
  - P2: harness cleanup missed descendants when the harness exited before `Close`.
  - P2: retrying a failed-check item skipped the check.
  - The fix adds the leaf package `pkg/proctree`. It records descendants by PID and start time while the child runs and just before input closes, so cleanup reaches reparented processes without touching reused PIDs. Checks also set `WaitDelay`, and `Run` reruns the check first.
  - `go test -race -count=2` passed for `pkg/app/tasks`, `jsonstdio`, and `pkg/proctree`, and a live Claude Code rerun left no `sleep 300`.
- 2026-10-25 later review rounds:
  - The validation check came from a T3 Opus 5.5 High worker: commit `06138158`, cherry-picked as `0797c7db`. It moved parsing to `pkg/vault/taskdef`.
  - Sol's confirmation round (`eff-2026-10-25-14-06-sol-review-r2`) confirmed the lock and retry fixes and found two more problems: the polling gap in descendant cleanup, and an unneeded retry hiding a failed attempt's worktree.
  - A live check showed Claude Code gives each shell its own session (`setsid`), so session membership alone is not enough. `a9aa862c` combines the child's session, recorded descendants, and the sessions those descendants lead. A live rerun left nothing behind.
  - Greptile round 2 (4/5, five findings), round 3 (3/5, three findings), and round 4 (3/5, two race findings) were all fixed with regression tests (`a9aa862c`, `1c6ea907`, `9b8ec3ec`).
  - Final gates at `9b8ec3ec`: `make check-full` exit 0, validation 0 issues, `rzm init --check` exit 0.
- 2026-10-25 final GPT-6.1 Sol xhigh full review (`eff-2026-10-25-14-06-sol-review-r3`): "ready after listed fixes", with four P2 findings, all reproduced and fixed in `50981265` with regression tests.
  - A check timeout now records descendants before killing. `Kill` takes one snapshot for both recording and choosing what to kill.
  - Start's reservation holds the run lock and refuses a task with another active attempt.
  - An interrupted run counts toward the clock, so recovery doesn't rerun it in the same pass.
  - Items close when tagged work newly reaches the primary ref, judged by ancestry rather than dates.
  - One `jsonstdio` failure appeared once under parallel race load and didn't reproduce in 8 isolated or 9 loaded runs. The likely cause was three process snapshots of about 45 ms each inside the tests' 300 ms close budget; one snapshot per `Kill` now. Six further loaded race runs passed.
- 2026-10-25: Drew authorized opening the pull request and squash-merging it once CI is green ("unless there's anything else I'm forgetting").
- 2026-10-25: `d8a4ac33` stops `run-due` from creating task state in repositories that declare no tasks on their primary ref or are untrusted, since runtimes start it in every Git checkout; `TestRunDueLeavesRepositoriesWithoutTasksUntouched` covers it. Pull request [#49](https://github.com/atomicobject/rhizome/pull/49) opened; CI is its full gate.
- 2026-10-26: the workbench redesign ([#48](https://github.com/atomicobject/rhizome/pull/48)) merged to `main` first with the same id, SPEC-0126. This spec, its story ids, and every scheduled-tasks reference were renumbered to SPEC-0127 when `main` was merged in; commit messages on this branch still say SPEC-0126.
