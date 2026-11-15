---
type: ReferenceDoc
summary: "Evidence on how a repository gets Rhizome starters installed, changed, and upgraded through rzm init, the desktop setup sheet and Settings tab, and rzm update, and which of those steps an agent may run alone versus with the user."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Onboarding mechanics

This note records the mechanics a usage skill would drive to add or change starters, and draws the line between what an agent can run on its own and what needs the user. **Verified** marks facts checked against code, `--help` output, or a throwaway-repository probe; **Inference** marks reasoning. Governing specs are SPEC-0038 (`docs/specs/product/init-starter-workflow.md`), SPEC-0118 (`docs/specs/experience/desktop-repository-setup.md`), SPEC-0138 (`docs/specs/product/starter-readiness.md`), and SPEC-0139 (`docs/specs/product/repository-upgrade.md`).

## Four surfaces, one executable

**Verified.** The desktop app does not reimplement setup. It calls the repository's selected `rzm` with `init --check --json` to plan and `init --json` to apply, so the terminal, the setup sheet, the Settings tab, and an agent all share one contract and one wording source.

| Surface | Used for | Starter choices offered |
| --- | --- | --- |
| `rzm init` in a terminal, first run | Findings, then at most a workflow question, a search key, and one confirmation (`Y/n/e`) | Three workflows; `e` opens the five-section settings menu, whose Add-ons section lists optional add-ons |
| `rzm init` in a terminal, rerun | Status summary and change list, then `Apply? [Y/n/s]`; asks about edited files after `Y` | Workflow and Add-ons sections, including optional add-ons and an eject choice |
| Desktop setup sheet (unconfigured folder) | Same findings and choices, a Writes list, one Set up button | Three workflows and **default** add-ons only (Action items). Optional add-ons are deliberately not listed (`addonOptions` in `pkg/app/cli/init/first_run_json.go`) |
| Desktop Settings, repository tab | Rhizome version, Workflow, Add-ons, Agents, Search, What gets indexed; staged changes, one Apply | Every add-on, optional ones included (`allAddonOptions` in `pkg/app/cli/init/settings_json.go`) |

So a person who sets up from the app can only reach `ao-kb` or `compounding` afterwards, from Settings, the terminal, or an agent.

## `rzm init` flags that matter for starters

**Verified** from `rzm init --help` and the code.

| Flag | When | Effect |
| --- | --- | --- |
| `--workflow agentic-engineering\|domain\|none` | Any run | Chooses the workflow. Raw starter ids also parse (`core` probed; `action-items` documented). On a configured folder, switching removes the old workflow's unedited Rhizome files (skills, managed blocks, recipes, views) and keeps team docs and ontology schemas. |
| `--addons a,b` or `none` | First run only | The exact add-on list. Default add-ons not listed are recorded as disabled. Fails with `not_first_run` on a configured folder. |
| `--add-addon`, `--remove-addon` | Configured only | Change named add-ons and nothing else. Adding an ejected add-on is refused in favor of `--restore`. |
| `--agents claude,codex,cursor\|shared\|none` | Any run | Which harness folders get skills and guidance. `AGENTS.md` and `.agents/skills/` are always written. |
| `--hooks on\|off` | Any run | Writes or removes hook entries in `.claude/settings.json` and `.codex/hooks.json` for add-ons that declare hooks (compounding). |
| `--check` | Any run | Writes nothing. Text mode exits 1 when something would change. |
| `--json` | Any run | One JSON document on stdout. With `--check` it is a plan (exit 0 when a report was produced); without it, an apply. Failures are `{schema, error: {code, message}}`. |
| `--take <path>`, `--keep <path>`, `--decision-snapshot <token>` | Configured only | Resolve pending edited-file decisions. A configured plan always carries `writes.decisionSnapshot`; passing it binds the apply to the reviewed file versions and rejects a stale review with `ErrDecisionsChanged`. |
| `--accept-suggestions` | Any run | Applies suggestions such as an add-on a workflow newly activates by default. The help text says it is "for agents and scripts after the person agrees". |
| `--refresh-docs` | Configured | Offers new versions of team-owned starter docs, applying the edit check. |
| `--eject`, `--restore` | Configured | Freeze a starter's files under team ownership, or resume management. Dependency-aware. Terminal or CLI only; the app does not offer it. |
| `--search`, `--search-key-stdin`, `--skip`, `--keep-indexed`, `--include-ignored` | First run (search key, skips) | Search provider and index scope. The key is read from a pipe so it never appears in arguments. |

