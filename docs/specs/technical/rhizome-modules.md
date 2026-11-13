---
type: TechnicalSpec
id: SPEC-0130
aliases:
  - SPEC-0130
  - Rhizome modules
summary: "A module is a folder under .rhizome/modules/<name>/ that groups the ontology, query recipes, views, tasks, and scripts one capability ships. Every starter installs as one module; repository-owned files in the flat .rhizome folders keep working. One asset locator gives every loader both layouts, rzm init migrates installed starter files, and rzm <module> <command> runs a module's recipes from the command line."
spec-status: active
last-updated: 2026-11-12
---

# Rhizome modules

## Summary

A starter today installs its ontology, query recipes, views, and skill scripts into folders shared with everything else: `.rhizome/ontology`, `.rhizome/query-recipes`, `.rhizome/views`, and each harness's skill folder. Nothing on disk says which files belong together, a new capability has to be spread across those folders, and an agent authoring one has to know every location.

A **module** is one folder that holds everything a capability ships:

```text
.rhizome/modules/<name>/
├── ontology/    # *.graphql
├── recipes/     # query recipes
├── views/       # configured and custom views
├── tasks/       # scheduled tasks
└── scripts/     # Node code used by tasks, hooks, and skills
```

Every starter installs as one module named after the starter. Repository-owned files in the flat folders keep working, so nothing a team wrote moves. Rhizome finds both layouts through one asset locator. `rzm init` moves the starter files it installed into their module, and `rzm <module> <command>` runs a module's query recipes from the command line.

This is slice 0 of the compounding-workspace plan: later work adds actions, which exist only inside modules ([[scheduled-tasks|SPEC-0127]] tasks and the compounding starter, [[session-memory-starter|SPEC-0124]], build on it).

## Goals

- Group each capability's Rhizome assets in one folder that a starter installs, refreshes, and removes as a unit.
- Let an agent author a new capability in one place.
- Keep every existing repository working unchanged, including files a team created in the flat folders, view preferences, and recipe ids scripts call.
- Give modules a command-line surface named after the module.

## Non-Goals

- Actions. They arrive in a later spec and live only inside modules.
- Moving skills, skill overlays, managed `AGENTS.md`/`CLAUDE.md` blocks, documents a starter installs into the repository, or hook declarations. Harnesses discover skills only in their own folders, installed documents are notes, and hooks are declared by the binary's registry.
- Grouping a module's views under one sidebar heading. It comes with the compounding workspace.
- A module manifest. The folder name is the module's name. A views folder, flat or in a module, may hold an `experience.yml` that declares an experience ([[experiences|SPEC-0136]] R1); it describes that folder's views, not the module.
- Code-indexing module scripts. `.rhizome/` stays excluded from the code index.
- Migrating repository-owned files from the flat folders into modules.

## Key Entities

- **Module:** a folder `.rhizome/modules/<name>/`. The name matches `^[a-z0-9][a-z0-9-]{0,62}$` and must not equal a built-in `rzm` command.
- **Asset kind:** ontology, recipes, views, tasks, or scripts. The flat folder for recipes is `.rhizome/query-recipes`; every other kind uses the same folder name in both layouts.
- **Asset root:** one folder holding assets of one kind, either flat or in a module.
- **Qualified name:** `<module>/<name>`.

## User Stories

### US1 - Every starter is a module

- id:: ^SPEC-0130-US1
- summary:: A repository initialized with any starter has that starter's ontology, recipes, views, tasks, and scripts under .rhizome/modules/<starter>/, and Rhizome loads them exactly as it loaded the flat files.
- status:: draft

#### Acceptance Criteria

- Each starter (core, action-items, agentic-engineering, complex-domain, compounding) installs its ontology, recipes, views, tasks, and scripts into `.rhizome/modules/<starter id>/<kind>/`. Skills, overlays, managed blocks, installed documents, and hook declarations stay where they are.
- Schema loading, recipe discovery, view loading and serving, task discovery from the primary ref, validation, and file watching find assets in the flat folders and in every module.
- `rzm init` refreshes and removes module files with the same managed-file rules it applies today, and the managed `.rhizome/.gitignore` allowlists `modules/`.
- Compounding's scripts and `models.json` are written once into its module instead of once per harness skill folder. Its skill keeps `SKILL.md` and `references/`, and every reference to a script names the module path.
- A hook command or setting may use `{module}`, which resolves to the declaring starter's module folder.

