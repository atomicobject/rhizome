---
type: ExperienceSpec
id: SPEC-0118
summary: "Rhizome Desktop sets up an unconfigured worktree from one review sheet backed by rzm init's machine-readable report, and gives every repository a page for administering what gets indexed, including rules inherited from .gitignore."
spec-status: proposed
last-updated: 2026-10-06
aliases:
  - SPEC-0118
---

# Desktop repository setup

## Summary

Opening a folder that has no Rhizome configuration in [[rhizome-desktop|Rhizome Desktop]] today shows one button that runs `rzm init` with every default, and that button reports only an exit status when something fails. This spec replaces it with a setup sheet that does in the app what the first run of [[init-starter-workflow|rzm init]] does in a terminal. The sheet shows what Rhizome found and preselects the recommended choices: a workflow and its add-ons, the agents to set up, a semantic search provider with an optional key, and ignored nested repositories to include. It lists what setup will write. One button writes everything, trusts the worktree, and shows the summary the terminal prints.

The same app also gets a "What gets indexed" page. It shows every rule that decides which paths Rhizome indexes: the rules in `.rhizome/ignore`, the rules inherited from `.gitignore` files, the built-in list, and configuration excludes. It lets the user add and remove Rhizome's own rules. The setup sheet opens the page before anything is written, and a repository's options open it at any time afterward.

The app does not reimplement setup. Each step runs the Rhizome executable that owns the folder: the executable that would set up the folder before setup, and the worktree's selected executable afterward. That executable reports its findings and choices as JSON and applies the choices passed as options in one run. Init wording, starter metadata, and the ignore-file format therefore stay in one place, and the app shows exactly what that version would do.

## Goals

- Set up a repository from the app with one reviewed confirmation, making the same choices the terminal first run offers, plus turning off a workflow's default add-on.
- Show the findings, recommendations, and planned writes before anything is written.
- Accept a semantic search key in the app without exposing it in process arguments, logs, or the repository.
- Explain what Rhizome indexes, including what it inherits from `.gitignore`, and let the user change Rhizome's own rules before and after setup.
- Give scripts and agents the same machine-readable setup and index-scope contracts the app uses.

## Non-Goals

- Rerun maintenance from the app: refreshing generated files, reviewing edited files, ejecting or restoring workflows, and changing an existing setup's workflow, agents, or search provider. Reruns stay in `rzm init` in a terminal.
- Editing `.gitignore` files, `notes.includes`, `notes.excludes`, `code.disabledLanguages`, or code folder limits. The page shows configuration excludes but does not edit them.
- Removing built-in rules before setup. They become ordinary `.rhizome/ignore` lines at setup and can be removed afterward.
- A path tester that explains why one path is indexed or skipped. `rzm index --explain` remains the tool for that.
- Choosing an external binary manager in the app. `rzm init --binary-manager external` remains a terminal option.
- Streaming progress from inside `rzm init`. Setup shows one progress state with elapsed time.
- Setup for an executable older than the release that ships these contracts. Like [[rhizome-desktop|SPEC-0113]], the app carries no compatibility behavior for older Rhizome versions. It names the missing capability and offers Manage installation.
- Windows and Linux desktop builds.

## User Stories

### US1 - Set up an unconfigured worktree from one review sheet

- id:: ^SPEC-0118-US1
- summary:: A person who opens an unconfigured folder in Rhizome Desktop reviews what Rhizome found, adjusts the preselected choices, and sets up the worktree with one button, then sees what was written.
- status:: draft

#### Acceptance Criteria

- An unconfigured worktree shows the setup sheet instead of the current setup panel. Opening the sheet writes nothing. When the executable that would set up the folder is repository-selected, as in a Rhizome source checkout, the existing trust step comes first, and the sheet follows it.
- The sheet names the folder and path and reports the docs, code, and suggested skips in the plain words `rzm init` prints, without config keys or glob syntax.
- Workflow choices, their descriptions, and the recommended choice come from the executable's report. The default add-on for each workflow (today, Action items) appears as a checkbox. It is checked when the chosen workflow activates it by default, and it can be turned on when the workflow does not include it.
- Agents appear as checkboxes for Claude Code, Codex, and Cursor. Each is preselected the way the terminal infers it and shows why: a repository marker or an installed command. The sheet says that AGENTS.md and shared skills are always written.
- Search offers Voyage AI, OpenAI, Ollama, and Off. It preselects the recommended provider and shows each provider's readiness as the app's environment and the global Rhizome configuration see it. That is what the runtime the app starts will see. When the chosen provider needs a key, a password field accepts it. When the executable bundles Atomic Object team keys, the field also accepts the Atomic Object Rhizome key, as the terminal prompt does. Leaving the field empty sets up without semantic search and says how to add a key later.
- Each ignored nested Git repository that init would ask about appears as an unchecked option to include it.
- A Writes row summarizes the files setup will create for the current choices and can expand to list all of them. It refreshes when a choice changes and shows that it is refreshing.
- The Skip row opens the What gets indexed page (US2) for this worktree. Edits made there before setup are part of the same Set up.
- Set up states that it pins the version of the executable that runs it and trusts this worktree. A typed key is sent only when Set up runs, never while the sheet refreshes its report. While setup runs, the content area shows progress with elapsed time, and other actions for this worktree are disabled.
- On success, the sheet is replaced by the summary `rzm init` prints: grouped writes, a saved key, a search hint when one applies, and the paths to commit. Open workspace continues through the normal open steps without a trust prompt, and the runtime builds the index. Reveal in Finder is also offered.
- When the files were written but the pinned executable could not be installed, the summary shows the written files and the installation error. The worktree is still trusted, and opening follows the existing install-error path.
- When setup fails before writing configuration, the sheet stays filled in and shows the executable's error message, not only an exit status, with Try again. A key saved before the failure stays saved, and the sheet says so.
- When the executable lacks these contracts, the sheet says Rhizome needs an update to set up from the app, offers Manage installation, and mentions `rzm init` in a terminal.
- A newer selection in the window discards the sheet. Switching back reloads the report, and edits that were never applied are discarded.

