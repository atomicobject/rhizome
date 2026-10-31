---
type: EffortNote
id: EFF-2026-10-31-14-31
aliases: [EFF-2026-10-31-14-31]
name: Scheduled tasks, phase 2 — task history
created-at: 2026-10-31T18:31:17Z
status: active
plan-approved-by: Drew Colthorp
summary: "Build phase 2 of scheduled tasks: run IDs and per-run folders, harness transcripts for every agent run, bounded per-task retention, runtime routes, and a Tasks page in the workbench and Rhizome Desktop that lists registered tasks, when each is due, and each run's outcome and collapsed transcript."
governing-specs: ["[[scheduled-tasks]]"]
---

# Scheduled tasks, phase 2 — task history

## Scope

Implement [[scheduled-tasks#^SPEC-0127-US8|US8]] of [[scheduled-tasks|SPEC-0127]] and the Tasks page's inbox actions (Delivery Phases, phase 2).

Phase 3 joined this effort on 2026-10-31 (see Deviations): the Rhizome Desktop badge, system notifications, Open worktree, and Start from the desktop.

Excluded: a task-level "Run now" that ignores cadence; Agent page changes; diff reconstruction for harnesses that send none; streaming a block before it completes; listing draft runs.

## Spec Set (Frozen)

- [[scheduled-tasks|SPEC-0127]], 2026-10-31 revision.

## Stories In Scope (Frozen)

- [[scheduled-tasks#^SPEC-0127-US8|US8]] See registered tasks and what each run did.
- [[scheduled-tasks#^SPEC-0127-US6|US6]]: the item actions on the Tasks page, and (added 2026-10-31) its Rhizome Desktop criterion: the badge, notifications, and Open worktree, plus Start from the desktop.

## Plan Approval

- 2026-10-31T20:22:42Z: Drew Colthorp widened the scope with "Implement the entire thing" (full stop): phase 3 joins this effort and PR #61.

- 2026-10-31T18:31:17Z: Drew Colthorp approved the council-refined plan (five rounds with Claude Fable 5.1 and GPT-6.1 Sol, final scores 8.5 with the two corrections applied, and 9) with "Execute". Approved revision: the Plan and Contracts sections below as first committed.

## Contracts Fixed by This Plan

**Run folder:** `task-state/runs/<run id>/` with `check.log` (check stderr only, capped at 1 MB by a writer that drops further bytes silently and appends `[check log truncated]`; never the failing `limitWriter`) and, only when agent work ran, `events.jsonl`. A run ID is `YYYYMMDDTHHMMSSZ-<8 hex>` (UTC), validated against `^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$` before any path join. The legacy `logs/` folder is removed.

**Go types (pkg/app/tasks):**

```go
const OutcomeInterrupted = "interrupted"

type RunRecord struct {
	ID        string    `json:"id,omitempty"`
	Task      string    `json:"task"`
	StartedAt time.Time `json:"startedAt,omitzero"`
	At        time.Time `json:"at"`              // finish time, as today
	Outcome   string    `json:"outcome"`
	Stage     string    `json:"stage,omitempty"` // "check" | "agent"; only on State.Running
	Tokens    int64     `json:"tokens,omitempty"`
	Message   string    `json:"message,omitempty"`
	Item      string    `json:"item,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Commit    string    `json:"commit,omitempty"` // base commit
	Worktree  string    `json:"worktree,omitempty"`
	Changed   []string  `json:"changed,omitempty"`
}
// State gains:
//   Running *RunRecord                  `json:"running,omitempty"`
//   Tokens  map[string]map[string]int64 `json:"tokens,omitempty"` // task -> "YYYY-MM-DD" (UTC) -> tokens, 30 days
// Item gains:
//   Run         string `json:"run,omitempty"`         // attempt that owns the item's worktree
//   LaunchError string `json:"launchError,omitempty"` // cleared on the item's next state change
```

**Agent request:** `AgentRequest` gains `Transcript string` (path to `events.jsonl`; empty means no capture). `AgentResult` keeps `Message` and `Tokens`.

**Transcript lines:** one JSON object per line, in the `agentchat.Event` shape (`id` = sequence number from 1, `sessionId` = run ID, `type`, `role`, `content`, `toolName`, `result`, `error`, `createdAt`), produced by an exported `agentchat.EventFromHarness`. Two extra `type` values: `final-message` (`content` = the message `RunPrompt` returns, bounded to 64 KB) and `transcript-truncated`. Rules:

- Text and reasoning deltas merge per (kind, itemId). The buffer flushes on a change of kind or item, on any other event except `token-usage`, and at the end. Diagnostics are not written but do flush. A completed text or reasoning event with non-empty text for the same itemId replaces the unflushed buffer, or the most recent written block with that itemId, or becomes a new block. Empty-text events are skipped.
- Command, tool, and file events: write `started` and every terminal phase (`completed`, `failed`, `declined`, `cancelled`); drop `updated`. Keep `content` (Text) when it is non-empty and drop `result.output`; otherwise keep `output`. Text is capped at 16 KB and Input is bounded with `boundedRaw` (moved into `pkg/harness`), setting `result.truncated`.
- `error` events are written. One whose `result.status` is `retrying` is not fatal. A `turn-completed` with phase `failed` or `declined` is fatal even without an error event.
- Token usage is written once, at the end, as the last `token-usage` seen.
- The transcript is capped at 10 MB; past it, one `transcript-truncated` line is written and later events are dropped, except `final-message` and the final `token-usage`, which are always written.
- Writes use O_APPEND, one write per line, 0600. Readers drop a trailing partial line.

**Retention** (under run.lock, at the end of every execution path): per task, keep the newest 20 records whose folder has `events.jsonl` plus the newest 100 other records (the global 500 cap is removed; `Last` stays). Keep `events.jsonl` for the newest 20 runs that have one and `check.log` for the newest 20 runs of any kind. Delete other payload files, empty folders, and folders not referenced by history and older than 7 days (by the timestamp in the run ID). The 7-day grace keeps draft runs, which are never recorded, and covers a run still in progress. Token buckets are trimmed to 30 days and seeded once from history when `State.Tokens` is nil. `tokensLast30Days` sums the buckets.

**Lifecycle:** every execution path (RunDue, Run, RunDraft) takes run.lock and first finalizes any `State.Running` as one `interrupted` record (keeping its folder), before ResolvePrimary or notePassError; `bookkeep` no longer records a second failure for that item. `Record` clears `Running` only when the IDs match. `Running.Item` is set at start when an item exists and when an item is created. `fail` sets branch, worktree, and commit as fields. Draft runs mint IDs and write folders but never call `Record` or touch token buckets; their item still appears in the inbox.

**Service methods** (pkg/app/tasks, used by the web routes):

```go
func Open(ctx context.Context, opts OpenOptions) (*Service, error) // moved from cmd/tasks.go openTasksService; cmd and pkg/app/web both call it
func (s *Service) LiveState(ctx context.Context) (LiveState, error) // reads state.json and the run.lock holder; no git
func (s *Service) Runs(task string) ([]RunView, error)              // newest first
func (s *Service) RunDetail(id string, after int64) (RunDetail, error)
func (s *Service) PrecheckRun(ctx context.Context, itemID string) error // item due or needs attention, trusted, run.lock free
func (s *Service) RecordLaunchError(ctx context.Context, itemID, reason string) error

type LiveState struct {
	Paused  bool        `json:"paused"`
	Running *RunRecord  `json:"running,omitempty"`
	Stale   bool        `json:"stale,omitempty"` // Running set but the run.lock holder is gone (dead PID or a different runtime identity)
	Items   []Item      `json:"items"`
	Recent  []RunRecord `json:"recent"` // newest 50 across tasks
}
type RunView struct {
	RunRecord
	HasTranscript bool `json:"hasTranscript"`
	HasCheckLog   bool `json:"hasCheckLog"`
}
type RunDetail struct {
	Run      RunView           `json:"run"`
	Events   []json.RawMessage `json:"events"`             // transcript lines after the offset
	Next     int64             `json:"next"`               // byte offset to resume from
	CheckLog string            `json:"checkLog,omitempty"` // only when after == 0
}
```

**HTTP routes** (pkg/app/web, JSON, same auth and vault scoping as `/api/agent`):

| Method and path | Body | Response |
| --- | --- | --- |
| `GET /api/tasks` | | the `rzm tasks status --json` Report; merge warning cached for 10 minutes |
| `GET /api/tasks/state` | | `LiveState` |
| `GET /api/tasks/runs?task=<id>` | | `{"runs": [RunView]}` |
| `GET /api/tasks/runs/<run id>?after=<n>` | | `RunDetail`; `after` clamped to [0, size], negative or non-numeric is 400, an invalid run ID is 400, a missing run is 404 |
| `POST /api/tasks/items/<item>/run` | | `{"launchedAt": RFC3339}` after `PrecheckRun` passes and `rzm tasks run <item>` is spawned with `appruntime.StartDetached`; precheck failures are 409 with `{"error"}` |
| `POST /api/tasks/items/<item>/{snooze,skip,discard,done}` | snooze: `{"until": RFC3339}` | `{}` |
| `POST /api/tasks/modes/<task>` | `{"mode": "auto"\|"notify"\|"off"\|"default"}` | `{}` |
| `POST /api/tasks/pause`, `POST /api/tasks/resume` | | `{}` |

**Page polling:** `GET /api/tasks` on load, on focus, and every 60 s. `GET /api/tasks/state` every 2 s while `running` is set, and after a POST run until a run or record with `startedAt >= launchedAt` appears for that task, the item shows a `launchError`, or 30 s pass. The open run's detail resumes from `next` while it is running.

## Plan

The work is split across parallel worktrees, each owning separate files, then integrated on this branch.

1. **Transcript (worker A):** `pkg/app/tasks/transcript.go` (writer, reader with offset, bounds), the `RunPrompt` collector in `agent.go` (writes through the writer, retry and failed-turn classification, final-message), `boundedRaw` moved into `pkg/harness`, `agentchat.EventFromHarness` exported. Fixture tests for Claude and Codex streams, including one Codex stream captured from a real session.
2. **Lifecycle and retention (worker B):** run IDs, run folders and the check-log writer, `State.Running` and stale finalization, `RunRecord` fields and `fail` fields, draft handling, per-task retention and token buckets with seeding, launch errors, and the service methods above (`Open` included, with `cmd/tasks.go` switched to it).
3. **Runtime routes (worker C, after B lands):** the HTTP routes, detached run launch, merge-warning cache, route tests.
4. **Tasks page and desktop section (worker D):** API client and types, the Tasks route and panes, ThinkingBlock, CommandBlock, and ToolBlock under the Tasks feature, polling, narrow layout, and the desktop sidebar section (`desktopHost.ts`, `desktop/src/api.ts`, `PageControls.tsx`, `page.rs`). Built against the contract with mocked responses, then checked against the real routes.
5. **Integration (lead):** merge, gates, a live run through the page, review, PR.

## Spec Coverage Checklist

- [x] US8, backend: run IDs and folders, per-run record fields, interrupted runs, drafts, retention and token totals, launch errors (`pkg/app/tasks/history_test.go`, `review_fixes_phase2_test.go`, `cmd/tasks_test.go`); transcripts for Claude Code and Codex from recorded real turns plus synthetic edge streams, retry and failed-turn classification (`transcript_test.go`, `agent_test.go`); routes and detached Run (`pkg/app/web/tasks_test.go`).
- [x] US8, page: task list, run history, run view with collapsed blocks, live progress, launch waiting, interrupted runs, desktop section (`web/src/components/tasks/*.test.ts*`, `AppShell.test.tsx`, `desktop` vitest, `cargo test page::`).
- [x] US6 (Tasks page actions): Run, Snooze, Skip, Discard, Done, mode, pause and resume (`TasksWorkspace.test.tsx`, `pkg/app/web/tasks_test.go`).

## Original Intended Delivery

As in Plan.

## Actual Delivered

PR #61. Everything in Plan, built in four parallel worktrees (`t3/task-history-transcript`, `-lifecycle`, `-routes`, `-web`) and integrated on `t3/scheduled-task-history-ui`. The real routes and page were checked against a throwaway repository served by this build: the report, live state, task list, task detail, and the untrusted notice all work. A live agent run through the page is still owed (see Status).

## Deviations

- **Scope widened to phase 3** at Drew's request. Desktop contract:
  - **Badge:** the Dock icon's badge counts the open items that need the person (due, ready, or needs attention, and not snoozed), summed across every window's repository. It is cleared when the count is 0.
  - **Notifications:** one system notification per item that newly enters due, ready, or needs attention, at most once per item and state, while the app runs. Items already in those states when a window opens do not notify.
  - **Open worktree:** a ready or needs-attention item with a worktree offers Open worktree in Rhizome Desktop, which opens that worktree in a new window through the existing open-folder path.
  - **Start:** Rhizome Desktop offers Start on a due or needs-attention item. It opens Terminal in the main checkout running `rzm tasks start <item>` (the repository's pinned or bundled rzm), which hands the person to their harness.
  - **Browsers:** in a plain browser, Open worktree and Start are not shown; the worktree path and its copy action remain.

- **Merge-warning cache moved into `tasks.Service`** (`cachedMergeWarning`, 10 minutes). A cache in the web handler could not stop `Status` from calling `gh`.
- **The runtime opens the tasks service once, with no harness drivers.** The page never runs prompts in process, because Run spawns `rzm tasks run`. A change to `.rhizome/config.yml` needs a runtime restart to reach the page.
- **Spawn failures in the run route are recorded on the item** with `RecordLaunchError`, not only returned as 500.
- **Live state is also polled every 60 s when idle**, so the page notices runs the scheduler starts. The contract only named the 2 s rate.
- **A corrected transcript block reuses its `id`.** The writer appends the corrected line with the same id, and the page keeps the last line per id.
- **Sol-loop fixes (3 rounds with GPT-6.1 Sol, approved):**
  - later check-log snapshots win;
  - a retried launch ignores an earlier launch error, with `launchedAt` sent at full precision;
  - history labels the run in progress;
  - a crash during a manual Run's check flags its item;
  - retention skips the folder sweep when state cannot be saved.

## Closure Checklist

- [x] Quality gates pass: `make verify` (check-full with race and integration tests, 1,718 web unit tests, 107 E2E tests) and `make desktop-check`, before the sol-loop fixes; the touched packages' tests after them.
- [ ] Live run of a synthetic task viewed in the Tasks page. Blocked: `rzm trust` needs Drew's interactive confirmation for the throwaway repository at `/tmp/th-live/repo`.
- [x] User guide (`docs/reference/guides/scheduled-tasks-guide.md`, "The Tasks page" and "Where state lives"), CHANGELOG, and the `pkg/app/tasks` and `pkg/app/web` CONTEXT notes updated. Story statuses stay `draft`, as for phase 1.
- [x] Follow-ups recorded.

## Compounding Follow-ups

- **Live run and desktop click-through:**
  - Trust `/tmp/th-live/repo`, let its `finalize` task run, and read the transcript in the page.
  - After merge, run `make desktop-install-dev` from the primary checkout and open the Tasks section in Rhizome Desktop.
- **Impeccable:** `.impeccable/design.json` is older than DESIGN.md, and a newer Impeccable is available. Bumping it needs the license pin in `scripts/licenses/generate.py`.

## Status

Active. Everything in scope is built and verified except the live agent run through the page, which waits on Drew trusting the throwaway repository.

## Execution Notes

- 2026-10-31: Effort opened from the council-refined plan.
- 2026-10-31: Workers A–D (Opus 5.5 high, T3 delegates) delivered their branches. The lead integrated them:
  - swapped the route stand-ins for the real types and connected the opener;
  - moved the merge-warning cache into the service;
  - fixed the page so a repeated transcript id replaces the line it holds.
- 2026-10-31: Live check against a throwaway repository: routes and page work. `rzm trust` refuses a scripted confirmation, so the agent run waits for Drew.
- 2026-10-31: Sol loop, 3 rounds with GPT-6.1 Sol: 4 should fixes plus 1 nit fixed, 1 nit declined. Round 3 approved.
- 2026-10-31: Fixed sol-loop S5 after all: `RunDue` in a repository with no tasks left records a leftover interrupted run (`TestInterruptedRunIsRecordedAfterTheLastTaskIsRemoved`). The "back-to-back Claude thinking blocks merge" follow-up was dropped: the writer already splits them at Claude's `content_block_stop` (`TestTranscriptClaudeThinkingBlocksSplitByDiagnostic`).
- 2026-10-31: Opened PR #61 (https://github.com/atomicobject/rhizome/pull/61).
- 2026-10-31: Greptile went 3 → 4 → 4 → 5/5. Fixes:
  - an attempt that ends without saving its result is recorded as failed instead of dropped;
  - the run view keeps reading an unfinished detail;
  - a failed later read keeps the transcript and offers Retry;
  - a phase 1 running item left by a crash before the upgrade is recorded;
  - the selected run's own open item keeps its actions.

  Declined: a length bound on agent message and thinking text. That text is limited by the model's output size, and the contract bounds tool and command output.
