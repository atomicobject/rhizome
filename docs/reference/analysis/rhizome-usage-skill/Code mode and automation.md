---
type: ReferenceDoc
summary: "Evidence on Rhizome's automation layers for the usage skill: what code mode can read and write, how skill scripts, module actions, scheduled tasks, and module lifecycle build on it, tested examples, limits, and the guidance gaps downstream agents face."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Code mode and automation

This note covers the automation rungs of the customization ladder: code mode, skill scripts, module actions, scheduled tasks, and module lifecycle. It records what each does, how they stack, and what a usage skill has to teach. **Verified** means checked against code, `--help` from a fresh v0.52.1 build of this checkout, or a run against the scratch vault described in `docs/reference/analysis/rhizome-usage-skill/Schema evolution and views for custom workflows.md`. **Inference** means reasoned from specs and docs.

## How the layers stack

| Layer | Who starts it | What runs | Writes | Declared in |
| --- | --- | --- | --- | --- |
| Code mode | An agent or a script | A JavaScript body with a connected `rzm` client | Only through a few explicit operations, with `--read-write` | Nothing; ad hoc |
| Skill script | An agent following a skill | A saved code-mode file, `rzm agent code execute --input ... < scripts/x.js` | Same as code mode | A skill's `scripts/` folder, named in `SKILL.md` |
| Module action | A view, an agent, or a person | A command (`run.command`) or a code-mode script (`run.script`) | Whatever the command does; script actions run with `--read-write` | `.rhizome/modules/<m>/actions/<name>.yaml` |
| Scheduled task | The runtime every 15 minutes, or a person | An optional model-free check, then an agent prompt in the user's harness | Commits in a task worktree; nothing pushed | `.rhizome/tasks/*.yml` or `.rhizome/modules/<m>/tasks/*.yml` |
| Module lifecycle | The runtime on open and every two seconds | The module's own `observe`, `setup`, `index`, and `verify` actions | Setup and index work, under runtime control | `.rhizome/modules/<m>/lifecycle.yml` |

Each layer reuses the one below it: actions and task checks can be code-mode scripts, and lifecycle steps are actions.

## Code mode

**Invocation (verified, `rzm agent code execute --help`):** `--code '<body>'` or the body on stdin; `--input '<json>'` or `--input-file <path>` sets the script's `input` (null when omitted); `--read-write` allows supported explicit writes; `--timeout` sets the whole-execution deadline (default 30 s, maximum 24 h); `--session-id` reuses a session's dedupe. Node.js must be on PATH. The command prints `{ ok, result }`.

**Operations (verified, `rzm agent code surface`):** 37 operations, each callable as `rzm.<method>`:

