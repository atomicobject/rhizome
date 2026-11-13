<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/rhizome-wordmark-reversed.svg">
    <img src="docs/brand/rhizome-wordmark.svg" alt="Rhizome" width="320">
  </picture>
</p>

# Rhizome

**Bring Atomic Object's Agentic Engineering process into your repository.**

Rhizome gives your team shared skills for defining work, planning implementation, building, and reviewing with AI agents. Specs, decisions, and delivery records live alongside your code, so the next developer or agent can pick up the context.

Setup installs the [Agentic Engineering](https://atomicobject.com/agentic-engineering) workflow by default. It gives your team defaults to adapt: how much planning a task needs, which checks prove it works, and when a person needs to make a decision. Small fixes stay small. The [process guide](https://atomicobject.com/agentic-engineering/overview) explains how the team works together.

## Get started

You'll need:

- A Mac with Apple silicon, or Ubuntu 24.04 with GNOME.
- A Git repository.
- Claude Code or Codex, installed and signed in on your computer.
- [Node.js](https://nodejs.org), available to your agent. Agents use it to run Rhizome scripts.

You don't need any API keys to begin.

Joining a repository that already uses Rhizome? Do step 1, then [join the project](docs/reference/guides/getting-started.md#join-a-project-your-team-already-uses).

### 1. Install Rhizome Desktop

On a Mac with Homebrew:

```bash
brew install --cask atomicobject/tap/rhizome-desktop
```

Then open Rhizome from Applications. Homebrew also puts the app's `rzm` command on your PATH, which is how your coding agent reaches Rhizome. If you have the older CLI-only formula, run `brew uninstall --formula atomicobject/tap/rhizome` first.

<details>
<summary>Ubuntu, or a Mac without Homebrew</summary>

On a Mac, download [Rhizome-macos-arm64.dmg](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-macos-arm64.dmg), drag Rhizome to Applications, and open it.

On Ubuntu, download the [x64](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-linux-x64.AppImage) or [Arm64](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-linux-arm64.AppImage) AppImage, move it somewhere permanent such as `~/Applications/Rhizome.AppImage`, make it executable, and open it. [Linux details](docs/reference/guides/getting-started.md#linux)

Then open **Settings** and, under **Command-line tool**, choose **Install Command-Line Tool**. This puts `rzm` on your PATH. macOS asks for an administrator password when it needs one.

</details>

The app updates itself. For Intel Macs, Windows, or a server, see the [command-line installation](docs/reference/guides/command-line.md#install-without-the-desktop-app). You can also ask your coding agent to [install Rhizome for you](#install-with-a-coding-agent).

### 2. Add your repository

Choose **Add Repository…** and pick your repository's folder. Rhizome inspects it and shows what it found and every file setup would write. Nothing is written until you confirm.

The defaults suit most projects: the Agentic Engineering workflow, guidance for the coding agents Rhizome finds, and keyword search. Semantic search needs a Voyage AI key, which you can paste now or add later.

Choose **Set up Rhizome**. When it finishes, commit the files it lists so your teammates get the same setup, then choose **Open workspace**. [What each choice means](docs/reference/guides/getting-started.md#set-up-a-new-repository)

If the repository already uses Rhizome, the app asks **Trust this worktree?** instead of showing setup. A worktree is your checkout of the repository. Choose **Trust and open**. [Joining a project](docs/reference/guides/getting-started.md#join-a-project-your-team-already-uses)

### 3. Start a new agent session

Setup wrote `AGENTS.md` and agent skills into the repository, plus `CLAUDE.md` for Claude Code. Your agent reads them and runs `rzm` to search the project, read its notes, and check its changes.

Close any agent session already open in the repository and start a new one, so the agent finds the new skills. Or use the **Agent** tab in the app, which runs Claude Code or Codex in the repository for you.

Ask:

> Use the rhizome skill to orient me to this repository. Show me the available workflow and any setup gaps before we start work.

The agent should describe the Agentic Engineering workflow. If it can't find `rzm`, or Codex can't reach Rhizome, see [Make Rhizome available to your agent](docs/reference/guides/getting-started.md#make-rhizome-available-to-your-agent).

### 4. Give your agent a task

To shape new work:

> Use the agentic-engineering skill to help me add password reset. Start with the intended behavior and the decisions we need to make.

For a clear fix:

> Use agentic-engineering implement to fix this bug, verify it, and prepare a pull request.

The skill sizes the work and follows your team's policy. Before you delegate implementation, put your real test commands in `docs/engineering/quality-gates.md`, because the skills run them to prove work is done. [Using the skills](docs/reference/guides/using-skills.md)

## Use the app alongside your agent

Each window shows one repository. Three tabs matter at first:

- **Notes** shows the project's Markdown and the **Delivery** view, which tracks specs, stories, and efforts from shaping to done. On any piece of work, **Copy workflow prompt** copies the prompt that continues it. Paste it into your agent.
- **Agent** runs Claude Code or Codex in the repository.
- **Tasks** lists scheduled agent work that needs you.

Press ⌘K to search the project and ? to see every shortcut. [A tour of the app](docs/reference/guides/getting-started.md#find-your-way-around)

## Make it your team's process

The setup gives you starting points to change. Edit `docs/engineering/` with your team's test commands, review expectations, and release steps; the skills consult those documents as they work. Change workflows and add-ons from the repository's tab in **Settings**. As you learn, improve the skills or ask an agent to build views for your project. [Adapting Rhizome](docs/reference/guides/adapting-rhizome.md)

## Go deeper

| You want to… | Read |
| --- | --- |
| Install on Linux, join a project, connect an agent, or set up search keys | [Getting started](docs/reference/guides/getting-started.md) |
| Define work, implement a spec, or capture meeting context | [Using the skills](docs/reference/guides/using-skills.md) |
| Change team policy, skills, data, or views | [Adapting Rhizome](docs/reference/guides/adapting-rhizome.md) |
| Build an experience: several views that run as one site, like Delivery | [Build an experience](docs/reference/guides/build-an-experience.md) |
| Schedule recurring agent work | [Scheduled tasks](docs/reference/guides/scheduled-tasks-guide.md) |
| Let Rhizome learn from your agent sessions | [Compounding](docs/reference/guides/compounding.md) |
| Compare workflows | [Choosing a starter](docs/reference/guides/choosing-your-starter.md) |
| Understand retrieval and links between notes and code | [How Rhizome works](docs/reference/guides/how-rhizome-works.md) |
| Use `rzm` from a terminal or script, or install without the app | [Command-line reference](docs/reference/guides/command-line.md) |
| Diagnose slow indexing, runtime failures, or workspace stalls | [Operational diagnostics](docs/reference/guides/diagnostics.md) |
| Contribute to Rhizome itself | [Contributor guide](CONTRIBUTING.md) |

## Install with a coding agent

Tell your agent: "Install Rhizome Desktop by following the 'Install with a coding agent' section of https://github.com/atomicobject/rhizome."

<details>
<summary>Agent instructions</summary>

These steps are for macOS with Apple silicon.

Prefer the desktop cask when Homebrew is available. If the CLI-only `rhizome` formula is installed, uninstall it first with `brew uninstall --formula atomicobject/tap/rhizome`. Install `atomicobject/tap/rhizome-desktop` with `--cask`, open the app, and verify `command -v rzm`, its symbolic link into `Rhizome.app`, and the app and CLI versions. Homebrew manages this CLI link, so skip the app's Install Command-Line Tool step. Preserve unrelated executables and report any collision instead of forcing an overwrite.

Use the following steps for a direct DMG installation when Homebrew is unavailable:

1. Stop and tell the user if `uname -m` is not `arm64`; follow the [command-line installation](docs/reference/guides/command-line.md#install-without-the-desktop-app) instead. If `/Applications/Rhizome.app` already exists, skip to step 3, because the app updates itself.
2. Download the notarized app, check it against the release checksums, and copy it into Applications.

   ```bash
   set -euo pipefail
   base=https://github.com/atomicobject/rhizome/releases/latest/download
   tmp=$(mktemp -d)
   curl -fsSL -o "$tmp/Rhizome-macos-arm64.dmg" "$base/Rhizome-macos-arm64.dmg"
   curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
   (cd "$tmp" && grep ' Rhizome-macos-arm64.dmg$' checksums.txt | shasum -a 256 -c -)
   mkdir "$tmp/mnt"
   hdiutil attach -nobrowse -readonly -mountpoint "$tmp/mnt" "$tmp/Rhizome-macos-arm64.dmg"
   ditto "$tmp/mnt/Rhizome.app" /Applications/Rhizome.app
   hdiutil detach "$tmp/mnt"
   open -a /Applications/Rhizome.app
   ```

3. Ask the user to open Rhizome › Settings and choose Install Command-Line Tool, and to enter their password when macOS asks. Wait for them to confirm. This is the only step that needs them.
4. Verify in a new login shell. The path should be `/usr/local/bin/rzm` and the version should match the app's:

   ```bash
   zsh -lc 'command -v rzm && rzm --version'
   defaults read /Applications/Rhizome.app/Contents/Info.plist CFBundleShortVersionString
   ```

   If another path comes first, Settings names it. Tell the user, for example to run `brew uninstall --formula atomicobject/tap/rhizome` for the CLI-only formula.
5. Report the installed version. Tell the user to set up a repository by choosing Add Repository… in the app.

</details>

## Help and contributing

Rhizome is maintained by [Atomic Object](https://atomicobject.com). [Report a bug or suggest an improvement](https://github.com/atomicobject/rhizome/issues), or read the [contributor guide](CONTRIBUTING.md). For vulnerabilities, follow the [security policy](SECURITY.md).

## Acknowledgments and license

Rhizome began as a fork of [Yakitrak/obsidian-cli](https://github.com/Yakitrak/obsidian-cli) by Kartikay Jainwal. Thank you to the original author and contributors.

Rhizome is [MIT licensed](https://github.com/atomicobject/rhizome/blob/main/LICENSE). Included third-party components retain their own licenses; see [third-party notices](THIRD_PARTY_LICENSES.md).