### US2 - Existing repositories migrate without loss

- id:: ^SPEC-0130-US2
- summary:: Running rzm init in a repository initialized before modules moves the starter files it installed into modules, keeps every edit and every team-owned file, and changes no behavior.
- status:: draft

#### Acceptance Criteria

- `rzm init` moves each managed starter file it recorded in `.rhizome/generated-files.yml` from its flat path to its module path, when the flat file is unedited.
- An edited flat copy stays where it is, the module copy of that one file is not installed, and init reports the file and how to adopt the module version. No duplicate type, recipe id, or view id results.
- Ontology identifier contracts carried by an installed flat schema survive the move.
- Files a team created in the flat folders are never touched.
- After migration, `rzm init --check` exits 0, and views, recipes, tasks, hooks, and saved view preferences behave as before.

### US3 - Ids stay stable and unambiguous

- id:: ^SPEC-0130-US3
- summary:: Existing view and recipe ids keep their names across the move, every id stays unique across the repository, and tasks inside modules get qualified ids.
- status:: draft

#### Acceptance Criteria

- A view or recipe keeps the id it declares, wherever its file lives. Validation reports a duplicate view or recipe id across the flat folders and all modules.
- A task file inside a module has the id `<module>/<file name>`; a flat task keeps its file name. Branches, worktree names, run data folders, routes, and pull-request matching handle the qualified id without colliding with a flat task.
- Ontology source names, hashes, and errors use the file's repository-relative path, so two modules may each ship a file with the same name.

### US4 - A module's recipes are commands

- id:: ^SPEC-0130-US4
- summary:: rzm <module> lists a module's commands, and rzm <module> <command> runs one of its query recipes with flags generated from the recipe's declared inputs.
- status:: draft

#### Acceptance Criteria

- `rzm <module>` lists the module's commands with their descriptions. Each query recipe in the module is a command named by its recipe id.
- `rzm <module> <recipe id> --<input> <value>` runs the recipe with its declared inputs as flags and prints the same output as `rzm query-recipe run`.
- Built-in commands always take precedence. `rzm module <name> [command]` always works, so documentation and scripts never depend on a module name staying free.
- `rzm validate` reports a module whose name equals a built-in command, or that does not match the module name pattern.

## Requirements

- MUST keep the flat folders fully supported for every asset kind.
- MUST resolve asset folders through one locator; loaders MUST NOT hard-code flat paths.
- MUST keep module files inside the repository's `.rhizome/modules/`; serving and saving views MUST stay confined to the asset root that holds the definition.
- MUST NOT put mutable script state inside a module folder, since modules are tracked. Local state stays in ignored folders such as `.rhizome/compounding/`.

## Edge Cases

- **A repository-owned flat folder named `modules`** under `.rhizome/views` is unaffected: module view folders are keyed `/module/<name>/<folder>`, which a flat folder key cannot be.
- **A module added while `rzm serve` runs** is found without a restart, because recipe and view discovery list module folders each time they load.
- **Two modules each ship `x.graphql`:** both load, named by their repository-relative paths.

## Decisions

- **Ids (2026-11-01):** existing view and recipe ids keep their names because view preferences are keyed by view id ([[view-preferences|SPEC-0114]]) and scripts call recipes by id. Tasks inside modules are new and take qualified ids.
- **Edited legacy copies (2026-11-01):** keep the team's edited flat copy and skip the module copy of that file, rather than install both and break on duplicates.
- **Command names (2026-11-01):** a recipe's command is its recipe id; actions add module-local names later.

## Open Questions

None.

## Documentation Plan

- Update the init template, ontology, graphql-query, validate, and CLI subsystem notes, [[scheduled-tasks|SPEC-0127]] for qualified task ids, [[init-template-architecture|SPEC-0039]] for module installation, the custom-views skill, the starters README, the compounding starter guide and skill, and `CHANGELOG.md`.

## Change record

2026-11-09: Views folders may hold an `experience.yml` ([[experiences|SPEC-0136]] R1); a module still has no manifest.
