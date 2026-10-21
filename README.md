<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/rhizome-wordmark-reversed.svg">
    <img src="docs/brand/rhizome-wordmark.svg" alt="Rhizome" width="320">
  </picture>
</p>

# Rhizome

**Bring Atomic Object's Agentic Engineering process into your repository.**

Rhizome gives your team shared skills for defining work, planning implementation, building, and reviewing with AI agents. Specs, decisions, and delivery records live alongside your code, so the next developer or agent can pick up the context.

Start with the [Agentic Engineering](https://atomicobject.com/agentic-engineering) starter. It installs useful defaults that your team can adapt: how much planning a task needs, which checks prove it works, and when a person needs to make a decision. Small fixes stay small.

The [process guide](https://atomicobject.com/agentic-engineering/overview) explains how the team works together. Rhizome supplies repository tools and agent guidance to help put that process into practice.

## Get started

You'll need a coding agent such as Codex or Claude Code, plus Node.js on its PATH for Rhizome's agent code mode. You can use the basic workflow without provider API keys or Obsidian.

### 1. Install Rhizome

On a Mac with Apple silicon, install Rhizome Desktop. It includes `rzm` and keeps both up to date.

1. Download [Rhizome-macos-arm64.dmg](https://github.com/atomicobject/rhizome/releases/latest/download/Rhizome-macos-arm64.dmg), open it, and drag Rhizome to Applications.
2. Open Rhizome. The app is not notarized yet, so the first time macOS may say it cannot verify it. Open System Settings > Privacy & Security, choose Open Anyway, and confirm.
3. In Rhizome > Settings, choose Install Command-Line Tool. macOS asks for your password once. `rzm` then works in terminals and coding agents until you delete the app.

Or ask your coding agent to [install it for you](#install-with-a-coding-agent).

For the command-line tool only, use Homebrew:

```bash
brew tap atomicobject/homebrew-tap
brew install rhizome
```

For Linux, Windows, Intel Macs, or a project-local launcher, follow the [installation guide](docs/reference/guides/getting-started.md#install).

### 2. Initialize your project

From your project's repository root:

```bash
rzm init
```

`rzm init` shows the docs, code, and agents it found, asks which workflow to install (Agentic Engineering is the default), and sets everything up after one confirmation, then offers to build the search index. If no semantic search key is available, it asks for a Voyage AI key; press Enter to skip that and set it up later. Rhizome adds project configuration, agent skills, and places for specs and delivery work. Commit `.rhizome/` and the agent files it creates so teammates use the same guidance. [What gets installed and how to share it](docs/reference/guides/getting-started.md#initialize-your-project)

When preparing another checkout, run `rzm new-worktree <source-worktree-path>` from the new worktree. It copies the index and seeds personal view settings once. Each worktree remembers later preference changes independently, and existing personal settings are preserved.

### 3. Give your agent a task

Open a new coding-agent session in the repository so it discovers the installed skills. Before delegating implementation, put your actual test commands in `docs/engineering/quality-gates.md`.

Then ask:

> Use the agentic-engineering skill to help me add password reset. Start with the intended behavior and the decisions we need to make.

Or, for a clear fix:

> Use agentic-engineering implement to fix this bug, verify it, and prepare a pull request.

The skill routes the work according to its size and your team's policy. You can shape an uncertain task together or delegate a bounded task with a clear outcome. [Using the skills](docs/reference/guides/using-skills.md)

## Make it your team's process

Start by editing the generated `docs/engineering/` documents with your team's test commands, review expectations, architecture rules, and release steps. The skills consult these documents as they work. Keep them in Git so improvements reach the whole team.

As you learn, you can improve the skills, change how specs are organized, and add workflows that fit your project. [Adapting your process](docs/reference/guides/adapting-rhizome.md)

Add-ons extend a workflow: `rzm init --add-addon session-memory` adds one that learns from your past agent sessions which facts agents keep rediscovering and what slows them down, and proposes fixes and context for you to approve. `--remove-addon` takes one out. [Add-ons](docs/reference/guides/getting-started.md#initialize-your-project)

A skill can ship code-mode scripts that gather its context in one step. An agent runs one with `rzm agent code execute --session-id <id> --input '{"path": "Projects/Atlas.md"}' < scripts/subject.js`, and the script reads that JSON value as `input`; `--input-file <path>` reads it from a file instead. To read a view's rows without its field capability catalog, add `--omit-capabilities` to `rzm agent view run`.

## See and shape the work

```bash
rzm start
```

Rhizome opens a local browser workspace where you can read notes and specs, follow efforts and stories, and use the starter's views. Keep the terminal running while you use it.

[Rhizome Desktop](desktop/README.md) keeps your repositories in a sidebar, starts each worktree's Rhizome on its own, and sets up new folders with the version it carries. When a repository pins an older Rhizome, it offers to update the pin; the change is a one-line edit to `.rhizome/config.yml` that you commit.

When the default views stop answering your team's questions, you can adapt them or ask an agent to build a custom view over your project data. Meeting notes, action items, requirements, and other information can become part of the same workspace. Display groups open Briefing, Trace, or Sections views; `rzm view eject <id>` (for example `rzm view eject group.briefing`) copies one into `.rhizome/views/` so your team can modify it. [Adapting the workspace](docs/reference/guides/adapting-rhizome.md#adapt-the-workspace)

## Install with a coding agent

These steps are written for a coding agent on macOS with Apple silicon. Tell your agent: "Install Rhizome Desktop by following the 'Install with a coding agent' section of https://github.com/atomicobject/rhizome."

Agent instructions:

1. Stop and tell the user if `uname -m` is not `arm64`; follow the [installation guide](docs/reference/guides/getting-started.md#install) instead. If `/Applications/Rhizome.app` already exists, skip to step 3: the app updates itself.
2. Download the app, check it against the release checksums, and copy it into Applications. Files fetched with `curl` carry no quarantine flag, so macOS opens the app without a Gatekeeper prompt.

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

3. Ask the user to open Rhizome > Settings and choose Install Command-Line Tool, and to enter their password when macOS asks. Wait for them to confirm. This is the only step that needs them.
4. Verify in a new login shell. The path should be `/usr/local/bin/rzm` and the version should match the app's:

   ```bash
   zsh -lc 'command -v rzm && rzm --version'
   defaults read /Applications/Rhizome.app/Contents/Info.plist CFBundleShortVersionString
   ```

   If another path comes first, Settings names it; tell the user, for example to run `brew uninstall rhizome` for a Homebrew copy.
5. Report the installed version. To set up a repository next, run `rzm init` from its root, or add the folder in Rhizome.

## Go deeper

| You want to… | Read |
| --- | --- |
| Install, join an existing project, or configure optional providers | [Getting started](docs/reference/guides/getting-started.md) |
| Define work, implement a spec, or capture meeting context | [Using the skills](docs/reference/guides/using-skills.md) |
| Change team policy, skills, data, or views | [Adapting Rhizome](docs/reference/guides/adapting-rhizome.md) |
| Explore other workflow starters | [Choosing a starter](docs/reference/guides/choosing-your-starter.md) |
| Understand retrieval and links between notes and code | [How Rhizome works](docs/reference/guides/how-rhizome-works.md) |
| Diagnose slow indexing, runtime failures, or degraded operations | [Operational diagnostics](docs/reference/guides/diagnostics.md) |
| Contribute to Rhizome itself | [Contributor guide](CONTRIBUTING.md) |

ID suggestions from `rzm agent next-id` and `rzm agent ontology-authoring-guide` use the current ontology and shared identifier pool.

For command options, run `rzm --help` or `rzm <command> --help`.

## Help and contributing

Rhizome is maintained by [Atomic Object](https://atomicobject.com). [Report a bug or suggest an improvement](https://github.com/atomicobject/rhizome/issues), or read the [contributor guide](CONTRIBUTING.md). For vulnerabilities, follow the [security policy](SECURITY.md).

## Acknowledgments and license

Rhizome began as a fork of [Yakitrak/obsidian-cli](https://github.com/Yakitrak/obsidian-cli) by Kartikay Jainwal. Thank you to the original author and contributors.

Rhizome is [MIT licensed](https://github.com/atomicobject/rhizome/blob/main/LICENSE). Included third-party components retain their own licenses; see [third-party notices](THIRD_PARTY_LICENSES.md).
