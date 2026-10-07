---
type: ReferenceDoc
reference-kind: guide
summary: "Install Rhizome, initialize an Agentic Engineering project, and join an existing team."
last-verified: 2026-10-01
status: active
---

# Getting started with Rhizome

[Back to the README](../../../README.md) · [Next: using the skills](using-skills.md)

Use the Agentic Engineering starter to give your coding agent a shared project workflow. You can start with the installed defaults and a small task, then adapt the process as you learn.

## Before you begin

- Have a Git repository and a coding agent such as Codex or Claude Code.
- Install Node.js and make sure the agent can run `node`. Rhizome's agent code mode uses it to combine retrieval and other tool calls.
- You can start without provider API keys. Semantic embeddings and TypeSafe evaluation are optional additions described below.

Obsidian is optional. Project documents are ordinary files that you can read in your editor, on GitHub, or in Rhizome's browser workspace.

## Install

### macOS with Homebrew

```bash
brew tap atomicobject/homebrew-tap
brew install rhizome
rzm --version
```

### Installer for macOS, Linux, and Windows

Run these commands from your project root. On Windows, use Git Bash.

```bash
curl -fsSLo /tmp/install-rzm.sh \
  https://raw.githubusercontent.com/atomicobject/rhizome/main/scripts/install/install-rzm.sh
bash /tmp/install-rzm.sh --project "$PWD" --user-binary auto
```

This installs a project launcher at `./bin/rzm`. If `rzm` is not already on PATH, it also installs a user binary for the bare command used by agent skills. Review the installer before running it if you wish. On macOS, PATH registration may ask for your password. Restart your terminal or coding app after installation if it cannot find `rzm`.

If the user binary is new and your shell cannot find it, add its directory to PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
rzm --version
```

Make that PATH entry persistent in your shell configuration, and ensure desktop-launched coding agents inherit it too.

The installer preserves an existing project version pin. A new project uses the latest release. Installation and initialization are separate steps.

If you only want the project launcher, use `--user-binary skip` instead. Invoke `./bin/rzm` directly, and put the project's `bin` directory on the PATH of any agent that needs the bare command. For an agent launched from the current terminal:

```bash
export PATH="$PWD/bin:$PATH"
```

### Already installed by your team?

If the repository has `.rhizome/config.yml`, its skills, and a committed `bin/rzm`, use the existing setup:

```bash
./bin/rzm --version
./bin/rzm index
```

Ensure your coding agent can also resolve `rzm`. You do not need to initialize a configured project again just to join it.

With a global installation, review the checkout and run `rzm trust` before automation. Trust lets that checkout select and run its configured Rhizome binary, including future updates there. Each new checkout or worktree needs its own trust decision. Revoke it with `rzm untrust`.

## Initialize your project

From the repository root:

```bash
rzm init
```

`rzm init` first shows what it found:

- **Docs**: how many notes it found and where. Rhizome indexes all Markdown, so docs added later are indexed too.
- **Code**: the languages it found. Code indexing covers the whole repository, and each file's language comes from its extension.
- **Skip**: bulky checked-in content it suggests leaving out of the index, such as vendored code, minified bundles, or very large files. This row appears only when there is something to suggest, and confirmed skips go into `.rhizome/ignore`.
- **Agents**: the coding agents it detected from the repository and from commands installed on your machine.
- **Search**: the semantic search provider. Voyage AI is the recommended provider; OpenAI and Ollama are alternatives.

It then asks which workflow to install: Agentic Engineering (the default), Agentic Engineering with domain modeling, or search and agent guidance only. If no semantic search key is available, it asks for one. Press Enter to skip the key and set it up later; semantic search stays off until a key is available. Finally it asks `Set up Rhizome? [Y/n/e to edit]`. Press `e` to change what gets indexed, the search provider, agents, or workflow before anything is written.

After writing the setup, it asks `Build the search index now? [Y/n]`. Press Enter to index right away, or `n` to run `rzm index` later. Runs without a terminal, such as an agent running init, skip this question and list `rzm index` as the next step.

With only a project launcher, or to choose the workflow without the question:

```bash
./bin/rzm init --workflow agentic-engineering
./bin/rzm index
```

When `rzm init` runs without a terminal, for example from an agent or a script, it uses the recommendations without asking and prints what it did. Use `--agents claude,codex,cursor` or `--search voyage|openai|ollama|off` to choose those settings without prompts.

A first run also accepts `--addons action-items` or `--addons none` to choose the add-ons that come with the workflow, `--skip <path>` and `--keep-indexed <path>` to change the suggested skips, and `--search-key-stdin` to save a key piped to it, so the key never appears in a command line. Add `--json` for one machine-readable document: with `--check`, what setup would do with those choices and every choice it offers; without it, what setup wrote. The Rhizome desktop app sets up folders this way.

The starter installs:

| Location | What your team uses it for |
| --- | --- |
| `docs/engineering/` | Team policy, test commands, review expectations, and release steps |
| `docs/specs/` | Intended behavior and constraints |
| `docs/efforts/` | Scoped delivery work, plans, and evidence |
| `docs/meetings/` and `docs/reference/` | Source material, decisions, and project context |
| `.agents/skills/` | Shared agent workflow instructions |
| `AGENTS.md` and, when enabled, `CLAUDE.md` | Guidance that routes agents into those skills |
| `.rhizome/` | Shared configuration, schema, saved queries, and view definitions, plus ignored local caches |

The Agentic Engineering starter also enables the base identity and action-item starters. Claude Code receives a `.claude/skills/` mirror when detected. If your agent was not detected, rerun with `--agents` and the agents you use, for example `rzm init --agents claude,codex`. Codex reads the shared `.agents/skills/` directory.

Open a new agent session after installing skills so the tool can discover them.

## Share the setup

Review and commit the generated skills, guidance, engineering documents, and tracked `.rhizome/` files, including `.rhizome/generated-files.yml`, which records what Rhizome wrote so later runs can tell which files someone edited. Commit `bin/rzm` too if you used the installer. Rhizome's generated ignore rules keep its binary downloads, indexes, and local sessions out of Git.

Do not commit provider keys or user-local configuration. Teammates install Rhizome, review and trust their checkout when using a global binary, and index their own copy. The project pin keeps the team's Rhizome version consistent.

Before delegating much work, fill in your actual commands in `docs/engineering/quality-gates.md` and your review expectations in `review-and-approval.md`. [Adapting the process](adapting-rhizome.md#start-with-team-policy)

## Check that it works

Ask your coding agent:

> Use the rhizome skill to orient me to this repository. Show me the available workflow and any setup gaps before we start work.

<details>
<summary>Optional command-line diagnostics</summary>

With a project launcher:

```bash
./bin/rzm --version
test -s .rhizome/config.yml
perl -0777 -e 'exit !(m{<!-- BEGIN RZM INIT RHIZOME BLOCK -->.*<!-- END RZM INIT RHIZOME BLOCK -->}s)' AGENTS.md 2>/dev/null || \
  perl -0777 -e 'exit !(m{<!-- BEGIN RZM INIT RHIZOME BLOCK -->.*<!-- END RZM INIT RHIZOME BLOCK -->}s)' CLAUDE.md 2>/dev/null
