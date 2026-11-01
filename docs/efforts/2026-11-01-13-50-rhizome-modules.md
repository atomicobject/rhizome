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
- **Views:** module view files are served under `/views/_module/<module>/_files/…` (stamps and checks under `/views/_module/<module>/_stamp|_check/<folder>`, mirroring the flat routes so a module folder named `_stamp` cannot collide), folder keys are `module:<module>/<folder>`, and saving writes inside the root that holds the definition.
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

## Compounding Follow-ups

None yet.

## Status

Active.

## Execution Notes

- 2026-11-01: B0 locator landed with unit tests (`go test ./pkg/vault/assetroots/` ok).
- 2026-11-01: B1 lanes merged without code conflicts: C tasks (`22f577a2`), E module CLI and `modules` validation (`a569c04d`; one note conflict in validate.md resolved by keeping both paragraphs), B views (`9e0c8dc4`; served at `/views/_module/<module>/_files/…`, accepted), A ontology and recipes (`89f74eb4`), D init, starters, hooks, scripts (`2d5e760d`).
- 2026-11-01: Integration: `NO_WEB=1 make build` ok; focused tests over `pkg/app/cli/... pkg/vault/... pkg/ontology/... pkg/app/tasks/... pkg/app/views/... pkg/app/web/... pkg/validate/... pkg/app/validationrun/... cmd/...` all pass, including the 9 init tests that needed lane A. Dogfood `rzm init` in this repository moved 11 starter files into `.rhizome/modules/{core,action-items,agentic-engineering}`; `rzm init --check` exits 0; `./scripts/rzm validate all` checks 18 recipes, 8 views and 3 modules with no issues in this change (17 link-hygiene findings are in files this branch does not touch). Fixed the rerun summary's "Removed N file Rhizome no longer shipss" plural.
- 2026-11-01: The schema hash now includes repository-relative file names, so each repository rebuilds its ontology projection once after upgrading. Ontology embedding reuse is keyed by each type's field signature (`embedindex.EmbeddingSchemaSignature`), not by file names, so no text is re-embedded.
