---
type: ProductSpec
summary: "Defines how rzm init resolves agent surface integration modes across detection, explicit flags, shared AGENTS.md guidance, .agents skills, interactive setup, and CI runs."
id: SPEC-0045
spec-status: active
last-updated: 2026-10-21
aliases:
  - SPEC-0045
  - agent-surface-integration-modes
code-anchors:
  go:
    - label: init.agent_surface_detection
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.detectAgentHarnesses
    - label: init.agent_surface_mode_resolution
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.applyAgentOverrides
    - label: init.agent_surface_onboarding
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.applyAgentOnboardingChoice
    - label: init.agent_surface_rendering
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.applyAgentSurfacesWithUpdaterOptions
    - label: init.agent_surface_run_pipeline
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.Run
---

# Agent Surface Integration Modes

## Summary

`rzm init` sets up Rhizome guidance in the agents people actually use without letting detection, reruns, or CI rewrite unrelated surfaces. People choose agents by name (Claude Code, Codex, Cursor) in the first-run summary, the settings checklist, or `--agents`; the shared `AGENTS.md` and `.agents/skills` surfaces follow automatically. Stored preferences in `.rhizome/config.yml` keep the per-surface `auto|on|off` contract: `auto` inherits stored preference or detected harnesses, `on` forces a surface to participate, and `off` is an explicit opt-out that wins over detection and shared defaults.

