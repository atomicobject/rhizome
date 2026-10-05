---
name: mcp-server-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/app/mcp, pkg/app/agentapi, or the agent runtime wiring in cmd (runtime_helpers.go, agent_surface.go). Loads MCP tool-handler design constraints (responsiveness-first async init, surface/doc sync) and review checklist.
---

# MCP server subsystem

## Goal

Change the MCP tool-handler layer (`pkg/app/mcp/`, `pkg/app/agentapi/`, agent runtime wiring in `cmd/`) without breaking its responsiveness, async-init, or surface-sync contracts.

Read `docs/reference/subsystems/mcp-server.md` first; its constraints are normative for this skill.

## Load-bearing rules

1. Respond first, initialize later: tool-serving processes must become callable immediately. All slow init runs in `LiveRuntime` background phases. For any new init code ask: "will this block the caller becoming responsive?"
2. Async state flows through `runtimeview.View`: handlers take fresh snapshots through `Config` accessors or `Runtime.Snapshot()`, never stale copied fields.
3. Tools wait or degrade: use only the relevant `WaitForSearch` / `WaitForSemantic` / `WaitForCodeIndex` when blocking is worthwhile; otherwise inspect that phase and return a targeted unavailable error. Semantic readiness never implies code or leader readiness.
4. Surface parity comes from one `ToolDescriptor`: add the handler factory, memberships, and CLI display metadata, then add the CLI front when applicable. Update `README.md` when user-facing.
5. Bias to minimal, high-signal options: prefer a sensible default over a new knob; justify any added option against the pared-down surface.
6. No ad hoc indexing from handlers: index/embedding writes go through leader coordination and the index lock (`pkg/vault/indexlock`).
7. Errors surface as structured tool error payloads (`IsError` + text) that `agentapi.CallJSON` converts; keep in-process behavior aligned with real MCP requests (JSON-normalized args).
8. Update `pkg/app/mcp/CONTEXT.md` when entry points or the async-init invariant change.

## Pre-handoff checklist

- [ ] No blocking init added to runtime construction paths; slow work remains in LiveRuntime phases
- [ ] Handlers use phase-specific waits/snapshots and degrade gracefully under cold start
- [ ] New tools/options represented by handler + descriptor memberships/display metadata + applicable CLI front; README updated if user-facing
- [ ] Tests beside handlers; `go test ./pkg/app/mcp/...` and `go test ./pkg/app/agentapi/...` pass
- [ ] `pkg/app/mcp/CONTEXT.md` reflects any entry-point or invariant change
- [ ] `make check` before commit
