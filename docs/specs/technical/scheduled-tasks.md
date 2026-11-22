---
type: TechnicalSpec
id: SPEC-0127
aliases:
  - SPEC-0127
  - Scheduled tasks
summary: "Repositories declare periodic agent tasks: an optional free pre-flight check that decides whether work is needed, then a prompt run by each person's own agent harness. A repository sets whether tasks run automatically or only notify, and each person can override that per task. Background runs land in the checkout Rhizome has open or in their own worktree, push only when the repository and person allow it, and report back with a message and the changed files. Git history is the team's shared clock, and run records stay in each person's ignored local state, so teammates using different harnesses need no coordination."
spec-status: active
last-updated: 2026-11-22
---

# Scheduled tasks

## Summary

Some agent work should happen periodically without anyone remembering to ask for it. Examples include mining recent sessions for agent memory ([[session-memory-starter|SPEC-0124]]), a "dream" pass that merges and retires memories, and tending a vault's evidence notes. Claude Code and Codex each have their own schedulers, but neither fits a repository whose teammates use different harnesses. Each stores tasks in its own format, outside the repository, and Claude's cloud routines cannot see a developer's local transcripts.

Rhizome owns the schedule instead. Tasks are declared once in the repository and run on each person's machine through that person's own harness ([[coding-agent-harness|SPEC-0101]]). A task has up to two parts:

- **Pre-flight check (optional, free).** A command or a code-mode script that decides whether the task is needed now and gathers context for the prompt. Checks make no model calls.
- **Prompt.** Agent work run by the person's preferred harness: Claude Code or Codex today, and any harness `pkg/harness` supports later. The prompt can tell the agent to run commands, such as a starter's scripts, to do the work.

The point of tasks is agent work that happens automatically or semi-automatically. Each task runs in one of two modes:

- **Auto:** when the task is due and its check says it is needed, the prompt runs in the background.
- **Notify:** the task appears in the inbox instead. **Run** does the work in the background now. **Start** opens the person's harness interactively in a new worktree. The person can also snooze or skip it.
- **Run now:** a task with no open inbox item can be run on request from its page or with `rzm tasks run --task <task>`. Rhizome adds a due item and runs it as Run would, check first.

The repository sets the default mode for its tasks, and each person can override it per task, including turning a task off. Cost is the task author's responsibility: a task should be well structured, use its check to avoid unneeded work, and keep its prompt efficient. A task that costs too much is a problem with the task, to be fixed or turned off, not something Rhizome meters.

A background run lands in one of two ways (US12, US13). **In place**, the default, runs in the checkout Rhizome has open on the host machine and commits only the files the task declared it writes. **PR** works in a worktree that Rhizome creates from the primary branch and commits whatever it changed to a local branch, tagged with the task. Pushing happens only when the repository and the person both allow it. The run ends with a message for the person and the list of changed files. The person reviews the worktree, for example by switching to it in Rhizome Desktop, then opens the pull request or discards the branch.

Coordination needs no shared service:

- **Git history is the team clock.** A task's commits carry a task tag. When a tagged change reaches the primary branch, every teammate's clock for that task resets.
- **Run records are local.** Each person's runs, including checks that found nothing to do, are recorded in ignored state that every worktree of the repository shares.
- **Duplicates are harmless.** Two people occasionally doing the same chore both still close the gap with the latest primary branch, so Rhizome does not lock across machines.

Tasks are trusted repository code, reviewed on the primary branch, the same trust that hooks get. Rhizome neither sandboxes them nor polices what they change.

Task authors can see and test a local version before it reaches the primary branch. **Test once** deliberately runs that version from a captured source snapshot, with fresh task data and separate test history. It does not enable a schedule or change the scheduled task's clock.

## Goals

- Let a repository declare periodic agent tasks once and have them work for teammates using Claude Code, Codex, or both.
- Let agent work happen automatically, or with one click, without anyone remembering to ask for it.
- Let a repository choose whether its tasks run automatically or only notify, and let each person override that per task.
- Do agent work only when a free check says the task is needed.
- Let a repository choose, per task, whether a task's commits land directly or go through a pull request.
- Run tasks on time on one designated machine, even with no window open.
- Run each due task once per person and repository, however many worktrees, runtimes, or triggers are active.
- Make the shared clock Git history, so a teammate's merged run counts without any coordination service.

