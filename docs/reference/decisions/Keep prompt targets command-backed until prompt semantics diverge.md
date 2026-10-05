---
type: ReferenceDoc
reference-kind: architecture
summary: "Keep init prompt target directories backed by the command/helper template family until prompts need distinct source ownership, and defer starter ownership metadata until multiple starters create real ambiguity."
decision-domain: architecture
status: active
last-verified: 2026-04-29
---

# Keep prompt targets command-backed until prompt semantics diverge

## Context

`rzm init` writes several agent-facing surfaces: managed agent docs, Cursor/Codex/Claude command files, Codex/Claude prompt files, shared `.agents` skills, Claude skills, starter docs, and optional starter ontology schemas.

The current implementation has one helper-artifact loader: `loadCommandTemplates()`. `applyCursorArtifactsWithUpdater` writes those templates to `.cursor/commands`; `applyCodexArtifactsWithUpdater` writes the same template set to `.codex/prompts` and `.codex/commands`; `applyClaudeArtifactsWithUpdater` writes the same template set to `.claude/commands` and `.claude/prompts`. In this checkout no embedded files match `pkg/app/cli/init/templates/commands/*`, and `fs.Glob` therefore yields a valid zero-template set; helper surfaces may be created and cleaned without writing helper files.

Starter ownership uses directory layout and family-specific loaders today. Generic starter scaffold files come from `pkg/app/cli/init/templates/starters/<template>/repo/**`; skills and the managed block live under `agents/`, and ontology, query recipes, and views under `rhizome/`. Starter managed blocks, starter skills, and starter ontology schemas have separate loaders, collision checks, and refresh semantics. `applyTemplateScaffold` rejects conflicting target paths between selected starters unless the bytes match; `loadAllSkillTemplates` rejects skill-name collisions.

The open questions in [[init-template-architecture]] are whether prompt targets should get a separate `templates/prompts/*` source tree, and whether future starters should declare ownership metadata beyond directory layout.

## Decision

Keep prompt target directories command-backed for now. Do not add `pkg/app/cli/init/templates/prompts/*` until prompt artifacts need behavior or source ownership that is not true for command helper artifacts.

A future prompt loader is justified only when at least one of these is true:

- a prompt artifact needs different template content than the matching command artifact for the same workflow
- prompt artifacts need different cleanup, naming, rejection, or refresh semantics than command artifacts
- a target agent treats prompts and commands as meaningfully different contracts, and mirrored content would confuse users or agents
- command artifacts should be absent while prompt artifacts should still install, or the reverse

Until then, prompt targets are adapter directories for the command/helper template family. Tests should assert parity by the loaded command-template count, including the valid zero-template case.

Keep starter ownership implicit in directory layout while `spec-driven` is the only starter and all families remain locally embedded. Add explicit ownership metadata before accepting multiple independently maintained starters, third-party starter bundles, or starter files whose lifecycle cannot be inferred from their directory family.

Starter metadata, when introduced, should declare at least:

- owned source families: scaffold docs, managed agent-doc blocks, skills, ontology schemas, command/helper templates, prompt-only templates if they exist
- target path policy: create-only, refreshable, managed-fence, direct-copy, or cleanup-owned
- collision policy: error, byte-identical sharing, or intentional shared ownership
- cleanup ownership: exact prefixes or target paths eligible for stale removal
- display/recommendation metadata for interactive init

## Consequences

- Current code remains simpler: one helper-artifact loader and one cleanup/update path cover Cursor commands plus Codex/Claude prompts and commands.
- A missing or empty `templates/commands/` file set remains a valid empty helper-artifact configuration, not evidence that prompts failed to load.
- The phrase "command-backed prompts" is the correct architectural term for the current prompt target behavior.
- Engineers adding prompt-specific behavior must first define why prompts have diverged from commands, then add a prompt loader, tests, docs, and migration/cleanup semantics together.
- Engineers adding another starter can continue using directory layout for now, but any move toward independent or overlapping starters should introduce metadata before widening the starter set.
- Collision rejection remains the practical ownership guard until metadata exists.

## Follow-ups

- If command/helper templates are reintroduced, add a regression test that exercises non-empty mirrored output across `.cursor/commands`, `.codex/commands`, `.codex/prompts`, `.claude/commands`, and `.claude/prompts`.
- If prompt-only templates are added, update [[init-template-architecture]], [[Init - Agent surfaces (prompts, commands, skills)]], and init tests in the same change.
- Before adding a second workflow starter, draft a starter ownership metadata spec and migrate `spec-driven` into that manifest rather than relying on convention plus collision checks.
