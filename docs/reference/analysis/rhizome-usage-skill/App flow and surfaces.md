---
type: ReferenceDoc
summary: "How a person meets Rhizome Desktop and the web UI end to end, from install through setup, the sections, and Settings, with the stuck points a usage skill must explain."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# App flow and surfaces

This note records what Rhizome Desktop and the web UI show a person today, step by step, so a council can decide what a usage skill must say about the app. Each section separates what was verified in code or specs from inference. The companion note `docs/reference/analysis/rhizome-usage-skill/In-app chat agent environment.md` covers the chat agent's own environment.

Sources read: `docs/reference/guides/getting-started.md` (desktop-first, last verified 2026-11-13), `docs/reference/guides/choosing-your-starter.md`, `desktop/README.md`, `desktop/CONTEXT.md`, `web/CONTEXT.md`, `desktop/src/SetupSheet.tsx`, `desktop/src/Status.tsx`, `desktop/src/Settings.tsx`, `desktop/src/RepositorySettings.tsx`, `web/src/components/desktopHost.ts`, `web/src/components/NotesLeftRail.tsx`, `pkg/app/cli/init/first_run_prompts.go`, `cmd/init.go`, the starter templates under `pkg/app/cli/init/templates/starters/`, and the efforts for repository settings, experiences, the Delivery experience, starter readiness, and in-app chat.

## 1. Install

**Verified.** Rhizome Desktop ships for Apple-silicon Macs (Homebrew cask or DMG) and Linux AppImages on x64 and Arm64 (`docs/reference/guides/getting-started.md`). The cask links the app's bundled `rzm` onto PATH. The DMG and AppImage do not: the person must choose **Install Command-Line Tool** under Settings › Command-line tool (`desktop/src/Settings.tsx`, heading "Command-line tool"). Intel Macs, Windows, and servers use the CLI-only install (`docs/reference/guides/command-line.md`). Prerequisites are a Git repository or Markdown folder, Claude Code or Codex installed and signed in, and `node` on the agent's PATH.

**What the person is trying to do.** Get a working app and an agent that can call `rzm`.

**Where people get stuck.**
- `rzm` missing from the agent's PATH is the documented failure ("If the agent can't run `rzm`"). The Settings section reports only the app's own link, so a Homebrew user can see "Not installed" while `rzm` works, and a different `rzm` at `/usr/local/bin` is left alone ("Another rzm is installed here").
- Codex reaches the loopback runtime only through the `rhizome` permission profile in `.codex/config.toml`, which Codex loads only for projects the person has trusted in Codex.

**What a usage skill needs.** A short diagnosis path: check `rzm --version`, and if the app is installed but `rzm` is absent, name the exact Settings control for this install type. It should never tell a person to add sandbox escapes; `docs/reference/guides/getting-started.md` describes the supported loopback grants instead.

## 2. Add a repository, trust, and run

**Verified.** Each window shows one repository. **Add Repository…** only inspects a folder. An unconfigured folder shows the setup sheet; a configured one asks **Trust this worktree?** because opening it runs the executable the worktree pins and may download that version (`desktop/src/Status.tsx`). Trust is per worktree on this computer, and Git worktrees of the same checkout share it. The runtime then starts and indexes in the background. Other status screens are a progress panel with elapsed time, "Rhizome is sleeping" with **Start Rhizome**, reconnecting, and errors that lead with **Update to Rhizome** *version* when the pin cannot install or run. The toolbar names the repository and worktree; ⌘O and ⇧⌘O switch them (`desktop/README.md`).

**Where people get stuck.**
- Trust is a security decision the person must make. The prompt explains why, but someone who just cloned a team repository may not know that opening it runs pinned code.
- A pin older than the app shows **Update to** *version*; a newer pin shows **Needs** *version*. Both change files the team must commit.

**What a usage skill needs.** Explain trust and pins in one or two sentences each, and point to the toolbar offers rather than reinventing upgrade steps. An agent should not run `rzm trust` on the person's behalf without asking. That is an inference from the trust model, not a written rule I found.

## 3. The setup sheet and the workflow choice

