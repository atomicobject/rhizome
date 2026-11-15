---
type: ReferenceDoc
summary: "Audit of the agent guidance Rhizome ships today: what the core rhizome skill assumes, which user questions it cannot answer, its size and loading costs, and the constraints any usage guidance must respect."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Current skill coverage and gaps

## Summary

The shipped guidance is a thorough operator's manual for an agent that already has a task in an initialized repository, and it contains no product model. Once a request is phrased in Rhizome's own terms (a type, a view mount, a recipe, a selector), the `rhizome` skill and the managed block route the agent to the right mechanic. Nothing explains what Rhizome is, what the desktop app shows, which starters exist, or how to turn a user's goal into one of those mechanisms. Of seven test questions, three get no answer, three get mechanics without the guidance needed to choose among them, and one is answered well. The gap is a translation layer between user intent and existing mechanics, not missing mechanics.

Unless marked as inference, every statement below was checked against the files cited.

## What ships today

| Guidance | Source of truth | When it installs | Size |
| --- | --- | --- | --- |
| `rhizome` skill | `pkg/app/cli/init/templates/skills/markdown/rhizome/` | Every repository with an agent surface | `SKILL.md` 78 lines, 8.4 KB; 21 references, 107 KB |
| `custom-views` skill | `pkg/app/cli/init/templates/skills/markdown/custom-views/` | Same | `SKILL.md` 137 lines, 18.7 KB; 5 references, 71 KB (`kit-api.md` alone is 42.7 KB) |
| `legacy-codebase-assessor` skill | `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/` | Same | 11.6 KB plus a 5.7 KB template |
| `client-harness-builder` skill | `pkg/app/cli/init/templates/skills/markdown/client-harness-builder/` | Same | 5.7 KB plus three references of about 8 KB |
| Starter skills | `pkg/app/cli/init/templates/starters/<id>/agents/skills/` | With their starter | `agentic-engineering` 3, `action-items` 2 (including `assumption-tracker`), `complex-domain` 8, `compounding` 1, `ao-kb` 1; `core` ships none |
| Managed blocks | `docs/rhizome-md-templates/RHIZOME.md` plus each starter's `agents/AGENTS.md` | With the agent surface and each starter | `RHIZOME.md` 380 words; starter blocks 60 (`core`) to 356 (`agentic-engineering`) words |
| Desktop chat prompt | `chatInstructions` in `pkg/app/agentchat/page_relay.go` | Every in-app chat turn | About 90 words |

The four core skills install regardless of starter. `loadAllSkillTemplatesWithReport` in `pkg/app/cli/init/helper_templates.go` merges every skill under `templates/skills/markdown/` with the selected starters' skills, and `pkg/app/cli/init/templates/starters/README.md` says core skills "install regardless of starter". The `none` workflow choice lists no starters (`workflowChoices` in `pkg/app/cli/init/first_run_prompts.go`), and `core` is reached only through other starters' `requires`, so a `none` repository receives the four core skills and no starter skill.

The desktop chat runs the repository's own harness. `pkg/app/agentchat/service.go` starts the session with `Cwd` set to the vault path and passes `chatInstructions` as `--append-system-prompt` for Claude Code (`pkg/harness/claude/options.go`) or `DeveloperInstructions` for Codex (`pkg/harness/codex/driver.go`). Claude runs with `--strict-mcp-config` and only the page MCP server, so the chat reaches Rhizome through the CLI and whatever skills the harness discovers in the repository. The chat prompt names no skill. It says only "Prefer Rhizome's own tools and the repository's notes for facts."

## 1. What the rhizome skill assumes

The premise is confirmed, with one refinement. The skill is written for an agent that is changing something and needs Rhizome's evidence and mechanics while doing it.

