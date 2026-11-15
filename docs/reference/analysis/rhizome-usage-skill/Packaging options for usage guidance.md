---
type: ReferenceDoc
summary: "Where new Rhizome usage guidance could live (references in the rhizome skill, a new core skill, starter-shipped guidance, desktop chat instructions, or shipped product docs), with each option's trigger, loading, refresh, and spec tradeoffs."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Packaging options for usage guidance

## Summary

Any home for usage guidance has to solve three problems. The first is triggering: the guidance must load for "what is Rhizome", "what can I do here", and "help me customize this" without stealing the change-work triggers the `rhizome` skill is tuned for. The second is content: a product model, a map from user intent to existing mechanisms, and starter recommendations. The third is reach: standalone harnesses and the desktop chat, in both new and existing repositories. No single location solves all three. The evidence favors new references in `rhizome`, with starter facts discovered from live data and a one-sentence pointer in the desktop chat prompt. The council's main decision is whether that guide belongs inside `rhizome`, which would require amending SPEC-0080's starter-name rule or working around it, or in a separate skill beside it.

The facts behind this note are in `Current skill coverage and gaps.md` in this folder. Statements marked "inference" are judgment, not checked behavior.

## What the guidance has to carry

The coverage audit found strong mechanics and a missing translation layer. The new content is therefore mostly orientation and routing:

- what Rhizome is and what its parts are: typed Markdown, the index and graph, views and experiences, the agent surfaces, the desktop app, starters, modules, actions, and scheduled tasks;
- what the user can do from the surface they are looking at;
- a decision table from intent to mechanism: generated view, authored native view, custom view, experience, ontology change, code-mode script, module action, or scheduled task;
- setup choices, starter recommendations, and install mechanics: `rzm init --check --json`, `--workflow`, `--add-addon` and `--remove-addon`, the Add-ons settings in Desktop, and `rzm init --eject`;
- a handoff into the existing references for every mechanic, so that no mechanic is restated.

## Option A: new references inside `rhizome`

This is Drew's starting intuition. It would add `references/usage.md` (product model, capability map, and decision table), possibly `references/starters.md`, one or two routing rows such as "Explain Rhizome, what this repository can do, or how to customize it", and a few words in the description.

**For**

- The trigger already fires. The managed block and `SKILL.md` both say to load the skill when the request names Rhizome, so questions that name it reach the router today and find no row.
- The description has room: 314 of 1024 characters are used. The always-on cost of a few added words is negligible.
- It keeps the single entry point that SPEC-0080 established when it retired four core skills.
- The decision table can link directly to `views.md`, `ontology-authoring.md`, `code-mode.md`, and `custom-views` without duplicating them.
- The desktop chat gets it in every repository that installs the skill for the chat's harness.
- A new reference file counts as a missing file, so it installs on the next `rzm init` even where someone has edited `SKILL.md`. The routing row, by contrast, does not reach an edited `SKILL.md` without a person's decision.

**Against**

- It conflicts with SPEC-0080, which requires the skill to "keep starter-specific names, spec/effort terminology, and starter-owned recipe ids out of its base body and references" and lists "naming bundled starters" as a non-goal. Starter recommendations need one of three fixes:
  1. Amend the spec.
  2. Discover starters at run time from `rzm init --check --json`. That surface omits optional add-ons (`addonOptions` skips `optionalAddon`, so `compounding` and `ao-kb` are invisible), so it needs a small CLI change to list them.
  3. Add a slot that each starter fills through an overlay. This describes only installed starters, so it cannot recommend one that is not installed.
- SPEC-0080 deliberately keeps the description away from blanket triggers because they overtrigger. Adding user-question phrases risks loading the router for unrelated chat (inference). Routing evaluations exist as a pattern to follow: `docs/reference/analysis/agentic-engineering-routing-evals-2026-09-04.md`.
- A "what is this" question pays for the router's operating rules on sessions, repairs, and validation, about 2.1k tokens, before the guide loads. That cost is acceptable but not useful.
- Required changes: update `expectedRefs` in `TestCoreRhizomeSkillRoutesToDirectFocusedReferences`, add the reference to SPEC-0080's Requirements list, and stay within the 1024-character description limit.

## Option B: a new core skill beside `rhizome`

This option adds a skill such as `rhizome-guide` under `pkg/app/cli/init/templates/skills/markdown/`, which installs everywhere, like `custom-views`.

**For**

- It gets its own description, written around user questions, and its own voice for people rather than for an agent mid-task.
- SPEC-0080's text governs the `rhizome` skill. A sibling could name starters without amending that rule, although doing so arguably breaks the spirit that core guidance stays starter-agnostic.
- It could be user-invocable, as the starter skills `agentic-engineering`, `foundation-review`, and `ingest-transcript` are, which gives a slash-command "help" entry in Claude Code and a `$` entry in Codex.
- Unconditional audience-specific core skills already have precedent: `legacy-codebase-assessor` and `client-harness-builder`.

**Against**

