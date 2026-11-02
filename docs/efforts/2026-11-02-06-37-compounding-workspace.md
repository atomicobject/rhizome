---
type: EffortNote
id: EFF-2026-11-02-06-37
aliases: [EFF-2026-11-02-06-37]
name: Module actions, view parameters, and the compounding workspace
created-at: 2026-11-02T11:37:09Z
status: active
plan-approved-by: Drew Colthorp
summary: "Give modules typed actions that views, agents, and the command line run through one runner; let views take identity and UI parameters; and ship the compounding workspace (Recommendations, Health, Memory) that turns a checklist into one agent prompt."
governing-specs: ["[[module-actions]]", "[[unified-view-contract]]", "[[custom-view-apps]]", "[[session-memory-starter]]"]
---

# Module actions, view parameters, and the compounding workspace

## Scope

Slices 3, 4, and 5 of the compounding-workspace plan (revision 7), with their documentation and the end-to-end proof. It builds on [[rhizome-modules|SPEC-0130]] (#64) and the compounding mine task (EFF-2026-11-01-16-54, #66).

Excluded:
- per-item approve buttons in the workspace;
- a Runs tab (mine runs link to the Tasks page);
- the code-mode client loader (plan option B);
- sidebar grouping of a module's views;
- actions outside modules.

## Spec Set (Frozen)

- [[module-actions|SPEC-0131]], new.
- [[unified-view-contract|SPEC-0110]], amended for view parameters.
- [[custom-view-apps|SPEC-0105]], amended for the action hooks and parameter APIs.
- [[session-memory-starter|SPEC-0124]], amended for the workspace.

## Stories In Scope (Frozen)

- SPEC-0131 US1–US3.
- The SPEC-0110 view-parameters story added by this effort.
- The SPEC-0105 kit criteria for `useActionQuery`, `useActionCommand`, `useViewParams`, `setViewParams`, and `openView(id, context, params)`.
- The SPEC-0124 workspace story added by this effort.

## Plan Approval

Drew Colthorp approved plan revision 7 in chat on 2026-11-01 and asked on 2026-11-02 to "do your 1-4" with subagents, stopping at a ready pull request.

Decisions taken under that approval:
- Every action requires a trusted repository, as recommended.
- The mine task stays in notify mode, as #66 shipped it.
- All of slices 3–5 go in one pull request. Separate pull requests would stack, and none could merge while this effort runs.

## Contracts Fixed by This Plan

### Actions (SPEC-0131)

- **Declaration and id:**
  - Each action is `.rhizome/modules/<m>/actions/<name>.yaml` with id `<m>/<name>`.
  - Fields: `description`, `inputs` (`string|int|bool|list`, `required`, `enum`), `run.command` or `run.script`, and `timeout` (default 60 s, at most 5 min).
- **Command form:**
  - `{module}` expands to the module folder relative to the repository root.
  - The command runs in the serving checkout's root, with the input JSON on stdin.
  - Exit 0 with one JSON value is ok. Exit 2 is refused, with the last stderr line as the reason. Anything else is an error.
- **Script form:** runs through `rzm agent code execute` with input `{action, input, moduleDir, repoRoot}`. A returned `{refused: "<reason>"}`, with no other keys, is refused.
- **Envelope:** `{status: ok|refused|error, result?, reason?, stderrTail?}`.
- **Trust:** the tasks trust check runs first. An untrusted repository gets refused with a reason naming `rzm trust`.
- **HTTP:**
  - `GET /api/v1/actions` returns `[{id, module, name, description, inputs}]`.
  - `POST /api/v1/actions/{module}/{name}` takes the input object as its body. It answers 200 with the envelope, 400 for invalid input, and 404 for an unknown action. It sits behind the mutation-origin guard.
- **Agents:** `action_list` and `action_run({id, input})` in the agent catalog.
- **CLI:** `rzm <m> <action> --<input> v` (a list repeats its flag). It exits 0 for ok, 2 for refused, and 1 for an error.
- **Install and validate:**
  - `actions` is an installed asset type.
  - `rzm validate actions` is part of `validate all`.

### Kit (SPEC-0105)

- **`useActionQuery(id, input, options?)`:**
  - A TanStack query keyed by `["action", id, input]` that polls every 15 s while the document is visible.
  - `data` is the ok `result`.
  - A refused or error envelope rejects with an `ActionError` carrying `status`, `reason`, and `stderrTail`.
- **`useActionCommand(id)`:**
  - A TanStack mutation whose `mutateAsync(input)` resolves to `result` and rejects the same way.
  - On ok it invalidates every `["action", …]` query once.
  - It never caches.

### View parameters (SPEC-0110)

- **Declaration:** a view definition (configured or custom) may declare `params: [{name, kind: string|int|bool, default?, identity?: bool}]`. `identity` defaults to false.
- **Where the values live:**
  - On the view's URL query, with an unprefixed name per param.
  - In `openView(id, context, params?)`.
  - In `useViewParams()`, which returns the declared params with defaults applied. Undeclared query keys are ignored.
- **Setting values:** `setViewParams(patch)` sends `params.changed` to the host.
  - A UI param pushes a history entry, and the frame does not remount.
  - An identity param is part of the invocation, so changing it remounts.
- **History:** Back and Forward compare identity params. When they changed, the frame remounts. Otherwise the host sends the UI params to the same frame, and `useViewParams()` updates.
- **Standalone pages** read and write their own URL query with the same rules.

### Compounding workspace (SPEC-0124)

- **Actions** in `.rhizome/modules/compounding/actions/`, each a `command` over one entry `scripts/workspace.mjs <op>` that reads stdin:
  - `recommendations`: pending proposals and open findings, each with its reference and details;
  - `item`: one reference in full;
  - `prompt`: input `items` (list of `proposal:<cluster>` or `finding:<path>`), returns `{prompt}`;
  - `dismiss`: input `cluster` and `reason`, dismisses locally;
  - `hide`: input `path`, hides a finding locally;
  - `health`: `report.mjs` as JSON;
  - `shards-check`: `shards.mjs check`.
- **The prompt's wording ships in the module.** It says the person reviewed these items in the workspace and approves publishing the proposals. It then asks the agent to:
  - approve each proposal as drafted with the compounding skill;
  - apply document edits;
  - implement the selected fixes and guards;
  - check off the action items it completes;
  - finish on one branch ready for one pull request.

  It lists references, not content.
- **Views:**
  - `workspace`, with tabs Recommendations, Health, and Memory. `tab` is a UI param. `item` is an identity param that opens an item expanded. Memory reuses the starter's configured shard and finding views.
  - `compounding-item`, which takes an identity param `item`.
- **The skill's review step** accepts the prompt's statement as the publication confirmation. Refusals still come back to the person one at a time.

## Plan

1. **C0 (lead):** this effort, SPEC-0131.
2. **C1, parallel lanes** in separate worktrees, each with focused tests and `make check-fast`:
   - **A, actions platform (Go):** runner shared with task checks, declarations, HTTP routes, agent operations, module CLI, validation, the installed asset type, subsystem notes.
   - **B, kit and view parameters (web, view definitions):** the action hooks over the HTTP contract, parameters in definitions, host, kit and standalone pages, SPEC-0110 and SPEC-0105 amendments, custom-views skill sections.
   - **C, compounding workspace (module):** `workspace.mjs`, the action declarations, the workspace and item views, the skill and guide, and the SPEC-0124 amendment.
3. **C2 (lead):** merge lanes, `make build`, `rzm init --check`, `make check`, `./scripts/rzm validate`, `make web-e2e`, and the end-to-end journey in the browser and in a desktop build.
4. **C3:** sol-loop review, pull request, Greptile to 5/5, CI green, closure.

## Spec Coverage Checklist

- [ ] SPEC-0131 US1–US3
- [ ] SPEC-0110 view parameters
- [ ] SPEC-0105 kit hooks and parameter APIs
- [ ] SPEC-0124 workspace

## Original Intended Delivery

Slices 3–5 with docs and the end-to-end proof, in one pull request.

## Actual Delivered

In progress.

## Deviations

- **No link on the task item.** The Tasks page shows a task item's summary as plain text, so the mine task item cannot link to the workspace. The mine run's final message, which the Tasks page renders as Markdown, ends with the link instead.
- **No direct MCP tool.** `action_list` and `action_run` are code-mode operations; `rzm mcp` reaches them through code mode only (SPEC-0131 non-goal).
- **Desktop clipboard check is left to Drew.** The harness cannot drive the Desktop webview, so Copy prompt in Rhizome Desktop is a human check after merge.

## Closure Checklist

- [ ] Every story delivered or covered by a deviation
- [ ] Gates recorded
- [ ] Pull request number recorded

## Compounding Follow-ups

- Copy prompt in Rhizome Desktop (clipboard access in its webview), checked by a person after merge.
- Switching Notes tabs rebuilds the URL from tab state, so a tab's view params are lost when the person leaves it and comes back; two custom frames visible side by side read the same query.
- A module action input named `help`, `vault`, or `no-pager` cannot be set from the CLI, and a string input whose value is literally `--vault=…` after `--summary` is read as the vault flag (Sol lane A S5, declined as a rare case).
- A direct `rzm mcp` tool for actions.
- `rzm agent current-user set "<title>"` resolved a Person only by path in the scratch repository; not investigated.

## Status

In progress.

## Execution Notes

- 2026-11-02: Branch `feat/compounding-workspace` from `dc94d242` (#66's head; #66 awaits a required review). It is rebased onto `main` once #66 merges.
- 2026-11-02, C2 integration after merging lanes A–C:
  - Gates: `make build` passed. `rzm init` regenerated the custom-views skill copies, and `rzm init --check` then exited 0. `make check` passed (141 Go packages; 190 web test files, 1757 tests). `./scripts/rzm validate` reports 0 issues. `make web-e2e` passed 109 tests. `rzm validate frozen-scope-drift` reports one issue in EFF-2026-10-03-18-33, which this branch does not touch.
  - The workspace TSX views type-check against the kit with tsc. `rzm validate views` and `rzm validate actions` were clean in a scratch repository.
  - Fixed: the Notes shell could not open a `mount.hidden` view. The catalog leaves hidden views out, so `openView("compounding-item", …, {item})` set `item` on the workspace's own URL instead. The tab now loads such a view by id.
  - The browser journey passed in a scratch repository with seeded proposals and a teammate's finding: refusal before trust, the checklist, the prompt, remove and hide, Health to the item view, Memory, history, and the link. There were no console errors.
