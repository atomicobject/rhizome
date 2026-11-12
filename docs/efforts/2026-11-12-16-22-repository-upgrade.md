---
type: EffortNote
id: EFF-2026-11-12-16-22
name: Repository upgrade
created-at: 2026-11-12T21:22:20Z
status: complete
summary: Make advancing a repository's Rhizome also refresh its generated agent files, with a reviewed desktop flow, a CLI hand-off, and detection of files behind the running Rhizome.
aliases: [EFF-2026-11-12-16-22]
governing-specs:
  - "[[repository-upgrade]]"
---

# Repository upgrade

## Scope

Deliver [[repository-upgrade|SPEC-0139]]. The motivating gap: the desktop pin offer and `rzm update` change only the pin, so committed skills and managed guidance stay on the previous release with no warning.

## Spec Set (Frozen)

- [[repository-upgrade|SPEC-0139]], initial revision on this branch dated 2026-11-12, all requirements and US1 through US3.

## Stories In Scope (Frozen)

- [[repository-upgrade#^SPEC-0139-US1]]
- [[repository-upgrade#^SPEC-0139-US2]]
- [[repository-upgrade#^SPEC-0139-US3]]

## Spec Coverage Checklist

- [x] Upgrade plan from the app's bundled Rhizome and the reviewed advance-then-apply flow with retry.
- [x] Background detection of generated files behind the worktree's Rhizome.
- [x] `rzm update` rerun hand-off.
- [x] Regression tests, required gates, and visual verification of the sheet and toolbar states.

## Plan

1. Go: add the bridge `upgrade-plan` operation that runs the app's bundled Rhizome `init --check --json` for a worktree with an advance offer, and the `rzm update` hand-off to the new executable's `init`. Reuse `runSetupJSON` and the existing `settings-report` and `settings-apply` operations rather than new apply paths.
2. Desktop: route the pin offer through a review sheet, chain `pin-advance` then `settings-apply`, show progress, result, and retry, and run a background `settings-report` after a worktree opens to offer a files-only update.
3. Review the visual result against the design, then review the combined diff with Sol before opening the pull request.

## Original Intended Delivery

Accepting an upgrade lands the new pin and the new release's generated files as one reviewable, uncommitted change, from the app or the CLI, and the app notices generated files that are behind the Rhizome a worktree runs.

## Actual Delivered

All three stories are implemented. The bridge `upgrade-plan` operation shares the pin-advance eligibility checks and returns the bundled Rhizome's `init --check --json` plan. The desktop pin offer and the repository settings update buttons open `UpgradeSheet`, which reviews the pin, change lines, files, and kept edited files, then runs `pin-advance`, `settings-apply` with the newly pinned binary, and the existing reopen or restart, resuming a failed step without repeating completed ones. The result lists changed files and copies a commit command. A background `settings-report` per opened, trusted, configured worktree shows "Update Rhizome files" when writes are pending and no advance offer is showing. `rzm update` runs the new executable's `init` after it advances a managed pin or updates the global Rhizome an unpinned configured repository runs; `--yes` makes that rerun non-interactive and a failed rerun keeps the update and prints the retry command.

## Execution Notes

- 2026-11-12: Drew agreed to the design and asked for full-stop delivery with GPT-6.1 Sol workers at xhigh effort on the priority tier, a design review by the parent before the Sol review loop, and a pull request left unmerged.
- The Go worker (`ff0ae23ce`) and desktop worker (`42b113d7e`) ran in parallel isolated worktrees against a fixed bridge contract and were merged without conflicts.
- The Go worker also made `rhizome.version: latest` resolve like an unpinned folder in `update.ResolveInvocation`; without it the CLI hand-off treated `latest` as an invalid exact pin.
- Design review against the worker's captures (plan, progress, failed file update, result, toolbar pill, light and dark) kept the layout and changed two controls in `99e6ad40e`: Cancel is hidden while a step runs instead of shown disabled, and the "Update Rhizome files" pill stays hidden while the upgrade sheet is open.
- Sol review: round 1 raised one `should` (the restart step completes when native `open` is acknowledged, before startup finishes). Declined with evidence: native open events drive `open.step` to "error" and the workspace's existing error panel offers "Try again", the same behavior the previous `onAdvance` had. Round 2 marked it resolved and approved.
- Verification on the merged branch: `make check` passed (Go unit tests and 272 web test files, 2224 tests); `cd desktop && npm run check` passed (184 tests); `make desktop-check` passed (184 desktop tests, 146 Rust tests, Clippy). The desktop worker reported `make desktop-build` passing and the packaged app opening a disposable repository on its branch. The Go worker's broad runs on its branch failed `TestStartCommandOpenFlagCanBeDisabled` and `TestRunHeadlessSurvivesUnavailableDiagnosticsCapture` in untouched code while both workers ran concurrently; both passed individually and the merged-branch `make check` passed.

## Deviations

- The design sketch put the upgrade in an in-page notice card with per-file keep or take choices. The app already offers pin updates as a toolbar pill, so the upgrade reuses it and shows progress and results in the sheet. Per-file choices need a new `init` flag; edited files are kept and the sheet points to `rzm init` in a terminal.
- Detection reuses `settings-report` with the worktree's executable instead of recording a generated-by version in `.rhizome/generated-files.yml`, which would change on every release and conflict between teammates.

## Compounding Follow-ups

- [ ] Report generated files behind the running Rhizome to agents through runtime readiness and `rzm agent` sessions. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Offer per-file keep or take choices for edited generated files in the desktop upgrade sheet. #action-item
  assignee:: [[Drew Colthorp]]
- [ ] Click through a real pin upgrade in the packaged app once a release newer than a fixture's pin exists, including a failed file update and Retry. #action-item
  assignee:: [[Drew Colthorp]]

## Closure Checklist

- [x] Approved scope delivered or deviations recorded.
- [x] Independent review completed and accepted findings fixed.
- [x] Delivery branch and commit recorded.

## Status

Complete on `t3/desktop-skill-init-upgrade`. The pull request number is recorded once it exists.