### Behaviors an agent must know

- **Bare `rzm init` without a terminal applies immediately.** A first run without a terminal "uses the recommendations": the Agentic Engineering workflow, Voyage AI search, detected agents. A rerun without a terminal applies the change list and keeps edited files. Exploring with plain `rzm init` is therefore a write. (**Verified**, help text and SPEC-0038 US3.)
- **A first-run apply has a network side effect.** It downloads the pinned Rhizome binary after writing files (`ensurePinnedBinary` in `pkg/app/cli/init/run.go`).
- **CLI init does not record trust.** Desktop setup does. Module actions (`rzm ao-kb check`, compounding actions) and scheduled tasks need a trusted checkout, and `rzm trust` refuses to run without an interactive terminal (`cmd/repository_trust.go`).
- **`--addons` replaces the default list.** `--workflow agentic-engineering --addons compounding` installs no `action-items` (probed). Use `--addons action-items,compounding`.
- **`--workflow none` installs no starter.** Not even `core`, so `Person` and current-user guidance are absent (probed).
- **Existing team files at scaffold paths are kept.** A pre-existing `docs/specs/README.md` or `docs/engineering/quality-gates.md` did not appear in the planned writes; an existing `AGENTS.md` gets the managed block with its other text preserved (probed; SPEC-0038 US3-AC1).
- **A same-named user skill blocks setup.** A pre-existing unmarked `.claude/skills/rhizome/` made the plan fail with `setup_failed`: "refusing to overwrite unmarked user-owned skill directory". The fix is renaming the user's skill or `--agents none`, a user decision (probed).
- **Desktop shows a non-workflow setup as "none".** `workflowChoiceFor` maps any starter set without `agentic-engineering` or `complex-domain` to the `none` choice, so a `core` or `action-items`-only repository reads as "Search and agent guidance only" in Settings (**Verified** in code; behavior on a later Settings workflow change is untested).
- **Init does not recommend from repository content.** Detection finds Markdown, code languages, agents, `.obsidian`, ignored nested repositories, and bulky content to skip. The recommended workflow is always the first menu entry. Nothing detects an AO KB (**Verified**, `pkg/app/cli/init/detect*.go`, `first_run_prompts.go`).

## After files are written: readiness

**Verified** from SPEC-0138 and `docs/efforts/2026-11-10-08-34-starter-readiness.md`. Writing a starter's files is one milestone; completing setup, indexing, and proving queries is another. The desktop runtime discovers a newly installed starter (from Settings or from the CLI while the app is open, and on reopen), runs safe unambiguous setup, and keeps a persistent notice until it is ready or blocked with a recovery action. Only `ao-kb` declares a `lifecycle.yml` today; other starters need only ordinary indexing. Non-interactive `init` itself never runs project compilers or indexing.

**Inference.** A usage skill should report the same split the `ao-kb-setup` skill uses: files installed (`rzm init --check`), starter-specific setup ready, and index ready with a working typed query. "init succeeded" alone overclaims.

## Upgrades

**Verified** from SPEC-0139, `docs/reference/guides/getting-started.md`, and `rzm update --help`.

