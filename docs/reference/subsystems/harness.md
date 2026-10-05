---
summary: "Design constraints and review checklist for the coding-agent harness boundary: child-process drivers for Codex app-server and Claude Code stream-json, sessions, approvals, and one-shot generation."
reference-kind: guide
last-verified: 2026-10-04
code-paths:
  - pkg/harness
tags: [subsystem/harness]
code-anchors:
  go:
    - label: subsystem.harness.guidance.0
      ref: glob:pkg/harness/**/*.go
---

# Coding-agent harness guidance

## Scope

- `pkg/harness/harness.go`: the process-independent boundary. `Harness{Status, StartSession, Generate}`, `Session{ID, SendTurn, Interrupt, Respond, Events, Stop}`, `SessionOptions`, typed `Event` kinds with normalized lifecycle phases and bounded structured output, per-model `Status`, `PermissionMode`, `Decision`. `errors.go` names failure phases (`ErrNotInstalled`, `ErrNotLoggedIn`, spawn, initialize, thread start, turn start, decode, transport closed, timeout, command failed, schema mismatch).
- `pkg/harness/codex/`: drives the user's installed `codex` CLI through `codex app-server` (JSON-RPC 2.0, newline-delimited over stdio) for sessions and `codex exec` for one-shot generation. Written against `codex-cli 0.154.0`; regenerate the reference schema with `codex app-server generate-json-schema --out <dir>`.
- `pkg/harness/claude/`: drives the user's installed `claude` CLI through `claude -p --input-format stream-json --output-format stream-json` with the control-request protocol (`initialize`, `can_use_tool`, `interrupt`, `set_permission_mode`, `set_model`) for sessions and `claude -p --output-format json --json-schema` for one-shot generation. Written against Claude Code 2.1.269.
- `pkg/harness/harnesstest/`: scripted in-memory transport and fakes for consumers.
- `pkg/harness/internal/command/`: shared short-lived process execution for version probes and generation. Vendor drivers keep their dependency interfaces, arguments, result decoding, phase mapping, and version caches.
- `pkg/harness/internal/jsonstdio/`: shared typed JSON framing and session process ownership. Vendor packages retain their message types, routing, approvals, and session state.
- Consumers: `pkg/app/agentchat` (chat), `cmd/harness.go` (`rzm harness status|smoke`), future automations and specialized agents. Contract: [[coding-agent-harness|SPEC-0101]].

## Design constraints

