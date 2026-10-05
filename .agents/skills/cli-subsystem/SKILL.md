---
name: cli-subsystem
description: Use when adding or modifying CLI commands under cmd/ or orchestration under pkg/app/cli, including init templates. Loads CLI layer design constraints and review checklist.
---

# CLI subsystem

## Goal

Change the CLI layer (`cmd/`, `pkg/app/cli/`, init templates) without violating its layering, parity, or template-management contracts.

Read `docs/reference/subsystems/cli.md` first; its constraints are normative for this skill.

## Load-bearing rules

1. `cmd/` stays thin: flag parsing, option-struct assembly, output writing. Business logic lives in `pkg/app/cli/` (or `pkg/vault/*`, `pkg/app/indexing`, `pkg/app/codeintel`) with tests beside it. One file per Cobra command, named after the verb.
2. Resolve user-supplied paths through `pkg/paths` (`AbsFromInputWithVaultPaths`, `ResolveNoteInputWithVaultPaths`, `ResolveCodeInputWithVaultPaths`). No ad hoc `filepath.Abs/Rel`; no legacy `obsidian.NormalizePath` in new code.
3. CLI <-> MCP parity: an agent-relevant flag or capability must land in the shared handler and authoritative `ToolDescriptor`, plus the `cmd/agent*.go` front and curated lists when applicable; document it in `pkg/app/mcp/CONTEXT.md`. Keep MCP options minimal; prefer defaults over knobs. Update `README.md` for user-facing changes.
4. `rzm agent *` commands are non-interactive JSON (`writeAgentPayload`/`writeAgentError`, `silentExitError`); human twins may prompt. Share the underlying runner (e.g. `productionValidationRunner` in `cmd/validation_product_runner.go`) between both fronts.
5. Init templates: edit sources only — directly embedded `docs/rhizome-md-templates/*` and `pkg/app/cli/init/templates/skills|starters/...`. Never touch generated `.agents/skills` / `.claude/skills` copies. Keep starter resolution metadata-driven via `template.yaml`.
6. New committed file under `.rhizome/`: update the managed `.gitignore` template/writer in init or the file gets re-ignored on the next `rzm init`.
7. Preserve user prose outside managed fences in `AGENTS.md`/`CLAUDE.md`; rely on the init preflight collision checks before writes.
8. Avoid editor-opening paths in non-interactive contexts (multiline `--content` hang); detect non-TTY and read stdin or fail.

## Pre-handoff checklist

- [ ] Business logic in `pkg/app/cli/` (or deeper), not `cmd/`; new logic has table-driven tests using testify and fixtures/temp dirs
- [ ] `go test ./cmd/... ./pkg/app/cli/...` passes
- [ ] `go run . --help` (and the changed command with `--vault <test-vault>`) behaves as expected
- [ ] README + MCP surfaces (handler, `ToolDescriptor`, `cmd/agent_surface.go`, `pkg/app/mcp/CONTEXT.md`) updated for user-facing/agent-relevant changes
- [ ] Init template edits made in source directories; no generated copies touched
- [ ] `make check` before commit