### US2 - See and change what gets indexed

- id:: ^SPEC-0118-US2
- summary:: A person sees every rule that decides which paths Rhizome indexes in a worktree, including the rules inherited from .gitignore, and adds or removes Rhizome's own rules before or after setup.
- status:: draft

#### Acceptance Criteria

- The page is opened from the setup sheet's Skip row and from the repository's options menu ("What Gets Indexed…"). From the menu, it acts on the worktree the window shows, or on the primary worktree when the window shows none.
- The page lists rules in evaluation order, grouped by layer. Each rule shows its source file and line where it has one:
  - **Rhizome rules** from `.rhizome/ignore`. Suggested skips show their reason; included folders are labeled as included.
  - **Inherited from .gitignore**: rules from the root and nested `.gitignore` files that Rhizome reads. A nested file's rules are labeled with the folder they apply to. These rules are read-only.
  - **Built-in list**: shown only while it applies, which is when a configured worktree's `.rhizome/ignore` has no rules.
  - **Configuration excludes** from `notes.excludes`, read-only, naming `.rhizome/config.yml`.
- The page states that hidden files and folders are never indexed and that the last matching rule wins.
- Before setup, the page shows the Rhizome rules setup will write as planned: the built-in list and the suggested skips for the sheet's current choices. The user can skip a folder or file by path, decline a suggested skip, and include a folder that `.gitignore` excludes. Planned built-in rules are read-only. Edits are held and become part of Set up. Leaving setup without applying discards them.
- After setup, the user can skip a folder or file by path, remove any rule from `.rhizome/ignore`, and include a folder that `.gitignore` excludes. Removing a suggested skip records that the path stays indexed, so init does not propose it again. Each edit is applied immediately by the worktree's selected executable, and the page reloads its rules from that executable. The running runtime picks up `.rhizome/ignore` changes as it already does. Opening the page for a configured worktree requires trust, like opening the worktree.
- Edit errors, such as a path that does not exist or a rule that is no longer present, are shown on the page and leave the file unchanged.

## Requirements

### Executable contracts

- Every `--json` document MUST carry a schema version and use one consistent key style. Display text, such as findings and summary lines, MUST come from the same code that produces the terminal's text.
- `rzm init --check --json` MUST write one JSON document to standard output and nothing else, and exit 0 when it produced a report. It MUST NOT prompt or write files. For an unconfigured folder, the document MUST include:
  - the project root, its name, and `configured: false`
  - the docs, code, and skip findings as display text
  - the workflow choices with labels, descriptions, and the recommended choice
  - the add-on starters with labels, descriptions, and which workflows activate each by default
  - for each of Claude Code, Codex, and Cursor: whether it would be enabled and why (repository marker or installed command)
  - each search provider's label and readiness, the name of the key it needs, whether team keys are offered, and the recommended provider
  - ignored nested repository candidates
  - the version setup would pin
  - the scope report described below, with the Rhizome rules setup would write marked as planned
  - the files and settings that would be written for the choices passed as options