- **The CLI is the credential.** Drivers spawn the vendor CLI and inherit the user's login. Rhizome never reads, stores, or forwards OAuth tokens or API keys for harness work, and never overrides `HOME`; isolation, when needed, uses `CODEX_HOME` / `CLAUDE_CONFIG_DIR`.
- **Short-lived commands have one process owner.** `internal/command.OSRunner` owns lookup, spawn/wait, output capture, context interruption, and process logs for both drivers. It keeps the final 16 KiB of stderr and preserves completed exit results even if the context is canceled afterward. An actual context-triggered kill returns the context error with captured output; each vendor still maps that result to its public phases and owns its version cache.
- **One process per session, owned by the session.** `StartSession` spawns; `Stop` has one three-second deadline for pending-approval denials, cancellation, pipe close, force-kill, and reader shutdown. Pending denials may use at most half of that deadline, and each write also has a short timeout, preserving time to reap the child. Closing stdin interrupts a blocked write. Transport EOF also closes `Events()` and resolves active turns and approvals. `Status` and `Generate` use separate short-lived processes and never touch a session's process.
- **Protocol failures must reach the session before cleanup.** The shared JSON stdio transport reports malformed frames, scanner failures, and stdout EOF with its already-captured stderr tail; it never waits for a live child's stderr to close before returning the error. The session failure path owns bounded termination. Initialize process identity and channels before launching readers. Output readers belong to the transport, so process reaping cannot close them before terminal frames are scanned; `Close` releases them within the caller's shutdown budget.
- **One protocol reader per session.** The lifetime reader dispatches correlated responses, server requests, and notifications. An internal unbounded queue separates protocol reads from the public event channel, so startup and turn progress cannot block on a slow consumer. Every server request receives an approval response, decline, or protocol error.
- **Stream close is graceful and bounded.** `Close` and transport-failure `Abort` stop accepting new events, then give the publisher up to two seconds to deliver every queued event before closing `Events()`. A transport failure queues its terminal error event before aborting the stream.
- **Protocol stays behind the driver.** Consumers see only `harness.Event` kinds, normalized `Phase` values, raw vendor `Status`, and phase errors. Tool and approval input, bounded tool/command output, exit codes, and vendor-provided diffs stay on the flat event. Each driver hand-writes types for the method subset it uses and records the CLI version it was verified against; unknown messages become `EventDiagnostic` and never abort a turn.
- **Approvals suspend the vendor action without blocking protocol reads.** An approval request is registered and emitted as `EventApprovalRequested`; each event states whether that request supports allow-for-session. `Respond`, `Stop`, transport EOF, or vendor cancellation resolves it exactly once. Nothing in the driver allows an action because the repository is trusted; permission modes are an explicit caller choice.
- **The session owns turn lifetime.** `SendTurn` is synchronous and rejects a concurrent turn immediately. Caller cancellation covers the user-message write, MCP reload, and turn-start request; it interrupts the vendor and waits up to five seconds for its terminal event, then stops the session if necessary. `Interrupt`, `Respond`, and `Stop` are safe from other goroutines.
- **Chat session startup and event persistence are bounded.** Agent chat reserves a session slot while starting the harness outside its service mutex, limits startup to 60 seconds, and lets shutdown cancel the reservation. A closed harness event stream evicts that exact live session and ends an interrupted turn with an error so the next message can start or resume a fresh process. Persisted harness events are serialized and transient SQLite busy or locked failures receive bounded retries; an unpersisted approval is denied and terminal persistence failures are published to the browser.
- **Zero `SessionOptions` means repository-default behavior.** Instructions, permission mode, stdio MCP servers, model, and effort are additive; chat passes the empty default and specialized agents in code populate fields. Claude enforces tool allowlists. Codex rejects non-empty allowlists because it cannot enforce them. MCP server names are unique and each entry requires a command.
- **Status describes valid choices.** Models retain their ID, display name, supported efforts, and default flag. Capabilities state supported permission modes, tool-allowlist enforcement, and allow-for-session support.
- **Generation gates the CLI version.** `Status` and `Generate` share a cheap `--version` check cached per driver for ten minutes. Commands killed by context cancellation preserve the caller's context error and map to `ErrTimeout`; interrupted version probes are never cached, so the next healthy request retries. Cancellation after command completion preserves the completed exit result and its diagnostics, including authentication failures. Claude structured generation verifies the top-level object or array type and required object keys. Complete JSON Schema validation remains deferred.
- **Tests use fakes and golden transcripts.** Live CLI tests are opt-in (`RHIZOME_HARNESS_LIVE=1`) and never run in gates.

## Must-dos when changing this subsystem

- New protocol message: add the typed struct, map it to an existing `EventKind` or make it a diagnostic; do not add an `EventKind` unless a consumer renders it.
- New `SessionOptions` field: plumb it through both drivers with a per-field transcript test, and update the zero-value contract above.
- CLI version bump: re-run the live tests, refresh golden transcripts, update the version in the driver's `doc.go` and here.
- Process lifecycle change: prove no orphaned child on `Stop`, on context cancellation, and on transport EOF.

## Review checklist — problems to catch

- A consumer decoding raw protocol JSON or spawning a CLI directly.
- Approval auto-allowed, or `Stop` leaving a pending approval unanswered / a child process alive.
- Events dropped or a goroutine blocked because `Events()` is unbuffered and undrained.
- Vendor API keys, `HOME` overrides, or token files touched by a driver.
- Golden transcripts edited by hand to make a test pass instead of recaptured from the CLI.
- A new option that only one driver honors without a documented exception.

## Key files

- `pkg/harness/harness.go`, `pkg/harness/errors.go`
- `pkg/harness/codex/driver.go`, `events.go`, `status.go`, `generate.go`, `transport.go`
- `pkg/harness/claude/driver.go`, `events.go`, `status.go`, `generate.go`
- `pkg/harness/harnesstest/`
- `pkg/harness/internal/command/runner.go`
- `pkg/harness/internal/jsonstdio/transport.go`
- `pkg/app/cli/harnesscheck/check.go`, `cmd/harness.go`

## Related docs

- [[coding-agent-harness|SPEC-0101]]
- [[mcp-server]] (the `rzm mcp serve` allowlist that specialized agents attach through `SessionOptions.MCPServers`)
