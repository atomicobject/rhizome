---
type: ReferenceDoc
summary: "How to declare scheduled tasks in .rhizome/tasks or a module's tasks folder, choose whether they run automatically or notify, and use the rzm tasks commands and inbox; also how a module declares on-demand actions."
reference-kind: guide
---

# Scheduled tasks

Last verified: 2026-11-02

Scheduled tasks are periodic agent work that a repository declares once and that runs on each teammate's machine, in that person's own agent harness (Claude Code or Codex). The contract is [[scheduled-tasks|SPEC-0127]].

A task has up to two parts:

- **A pre-flight check (optional).** A command or a code-mode script that makes no model calls. It decides whether the task is needed now and passes context to the prompt.
- **A prompt.** Agent work that runs in your harness. It can tell the agent to run commands, such as a starter's scripts.

## Declare a task

Commit a file to `.rhizome/tasks/<id>.yml` on the primary branch. The id is the file name: lowercase letters, digits, and hyphens.

A task can also live in a module, at `.rhizome/modules/<module>/tasks/<name>.yml` ([[rhizome-modules|SPEC-0130]]). Its id is qualified with the module name, such as `compounding/sweep`, so it never collides with a flat task. Use the qualified id wherever a command takes a task, as in `rzm tasks mode compounding/sweep off`. Only files directly inside a `tasks` folder are tasks.

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

Scheduled execution reads task files from the primary branch's history. Editing a task on a branch does not change its schedule. The Tasks page also shows definitions in the selected checkout, including uncommitted new tasks, with their local version marked as new, different, unchanged, or invalid.

To try one before merging, use **Test once** on its local version or run `rzm tasks run --file .rhizome/tasks/finalize.yml`. The test captures the checkout's current source, including new nonignored helper scripts and uncommitted changes, into a detached worktree. It leaves your working files and staging area unchanged and compares the output against that capture. Invalid local definitions display their error; a valid scheduled version stays available.

Each test starts with fresh task data and has its own visible result and transcript. It does not change the task's next due time, mode, scheduled usage totals, or ordinary inbox items. Test output is not committed or tagged as scheduled work. A running test uses the repository's task lock, so scheduled work may wait until it finishes. Local-only tasks receive scheduling controls after they reach the primary branch.

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

For a test, `RZM_TASK_COMMIT` names the source snapshot and `RZM_TASK_DATA_DIR` is fresh for that attempt. `RZM_TASK_TEST=1` lets a task with its own private state choose separate test storage; the compounding module does this automatically. Command checks and agent work use the captured worktree. A code-mode test reads its captured script but queries the selected checkout's existing index; create that index first if it is missing. It does not automatically index a new test worktree.

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

A background run, automatic or started with `rzm tasks run <item>`, works in a worktree that Rhizome creates under `.rhizome/task-state/worktrees/`, on the branch `rzm/<task>/<date>`; a module task's branch is `rzm/<module>/<name>/<date>`. When it finishes, Rhizome commits every change with the agent's final message and the task tag. Nothing is pushed and no pull request is opened. A run that changes nothing removes its worktree and branch.

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

A due item stays current. Each time its task comes due again, Rhizome runs the check again: if nothing is needed now, the item closes as done; if it is still needed, its summary and context are refreshed in place, and any snooze is kept. A re-check never adds a second item or starts work, and a check that fails moves the item to needs attention. Running, ready, and started items keep their task from checking until they close.

An item closes by itself when a commit carrying its task's tag reaches the primary branch, whoever made it.

## The Tasks page

Open **Tasks** in the workbench, or the Tasks section in Rhizome Desktop, to see every registered task with its mode, cadence, last outcome, next due time, and inbox state. Pick a task to change its mode, act on its inbox item (Run, Copy prompt, Snooze, Skip, Discard, Done), and read its run history, newest first. Checks that found nothing to do appear as quiet single lines.

**Copy prompt** copies any open item's prompt, for an agent you start some other way. It carries the task's instructions, the check's findings, and the locations, but not Start's `git switch` step, so pasting it into a session in the main checkout never moves its branch. When the clipboard is unavailable, the page shows the prompt to copy by hand. The same text is served at `GET /api/tasks/items/<id>/prompt`.

