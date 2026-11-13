---
type: TechnicalSpec
id: SPEC-0131
aliases:
  - SPEC-0131
  - Module actions
summary: "An action is a named, typed operation a module declares in .rhizome/modules/<name>/actions/. One runner executes it as a command or a code-mode script with bounded time and output and returns one envelope: ok, refused, or error. Views call it over HTTP through two kit hooks, agents through the action_list and action_run operations, and people through rzm <module> <action>. Every run requires a trusted repository."
spec-status: active
last-updated: 2026-11-12
---

# Module actions

## Summary

A custom view runs in the browser and has no shell, so a view that needs to do something a script knows how to do (copy a prompt built from private state, dismiss a proposal, render a report) has no way to ask for it. Agents can run the script directly, but each caller would then own its own login-PATH lookup, timeout, process cleanup, output limit, and result shape.

An **action** is a named operation a [[rhizome-modules|SPEC-0130]] module declares, with typed inputs and one way to run. One runner owns the process mechanics, which it shares with [[scheduled-tasks|SPEC-0127]] task checks, and returns one envelope. Views reach actions over HTTP through two [[custom-view-apps|SPEC-0105]] kit hooks, agents through the agent catalog, and people through the module command line.

Actions are not a sandbox. Views already run same-origin with every route, and an action does only what its module's script does when an agent runs it.

## Goals

- Let a module expose an operation once and have views, agents, and the command line call it the same way.
- Keep authoring to one YAML file beside the script it runs.
- Reuse the task check runner's process handling rather than writing a second one.

## Non-Goals

- Sandboxing or per-action permissions beyond repository trust.
- Retries, queues, background runs, or progress streaming. An action runs once and returns.
- Actions outside modules; the flat `.rhizome/` folders get no `actions/`.
- Merging actions with query recipes or task declarations.
- A direct `rzm mcp` tool for actions, for now.

## Declaration

`.rhizome/modules/<module>/actions/<name>.yaml` declares one action. Its id is `<module>/<name>`, from the folder and file name.

```yaml
description: Build one agent prompt for the selected recommendations
inputs:
  items: { kind: list, required: true }
  reason: { kind: string, enum: [not-relevant, wrong, already-fixed] }
run:
  command: [node, "{module}/scripts/workspace.mjs", "prompt"]
timeout: 30s
```

- `description` is required.
- `inputs` maps each input name to a `kind` (`string`, `int`, `bool`, or `list` of strings), with optional `required` and, for `string` and `list`, an `enum`. Input names are lowercase words joined by hyphens.
- `run` holds exactly one of:
  - `command`: an argument vector. `{module}` expands to the module folder relative to the repository root, as in hook commands. The first word resolves through the login shell's PATH. It runs in the repository root of the checkout serving the vault, with the input JSON on stdin, which is then closed.
  - `script`: a module-relative path to a code-mode script. It runs through `rzm agent code execute --read-write` against the serving vault, so it may write as a command action can, with `{action, input, moduleDir, repoRoot}` as its input, where both directories are absolute.
- `timeout` defaults to 60 s and may not exceed 5 minutes.

An action and a query recipe in one module may not share a name.

## Running

The runner validates the input against the declaration. It rejects unknown names, wrong kinds, missing required inputs, and values outside an enum. It then starts the process in its own process group with the login PATH, kills the whole tree when the process ends or the timeout passes, and keeps at most 1 MiB of stdout. Stderr is kept as a bounded tail. Nothing retries.

Every run first checks repository trust with the same check scheduled tasks use. In an untrusted repository the run returns `refused`, and its reason names `rzm trust`. Actions never take the task run lock; a script that needs mutual exclusion takes its own lock.

### Envelope

```json
{ "status": "ok", "result": { } }
{ "status": "refused", "reason": "mining in progress" }
{ "status": "error", "reason": "exit status 1", "stderrTail": "…" }
```

