---
type: ReferenceDoc
reference-kind: guide
summary: "Reference for the rzm command: installing without the desktop app, setting up and updating repositories from a terminal or script, trust, the browser workspace, the team API gateway, modules and actions, and agent-facing commands."
last-verified: 2026-11-13
status: active
---

# Command-line reference

[Back to the README](../../../README.md) · [Getting started](getting-started.md)

Rhizome Desktop is the recommended way to set up and use Rhizome; [Getting started](getting-started.md) describes it. Everything the app does runs through `rzm`, the same program your coding agent uses. This reference covers `rzm` for terminals, scripts, and installations without the app.

For every command and option, run `rzm --help` or `rzm <command> --help`.

## Install without the desktop app

Use one of these on Intel Macs, Windows, servers, or anywhere you want only the command. Your coding agent also needs Node.js available as `node` to run Rhizome scripts.

### Homebrew formula

```bash
brew install --formula atomicobject/tap/rhizome
rzm --version
```

The formula and the `rhizome-desktop` cask both provide `rzm`. Install only one; to move to the app, uninstall the formula first.

### Installer script for macOS, Linux, and Windows

Run these commands from your project root. On Windows, use Git Bash.

```bash
curl -fsSLo /tmp/install-rzm.sh \
  https://raw.githubusercontent.com/atomicobject/rhizome/main/scripts/install/install-rzm.sh
bash /tmp/install-rzm.sh --project "$PWD" --user-binary auto
```

This installs a project launcher at `./bin/rzm`. If `rzm` is not already on PATH, it also installs a user binary for the bare command used by agent skills. Review the installer before running it if you wish. On macOS, PATH registration may ask for your password. Restart your terminal or coding app if it cannot find `rzm`.