- The description's triggers are change-in-progress conditions: "before changing behavior a note, spec, or decision record may constrain, when the rationale behind existing code matters, when a note or linked heading moves, or for any explicit Rhizome operation", and "Not for a local edit whose whole effect is visible in the diff."
- Four of the five "When to reach for it" bullets describe a change. The fifth is "The user names Rhizome, a session, search, validation, typed notes, or the ontology."
- The three worked examples are changing indexer write batching, explaining search warnings, and fixing a README typo.
- Session seeds, `file-context.md`, and `onboarding.md` speak of `src/auth`, subsystems, entrypoints, and tests.
- `references/evidence-and-composition.md` frames everything Rhizome returns as "evidence, not authority" and leaves the deliverable to a workflow skill.
- SPEC-0080 (`docs/specs/product/base-rhizome-agent-guidance.md`) states this intent: the skill owns "universal mechanics" while "starter and workflow skills continue to own deliverables such as coding, specification, planning, and domain work."

The refinement is that the skill covers more than coding. It also routes structured-note authoring, ontology changes, view YAML, validation repair, safe moves, and installation. A more accurate description is an operator's manual, organized by tool family, for an agent with a task in a repository where Rhizome already runs. An earlier analysis reached the same conclusion about its organization (`docs/reference/analysis/agent-experience-research-handoff.md`). The skill makes five implicit assumptions:

1. Rhizome is installed and initialized, or the agent is installing it for someone who has already chosen it.
2. The request arrives in Rhizome's vocabulary: type, interface, display group, mount, recipe, selector, variant.
3. The agent's job is mechanics. Intent comes from the user or a workflow skill.
4. The agent works in a shell with `rzm`, and its output is commands and files rather than explanation.
5. The repository is a software project.

A search of the skill folder for product-level terms finds the following. The desktop app appears only twice, both in passing: in the code-mode sandbox note and in diagnostics. The in-app chat never appears. "Starter" appears once, in "without assuming starter vocabulary". `--add-addon`, scheduled tasks, and module actions never appear. "Experience" appears only in the sense of an "integrated experience" during installation. SPEC-0080 makes the starter gap deliberate. Its Requirements say the skill "MUST keep starter-specific names, spec/effort terminology, and starter-owned recipe ids out of its base body and references", and its Non-Goals exclude "naming bundled starters".

## 2. Seven user questions against the routing table

The trigger works for any question that names Rhizome. The managed block tells the agent to load the skill "when the request names Rhizome", and `SKILL.md` repeats this. The failure comes after loading: the routing table has no row for explaining Rhizome, choosing among mechanisms, or choosing starters. A question that does not name Rhizome, such as "what can I do here?", depends on context. In the desktop chat, the system prompt says the assistant is "inside Rhizome", and each turn can start with a `[Rhizome view] <kind> "<title>"` block (`pkg/app/agentchat/view_context.go`).

