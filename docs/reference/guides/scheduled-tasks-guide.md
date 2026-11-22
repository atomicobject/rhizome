---
type: ReferenceDoc
summary: "How to declare scheduled tasks in .rhizome/tasks or a module's tasks folder, choose whether they run automatically or notify, and use the rzm tasks commands and inbox; also how a module declares on-demand actions."
reference-kind: guide
---

# Scheduled tasks

Last verified: 2026-11-22

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
every: 1d                           # Go duration, or whole days (7d) or weeks (2w); or a schedule
scope: person                       # person (default) or team
check:                              # optional: command or script, not both
  command: [sh, scripts/needs-finalizing.sh]
  # script: .rhizome/tasks/finalize-check.js
prompt: |
  Do what the pre-flight check says.
timeout: 1h                         # optional, applies to the check and the prompt each
writes: ["notes/**"]                # files the task may commit in place (see below)
land: in-place                      # in-place (default with writes) or pr (default without)
memory: true                        # optional: a short handoff note between runs
```

### Run at set times

`every` counts from the last run, so its start time drifts. To run at fixed times of day, give a `schedule` instead; a task has one or the other.

```yaml
schedule:
  - at: ["05:00", "13:00"]          # one "HH:MM" or a list, 24-hour
    days: weekdays                  # daily (default), weekdays, weekends, or [mon, thu]
    timezone: America/Detroit       # optional IANA zone
    late: 2h                        # optional, default 1h
  - every: 2h                       # or a window: every 2h from 08:00 through 18:00
    between: ["08:00", "18:00"]
    days: [sat, sun]
```

A slot is due once its time has passed and no run of the task started at or after it. A slot missed while the machine slept or Rhizome was closed still runs if the next check comes within `late` of it; several missed slots run once. A slot inside a daylight-saving gap runs at the first instant after the gap, and a slot in a repeated hour runs once, at its first occurrence; a run that started in either occurrence counts for it. The zone is the entry's `timezone`, else your `tasks.timezone` setting (below), else the machine's. Windows that cross midnight, cron strings, and RRULEs are not supported.

Rhizome versions before `schedule` reject a task file that uses it, because task files reject unknown fields. A starter that ships scheduled tasks states its minimum Rhizome version.

Scheduled execution reads task files from the primary branch's history. Editing a task on a branch does not change its schedule. The Tasks page also shows definitions in the selected checkout, including uncommitted new tasks, with their local version marked as new, different, unchanged, or invalid.

To try one before merging, use **Test once** on its local version or run `rzm tasks run --file .rhizome/tasks/finalize.yml`. The test captures the checkout's current source, including new nonignored helper scripts and uncommitted changes, into a detached worktree. It leaves your working files and staging area unchanged and compares the output against that capture. Invalid local definitions display their error; a valid scheduled version stays available.

Each test starts with fresh task data and has its own visible result and transcript. It does not change the task's next due time, mode, scheduled usage totals, or ordinary inbox items. Test output is not committed or tagged as scheduled work. A running test uses the repository's task lock, so scheduled work may wait until it finishes. Local-only tasks receive scheduling controls after they reach the primary branch.

`rzm validate tasks` (also part of `rzm validate all`) checks the task files you have checked out and reports each one that does not parse, such as a bad id, an unknown field, an invalid `every`, `schedule`, or `timeout`, or an id that another file also declares. `rzm tasks status` reports the same problems for the primary branch.

Starters can ship task files the way they ship views; `rzm init` installs and refreshes those. Task files your team writes are left alone.

### The check

A command check runs in a Rhizome-managed checkout of the primary branch, reset before each run, with your login shell's `PATH`, no input, and the task's timeout. A script check is read from the primary branch and run with `rzm agent code execute` against the main checkout's vault, with the locations below as its input.

Either kind prints one JSON object:

```json
{"needed": true, "summary": "3 notes need finalizing", "context": "notes/a.md notes/b.md"}
```

or `{"needed": false}`. A task with no check is needed whenever it is due.

Checks and prompts receive the same locations: `RZM_TASK_ID`, `RZM_TASK_MAIN_CHECKOUT`, `RZM_TASK_COMMIT`, and `RZM_TASK_DATA_DIR`, a private folder that keeps the task's state between runs. Prompts receive them in their instructions. In both, `rzm` on `PATH` is the binary running the task, so a repository that pins its own Rhizome build uses that build.

For a test, `RZM_TASK_COMMIT` names the source snapshot and `RZM_TASK_DATA_DIR` is fresh for that attempt. `RZM_TASK_TEST=1` lets a task with its own private state choose separate test storage; the compounding module does this automatically. Command checks and agent work use the captured worktree. A code-mode test reads its captured script but queries the selected checkout's existing index; create that index first if it is missing. It does not automatically index a new test worktree or start a runtime. If no runtime is running, code mode uses its existing in-process read path with `RZM_RUNTIME_AUTOSTART=0`.

### Cost is the task's job

Use the check so the prompt runs only when there is work, and keep the prompt efficient. `rzm tasks status` shows each task's recent token use; a task that costs too much should be fixed or turned off. As a safety net, you can set a daily token cap per repository on your machine (`dailyTokenCap`, below).

### What a task needs

A task can name the commands and MCP servers it needs, and the tools its agent must not use:

```yaml
requires:
  commands: [gh, jq]                  # on your login shell's PATH
  mcp: [claude.ai Linear]             # configured and usable in your harness