If the user binary is new and your shell cannot find it, add its directory to PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
rzm --version
```

Make that PATH entry persistent in your shell configuration, and make sure desktop-launched coding agents inherit it too.

The installer preserves an existing project version pin. A new project uses the latest release. Installation and initialization are separate steps.

If you only want the project launcher, use `--user-binary skip`. Invoke `./bin/rzm` directly, and put the project's `bin` directory on the PATH of any agent that needs the bare command:

```bash
export PATH="$PWD/bin:$PATH"
```

With either choice, set up the repository with the launcher:

```bash
./bin/rzm init --workflow agentic-engineering
./bin/rzm index
```

Commit `bin/rzm` with the other files setup writes, so teammates get the same launcher.

### A repository with a committed launcher

If the repository has `.rhizome/config.yml`, its skills, and a committed `bin/rzm`, use the existing setup:

```bash
./bin/rzm --version
./bin/rzm index
```

Make sure your coding agent can also resolve `rzm`. You don't need to initialize a configured project again to join it. With a global installation instead of the launcher, review the checkout and run `rzm trust` before automation.

## Use the desktop app's `rzm`

Rhizome Desktop bundles its own `rzm`. The Homebrew cask links it into your PATH; otherwise choose **Install Command-Line Tool** under **Command-line tool** in the app's **Settings**, which links `/usr/local/bin/rzm` on macOS and `~/.local/bin/rzm` on Linux. To make the app run a standalone installation or a local build instead, choose **Use an existing executable** in **Settings**. [Make Rhizome available to your agent](getting-started.md#make-rhizome-available-to-your-agent)

## Set up a repository

From the repository root:

```bash
rzm init
```

In a terminal, `rzm init` shows what it found:

- **Docs**: how many notes it found and where.
- **Code**: the languages it found. Code indexing covers the whole repository, and each file's language comes from its extension.
- **Skip**: bulky checked-in content it suggests leaving out of the index. Confirmed skips go into `.rhizome/ignore`.
- **Agents**: the coding agents it detected from the repository and from commands installed on your machine.
- **Search**: the semantic search provider.

It then asks which workflow to install, asks for a semantic search key if none is available (press Enter to skip, or type `other` to choose OpenAI or Ollama), and asks `Set up Rhizome? [Y/n/e to edit]`. Press `e` to change what gets indexed, the search provider, agents, workflow, or add-ons before anything is written. Afterward it asks `Build the search index now? [Y/n]`.

To choose without prompts:

| Option | Effect |
| --- | --- |
| `--workflow agentic-engineering\|domain\|none` | The workflow; `core` and `action-items` are also accepted. See [Choosing a starter](choosing-your-starter.md). |
| `--agents claude,codex,cursor` | The agents to write guidance and skills for |
| `--search voyage\|openai\|ollama\|off` | The semantic search provider |
| `--search-key-stdin` | Save a key piped to standard input, so it never appears on a command line |
| `--addons action-items,compounding` or `--addons none` | A first run's add-ons, replacing the workflow's defaults |
| `--skip <path>`, `--keep-indexed <path>` | Change the suggested index skips |

When `rzm init` runs without a terminal, for example from an agent or a script, it uses the recommendations without asking and prints what it did, listing `rzm index` as the next step.

`--json` prints one machine-readable document. With `--check`, it reports what setup would do with the given options and every choice it offers, without writing; without `--check`, it applies them and reports what it wrote. Rhizome Desktop sets up and changes repositories this way. For example, `rzm init --check --json --workflow domain --add-addon compounding` previews a change.

### Change a configured repository

Rerunning `rzm init` shows the current setup and a list of changes, then asks `Apply? [Y/n/s for settings]`. Press `s` to change the workflow, add-ons, agents, or search provider.

- `rzm init --add-addon <id>` and `--remove-addon <id>` add or remove one add-on without changing the others. The settings menu's Add-ons section does the same, and offers to turn hooks on for an add-on that uses them.
- `rzm init --hooks on` lets add-ons that use agent hooks, such as compounding, deliver context to Claude Code and Codex automatically. In Codex, trust each new or changed entry through `/hooks`.
- An approved compounding guard rule refuses a matching command before it runs. To activate one, commit its finding note, rerun `rzm init`, and in Codex trust the changed entry through `/hooks`.
- `rzm init --refresh-docs` offers Rhizome's latest versions of the team-owned starter documents.
- `rzm init --eject <starter>` takes over management of a starter's files, and `--restore <starter>` returns it to managed updates.
- `rzm init --check` reports what init would change, without writing.

Rhizome files that nobody edited update without questions; init asks only about files someone edited. Runs without a terminal keep edited files and list them. [Update ownership](adapting-rhizome.md#keep-updates-under-control)

## Trust

```bash
rzm trust
rzm untrust
```

Trust lets a checkout select and run its pinned Rhizome binary, including future updates, and lets its scheduled tasks and module actions run. Each new clone needs its own trust decision. A Git worktree of a trusted checkout inherits that checkout's trust. Revoke inherited trust with `rzm untrust` in the main checkout; a worktree trusted on its own keeps its record until you run `rzm untrust` there too. Rhizome Desktop records the same trust when you choose **Trust and open** or set up a repository.

## Index and open the workspace

| Command | What it does |
| --- | --- |
| `rzm index` | Build or update the search index. `--rebuild` starts over. |
| `rzm start` | Start Rhizome for this worktree and open the workspace in your browser. Keep the terminal running; Ctrl+C ends it. |
| `rzm stop` | Stop this worktree's background Rhizome. |
| `rzm desktop [path]` | Open a folder in Rhizome Desktop, adding it to the library if needed. |
| `rzm new-worktree <source-worktree-path>` | Run from a new Git worktree to copy the source's index and seed personal view settings once. The app does this automatically. |

## Updates and version pins

`rzm update` reviews and applies newer releases for the global installation and for the repository's pin. When `rzm` is Rhizome Desktop's, the app installs the newer release. When the update changes the version a configured repository runs, `rzm update` reruns the new version's `init`; review its confirmation, or pass `--yes` to apply without asking while keeping edited files. Commit the refreshed files with the pin change.

If you manage tools with mise or another external manager, use `rzm init --binary-manager external`. That manager then owns installation and upgrades instead of Rhizome's pin. See the [version-pinning design](../../specs/technical/version-pinned-install-and-update.md) for the ownership rules.

## Verify an installation

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

## Provider keys

Keep keys in your environment or in the user-global `~/.config/rhizome/config.yml`, outside Git. `rzm init --search voyage|openai|ollama|off` changes a repository's provider.

To import keys from 1Password items, install and sign in to the [1Password CLI](https://developer.1password.com/docs/cli/), then replace these example references with the account, vault, item, and fields your team supplies:

```bash
rzm credentials import --from 1password --account example.1password.com \
  --key 'VOYAGE_API_KEY=op://Shared/Rhizome/VOYAGE_API_KEY' \
  --key 'TYPESAFE_API_KEY=op://Shared/Rhizome/TYPESAFE_API_KEY'