- Typed data: `ontologyQuery`, `ontologyQuerySchema`, `ontologyReference`, `ontologyInspect`, `ontologyAuthoringGuide`, `queryRecipe`, `view`, `nextId`, `nodeLink`.
- Search and context: `semanticQuery`, `files`, `fileContext`, `vaultContext`, `findConnections`, `graphPath`, `communityList`, `listProperties`, `listTags`, `checkPaths`, `start`, `surface`.
- Code evidence: `codeSymbol`, `codeSymbolContext`, `codeReferences`, `codeRationale`, `externalReferences`, `report`.
- Health and validation: `validate`, `vaultHealth`.
- Models: `evaluate` and `evaluateBatch` (Jev through TypeSafe, sends supplied content off the machine and incurs usage); `generate` (one tool-free model call through the user's own Claude Code or Codex CLI login).
- Actions and identity: `actionList`, `actionRun`, `currentUser`.
- Mutations: `noteMove`, `noteRenameHeading` (plan only from the agent CLI).

**Writes are narrow (verified from each operation's declared effects).** Only `noteMove`, `validate` with fix apply, `currentUser` set, `view` eject, and `actionRun` change anything. There is no general "edit this note's field" operation in code mode. Bulk content changes go through the agent's own file edits, the validation fix plan, `rzm note properties rename|set|delete` on the command line, a module action, or a custom view's `setField`. A usage skill that promises "code mode can update every opportunity's stage" would be wrong.

**Tested (verified):**

```js
// rzm agent code execute --code '...'
const r = await rzm.ontologyReference({ type: "Effort" });
if (!r.ok) return r;
const t = r.payload.types[0];
return { profile: t.profile, fields: t.fields.map(f => `${f.name}:${f.kind}`) };
```

This returned `{ lifecycleField: "status", primaryDateField: "createdAt", relationFields: ["governingSpecs"], shape: "workflow", summaryField: "summary" }` for this repository's `Effort`. A second script listed the view catalog through `rzm.view({ action: "list" })` and returned `targets` and `views` with each view's `origin` and `experienceKey`. With runtime auto-start off, code mode printed `executing operations in this process (vault runtime auto-start is disabled)` on stderr and still worked.

**Rules the skill must carry (verified in `pkg/app/cli/init/templates/skills/markdown/rhizome/references/code-mode.md`):**

- Every call resolves to `{ ok, exitCode, payload, stdout, stderr, diagnostic }`. Check each operation's `ok`, not the script's outer `ok`. Invalid input and transport faults throw `CodeModeError`.
- GraphQL partial data with errors still fails the operation. Pagination lives in `payload.extensions.typedRoots[alias]` (one level deeper for recipes); follow `nextOffset` while `hasMore`.
- Each call has its own 30 s deadline including queue time; pass `{ timeoutMs }` as a second argument to raise it. Variables do not survive between executions.
- Session dedupe can withhold content already delivered to JavaScript (`contentOmittedReason: "deduped"`); reread with `dedupe: false`.
- Code mode is not a sandbox. It runs with the caller's host permissions, and in a sandboxed harness it may need loopback access to the repository's Rhizome process (`runtime_access_denied`).
- Discover inputs with `rzm agent code describe --operation <name> --schema input`. Other entry points exist: `rzm agent code serve` (JSON-RPC over stdio) and `rzm agent code generate` (a typed ESM client for integrations).

**Gap (verified):** `code-mode.md` documents Jev evaluation but not the `generate` operation, although the compounding module's scripts depend on `rzm agent generate` (`pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/SKILL.md`).

## Skill scripts

`pkg/app/cli/init/templates/skills/markdown/rhizome/references/skill-scripts.md` is the shipped guidance, and it is sound. A saved script belongs in a skill when agents would otherwise rebuild the same multi-call retrieval. The script owns paging, coverage, deduplication, and truncation; the skill's prose owns judgment. Scripts should read the type profile (`profile.lifecycleField`, `gapFields`, `summaryField`, each enum value's `stage`) rather than hardcode field names, so they survive schema edits. The reference includes a complete type-brief script.

**Scenario (inference):** "Brief me on every stalled deal" becomes a skill script that queries `Opportunity`, keeps active-stage records whose `nextCall` is empty, and returns titles, summaries, and paths with the coverage status. The skill tells the agent to run it and what to do with the result.

## Module actions (SPEC-0131)

**Declaration (verified, `pkg/vault/actiondef` and `rzm validate actions`):**

```yaml
# .rhizome/modules/sales/actions/mark-lost.yaml
description: Mark an opportunity lost with a reason
inputs:
  path: { kind: string, required: true }
  reason: { kind: string, enum: [price, timing, competitor, no-fit] }
run:
  script: scripts/mark-lost.js   # module-relative; or command: [node, "{module}/scripts/x.mjs", "op"]
timeout: 30s
```

In the scratch vault, `rzm validate actions` reported `.rhizome/modules/sales/scripts/mark-lost.js does not exist`, and `rzm sales` listed `mark-lost` under Actions beside the module's recipe.

**Contract (verified in `docs/specs/technical/module-actions.md`):**

- Input kinds are `string`, `int`, `bool`, and `list`; `enum` only on `string` and `list`. The runner rejects bad input before starting a process.
- A command gets its input as JSON on stdin and must print exactly one JSON value. Exit 0 is `ok`, exit 2 is `refused` with the last stderr line as the reason, and anything else is `error`. A script action runs through `rzm agent code execute --read-write` with `{ action, input, moduleDir, repoRoot }` as input; returning `{ refused: "<reason>" }` refuses.
- Timeout defaults to 60 s and may not exceed 5 minutes. Stdout is capped at 1 MiB. Nothing retries, and actions do not take the task lock.
- Every run checks repository trust and refuses an untrusted repository with a reason naming `rzm trust`.
- Callers: `GET /api/v1/actions` and `POST /api/v1/actions/{module}/{name}`; `useActionQuery` and `useActionCommand` in views; `actionList` and `actionRun` in code mode; `rzm <module> <action> --<input> <value>` on the command line, exiting 0, 2, or 1.
- Actions exist only inside modules, and an action may not share a name with a recipe in the same module.

**Working examples:** compounding's seven actions (`.rhizome/modules/compounding/actions/`) behind its workspace view, and AO KB's `readiness`, `refresh-bindings`, `index`, `check`, and `read-evidence` (`pkg/app/cli/init/templates/starters/ao-kb/module/actions/`).

## Scheduled tasks (SPEC-0127)

**Declaration (verified, `pkg/vault/taskdef/taskdef.go` and `rzm validate tasks`):** `name` (required), `every` (Go duration or whole days or weeks such as `7d`, `2w`), optional `description`, `scope` (`person` default, or `team`), `check` with exactly one of `command` or `script`, `prompt`, and `timeout` (applies to the check and the prompt separately). A task needs a check, a prompt, or both. Unknown fields are errors. A module task's id is `<module>/<file name>`.

This scratch-vault task validated with `rzm validate tasks`:

```yaml
# .rhizome/modules/sales/tasks/stale-deals.yml
name: Flag stale deals
description: Reviews active opportunities with no call booked
every: 7d
check:
  script: .rhizome/modules/sales/scripts/stale-check.js
prompt: |
  For each opportunity the check lists, read its note and recent meeting notes,
  then add a one-line status under "## Next step". Change no other notes.
```

```js
// .rhizome/modules/sales/scripts/stale-check.js: model-free, returns the check contract
const r = await rzm.ontologyQuery({ query: `{ opportunity(first: 200) { path stage nextCall } }` });
const stale = (r.payload?.data?.opportunity ?? [])
  .filter(o => ["qualified", "proposal"].includes(o.stage) && !o.nextCall);
return stale.length
  ? { needed: true, summary: `${stale.length} active deals have no call booked`, context: stale.map(o => o.path).join(" ") }
  : { needed: false };
```

The check script was not run as a task; `rzm validate tasks` checks the file's shape. A production check should read stages from the type profile rather than hardcode them, check `r.ok`, and page until coverage is complete.

**Behavior (verified in `docs/reference/guides/scheduled-tasks-guide.md` and `rzm tasks --help`):**

- Scheduled runs read task files from the primary branch (`tasks.primaryRef`, default `origin/HEAD`). Editing a task on a branch changes nothing until it merges. `rzm tasks run --file <path>` tests a draft in a detached capture without touching the schedule.
- While Rhizome Desktop or a headless runtime is up for a vault in a Git checkout, it runs `rzm tasks run-due` every 15 minutes. Each due task runs once per person and repository.
- Modes are `auto`, `notify`, and `off`; the repository default is `tasks.defaultMode` in `.rhizome/config.yml` (notify when unset), and `rzm tasks mode <task> auto|notify|off|default` overrides per person.
- A background run works in a worktree under `.rhizome/task-state/worktrees/` on branch `rzm/<task>/<date>` (`rzm/<module>/<name>/<date>` for module tasks), commits its changes with a `Rhizome-Task:` tag, and pushes nothing. A team-scope task's clock resets for everyone when a tagged commit reaches the primary branch.
- Checks receive `RZM_TASK_ID`, `RZM_TASK_MAIN_CHECKOUT`, `RZM_TASK_COMMIT`, and `RZM_TASK_DATA_DIR`. Tasks run only in a trusted repository. Rhizome does not meter or cap a task's model spend.

**Working example:** compounding's `mine.yml` (`.rhizome/modules/compounding/tasks/mine.yml`) runs a command check, then a prompt that runs two module scripts within a stated budget and ends with a workspace link.

## Module lifecycle

`docs/reference/module-lifecycle.md` (verified against `pkg/app/cli/init/templates/starters/ao-kb/module/lifecycle.yml`): a module can declare `title`, the four action names `observe`, `setup`, `index`, and `verify`, and one to 32 watched `inputs` with optional `excludes`. The runtime fingerprints the inputs, runs the actions serially, and reports readiness at `GET /api/v1/starter-readiness` with states `checking`, `working`, `required_input`, `blocked`, `failed`, and `ready`. AO KB is the only shipped user. **Inference:** this is an advanced, starter-author feature. A usage skill should recognize a readiness state and point at Retry or repository Settings, not teach users to write lifecycle files.

## What automation across a vault looks like

Patterns that fit the shipped primitives (**inference**, grounded in the examples above):

1. **Read and report:** a script or recipe gathers typed records, coverage, and gaps, and the agent writes prose or a note. This is the safest and most common pattern.
2. **Plan, approve, apply:** `rzm validate fix <check>` previews deterministic repairs; `--apply` applies safe ones; `--action` or `--from-plan` applies approved confirmable ones.
3. **Bulk frontmatter changes:** `rzm note properties rename|set|delete --dry-run`, then without `--dry-run` after approval. These are top-level commands, not code-mode operations.
4. **Judgment at scale:** `rzm.evaluateBatch` scores or classifies many records with Jev, with explicit thresholds and checkpoints for large runs; `rzm.generate` drafts text through the user's harness. Both send content to a provider and cost money.
5. **Buttons in a view:** a module action wraps a script so a view can run it with one click and show a refusal reason.
6. **Recurring work:** a scheduled task's check decides whether work exists, and its prompt does the work in a reviewable worktree.

## Gaps and risks for the usage skill

- **No shipped route (verified).** The `rhizome` skill's route table and references cover code mode and skill scripts but not module actions, scheduled tasks, modules, or lifecycle. The only downstream mentions of actions are in `custom-views` references. The scheduled-tasks and module-lifecycle guides live only in this repository.
- **Write expectations (verified).** Code mode's narrow write surface is stated per operation but not summarized anywhere. Users will expect "automate edits across my vault" to mean code mode.
- **Cost and trust (verified).** Tasks spend the person's model budget without a cap, actions and tasks require `rzm trust`, and both run repository code with the user's permissions. The skill should say this before installing or writing either.
- **Not exercised here:** running an action or task end to end needs `rzm trust`, which writes user-level trust state, so this research did not run one. A council that wants that evidence should run it in a disposable checkout.