test -f .agents/skills/rhizome/SKILL.md || \
  test -f .claude/skills/rhizome/SKILL.md
./bin/rzm agent start --intent "verify Rhizome integration"
```

Use `rzm` in place of `./bin/rzm` for a global installation. The final command should return a session ID. The preceding checks confirm that the configuration and guidance are present; a session alone does not prove the skills were installed.

</details>

If guidance is missing, run `rzm init --check` to see what init would change, then run `rzm init` and check again. If the agent cannot find `rzm` or `node`, fix its PATH and restart that agent.

## Open the workspace

```bash
rzm start
```

This opens the local browser UI. You can read project documents and browse the starter's spec, effort, story, and action-item views. Keep the terminal running; press Ctrl+C to end the attached runtime when finished. The view definitions and source notes stay in your repository.

## Optional providers

Rhizome's workflow skills use your installed coding agent. Their basic use does not require a separate Rhizome provider key.

| Feature | Provider setup |
| --- | --- |
| Semantic embeddings for search | A Voyage AI key (recommended), an OpenAI key, or a local Ollama embedding model |
| Content evaluation through Rhizome's `evaluate` tools | TypeSafe credentials |

Without embeddings, indexed text, code navigation, and structured queries still work. Choose a semantic search provider during `rzm init`, or later with `rzm init --search voyage|openai|ollama|off`. Keep keys in your environment or user-global `~/.config/rhizome/config.yml`, outside Git.

For teams using 1Password, install and authenticate the [1Password CLI](https://developer.1password.com/docs/cli/). Replace these example references with the account, vault, item, and fields supplied by your team:

```bash
rzm credentials import --from 1password --account example.1password.com \
  --key 'VOYAGE_API_KEY=op://Shared/Rhizome/VOYAGE_API_KEY' \
  --key 'TYPESAFE_API_KEY=op://Shared/Rhizome/TYPESAFE_API_KEY'
```

The import stores plaintext values in your user-global config with restricted permissions and does not print them. Normal use needs no running 1Password process. Reimport after a key rotation.

## Updates and other installation choices

For a Rhizome-managed installation, run `rzm update` and review its proposed version changes, then rerun `rzm init`. A rerun shows the current setup and a list of changes, then asks `Apply? [Y/n/s for settings]`. Rhizome files that nobody edited update without further questions; init asks only about files someone edited. Commit shared changes for teammates. Normal init reruns preserve team-owned engineering documents; see [update ownership](adapting-rhizome.md#keep-updates-under-control).

If you manage tools with mise or another external manager, use `rzm init --binary-manager external`. That manager then owns installation and upgrades instead of Rhizome's version pin. Read the [version-pinning design](../../specs/technical/version-pinned-install-and-update.md) for the detailed ownership rules.

[Next: use the skills on real work](using-skills.md)