- **Pin offer.** When the app carries a newer release than the pin, the toolbar shows **Update to** *version*. It opens a review sheet planned by the new release, then advances the pin, applies the rerun, and restarts the worktree. Nothing is committed; the result lists paths to commit.
- **Update Rhizome files.** After a worktree opens, the app plans a rerun in the background and shows an offer when generated files are behind the executable that runs. Edited files alone do not trigger it.
- **`rzm update`.** When it changes the version a configured repository runs, it reruns the new executable's `init`. `--yes` applies without asking and keeps edited files.
- **Edited files.** Every non-interactive path keeps edited files and lists them. Choosing take, keep, or diff per file is a terminal `rzm init` decision, or `--take`/`--keep` with `--decision-snapshot` through JSON. `.rhizome/generated-files.yml` records hashes and declined versions and must be committed.
- **Safe upgrade recovery** (`docs/efforts/2026-11-13-15-07-safe-upgrade-recovery.md`, PR #144). Upgrades recover lost starter ownership from exact matches in bounded Git history, keep edited or unproven content pending as take, keep, or defer decisions, preserve sequential effort identifiers, and bind desktop applies to a snapshot of the reviewed versions.
- **Gap.** SPEC-0139 leaves agent-facing staleness reporting as a follow-up: runtime readiness and `rzm agent` sessions do not say generated files are behind. A usage skill has to detect "behind" itself with `rzm init --check --json` (**Inference** from the spec's Non-Goals and Open Questions).

## What an agent may run, and what needs the user

**Inference**, built from the verified behaviors above and the rules in the shipped skills (`ao-kb-setup`, `compounding`, `action-items`, core `rhizome` `references/installation-and-integration.md`, which already says "Initialize only with consent").

| Without asking (read-only) | After the person agrees to a reviewed plan | Only the person |
| --- | --- | --- |
| `rzm --version`, `rzm init --help` | `rzm init --json <options>` or `rzm init` applying a plan the person saw | `rzm trust` (needs a terminal) |
| `rzm init --check` and `rzm init --check --json <options>` on any folder | `--add-addon`, `--remove-addon`, `--workflow` changes (the last removes files) | Pasting provider keys, in the app's Provider access or a terminal prompt; never in agent arguments |
| `rzm index scope --json` | `--take`/`--keep` for named files, bound with `--decision-snapshot` | Codex project trust and `/hooks` trust for hook entries |
| `rzm agent current-user show` and `validate` | `--hooks on`, `--accept-suggestions`, `--refresh-docs`, `--eject`, `--restore` | Choosing which `Person` is "me"; a compounding spend budget; approving each compounding proposal |
| `rzm ao-kb check` (read-only, needs trust) or `node .rhizome/modules/ao-kb/scripts/check.mjs` | `rzm index`, `rzm ao-kb refresh-bindings`, adding note includes | Retiring or renaming colliding local schema types; choosing the AO KB product |
| Compounding `lookup.mjs`, and `mine.mjs` without `--budget` (spends nothing) | `rzm agent current-user set` after the person names the `Person`; creating that note | Renaming or removing a user skill that collides with a Rhizome skill |
| Reading `.rhizome/workflows.yml`, `.rhizome/config.yml`, templates | `rzm update --yes` | Committing, pushing, and the administrator prompt for installing the command-line tool |

### In the desktop app's chat versus a standalone repository

**Inference.** An agent in the app's chat has no terminal, so every interactive branch is unavailable: plain `rzm init` applies without asking, and `rzm trust` cannot run. The app already trusted any worktree it opened, so module actions work there. For workflow and add-on changes the skill can either point the person to Settings, where Apply shows the same file list, or show the `--check --json` plan itself and apply with the same options and `--decision-snapshot`. Standalone in a terminal-backed harness, the same JSON flow works; the person may also run the interactive `rzm init` to choose per file, and must run `rzm trust` themselves when the repository was set up from the CLI.

## Open questions for the council

- Should the usage skill drive changes through the JSON contract or hand the person to Settings when it runs inside the app? The JSON route gives exact file lists in chat; Settings gives Undo and the canonical review UI.
- Should first-run setup from the skill ever install an optional add-on, given the setup sheet intentionally hides them?
- Should the skill own the "files installed / setup ready / index ready" report for every starter, or only for starters with a lifecycle?
