<p align="center">
  <img src="docs/logo.png" alt="Rhizome" width="200">
</p>

# Rhizome

**Bring Atomic Object's Agentic Engineering process into your repository.**

Rhizome gives your team shared skills for defining work, planning implementation, building, and reviewing with AI agents. Specs, decisions, and delivery records live alongside your code, so the next developer or agent can pick up the context.

Start with the [Agentic Engineering](https://atomicobject.com/agentic-engineering) starter. It installs useful defaults that your team can adapt: how much planning a task needs, which checks prove it works, and when a person needs to make a decision. Small fixes stay small.

The [process guide](https://atomicobject.com/agentic-engineering/overview) explains how the team works together. Rhizome supplies repository tools and agent guidance to help put that process into practice.

## Get started

You'll need a coding agent such as Codex or Claude Code, plus Node.js on its PATH for Rhizome's agent code mode. You can use the basic workflow without provider API keys or Obsidian.

### 1. Install Rhizome

On macOS with Homebrew:

```bash
brew tap atomicobject/homebrew-tap
brew install rhizome
```

For Linux, Windows, or a project-local launcher, follow the [installation guide](docs/reference/guides/getting-started.md#install).

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

A skill can ship code-mode scripts that gather its context in one step. An agent runs one with `rzm agent code execute --session-id <id> --input '{"path": "Projects/Atlas.md"}' < scripts/subject.js`, and the script reads that JSON value as `input`; `--input-file <path>` reads it from a file instead. To read a view's rows without its field capability catalog, add `--omit-capabilities` to `rzm agent view run`.

## See and shape the work

```bash
rzm start
```

Rhizome opens a local browser workspace where you can read notes and specs, follow efforts and stories, and use the starter's views. Keep the terminal running while you use it.

[Rhizome Desktop](desktop/README.md) provides a Tauri app with a saved folder list, automatic runtime startup, and global installation controls. It opens each folder's configured Rhizome UI in its own window. The desktop app is currently built from source on macOS.

When the default views stop answering your team's questions, you can adapt them or ask an agent to build a custom view over your project data. Meeting notes, action items, requirements, and other information can become part of the same workspace. Display groups open Briefing, Trace, or Sections views; `rzm view eject <id>` (for example `rzm view eject group.briefing`) copies one into `.rhizome/views/` so your team can modify it. [Adapting the workspace](docs/reference/guides/adapting-rhizome.md#adapt-the-workspace)

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