This spec narrows the product behavior inside [[init-starter-workflow]] and the template mechanics inside [[init-template-architecture]]: it defines which agent surfaces are considered enabled before the existing rendering pipeline writes managed docs, commands, prompts, and skills. The source family for command and prompt artifacts remains owned by [[init-template-architecture#^SPEC-0039-US1-AC1]].

## Goals

- make `auto|on|off` mode semantics predictable across fresh init, reruns, and CI
- distinguish detected harnesses from forced harnesses without hiding the final enabled surface set
- make `AGENTS.md` the default shared instruction surface whenever any agent integration is enabled, unless the user explicitly disables it
- make `.agents/skills` the shared team skill surface that can be enabled by default while still honoring explicit opt-out
- guarantee disabled surfaces stay untouched even when other surfaces are refreshed
- make an enabled Rhizome agent surface self-verifying: lean routing guidance and the `rhizome` skill must agree about whether integration is active

## Non-Goals

- changing runtime behavior beyond documenting the current contract
- replacing [[init-starter-workflow]] as the broad `rzm init` product workflow
- replacing [[init-template-architecture]] as the source-family and managed-block technical contract
- defining every command, prompt, rule, or skill template emitted for each harness
- deleting or cleaning user-owned agent files when a surface is disabled
- making the base Rhizome skill own starter-specific development workflows

## User Stories

### US1 - Resolve each agent integration from detection, stored preferences, and explicit flags so the final enabled surfaces are explainable
- id:: ^SPEC-0045-US1
- summary:: Resolve each agent integration from detection, stored preferences, and explicit flags so the final enabled surfaces are explainable.
- status:: ready

Mode resolution should be boring to predict. Detection is a recommendation, stored config is preference memory, and explicit flags are the strongest one-run signal.

Code anchors: `init.agent_surface_detection`, `init.agent_surface_mode_resolution`, `init.agent_surface_run_pipeline`.

#### Acceptance Criteria

- `auto` inherits stored preference first, then detected harness presence.
  verification:: go test ./pkg/app/cli/init -run 'TestRun_ForceEnableAgentsCreatesArtifacts|TestRun_ForceDisableSkipsArtifacts|TestRun_InteractiveFirstRun_EnablesSharedAndDetectedAgents'

  - For each surface, an empty mode or `auto` means "use the stored preference if one exists; otherwise use detection."
  - Detection treats `.cursor` as Cursor, `.codex` as Codex, `.claude` or `CLAUDE.md` as Claude, `.agents` as shared agent skills, and `AGENTS.md` as the shared agent document.
  - On a first run, detection also treats a `claude`, `codex`, or `cursor` command on `PATH` as a signal for that harness. Home configuration directories are not a signal because they outlive uninstalled tools. A harness enabled this way is stored as `on` so later runs do not depend on the machine.
  - `auto` flags do not overwrite stored `agents:` preferences in `.rhizome/config.yml`.
- `on` forces participation even when the harness was not detected.
  verification:: go test ./pkg/app/cli/init -run TestRun_ForceEnableAgentsCreatesArtifacts

  - A surface set to `on` is enabled for the current run even if its harness directory or doc did not exist before init.
  - Forced harnesses may cause their target directories or files to be created by the normal template renderer.
  - Forced participation still flows through managed fences, Rhizome-prefixed artifacts, and diff/rejection handling defined by [[init-template-architecture]].
- `off` is an explicit opt-out and wins over detection, stored defaults, and implied shared surfaces.
  verification:: go test ./pkg/app/cli/init -run 'TestRun_ForceDisableSkipsArtifacts|TestRun_InteractiveFirstRun_CanDeclineAgentSetup'

  - A surface set to `off` is disabled for the current run even if detection found that harness.
  - `agentsmd: off` disables the shared `AGENTS.md` fallback; it must not be recreated merely because another agent surface is enabled.
  - `agent-skills: off` disables the shared `.agents/skills` fallback; it must not be recreated merely because another agent surface is enabled.
  - Invalid stored mode values fail fast with an `auto|on|off` error instead of falling back silently.
- `--agents` names the complete harness set for one run. ^SPEC-0045-US1-AC4
  - `--agents claude,codex,cursor` (any subset) stores the named harnesses as `on`, the unnamed ones as `off`, and the shared surfaces as `on`.
  - `--agents none` stores every surface as `off`.
  - An unknown agent name fails before any write and lists the accepted names.

### US2 - Receive the shared instruction and skill surfaces that make agent workflows portable across harnesses, while leaving disabled harnesses alone
- id:: ^SPEC-0045-US2
- summary:: Receive the shared instruction and skill surfaces that make agent workflows portable across harnesses, while leaving disabled harnesses alone.
- status:: ready

Rhizome should prefer a team-wide instruction and skill surface when agent help is enabled. Harness-specific folders are adapters; `AGENTS.md` and `.agents/skills` are the shared baseline.

Code anchors: `init.agent_surface_mode_resolution`, `init.agent_surface_rendering`.

#### Acceptance Criteria

- Any enabled agent surface implies `AGENTS.md` unless `AGENTS.md` is explicitly disabled.
  verification:: go test ./pkg/app/cli/init -run 'TestRun_ForceEnableAgentsCreatesArtifacts|TestApplyAgentSurfaces_SkipsAllFilesWhenAgentsDisabled'

  - If Cursor, Claude, Codex, `.agents/skills`, or existing `AGENTS.md` is enabled, init treats `AGENTS.md` as enabled unless `agentsmd` resolves to `off`.
  - Enabled `AGENTS.md` receives the core Rhizome managed block and any active workflow-starter managed blocks described by [[init-starter-workflow#^SPEC-0038-US2-AC2]].
  - User-authored prose outside managed fences remains preserved by the existing managed-block updater.
- `.agents/skills` is the shared team skill surface and is enabled by default when agent setup is accepted.
  verification:: go test ./pkg/app/cli/init -run 'TestRun_InteractiveFirstRun_EnablesSharedAndDetectedAgents|TestApplyAgentSurfaces_OpenAgentSkillsIncludeBundledWhenTemplateSelected'

  - Accepting first-run agent setup enables `.agents/skills` and `AGENTS.md` as the shared team surfaces.
  - If any agent surface is otherwise enabled and `agent-skills` is not explicitly `off`, init enables `.agents/skills` so core and starter skills have a portable home.
  - Starter-selected skills are installed into `.agents/skills` only when the starter is active, matching [[init-starter-workflow#^SPEC-0038-US2-AC3]].
- Disabled surfaces stay untouched while enabled surfaces refresh.
  verification:: go test ./pkg/app/cli/init -run TestRun_ForceDisableSkipsArtifacts

  - A disabled harness receives no new Rhizome command, prompt, rule, doc, or skill files during that run.
  - Disabling one surface does not prevent enabled surfaces from refreshing their managed Rhizome artifacts.
  - Init does not delete or rewrite user-owned files merely because their surface resolves to disabled.

### US3 - Use the same mode contract in prompts, reruns, and non-interactive automation without surprise writes
- id:: ^SPEC-0045-US3
- summary:: Use the same mode contract in prompts, reruns, and non-interactive automation without surprise writes.
- status:: ready

Interactive setup should show the user what will happen. CI and scripted runs should get deterministic behavior from flags and stored config, where `rzm init` without a terminal applies the recommended agent onboarding defaults that `--check` reports.

Code anchors: `init.agent_surface_onboarding`, `init.agent_surface_run_pipeline`.

#### Acceptance Criteria

- First-run interactive setup shows the agents it will set up before writing and records the confirmed set.
  verification:: go test ./pkg/app/cli/init -run 'TestRun_InteractiveFirstRun'

  - The first-run summary names the enabled harnesses; the confirmation covers them, and `e` opens the agents checklist before anything is written.
  - Confirming records `.agents/skills: on` and `AGENTS.md: on`, keeps repository-detected Cursor/Claude/Codex surfaces in `auto`, and stores machine-detected ones as `on`.
  - Choosing "stop managing agent files" records Cursor, Claude, Codex, `.agents/skills`, and `AGENTS.md` as `off`, and no agent surfaces are written.
- Non-interactive acceptance uses deterministic defaults that are safe for CI.
  verification:: go test ./pkg/app/cli/init -run 'TestRun_ForceEnableAgentsCreatesArtifacts|TestRun_ForceDisableSkipsArtifacts'

  - `rzm init` without a terminal on a fresh repo applies the first-run agent onboarding default: shared `.agents/skills` and `AGENTS.md` are enabled.
  - CI can force the exact harness set with `--agents`.
  - `--agents none`, or a stored `off`, remains the way CI prevents template writes for a surface even when harness markers exist in the repository.
- Reruns honor the resolved surface set before template rendering.

  - Reruns apply stored `agents:` preferences plus any explicit `--agents` choice before rendering agent docs or template scaffolds.
  - A rerun that changes no setting leaves `.rhizome/config.yml` untouched while enabled managed docs and templates refresh.
  - If the resolved surface set is empty, the agent-surface renderer performs no writes.

### US4 - Receive a coherent Rhizome-integrated agent experience
- id:: ^SPEC-0045-US4
- summary:: An enabled agent surface receives lean routing guidance and the installed `rhizome` skill together, and reports clearly when either integration layer is missing.
- status:: ready

#### Acceptance Criteria

- The core managed block routes work meeting SPEC-0080's named conditions (a documented boundary or contract, needed rationale, a note or heading mutation, a durable-knowledge change) and explicit Rhizome work to the `rhizome` skill; a local edit whose effect is visible in the diff stays lightweight. Risky Markdown moves/renames and structured-note work remain hard routes to that guidance, and unavailable integration is reported at the failed layer. ^SPEC-0045-US4-AC1
- When creating or revising a skill, the managed block asks whether Rhizome capabilities would materially help only when the answer is not already explicit; an opt-in composes `rhizome` with available general skill-authoring guidance. ^SPEC-0045-US4-AC2
- If the managed block is present but the `rhizome` skill cannot be loaded, the agent tells the user Rhizome integration is incomplete and routes to installation/integration repair instead of silently imitating it. ^SPEC-0045-US4-AC3
- Fresh init and refresh tests verify that each enabled skill surface receives `rhizome`; when a managed-doc surface is enabled while skill surfaces are explicitly disabled or missing, the block reports integration as incomplete rather than writing into the disabled surface. ^SPEC-0045-US4-AC4

### US5 - Let starters deliver context through agent hooks that Rhizome installs and dispatches
- id:: ^SPEC-0045-US5
- summary:: A starter declares the agent hooks it needs; when the person turns hooks on, init installs one Rhizome dispatcher entry per hook event in each enabled harness, and the dispatcher runs the handlers of enabled starters and returns their context to the agent.
- status:: ready

Claude Code and Codex run project hooks around tool calls and accept context from them ([Claude Code hooks](https://code.claude.com/docs/en/hooks), [Codex hooks](https://learn.chatgpt.com/docs/hooks)). Starters should not each write their own entries into harness settings, parse host payloads, or solve deadlines and Windows launching. Rhizome owns the harness side once; a starter supplies only a handler that turns a normalized event into context. Handler declarations come only from the starter registry built into Rhizome, not from repository files. Rhizome installs one fixed entry per hook point and aggregates every starter's handlers behind it. Entries carry no tool filter, so enabling, disabling, or updating a starter never changes them, and Codex asks for trust again only when a Rhizome release adds a hook point. Turning hooks on authorizes the handlers of enabled starters, including later starter updates; a host's hook trust covers the entry, not the handler's contents. Session memory ([[session-memory-starter|SPEC-0124]]) is the first starter with handlers, and other templates, including core, are expected to follow. Core's first candidate is a reminder to start a new Rhizome session after a compaction or clear; it waits until [[agent-start-performance#^SPEC-0082-US12]], which makes session dedupe announce what it withheld, shows whether agents still need it.

#### Acceptance Criteria

- A starter declares hooks in its `template.yaml`: an ID, the events it handles (session start, subagent start, after a tool call; the after-compaction hook point is the dispatcher's own), the tool classes it handles (read, edit, shell, patch), the handler command as an argument list relative to the repository root, and a timeout. A handler shipped in a starter skill names that skill and writes `{skill}` for its folder, which resolves to the first enabled skill folder that holds it (`.agents/skills`, then `.claude/skills`); when no enabled folder holds it, the handler does not run, even if a disabled folder keeps an old copy, and status says so. The handler declaration lives in the registry built into Rhizome. An ejected starter keeps its handler, now running team-owned files, including after it leaves the selection with its files retained, and status names it. ^SPEC-0045-US5-AC1
- Hooks are off until the person turns them on. When an enabled starter declares hooks, the first run and reruns name the starters that would use them and what each delivers, and `--hooks on|off` sets the choice. The choice is stored in `.rhizome/config.yml` and survives reruns, settings edits, and add-on changes. ^SPEC-0045-US5-AC2
- With hooks on, init writes into `.claude/settings.json` (Claude Code enabled) and `.codex/hooks.json` (Codex enabled) one marker-identified entry for each hook point Rhizome supports (session start, subagent start, after a tool call, and after a compaction), matching every tool. Each entry runs `rzm hook <harness> <event>` and sets the host timeout slightly above the dispatcher's deadline. Before writing entries, init checks with `rzm hook --check` that the `rzm` on `PATH`, which every entry launches, and the binary the repository selects both support `rzm hook`; otherwise it leaves the entries as they are and says why. Init preserves every other entry, rewrites nothing when the entries are current, and removes its entries when hooks are turned off, the harness is disabled, or no enabled starter declares hooks. Rhizome owns only its own marker-identified commands, never another command in the same matcher group, which init leaves in place. A Rhizome command someone edited, or one Rhizome has no record of writing, follows the ownership rule in [[init-starter-workflow]]: init replaces or removes it only after a person decides. ^SPEC-0045-US5-AC3
- `rzm hook` reads the host's payload and gives each matching handler one normalized event on standard input: harness, event, repository root, the agent's identity (canonical worktree, harness, session, agent ID when the host gives one, and turn when it gives one; Claude Code gives an agent ID only to subagents and no turn, and Codex gives a turn and, when present, an agent ID), whether the host reset that context (compact or clear), the tool class, the repository-relative paths the call read or edited, and the shell command text. ^SPEC-0045-US5-AC4
- Paths come from the host's structured fields (`Read`, `Edit`, `Write`, `MultiEdit`, and the patch text Codex sends as `apply_patch`'s `command`) and from shell commands in a bounded set of read forms (`cat`, `sed -n`, `head`, `tail`, `nl`, and `rg`/`grep` file operands, optionally after `cd`), parsed with shell quoting and never executed. Codex reports its shell and unified exec calls as `Bash` with the command as one string. Neither host reports the directory a command or patch ran in when it differs from the payload's `cwd`. Relative paths resolve against the hook's `cwd`, plus any `cd` earlier in the command; a call that ran in another working directory (for example through Codex's `workdir` parameter) may be missed or attributed to a same-named file under that `cwd`. To reduce noise, a shell read that names no existing file and a patch path whose directory does not exist are dropped. A path outside the session's own worktree, including a symlink escape, is dropped. Containment follows each symlink before a later `..` applies, as the operating system does when it opens the path, so `link/../file` is checked where it really points. A `cd` target is resolved logically, as Bash's default `cd` does, so `cd link/..` returns to the directory that holds `link`. ^SPEC-0045-US5-AC5
- `rzm hook` starts a handler only when the event, tool class, and parsed paths give it something to act on. A shell call with no recognized read starts a handler only if the handler declared a command-prefix file and the command text starts with one of the prefixes it lists; the file is a JSON list of strings in the handler's local state, which the handler rewrites when its prefixes change, and a missing or unreadable file starts nothing. When no handler applies, it exits before reading emitted-item state. It passes each handler the item IDs already emitted to that agent context and the share of the output budget still unused. An agent context is the agent ID when the host gives one, else the turn when it gives one, else the session. A handler returns `{"items": [{"id": "<id>", "text": "<text>"}]}` and must leave out the IDs it was given before applying its own limits. `rzm hook` drops any item beyond the allowance, joins the rest in starter order, emits them in the host's event-specific JSON form, and only then records their IDs as emitted. Neither host starts a subagent's context again after it compacts: Codex skips `SessionStart` for a compacted subagent, and a Claude Code subagent's auto-compaction discards what `SubagentStart` gave it. The after-compaction hook (`PostCompact`, whose payload carries the subagent's agent ID) therefore resets that agent's context and marks its start items due, and the agent's next tool call runs the start handlers as well as the tool call's own, within the same allowance. A main agent's compaction is reset by the `SessionStart` with source `compact` that follows it, so the after-compaction hook does nothing for it. ^SPEC-0045-US5-AC6
- Once started, `rzm hook` never blocks or fails a tool call. The whole invocation has one fixed deadline that covers selecting the repository's binary, reading and parsing the payload, lock waits, handlers, and cleanup. Selection never prompts for trust, probes a binary, or installs one: `rzm hook` hands off only to a trusted checkout's development build or to a managed pin whose recorded version matches, and otherwise runs itself. A version marker selects a binary without proving that it handles hooks, so a handoff runs the selected binary as a supervised child under the same deadline, with the host's payload on its standard input and the host's standard output as its own. The binary that emits owns the commit: the child writes its output to the host and records the emitted IDs only after that write succeeds, exactly as `rzm hook` does when it runs itself, and the parent neither buffers, validates, nor re-emits it. A nonzero exit, a timeout, which kills the child's process tree, or a binary that does not start leaves a diagnostic and exit status 0. A child killed after writing but before recording may deliver those items again later, the safe direction; one killed while writing leaves truncated output the host rejects as invalid, with nothing recorded. An inherited deadline that has already passed ends the invocation at once. Input and handler output are size-bounded, and a timed-out handler's process tree is killed. One handler's failure does not discard another's items. Any error ends with exit status 0, the items gathered so far or no output, and one entry in a bounded local diagnostic log. A handler's standard error, bounded, goes into that entry whatever its exit status, so a handler that recovers from its own error and returns no items still leaves a trace. A host that cannot start `rzm` at all reports that through its own hook diagnostics. ^SPEC-0045-US5-AC7
- Claude Code entries use the command-and-arguments form (`"command": "rzm"`, `"args": ["hook", "claude", "<event>"]`), which Claude Code runs without a shell, finding `rzm` on `PATH` (`rzm.exe` on Windows). The entry is identical on every OS, so `.claude/settings.json` holds no OS-dependent content. Codex entries run `command` through a shell and set `commandWindows`, quoted for `cmd.exe /C`. ^SPEC-0045-US5-AC8
- The rerun summary and `rzm init --check` report, per harness: whether the entries are installed and current, which starters use them, and when each handler last returned context. They tell Codex users that Codex asks to trust a new or changed hook entry through `/hooks`. ^SPEC-0045-US5-AC9

### US6 - Let approved guard rules refuse a tool call before it runs

- id:: ^SPEC-0045-US6
- summary:: Where the host can refuse a tool call before it runs, the dispatcher can block a call that matches an approved guard rule and tell the agent why and what to do instead.
- status:: ready

Some rules agents keep breaking are better enforced than remembered: a bare `go mod vendor` deletes hand-vendored sources, and the fix is to run `make vendor-treesitter` instead. A context shard arrives after the command has run. Claude Code can refuse a tool call from a `PreToolUse` hook with a reason ([Claude Code hooks](https://code.claude.com/docs/en/hooks)). Guard rules are data a person approved in the repository, matched against the normalized event and never executed; the handler that applies them comes from Rhizome's built-in registry like every other handler.

#### Acceptance Criteria

- A guard rule names a tool class, a match on the normalized event (command text or a contained path), the reason, and the alternative to use. Rules live in tracked files and take effect only after a person approves them, as [[session-memory-starter#^SPEC-0124-US2]] approves a `guard` finding. ^SPEC-0045-US6-AC1
- With hooks on and at least one approved rule, the before-tool-call hook point is installed in each host that can refuse a call. A matching call is refused with one message naming the rule, its reason, and the alternative; other calls proceed unchanged. ^SPEC-0045-US6-AC2
- A guard never blocks on its own failure: a parse error, a timeout, or an unreadable rule lets the call proceed and writes the diagnostic log entry. ^SPEC-0045-US6-AC3
- In a host that cannot refuse a call before it runs, the rule's text is delivered as context after a matching call instead, and status says so. ^SPEC-0045-US6-AC4
- Status lists the active guard rules per host, and how often each refused a call. ^SPEC-0045-US6-AC5

## Requirements

- Stored agent preferences MUST accept only `auto`, `on`, and `off` for Cursor, Claude, Codex, `.agents/skills`, and `AGENTS.md`.
- `--agents` MUST accept a comma-separated subset of `claude`, `codex`, and `cursor`, or `none`, and MUST store the complete resolved set.
- `auto` MUST preserve an existing stored preference when one exists and otherwise inherit detected harness presence.
- `on` MUST enable the requested surface for the current run even when detection did not find a harness marker.
- `off` MUST disable the requested surface for the current run even when detection found a harness marker.
- `off` MUST prevent implied shared-surface fallback for that same surface.
- Any enabled agent surface MUST imply `AGENTS.md` unless `agentsmd` resolves to `off`.
- Any enabled agent surface SHOULD imply `.agents/skills` unless `agent-skills` resolves to `off`.
- Enabled `AGENTS.md` and `CLAUDE.md` MUST preserve user-authored prose outside managed fences.
- Enabled `.agents/skills` MUST receive core skills independently of workflow starter choice.
- Enabled `.agents/skills` MUST receive starter skills only for active workflow templates.
- Disabled surfaces MUST NOT receive new Rhizome command, prompt, rule, doc, or skill artifacts during that run.
- Fresh interactive init MUST show the agents it will set up and write nothing to agent surfaces before confirmation.
- Fresh `rzm init` without a terminal MUST apply the first-run agent onboarding defaults shown by `--check`.
- Reruns MUST resolve agent modes before applying managed template updates.
- When shared agent guidance and a skill surface are both enabled, init MUST install the `rhizome` core skill and matching lean managed routing block as one coherent integration; explicit skill-surface opt-out MUST remain authoritative.
- The managed block MUST report a missing core skill as incomplete Rhizome integration.
- Hook entries MUST be identical for every combination of enabled starters, and MUST change only when a Rhizome release adds or removes a supported hook point.
- `rzm hook` MUST take an early path that skips binary bootstrap, update, download, version probes, and trust prompts, and MUST start its deadline before it selects a repository binary. It MUST NOT start the vault runtime, open the index, or reach the network; handlers that need Rhizome data read files directly.
- Emitted-item state MUST be updated under an exclusive lock with a bounded wait. Dedupe is best effort across crashes: when output and state cannot both be committed, a repeat is preferred to a lost item. Session-start delivery and tool-call delivery MUST use separate scopes, and a missing child agent ID MUST NOT be replaced by the parent's identity. A compaction or clear MUST discard the items recorded for that agent's context in both scopes, so they are delivered again, including when the lock is busy: the pending reset is kept beside the state and applied by every later read until a locked write commits it.
- The dispatcher's own overhead MUST be measured on each supported platform, with and without a matching handler, and recorded before hooks leave draft. Starting `rzm` for every tool call is affordable because an agent step takes seconds: `rzm --version` took a median of 30 ms on macOS (2026-10-21). If the median overhead with no matching handler exceeds 50 ms on a platform, entries on that platform MAY narrow their tool filter to the tools Rhizome's handlers support.
  - macOS, 2026-10-22 (Apple M4 Pro, macOS 27.2, Node 22.23): `rzm hook claude <event>` run directly in a scratch repository with session memory on and five synthetic shards, one warm-up and 30 timed runs per case, each with a fresh session so items are delivered every time. With no matching handler the dispatcher costs what starting `rzm` costs; a handler run adds about 70 ms, almost all of it starting Node. A pinned repository's `rzm` on `PATH` runs the pinned binary as a supervised child, which adds one more process start.

    | Case | Median | p90 |
    | --- | --- | --- |
    | No match (shell call with no read or prefix; no handler starts) | 27 ms | 28 ms |
    | Shard cache hit, delivers | 96 ms | 99 ms |
    | Shard cache miss (a shard changed), delivers | 98 ms | 102 ms |
    | Cold (no dispatcher state or shard cache), delivers | 97 ms | 102 ms |
    | Handler failure (shard folder unreadable), no output | 72 ms | 74 ms |
    | Session start, three shards that apply everywhere | 102 ms | 108 ms |
    | `rzm --version`, for reference | 27 ms | 28 ms |

## Open Questions

- Should Codex, which reads the shared surfaces directly, stop appearing as a separate checklist item once no Codex-specific artifacts ship?
