---
type: ReferenceDoc
reference-kind: guide
summary: "Install Rhizome Desktop, set up a repository or join one your team already uses, connect a coding agent, find your way around the app, and configure optional providers."
last-verified: 2026-11-13
status: active
---

# Getting started with Rhizome

[Back to the README](../../../README.md) · [Next: using the skills](using-skills.md)

Rhizome Desktop sets up your repository, runs Rhizome for it, and gives you a workspace for the project's notes, views, scheduled tasks, and agent chat. Your coding agent works in the same repository through the skills that setup writes and the `rzm` command the app provides.

## Before you begin

- A Mac with Apple silicon, or Ubuntu 24.04 LTS with GNOME on x64 or Arm64.
- A Git repository. Rhizome also works in a plain folder of Markdown.
- Claude Code or Codex, installed and signed in on your computer. Cursor can use the installed skills too.
- Node.js, available to your agent as `node`. Agents use it to run Rhizome scripts.

You don't need any API keys to begin; semantic search is [optional](#optional-providers). Obsidian is optional too. Project documents are ordinary Markdown files that you can read in the app, your editor, or on GitHub.

For Intel Macs, Windows, or a server, see [Install without the desktop app](command-line.md#install-without-the-desktop-app).

## Install Rhizome Desktop

### macOS

With Homebrew:

```bash
brew install --cask atomicobject/tap/rhizome-desktop
```

Then open Rhizome from Applications. The cask also links the app's `rzm` into your PATH. If you previously installed the CLI-only formula, remove it first with `brew uninstall --formula atomicobject/tap/rhizome`.

Without Homebrew, download [Rhizome-macos-arm64.dmg](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-macos-arm64.dmg), drag Rhizome to Applications, and open it. Then choose **Settings…** from the Rhizome menu (⌘,) and, under **Command-line tool**, choose **Install Command-Line Tool**. That links `/usr/local/bin/rzm` to the app's own `rzm`. macOS asks for an administrator password when it needs one. The link stops working if you delete the app.

A coding agent can do all of this except the password prompt by following the [agent install steps](../../../README.md#install-with-a-coding-agent).

### Linux

Download the AppImage for your machine: [x64](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-linux-x64.AppImage) or [Arm64](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-linux-arm64.AppImage). Before the first launch, move it to a permanent location that you own and make it executable. Leave it there, because the command-line tool and in-app updates use that path.

```bash
mkdir -p ~/Applications
mv ~/Downloads/Rhizome-linux-x64.AppImage ~/Applications/Rhizome.AppImage
chmod +x ~/Applications/Rhizome.AppImage
~/Applications/Rhizome.AppImage
```

The first launch adds Rhizome to your applications menu. Then open **Settings** and, under **Command-line tool**, choose **Install Command-Line Tool**. On Linux it installs `~/.local/bin/rzm`. If your shell doesn't find `rzm` afterward, add `~/.local/bin` to your PATH or log in again.

<details>
<summary>Linux troubleshooting and removal</summary>

- Ubuntu 24.04 needs `libfuse2t64` to run AppImages. Follow the [AppImage FUSE guidance](https://docs.appimage.org/user-guide/troubleshooting/fuse.html) if launch reports a missing FUSE library.
- Signing in with an Atomic Object account needs a running Secret Service keyring, such as GNOME Keyring.
- To remove Rhizome, choose **Remove Desktop Integration** in **Settings** before deleting the AppImage. It removes the command-line entry and the applications-menu launcher it created.

</details>

## Set up a new repository

Choose **Add Repository…** and pick the repository's root folder. Adding a repository only inspects it. A folder that Rhizome hasn't set up shows **Set up Rhizome in** *name*, and nothing is written until you confirm.

| Row | What it shows and lets you change |
| --- | --- |
| **Found** | How many notes and which code languages Rhizome found. Rhizome indexes all Markdown, so docs you add later are indexed too. **Skip** suggests bulky checked-in content to leave out, such as vendored code or minified bundles. **What gets indexed…** opens the full list of rules, including the ones inherited from `.gitignore`. |
| **Workflow** | **Agentic Engineering** (recommended), **Agentic Engineering with domain modeling**, or **Search and agent guidance only**. [Choosing a starter](choosing-your-starter.md) compares them. Below the choices, check the add-ons you want, such as **Action items**. |
| **Agents** | **Claude Code**, **Codex**, and **Cursor**. Rhizome preselects the agents it finds in the repository or on your computer. `AGENTS.md` and the shared skills in `.agents/` are always written, and most agents read them. |
| **Search** | The semantic search provider: **Voyage AI** (recommended), **OpenAI**, **Ollama**, or **Off**. When a key is needed, paste it here. It is saved in your user configuration, never in the repository. Leave it empty to set up without semantic search and add a key later. Keyword search works either way. |
| **Writes** | Every file setup will create or change. |

Atomic Object employees can choose **Sign in with Atomic Object** under **Team account**, or import keys from the team's 1Password Environment, before setting up.

Choose **Set up Rhizome**. Setup pins this Rhizome release in the repository, so every teammate runs the same version, and records that you trust this worktree on this computer. When it finishes, the sheet lists what it did and the files to commit. Choose **Open workspace**. Rhizome starts and builds the search index in the background.

Then [check that it works](#check-that-it-works).

### What setup installs

| Location | What your team uses it for |
| --- | --- |
| `docs/engineering/` | Team policy, test commands, review expectations, and release steps |
| `docs/specs/` | Intended behavior and constraints |
| `docs/efforts/` | Scoped delivery work, plans, and evidence |
| `docs/meetings/` and `docs/reference/` | Source material, decisions, and project context |
| `.agents/skills/` | Shared agent workflow instructions, plus a `.claude/skills/` mirror for Claude Code |
| `AGENTS.md` and, for Claude Code, `CLAUDE.md` | Guidance that routes agents into those skills |
| `.rhizome/` | Shared configuration, schema, saved queries, views, and the Delivery experience, plus ignored local caches |

The Agentic Engineering workflow also includes the base identity and action-item starters.

So that sandboxed agents can reach Rhizome on `127.0.0.1` while staying sandboxed, setup adds `sandbox.network.allowLocalBinding` to `.claude/settings.json` for Claude Code on macOS, and a loopback-only `rhizome` permission profile to `.codex/config.toml`. Codex loads that profile only in projects you have trusted in Codex.

### Share the setup

Review and commit the generated skills, guidance, engineering documents, and tracked `.rhizome/` files. That includes `.rhizome/generated-files.yml`, which records what Rhizome wrote so later updates can tell which files someone edited. Rhizome's generated ignore rules keep its downloads, indexes, and local sessions out of Git. Don't commit provider keys or personal settings.

Before delegating much work, fill in your actual commands in `docs/engineering/quality-gates.md` and your review expectations in `review-and-approval.md`. [Adapting the process](adapting-rhizome.md#start-with-team-policy)

## Join a project your team already uses

If the repository already has `.rhizome/config.yml`, its skills, and its guidance, you don't set it up again.

1. Clone the repository and install Rhizome Desktop.
2. Choose **Add Repository…** and pick your clone.
3. The app asks **Trust this worktree?**, because opening the repository runs the Rhizome version it pins. A worktree is your checkout of the repository. If you don't know where the checkout came from, review it first. Then choose **Trust and open**. Trust is recorded on this computer, and other Git worktrees of this checkout share it.
4. Rhizome downloads the pinned version if you don't have it, starts it, and builds your index.
5. If the team uses semantic search, add a key under [Optional providers](#optional-providers).

Then [check that it works](#check-that-it-works).

## Check that it works

Close any agent session that was already open in the repository and start a new one, so the agent discovers the installed skills. Then ask:

> Use the rhizome skill to orient me to this repository. Show me the available workflow and any setup gaps before we start work.

The agent should describe the installed workflow without errors about `rzm`. You can ask the same question in the app's **Agent** tab instead: choose the **Harness** and **Model**, and Rhizome runs that agent in the repository.

In the app, the **Notes** sidebar should list your notes and, for the Agentic Engineering workflow, the **Delivery** experience.

If the agent can't run `rzm`, read the next section.

## Make Rhizome available to your agent

Your coding agent reaches Rhizome by running `rzm`, the same program the app runs. The skills and hooks that setup installs call it by name, so it must be on the PATH of every agent you use.

Run `rzm --version` in a new terminal. If it prints a version, the command is installed. If not, open **Settings** and look under **Command-line tool**, which reports the `rzm` your login shell runs:

- **Installed from the DMG or AppImage:** choose **Install Command-Line Tool**, or **Repair Command-Line Tool** if the link is broken.
- **Installed with the Homebrew cask:** Homebrew links `rzm` for you. The section may still say "Not installed" and offer **Install Command-Line Tool**, because it checks only for the app's own link. You can ignore it.
- **"Another rzm is installed here":** a different Rhizome installation, such as one from the installer script, already owns `/usr/local/bin/rzm`, and Rhizome leaves it alone. Remove that installation and choose **Install Command-Line Tool**. Or keep it, and select it under **Use an existing executable** so the app and your agents run the same `rzm`.

If `rzm` works in a terminal but your agent still can't find `rzm` or `node`, fix the PATH that agent inherits and restart it. An agent started from a terminal inherits that terminal's PATH. An editor or desktop agent may need restarting after you install the command-line tool.

**Codex users:** trust the project in Codex. Sandboxed Codex sessions can reach Rhizome only through the permission profile in `.codex/config.toml`, which Codex loads only for trusted projects. [What setup installs](#what-setup-installs)

## Find your way around

Each window shows one repository. The toolbar names the repository and the current worktree; click either to switch. ⌘O switches repositories and ⇧⌘O switches worktrees (use Ctrl on Linux). Rhizome lists every Git worktree of a repository together and runs each one separately.

The toolbar's tabs are:

| Tab | What it's for |
| --- | --- |
| **Notes** | Read and edit the project's Markdown, browse views, and open experiences such as Delivery |
| **Ontology** | The types and relationships your project's notes use |
| **Explorer** | A graph of how notes, files, and code link together |
| **Agent** | Chat with Claude Code or Codex in this repository |
| **Tasks** | Scheduled agent work and the inbox of items that need you |
| **GraphQL** | Ask structured questions of the project's notes |

Press ⌘K to search the project, ⌘P to open a note or file, and ? to see every shortcut.

**Delivery.** The Agentic Engineering workflow adds a **Delivery** experience to the **Notes** sidebar: a set of views that maps specs, stories, and efforts from shaping through building to settled work. Its pages show where each piece of work stands, and **Copy workflow prompt** copies the prompt that continues it in your agent. Delivery never starts an agent itself.

**Editing.** Choose **Edit** above a note to start an editing session. Rhizome tracks your changes across notes until you choose **Save** or **Discard**, and **Review** shows the diff first.

**Tasks.** A repository can declare scheduled agent work that runs on each teammate's computer in their own Claude Code or Codex. The **Tasks** tab shows what needs you, the Dock badge counts it, and a notification arrives when a run is ready for review. [Scheduled tasks](scheduled-tasks-guide.md)

## Optional providers

Rhizome's workflow skills use your installed coding agent and need no separate key. Two features use external providers:

| Feature | Provider |
| --- | --- |
| Semantic search | A Voyage AI key (recommended), an OpenAI key, or a local Ollama embedding model |
| Content evaluation, and session screening for the compounding add-on | Jev, TypeSafe's evaluation model, with a TypeSafe key |

Without embeddings, keyword search, code navigation, and structured queries still work.

Manage both in **Settings › Rhizome on this Mac › Provider access**:

- **Keys on this Mac.** Add, replace, or clear your own Voyage and TypeSafe keys. Saved keys are never displayed again.
- **Import from 1Password.** Enter your team's 1Password account and Environment ID, choose the keys to import, and choose **Import selected keys**. This needs the 1Password CLI installed and signed in. The keys are saved on your computer, and ordinary use needs no 1Password prompt. Reimport after a key rotation.
- **Atomic Object server.** Atomic Object employees can choose **Sign in with Atomic Object** to use the team's gateway. By default Rhizome uses your own key when you have one and the server otherwise. To choose explicitly, set **Source for new connections** to **Atomic Object server** or **Your key**, separately for Voyage and Jev.

Voyage keeps its connection until the repository restarts, so after changing its source, choose **Restart** for that repository under **Running repositories**. Jev checks its source on each evaluation.

To change a repository's search provider, use its Settings tab, described next.

## Change a repository's setup later

Open **Settings** and choose the repository's tab, or right-click the repository name in the toolbar and choose **Repository Settings…**. The tab shows:

- **Rhizome version.** **Pinned version** is what everyone on the team runs. **The app's Rhizome** or **Another executable…** changes only this computer and leaves the pin alone.
- **Workflow**, **Add-ons**, **Agents**, and **Search**, with the current choices. Every add-on is listed here, including optional ones such as **Compounding** that first-time setup doesn't offer. Change any of them, review the files the change writes, and choose **Apply changes**. Each changed row has **Undo** until you apply.
- **What gets indexed**, with **Edit rules…** to change it.

Commit the files the tab lists after applying, so your team gets the same change.

## Keep Rhizome up to date

- **The app.** Rhizome checks for updates at launch and once a day; choose **Check for Updates…** from the Rhizome menu to check now. The app and its bundled `rzm` always update together. Running `rzm update` in a terminal hands the update to the app.
- **A repository's pinned version.** When the repository pins an older Rhizome than the app carries, the toolbar shows **Update to** *version*. Choosing it installs that version, updates the pin in `.rhizome/config.yml`, refreshes Rhizome's files in the repository, and restarts the worktree. The sheet then lists the files to commit for your team. **Not Now** hides the offer until the next app release. If the repository pins a newer version than the app, the toolbar shows **Needs** *version*, and its menu offers **Check for App Updates**.
- **Rhizome's files.** When a newer Rhizome ships updated skills or guidance for your workflow, the toolbar offers **Update Rhizome files**. Rhizome updates files that nobody edited and keeps the ones your team changed. [Update ownership](adapting-rhizome.md#keep-updates-under-control)

[Next: use the skills on real work](using-skills.md)
