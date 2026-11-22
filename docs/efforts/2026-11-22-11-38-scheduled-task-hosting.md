---
type: EffortNote
id: EFF-2026-11-22-11-38
aliases: [EFF-2026-11-22-11-38]
name: Scheduled task hosting and landing
created-at: 2026-11-22T16:38:28Z
status: complete
summary: Make scheduled tasks run on time from a Rhizome Desktop menu bar host, land their changes in the open checkout or through a pull request, and report quietly or loudly as needed (approved plan phases 1–3).
governing-specs:
  - "[[scheduled-tasks]]"
  - "[[vault-runtime-coordination]]"
---

# Scheduled task hosting and landing

## Scope

Phases 1–3 of the plan Drew approved on 2026-11-22 after two council rounds (Claude Fable 5.2, GPT-6.2 Astra, Gemini 4 Neon): the menu bar host and on-time runs, in-place landing and push, and operations (handoff memory, result file and quiet rules, notify command, token cap).

Out of scope: phase 4 (vault cutover, a separate effort in Drew's vault repository) and phase 5 (the daily-ops starter). The plan's "Not building" list is binding: RRULE or cron authoring, vendor automation backends, a launchd or systemd host install, cross-machine locks or central state, per-task locks, circuit breakers, section-level content validation, LLM conflict resolution, cloud execution, task dependencies, and an agent-produced patch format for in-place landing.

Constraints from the brief: work only in this worktree of `atomicobject/rhizome`; never modify Drew's vault or his Codex automations; use disposable fixture repositories and remotes for landing tests.

## Spec Set (Frozen)

- [[scheduled-tasks|SPEC-0127]], revision 2026-11-22 (amended in this effort's first commit: US3, US7 wording, new US9–US14, Requirements, Non-Goals, Decisions).
- [[vault-runtime-coordination|SPEC-0104]], revision 2026-11-22 (new US7 and US8, Lifecycle requirement).

## Stories In Scope (Frozen)

- [[vault-runtime-coordination#^SPEC-0104-US7|SPEC-0104.US7]] Rhizome Desktop holds runtimes that host scheduled tasks (phase 1)
- [[vault-runtime-coordination#^SPEC-0104-US8|SPEC-0104.US8]] Rhizome Desktop keeps running in the menu bar (phase 1)
- [[scheduled-tasks#^SPEC-0127-US3|SPEC-0127.US3]] amended dispatch under a host (phase 1)
- [[scheduled-tasks#^SPEC-0127-US9|SPEC-0127.US9]] wall-clock schedules (phase 1)
- [[scheduled-tasks#^SPEC-0127-US10|SPEC-0127.US10]] where and how tasks run (phase 1)
- [[scheduled-tasks#^SPEC-0127-US11|SPEC-0127.US11]] requirements, MCP passthrough, doctor (phase 1)
- [[scheduled-tasks#^SPEC-0127-US12|SPEC-0127.US12]] in-place landing (phase 2)
- [[scheduled-tasks#^SPEC-0127-US13|SPEC-0127.US13]] push and pull-request landing (phase 2)
- [[scheduled-tasks#^SPEC-0127-US14|SPEC-0127.US14]] handoff memory, quiet rules, notify command, token cap (phase 3)

## Spec Coverage Checklist

- [x] SPEC-0104 US7: host stream, host-only dispatch, 30-second wall-clock check, drain and stop, terminate path (lanes B, C).
- [x] SPEC-0104 US8: accessory app, custom quit items, session stash and resume, launch at login, menu bar menu, notifications (lane C).
- [x] SPEC-0127 US3: `run-due --host`, nearest deadline first (lanes A, B).
- [x] SPEC-0127 US9: `schedule` parsing, slot math, `late`, DST, status (lane A).
- [x] SPEC-0127 US10: per-machine settings, `machine`, `hosts`, skip reasons, pause until, run-due output kept (lanes A, B).
- [x] SPEC-0127 US11: `requires`, MCP passthrough for Claude and Codex, `deny`, `doctor` (lane A2).
- [x] SPEC-0127 US12: in-place landing (lane D).
- [x] SPEC-0127 US13: push, `pr` landing, slot trailers (lane D).
- [x] SPEC-0127 US14: memory, result file, quiet rules, notify command, token cap (lane E).

## Plan

### Approval

Drew approved the plan below on 2026-11-22 after two council rounds and delegated implementation of phases 1–3 to this thread, treating the plan as authorization. No current user is configured in this worktree (`rzm agent current-user show` reports `missing_current_user`), so `plan-approved-by` stays absent; this paragraph records the chat authorization without inventing an identity.

### Approved plan (verbatim summary of decisions)

**Why tasks don't run today.** Default mode is `notify`; nothing wakes Rhizome (the tick lives in a runtime that starts only with a window and idle-exits headless); `every` drifts and cannot express "5 AM"; no fetch before resolving the primary ref; task state is per clone; Claude task runs get no MCP servers (`--strict-mcp-config` with none passed); task runs share the chat agent's model and effort; an open ready/started item blocks later runs; untrusted repositories skip silently; detached `run-due` children discard stdio.

**Decision 1 — Rhizome is the only runner; on a Mac, Rhizome Desktop hosts it from the menu bar.** Dock icon only while a window is open; closing the last window switches to an accessory app that keeps running. Replace `PredefinedMenuItem::quit` with **Quit Rhizome (⌘Q)** (stash the window session, close windows, drop the Dock icon, keep the menu bar and tasks; one-time notice) and **Quit Rhizome Completely** (if a task is running, ask wait or stop; then exit via the normal terminate path). OS logout, restart, shutdown, Dock Quit, and updater restarts go through native terminate → `RunEvent::Exit`, which cannot be prevented: stop held runtimes and any active task there without prompting (recorded as interrupted); never call `prevent_exit()` for OS termination, only for last-window-close. Setting "Keep running in the menu bar" (default on; off: ⌘Q quits completely and the second item is hidden). Menu bar menu: next task and time per enabled repository, running indicator, inbox count, last failure, Run now, Pause tasks (1h / until tomorrow), Open Rhizome, Quit Rhizome Completely; status from the runtime API, not webviews. Launch at login starts menu-bar-only. Session stash separate from `Sessions::quit` (persist geometry, move windows to a stashed list, clear live windows, leave `quitting` false). One resume step restores all stashed windows and is called by every window opener (`windows::reopen`, Open Rhizome, `menu.rs:270`, `menu.rs:437`, `terminal.rs:170`, `terminal.rs:224`). Launch at login and the updater's `app.restart()` start with no windows but keep the saved session pending; `windows::restore` must not force a window then; a complete quit, logout, or update before the first Open must not overwrite the saved layout. A window-independent host loop ensures a runtime for each repository with "run tasks on this machine" on, at launch and on its presence poll, and holds one open stream so `IdleTracker.Hold` keeps it alive; no new no-idle-exit mode. Scheduled dispatch only while a Desktop host holds the runtime; a 30-second wall-clock check replaces the 15-minute tick. Quit Completely: disable new dispatch, wait for or stop the active run by signalling the `run.lock` holder's process group, record the outcome, then stop the runtimes the app holds.

**Decision 2 — where it runs is per person, per machine.** "Run tasks on this machine" (new machines default off), harness, model and effort per task, timezone, notify command, token cap, push. None of it goes in the task file.

**Decision 3 — landing is `in-place` (default) or `pr`.** As written in SPEC-0127 US12–US13, including the cleanup-ownership rule (`work.go:106`, `work.go:212`, `actions.go:305` must never touch the open checkout) and the widening rule (a task whose landing or `writes` widens drops to notify until re-approved).

**Decision 4 — wall-clock schedules in a small form; keep `every`.** As written in SPEC-0127 US9. Taskdef decodes with `KnownFields(true)`, so starters need a minimum Rhizome version.

**Operations.** Single run lock, nearest deadline first; host designation guards across machines and slot trailers dedupe as a backstop; `memory: true` handoff; `$RZM_TASK_RESULT_FILE` quiet rules; macOS notifications plus an optional `notify.command`; `rzm tasks status` explains skips; `rzm tasks doctor`; task sessions load the person's MCP config and optional `deny:` maps to `SessionOptions.DisallowedTools`; per-task model/effort and an optional daily token cap.

**Phase done-when criteria.**
1. A no-op auto task fires within a minute of its slot on the mini for three days with all windows closed (including across sleep) and does not run on the laptop; it reaches an MCP server under Claude and Codex; logout/restart is never blocked; with two windows open, ⌘Q then double-clicking the app restores both; login → Quit Completely → relaunch restores the saved layout; Quit Completely leaves no runtime or task process held by the app.
2. An in-place test task lands 20 runs while another clone pushes every few minutes; a file dirty before a run stays uncommitted and is named; a pre-staged change is untouched; an edit to `.agents/` during a run is restored and flagged; a forced divergence with a dirty tree keeps the commit and raises one notice; a failed run's files are adopted by the next successful run; Discard on an in-place item leaves the checkout intact.
3. The host's failures reach the person off-machine and a quiet run makes no inbox item.

**Top risks.** A human edit inside `writes` during a run is committed with the task's (documented limit); silent host failure (mitigated by notifications, notify command, doctor); prompt injection from transcripts in a full-access agent that can push (controls: `writes`, protected-path restore, `deny`; need tests).

### Delivery shape

Three stacked pull requests: phase 1 against `main` from `t3/scheduled-task-hosting`, phase 2 on top of it, phase 3 on top of phase 2. The effort closes in the phase 3 pull request. Implementation lanes run as T3 delegates (Opus 5.5, xhigh) in their own worktrees; the lead integrates, reviews the full diff, runs the gates, and drives the real-app checks.

### Lanes and contracts

Phase 1 runs four lanes in parallel from this effort's commit.

- **Lane A — schedule, settings, dispatch rules** (`pkg/vault/taskdef`, `pkg/app/tasks`, `pkg/vault/repoconfig`, `cmd/tasks.go`). `schedule` parsing and slot math (US9); per-machine settings (US10); `run-due --host`; nearest deadline first; fetch before a pass with due tasks; skip reasons in status; `pause --until/--for`; keep run-due child output; per-task harness/model/effort in `HarnessAgent`.
- **Lane A2 — requirements, tools, doctor** (`pkg/harness/claude`, `pkg/harness/codex`, `pkg/app/tasks`, `cmd`). `requires`, `deny`, MCP passthrough for task sessions, `rzm tasks doctor` (US11).
- **Lane B — runtime host** (`pkg/app/cli/serve`, `pkg/app/web/tasks.go`, `pkg/app/tasks` stop support, web Tasks page toggle). Host stream, host-only dispatch, 30-second check, drain, stop, run-now, pause-until route, the Tasks page "Run tasks on this machine" toggle (SPEC-0104 US7).
- **Lane C — desktop** (`desktop/`). Everything in SPEC-0104 US8 plus the host loop of US7.

Contracts the lanes share (names are binding; lanes may add fields, not rename):

1. **User configuration** (`repoconfig.CliConfig`, new `Tasks *TasksUserConfig` with YAML key `tasks`):
   ```yaml
   tasks:
     timezone: America/Detroit          # optional person-wide default
     notify: {command: [..]}            # phase 3
     repositories:
       "<canonical Git common dir>":
         runHere: true
         checkout: /abs/path/of/host/checkout   # registered for task running; defaults to the main checkout
         push: false                    # phase 2
         dailyTokenCap: 0               # phase 3; 0 = none
         tasks:
           "<task id>": {harness: claude, model: claude-opus-5-5, effort: high}
   ```
   Lane A owns the type, load/update helpers (under the existing config file lock), and Go accessors in `pkg/app/tasks` (`MachineSettings`, `SetRunHere`). Tests must never touch the real user configuration; use the existing environment override pattern.
2. **CLI** (lane A): `rzm tasks machine on|off [--checkout <path>]`, `rzm tasks hosts --json` → `[{"commonDir": "...", "checkout": "...", "runHere": true}]`, `rzm tasks run-due --host` (runs nothing unless `runHere` is on for this repository), `rzm tasks pause [--until <RFC3339>|--for <duration>]`. `rzm tasks status --json` adds per task `nextAt`, `timezone`, `blockedBy` (a stable code: `not-run-here`, `no-host`, `untrusted`, `paused`, `off`, `open-item`, `wrong-branch`, `requires`, `fetch-failed`) and report-level `runHere`, `pausedUntil`, `fetch: {at, error}`.
3. **Service API** (lane A exports, lane B consumes): `tasks.NextDueAt(ctx) (time.Time, bool, error)` — the earliest due time across enabled tasks, computed without network or Git writes and cheap enough for every 30-second check; `Service.Stop(ctx, wait time.Duration) (stopped bool, err error)` is lane B's, signalling the `run.lock` holder's process group and recording outcome `stopped`.
4. **Host HTTP API** (lane B serves, lane C consumes), same host and origin checks as other `/api/tasks` routes:
   - `GET /api/tasks/host` — `text/event-stream`. Holding it calls `IdleTracker.Hold` and registers a host. Sends `event: status` with a `HostStatus` JSON at connect, on change, and at least every 30 seconds.
   - `HostStatus`: `{"repository": "<main checkout>", "commonDir": "...", "runHere": bool, "paused": bool, "pausedUntil": "RFC3339"?, "draining": bool, "running": {"task", "name", "runId", "startedAt", "stage"}?, "dispatching": {"pid", "startedAt"}?, "next": {"task", "name", "at"}?, "attention": int, "lastFailure": {"task", "name", "at", "message", "item"}?, "notices": [{"id", "kind": "attention"|"failed"|"ready"|"due", "task", "name", "title", "body"}]}`. Notice ids are stable so Desktop notifies once per id. Every listed key of `lastFailure` and a notice is always sent; a pass-level failure has `"task": ""` and `"name": "Scheduled tasks"`. `dispatching` (added after review) is a `run-due` or Run now process this runtime started that is alive while `running` is absent, because it has not recorded a run yet.
   - `POST /api/tasks/host/drain` → `{"running": {...}|null, "dispatching": {"pid", "startedAt"}|null}`; stops new dispatch until the runtime restarts. Drain and the scheduled check-and-start share one lock, so no pass starts after a drain returns.
   - `POST /api/tasks/stop` → `{"stopped": bool}`. It stops the `run.lock` holder or, when no process holds the lock yet, the process this runtime started (its process group).
   - `POST /api/tasks/pause` accepts optional JSON `{"until": "RFC3339"}`.
   - `POST /api/tasks/tasks/<task>/run` → adds or reuses the task's item and starts detached `rzm tasks run <item>` (Run now; equivalent to `rzm tasks run --task <id>`), 409 when busy or blocked, including while a run this runtime started has not taken the run lock yet, so one run starts at a time per runtime.
   - `POST /api/tasks/machine` `{"runHere": bool}` for the Tasks page toggle.
5. **Run outcomes**: add `OutcomeStopped = "stopped"` (lane B). Lane A's `blockedBy` codes are the only status vocabulary Desktop shows.

Phase 2 (lane D, in-place landing, push, `pr` landing, slot trailers) and phase 3 (lane E, memory, result file, quiet rules, notify command, token cap) start from the integrated phase 1 head. Lane E rebases onto lane D before its pull request.

### Validation

Per lane: focused Go tests (`GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/<path>/...`), `make check` before commits; desktop: `make desktop-deps`, `make desktop-check`, `make desktop-build`. Lead: `make check-full` on each phase head (the change touches the vault runtime), `./scripts/rzm validate` and `frozen-scope-drift`, Greptile (`greptile review --json`) to 5/5, and the real-app checks on this Mac mini with disposable fixture repositories.

### Documentation

Amend SPEC-0127 and SPEC-0104 (done first); update `pkg/app/tasks/CONTEXT.md`, `pkg/app/web/CONTEXT.md`, `desktop/CONTEXT.md`, the vault-runtime subsystem note, `docs/reference/guides/scheduled-tasks-guide.md`, and `CHANGELOG.md` under `## [Unreleased]`.

## Original Intended Delivery

A no-op auto task runs within a minute of its wall-clock slot on the Mac mini with every window closed, through Rhizome Desktop's menu bar host, and never on a machine without "Run tasks on this machine". In-place tasks commit only their declared files in the open checkout and push safely when allowed. Quiet runs make no inbox item, and failures reach the person off-machine.

## Actual Delivered

Delivered on three stacked pull requests: phase 1 [#163](https://github.com/atomicobject/rhizome/pull/163) (`t3/scheduled-task-hosting`), phase 2 [#164](https://github.com/atomicobject/rhizome/pull/164) (`t3/scheduled-task-landing`), phase 3 [#165](https://github.com/atomicobject/rhizome/pull/165) (`t3/scheduled-task-operations`, which carries this closure).

- **Phase 1, menu bar host and on-time runs.**
  - Rhizome Desktop on macOS keeps a menu-bar-only app after the last window closes or ⌘Q. ⌘Q stashes the window session, and every window opener resumes it.
  - Quit Rhizome Completely (⌥⌘Q) asks wait or stop, freezes the host loop, drains, stops runs (including a pass that has not recorded a run yet), then stops the held runtimes.
  - The native terminate path never prevents or prompts, and stops held runtimes and the active run within 3 s.
  - Settings: Keep running in the menu bar (default on) and Launch at login (a LaunchAgent with `--login`, menu-bar-only). The menu shows next task, running task, inbox count and last failure from the runtime's host stream. It offers Run now, Pause, Open Rhizome and Quit Completely, plus one macOS notification per notice id, persisted across launches.
  - The runtime serves `GET /api/tasks/host` (SSE `HostStatus`, including `dispatching`), plus drain, stop, Run now, pause until, and machine. A 30-second wall-clock check starts `run-due --host` only while a host stream is held and not drained.
  - `rzm tasks stop` ends the run.lock holder's process tree and records it once as stopped.
  - `schedule` (`at`/`every`+`between`, `days`, `timezone`, `late`, with DST handling), per-machine settings (`runHere`, `checkout`, per-task harness/model/effort, `timezone`), and `rzm tasks machine|hosts|pause --until/--for`.
  - A pass with something due fetches first, with credentials redacted from errors. Status explains every skip (`blockedBy`). A settling pass runs every 15 minutes while items wait.
  - `requires` checks commands and MCP servers before any token is spent, each MCP listing bounded to 30 s and its process tree killed. Claude task sessions load the person's MCP config. `deny` maps to disallowed tools, and Codex refuses it. `rzm tasks doctor` reports readiness and lists invalid task files.
- **Phase 2, in-place landing and push.** `land: in-place|pr` and `writes`.
  - In-place runs fast-forward with `read-tree -m -u` and commit only task-owned paths through a private-index `commit-tree`, moving the branch under a locked `update-ref` transaction.
  - Pre-dirty, staged, assume-unchanged, ignored-before-run, outside-`writes`, and nested-repository paths are never committed.
  - Protected paths are restored through checkout filters, never through symlinks, and never across a branch switch. A failed run's files are adopted only while unchanged.
  - Push needs `tasks.allowPush` and the person's push setting. It checks the push destination's ref, publishes only Rhizome-landed commits, never forces or rebases, and replays the single task commit on divergence.
  - `pr` landing pushes the branch and opens a pull request.
  - Slot trailers dedupe across machines, and widened landings run as notify until `rzm tasks approve`. Discard and cleanup never touch the open or host checkout.
- **Phase 3, operations.**
  - `memory: true` handoff notes, trimmed safely to 8 KB.
  - `$RZM_TASK_RESULT_FILE` (`quiet|ok|attention`), read only as a regular file.
  - Quiet rules: not-needed, no-change, quiet, ok, landed in-place, and quiet runs with an opened pull request make no inbox item.
  - A per-person `notify.command` alert, queued durably in state and delivered after run.lock is released, including one alert per distinct pass-level failure (untrusted, unreadable primary ref, fetch failing over 6 h).
  - `rzm tasks notify-test`, and a per-repository daily token cap (UTC day) with `blockedBy: token-cap`.

## Execution Notes

- 2026-11-22T16:38:28Z: Effort opened; SPEC-0127 and SPEC-0104 amended to carry the approved plan as acceptance criteria.
- 2026-11-22T16:45:00Z: Phase 1 lanes A (schedules, settings, status), A2 (requires, MCP, doctor), B (runtime host), C (desktop), and phase 2 lane D started as Claude Opus 5.5 xhigh T3 delegates in their own worktrees. Lane E (phase 3) started after lanes A and A2 were integrated.
- 2026-11-22T18:00:00Z: Integration resolved overlaps between lanes. NextDueAt keeps lane A's schedule-aware rules with lane B's per-commit cache. Per-task harness overrides flow through `HarnessAgent.resolve`, so preflight, doctor, and runs agree. Slots come from the schedule. Lane E's quiet rules, result file, and alerts were extended to in-place runs (`TestInPlaceRunsFollowTheQuietRules`).
- 2026-11-22T18:30:00Z: Independent reviews (GPT-6.2 Sol, high) of each phase. Phase 1: 8 P1 and 4 P2 (stop targeting, drain/dispatch race, runHere mid-pass, blocked items, repeated DST hour, respawn loop, doctor hooks, fetch credential redaction). Phase 2: 14 P1 checkout-safety findings, which led to replacing `merge --ff-only`/`commit --only`/`pull --rebase`. Phase 3: 5 P1 (FIFO/symlink result file, symlinked handoff, pass failures never alerting, alerts under run.lock, quiet PR runs). All fixed in dedicated fix lanes with regression tests shown failing first.
- 2026-11-22T19:00:00Z: Real-app check on the Mac mini with a separate bundle identifier, data dir, and HOME, and a throwaway fixture repository. All passed: login launch menu-bar-only; runtime held 3m44s past a 1-minute idle timeout; host stream status and the untrusted-pass report; ⌘Q with two windows then reopen restores both; Quit Completely leaves no runtime or task process; login → Quit Completely → relaunch restores the layout; pause and resume; the quit Apple Event exits in 0.28 s with no prompt. One menu label bug was fixed (ef7f5efdc).
- 2026-11-22T19:30:00Z: A second phase 2 review on Sol was cut off by the provider's safety filter. Claude Fable 5.2 (high) completed it and found one P1 (ignore rules changing mid-run) and five P2, all fixed.
- 2026-11-22T20:00:00Z: Greptile on phase 1, rounds 1–5. Nine findings were fixed, plus a `cmd.Cancel` race it raised on #163. It repeats two already-fixed comments (it excludes binary files, so it reports the tracked `menu-bar-template.png` as missing).
- `make check-full` on phase 1: two runs under load averages of 14–70 from parallel lanes each failed one untouched timing test (`TestIdentifierBlockIDMigrationCancelsSectionTraversal`, `TestDelegatedProcessOwnsLauncherPIDAndReceivesTermination`). Both pass in isolation and on the quiet run. A third failure was a real race in a new fixture (`TestStopEndsTheRunsProcessTreeAndRecordsItOnce` read a half-written PID file), fixed in 33c282aa4. `make check-full` then passed on 33c282aa4. `make check-full` passed on phase 2 at 6e1e18720 and on phase 3 at 443272527 (after a file-size split; the first phase 3 run failed only `file-size-check`).
- `make desktop-check` passed (198 Rust tests, clippy `-D warnings`) and `make desktop-build` built the app. `rzm validate` and `frozen-scope-drift`: 0 issues.

## Deviations

- 2026-11-22, review fixes on the integrated phase 1 (additive to contract 4): `HostStatus` and the drain response gain `dispatching`, notices gain `name`, stop also ends a started run that holds no `run.lock` yet, and Run now launches through the task's item, which is equivalent to `--task`.
- 2026-11-22T17:05:00Z, routine resolution within the approved plan: `land` defaults to `in-place` only when a task declares `writes`, and to `pr` (today's local worktree branch, no push unless allowed) otherwise. In-place requires `writes`, so a literal in-place default would make every existing task file, including the shipped `compounding/mine`, invalid. SPEC-0127 US12 states the rule.

- 2026-11-22T18:40:00Z, routine resolution within the approved plan (phase 2 data-safety review): in-place landing no longer shells out to `git merge --ff-only`, `git commit --only`, or `git pull --rebase`, which can autostash, run hooks, overwrite ignored files, commit a folder swap over staged work, land on a branch switched to midway, or rewrite a person's commit. It fast-forwards with `read-tree` and commits with `commit-tree`, moving the branch under a `git update-ref` transaction that locks HEAD and the branch; on divergence it remakes its single commit on the fetched upstream. Pushes check the push destination's own ref, and push only commits Rhizome landed, with `--force-with-lease` set to the commit the destination reported, so the push lands only while the remote still holds that commit, which the pushed commit descends from: always a fast-forward, never a force (a plain push would republish commits a remote rewound since the check). Approval accepts only the landing the notice described. US12 and US13 now state these behaviours; each situation Rhizome cannot handle safely skips with one notice.

## Closure Checklist

- [x] Required quality gates pass (see Execution Notes; CI on each pull request is the merge gate).
- [x] Alignment and actual outcomes are verified, except the time-bound and human-only done-when items carried forward below.
- [x] Specs and documentation are reconciled: SPEC-0127 (US3, US9–US14, Requirements, Non-Goals, Decisions) and SPEC-0104 (US7–US8) match delivery. The CONTEXT files, the scheduled-tasks guide, the command-line guide, the vault-runtime subsystem note, desktop CONTEXT, and CHANGELOG are updated.
- [x] Follow-ups are triaged (below).

Carried forward as action items. They need real time or Drew, and cannot be verified from this session:

- [ ] Phase 1 soak: enable a no-op auto task with `schedule` on the Mac mini, close all windows, and confirm it fires within a minute of its slot for three days, including across sleep; confirm it never runs on the laptop (runHere off there) #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Trust a fixture repository with `rzm trust`, then run a task that uses an MCP server under Claude Code and under Codex (requires.mcp, doctor), and exercise Quit Completely's wait/stop question with a task running #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Turn on Launch at login and log out and back in (and restart once); confirm Rhizome starts menu-bar-only and never blocks logout or restart; confirm an updater restart keeps the session #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Phase 2 live check on a disposable clone with a real agent: an in-place task landing while another clone pushes, then Discard on an in-place item (covered by tests; not yet with a real harness) #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Phase 3 live check: configure `tasks.notify.command` (for example ntfy) and run `rzm tasks notify-test`, then confirm a host failure reaches your phone and a quiet run makes no inbox item #action-item
  assignee:: [[Drew Colthorp]]

## Compounding Follow-ups

- Running five implementation lanes in parallel on one machine pushed load averages to 40–100, which made untouched timing tests fail and slowed gates. Run full gates only after lanes finish, one checkout at a time.
- Greptile repeats already-fixed comments on a large branch and excludes binary files, so it reports tracked images as missing. Use an independent model review for large diffs and treat repeated Greptile comments as stale after checking the tree.
- GPT Sol reviews of Git data-safety code can be stopped by the provider's cybersecurity filter; Claude Fable completed the same review.

## Status

Complete on the phase 3 branch. Delivery is carried by [#163](https://github.com/atomicobject/rhizome/pull/163), [#164](https://github.com/atomicobject/rhizome/pull/164), and [#165](https://github.com/atomicobject/rhizome/pull/165). Drew merges them in order. The carried-forward action items above need real time or Drew.
