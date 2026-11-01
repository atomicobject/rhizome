---
type: EffortNote
id: EFF-2026-11-01-13-50
aliases: [EFF-2026-11-01-13-50]
name: Rhizome modules — every starter is a module
created-at: 2026-11-01T18:50:32Z
status: active
plan-approved-by: Drew Colthorp
summary: "Introduce modules (.rhizome/modules/<name>/ holding ontology, recipes, views, tasks, and scripts), resolve every asset folder through one locator, install each starter as a module with a lossless migration from the flat folders, and run a module's recipes as rzm <module> <command>."
governing-specs: ["[[rhizome-modules]]"]
---

# Rhizome modules — every starter is a module

## Scope

Implement [[rhizome-modules|SPEC-0130]]: slice 0 of the compounding-workspace plan (actions, view parameters, the compounding tasks, and the workspace follow in later efforts).

Excluded: actions; grouping a module's views in the sidebar; code-indexing module scripts; moving skills, overlays, managed blocks, installed documents, or hook declarations; migrating repository-owned flat files.

## Spec Set (Frozen)

- [[rhizome-modules|SPEC-0130]], 2026-11-01 revision.

## Stories In Scope (Frozen)

- [[rhizome-modules#^SPEC-0130-US1|US1]] Every starter is a module.
- [[rhizome-modules#^SPEC-0130-US2|US2]] Existing repositories migrate without loss.
- [[rhizome-modules#^SPEC-0130-US3|US3]] Ids stay stable and unambiguous.
- [[rhizome-modules#^SPEC-0130-US4|US4]] A module's recipes are commands.

## Plan Approval

Drew Colthorp approved the plan in chat on 2026-11-01 (revision 7 of the compounding-workspace architecture, slice 0) and asked for it to be taken to a pull request ready to merge ("full stop": never merged by the agent).

## Contracts Fixed by This Plan

- **Locator:** `pkg/vault/assetroots` (`Roots`, `AllRoots`, `Classify`, `Modules`, `ModuleDir`, `FlatDir`, `Qualify`). Flat folders first, then valid modules by name. Recipes' flat folder is `.rhizome/query-recipes`; the module folder is `recipes/`. Scripts have no flat folder.
- **Ontology:** source names, hashes, and errors use repository-relative paths; the schema cache keys by repository root.
- **Recipes:** module `recipes/` folders are explicit recipe roots; discovery lists modules on each load.
- **Views:** module view files are served under `/views/_module/<module>/…`, folder keys are `module:<module>/<folder>`, and saving writes inside the root that holds the definition.
- **Tasks:** a module task's id is `<module>/<file name>`; the id pattern allows one `/`; run data, worktree names, and the pull-request prefix use a collision-free form; web routes read the escaped path.
- **Init:** starter templates live under `starters/<id>/module/<kind>/` and install to `.rhizome/modules/<id>/<kind>/`; unedited flat copies recorded in `generated-files.yml` are removed in favor of the module copy; an edited flat copy is kept and its module copy skipped, with a report; ontology identifier contracts carry over; `.rhizome/.gitignore` allowlists `modules/`.
- **Hooks:** `{module}` resolves to `.rhizome/modules/<starter id>`.
- **Compounding scripts:** written once to `.rhizome/modules/compounding/scripts/`; `models.json` and the calibration fixture sit at the module root and under `references/` so relative paths are unchanged.
- **CLI:** `rzm <module>` and `rzm <module> <recipe id> --<input> <value>`; `rzm module <name> [command]` always works; built-ins win; validation rejects module names that equal a built-in or break the pattern.

## Plan

1. **B0, foundation (lead):** spec, this effort, and `pkg/vault/assetroots` with tests. Exit: locator tests pass; committed before lanes start.
2. **B1, parallel lanes** in separate worktrees from the B0 commit, each with focused tests:
   - **A, ontology and recipes:** schema loading, cache, watchers (`watchhub`, `bootstrap/watcher_sync`, `cache/service`), recipe roots, validation-run presence checks, `cmd/ontology`, search-quality isolation, release evidence scripts.
   - **B, views:** loader roots, service, save confinement, eject, custom-view serving and stamps, the change poller, custom-view validation, CLI text.
   - **C, tasks:** taskdef ids, Git tree discovery, data and worktree keys, pull-request prefix, web routes, `rzm validate tasks`.
   - **D, init and starters:** module install targets, starter tree move, migration of flat installs, identifier carry-over, gitignore allowlist, `{module}` hooks, compounding script move and path fixes, init and hook tests, compounding test fixtures and the session-memory bench.
   - **E, CLI:** `rzm <module>` dispatch, `rzm module`, recipe inputs as flags, module-name validation.
   Exit: each lane's focused tests and `make check-fast` pass on its branch.
3. **B2, integration (lead):** merge lanes, `make build`, dogfood `rzm init` migration in this repository and `rzm init --check`, documentation (subsystem notes, SPEC-0127, SPEC-0039, custom-views skill, starters README, compounding guide and skill, CHANGELOG), `make check`, `./scripts/rzm validate`, `make web-e2e` if web code changed. Exit: all gates pass.
4. **B3, review and closure:** independent review loop, Greptile to 5/5, CI green, effort closure on the branch.

## Spec Coverage Checklist

- [ ] US1 every starter is a module
- [ ] US2 lossless migration
- [ ] US3 stable ids
- [ ] US4 module commands

## Original Intended Delivery

All four stories of SPEC-0130 in one pull request.

## Actual Delivered

Pending.

## Deviations

None yet.

## Closure Checklist

- [ ] Every story delivered or covered by a deviation
- [ ] Gates recorded
- [ ] Pull request number recorded

## Status

Active.

## Execution Notes

- 2026-11-01: B0 locator landed with unit tests (`go test ./pkg/vault/assetroots/` ok).