| Question | Route found | Verdict | What is missing |
| --- | --- | --- | --- |
| "What is Rhizome?" | None. The nearest row, `onboarding.md`, orients to the project's own content, not to Rhizome. | No answer | A product model covering typed Markdown, the index and graph, views and experiences, agent surfaces, the desktop app, and starters. The only descriptions shipped are the managed block's single sentence and the chat prompt's "the knowledge base app". |
| "What can I do here?" (in the app, looking at a view) | None. `views.md` describes the bundled Briefing, Overview, Trace, and Sections views in detail, but only for configuring them, and no row sends this question there. | No answer | A map from the current surface (type collection, group, node, All notes, experience) to what the user can ask for or change. |
| "Set up my repo" | `installation-and-integration.md`, then `configuration.md` | Mechanics only | The installer, mise, consent through `rzm init --check`, and verification layers are covered well. Not covered: the desktop setup path, how to choose a workflow and add-ons, and what to do first after setup. `configuration.md` mentions `--workflow` only as a flag. The desktop path is documented in `docs/reference/guides/getting-started.md`, which is not shipped downstream. |
| "Which starter should I use?" | None | No answer | SPEC-0080 bars naming starters. Live data exists: `rzm init --check --json` returns `workflows` (`agentic-engineering`, `domain`, `none`, each with a label, a description, and `recommended`) and `addons` (`SetupPlan` in `pkg/app/cli/init/first_run_json.go`). However, `addonOptions` skips starters marked `optionalAddon`, so `compounding` and `ao-kb` never appear there, and no skill points at this surface. No core skill mentions `--add-addon`, `--remove-addon`, or the Add-ons settings; `--eject` appears only in `custom-views/references/experiences.md`. `docs/reference/guides/choosing-your-starter.md` is not shipped downstream. |
| "Add a pipeline view" | `views.md` for native views, `custom-views` for custom ones, and `ontology-authoring.md` for lifecycle stages | Mechanics without a decision | The mechanics are thorough: the generated Board for workflow-shaped types, `@view(stage:)`, `replaceGenerated`, and `variants.kanban.laneField`. Missing is the decision path from "pipeline" to three questions: does the type have a lifecycle enum with stages, does the generated Board already serve the need, and should the answer be an authored native view or a custom one? The agent has to assemble that path from three references, about 8k tokens with `SKILL.md`. |
| "Automate X across my vault" | `code-mode.md`, `skill-scripts.md`, `validation-and-repair.md` | Partial | One-off scripted reads and authorized writes are covered, including `--read-write` and plan-only operations. Durable automation has no agent guidance. No core skill covers declaring a module action (`actions/<name>.yaml`) or a scheduled task. Both are documented in `docs/reference/guides/command-line.md` and `docs/reference/guides/scheduled-tasks-guide.md`, and neither guide is shipped downstream. `custom-views` covers only calling an action from a view. |
| "Make my own experience" | `custom-views`, whose description names experiences, then `references/experiences.md` | Answered | `experience.yml`, panels, record pages, Tailwind, and ejecting a starter are covered. Missing is the path from a goal such as "a handbook site for my team" to the types, display group, and views it needs. `docs/reference/guides/build-an-experience.md` covers that path and is not shipped downstream. |

Schema evolution has a route (`ontology-authoring.md`, "Change ontology SDL, migrations, or recipes"), framed as high risk and disciplined about migration. It serves a user who already says "change the schema", but no beginner path leads from "I want to track a due date on these notes" to it.

## 3. Size, token budget, and loading

Token figures are bytes divided by four, which is an approximation.

**Always in context, standalone harness.** Every installed skill contributes its name and description. A default install (the four core skills plus `agentic-engineering` and its default add-on `action-items`, nine skills) has 2,138 description characters, about 530 tokens. The `rhizome` description is 314 characters, `custom-views` 263, `client-harness-builder` 263, and `legacy-codebase-assessor` 418, the longest. The managed blocks for that install total about 870 words, about 1.2k tokens. The desktop chat adds about 90 words to every turn.

**On load.** The `rhizome` router costs about 2.1k tokens. References load one at a time and range from `file-context.md` (1.4 KB, about 360 tokens) to `code-mode.md` (16.6 KB, about 4.2k tokens). `views.md` is 15.0 KB (about 3.7k tokens) and `installation-and-integration.md` is 11.3 KB (about 2.8k tokens). All 21 references together are about 27k tokens. The `custom-views` router is about 4.7k tokens, 2.2 times the `rhizome` router, and its `kit-api.md` is about 10.7k tokens.

**How references load.** Skills use progressive disclosure. A short `SKILL.md` holds a classification table naming `references/<file>.md`, and the agent reads the chosen file with its own file tool. Skills have no include mechanism at run time. Two mechanisms run when `rzm init` renders the templates:

- `expandAgentTemplateEmbeds` (`pkg/app/cli/init/helper_template_embeds.go`) replaces each `![[Name]]` embed with the matching file from `docs/rhizome-md-templates/`. A test asserts that no file in the `rhizome` skill contains `![[`, and no current template uses an embed. The ontology files in `docs/rhizome-md-templates/` are exercised only by `helper_templates_test.go`.
- Starter overlays (`agents/skill-overlays/*.yaml`, `apiVersion: rhizome.skill-overlay.v1`) append fragments at declared `<!-- rzm:skill-slot id="..." mode="extension" -->` markers. Unknown files and slots fail closed. Only `complex-domain` ships overlays, all of them targeting `agentic-engineering`. `skilloverlay.Render` receives core and starter skills together, so a starter could extend a core skill. The `rhizome` skill declares no slots today.

