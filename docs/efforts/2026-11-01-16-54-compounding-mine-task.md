---
type: EffortNote
id: EFF-2026-11-01-16-54
aliases: [EFF-2026-11-01-16-54]
name: Compounding runs as a task — shared state, task mode, and the mine task
created-at: 2026-11-01T21:54:49Z
status: active
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

- [ ] SPEC-0124 US8
- [ ] SPEC-0127 US6 re-check and Copy prompt criteria

## Original Intended Delivery

Both slices in one pull request.

## Actual Delivered

Pending.

## Deviations

None yet.

## Closure Checklist

- [ ] Every story delivered or covered by a deviation
- [ ] Gates recorded
- [ ] Pull request number recorded

## Compounding Follow-ups

None yet.

## Status

Active.

## Execution Notes

- 2026-11-01: Branch `feat/compounding-mine-task` from `89423312` (#64 merged).
