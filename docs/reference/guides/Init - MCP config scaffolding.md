---
type: ReferenceDoc
summary: "Current-state guide for init: MCP config files are no longer scaffolded and legacy files are left untouched."
reference-kind: guide
last-verified: 2026-04-28
status: active
---

# Init - MCP config scaffolding

## Summary

`rzm init` no longer creates MCP config files. It updates agent docs, prompts, commands, and skill scaffolding, while leaving any legacy MCP config files alone.

## Contracts

- do not generate new `.cursor/mcp.json`, `.mcp.json`, or `.codex/config.toml`
- do not clobber existing legacy MCP config files during init refreshes
- keep MCP setup guidance in managed agent docs and user-facing docs, not in generated MCP config files

## Current init-owned surfaces instead

- `AGENTS.md` / `CLAUDE.md` managed Rhizome guidance blocks
- `.agents/skills/*` shared skills when enabled
- `.claude/skills/*` Claude skills when Claude is detected
- `.cursor/rules/rhizome.mdc` and `.cursor/commands/rhizome-*` when Cursor is detected
- `.codex/prompts/rhizome-*` when Codex is detected

## Related contracts

- [[init-starter-workflow]]
- [[init-template-architecture]]
