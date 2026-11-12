---
type: EffortNote
id: EFF-2026-11-12-16-22
name: Repository upgrade
created-at: 2026-11-12T21:22:20Z
status: active
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

- [ ] Upgrade plan from the app's bundled Rhizome and the reviewed advance-then-apply flow with retry.
- [ ] Background detection of generated files behind the worktree's Rhizome.
- [ ] `rzm update` rerun hand-off.
- [ ] Regression tests, required gates, and visual verification of the sheet and toolbar states.

## Plan

1. Go: add the bridge `upgrade-plan` operation that runs the app's bundled Rhizome `init --check --json` for a worktree with an advance offer, and the `rzm update` hand-off to the new executable's `init`. Reuse `runSetupJSON` and the existing `settings-report` and `settings-apply` operations rather than new apply paths.
2. Desktop: route the pin offer through a review sheet, chain `pin-advance` then `settings-apply`, show progress, result, and retry, and run a background `settings-report` after a worktree opens to offer a files-only update.
3. Review the visual result against the design, then review the combined diff with Sol before opening the pull request.

## Decisions

- The design started from a mockup with an in-page notice card and per-file keep/take choices. The app already shows the pin offer as a toolbar pill with a menu, so the upgrade reuses that pill and puts progress and result in the review sheet. Per-file choices need a new `init` flag; edited files are kept and the sheet points to terminal `rzm init`.
- Detection reuses `settings-report` with the worktree's executable instead of recording a generated-by version in `.rhizome/generated-files.yml`, which would churn on every release and conflict between teammates.

## Follow-ups

- Report generated files behind the running Rhizome to agents through runtime readiness and `rzm agent` sessions.
- Per-file keep/take choices for edited files in the app.