deny: [WebFetch, "Bash(git push:*)"]  # tools the harness refuses during the prompt
```

Rhizome checks every requirement before the run's check or prompt, so a missing one costs no tokens. The run fails, and its inbox item needs attention naming what is missing, when a command is not on your login shell's `PATH`, when your harness does not list an MCP server, or when it lists one as unusable: in Claude Code anything but connected, such as needs authentication or failed to connect; in Codex disabled or not logged in. `requires.commands` takes command names, not paths, and `rzm` always resolves to the binary running the task. Name MCP servers as `claude mcp list` or `codex mcp list` prints them. Checking `requires.mcp` runs that command, and `claude mcp list` connects to every server to check it, which takes a few seconds.

Background runs and tests load your own MCP configuration: in Claude Code your user, project, plugin, and claude.ai connector servers; in Codex the servers in `~/.codex/config.toml`. Agent chat keeps its own fixed set.

`deny` passes its names to Claude Code as disallowed tools, in the syntax of Claude Code's `--disallowedTools`. Codex cannot refuse a named tool, so under Codex a task with `deny` fails before it runs; use Claude Code for that task or remove `deny`. `requires.mcp` and `deny` apply to the prompt, so a task without one cannot declare them.

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

Override one task for yourself with the **Mode** selector on its page in the workspace's **Tasks** tab, or with `rzm tasks mode <task> auto|notify|off`. Choose **Default** or run `rzm tasks mode <task> default` to return to the repository's default. **Pause all** on the Tasks page, or `rzm tasks pause`, stops all background task work for you in the repository until **Resume all** or `rzm tasks resume`. `rzm tasks pause --for 1h` or `--until 2026-11-23T09:00:00-05:00` resumes on its own at that time.

Tasks run only in a trusted repository. Rhizome Desktop trusts a repository when you set it up or choose **Trust and open**; otherwise choose **Trust this worktree** in the repository's Settings tab, or run `rzm trust`. The first time Rhizome sees a task, your inbox gets a note naming it and its mode.

## Choose where tasks run

Scheduled tasks run only on machines where you turn them on, so a laptop and a desktop do not both run them. The setting is yours and this machine's, and new machines start with it off:

```sh
rzm tasks machine on                      # in any checkout of the repository
rzm tasks machine on --checkout ~/notes   # run from a specific checkout
rzm tasks machine off
rzm tasks hosts --json                    # repositories that run here
```

The same settings, with a few more, live in your user configuration (`~/.config/rhizome/config.yml`), keyed by the repository's Git common directory:

```yaml
tasks:
  timezone: America/Detroit          # default zone for schedules
  notify:
    command: [/Users/me/bin/rhizome-notify]   # optional; see Quiet runs and alerts
  repositories:
    /Users/me/notes/.git:
      runHere: true
      checkout: /Users/me/notes      # optional; defaults to the main checkout
      dailyTokenCap: 2000000         # optional; 0 or absent is no cap
      tasks:
        compounding/sweep: {harness: codex, model: gpt-6.1, effort: high}