**Verified** (`desktop/src/SetupSheet.tsx`, `pkg/app/cli/init/first_run_prompts.go`). The sheet is titled **Set up Rhizome in** *name* and has these rows:
- **Found**, with note and language counts and **Skip** suggestions for bulky content.
- **Workflow**, with three choices: **Agentic Engineering** (recommended), **Agentic Engineering with domain modeling**, and **Search and agent guidance only**. These map to `--workflow agentic-engineering`, `domain`, and `none`.
- Add-ons, shown under the workflow choices.
- **Agents**, with Claude Code, Codex, and Cursor preselected by detection.
- **Search**, with Voyage, OpenAI, Ollama, or Off, plus a key field.
- **Ignored repos**, listing nested repositories.
- **Writes**, listing every file setup will touch.

Nothing is written until the person confirms. The finished sheet is titled **Rhizome is set up in** *name* and lists the files to commit.

**AO KB is not a workflow choice.** `pkg/app/cli/init/templates/starters/ao-kb/template.yaml` declares `optionalAddon: true` and `requires: core`. The guide says first-time setup does not offer optional add-ons such as Compounding; they appear in the repository's Settings tab under **Add-ons** (`docs/reference/guides/getting-started.md`, `docs/efforts/2026-11-09-08-24-desktop-repository-settings.md`). `rzm init --add-addon <id>` adds one from a terminal (`cmd/init.go`). The AO KB managed block routes setup to an `ao-kb-setup` skill, and module actions need a trusted repository (`pkg/app/cli/init/templates/starters/ao-kb/agents/AGENTS.md`). `docs/reference/guides/choosing-your-starter.md` (last verified 2026-10-01) does not list AO KB at all.

**What the person is trying to do.** Pick the right process for this repository without understanding starters, ontologies, or modules.

**Where people get stuck.**
- Three workflow labels with no AO KB option. A person who wants the AO KB starter cannot find it at setup and must know to look in Settings › Add-ons afterward.
- The setup sheet shows the add-ons that the chosen workflow includes, but optional add-ons appear only later.
- After setup the team must fill in `docs/engineering/quality-gates.md` and `review-and-approval.md` before delegating much work. The guide says so, but the app does not prompt for it. I found no such prompt in `desktop/src`.

**What a usage skill needs.** A starter recommendation flow that ends in a concrete action the person can take in the app (the Settings row and **Apply changes**) or a command (`rzm init --check --json` to preview and `rzm init --json --add-addon …` or `--workflow …` to apply). Rerun-on-configured-folder JSON is the same path the Settings tab uses (`docs/efforts/2026-11-09-08-24-desktop-repository-settings.md`). `cmd/init.go` also says that without a terminal, init "uses the recommendations and keeps edited files", so an agent can run it non-interactively.

## 4. Sections and navigation

**Verified.** The sections are `notes`, `ontology`, `explorer`, `agent`, `tasks`, and `graphql` (`web/src/components/desktopHost.ts`, `SECTIONS`). In Desktop the native toolbar draws the section tabs, search, issue badge, and shortcuts; the web header renders none there (`web/CONTEXT.md`, Shared chrome). The global shortcuts are ⌘K search, ⌘P note and file finder, `?` shortcuts, `]` right pane, ⌘B left sidebar, and ⌥⌘B Chat.

