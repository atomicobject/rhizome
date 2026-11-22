# Automation

Use this route when work over the vault should repeat, run from a button, or run on a schedule. Each layer builds on the one before it; choose the lowest that fits. Every layer needs a configured repository: until `rzm init --check --json` reports `configured: true`, `rzm` reads the registered default vault, another repository, so set it up first (`references/setup-and-starters.md`).

| Layer | Fits when | Started by | Lives in |
| --- | --- | --- | --- |
| Code mode | One answer needs several calls, once | You | Nothing saved (`references/code-mode.md`) |
| Saved skill script | Agents keep rebuilding the same retrieval | An agent following a skill | The skill's `scripts/` (`references/skill-scripts.md`) |
| Module action | A person, a view, or an agent runs a named operation with typed inputs | `rzm <module> <action>`, a view button, or `rzm.actionRun` | `.rhizome/modules/<module>/actions/` |
| Scheduled task | Agent work recurs when a check says it is needed | The Rhizome runtime, per person | `.rhizome/tasks/` or a module's `tasks/` |

Code mode reads broadly and writes narrowly: only operations whose contract lists a write effect change anything (`rzm agent code describe --operation <name>`), and none edits a note's fields in bulk. For bulk property changes use `rzm note properties rename|set|delete` with `--dry-run` first. For other edits, edit the files and validate, or put the logic in an action's command.

## Say what it costs and runs first

Actions and tasks are repository code that runs with each person's full authority, unsandboxed, on every teammate's machine. Both refuse to run until the person trusts the repository with `rzm trust`, which needs their own interactive terminal. A task's prompt spends the model budget of whoever's machine runs it, and Rhizome sets no cap. Operations that call a provider (`semanticQuery`, `evaluate`, `evaluateBatch`, `generate`) send content off the machine. State these before writing an action or task, and get the person's agreement to the schedule and spend.

## Module layout

A module is a folder; its name is its id (`^[a-z0-9][a-z0-9-]{0,62}$`, not the name of an `rzm` command). It has no manifest.

```text
.rhizome/modules/<module>/
  ontology/*.graphql   schema
  recipes/*.yaml       saved queries, each also runnable as rzm <module> <recipe-id>
  views/               view definitions and custom view folders
  actions/<name>.yaml  module actions
  tasks/<name>.yml     scheduled tasks, id <module>/<name>
  scripts/             files the actions and tasks run
```

Skills stay in the harness skill folders, not in the module. `rzm validate modules` checks names, and `rzm <module>` lists the module's recipes and actions.

## Declare an action

```yaml
# .rhizome/modules/pipeline/actions/stalled.yaml
description: List active deals with no call booked
inputs:
  limit: { kind: int }
run:
  script: scripts/stalled.js
timeout: 30s
```

- `description` and `run` are required. Unknown keys are errors.
- `inputs` are named in lowercase words joined by hyphens, each with a `kind` of `string`, `int`, `bool`, or `list`, optional `required`, and optional `enum` on `string` and `list`. Input that does not match is rejected before anything runs.
- `run` holds exactly one of `script` (a module-relative code-mode script) or `command` (an argument list; `{module}` expands to the module folder).
- `timeout` defaults to 60 seconds and cannot exceed 5 minutes.

A script action runs through read-write code mode with `input` set to `{ action, input, moduleDir, repoRoot }`, so the caller's values are at `input.input`. Whatever it returns is the result; returning `{ refused: "<reason>" }` refuses with that reason. A command receives the input as JSON on stdin and prints one JSON value: exit 0 is ok, exit 2 is refused with the last stderr line as the reason, anything else is an error.

```js
// .rhizome/modules/pipeline/scripts/stalled.js
const { input: args } = input ?? {};
const found = await rzm.ontologyQuery({ query: `{ deal(first: 200) { title path stage nextCall } }` });
if (!found.ok) return found;
const stalled = found.payload.data.deal.filter(d => d.stage === "proposal" && !d.nextCall);
return { stalled: stalled.slice(0, args?.limit ?? 20), total: stalled.length, coverage: found.payload.extensions?.typedRoots?.deal };
```

The type and field names come from the pipeline example in `references/customization.md`; a real script reads stages from the type profile and pages to full coverage (`references/skill-scripts.md`).

## Declare a scheduled task

```yaml
# .rhizome/modules/pipeline/tasks/stalled-deals.yml
name: Flag stalled deals
description: Notes a next step on active deals with no call booked
every: 7d
check:
  script: .rhizome/modules/pipeline/scripts/stalled-check.js
prompt: |
  For each deal the check lists, read its note and add one line under
  "## Next step" saying what is blocking the next call. Change no other notes.
```

- `name` and `every` are required; `every` is a duration such as `12h`, `7d`, or `2w`. Unknown keys are errors.
- A task needs a `check`, a `prompt`, or both. `check` holds exactly one of `command` or `script`, with the script path relative to the repository root. `scope` is `person` (the default) or `team`; `timeout` defaults to one hour.
- A check makes no model calls and returns `{ needed: false }` or `{ needed: true, summary: "...", context: "..." }`. The prompt runs only when needed, with `context` passed to it. Put the filtering in the check so the prompt spends tokens only on real work.

How a task runs shapes how to write one:

- Scheduled runs read task files from the primary branch. A task edited on a branch does nothing until it merges.
- Each task runs once per person and repository while that person's Rhizome app or runtime is up. The repository default is to notify the person; `tasks.defaultMode` in `.rhizome/config.yml` sets it, and `rzm tasks mode <task> auto|notify|off|default` is each person's override.
- By default a background run works in its own worktree and commits there; the person reviews the branch and opens the pull request. A task that declares `writes: [<glob>, ...]` runs in the open checkout instead and commits only its changed files that match, leaving the person's own changes alone and undoing changes under `.agents/` or `.rhizome/` that no pattern names. Declare the narrowest `writes` that covers the work: widening it later makes the task wait for each person's `rzm tasks approve <task>`. Pushing happens only when `tasks.allowPush` and the person both allow it. Write the prompt to end with a message saying what changed.

## Write for unattended runs

No one is there to answer a question or approve a prompt, so remove the guesses in advance:

- Discover each operation's input shape once with `rzm agent code describe --operation <name> --schema input`, then fix it in the saved script. A script that rediscovers contracts every run pays for it every run.
- Reuse one session per run, and pass arguments through `--input`.
- Return coverage, warnings, and truncation with the data. When an optional service fails, such as a provider key or a degraded search lane, finish on the reduced path and say in the result which path ran. Stop only when the missing piece is the work itself.
- Name in the task prompt exactly which notes or folders the agent may change.

## Verify by running it once

1. `rzm validate actions` or `rzm validate tasks` reports declaration problems without running anything.
2. Run the script directly, which needs no trust: `rzm agent code execute --input '{"input": {"limit": 5}}' < .rhizome/modules/pipeline/scripts/stalled.js`.
3. After the person trusts the repository, run the real thing: `rzm pipeline stalled --limit 5`, or `rzm tasks run --file .rhizome/modules/pipeline/tasks/stalled-deals.yml`, which runs a draft once without touching the schedule.
4. Read the result or the run's worktree with the person before the task merges to the primary branch.

Done when one real run produced the intended result and the person has seen what it changed and what it cost.