- **Command:** exit 0 with exactly one JSON value on stdout is `ok`, and that value is `result`. Exit 2 is `refused`, with the last non-empty stderr line as `reason`. Any other exit, a timeout, or output that is not one JSON value is `error`.
- **Script:** a returned value is `ok`, unless it is an object whose only key is `refused` holding a string, which is `refused` with that reason. A thrown error is `error`, with code mode's message as `reason`.

## Callers

- **HTTP.** `GET /api/v1/actions` lists every action: id, module, description, and inputs. `POST /api/v1/actions/{module}/{name}` takes the input object as its JSON body and answers 200 with the envelope for every outcome a run reaches, including `refused` and `error`. It answers 400 for invalid input and 404 for an unknown action. The route sits behind the same mutation-origin guard as other writes.
- **Agents.** The agent catalog registers `action_list` and `action_run({id, input})`, which return the same list and envelope. They are available to code mode and listed by `rzm agent code surface`.
- **Command line.** `rzm <module> <action> --<input> <value>` runs an action. A `list` input repeats its flag. `rzm <module>` lists the module's actions beside its recipes. It prints `result` as JSON and exits 0 for `ok`, prints the reason and exits 2 for `refused`, and exits 1 for `error`.
- **Views.** The kit's `useActionQuery(id, input)` and `useActionCommand(id)` (SPEC-0105) call the HTTP route.

## Install and validate

Starters install `actions` as a managed asset type, refreshed and removed by `rzm init` like views. `rzm validate actions`, which is part of `rzm validate all`, reports:

- unreadable declarations;
- a missing description;
- an unknown input kind;
- an enum on a kind that cannot have one;
- `run` with zero forms or two;
- a script or command path under `{module}` that does not exist;
- a timeout over the limit;
- an action named like a recipe in the same module.

## User Stories

### US1 - A view runs a module's operation

- id:: ^SPEC-0131-US1
- summary:: A custom view calls a script its module ships through useActionCommand and useActionQuery and receives one envelope, and an untrusted repository refuses every action.
- status:: draft

As a custom view author, I want to call a script my module ships, so that a view can do what the browser cannot.

#### Acceptance Criteria

- A view's `useActionCommand("<module>/<name>")` runs the declared command with its input on stdin and receives the envelope.
- `useActionQuery` polls while the view is visible, and a successful command refreshes the view's action queries once.
- In an untrusted repository every action returns `refused` with a reason naming `rzm trust`.

### US2 - Agents and people reach the same actions

- id:: ^SPEC-0131-US2
- summary:: Agents run actions through action_list and action_run, and people through rzm <module> <action>, with the same envelope the HTTP route returns.
- status:: draft

As an agent or a person at the terminal, I want the actions a view uses, so that I can check or repeat what the view did.

#### Acceptance Criteria

- `rzm agent code surface` lists `action_list` and `action_run`, and `action_run` returns the HTTP route's envelope.
- `rzm <module> <action> --<input> <value>` runs the action and exits 0, 2, or 1 for ok, refused, or error.

### US3 - Declarations fail early

- id:: ^SPEC-0131-US3
- summary:: rzm validate actions reports bad declarations, and the runner rejects mismatched input and kills a timed-out process tree.
- status:: draft

As a module author, I want mistakes reported before anyone runs the action.

#### Acceptance Criteria

- `rzm validate actions` reports a bad input kind, a missing script, a timeout over the limit, and a name shared with a recipe.
- The runner rejects input that does not match the declaration, without starting a process.
- A timeout kills the action's whole process tree and returns `error`.

## Requirements

- MUST check repository trust before every run and refuse an untrusted repository with a reason naming `rzm trust`.
- MUST validate input against the declaration before starting a process.
- MUST run commands and task checks through one shared runner that bounds time and output and kills the whole process tree.
- MUST return the same envelope to views, agents, and the command line.
- MUST NOT take the task run lock.