- Its triggers would collide. "Set up my repo" matches both this skill and `rhizome`'s installation route. "Make a view" or "make an experience" matches both this skill and `custom-views`. The agent may load both (inference), which costs more than Option A.
- It adds about 250 to 400 description characters, roughly 60 to 100 tokens, to every repository's always-on context.
- It reverses SPEC-0080's consolidation, which retired `rhizome-onboard`, `rhizome-note-authoring`, `rhizome-ontology`, and `rhizome-skill-creator`. Init still deletes folders with those names, so none of them can be reused.
- `references/evidence-and-composition.md` says "do not make one skill's files depend on a sibling skill's files". The guide could name the `rhizome` skill and its routes, but it could not link to `rhizome/references/*.md` by path, which makes the handoff weaker than in Option A.
- It adds two more generated copies per repository (`.agents/skills/<name>`, `.claude/skills/<name>`), a new spec or SPEC-0080 amendment, and its own tests.

## Option C: starter-shipped guidance

Each starter could describe what it adds, either as its own skill or as a slot overlay into an Option A reference.

**For**

- Starter names stay with their starters, which fits SPEC-0080 without amendment if delivered through a slot.
- Content matches each installation exactly, because only installed starters contribute.
- Starters already ship human guides such as `docs/reference/guides/action-items.md` and `compounding.md`, which a slot fragment could point to.
- The mechanism exists. `skilloverlay.Render` applies starter overlays to core and starter skills together, and unknown slots fail closed.

**Against**

- It cannot recommend a starter that is not installed, which is the point of "which starter?".
- It is absent where it is most needed. A `none` repository installs no starter at all, not even `core`.
- As standalone skills, it fragments the guide and adds a description per starter. As a slot, it depends on Option A existing.
- Overlays are lightly used today: only `complex-domain` ships them, and only into `agentic-engineering`.

## Option D: desktop chat instructions only

This would extend `chatInstructions` in `pkg/app/agentchat/page_relay.go`, which is appended to every turn.

**For**

- It always applies in the app, with no dependence on a description trigger.
- It knows the user is inside the app and can explain the `[Rhizome view]` block and the `page_text` tool.
- It is the only channel that reaches a Claude chat in a repository set up with `--agents shared`, where no `.claude/skills` exist (inference, not yet checked at run time).

**Against**

- It does nothing for standalone harnesses.
- It costs tokens on every turn, so it can carry a pointer but not content.
- It ships in the Go binary, not in the repository, so teams cannot review or adapt it like other agent guidance.
- It would duplicate whatever the skill says, which invites the two to drift apart.

Inference: the role that fits is one or two sentences. One routes questions about Rhizome itself to the `rhizome` skill; the other gives a minimal fallback when no skill is installed.

## Option E: ship the product guides

Rhizome could embed its human guides in the binary, served by a command, so that the skill can route to them instead of restating them. `docs/rhizome-md-templates/` already uses `go:embed` this way.

**For**

- One source serves people and agents, which removes drift between `choosing-your-starter.md` and any skill reference that restates it.
- The skill stays small.

**Against**

- It needs a new CLI surface.
- The guides are written for people using the desktop app and describe click paths an agent cannot follow.
- `AGENTS.md` says feature and configuration documentation belongs in vault notes rather than in agent templates.

This option is a complement to the others, not a replacement.

## Comparison

| | A: references in `rhizome` | B: new core skill | C: starter-shipped | D: chat prompt | E: shipped guides |
| --- | --- | --- | --- | --- | --- |
| Loads for "what is Rhizome" | Yes, once a row exists | Yes, if its description wins | Only with A | Always, in the app | Only through a skill route |
| Always-on cost | A few words | 60 to 100 tokens | One description per starter, if separate skills | Every turn | None |
| Standalone reach | Yes | Yes | Installed starters only | No | Yes |
| Desktop chat reach | Yes, where the skill is installed | Same | Same | Yes, every repository | Same |
| Recommends uninstalled starters | Needs a spec amendment or live data | Yes | No | No | Yes |
| Spec and test changes | SPEC-0080 list, `expectedRefs` | New spec, new tests, reverses consolidation | Slot declaration, overlay tests | Go constant | New command |
| Refresh in existing repositories | New file installs; edited `SKILL.md` keeps its old routing | Whole skill installs | Follows its host skill | Binary update | Binary update |

## Researcher's lean (inference)

Combine A and D, and source starter facts from live data:

1. Add one `references/usage.md` to `rhizome`, containing the product model, the per-surface capability map, and the intent-to-mechanism table, all pointing into existing references. Add one routing row and a short description phrase. Evaluate triggering on Claude Code and Codex before shipping.
2. Have the guide read workflows and add-ons from `rzm init --check --json`, and change that output (or add a small listing command) so it includes optional add-ons. The skill then names no starters, and SPEC-0080 needs only its reference list amended.
3. Declare one slot in the guide so that each installed starter can say what it adds and where its human guide lives.
4. Add one sentence to `chatInstructions` that sends questions about Rhizome itself to the `rhizome` skill.

Reconsider Option B only if routing evaluations show that the widened `rhizome` description over-triggers on ordinary work or under-triggers on user questions. Option E is worth separate consideration if the decision table and the human guides start restating the same facts.
