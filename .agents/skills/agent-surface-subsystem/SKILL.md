---
name: agent-surface-subsystem
description: Use when implementing, modifying, or reviewing the rzm agent command family or context-pack rendering under pkg/app/agent, pkg/app/agentapi, pkg/app/contextpack, pkg/app/answer, or pkg/app/presentation. Loads agent-surface constraints and review checklist.
---

# Agent surface subsystem

## Goal

Change the `rzm agent *` surface or its rendering pipeline without breaking the contract AI agents parse: stable JSON shapes, token-budgeted output, structured warnings instead of false-confidence matches.

## First step

Read `docs/reference/subsystems/agent-surface.md` first. Its constraints and review checklist are normative for this work.

## Load-bearing rules

- Output is an API consumed by agents. Treat JSON field renames/removals, exit-code changes, and packed-text restructuring as breaking changes; add fields, don't mutate them.
- CLI and MCP share one implementation: `cmd/agent_*.go` -> `agentapi.CallJSON` -> catalog factory -> `pkg/app/mcp` handler. Never fork a CLI-only response shape; agent-relevant options must land in the shared handler and catalog descriptor and be documented in `pkg/app/mcp/CONTEXT.md` / `README.md`.
- Honor contextpack invariants (`pkg/app/contextpack/pack.go`): priority -> score -> key ordering, first piece always included, caller-supplied budgets win. Search paths honor the query-spec budget, not the 150k agent CLI floor.
- Ambiguous target resolution returns structured `search.Warning`s; never widen to broad fuzzy matches. `pkg/app/answer` selects from supplied evidence and must not invent sources.
- Every new agent command threads `sessionId` into its args map (`maybeSetString(payload, "sessionId", agentSessionID)`), registers in `cmd/agent.go`, and declares its CLI display order/category on its `ToolDescriptor`.
- If agent workflow guidance changes, update the directly embedded `docs/rhizome-md-templates/*` sources; never edit generated `.agents/skills`/`.claude/skills` copies.
- Respect layer boundaries: planning in `pkg/app/agent`, adaptation in `pkg/app/agentapi`, retrieval/runtime in `pkg/app/mcp`, packing in `contextpack`, rendering in `presentation`, packet assembly in `answer`.
- `rzm agent *` is non-interactive: JSON via `writeAgentPayload`/`writeAgentError`, failure via `silentExitError{code: 1}`; no editors or prompts.

## Pre-handoff checklist

- [ ] Tests added/updated beside the change (red-green; regression test for contract fixes).
- [ ] `go test ./pkg/app/agent/... ./pkg/app/agentapi/... ./pkg/app/contextpack/... ./pkg/app/answer/... ./pkg/app/presentation/... ./cmd/...` green.
- [ ] CLI/MCP parity verified: handler + `ToolDescriptor` memberships and CLI display metadata reflect any new agent-relevant options; `pkg/app/mcp/CONTEXT.md` / `README.md` updated.
- [ ] Directly embedded RHIZOME.md template sources updated if guidance changed.
- [ ] Review checklist in `docs/reference/subsystems/agent-surface.md` walked against the diff (budget blowouts, schema drift, false-confidence fallbacks, sessionId omissions, template drift).
- [ ] `make check` before commit.
