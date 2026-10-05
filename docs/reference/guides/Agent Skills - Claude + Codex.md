---
type: ReferenceDoc
summary: "Implementation guide for skills that need to work across Claude and Codex, including layout, metadata, and Rhizome-specific packaging choices."
reference-kind: guide
derived-from:
  - docs/reference-notes/Agent Skills - Claude + Codex.md
last-verified: 2026-04-12
status: active
---

# Agent Skills - Claude + Codex

## Summary

Rhizome skill work often needs to survive across Claude-style and Codex-style skill systems. The durable common ground is the skill-folder contract, metadata-first discovery, and progressive disclosure.

## Contracts

- keep `SKILL.md` at the root of each skill directory
- rely on metadata-first discovery and load supporting files only when needed
- author templates once in the init template tree, then materialize client-specific copies
- validate both invocation style and packaging constraints when targeting multiple agent clients
