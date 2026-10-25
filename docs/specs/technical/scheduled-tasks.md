---
type: TechnicalSpec
id: SPEC-0126
aliases:
  - SPEC-0126
  - Scheduled tasks
summary: "Repositories declare periodic agent tasks: an optional free pre-flight check that decides whether work is needed, then a prompt run by each person's own agent harness. A repository sets whether tasks run automatically or only notify, and each person can override that per task. Background runs work in their own worktree, never push, and report back with a message and the changed files. Git history is the team's shared clock, and run records stay in each person's ignored local state, so teammates using different harnesses need no coordination."
spec-status: proposed
last-updated: 2026-10-25
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

The repository sets the default mode for its tasks, and each person can override it per task, including turning a task off. Cost is the task author's responsibility: a task should be well structured, use its check to avoid unneeded work, and keep its prompt efficient. A task that costs too much is a problem with the task, to be fixed or turned off, not something Rhizome meters.

A background run works in a worktree that Rhizome creates from the primary branch. When it finishes, Rhizome commits whatever it changed to a local branch, tagged with the task. Nothing is pushed and no pull request is opened. The run ends with a message for the person and the list of changed files. The person reviews the worktree, for example by switching to it in Rhizome Desktop, then opens the pull request or discards the branch.

Coordination needs no shared service:

- **Git history is the team clock.** A task's commits carry a task tag. When a tagged change reaches the primary branch, every teammate's clock for that task resets.
- **Run records are local.** Each person's runs, including checks that found nothing to do, are recorded in ignored state that every worktree of the repository shares.
- **Duplicates are harmless.** Two people occasionally doing the same chore both still close the gap with the latest primary branch, so Rhizome does not lock across machines.

Tasks are trusted repository code, reviewed on the primary branch, the same trust that hooks get. Rhizome neither sandboxes them nor polices what they change.

## Goals

- Let a repository declare periodic agent tasks once and have them work for teammates using Claude Code, Codex, or both.
- Let agent work happen automatically, or with one click, without anyone remembering to ask for it.
- Let a repository choose whether its tasks run automatically or only notify, and let each person override that per task.
- Do agent work only when a free check says the task is needed.
- Put every change that reaches the primary branch behind a person's review and a pull request.
- Run each due task once per person and repository, however many worktrees, runtimes, or triggers are active.
- Make the shared clock Git history, so a teammate's merged run counts without any coordination service.

## Non-Goals

- Installing tasks into Claude Code's or Codex's own schedulers, or into cloud routines.
- Pushing, opening pull requests, or merging without a person.
- Metering or capping what a task spends. Rhizome shows what each run used, as the harness reports it; keeping a task affordable is its author's job.
- Sandboxing tasks, or detecting what they change outside their own worktree.
- Locks or claims across machines, or any central store of run state.
- Building the session-memory and dream tasks, or changing [[session-memory-starter|SPEC-0124]]. This spec provides the capability.
- Findings with per-item identity, such as "this note is stale". A check reports whether the task is needed and passes context to the prompt.

## Key Entities

- **Task:** a YAML declaration with an id, a cadence, a scope (`person` or `team`), an optional check, an optional prompt, and an optional timeout. A task needs a check, a prompt, or both.
- **Primary ref:** the branch tasks are read from and work starts from. It defaults to the remote's default branch (`origin/HEAD`) and can be overridden in `.rhizome/config.yml`, for example for teams that work on `dev`. Each run resolves it to one commit first and uses that commit throughout.
- **Mode:** auto, notify, or off. The repository's default applies unless the person overrides it for a task.
- **Task state:** ignored local state for one person and repository: mode overrides, run records, usage, inbox items, and each task's private data folder.
- **Inbox item:** a task that needs the person. Its state is due, running, ready, started, needs attention, done, or dismissed (US6).
- **Task tag:** the line `Rhizome-Task: <task id>` in a commit message.

## User Stories

### US1 - Declare a task once for the whole repository

- id:: ^SPEC-0126-US1
- summary:: A maintainer commits a task declaration, and every teammate's Rhizome reads the same definition from the primary ref.
- status:: draft

#### Acceptance Criteria

- A task is a YAML file in `.rhizome/tasks/`. The folder holds two kinds, read the same way:
  - **Starter tasks** are installed by `rzm init` as a managed asset type, the same way starters install views. `rzm init` refreshes them.
  - **Custom tasks** are written by the repository's own team and left alone by `rzm init`.
- `rzm init` adds `tasks/` to the managed `.rhizome/.gitignore` allowlist.
- A declaration names its check, as an argument list or a code-mode script run with `rzm agent code execute`; its prompt, as text; and its cadence, scope, and timeout. The timeout defaults to one hour.
- Rhizome reads task files from the resolved primary ref through Git objects, without a checkout. Editing a task file on a branch never changes what runs. `rzm tasks run <id> --file <path>` runs a draft once by hand.
- An invalid task file is reported by `rzm tasks status` and by validation, and does not prevent other tasks from running.

### US2 - Choose whether tasks run automatically or notify

- id:: ^SPEC-0126-US2
- summary:: A repository sets the default mode for its tasks, and each person can override it per task or pause everything.
- status:: draft

#### Acceptance Criteria

