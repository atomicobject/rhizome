---
type: ReferenceDoc
summary: "Harness capability notes for Agentic Engineering starter skills and subagent delegation."
reference-kind: guide
---

# Harness capabilities

Last verified: 2026-09-07

Agentic Engineering skills should name skills by skill name and let the active harness resolve the installed location.

## Skill surfaces

| Harness surface | Installed path | Notes |
| --- | --- | --- |
| Shared agent skills | `.agents/skills/<skill>/SKILL.md` | Canonical installed surface for starter skills. |
| Claude Code | `.claude/skills/<skill>/SKILL.md` | Mirror of `.agents/skills` when Claude skills are enabled. |
| Codex | `.agents/skills/<skill>/SKILL.md` | Codex loads repo skills from the shared surface; do not hard-code `.codex/skills`. |
| Cursor | `.cursor/rules/rhizome.mdc` + `.cursor/commands/` | Cursor is pointed at the canonical `AGENTS.md` surface; init does not generate a `.cursor/skills/` mirror. |

## Delegation capability

Delegation is available only when the current session exposes a callable task or subagent tool. Use it for bounded work that benefits from independent execution or review. When it is unavailable or adds no value, run the work directly; absence alone needs no ceremony.

Per-harness divergence should use starter skill overlays or installer surfaces. Do not add provider-specific placeholders to base skill bodies unless a concrete harness requires different behavior.

## Capability boundary

The core `rhizome` skill owns current capability discovery, sessions, retrieval mechanics, availability interpretation, mutations, and validation. Agentic Engineering skills decide which evidence protects an engineering decision and what belongs in a spec or effort. They should not copy a generic startup sequence or command catalog.

Saved query recipes provide bounded typed context for specific phase decisions. Start with the smallest relevant recipe or known file evidence, deepen only for missing or conflicting information, and reuse current results across phases. An unavailable lane is unknown rather than empty; state the limitation when it affects a claim, avoid retry loops, and continue independent work that does not require it.

Runtime details can drift. Inspect the active harness when a task depends on a capability rather than treating this guide as a readiness report.