```

The import stores plaintext values in your user-global config with restricted permissions and does not print them. Reimport after a key rotation. Rhizome Desktop's Settings can also import from a team 1Password Environment; see [Credential setup](../../engineering/secrets.md#developer-setup).

## Team API gateway

A team API gateway provides per-person Google sign-in for the Atomic Object pilot. Your own provider keys still take precedence. [Team API gateway](../../../services/team-api/README.md)

- `rzm auth login --server <gateway URL>` signs in once for every Rhizome process on your computer.
- `rzm auth status` and `rzm auth logout` manage the session.
- `rzm auth admin` lets admins check usage and manage people, sessions, and limits.

Admins set per-provider daily budgets with `rzm auth admin config --server <gateway URL>`; the [operations runbook](../../../services/team-api/RUNBOOK.md#provider-budgets-and-first-enablement) covers the options, limits, and deployment.

## Scheduled tasks

| Command | What it does |
| --- | --- |
| `rzm tasks status` | Each task's mode, schedule, recent token use, and problems |
| `rzm tasks inbox` | Items that need you |
| `rzm tasks run --task <task>` | Run one task now, check first |
| `rzm tasks start <item>` | Open your agent harness on an inbox item |
| `rzm tasks mode <task> auto\|notify\|off\|default` | Your own mode for a task |
| `rzm tasks pause`, `rzm tasks resume` | Stop or restart all background task work for you in this repository |

[Scheduled tasks](scheduled-tasks-guide.md) covers declaring tasks and every state.

## Modules, actions, and views

Each starter installs as a module, a folder `.rhizome/modules/<name>/` that holds its ontology, query recipes, views, tasks, actions, and scripts. `rzm <module>` lists a module's query recipes and actions, and `rzm <module> <recipe> --<input> <value>` runs a recipe. `rzm module <name> ...` does the same when a built-in command shares the module's name. [Rhizome modules](../../specs/technical/rhizome-modules.md)

An action is an operation a module declares in `actions/<name>.yaml`, with typed inputs and a command or code-mode script to run. `rzm <module> <action> --<input> <value>` runs one; repeat the flag for a list input, and pass `true` or `false` for a bool input. Custom views run actions through the kit and agents through code mode. Every run requires a trusted repository. It prints the result as JSON and exits 0, prints the reason and exits 2 when the action refuses, and exits 1 on an error. `rzm validate actions` checks the declarations. [Module actions](../../specs/technical/module-actions.md)

`rzm view eject <id>`, for example `rzm view eject group.briefing`, copies a bundled view into `.rhizome/views/` so your team can change it. `rzm validate views` checks view definitions.

## Agent-facing commands

Agents normally reach these through the installed skills; they are listed here for scripting and debugging.

- `rzm agent ontology-query-schema --type Effort --depth 1` reports a type's query fields and related types in one call, or `rzm.ontologyQuerySchema({type: "Effort", depth: 1})` in code mode. `--types` selects several types.
- `rzm agent code execute --session-id <id> --input '{"path": "Projects/Atlas.md"}' < script.js` runs a code-mode script, which reads the JSON value as `input`. `--input-file <path>` reads it from a file instead. Skills can ship such scripts to gather their context in one step.
- `rzm agent view run --omit-capabilities` reads a view's rows without its field capability catalog.
- `rzm agent next-id` and `rzm agent ontology-authoring-guide` suggest identifiers from the current ontology and shared identifier pool.
- `rzm agent current-user set "<Person title>"` and `rzm agent current-user validate` configure the local user's Person note.
- `rzm harness status --configured` reports which coding agent `agent.harness` selects, without starting it. The workspace's Agent tab sets the same value.
- `rzm agent evaluate-batch --check-credentials` reports whether Jev evaluation has a credential, without calling it or printing the key. It prints `{"configured": true|false, "source": ...}` and always exits 0.

## Diagnostics

`rzm diagnostics perf` summarizes performance. [Operational diagnostics](diagnostics.md) covers slow indexing, runtime failures, and workspace stalls.
