---
type: ProductSpec
summary: "Defines how rzm init resolves agent surface integration modes across detection, explicit flags, shared AGENTS.md guidance, .agents skills, interactive setup, and CI runs."
id: SPEC-0045
spec-status: active
last-updated: 2026-10-01
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

## Open Questions

- Should Codex, which reads the shared surfaces directly, stop appearing as a separate checklist item once no Codex-specific artifacts ship?
