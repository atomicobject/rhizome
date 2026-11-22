---
type: EffortNote
id: EFF-2026-11-22-11-38
aliases: [EFF-2026-11-22-11-38]
name: Scheduled task hosting and landing
created-at: 2026-11-22T16:38:28Z
status: active
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

- [ ] SPEC-0104 US7: host stream, host-only dispatch, 30-second wall-clock check, drain and stop, terminate path (lanes B, C).
- [ ] SPEC-0104 US8: accessory app, custom quit items, session stash and resume, launch at login, menu bar menu, notifications (lane C).
- [ ] SPEC-0127 US3: `run-due --host`, nearest deadline first (lanes A, B).
- [ ] SPEC-0127 US9: `schedule` parsing, slot math, `late`, DST, status (lane A).
- [ ] SPEC-0127 US10: per-machine settings, `machine`, `hosts`, skip reasons, pause until, run-due output kept (lanes A, B).
- [ ] SPEC-0127 US11: `requires`, MCP passthrough for Claude and Codex, `deny`, `doctor` (lane A2).
- [ ] SPEC-0127 US12: in-place landing (lane D).
- [ ] SPEC-0127 US13: push, `pr` landing, slot trailers (lane D).
- [ ] SPEC-0127 US14: memory, result file, quiet rules, notify command, token cap (lane E).

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

Not yet delivered.

## Execution Notes

- 2026-11-22T16:38:28Z: Effort opened; SPEC-0127 and SPEC-0104 amended to carry the approved plan as acceptance criteria.

## Deviations

- 2026-11-22, review fixes on the integrated phase 1 (additive to contract 4): `HostStatus` and the drain response gain `dispatching`, notices gain `name`, stop also ends a started run that holds no `run.lock` yet, and Run now launches through the task's item, which is equivalent to `--task`.
- 2026-11-22T17:05:00Z, routine resolution within the approved plan: `land` defaults to `in-place` only when a task declares `writes`, and to `pr` (today's local worktree branch, no push unless allowed) otherwise. In-place requires `writes`, so a literal in-place default would make every existing task file, including the shipped `compounding/mine`, invalid. SPEC-0127 US12 states the rule.

- 2026-11-22T18:40:00Z, routine resolution within the approved plan (phase 2 data-safety review): in-place landing no longer shells out to `git merge --ff-only`, `git commit --only`, or `git pull --rebase`, which can autostash, run hooks, overwrite ignored files, commit a folder swap over staged work, land on a branch switched to midway, or rewrite a person's commit. It fast-forwards with `read-tree` and commits with `commit-tree`, moving the branch under a `git update-ref` transaction that locks HEAD and the branch; on divergence it remakes its single commit on the fetched upstream. Pushes check the push destination's own ref, and push only commits Rhizome landed, with `--force-with-lease` set to the commit the destination reported, so the push lands only while the remote still holds that commit, which the pushed commit descends from: always a fast-forward, never a force (a plain push would republish commits a remote rewound since the check). Approval accepts only the landing the notice described. US12 and US13 now state these behaviours; each situation Rhizome cannot handle safely skips with one notice.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

None yet.

## Status

Active. Phase 1 lanes A, A2, B, and C are next.