```

A task's `harness`, `model`, and `effort` override your chat agent settings for that task only.

`dailyTokenCap` stops new scheduled agent work in the repository once its scheduled runs have used that many tokens today. Today is the UTC day Rhizome stores token totals in, so the cap resets at 00:00 UTC. Checks still run, since they cost nothing; a task that needs agent work records a skipped run and waits for its next slot or cadence. The first stop each day runs your notify command once. The cap is checked before a run starts, so one run can finish past it. Notify-mode items, tests, and runs you start yourself with **Run** or `rzm tasks run` are not capped, and their tokens do not count toward the cap, though they still show in each task's token use in `rzm tasks status`.

## When tasks run

Scheduled tasks run on a machine where you turned on **Run tasks on this machine** (the Tasks page or `rzm tasks machine on`), while Rhizome Desktop runs there, even with every window closed. A runtime that Desktop does not hold, such as one started by `rzm` commands or MCP, runs no scheduled tasks. `rzm tasks stop` stops a run in progress and records it as stopped.

While Rhizome Desktop holds the runtime of a repository you turned on here, it checks the clock every 30 seconds and starts `rzm tasks run-due --host` when a task is due. `--host` runs nothing unless the repository runs here, so a runtime on another machine never does the work; turning **Run tasks on this machine** off during a pass stops it before the next task. You can also run `rzm tasks run-due` yourself. Each due task runs once per person and repository, however many worktrees or runtimes are open. A task missed while Rhizome was closed runs once at the next pass, within its `late` window for a scheduled slot. When several tasks are due, the one with the nearest deadline runs first: a slot by the end of its `late` window, an `every` task by its due time.

Deciding what is due reads only the local primary ref. A pass with something due first fetches the primary ref's remote, without prompting and with a time limit, then reads the tasks again; if the fetch fails, the pass uses the last fetched ref and `rzm tasks status` shows the failure. A task added on the remote is seen at the next pass that fetches.

`rzm tasks status` says why each task is not running: tasks do not run on this machine, the repository is untrusted, work is paused (and until when), the task is off, an open item waits for you, a requirement was missing, or today's token cap is reached. `--json` reports the same as a stable `blockedBy` code (`not-run-here`, `no-host`, `untrusted`, `paused`, `off`, `open-item`, `wrong-branch`, `requires`, `fetch-failed`, `token-cap`), with each task's `nextAt` and `timezone` and the repository's `runHere`, `pausedUntil`, last `fetch`, and `tokenCap` (`cap`, `used`, `day`, `reached`). Only the runtime knows whether Rhizome Desktop hosts it, so `no-host` appears on the Tasks page (with tasks on here and no Desktop host) and never in `rzm tasks status`. A failed fetch never blocks a task, since the pass falls back to the last fetched branch, so `fetch-failed` is reserved and the failure shows only as the report's `fetch`. An untrusted repository is reported once, as the last pass's error. To run a task without waiting for a pass, choose **Run now** on its page or run `rzm tasks run --task <task>`. Its check runs first, and the work starts only if the check finds some. A task with an open inbox item runs from that item instead.

A `person` task is due when your own last run is older than its cadence. A `team` task also counts teammates: when a commit tagged `Rhizome-Task: <id>` reaches the primary branch, everyone's clock resets. Squash merges keep the tag when the repository squashes with commit messages. They drop it when it squashes with the pull request title alone.

## Check what tasks need

`rzm tasks doctor [task]` reports whether this machine can run every task, or one, and exits 1 when a check fails; add `--json` for scripts. It changes nothing. It checks:

- repository trust;
- the primary branch and when the repository last fetched it;
- `git push --dry-run` to the primary branch's remote, when your push setting is on, with the repository's hooks turned off; in an untrusted repository it is skipped, because a push runs commands the repository configures;
- Obsidian Git in the host checkout: a timed auto commit-and-sync (`autoSaveInterval`) or auto pull (`autoPullInterval`) moves HEAD under an in-place run;
- each `requires.commands` entry on your login shell's `PATH`;
- for a task with a prompt: whether your harness is installed and signed in, whether it can enforce `deny`, and each `requires.mcp` server's status as the harness lists it. Claude Code connects to each server. Codex does not start its servers to list them, so doctor reports a Codex server as configured (Codex does not report connection status), or as disabled or not logged in; a configured server can still fail to connect when the task runs.

## Where changes land

A background run, automatic or started with **Run**, **Run now**, or `rzm tasks run`, lands in one of two ways, set by the task's `land`.

### In place

`land: in-place` runs in the checkout Rhizome has open, the main checkout unless you chose another, and commits only the files the task declares in `writes`: repository-relative glob patterns where `*` stays within a folder and `**` crosses folders. A task that declares `writes` lands in place unless it says `land: pr`; `land: in-place` without `writes` is invalid.

Before the run, Rhizome fetches the branch's remote and fast-forwards the branch to the primary ref, even with uncommitted files, then records the working tree, HEAD, and which files you had changed, staged, marked `assume-unchanged` or `skip-worktree`, or had ignored. The fast-forward works like `git merge --ff-only`, but never autostashes or runs hooks. Rhizome checks for files in its way while it holds Git's locks, and a stop does not cut the fast-forward short, so it never leaves Git's lock files behind. When Rhizome cannot do this safely, the task is skipped with one inbox notice, which later skips update instead of repeating, and it tries again at its next due time. A task is skipped when:

- the checkout is not on the local branch of the primary ref (for `origin/main`, a `main` that tracks it), or you switch branches while Rhizome fetches;
- a merge, rebase, cherry-pick, revert, or bisect is in progress there, or conflicts are unresolved;
- the branch has commits the primary ref lacks, so it cannot fast-forward;
- the incoming commits change a file you have uncommitted changes to, or one marked `assume-unchanged` or `skip-worktree`, or add a file where an untracked or ignored file of yours is. Git would replace an ignored file without asking.

If Git fails partway through the fast-forward, for example when it cannot write a file, it may already have updated some files while HEAD and your staging area stay where they were. The notice names those files: check `git status`, and `git checkout -- <file>` puts a tracked one back. Rhizome does not try to repair it.

A run is also skipped, without a notice, when the fetch brings another machine's commit for the same scheduled slot.

After the run, the task's changes are the files that changed during the run, were clean before it, and match `writes`. Rhizome commits exactly those, using the agent's final message, the `Rhizome-Task: <id>` tag, and a `Rhizome-Task-Slot:` line naming the run. It builds the commit outside your index and then updates only those files' index entries, so anything you staged stays staged. Repository hooks and commit signing are off. Everything else stays as it is and is named in the run's summary:

- files you had changed or staged before the run, or staged during it;
- files marked `assume-unchanged` or `skip-worktree`;
- files Git ignored before the run, and everything inside a folder it ignored, even when the run changed the ignore rules so they now show up;
- a nested Git repository or submodule;
- files the agent changed outside `writes`;
- a task file that would replace a file or folder that is not the task's, such as a file where a folder of your staged work was.

Other cases:

- **Protected files.** Changes under `.agents/` or `.rhizome/`, including task files, are put back at once unless a `writes` pattern names that folder itself (`.rhizome/memory/**`; `**` alone does not count). The run's diff of them is saved as `runs/<id>/protected.diff`, and the item needs attention. Rhizome restores nothing if the branch or HEAD changed since the run started. It writes files as a checkout would, with your line-ending and filter settings, never writes through a folder the run replaced with a symlink, and leaves a rename that changes only letter case, files Git ignored before the run, and nested repositories. The notice names what it left.
- **Ignore rules.** A run that changes a `.gitignore` outside `writes`, or the repository's `.git/info/exclude`, needs attention. Files those rules hid before the run stay yours either way: Rhizome neither commits nor removes them.
- **HEAD moved.** If you commit, switch branches, or pull in that checkout while the task runs, Rhizome commits nothing, keeps the task's files in place, and raises one notice. It moves the branch only while holding Git's locks on HEAD and the branch, so a commit or switch you make at that moment either waits for Rhizome or makes Rhizome stop.
- **Failed or stopped runs** leave their files in place. The task's next successful run commits them as its own, as long as they still match `writes` and are exactly as the failed run left them. A file you edited or staged since then is yours.
- **Known limit.** An edit you make during the run to a clean file inside `writes` cannot be told apart from the task's, and is committed with it.

A successful in-place run closes its inbox item; its result is in the task's run history. **Discard** on an in-place item only dismisses the notice; it never touches the checkout.

### Pull request

`land: pr`, and any task without `writes`, works in a worktree that Rhizome creates under `.rhizome/task-state/worktrees/`, on the branch `rzm/<task>/<date>`; a module task's branch is `rzm/<module>/<name>/<date>`. When it finishes, Rhizome commits every change with the agent's final message, the task tag, and a `Rhizome-Task-Slot:` line naming the run's slot, so once the pull request merges with its commit messages, another machine skips that slot. A run that changes nothing removes its worktree and branch.

Review a finished run in its worktree, then open the pull request yourself, or discard it. In Rhizome Desktop, **Open worktree** opens the run's worktree in a new window, and **Discard** removes the worktree and branch. From a terminal, use `rzm tasks discard <item>`. Discard leaves a worktree alone when you have made it the checkout tasks run in or switched it to another branch; remove it yourself with `git worktree remove`.

### Pushing

Nothing is pushed unless the repository sets `tasks.allowPush: true` in `.rhizome/config.yml` and you turn push on for the repository on this machine. Then:

- An in-place run pushes its commit when that fast-forwards the branch as the push destination has it. Rhizome asks the push URL itself, which can differ from the remote you fetch from, and pushes only commits it made in place on this machine. If the push fails, Rhizome fetches. It treats the run as pushed only when the push URL already has its commit, and keeps its commit local with one notice when a commit with the same slot line is already on the branch. If the upstream moved, Rhizome rebases its commit onto the fetched upstream and pushes again. It does this only when HEAD is still that commit, it is the only local commit, no tracked file has uncommitted changes, no merge, rebase, or bisect is in progress, and the upstream did not change the task's files. Otherwise the commit stays local with one notice. Rhizome remembers each commit it made, including a rebased one whose push failed or a run you stopped mid-push, so the next run pushes it along with its own. Rhizome never starts or aborts a `git rebase`, never force-pushes or autostashes, and never pushes your commits.
- A pr run pushes its branch and opens a pull request with `gh pr create`, with the task tag in its body. Discard still removes only the local worktree and branch.

### Approving a wider landing

The first time Rhizome sees a task, the inbox note says how it lands. When a task file later widens its landing, from a worktree to in place or with a new `writes` pattern, the task runs as notify until you approve it with `rzm tasks approve <task>`; an inbox note says so, and `rzm tasks status` shows its mode source as `approval`. Narrowing needs no approval. Approval accepts only the landing that note described: if the task file widened again since, `approve` refuses and a new note describes the new landing.

## Quiet runs and alerts

A task that runs every day should not ask for you every day. Before it finishes, a background run's agent may write the file named by `$RZM_TASK_RESULT_FILE` (the preamble gives its path):

```json
{"status": "quiet", "summary": "No new sessions to mine."}
```

`status` is `quiet` (nothing worth mentioning), `ok` (a normal run), or `attention` (you need to look). Rhizome keeps the status and summary on the run. A missing file means ok. A file that is not one JSON object, is over 16 KB, has another status, or is not a regular file in the run's folder (a symlink or FIFO, say) is recorded on the run with its error and also counts as ok; it never fails the run.

Which runs need you:

- **History only, no inbox item, no alert:** the check found nothing to do, the run changed nothing, or a `quiet` or `ok` run changed nothing.
- **Inbox item and alert:** a failure (including an interrupted run or a missing requirement), an `attention` result, and a task that is due for you (in notify mode, or with only a check). An `attention` run that changed nothing waits in the inbox as needs attention with its summary.
- **Changes to review:** a `pr` run that committed to its `rzm/<task>/<date>` branch makes a ready item and an alert whatever its status, because the branch waits on you until you review, open a pull request, or discard it. When Rhizome pushed the branch and opened its pull request, the pull request is where you review it: a `quiet` or `ok` run is history only, with the pull request on the run, and its worktree is removed (the local branch stays). An `attention` result or a failed push or pull request still makes a ready item and an alert. An in-place run that landed its commit is history only unless it reports `attention` or raises a notice (restored protected files, a moved HEAD, a push problem); the alert then names the notice before the agent's summary.

### Handoff memory

With `memory: true`, the preamble tells the agent about `$RZM_TASK_DATA_DIR/handoff.md`: read it first, rewrite it last, and keep it short. After each background run or test, Rhizome cuts a note over 8 KB to its first 8 KB, ending at a whole line, with a marker that says it was cut. It trims only a regular file and replaces it rather than writing through a link. **Start** and **Copy prompt** name the note too.

### Alerts on your phone

Rhizome Desktop shows a system notification when an item needs you, which only helps at that Mac. For alerts that reach you elsewhere, set `tasks.notify.command` in your user configuration to an argument list. After each run that makes an alert, Rhizome runs it with a one-line summary, such as `Mine sessions failed: check failed: exit status 1`, on stdin and these variables:

| Variable | Value |
| --- | --- |
| `RZM_TASK_ID` | the task id; unset for a host problem |
| `RZM_TASK_OUTCOME` | `failed`, `interrupted`, `notified`, `ready`, `attention`, or `token-cap`; `untrusted`, `pass-failed`, or `fetch-failed` for a host problem (below); `test` from `rzm tasks notify-test` |
| `RZM_TASK_RUN_ID` | the run's ID; unset for a host problem |
| `RZM_TASK_REPOSITORY` | the repository's main checkout |
| `RZM_TASK_ITEM` | the inbox item, when there is one |

The command runs with your login shell's `PATH`, in the main checkout, and has 30 seconds. Its output goes to `runs/<id>/notify.log`. If it fails or times out, the run records why (`notifyError`) and is otherwise unchanged. Tests never notify, and a run you stop yourself does not alert.

Rhizome queues each alert in task state when it records the run, then runs the command after the run finishes and releases the repository's run lock, so a slow command never holds up the next run. A process stopped while sending leaves the alert queued; the next `rzm tasks run-due` or `rzm tasks run` sends it, oldest first. The queue holds 50 alerts; past that the oldest are dropped, and the next alert says how many.

A machine that runs a repository's tasks also alerts when they cannot run at all: the repository is not trusted, the primary branch or its task files cannot be read, or fetching the primary branch has kept failing for six hours (passes still run from the last fetched commit). Each distinct problem alerts once until it clears and again if it comes back. These alerts have no task or run, so `RZM_TASK_ID` and `RZM_TASK_RUN_ID` are unset and nothing is written to a `notify.log`. While the primary branch cannot be read, the runtime starts a pass every 15 minutes to record and report it.

For example, a script that posts to [ntfy](https://ntfy.sh), which has a phone app; the topic acts as a password, so use a long random one:

```sh
#!/bin/sh
# ~/bin/rhizome-notify: send the task summary on stdin to your phone.
curl -fsS -H "Title: Rhizome $RZM_TASK_OUTCOME: $RZM_TASK_ID" \
  --data-binary @- "https://ntfy.sh/replace-with-your-long-random-topic"
```

[Pushover](https://pushover.net) works the same way with `curl -fsS --form-string "token=<app token>" --form-string "user=<user key>" --form-string "message=$(cat)" https://api.pushover.net/1/messages.json`. Keep the keys in the script (`chmod 700`), not in the configuration. Then point the setting at the script and check it:

```yaml
tasks:
  notify:
    command: [/Users/me/bin/rhizome-notify]
```

```sh
rzm tasks notify-test   # sends a sample notification; exits 1 and prints the output if it fails
```

## The inbox

In Rhizome Desktop, the toolbar's **Tasks** tab shows how many items need you, and the Dock badge counts them across every open repository. A system notification arrives when an item becomes due, ready for review, or needs attention; clicking it opens the task. Each item's page offers the actions for its state:

- **Run** starts a background run.
- **Start** opens a terminal running `rzm tasks start`, which hands the task to your agent harness.
- **Copy prompt** copies the task's prompt for an agent session you start yourself.
- **Open worktree** and **Discard** handle a finished run.
- **Snooze…**, **Skip**, and **Done** set the item aside.

The harness is the one chosen in the workspace's **Agent** tab. When it is unset and only one of Claude Code or Codex is installed and signed in, Rhizome uses that one.

Each item is in one of these states:

| State | Meaning | Actions |
| --- | --- | --- |
| due | A notify-mode task is needed. | `run`, `start`, `snooze`, `skip` |
| ready | A background run finished with changes. | review, then `done` or `discard` |
| needs-attention | A check or run failed or timed out, or the agent asked for attention. It waits for its next cadence instead of retrying. | `run`, `start`, `discard`, `done` |
| started | You started it with `start`. | `done` |

From a terminal, `rzm tasks inbox` lists the items and `rzm tasks <action> <item>` acts on one.

`rzm tasks start <item>` opens your harness in a new worktree that the harness manages (`claude --worktree` or `codex --worktree`). The prompt's first step switches to a fresh `rzm/<task>/<date>` branch at the primary commit. `--print` prints that prompt without starting anything, for agents you start some other way. Claude Code and Codex each ask once before they work in a folder they have not seen; if Start reports that the harness exited at once, run `claude` or `codex` in the main checkout, accept, and start again.

A due item stays current. Each time its task comes due again, Rhizome runs the check again: if nothing is needed now, the item closes as done; if it is still needed, its summary and context are refreshed in place, and any snooze is kept. A re-check never adds a second item or starts work, and a check that fails moves the item to needs attention. Running, ready, and started items keep their task from checking until they close.

An item closes by itself when a commit carrying its task's tag reaches the primary branch, whoever made it. A pass notices that: while an item waits for its work to reach the primary branch, the Desktop host starts a pass at most every 15 minutes that fetches and closes it, and runs nothing else for that task.

## The Tasks page

Open **Tasks** in the workbench, or the Tasks section in Rhizome Desktop, to see every registered task with its mode, cadence, last outcome, next due time, and inbox state. Pick a task to change its mode, act on its inbox item (Run, Copy prompt, Snooze, Skip, Discard, Done), and read its run history, newest first. Checks that found nothing to do appear as quiet single lines.

**Copy prompt** copies any open item's prompt, for an agent you start some other way. It carries the task's instructions, the check's findings, and the locations, but not Start's `git switch` step, so pasting it into a session in the main checkout never moves its branch. When the clipboard is unavailable, the page shows the prompt to copy by hand. The same text is served at `GET /api/tasks/items/<id>/prompt`.

A run opens on its result: the agent's final message, the changed files, tokens, branch, and the worktree path with a copy button. Below that is the run's timeline. The check's stderr, the agent's thinking, each command with its exit code, duration, and output, tool calls, and file changes start collapsed. While a run is in progress, finished blocks appear as they arrive. Run starts the work in a separate process; if it cannot start, the item says why.

In Rhizome Desktop, the Dock icon's badge counts the task items that need you (due, ready, or needs attention, not snoozed) across your open repositories, and a system notification arrives when an item newly needs you; clicking it opens the task. Two more actions appear there:

- **Open worktree** opens a ready or needs-attention item's worktree in a new window.
- **Start** opens Terminal in the main checkout running `rzm tasks start <item>`. The first time, macOS asks to let Rhizome control Terminal.

On a Mac, Rhizome Desktop hosts scheduled tasks from the menu bar for every repository with **Run tasks on this machine** on, with or without windows. ⌘Q (Quit Rhizome) closes the windows and keeps the menu bar icon; opening Rhizome again brings the windows back. The menu bar menu shows each repository's running and next task, the inbox count, and the last failure, and offers Run now, Pause Tasks, and Open Rhizome. **Quit Rhizome Completely** (⌥⌘Q, or in the menu bar menu) stops hosting: it asks whether to wait for or stop a running task first. Logging out, restarting, or quitting from the Dock stops a running task without asking. Settings > Menu bar turns off "Keep running in the menu bar", so ⌘Q quits completely, or turns on "Launch at login".

## Where state lives

Everything personal stays in the main checkout's ignored `.rhizome/task-state/`, shared by every worktree of the repository: modes, run history, inbox items, approved landings, files a failed in-place run left, the check checkout, background worktrees, data folders, and run folders. Folder names replace the slash in a module task's id with `_` (`data/compounding_sweep/`), which no id contains. A repository without a main checkout keeps it in Rhizome's per-user configuration folder.

Every run gets an ID like `20261031T143000Z-1a2b3c4d` before its check starts, and a folder `runs/<id>/` holding `check.log` (the check's stderr, cut at 1 MB) and, when agent work ran, `events.jsonl` (the harness transcript), the agent's `result.json`, and `notify.log` when the notify command printed anything. A `memory: true` task's `handoff.md` lives in its data folder. A run left unfinished by a crash or reboot is recorded once as `interrupted` by the next run. Per task, Rhizome keeps the 20 newest runs with a transcript and 100 others, and the 20 newest transcripts and check logs; token totals cover the last 30 days even after older runs are trimmed. Tests have a separate bounded history, labelled Test, with their source checkout and snapshot. Their records and owned output worktrees are retained together and do not move the task's clock.

A `run-due` pass that a runtime starts writes its output to `run-due.log` in the same folder, keeping the last 1 MB.

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