**Constraints a change must meet.**

- Tests in `pkg/app/cli/init/helper_templates_test.go`:
  - `TestCoreRhizomeSkillRoutesToDirectFocusedReferences` pins the exact list of 21 `rhizome` references.
  - `requireReachableSkillBundle` limits each description to 1024 characters and each name to 64, and requires every cited `references/...` path and query-recipe id to exist.
  - `TestCoreSkillCreatorStaysTokenDense` holds `skill-authoring.md` to 40 lines.
  - The `agentic-engineering` router is held to 55 lines, and its phase references to 200 lines combined.
  - The `rhizome` router must contain "Classify, then route" and `rzm agent start --intent`.
- SPEC-0080 requires one base skill named `rhizome`, one primary route, and references loaded only when needed. It bars starter names from the skill and requires the managed block to stay a lean router. It retired `rhizome-onboard`, `rhizome-note-authoring`, `rhizome-ontology`, and `rhizome-skill-creator`. Init still plans removal of skill folders with those names (`retiredCoreSkillNames` in `pkg/app/cli/init/agent_surfaces.go`), so a new skill cannot reuse them.
- `AGENTS.md` scopes the managed-block templates to "how agents use Rhizome tools"; feature, configuration, and troubleshooting docs belong in vault notes. Skill templates are the source of truth, and `.agents/skills` and `.claude/skills` are generated. In this checkout the generated `rhizome` and `custom-views` copies match their templates exactly.
- The guides `docs/reference/guides/Agent Skills - Authoring best practices.md`, `Agent Skills - Anthropic Skills Guide.md`, and `Agent Skills - Claude + Codex.md` ask for a short `SKILL.md` with depth in supporting files, descriptions written around real trigger phrases, metadata-first discovery, testing on each target runtime, and authoring once in the init template tree.
- `loadAllSkillTemplatesWithReport` fails init when a starter skill shares a core skill's name.
- Refresh follows the generated-files ownership rule in `pkg/app/cli/init/CONTEXT.md`. Missing and unedited files are written without asking. An edited file is asked about only in a terminal; otherwise it is kept and listed. A new reference file therefore reaches a repository on its next `rzm init`, but a new routing row in a locally edited `SKILL.md` does not.

## Other findings

- **Product facts are not available downstream.** The human guides (`getting-started.md`, `choosing-your-starter.md`, `build-an-experience.md`, `adapting-rhizome.md`, `command-line.md`, `scheduled-tasks-guide.md`, `using-skills.md`) exist only in Rhizome's repository. Starters ship only their own guides, such as `docs/reference/guides/core-identity.md`, `action-items.md`, and `compounding.md`. `configuration.md` links one GitHub guide for expert fields. A downstream agent has no local source for what Rhizome is or offers.
- **The Claude chat may have no Rhizome skill in shared-only repositories.** `agentFlagModes` in `pkg/app/cli/init/configure_misc.go` turns Claude off for `--agents shared`, so no `.claude/skills` or `CLAUDE.md` block is written, and the desktop setup sheet sends `shared` when no agent is checked (`pkg/app/cli/init/CONTEXT.md`). Inference, not yet checked at run time: a Claude Code chat in such a repository discovers no Rhizome skill and has only the 90-word chat prompt, because Claude Code reads `.claude/skills` rather than `.agents/skills`.
- **Audience-specific skills already ship to every repository.** `legacy-codebase-assessor` and `client-harness-builder` serve assessment and handoff engagements but install unconditionally, adding 681 description characters everywhere. `assumption-tracker` arrives through `action-items`, which `agentic-engineering` activates by default. A core skill for a different audience therefore has precedent, and so does the always-on cost it adds.
- **Every setup choice assumes an engineering workflow.** The `workflowChoices` are Agentic Engineering, Agentic Engineering with domain modeling, and "Search and agent guidance only". A notes-only knowledge vault has no matching choice beyond `none`.
