---
type: TechnicalSpec
id: SPEC-0101
aliases: [SPEC-0101, coding-agent-harness]
summary: "Drive Claude Code and Codex as child-process harnesses over stdio for chat, one-shot generation, and specialized in-code agents, using the user's own subscriptions and a per-user harness setting."
spec-status: active
last-updated: 2026-09-12
---

# Coding-agent harness

## Summary

Rhizome calls large language models through the user's installed coding agents, Claude Code and Codex, instead of vendor HTTP APIs. One Go harness boundary owns process lifecycle, protocol framing, sessions, turns, approvals, interrupts, status, and one-shot structured generation. Both harnesses are child processes speaking newline-delimited JSON over stdio: Codex through `codex app-server` (JSON-RPC 2.0) and Claude Code through `claude -p --input-format stream-json --output-format stream-json` with its control-request protocol. No Node sidecar and no vendor API keys are required.

The built-in chat is the first consumer. Automations and agentic commands are the reason the boundary exists: any caller composes a session from instructions, a tool allowlist, a permission mode, and optional MCP servers, so specialized agents can be defined in code without touching the drivers. This spec supersedes the design direction of the unmerged PR #101 (Codex-only, tool-catalog parity, MCP bridge); it reuses that branch's Codex app-server client primitives where they still fit.