- The check report MUST reflect every choice option it is given, including skip edits and included folders, so the planned rules and file list match what the same options would apply.
- `rzm init --json` without `--check` MUST apply the choices passed as options without prompting. It MUST report as JSON what was created, updated, and kept, plus the summary lines the terminal prints, the paths to commit, any saved key's label (never its value), the search hint, warnings, and the pinned-executable result. When files were written but installing the pinned executable failed, the report MUST include both, and the command MUST exit non-zero.
- On a configured folder, `rzm init --json`, with or without `--check`, MUST fail with a JSON error stating that machine-readable output covers first-run setup. Rerun reporting is out of scope.
- Every `--json` failure MUST be a JSON object with a stable `code`, a human-readable `message`, and any saved key's label, written to standard output, with a non-zero exit.
- `rzm init --addons <ids|none>` MUST choose add-on starters explicitly. Listed add-ons install. Default add-ons of the chosen workflow that are not listed are recorded as disabled in `.rhizome/workflows.yml`. Unknown ids and required dependencies MUST be rejected before any write.
- `rzm init --skip <path>` and `rzm init --keep-indexed <path>`, both repeatable, MUST add a path to the suggested skips and decline a suggested skip, in the same write as the rest of setup, with the reasons and markers the settings menu writes.
- `rzm init --search-key-stdin` MUST read one key from standard input for the chosen provider and save it exactly as the terminal prompt does, including recognizing the Atomic Object Rhizome key. It MUST be rejected with `--check`, because saving a key is a write. The key MUST NOT appear in output, logs, diagnostics, or repository files.
- `rzm index scope --json` MUST report, for a configured folder, the rules in the four layers above, in evaluation order. Each rule includes its source, line, and pattern, and each `.rhizome/ignore` rule includes its suggested-skip reason or included-folder marker. The report MUST also say whether the built-in list applies and list the paths marked to stay indexed. It MUST use the same scope shape that the init check report embeds. On an unconfigured folder, it MUST fail with a `not_configured` error.
- `rzm index scope` MUST accept repeatable `--skip <path>`, `--remove-rule <rule>`, and `--include-ignored <path>` edits. `--remove-rule` names a rule by its exact pattern text as listed and removes every line with that text. A removed suggested skip also loses its reason line and gains a keep-indexed marker. A rule that is not present is an error. Edits MUST use init's existing file sections and markers, preserve unrelated lines and comments, and apply all together or not at all. Without `--json`, the command MUST print a readable listing.

### Desktop

- The desktop app MUST obtain setup reports and apply setup through the executable selection `Initialize` uses today for an unconfigured folder. It MUST obtain and edit index scope through the worktree's selected executable once the worktree is configured. Whenever that selection is repository-selected, trust MUST precede the first report.
- Before using these contracts, the app MUST confirm that the selected executable supports them, such as by probing `--help`. When it does not, the app MUST report a distinct `setup_unsupported` problem rather than a generic failure.
- A key MUST travel from the shell to the executable only through command payloads and standard input. It MUST NOT be stored by the app, placed in process arguments or environment variables, or logged by the shell, native layer, or bridge.
- Setup MUST record trust for the worktree's canonical checkout, through the same trust store as the Trust action, whenever the folder's configuration exists after the setup run, even if a later step failed. It MUST NOT record trust when no configuration was written.
- Setup and index-scope edits for one worktree MUST be serialized with that worktree's other open, trust, and setup work, as `Initialize` is today.
- Only shell webviews MAY invoke these operations. Repository content webviews remain without native command permissions.

### Verification

- Go tests MUST cover the JSON check report and apply result for a first run, including planned rules that follow `--include-ignored`, `--skip`, and `--keep-indexed`. They MUST also cover `--addons` (including turning off a default add-on and rejecting a required dependency), key input from standard input (saved, never echoed, rejected with `--check`, and team-key recognition), the configured-folder JSON errors, `index scope` reporting across all four layers, and `index scope` edits, including removing a suggested skip and all-or-nothing failure.
- Desktop tests MUST cover the bridge setup and scope operations against fixture executables, the `setup_unsupported` problem, trust recorded exactly when configuration was written, and shell behavior for choices, held edits passed to Set up, the summary, and errors.
- Native verification MUST exercise the packaged app on an unconfigured fixture repository with an ignored nested repository: set it up with non-default choices and held edits, then add and remove rules after setup. It MUST confirm that the written files and `.rhizome/ignore` match a terminal run of `rzm init` with the same options, and that the workspace opens and indexes without a trust prompt.

## Open questions

None. The shape, starter choice, in-app key, trust on setup, and the page's availability before and after setup were settled with Drew on 2026-10-06.

## Documentation plan

- Add a requirement to [[rhizome-desktop|SPEC-0113]] that points unconfigured-worktree setup to this spec. List `--json`, `--addons`, `--skip`, `--keep-indexed`, and `--search-key-stdin` under [[init-starter-workflow|SPEC-0038]]'s non-interactive options. Both specs are frozen by active efforts, so each edit records a deviation on the effort that froze it.
- Update `desktop/README.md` and `pkg/app/desktop/CONTEXT.md` for the setup sheet, the What gets indexed page, trust on setup, and the new bridge operations.
- Update `pkg/app/cli/init/CONTEXT.md` for the JSON contract, add-on choice, skip options, and key input, and update the rhizome skill's `index-scope.md` template for `rzm index scope`.
