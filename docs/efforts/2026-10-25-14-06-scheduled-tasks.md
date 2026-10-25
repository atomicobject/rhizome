---
type: EffortNote
id: EFF-2026-10-25-14-06
aliases: [EFF-2026-10-25-14-06]
name: Scheduled tasks, phase 1
created-at: 2026-10-25T18:06:44Z
status: active
summary: "Build phase 1 of scheduled tasks: repository task files, modes, the Git-history clock, the repository-wide runner with free checks and background prompt runs in Rhizome-created worktrees, interactive Start through the person's harness, the CLI inbox, and the runtime trigger."
governing-specs: ["[[scheduled-tasks]]"]
---

# Scheduled tasks, phase 1

## Scope

Implement phase 1 of [[scheduled-tasks|SPEC-0126]]: the task capability and its command line, triggered by `rzm tasks run-due` and the runtime interval. A synthetic task proves the whole path.

Excluded: the Rhizome Desktop inbox, badge, notifications, and Open worktree (the desktop part of US6, phase 2); launchd or systemd jobs; Windows behavior beyond compiling and failing clearly; diff views; starter-provided outcome views; real session-memory or dream tasks (SPEC-0126 Delivery Phases and Non-Goals).

## Spec Set (Frozen)

- [[scheduled-tasks|SPEC-0126]], 2026-10-25 revision.

## Stories In Scope (Frozen)

- [[scheduled-tasks#^SPEC-0126-US1|US1]] Declare a task once for the whole repository.
- [[scheduled-tasks#^SPEC-0126-US2|US2]] Choose whether tasks run automatically or notify.
- [[scheduled-tasks#^SPEC-0126-US3|US3]] Run due tasks once, whatever triggers them.
- [[scheduled-tasks#^SPEC-0126-US4|US4]] Check, then run the prompt only when needed.
- [[scheduled-tasks#^SPEC-0126-US5|US5]] Start a task in the person's harness, in its own worktree.
- [[scheduled-tasks#^SPEC-0126-US6|US6]] See and act on tasks in the inbox, CLI criteria only; the Rhizome Desktop criterion is phase 2.
- [[scheduled-tasks#^SPEC-0126-US7|US7]] Keep each task's clock.

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

- [ ] US1: task files, starter and custom, read from the primary ref; drafts with `--file`; invalid files isolated.
- [ ] US2: repository default mode, per-person overrides, trust gate, first-seen announcement, pause and resume.
- [ ] US3: `run-due`, runtime interval, shared state, repository-wide lock, one run after a missed cadence.
- [ ] US4: checks in the reset checkout, shared locations, background worktree runs, Rhizome's tagged commit, usage, process groups, failure handling.
- [ ] US5: Start through each harness's worktree, branch preamble, untrusted Claude folder, `--print`, started after launch, open pull request check.
- [ ] US6 (CLI): item states, no-change cleanup, the actions, done on tagged merge.
- [ ] US7: `person` and `team` clocks, whole-line tag match, `status`.
- [ ] Requirements and edge cases: no push, ref-only definitions, owner-only state, no fetch, login-shell PATH, bare repositories, missing ref, merge settings, no harness.

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

Phase 1 of SPEC-0126 on one branch: task files and modes, the Git clock, the serialized runner with checks and background prompt runs, Start, the CLI inbox, the runtime trigger, init support, and a synthetic task proving the path, with documentation and a pull request ready for Drew.

## Actual Delivered

Not yet delivered.

## Deviations

None yet.

## Closure Checklist

- [ ] Every in-scope criterion traced to evidence.
- [ ] `make check-full`, validation, `rzm init --check`, and the live end-to-end run recorded.
- [ ] Greptile 5/5 with no unresolved comments, and the full diff self-reviewed.
- [ ] Pull request number recorded.

## Compounding Follow-ups

None yet.

## Status

Active.

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