- `.rhizome/config.yml` sets the repository's default mode, `auto` or `notify`. With no setting, the default is `notify`.
- `rzm tasks mode <id> auto|notify|off` overrides one task for the person, and `rzm tasks mode <id> default` removes the override. Overrides live in task state.
- Tasks run only in a repository the person has trusted, the same check Rhizome Desktop applies before running a repository's pinned executable.
- The first time a person's Rhizome sees a task, it adds an informational inbox item naming the task, what it does, and its mode, so a new task never runs unannounced. In auto mode, the task runs without waiting for the person to read it.
- `rzm tasks pause` stops all background work for the person in the repository until `rzm tasks resume`.

### US3 - Run due tasks once, whatever triggers them

- id:: ^SPEC-0126-US3
- summary:: While Rhizome Desktop or any headless runtime is up, due tasks run once per person and repository, even with many worktrees open.
- status:: draft

#### Acceptance Criteria

- `rzm tasks run-due` is a one-shot command. It handles every task that is due, then exits.
- While a runtime is up, it runs `rzm tasks run-due` as a separate process on an interval. Rhizome Desktop keeps a runtime up while it is open, so the desktop app is the usual trigger. The interval does not count as client activity, so a headless runtime still exits when idle, and a run in progress survives the runtime exiting.
- Task state lives in the main checkout's ignored `.rhizome/task-state/`, keyed by the repository's Git common directory. Every worktree of one repository shares one clock.
- One repository-wide lock serializes all checks and background work for a repository. It is held from resolving the primary ref through publishing the result, and reuses `pkg/vault/indexlock`'s ownership and reclamation. A trigger that finds the lock held skips this tick.
- A task missed while the machine slept or Rhizome was closed runs once at the next tick. Missed runs are not replayed.

### US4 - Check, then run the prompt only when needed

- id:: ^SPEC-0126-US4
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

### US5 - Start a task in the person's harness, in its own worktree

- id:: ^SPEC-0126-US5
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

- id:: ^SPEC-0126-US6
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
- Rhizome Desktop lists items for the open repository, shows a badge with the number that need the person, sends one system notification per new due, ready, or needs-attention item, and switches to a ready item's worktree with Open worktree. It reads and changes items through the runtime.

### US7 - Keep each task's clock

- id:: ^SPEC-0126-US7
- summary:: A task comes due from the latest run that counts for its scope, read from local records and the primary ref's history.
- status:: draft

#### Acceptance Criteria

- A `person` task is due when the person's latest local run, including a check that found nothing needed, is older than its cadence.
- A `team` task is due when both the person's latest local run and the latest commit on the primary ref carrying the task's tag are older than its cadence. Each commit is dated by its committer date. The tag is matched as a whole line anywhere in the message (`git log --grep='^Rhizome-Task: <id>$'`), because squash merges move trailers into the body.
- `rzm tasks status` shows, per task, its mode and where the mode came from, its last run and who made it, its recent token usage, when it is next due, the primary ref and how long ago it was fetched, and any problem with the repository's merge settings (see Edge Cases).

## Requirements

- MUST run a task's prompt in the background only in auto mode, or when the person chose Run. Checks make no model calls.
- MUST NOT push, open pull requests, or merge on the person's behalf.
- MUST run a task only from the resolved primary ref's definition, and only in a repository the person trusted.
- MUST keep task state outside every Git-tracked path, readable only by the person on macOS and Linux.
- MUST NOT fetch from the network to decide what is due. The primary ref is as fresh as the person's last fetch.
- MUST resolve each check's interpreter the way the person's login shell does, because a runtime started by the desktop app or launchd lacks the shell's PATH, and version-manager shims such as mise fail without their environment.
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
- **Phase 2, desktop:** the Rhizome Desktop part of US6: the inbox, badge, notifications, and Open worktree.
- **Later:** a launchd or systemd job for runs while Rhizome is closed; Windows; a branch-versus-primary diff view in Rhizome Desktop; starter-provided outcome views; adopting tasks in session memory.

## Decisions

- **Task sources (2026-10-25):** both starter tasks and custom tasks, as files in `.rhizome/tasks/`. Hooks take a different approach and accept declarations only from the binary's built-in registry. Tasks can be repository files because a task runs only from the reviewed primary ref.
- **Worktrees (2026-10-25):** interactive work uses the person's harness's own worktrees (US5). Background work uses worktrees Rhizome creates, because Rhizome must find them to show the outcome (US4, US6).
- **Trust (2026-10-25):** tasks are trusted code, like hooks. Rhizome does not sandbox them or detect what they change outside their worktree.
- **Cost (2026-10-25):** Rhizome does not meter or cap tasks. The question for a person is whether they want a task to happen, answered by its mode. An expensive task is fixed by its author or turned off.
- **Modes (2026-10-25):** the repository sets the default mode, and each person can override it per task. Automatic agent work is the point of the feature; notify exists for people and repositories that want to approve each run.
- **Trigger (2026-10-25):** Rhizome Desktop, through the runtime, is the usual trigger. A launchd or systemd job comes later.

## Open Questions

- Whether `codex --worktree "<prompt>"` passes its prompt into the first turn of an interactive session. Its docs show a prompt only with `codex exec --worktree`. Verify this before building US5.

## Documentation Plan

- A vault reference note for task files, modes, and `rzm tasks` commands, written when phase 1 lands.
- When session memory adopts tasks, [[session-memory-starter|SPEC-0124]] removes "Scheduled or background runs" from its non-goals, runs mining from a task's prompt, and moves its mining state into the task's private data folder.
