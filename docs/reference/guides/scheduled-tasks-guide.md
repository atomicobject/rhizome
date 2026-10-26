---
type: ReferenceDoc
summary: "How to declare scheduled tasks in .rhizome/tasks, choose whether they run automatically or notify, and use the rzm tasks commands and inbox."
reference-kind: guide
---

# Scheduled tasks

Last verified: 2026-10-25

Scheduled tasks are periodic agent work that a repository declares once and that runs on each teammate's machine, in that person's own agent harness (Claude Code or Codex). The contract is [[scheduled-tasks|SPEC-0127]].

A task has up to two parts:

- **A pre-flight check (optional).** A command or a code-mode script that makes no model calls. It decides whether the task is needed now and passes context to the prompt.
- **A prompt.** Agent work that runs in your harness. It can tell the agent to run commands, such as a starter's scripts.

## Declare a task

Commit a file to `.rhizome/tasks/<id>.yml` on the primary branch. The id is the file name: lowercase letters, digits, and hyphens.

```yaml
name: Finalize notes                # shown in the inbox
description: Replaces draft markers # optional
every: 1d                           # Go duration, or whole days (7d) or weeks (2w)
scope: person                       # person (default) or team
check:                              # optional: command or script, not both
  command: [sh, scripts/needs-finalizing.sh]
  # script: .rhizome/tasks/finalize-check.js
prompt: |
  Do what the pre-flight check says.
timeout: 1h                         # optional, applies to the check and the prompt each
```

Rhizome reads task files from the primary branch's history, never from the files you have checked out. Editing a task on a branch changes nothing until it merges. To try a draft, run it once by hand: `rzm tasks run --file .rhizome/tasks/finalize.yml`.

`rzm validate tasks` (also part of `rzm validate all`) checks the task files you have checked out and reports each one that does not parse, such as a bad id, an unknown field, an invalid `every` or `timeout`, or an id that another file also declares. `rzm tasks status` reports the same problems for the primary branch.

Starters can ship task files the way they ship views; `rzm init` installs and refreshes those. Task files your team writes are left alone.

### The check

A command check runs in a Rhizome-managed checkout of the primary branch, reset before each run, with your login shell's `PATH`, no input, and the task's timeout. A script check is read from the primary branch and run with `rzm agent code execute` against the main checkout's vault, with the locations below as its input.

Either kind prints one JSON object:

```json
{"needed": true, "summary": "3 notes need finalizing", "context": "notes/a.md notes/b.md"}
```

or `{"needed": false}`. A task with no check is needed whenever it is due.

Checks and prompts receive the same locations: `RZM_TASK_ID`, `RZM_TASK_MAIN_CHECKOUT`, `RZM_TASK_COMMIT`, and `RZM_TASK_DATA_DIR`, a private folder that keeps the task's state between runs. Prompts receive them in their instructions.

### Cost is the task's job

Rhizome does not meter or cap what a task spends. Use the check so the prompt runs only when there is work, and keep the prompt efficient. `rzm tasks status` shows each task's recent token use; a task that costs too much should be fixed or turned off.

## Choose how tasks run

Each task runs in one of three modes:

- **auto:** when due and needed, the prompt runs in the background.
- **notify:** the task waits in your inbox until you run it or start it.
- **off.**

The repository sets the default in `.rhizome/config.yml`; without one, tasks notify.

```yaml
tasks:
  defaultMode: auto      # auto or notify
  primaryRef: origin/dev # optional; defaults to origin/HEAD
```

Override one task for yourself with `rzm tasks mode <task> auto|notify|off`, or return to the repository's default with `rzm tasks mode <task> default`. `rzm tasks pause` stops all background task work for you in the repository until `rzm tasks resume`.

Tasks run only in a repository you trusted with `rzm trust`. The first time Rhizome sees a task, your inbox gets a note naming it and its mode.

## When tasks run

While Rhizome Desktop or any headless `rzm` runtime is up for a vault inside a Git checkout, it starts `rzm tasks run-due` every 15 minutes. You can also run `rzm tasks run-due` yourself. Each due task runs once per person and repository, however many worktrees or runtimes are open. A task missed while Rhizome was closed runs once at the next pass.

A `person` task is due when your own last run is older than its cadence. A `team` task also counts teammates: when a commit tagged `Rhizome-Task: <id>` reaches the primary branch, everyone's clock resets. Squash merges keep the tag when the repository squashes with commit messages. They drop it when it squashes with the pull request title alone.

## Background runs

A background run, automatic or started with `rzm tasks run <item>`, works in a worktree that Rhizome creates under `.rhizome/task-state/worktrees/`, on the branch `rzm/<task>/<date>`. When it finishes, Rhizome commits every change with the agent's final message and the task tag. Nothing is pushed and no pull request is opened. A run that changes nothing removes its worktree and branch.

Review a finished run in its worktree, then open the pull request yourself, or discard it with `rzm tasks discard <item>`.

## The inbox

`rzm tasks inbox` lists what needs you:

| State | Meaning | Actions |
| --- | --- | --- |
| due | A notify-mode task is needed. | `run`, `start`, `snooze`, `skip` |
| ready | A background run finished with changes. | review, then `done` or `discard` |
| needs-attention | A check or run failed or timed out. It waits for its next cadence instead of retrying. | `run`, `start`, `discard`, `done` |
| started | You started it with `start`. | `done` |

`rzm tasks start <item>` opens your harness in a new worktree that the harness manages (`claude --worktree` or `codex --worktree`). The prompt's first step switches to a fresh `rzm/<task>/<date>` branch at the primary commit. `--print` prints that prompt without starting anything, for agents you start some other way. Claude Code and Codex each ask once before they work in a folder they have not seen; if Start reports that the harness exited at once, run `claude` or `codex` in the main checkout, accept, and start again.

An item closes by itself when a commit carrying its task's tag reaches the primary branch, whoever made it.

## Where state lives

Everything personal stays in the main checkout's ignored `.rhizome/task-state/`, shared by every worktree of the repository: modes, run history, inbox items, the check checkout, background worktrees, data folders, and logs. A repository without a main checkout keeps it in Rhizome's per-user configuration folder.