## Non-Goals

- Installing tasks into Claude Code's or Codex's own schedulers, or into cloud routines.
- Merging pull requests without a person.
- Vendor automation backends (the Claude or Codex apps' schedulers), RRULE or cron authoring, and a launchd or systemd host install.
- Metering beyond an optional per-person daily token cap (US14). Keeping a task affordable is still its author's job.
- Sandboxing tasks, or detecting what they change outside their own worktree.
- Locks or claims across machines, any central store of run state, per-task locks, circuit breakers, or task dependencies.
- Building the session-memory and dream tasks, or changing [[session-memory-starter|SPEC-0124]]. This spec provides the capability.
- Findings with per-item identity, such as "this note is stale". A check reports whether the task is needed and passes context to the prompt.

## Key Entities

- **Task:** a YAML declaration with an id, a cadence, a scope (`person` or `team`), an optional check, an optional prompt, and an optional timeout. A task needs a check, a prompt, or both.
- **Primary ref:** the branch scheduled tasks are read from and scheduled work starts from. It defaults to the remote's default branch (`origin/HEAD`) and can be overridden in `.rhizome/config.yml`, for example for teams that work on `dev`. Each scheduled run resolves it to one commit first and uses that commit throughout.
- **Local version:** a task definition in the selected checkout, including uncommitted changes. Its state is new, differs, same, or invalid relative to the scheduled definition. It is not automatically scheduled.
- **Test run:** an explicit run of the local version from an immutable source snapshot. Its record names the source checkout and snapshot, and its data and history are separate from scheduled runs.
- **Mode:** auto, notify, or off. The repository's default applies unless the person overrides it for a task.
- **Task state:** ignored local state for one person and repository: mode overrides, run records, usage, inbox items, and each task's private data folder.
- **Inbox item:** a task that needs the person. Its state is due, running, ready, started, needs attention, done, or dismissed (US6).
- **Task tag:** the line `Rhizome-Task: <task id>` in a commit message.

## User Stories

### US1 - Declare a task once for the whole repository

- id:: ^SPEC-0127-US1
- summary:: A maintainer commits a task declaration, and every teammate's Rhizome reads the same definition from the primary ref.
- status:: draft

#### Acceptance Criteria

- A task is a YAML file in `.rhizome/tasks/` or in a module's `tasks/` folder (`.rhizome/modules/<module>/tasks/`, [[rhizome-modules|SPEC-0130]]). A flat task's id is its file name; a module task's id is `<module>/<file name>`, and the branch, worktree, run data, and pull-request matching built from it never collide with a flat task's. The files hold two kinds, read the same way:
  - **Starter tasks** are installed by `rzm init` as a managed asset type, the same way starters install views. `rzm init` refreshes them.
  - **Custom tasks** are written by the repository's own team and left alone by `rzm init`.
- `rzm init` adds `tasks/` to the managed `.rhizome/.gitignore` allowlist.
- A declaration names its check, as an argument list or a code-mode script run with `rzm agent code execute`; its prompt, as text; and its cadence, scope, and timeout. The timeout defaults to one hour.
- Rhizome reads scheduled task files from the resolved primary ref through Git objects, without a checkout. Editing a task file on a branch never changes automatic execution. `rzm tasks run --file <path>` and **Test once** run a local version deliberately.
- The Tasks page includes local definitions from its selected checkout. A new local task appears before it is committed or merged. A changed or invalid local version does not replace the valid scheduled definition, and a task absent locally remains visible if scheduled. Local-only tasks have no mode, skip, or snooze controls; the mode API rejects an ID absent from the primary ref.
- An invalid task file is reported by `rzm tasks status` and by validation, and does not prevent other tasks from running.

### US2 - Choose whether tasks run automatically or notify

- id:: ^SPEC-0127-US2
- summary:: A repository sets the default mode for its tasks, and each person can override it per task or pause everything.
- status:: draft

#### Acceptance Criteria

- `.rhizome/config.yml` sets the repository's default mode, `auto` or `notify`. With no setting, the default is `notify`.
- `rzm tasks mode <id> auto|notify|off` overrides one task for the person, and `rzm tasks mode <id> default` removes the override. Overrides live in task state.
- Tasks run only in a repository the person has trusted, the same check Rhizome Desktop applies before running a repository's pinned executable.
- The first time a person's Rhizome sees a task, it adds an informational inbox item naming the task, what it does, and its mode, so a new task never runs unannounced. In auto mode, the task runs without waiting for the person to read it.
- `rzm tasks pause` stops all background work for the person in the repository until `rzm tasks resume`.

### US3 - Run due tasks once, whatever triggers them

- id:: ^SPEC-0127-US3
- summary:: While Rhizome Desktop or any headless runtime is up, due tasks run once per person and repository, even with many worktrees open.
- status:: draft

#### Acceptance Criteria

- `rzm tasks run-due` is a one-shot command. It handles every task that is due, then exits.
- While a Rhizome Desktop host holds the runtime on a machine where the person turned on "Run tasks on this machine" ([[vault-runtime-coordination#^SPEC-0104-US7|SPEC-0104 US7]]), the runtime checks the wall clock every 30 seconds and runs `rzm tasks run-due --host` as a separate process when something is due. The check does not count as client activity, and a run in progress survives the runtime exiting. A runtime with no host dispatches nothing.
- Task state lives in the main checkout's ignored `.rhizome/task-state/`, keyed by the repository's Git common directory. Every worktree of one repository shares one clock.
- One repository-wide lock serializes all checks and background work for a repository. It is held from resolving the primary ref through publishing the result, and reuses `pkg/vault/indexlock`'s ownership and reclamation. A trigger that finds the lock held skips this tick.
- A task missed while the machine slept or Rhizome was closed runs once at the next check, within its `late` window for a scheduled slot (US9). Missed runs are not replayed. When several tasks are due, the one with the nearest deadline runs first.

### US4 - Check, then run the prompt only when needed

- id:: ^SPEC-0127-US4
- summary:: A check decides whether the task is needed and passes context to the prompt, and background work runs in its own worktree and ends in a local, tagged commit.
- status:: draft

#### Acceptance Criteria

- A check runs in a Rhizome-managed detached checkout (`.rhizome/task-state/checkout`), which is forcibly reset and cleaned to the resolved commit before each check. The check runs with stdin closed and a timeout. It prints one JSON object: `{"needed": false}`, or `{"needed": true, "summary": "...", "context": "..."}`. Anything else fails the run. A task with no check is needed whenever it is due.
- A check and its prompt receive the same locations: the repository's main checkout, the resolved commit, and the task's private data folder. A prompt receives them in its preamble, so work started later, including with Start or `--print`, can read what the check stored.
- Background work creates the branch `rzm/<task id>/<YYYY-MM-DD>`, adding a numeric suffix when that name is taken, and a worktree at the resolved commit under the main checkout's `.rhizome/task-state/worktrees/`. It runs the prompt there through `pkg/harness`, in the harness's full-access mode, with the check's context and a preamble asking it to end with a message for the person.
- When the work finishes, Rhizome commits every change in the worktree with the work's message and the task tag. Commits the work made itself are kept.
- Rhizome records the token usage the harness reports for each run.
- Rhizome runs every check and every prompt run in its own process group. When the task's timeout passes, it terminates the whole group, including harness processes, before releasing the lock.
- A failed run or a timeout creates a needs-attention item that gives the reason, and the task waits for its next cadence rather than retrying on every tick. The person can retry it from the item.
- A local test captures HEAD and effective working-tree contents through a private Git index before parsing its definition. Unstaged contents win, while tracked dependencies, deletions, executable modes, symlinks, and gitlinks keep their Git meaning. Untracked ignored files are excluded. The author's checkout and index are unchanged. The capture becomes an unsigned snapshot commit and a detached test worktree; it is the baseline for changed-file reporting.
- Runner-owned Git index and checkout operations disable repository hooks for that invocation, without changing the repository's hook configuration.
- A test's command check and agent work run from that snapshot. A code-mode check uses script bytes from the snapshot against the selected checkout's existing indexed vault. If the selected checkout has no index, the test fails with a clear setup message rather than starting an index or embedding work. Contextual queries therefore read the selected live index, not a historical snapshot of that index. Code-mode test children disable runtime autostart so an existing offline index cannot trigger indexing catch-up.
- A test receives fresh `RZM_TASK_DATA_DIR` data for each attempt and `RZM_TASK_TEST=1` in its checks and agent subprocesses. It creates no ordinary task inbox item, branch, commit, or task tag. Its outcome, errors, changed files, and transcript remain available as test history. The same repository execution lock serializes tests and scheduled work; a test may delay a tick, but never advances a scheduled clock or suppresses future work.
- A draft file must belong to the same canonical Git repository as the selected checkout. The HTTP test operation accepts a discovered task ID, never an arbitrary filesystem path, and performs the same trust and busy checks as the CLI.

### US5 - Start a task in the person's harness, in its own worktree

- id:: ^SPEC-0127-US5
- summary:: A person starts a due task interactively and lands in their own harness, in a worktree that harness creates and manages, on a branch from the primary ref.
- status:: draft

#### Acceptance Criteria

- `rzm tasks start <item>` opens the person's harness interactively from the main checkout, in a new worktree the harness creates and manages: `claude --worktree <name> "<prompt>"` for Claude Code and `codex --worktree "<prompt>"` for Codex. The harness owns the worktree's location and cleanup.
- The harnesses start worktrees from different commits: Claude Code from the remote's default branch, or from local HEAD when its `worktree.baseRef` setting is `head`, and Codex from local HEAD. The preamble's first step is therefore `git switch -c <branch> <resolved commit>`, where the branch is `rzm/<task id>/<YYYY-MM-DD>` with a numeric suffix when that name is taken. That is safe in a worktree that has no changes yet.
- The rest of the preamble says: confirm the task is still needed, add the task tag to every commit, and stop without a pull request if nothing is left to do.
- When Claude Code has not been trusted in the main checkout, `start` relays its error and tells the person to run `claude` there once.
- `rzm tasks start <item> --print` prints the full prompt and changes nothing, for people who start agents some other way, such as T3 Code.
- An item becomes started only after the harness launches.
- Before launching, `start` checks for an open pull request whose commits carry the same task tag (using `gh` when available). If one exists, it names that pull request and asks whether to continue.

### US6 - See and act on tasks in the inbox

- id:: ^SPEC-0127-US6
- summary:: A person sees due tasks and finished runs in Rhizome Desktop and the CLI, acts on them, and items that are finished or handled by someone else close on their own.
- status:: draft

#### Acceptance Criteria

- An item's states:
  - **due:** a notify-mode task is needed.
  - **running:** its work is in progress.
  - **ready:** background work finished with changes.
  - **started:** the person used `start`.
  - **needs attention:** a run failed or timed out.
  - **done** or **dismissed:** the end.
- A check that finds nothing needed records a run and creates no item. Background work that finishes without changes moves its item to done, keeps the work's message in the item's history, and removes the empty worktree and branch.
- A due item offers Run, Start, Snooze, and Skip.
- A ready item shows the work's message, the changed files compared with the resolved commit, the tokens used, and the worktree's path. It offers Open worktree and Discard. Discard removes the worktree and its local branch.
- An item becomes done when a commit carrying its task tag reaches the primary ref after the item was created, whoever made it, or when the person marks it done with `rzm tasks done <item>`.
- `rzm tasks inbox` lists items. `run`, `start`, `snooze`, `skip`, `discard`, and `done` act on one.
- A due item stays current: when its task is due again, the check runs again. `needed: false` closes the item as done; `needed: true` refreshes its summary and context in place and keeps any snooze. Items that are running, ready, or started still keep their task from checking, and a re-check never creates a second item or starts work.
- Any open item's prompt can be copied, for agents started some other way (`GET /api/tasks/items/{id}/prompt` and the Tasks page's Copy prompt). The copied prompt carries the task prompt, the check's context, and the locations, without Start's `git switch` step, so pasting it into a session in the main checkout never moves its branch.
- Rhizome Desktop lists items for the open repository, shows a badge with the number that need the person, sends one system notification per new due, ready, or needs-attention item, and switches to a ready item's worktree with Open worktree. It reads and changes items through the runtime.

### US7 - Keep each task's clock

- id:: ^SPEC-0127-US7
- summary:: A task comes due from the latest run that counts for its scope, read from local records and the primary ref's history.
- status:: draft

#### Acceptance Criteria

- A `person` task is due when the person's latest local run, including a check that found nothing needed, is older than its cadence.
- A `team` task is due when both the person's latest local run and the latest commit on the primary ref carrying the task's tag are older than its cadence. Each commit is dated by its committer date. The tag is matched as a whole line anywhere in the message (`git log --grep='^Rhizome-Task: <id>$'`), because squash merges move trailers into the body.
- `rzm tasks status` shows, per task, its mode and where the mode came from, its last run and who made it, its recent token usage, when it is next due, the primary ref and how long ago it was fetched, and any problem with the repository's merge settings (see Edge Cases).

### US8 - See registered tasks and what each run did

- id:: ^SPEC-0127-US8
- summary:: A person opens Tasks in the workbench or Rhizome Desktop, sees every registered task and when it is next due, and reads any recent run as a transcript: the outcome first, then the check log, thinking, commands, and tool calls, collapsed until opened.
- status:: draft

#### Acceptance Criteria

- Every run, including a check that found nothing needed, gets a run ID before its check starts. Its folder under task state holds the check's stderr log and, when agent work ran, a transcript.
- A run record keeps its own outcome, start and finish times, tokens, inbox item, branch, base commit, worktree, and changed files, so an old run never shows a newer attempt's work.
- A run interrupted by a crash is recorded once as interrupted, with whatever transcript it wrote. The page reports it as interrupted as soon as the process holding the run lock is gone.
- The transcript holds whole thinking blocks and messages, each command and tool call with its outcome, file changes, errors, the agent's final message, and token usage, for both Claude Code and Codex. Text and inputs are bounded and marked when cut; the final message and usage are always kept.
- A retry notice from the harness does not fail a run, and a turn the harness reports as failed does, even without a separate error.
- Per task, the 20 newest transcripts and 20 newest check logs are kept, along with at most 100 other run records. Token totals for the last 30 days stay correct after older records are trimmed.
- Test runs (`rzm tasks run --file` or **Test once**) are stored separately from scheduled history and token totals. They appear chronologically with a Test label, source checkout, and snapshot. Check-only, not-needed, failed, and interrupted tests remain inspectable. Retention has a separate bounded test quota and removes a retained test worktree only through its owned cleanup path.
- A scheduled task opens on its most recent scheduled run. Its local-version section still offers Test once when the YAML is unchanged, because a helper may have changed. An invalid local definition displays its error and disables testing without hiding scheduled status.
- The Tasks page lists each registered task with its mode, cadence, last outcome, next due time, and inbox state. A task's history lists its runs newest first, with checks that found nothing needed shown as quiet single lines.
- A run's view shows its final message as Markdown, changed files, tokens, branch, and worktree path with a copy action. Under that is a chronological timeline. Thinking, commands (with exit code, duration, and output), tool calls, file changes, and the check log are collapsed by default.
- While a run is in progress, the page shows completed blocks as they arrive. Run on an inbox item starts the work in a separate process and returns at once; a failure before the work starts appears on the item.
- Rhizome Desktop shows Tasks as a sidebar section.

### US9 - Schedule tasks by the wall clock

- id:: ^SPEC-0127-US9
- summary:: A task can run at fixed local times on chosen days, or hourly within a window, instead of drifting with "last run plus a duration".
- status:: draft

#### Acceptance Criteria

- A task declares `every: <duration>` (unchanged: due when the latest counting run is older) or `schedule`, never both. `schedule` is one entry or a list of entries. An entry has `at` (`"HH:MM"` or a list of them) or `every` plus `between: ["HH:MM", "HH:MM"]`; optional `days` (`daily`, the default, `weekdays`, `weekends`, or a list of `mon`…`sun`); optional `timezone` (an IANA name); and optional `late` (a duration, default 1h).
- A slot is due when its time has passed in the task's timezone (the entry's, else the person's `tasks.timezone`, else the host's zone) and no run of the task started at or after the slot. A missed slot runs only if the check happens within `late` of it, and missed slots collapse into one run. Daylight-saving gaps run at the first valid instant after the gap, and a repeated hour runs once.
- `rzm tasks status` shows the next slot and the timezone it uses. RRULE and cron syntax are rejected.
- A task file using any field this version does not know fails to parse, so starters that use `schedule` state a minimum Rhizome version.

### US10 - Choose where tasks run and how

- id:: ^SPEC-0127-US10
- summary:: Each person chooses, per machine and repository, whether scheduled tasks run there, and which harness, model, and effort each task uses, without changing the task file.
- status:: draft

#### Acceptance Criteria

- Per-person, per-machine settings live in the user configuration file, never in a task file: per repository, keyed by its Git common directory, `runHere` (default off on every machine) and the checkout registered for task running; and per task, `harness`, `model`, and `effort`, which override the person's chat agent settings for that task only. A person-wide `tasks.timezone` sets the default schedule zone.
- `rzm tasks machine on|off` and the Tasks page change `runHere` for the selected repository. `rzm tasks hosts --json` lists the repositories with `runHere` on, for the Desktop host loop.
- `rzm tasks run-due --host` runs nothing when `runHere` is off. `rzm tasks status` explains, per task, why it is not running: not run on this machine, no Desktop host, untrusted repository, paused (and until when), off, an open item blocking it, the checkout on the wrong branch, a missing requirement, or a failed fetch. An untrusted repository is reported once, not skipped silently.
- `rzm tasks pause --until <time>` pauses until that time; `--for 1h` is accepted.
- `rzm tasks run-due` children keep their stdout and stderr in the run's folder instead of discarding them.

### US11 - Check what a task needs before spending tokens

- id:: ^SPEC-0127-US11
- summary:: A task declares the commands and MCP servers it needs; Rhizome checks them before any model call, task sessions load the person's own MCP configuration, and `rzm tasks doctor` explains what is missing.
- status:: draft

#### Acceptance Criteria

- `requires: {commands: [...], mcp: [...]}` names commands that must resolve on the login shell's PATH and MCP servers the person's harness must have configured. A missing requirement fails the run as needs attention before the check or prompt runs.
- Task sessions load the person's own MCP configuration for both Claude Code and Codex. Claude Code task sessions no longer run with an empty strict MCP configuration.
- Optional `deny: [...]` names tools the harness must refuse, passed as the session's disallowed tools.
- `rzm tasks doctor [task]` reports, per task: `requires` commands on the login PATH, harness sign-in, MCP server reachability, repository trust, how fresh the primary ref is, `git push --dry-run` when push is on, and whether Obsidian Git auto-commit is enabled in the host checkout.

### US12 - Land changes in the open checkout

- id:: ^SPEC-0127-US12
- summary:: An in-place task runs in the checkout Rhizome has open on the host and commits only the files it declared it writes, leaving every human change alone.
- status:: draft

#### Acceptance Criteria

- `land: in-place` or `land: pr`. An in-place task must declare `writes:`, a list of repository-relative glob patterns. `land` defaults to `in-place` when the task declares `writes` and to `pr` otherwise, so task files written before `writes` existed keep landing in a worktree. A task file whose `land` or `writes` widens compared with the version the person last saw drops to notify until the person re-approves it.
- Before the run: if the checkout is not on the local branch matching the primary ref, the task is skipped with one deduplicated notice. Otherwise Rhizome fetches and runs `git merge --ff-only`, even with dirty files, then snapshots HEAD and the working tree as a Git tree through a private index, plus the set of paths dirty or staged before the run.
- After the run, the task's changes are the paths that changed during the run, were clean before, and match `writes`. Rhizome commits them with `git commit --only -- <paths>`, leaving pre-staged changes untouched, with the task tag and a `Rhizome-Task-Slot:` trailer naming the slot. It rechecks the branch and HEAD at commit time.
- Paths outside `writes`, and paths dirty before the run, stay uncommitted and are named in the run summary.
- A protected path (`.agents/**`, `.rhizome/**`, unless listed in `writes`) changed during the run is restored to its snapshot content at once, the diff is saved under the run's folder, and one needs-attention item is raised.
- If HEAD moved during the run, Rhizome does not commit; it records the files and raises one notice.
- A failed or stopped run leaves its changes in place and records its changed paths; the next successful run of that task treats them as its own.
- Known limit: a person's edit to a clean file inside `writes` during the run cannot be told apart from the task's and is committed with it.
- Each run records its landing mode. Worktree cleanup applies only to worktrees Rhizome created: no-change cleanup and Discard never touch the open checkout, and Discard on an in-place item dismisses the notice only.

### US13 - Push task commits, or open a pull request

- id:: ^SPEC-0127-US13
- summary:: When the repository and person allow it, Rhizome pushes an in-place task's commit or a PR task's branch, recovering safely from races with other pushers.
- status:: draft

#### Acceptance Criteria

- Push needs both the repository's `tasks.allowPush: true` and the person's per-machine push setting.
- In place: Rhizome pushes the recorded commit when it fast-forwards the remote branch. After an ambiguous failure it fetches and looks for the slot trailer before retrying. When the remote diverged, it runs `git pull --rebase` only when the tracked tree is clean, aborting on conflict; otherwise it keeps the local commit and raises one notice. It never rebases with dirty files, autostashes, or force-pushes.
- `land: pr`: the worktree path in US4, then Rhizome pushes the branch and opens a pull request with `gh`, carrying the task tag in its body.
- Slot trailers deduplicate across machines: a pass that finds the slot's trailer on the primary ref treats the slot as done.

### US14 - Remember, stay quiet, and alert off-machine

- id:: ^SPEC-0127-US14
- summary:: A task can keep a short handoff note between runs, report whether a run needs the person, alert the person off-machine on failures, and stay under an optional daily token cap.
- status:: draft

#### Acceptance Criteria

- With `memory: true`, the preamble names `$RZM_TASK_DATA_DIR/handoff.md` (about 8 KB at most). The agent reads it first and rewrites it last; Rhizome trims it to the cap.
- The agent may write `$RZM_TASK_RESULT_FILE` as JSON `{"status": "quiet" | "ok" | "attention", "summary": "..."}`. Not-needed, no-change, quiet, and ok runs record history only; attention, conflicts, and failures always create an inbox item and notify.
- A per-person `tasks.notify.command` (an argument list) runs after each run that notifies, with the summary on stdin and the task, outcome, and run ID in its environment, for alerts that reach the person off-machine. Its failure is recorded and never fails the run.
- An optional per-person `dailyTokenCap` per repository stops new agent work once that day's tokens, from the existing day buckets, reach it, and `status` says so.

## Requirements

- MUST run a task's prompt in the background only in auto mode, or when the person chose Run. Checks make no model calls.
- MUST NOT push unless the repository sets `tasks.allowPush: true` and the person turned push on for the repository on this machine. MUST NOT merge pull requests, force-push, autostash, or rebase with a dirty tracked tree.
- MUST use the resolved primary ref's definition for scheduled execution, and run local versions only through an explicit test in a repository the person trusted.
- MUST keep task state outside every Git-tracked path, readable only by the person on macOS and Linux.
- MUST fetch the primary ref's remote before resolving it for a pass with due tasks, and fall back to the last fetched ref, reported by `status`, when the fetch fails. Deciding whether anything is due makes no network call.
- MUST resolve each check's interpreter the way the person's login shell does, because a runtime started by the desktop app or launchd lacks the shell's PATH, and version-manager shims such as mise fail without their environment.
- MUST run the runtime's own `rzm` for a check command named `rzm`, and put that binary first on the PATH of checks and agent work. The runtime runs with repo delegation off, and its subprocesses inherit that, so a bare `rzm` would otherwise run whichever `rzm` the shell finds first.
- SHOULD keep deciding what is due cheap enough to run every tick.

## Edge Cases

- **Repository with no main checkout, such as a bare-repository layout:** task state falls back to Rhizome's per-user state folder, keyed by the Git common directory.
- **Primary ref missing or unresolvable:** no task runs, and `status` explains why.
- **Merge settings that drop the tag:** merge commits and rebase merges keep the original commits. A squash merge keeps commit messages when the repository squashes with commit messages (GitHub's `COMMIT_MESSAGES`), but drops them when it squashes with the pull request title alone. Then a `team` task's merged runs don't reset anyone's clock. `status` reports this when it can read the setting with `gh`.
- **Person has no signed-in harness:** prompt runs fail with a needs-attention item. Checks are unaffected.
- **Claude Code not yet trusted in the main checkout:** `start` fails with Claude's error until the person runs `claude` there once (US5). Background work through `pkg/harness` is unaffected.

## Dependencies and Assumptions

- [[coding-agent-harness|SPEC-0101]] provides harness selection, sessions, and full-access mode. One harness change is needed: the JSON stdio transport stops only its direct child process, and it must stop the whole process group.
- The runtime lifecycle (`pkg/app/runtime`, `pkg/app/cli/serve`) hosts the interval trigger.
- [[custom-view-apps|SPEC-0105]] is how a starter can later add a view that explains a task's outcome.
- Verified 2026-10-25 on macOS:
  - Claude Code 2.1.302 and Codex 0.163.0 both reach their saved logins from a LaunchAgent.
  - A mise `node` shim fails under launchd's PATH; a login shell finds `node`.
  - `claude --worktree <name> "<prompt>"` starts from the remote's default branch and answers the prompt, once the folder is trusted.
  - `codex --worktree` starts from the current local commit, detached.
  - This repository squashes with `COMMIT_MESSAGES`, so commit trailers survive.

## Delivery Phases

- **Phase 1, core and CLI:** US1–US5, US7, and the CLI part of US6, triggered by `rzm tasks run-due` and the runtime interval, plus the harness process-group change. A synthetic task with a check and a prompt proves the whole path: the check stores context, background work uses it and produces a ready branch, and `start --print` carries the same locations.
- **Phase 2, task history and desktop:** US8, the inbox actions on the Tasks page (Run, Snooze, Skip, Discard, Done, mode, pause), and the Rhizome Desktop part of US6: the Tasks section, the badge, notifications, Open worktree, and Start (Terminal running `rzm tasks start <item>`).
- **Phase 3, hosting and landing (EFF-2026-11-22-11-38):** US9–US14 and the Desktop menu bar host ([[vault-runtime-coordination#^SPEC-0104-US7|SPEC-0104 US7–US8]]).
- **Later:** a launchd or systemd host install for machines without the app; Windows; a branch-versus-primary diff view in Rhizome Desktop; starter-provided outcome views; adopting tasks in session memory.

## Decisions

- **Task sources (2026-10-25):** both starter tasks and custom tasks, as files in `.rhizome/tasks/`. Hooks take a different approach and accept declarations only from the binary's built-in registry. Tasks can be repository files because a task runs only from the reviewed primary ref.
- **Local tests (2026-11-03):** scheduled execution keeps the primary-ref contract. Authors can deliberately test the selected checkout's task and supporting files before merge. Test records, task data, and output are distinct from scheduled work; tests do not create task-tagged commits. Code-mode checks reuse the selected checkout's existing index to avoid unexpected indexing cost.
- **Worktrees (2026-10-25):** interactive work uses the person's harness's own worktrees (US5). Background work uses worktrees Rhizome creates, because Rhizome must find them to show the outcome (US4, US6).
- **Trust (2026-10-25):** tasks are trusted code, like hooks. Rhizome does not sandbox them or detect what they change outside their worktree.
- **Cost (2026-10-25, amended 2026-11-22):** Rhizome does not meter tasks beyond an optional per-person daily token cap (US14). The question for a person is whether they want a task to happen, answered by its mode. An expensive task is fixed by its author or turned off.
- **Modes (2026-10-25):** the repository sets the default mode, and each person can override it per task. Automatic agent work is the point of the feature; notify exists for people and repositories that want to approve each run.
- **Trigger (2026-10-25):** Rhizome Desktop, through the runtime, is the usual trigger. A launchd or systemd job comes later.
- **Hosting and landing (2026-11-22):** Drew approved, after two council rounds, that Rhizome is the only runner, hosted on a Mac by Rhizome Desktop from the menu bar; where tasks run is a per-person, per-machine setting; landing is `in-place` by default or `pr`; pushing is allowed when both the repository and the person opt in; schedules use a small wall-clock form, not RRULE or cron. This supersedes the 2026-10-25 non-goals on pushing and metering.

## Open Questions

- Whether `codex --worktree "<prompt>"` passes its prompt into the first turn of an interactive session. Its docs show a prompt only with `codex exec --worktree`. Verify this before building US5.

## Documentation Plan

- A vault reference note for task files, modes, and `rzm tasks` commands, written when phase 1 lands.
- When session memory adopts tasks, [[session-memory-starter|SPEC-0124]] removes "Scheduled or background runs" from its non-goals, runs mining from a task's prompt, and moves its mining state into the task's private data folder.
