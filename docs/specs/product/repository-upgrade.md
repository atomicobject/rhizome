---
type: ProductSpec
id: SPEC-0139
summary: Upgrading a repository's Rhizome also updates the agent files Rhizome generates, through one reviewed change in the desktop app and in `rzm update`, and the app notices when those files fall behind.
spec-status: active
last-updated: 2026-11-12
aliases: [SPEC-0139]
---

# Repository upgrade

## Summary

A repository's version pin and the agent files `rzm init` generates (skills, managed blocks in `AGENTS.md` and `CLAUDE.md`, hook and sandbox settings, module files) are both committed, and both come from one Rhizome release. Today advancing a pin changes only `.rhizome/config.yml`, so agents keep the previous release's skills until someone remembers to run `rzm init`, and nothing tells them.

This spec makes the pin and the generated files one upgrade. The invariant is that a repository's generated files match the Rhizome that runs for it. The desktop app and `rzm update` both produce the whole change, the person reviews it before anything is written, and Rhizome leaves it uncommitted with the paths to commit. The app also notices generated files that are behind the Rhizome a repository already runs.

This extends [[desktop-distribution-and-updates#^SPEC-0122-US4]] and [[version-pinned-install-and-update]] and reuses the machine rerun documents of [[desktop-repository-setup|SPEC-0118]] (`rzm init --check --json` and `rzm init --json` on a configured folder).

## Goals

- Advancing a pin from the app or from `rzm update` also refreshes the generated files with the new release.
- A person sees what an upgrade will write before it writes anything.
- The app notices when generated files are behind the Rhizome a worktree runs, whatever the cause, and offers the same reviewed update.
- A failure part way through leaves a state the next check recognizes and can finish.

## Non-Goals

- Committing, branching, or pushing for the person.
- Choosing per file between an edited file and the new version in the app. Edited files are kept, as every run without a terminal keeps them; the terminal `rzm init` still offers that choice.
- Reporting stale generated files to agents through runtime readiness or `rzm agent` sessions. That is a follow-up.
- Changing how the app updates itself.

## User Stories

### US1 - Upgrade a repository from the app

- id:: ^SPEC-0139-US1
- summary:: A person accepts the app's offer to advance an older pin, reviews the pin and generated-file changes, and gets both in one step.
- status:: ready

#### Acceptance Criteria

- The existing pin offer opens a review sheet instead of advancing immediately. The sheet is built from the app's bundled Rhizome planning a rerun of the worktree (`init --check --json`), so it shows the new release's changes before anything is installed or written.
- The sheet shows the pin change, the planned change lines, the files they touch, and edited files that will be kept with a pointer to `rzm init` in a terminal for choosing.
- Accepting advances the pin through the existing pin-advance path, then applies the rerun with the newly pinned Rhizome, then reopens the worktree so its runtime uses the new build. Progress is visible per step.
- The result names the files changed and gives a commit command for them. Nothing is committed.
- If the file update fails after the pin moved, the sheet shows the reason and a Retry that repeats only the file update.
- "Not now" keeps the existing per-version dismissal.

### US2 - Notice generated files behind the running Rhizome

- id:: ^SPEC-0139-US2
- summary:: A person whose worktree's generated files are older than the Rhizome it runs sees a quiet offer to update them.
- status:: ready

#### Acceptance Criteria

- After a trusted, configured worktree opens, the app plans a rerun with the worktree's own executable in the background. Pending writes show a toolbar offer to update Rhizome files. No pending writes, an unsupported executable, or a failed check shows nothing and never blocks the workspace.
- This covers a teammate's pin bump committed without refreshed files, a repository that follows the app's or PATH's Rhizome, and a file update that failed earlier.
- The offer opens the same review sheet without the pin row. Applying it runs the rerun with the worktree's executable and reports the result as in US1.
- Files someone edited do not by themselves produce an offer.
- When a pin offer is showing, it covers this case and only the pin offer appears.

### US3 - Upgrade a repository from the CLI

- id:: ^SPEC-0139-US3
- summary:: `rzm update` run in a repository updates the generated files whenever it changes the Rhizome that repository runs.
- status:: ready

#### Acceptance Criteria

- When `rzm update` advances the repository's pin, or updates the executable an unpinned configured repository runs, it then runs that new executable's `rzm init` in the repository root.
- In a terminal that rerun asks its usual single confirmation, including choices for edited files. With `--yes` or without a terminal it applies without asking and keeps edited files.
- An update that does not change the repository's Rhizome does not run `init`.
- A failed rerun reports its error and the command to retry, and does not undo the update.

## Requirements

- Every surface derives "behind" from a rerun plan of the Rhizome that will run, not from a stored version, so repeated or interrupted upgrades converge.
- Plans and applies run the intended executable explicitly. A PATH lookup or repository delegation never substitutes another build.
- Existing trust, pin-offer eligibility, and per-version dismissal rules continue to apply.
- Regression tests cover the plan request, the advance-then-apply sequence including a failed apply and its retry, the background check states, and the CLI rerun hand-off in each mode.

## Open Questions

None blocking. Agent-facing staleness reporting is a planned follow-up.