| Section | What it shows (verified) | Non-obvious for a newcomer |
| --- | --- | --- |
| Notes | The left rail has quick scopes (All notes, Recently changed, Problems with an issue count), Views, the type tree with display groups, experience panels, and a **Files** tree docked at the bottom (`web/src/components/NotesLeftRail.tsx`, PR #147). It has no note list; ⌘P replaces it. Notes open in retained tabs. The right pane has Info, Outline, Graph, and Chat for a note, or Record and Chat for a selected row. | A table row click selects the row into Record, and a title click opens the note. "Untyped + ambiguous" opens an `__untyped__` listing. **Edit** starts an editing session across notes that ends with **Save**, **Discard**, or **Review**. |
| Delivery (an experience inside Notes) | Installed by the Agentic Engineering workflow. It maps specs, stories, and efforts through stage tabs, and **Copy workflow prompt** copies the prompt that continues a piece of work. It never starts an agent itself (`docs/reference/guides/getting-started.md`, `docs/efforts/2026-11-09-21-28-agentic-engineering-experience.md`). | The copied prompt is meant for an agent. With in-app chat, the obvious place to paste it is the Chat pane, but no doc says so (inference). |
| Knowledge (AO KB experience) | Installed by the AO KB add-on. It is a multi-page site with a sidebar panel (`docs/efforts/2026-11-09-09-16-experiences.md`). | It exists only after the add-on is installed and set up through its readiness flow. |
| Ontology | Types and relationships (the Atlas) and per-type schema (`web/src/components/chat/viewContext.ts` names these `ontology` and `type`). | A person rarely knows what an ontology is; this is where schema evolution shows. |
| Explorer | A graph of how notes, files, and code link. | Chat context for this section is only the section name. |
| Agent | Full-page sessions with Claude Code or Codex, with Harness and Model pickers and a permission setting behind a disclosure (`web/src/components/agent/AgentSettingsPanel.tsx`). It is the expanded view of the Chat pane's sessions. | The empty-state example prompts are fixed strings, one of which ("Which notes explain how search ranks results?") reads as specific to Rhizome's own repository (`web/src/components/agent/ExamplePrompts.tsx`). |
| Tasks | Scheduled agent work and the inbox of items that need the person; Desktop badges the Dock (`docs/reference/guides/scheduled-tasks-guide.md`). | Runs use the teammate's own harness on their computer. |
| GraphQL | Structured queries over typed notes. | A power-user surface; Chat context for it is only the section name. |

**Where people get stuck (inference from the code and docs).** The app offers many surfaces but no guided tour. The user guides predate the Chat pane: `docs/reference/guides/getting-started.md` lists the Agent tab as the place to chat and does not mention the right-pane Chat, ⌥⌘B, or view context. Experiences, views, and types sit together in one sidebar, and nothing in the UI explains which came from a starter and which the team made.

**What a usage skill needs.** A compact map of the sections keyed to what the person wants to do ("see where work stands": Delivery; "find a note": ⌘P; "see the schema": Ontology; "fix validation problems": Problems). It should include the shortcuts, because the chat agent cannot press them for the person.

## 5. Settings

**Verified** (`desktop/src/Settings.tsx`, `desktop/src/RepositorySettings.tsx`, `docs/reference/guides/getting-started.md`). Settings (⌘,) has two tabs.
- **Rhizome on this Mac** has Appearance, Diagnostics, Updates, Command-line tool, Desktop integration (Linux), Developer builds, Use an existing executable, Provider access (keys on this Mac, 1Password import, source per provider), Team account, and Running repositories with Restart.
- The **repository** tab, named after the repository and its branch, has Rhizome version (pinned, the app's, or another executable), **Workflow**, **Add-ons** (every add-on, optional ones included), **Agents**, **Search**, and **What gets indexed** with **Edit rules…**. Changed rows show Undo, a pinned bar lists the files a change writes, and **Apply changes** applies them. The person then commits the listed files.

Open PR #149 (unmerged as of 2026-11-15) adds a **Chat** section under Rhizome on this Mac for choosing the chat agent and model.

**What a usage skill needs.** Name the repository tab as the place to change workflow, add-ons, agents, search, and index scope from the app. Name `rzm init --check --json` and `rzm init --json` with flags as the agent-side equivalent. Remind the person to commit what either path wrote.

## 6. Keeping current

**Verified.** The app self-updates and its bundled `rzm` updates with it. Pin offers appear in the toolbar, and **Update Rhizome files** appears when a newer release ships changed skills or guidance; it updates unedited files and keeps edited ones (`desktop/CONTEXT.md`, `docs/reference/guides/getting-started.md`). Upgrades and settings applies leave changes uncommitted and list the paths to commit.

**What a usage skill needs.** One line: Rhizome updates its own files through the toolbar offer, so an agent should not hand-edit generated skill copies (the managed-block rule in `AGENTS.md` already says this for the repository's own guidance).

## Summary of design implications

1. The app path and the agent path change the same files. A usage skill should present each capability as "in the app: this control; from the agent: this command", and say that both end in files to commit.
2. AO KB is reachable only as an add-on after setup. A skill that "recommends and installs starters" must distinguish first-run workflows from add-ons and route AO KB through Settings › Add-ons or `--add-addon`.
3. The user guides do not yet describe the Chat pane. Until they do, the usage skill is the only explanation of Chat that an in-app agent can give.
4. Section context reaching the chat is rich for Notes and Ontology and thin elsewhere, so the skill's section map is what lets the agent explain Explorer, Tasks, and GraphQL.
