---
type: EffortNote
id: EFF-2026-11-01-16-54
aliases: [EFF-2026-11-01-16-54]
name: Compounding runs as a task — shared state, task mode, and the mine task
created-at: 2026-11-01T21:54:49Z
status: complete
plan-approved-by: Drew Colthorp
summary: "Keep compounding's private state once per repository with publication records and a lossless migration, run mining in task mode, ship the compounding/mine task with a model-free check, keep due task items current, and let any task item's prompt be copied."
governing-specs: ["[[session-memory-starter]]", "[[scheduled-tasks]]"]
---

# Compounding runs as a task — shared state, task mode, and the mine task

## Scope

Slices 1 and 2 of the compounding-workspace plan (revision 7), built on the modules from [[rhizome-modules|SPEC-0130]] (EFF-2026-11-01-13-50, #64).

Excluded: actions, view parameters, and the compounding workspace (later slices); the `compounding/review` and `compounding/maintain` tasks; moving compounding scripts to the code-mode client; carrying edited pre-module compounding skill scripts into the module.

## Spec Set (Frozen)

- [[session-memory-starter|SPEC-0124]], 2026-11-01 revision.
- [[scheduled-tasks|SPEC-0127]], 2026-11-01 revision.

## Stories In Scope (Frozen)

- [[session-memory-starter#^SPEC-0124-US8|SPEC-0124 US8]] Mining runs as a task and shares state across worktrees.
- [[scheduled-tasks#^SPEC-0127-US6|SPEC-0127 US6]]: the two criteria added 2026-11-01 (due items stay current; any open item's prompt can be copied).

## Plan Approval

Drew Colthorp approved the compounding-workspace plan (revision 7) in chat on 2026-11-01, merged #64, and asked to "move on to the next step", which this effort is.

## Contracts Fixed by This Plan

- **State root:** the main checkout's `.rhizome/compounding/`, from the parent of `git rev-parse --git-common-dir`; the ignore guard uses the same root. Bare layouts fall back to the per-user state folder keyed by the common dir.
- **Decision events:** every approval writes a local event with `{learning, payload, witness, checkout, headAtPublish, publishedAt}`; the witness is the shard, the finding note, or for a document edit the shared decision file.
- **Status order:** `merged` (witness at the primary ref) → `present` (in this checkout) → `committed-elsewhere` (at a local branch tip, via `git for-each-ref refs/heads` and `git cat-file -e`) → `published-elsewhere` (recorded checkout still a worktree with the file) → `missing` (back to pending from the payload).
- **Stale guard:** approve and dismiss take optional `--revision` and `--head`; a mismatch exits 2.
- **Lock:** a held store lock makes a change exit 2 with "mining in progress".
- **Migration:** adopt the main checkout's store, else the newest worktree store; convert legacy `reviewed.json` approvals in every store; import other stores' decisions; refuse while any store has an open run; print the count of undecided proposals left behind; leave old stores in place, marked migrated.
- **Sessions:** discovery skips working directories under `.rhizome/task-state/`.
- **Task mode:** `mine.mjs --task-mode` (budget approved, no 200-session question, non-zero exit on a credential stop).
- **Credential presence:** `rzm agent evaluate-batch --check-credentials` reports whether Jev has a credential through `provideraccess.TypeSafeSource`, without a model call and without printing it.
- **Mine task:** `.rhizome/modules/compounding/tasks/mine.yml`, id `compounding/mine`, person scope, every 7d, notify by default; its check is a module script.
- **Re-check:** only `StateDue` items re-check; `needed:false` closes as done; `needed:true` updates in place keeping `snoozedUntil`.
- **Copy prompt:** prompt rendering separated from `PlanStart`'s eligibility; `GET /api/tasks/items/{id}/prompt` returns the copy variant without the `git switch` preamble for any open item; the Tasks page offers Copy prompt.

## Plan

1. **B0 (lead):** spec amendments, this effort.
2. **B1, parallel lanes** in separate worktrees, each with focused tests:
   - **A, compounding state and approvals:** state root, decision events, status order, stale guard, lock refusal, migration (`lib/store.mjs`, `lib/shards.mjs`, `shards.mjs`, `mine.mjs` root resolution, `report.mjs`, testdata).
   - **B, task mode and the mine task:** session exclusion, `--task-mode`, `--check-credentials`, the task file and its check script, skill and guide text.
   - **C, tasks platform:** due-item re-check, prompt route, Copy prompt on the Tasks page.
   Exit: each lane's focused tests and `make check-fast` pass.
3. **B2 (lead):** merge lanes, `make build`, `rzm init --check`, dogfood the check against this repository's state, docs (compounding guide, scheduled-tasks guide, subsystem notes, CHANGELOG), `make check`, `./scripts/rzm validate`, `make web-e2e`.
4. **B3:** sol-loop review, pull request, Greptile to 5/5, CI green, closure.

## Spec Coverage Checklist

- [x] SPEC-0124 US8: state root, decision events, status order, stale guard, lock refusal and migration (`state.test.mjs`); session exclusion, task mode, the task and its check (`task.test.mjs`, `TestCompoundingInstallsTheMineTask`); `--check-credentials` (cmd test).
- [x] SPEC-0127 US6 re-check and Copy prompt criteria (`recheck_test.go`, `tasks_test.go`, Tasks page unit tests).

## Original Intended Delivery

Both slices in one pull request.

## Actual Delivered

Both slices, in [#66](https://github.com/atomicobject/rhizome/pull/66) (branch `feat/compounding-mine-task`):

- Compounding's private state lives once per repository, in the main checkout's `.rhizome/compounding/` (`lib/state.mjs`); every approval writes a decision event with its witness and payload (`lib/approvals.mjs`); `list --all` and `show` report each approval as merged, present, committed-elsewhere, published-elsewhere, or missing (back to pending); approve and dismiss take `--revision` and `--head`; a held lock refuses with exit 2; migration adopts one store, converts legacy approvals (recovering dropped proposals from run snapshots or verified clusters), imports decisions and uncommitted shared dismissals from other checkouts, refuses with an open run, and reports what it left behind. `mine.mjs` exits 2 with a reason when migration refuses.
- Mining skips sessions under `.rhizome/task-state/`; `mine.mjs --task-mode`; `rzm agent evaluate-batch --check-credentials`; the `compounding/mine` task with its model-free `task-check.mjs`.
- Due task items re-check when their task comes due again; `GET /api/tasks/items/{id}/prompt` and a Copy prompt button on the Tasks page.

Verification: 151 compounding Node tests pass; `make check` passed; `make check-fast` passed after each fix; `make web-e2e` passed (107 tests). Dogfood with the new binary: a scratch repository with compounding installed validated the task (`rzm validate tasks`), `--check-credentials` returned `{"configured":true,"source":"team"}`, `task-check.mjs` returned `{"needed":false}` without a model call, a second worktree read the main checkout's state, and in a real browser the Tasks page showed `compounding/mine` (Default · Notify, every 7d) and Copy prompt put the 1,665-character prompt, without `git switch`, on the clipboard with no console errors. Review: sol-loop, 3 rounds with GPT-6.1 Sol (approved); its findings fixed were migration of approvals whose proposals mining had dropped, uncommitted shared dismissals in other checkouts, and keeping a finding's assignee when it is restored. Greptile reached 5/5 with no comments after seventeen rounds on #66; its fixes: a failed re-check never reopens an item the person closed; the Tasks page resets Copy prompt per item; the README documents `--check-credentials`; a resumed task run verifies in a live checkout when its worktree is gone; a run stopped for a credential or model that is available again resumes instead of waiting on a person; migration takes an approved shard's published wording and marks other legacy approvals as drafts (`previous.draft`), which the skill tells the person about. Later rounds: checkouts compare by native real paths, so Windows short names match Git's (CI caught it); a recurrence is judged against its cluster's newest approval only; `revise` records the revised content as the approval's newest event; retiring a revised shard retires every version, and retirements are kept privately so other worktrees see them; a later dismissal or retirement closes a cluster's earlier approvals; migration carries saved model choices, calibration evidence, and merged extra session paths; a copy's success timer clears only its own message. Final rounds: calibration evidence matches by its run whichever path form a report holds; migration keeps the main checkout's uncommitted shared decisions too; the state tests check worktrees out without line-ending conversion, which Windows CI needed.

## Deviations

None.

## Closure Checklist

- [x] Every story delivered or covered by a deviation
- [x] Gates recorded
- [x] Pull request number recorded: [#66](https://github.com/atomicobject/rhizome/pull/66)

## Compounding Follow-ups

- A budget-stopped run needs a person (raise the budget through the skill, or `--abandon`), because the task's fixed budget cannot resume a cumulative ceiling.
- A crash mid-copy during migration leaves a partial store that the rerun adopts (`ponytail:` note in `lib/state.mjs`).
- A worktree still on an older branch runs its older scripts, which keep writing to its own store after migration marked it migrated.
- Copy prompt uses the primary branch's current commit, as Start does, not the item's original commit.

## Status

Complete. Delivered in [#66](https://github.com/atomicobject/rhizome/pull/66).

## Execution Notes

- 2026-11-01: Branch `feat/compounding-mine-task` from `89423312` (#64 merged).