A run opens on its result: the agent's final message, the changed files, tokens, branch, and the worktree path with a copy button. Below that is the run's timeline. The check's stderr, the agent's thinking, each command with its exit code, duration, and output, tool calls, and file changes start collapsed. While a run is in progress, finished blocks appear as they arrive. Run starts the work in a separate process; if it cannot start, the item says why.

In Rhizome Desktop, the Dock icon's badge counts the task items that need you (due, ready, or needs attention, not snoozed) across your open repositories, and a system notification arrives when an item newly needs you; clicking it opens the task. Two more actions appear there:

- **Open worktree** opens a ready or needs-attention item's worktree in a new window.
- **Start** opens Terminal in the main checkout running `rzm tasks start <item>`. The first time, macOS asks to let Rhizome control Terminal.

## Where state lives

Everything personal stays in the main checkout's ignored `.rhizome/task-state/`, shared by every worktree of the repository: modes, run history, inbox items, the check checkout, background worktrees, data folders, and run folders. Folder names replace the slash in a module task's id with `_` (`data/compounding_sweep/`), which no id contains. A repository without a main checkout keeps it in Rhizome's per-user configuration folder.

Every run gets an ID like `20261031T143000Z-1a2b3c4d` before its check starts, and a folder `runs/<id>/` holding `check.log` (the check's stderr, cut at 1 MB) and, when agent work ran, `events.jsonl` (the harness transcript). A run left unfinished by a crash or reboot is recorded once as `interrupted` by the next run. Per task, Rhizome keeps the 20 newest runs with a transcript and 100 others, and the 20 newest transcripts and check logs; token totals cover the last 30 days even after older runs are trimmed. Tests have a separate bounded history, labelled Test, with their source checkout and snapshot. Their records and owned output worktrees are retained together and do not move the task's clock.

When `rzm tasks run <item>` fails before the run starts, for example because another run holds the lock, the reason is saved on the item until its next state change.

## Module actions

A module can also declare actions: named operations that a view, an agent, or a person runs on demand rather than on a schedule ([[module-actions|SPEC-0131]]). An action runs once and returns; it uses the same process handling and repository trust as a task's check.

Declare one action per file in `.rhizome/modules/<module>/actions/<name>.yaml`. Its id is `<module>/<name>`.

```yaml
description: Build one agent prompt for the selected recommendations
inputs:
  items: { kind: list, required: true }
  reason: { kind: string, enum: [not-relevant, wrong, already-fixed] }
run:
  command: [node, "{module}/scripts/workspace.mjs", "prompt"]
timeout: 30s
```

- `inputs` names each input, lowercase words joined by hyphens, with a `kind` of `string`, `int`, `bool`, or `list` (of strings), and optionally `required` and, for `string` and `list`, an `enum`.
- `run` holds exactly one of:
  - `command`: runs in the repository root with the input as JSON on stdin. `{module}` stands for the module folder. Print exactly one JSON value and exit 0 to succeed. Exit 2 to refuse, with the reason as the last line on stderr. Any other exit is an error.
  - `script`: a code-mode script inside the module, run through `rzm agent code execute` with `input` set to `{action, input, moduleDir, repoRoot}`. Return the result, or `{ refused: "<reason>" }` to refuse. The script can load a module file with `await import(input.moduleDir + "/scripts/lib.mjs")`.
- `timeout` defaults to 60 s and may be at most 5 minutes. The action and everything it started are stopped when it passes.

Every run returns `{"status": "ok", "result": …}`, `{"status": "refused", "reason": …}`, or `{"status": "error", "reason": …, "stderrTail": …}`. In a repository you have not trusted, every action is refused until you run `rzm trust`.

Run an action from:

- **The command line:** `rzm <module>` lists the module's actions beside its recipes. `rzm <module> <action> --<input> <value>` runs one, repeating the flag for each value of a `list` input. It prints the result and exits 0, prints the reason and exits 2 when the action refuses, and exits 1 on an error.
- **Agents:** the code-mode operations `action_list` and `action_run({id, input})`.
- **Views:** the kit hooks `useActionQuery` and `useActionCommand`, over `GET /api/v1/actions` and `POST /api/v1/actions/<module>/<name>`.

`rzm validate actions`, part of `rzm validate all`, reports a declaration that cannot be read or parsed, a missing description, an unknown input kind, an enum on `int` or `bool`, a `run` with no form or both, a script or `{module}` path that does not exist, a timeout over the limit, and an action named like a query recipe in its module. Starters install their `module/actions/` files with the rest of the module.