Reference implementation for protocol and lifecycle decisions: T3 Code ([pingdotgg/t3code](https://github.com/pingdotgg/t3code), `apps/server/src/provider/`).

## Goals

- One harness interface that chat, automations, and agentic commands all call; no consumer touches raw protocol messages or process handles.
- Codex and Claude Code drivers with equivalent behavior for start, turn, streaming, approvals, interrupt, stop, resume, and status.
- One-shot structured generation (prompt plus JSON schema in, JSON out) that needs no session or UI.
- A per-user harness preference and per-harness model, effort, and permission mode in the existing user config file.
- Chat sessions persist and resume across Rhizome restarts using the harness's own thread or session identity, exposed through `Session.ID()`.
- Specialized agents are ordinary Go values: session options with instructions, tool allowlist, permission mode, and MCP servers.
- Deterministic tests with fake transports; live-CLI tests are opt-in.

## Non-Goals

- Keeping the API-key agent loop, its provider engines, or the in-process tool catalog in chat.
- Tool parity between engines, an MCP bridge, or a shared tool catalog. There is one engine kind.
- An HTTP MCP transport, per-session bearer tokens, or multi-user hosting. A stdio MCP server with an allowlist is sufficient until a use case needs more.
- Vault- or config-defined agent definitions. Agents are defined in code for now.
- A programmatic login flow. Rhizome detects auth state and tells the user which CLI command to run.
- The final visual design of the chat workspace. This spec fixes the event model and baseline states; the polished experience gets its own spec.
- Removing `pkg/llm` where it serves non-chat features such as embeddings or model listing.

## User Stories

### US1 - Drive either harness through one boundary

- id:: ^SPEC-0101-US1
- summary:: A caller starts a session on the configured harness, sends turns, receives a typed event stream, answers approval requests, interrupts, and stops, without knowing which CLI is underneath.
- status:: ready

#### Acceptance Criteria

- A session started with the Codex driver performs `initialize`, `initialized`, then `thread/start` (or `thread/resume` with fallback to start on a recoverable not-found error) before the first `turn/start`.
  verification:: fake-transport transcript asserts order.
- A session started with the Claude driver spawns the CLI with stream-json input and output, partial messages enabled, and the configured permission mode, and answers `can_use_tool` control requests from the harness approval flow.
  verification:: fake-transport transcript asserts spawn args, initialize exchange, and a control_response for a can_use_tool request.
- Assistant text deltas, reasoning, command execution, file changes, tool calls, approval requests, turn start and completion, token usage, and errors arrive as typed harness events with stable kinds; unknown protocol messages become diagnostic events and never abort the turn.
  verification:: mapping tests per event kind for both drivers, including an unknown method.
- An approval request blocks the harness turn until `Respond` is called with allow, deny, or allow-for-session; a session stop while an approval is pending denies it and terminates the process.
  verification:: fake-transport test proves no continuation before the decision and clean shutdown with a pending request.
- `Interrupt` stops the running turn; for Claude the driver closes the query when the CLI does not acknowledge within a bounded time.
  verification:: driver tests with an unresponsive fake.
- `Status` reports, per harness: not installed, installed but not logged in, ready, and version; auth is read from `account/read` for Codex and from the Claude initialize result without making a model request.
  verification:: status tests for each state with fakes; an opt-in live test against the installed CLIs.

### US2 - One-shot structured generation

- id:: ^SPEC-0101-US2
- summary:: A caller passes a prompt, an optional JSON schema, and model options and receives the parsed result without creating a persistent session.
- status:: ready

#### Acceptance Criteria

- Codex generation runs `codex exec --ephemeral --skip-git-repo-check -s read-only` with the schema and model options, prompt on stdin, and returns the last message parsed against the schema.
  verification:: exec-runner test with a fake binary asserts args and parsing.
- Claude generation runs `claude -p --output-format json --json-schema ... --tools "" --strict-mcp-config --permission-mode dontAsk` with the prompt on stdin and returns the structured result.
  verification:: exec-runner test with a fake binary asserts args and parsing.
- Generation failures name the phase: not installed, not logged in, spawn, timeout, non-zero exit with stderr tail, or schema mismatch.
  verification:: one test per failure class.

### US3 - Per-user harness settings

- id:: ^SPEC-0101-US3
- summary:: The user chooses a harness and per-harness model, reasoning effort, and permission mode once, in the user config file, and every consumer respects it.
- status:: ready

#### Acceptance Criteria

- `~/.config/rhizome/config.yml` `agent:` holds `harness: claude | codex` plus per-harness `model`, `effort`, and `permission-mode`; `enginePreference` and provider API-key selections are removed from the agent block.
  verification:: settings load, save, defaults, and unknown-value tests.
- Model and effort choices come from the harness (`model/list` for Codex, the initialize result for Claude), not from vendor APIs; `Status.Models` is a list of `ModelOption{id, displayName, efforts, default}` so effort is constrained per model. The web settings endpoint exposes them with the current status.
  verification:: handler tests with fake drivers.
- When no harness is configured, the default is the first installed and logged-in harness, and the UI states which one was chosen and why.
  verification:: resolution tests for none, one, and both installed.

### US4 - Chat runs on the harness

- id:: ^SPEC-0101-US4
- summary:: The built-in chat routes every turn through the harness, persists the timeline locally, resumes sessions after restart, and drops the API-key loop.
- status:: ready

#### Acceptance Criteria

- Each chat session owns one harness session with cwd at the vault root; the harness thread or session id, harness kind, model, and last turn id persist in `.rhizome/agent/sessions.sqlite` and are used to resume after restart.
  verification:: store migration test plus a service test that resumes with a fake driver.
- Harness events map to persisted chat events and the existing SSE stream; assistant text persists as one message per turn after completion. Sending a message starts the turn asynchronously (202 with `turnRunning`) and a client disconnect does not cancel it; `diagnostic` events are streamed to subscribers but not persisted.
  verification:: endpoint test with a fake driver proves persisted messages and events survive reload.
- Approval requests and decisions are persisted events; decisions are written with the service context so a client disconnect cannot lose the record.
  verification:: service test with a canceled request context.
- `generateAnswer`, the provider engines, the mock fallback, `tools.go`, and the `workspace_*` tools are deleted from `pkg/app/agentchat`; `pkg/llm` remains only for non-chat callers.
  verification:: package compiles with no `pkg/llm` import in `pkg/app/agentchat`; `go vet` and tests pass.

### US5 - Specialized agents defined in code

- id:: ^SPEC-0101-US5
- summary:: A Go caller composes an agent from instructions, a tool allowlist, a permission mode, and MCP servers, and can expose a chosen subset of Rhizome operations to it over stdio MCP.
- status:: ready

#### Acceptance Criteria

- `SessionOptions` carries `Instructions`, `AllowedTools`, `DisallowedTools`, `PermissionMode`, `MCPServers`, `Model`, `Effort`, `Cwd`, and `Resume`; the zero value means the harness's repository-default behavior, which is what chat uses. Permission modes are `approval-required`, `auto-accept-edits`, and `full-access`; there is no `auto` mode because Claude Code's `-p` mode cannot honor it. A driver that cannot enforce an option fails closed: Codex rejects non-empty `AllowedTools`/`DisallowedTools` at `StartSession` rather than approximating them with instructions. `Status.Capabilities` reports `supportsAllowedTools`, `supportsAllowForSession`, and the supported permission modes so callers can choose before starting.
  verification:: each driver test asserts how every populated field reaches the CLI (Claude flags and initialize options; Codex `-c` overrides, `collaborationMode.settings.developer_instructions`, sandbox and approval policy).
- `MCPServer` is stdio-only: `Name`, `Command`, `Args`, `Env`. An HTTP variant needs its own contract for headers, authentication, and network policy before it is added.
- `rzm mcp serve --tools <allowlist>` exposes existing `pkg/app/mcp` handlers over stdio MCP using the vendored `mcp-go` server; handlers are not duplicated or re-described.
  verification:: server test lists only allowlisted tools and dispatches one call through the shared handler.
- A session with an MCP server entry injects it as a Claude `mcpServers` entry and a Codex `mcp_servers.<name>` override, and the Codex driver reloads the MCP catalog before each turn.
  verification:: driver transcript tests.

### US6 - Baseline chat states on the new event model

- id:: ^SPEC-0101-US6
- summary:: The chat workspace renders harness status, streaming turns, command and file-change items, approval prompts with decisions, and interrupt, with density-first layout ready for a later experience pass.
- status:: ready

#### Acceptance Criteria

- The workspace shows which harness backs the session and one of: not installed with the install hint, not logged in with the exact login command, ready, or degraded with the last error.
  verification:: component tests per state.
- Approval prompts show the action, target paths or command, and the harness-provided reason, with allow, deny, and allow-for-session; controls disable after a decision.
  verification:: component tests.
- Interrupt is available during a turn and the timeline shows the interruption.
  verification:: component test plus an end-to-end journey with a fake driver.

## Requirements

### Boundaries and placement

- MUST add `pkg/harness` with `harness.go` (interface, options, events, errors), `codex/` and `claude/` drivers, and `harnesstest/` fakes. Files stay under about 500 lines.
- MUST keep protocol types behind the driver. Both drivers hand-write typed structs for the method subset they use, record the CLI version they were written against (`codex app-server generate-json-schema` is the Codex reference; the installed `claude` CLI is the Claude reference), and cover them with golden transcripts. Unused protocol surface is not modeled.
- MUST make `pkg/app/agentchat` a consumer of `pkg/harness` and nothing else for model access.
- SHOULD reuse `codexapp/{client,stdio,readiness,fake}.go` from branch `codex/codex-app-server-support` as the starting point for the Codex driver, updated to the current protocol.

### Process model

- MUST spawn one child process per harness session, owned by the session, killed on stop with a bounded force-kill delay; a separate short-lived process per status probe and per one-shot generation.
- MUST keep one reader goroutine per session that owns the transport for the session's lifetime and feeds consumers through an internal unbounded queue, so a slow consumer never blocks protocol reading and startup notifications cannot deadlock `StartSession`. Every server request receives a reply. `SendTurn` is synchronous, rejects a second concurrent turn, and on caller cancellation interrupts the vendor turn and waits a bounded time before force-closing the session.
- MUST answer or deny every pending approval on `Respond`, protocol cancellation, transport EOF, and `Stop`, exactly once.
- MUST pass cwd explicitly (spawn cwd and `thread/start.cwd` for Codex; `--add-dir`/cwd for Claude) and inherit the user's environment; isolation, when added, uses `CODEX_HOME` and `CLAUDE_CONFIG_DIR`, never `HOME`.
- MUST gate on a minimum CLI version per harness and report the installed version in status.
- MUST drain stderr and keep a bounded tail for diagnostics.

### Settings

- MUST store the harness preference in the user config, not repository config, via the existing `AgentUserConfig` path and 0600 file mode.
- MUST NOT read vendor API keys for chat.

### Safety

- MUST default chat to an approval-required permission mode; allow-for-session and broader modes are explicit user choices per session or per specialized agent.
- MUST NOT auto-approve shell, patch, filesystem, or network actions because the repository is trusted.
- MUST surface harness file changes as events; Rhizome never commits or reverts them implicitly.

### Persistence

- MUST migrate `sessions.sqlite` additively: harness kind, harness session id, model, last turn id; existing sessions load and render as read-only history when they predate the harness.

### Testing and observability

- MUST test drivers with fake transports and golden transcripts; live tests behind a build tag or env flag.
- MUST log every process spawn, exit code, and protocol decode failure with the session id.
- MUST include a headless `rzm harness status` and `rzm harness smoke` (one turn, one generation) for manual verification without the web UI. They live at the root, not under `rzm agent`, because every `rzm agent` subcommand must join the one-shot runtime registry and code-mode catalog, which these diagnostics do not belong to.

## Open questions

[TODO: Confirm with user] HTTP MCP with per-session tokens, as T3 Code does, is deferred until a web-hosted multi-session need appears. Proposed: keep stdio only.

[TODO: Confirm with user] Subscription terms. Driving the vendors' own CLIs is the same posture T3 Code and this repository's `scripts/claude-fable` already take. Proposed: proceed, and document that Rhizome never extracts or reuses OAuth tokens.

[TODO: Confirm with user] Old provider-backed chat sessions. Proposed: keep them readable, do not attempt to resume them.

## Documentation plan

- New subsystem note `docs/reference/subsystems/harness.md` with `code-paths: pkg/harness` and a `harness-subsystem` skill; add the row to `docs/reference/subsystems/README.md` and regenerate the Greptile map.
- Update `docs/reference/subsystems/mcp-server.md` when `rzm mcp serve` lands (the note currently states there is no standalone stdio MCP server) and fix `SUMMARY.md`.
- Update `pkg/app/web/CONTEXT.md` agent-chat invariant and `pkg/app/agentchat/doc.go`.
- Record the pinned Codex protocol ref and the Claude CLI minimum version in the subsystem note.
