---
type: ReferenceDoc
summary: "Evidence catalog of Rhizome's six built-in starters: what each installs, how they compose, the skills they ship, how each is used day to day, and where starter docs disagree with the code."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Starter catalog

This note records what each built-in starter under `pkg/app/cli/init/templates/starters/` installs and how it is meant to be used, as evidence for the Rhizome usage skill council. It is research, not a design.

Evidence base: the template tree and the init Go code in `pkg/app/cli/init/` at commit `395747786`, and read-only `rzm init --check --json` plans produced by the locally built `bin/darwin/rzm` (reports v0.52.1) in a throwaway Git repository that was deleted afterwards. Statements marked **Verified** were checked against code or a probe. Statements marked **Inference** are reasoning from that evidence.

## How starters compose

**Verified.** Each starter directory has a `template.yaml` read by `loadStarterTemplateMetadataRegistry` in `pkg/app/cli/init/template.go`, and installs by folder (`pkg/app/cli/init/templates/starters/README.md`):

| Source under `<id>/` | Installs to |
| --- | --- |
| `repo/**` | The same path in the target repository. Team-owned and create-only on reruns. |
| `module/**` | `.rhizome/modules/<id>/`: `ontology/`, `recipes/`, `views/`, `tasks/`, `scripts/`, `actions/`, plus files scripts read. |
| `agents/skills/<name>/**` | `.agents/skills/<name>/`, mirrored to `.claude/skills/<name>/` when Claude Code is enabled. |
| `agents/AGENTS.md` | The starter's managed block in `AGENTS.md` and `CLAUDE.md`. |
| `agents/skill-overlays/*.yaml` | No file. Patches named slots in another starter's skills before install. |

| Starter | `requires` | `activatesByDefault` | `optionalAddon` | `description` in template.yaml | Hooks |
| --- | --- | --- | --- | --- | --- |
| `core` | none | none | no | absent | none |
| `action-items` | `core` | none | no | present | none |
| `agentic-engineering` | `core` | `action-items` | no | present | none |
| `complex-domain` | `agentic-engineering` | none | no | absent | none |
| `ao-kb` | `core` | none | yes | present | none |
| `compounding` | `core` | none | yes | present | `deliver-shards`, `guard-rules` |

**Verified resolution rules** (`resolveTemplateSet` in `pkg/app/cli/init/template.go`, `pkg/app/cli/init/first_run_options.go`):

- `requires` pulls dependencies in transitively and cannot be disabled.
- `activatesByDefault` adds a default add-on unless `.rhizome/workflows.yml` records it under `addons.disabled`.
- An optional add-on installs only when a person chooses it. The loader refuses a starter that activates an optional add-on by default.
- Add-on ids are the union of default add-ons and optional add-ons: today `action-items`, `ao-kb`, and `compounding`.
- The workflow menu has three choices (`workflowChoices` in `pkg/app/cli/init/first_run_prompts.go`): `agentic-engineering`, `domain` (installs `complex-domain`), and `none`. `--workflow` also accepts raw starter ids; `--workflow core` was probed and works.
- Core skills install for every repository regardless of starter: `rhizome`, `custom-views`, `client-harness-builder`, and `legacy-codebase-assessor`, from `pkg/app/cli/init/templates/skills/markdown/`.

**Verified probe results** (first-run plans with `--search off --agents claude`):

| Options | Starter modules | Skills (each mirrored to `.claude/skills/`) | Starter docs |
| --- | --- | --- | --- |
| `--workflow none` | none, not even `core` | 4 core skills | none |
| `--workflow core` | `core` | 4 core skills | `core-identity.md` |
| `--workflow agentic-engineering` | `core`, `action-items`, `agentic-engineering` | 9 | 30, with 188 files in total |
| `--workflow domain` | adds `complex-domain` | 17 | 37 |
| `--workflow agentic-engineering --addons compounding` | `core`, `agentic-engineering`, `compounding` | 8 | `action-items` is dropped |
| `--workflow none --addons ao-kb` | `core`, `ao-kb` | 5 | `ao-kb.md`, `core-identity.md` |

Every plan also writes `.rhizome/config.yml`, `.rhizome/workflows.yml`, `.rhizome/ignore`, `AGENTS.md`, `CLAUDE.md`, and `.claude/settings.json` (sandbox loopback permission).

## `core`: identity base

- **Purpose and target user.** The accountable-human layer every other starter depends on. No one chooses it alone except for a custom workflow or minimal integration.
- **Installs.** `Person` type (`module/ontology/core.graphql`); recipes `runtime-authoring-context` and `runtime-schema-capability-pack`; guide `docs/reference/guides/core-identity.md`; a "Core Identity" managed block. No skills or views.
- **Day to day.** Create a tracked `Person` note (under `people/` or `docs/people/`), then `rzm agent current-user set "<Person title>"` and `rzm agent current-user validate`. The current user lives in ignored `.rhizome/agent/user.yml`.
- **Needs the user.** Which `Person` is the current user. Every starter's guidance forbids inferring it from chat, OS user, Git author, or vault name.
- **Observation.** The shipped guide's `Person` example uses a real maintainer's name, which installs into downstream repositories.

## `action-items`: commitments with owners and dates

- **Purpose and target user.** Lightweight tracking of commitments found anywhere in notes. Default add-on of Agentic Engineering, so most repositories get it implicitly.
- **Installs.** `ActionItem` type; recipes `all-action-items`, `my-action-items`, `assigned-action-items`, `action-items-in-note`, `assessment-assumptions`, `action-items-for-context`; the Action Items view (defaults to open items, with a Mine filter bound to the current user); guide `docs/reference/guides/action-items.md`; managed block; skills `action-items` and `assumption-tracker`.
- **Skills.** `action-items` creates, queries, updates, and completes `#action-item` checkboxes with `assignee::` (must resolve to a `Person`) and optional `due:: YYYY-MM-DD`, validating after field edits. `assumption-tracker` flags uncertain findings while documenting a legacy codebase as typed `#assumption/<type>` action items and synthesizes them into kickoff questions.
- **Day to day.** Write commitments where they arise; ask the agent for "my action items" after current-user validation; use the view.
- **Observation (Verified).** The legacy-assessment chain in `docs/reference/guides/playbook-legacy-codebase-assessment.md` is `legacy-codebase-assessor` → `assumption-tracker` → `client-harness-builder`. Two of those are core skills, but `assumption-tracker` ships only with `action-items`, so `--workflow none` leaves the chain incomplete. The playbook works around this by recommending Agentic Engineering or `action-items` alone.

## `agentic-engineering`: the delivery harness

- **Purpose and target user.** A code repository with delivery work: intended behavior, bounded efforts, plans, verification evidence, and one route for coding agents. Described as "a repository harness, not a complete substitute for a team's process." It is the recommended first-run choice.
- **Ontology.** `module/ontology/spec-driven.graphql`: `SpecLike` specs (`ProductSpec`, `TechnicalSpec`, `ExperienceSpec`, `OperationsSpec`, `ProcessSpec`), `UserStory`, `AcceptanceCriterion`, `FeatureArea`, the `Effort` interface (`EffortNote`, `EffortWorkspace`, `EffortMaterial`), `ReferenceDoc`, and status and kind enums.
- **Recipes.** `open-efforts`, `note-by-path`, `effort-execution-context`, `frozen-spec-index-pack`, `frozen-spec-detail-pack`, `story-acceptance-pack`, `closure-drift-pack`, `runtime-code-evidence-pack`, `runtime-note-code-evidence-pack`, `topic-typed-survey`.
- **Views.** Specs, User stories, Efforts, and Feature areas, plus the **Delivery** experience (`module/views/delivery/`): a home page, stage pages (shaping, ready, planned, active, history), per-kind spec pages, effort pages, and a sidebar panel. Each page's **Copy workflow prompt** copies the prompt that continues the work; Delivery never starts an agent.
- **Repository scaffolds.** `docs/engineering/` (README plus `architecture.md`, `documentation.md`, `quality-gates.md`, `release.md`, `review-and-approval.md`, `testing-policy.md`); `docs/specs/{product,technical,experience,operations}/`; `docs/efforts/` with effort and workspace templates; `docs/feature-areas/`, `docs/meetings/`, `docs/reference/{analysis,architecture,guides}/`; guides `harness-capabilities.md`, `ontology-driven-transcript-ingestion.md`, `spec-driven-query-bundle.md`.
- **Skills.** `agentic-engineering` is a phase router whose first argument selects one reference: classify (default), specify, effort, plan, implement, gates, align, reconcile, compound, finish. `foundation-review` settles formative decisions one at a time when a plan asks for it. `ingest-transcript` turns provenance-bearing sources into durable notes, treating source text as data, and exposes skill slots that `complex-domain` fills.
- **Managed block.** A route table (clear local task → `implement`; unclear → classify; define behavior → `specify`; bounded multi-phase work → `effort` then `plan`; close → `finish`), precedence (user request, then `docs/engineering/`, then skill defaults), and the Agent Partnership and Agent Delegation modes.
- **Day to day.** Describe the task; the router classifies it. Small local work goes straight to implementation. Larger work is shaped into a spec, frozen into an effort, planned, implemented, and closed, with the Delivery experience showing where each piece stands.
- **Needs the user.** Real commands in `docs/engineering/quality-gates.md` (a bracketed placeholder is a setup gap the skill must not fill by invention), review expectations, plan approval, and completion truth.

## `complex-domain`: source-backed requirements

- **Purpose and target user.** Products whose behavior depends on regulations, contracts, research, operational rules, or another source-backed domain. It implies Agentic Engineering and adds an upstream evidence layer; specs remain the delivery contract.
- **Ontology.** `DomainContext`, `DomainType`, `DomainProcess`, `UserWorkflow`, `RequirementSource`, `Requirement`, and enums for lifecycle, review, confidence, requirement status, kind, priority, and source kind. `FeatureArea` comes from Agentic Engineering.
- **Recipes.** `domain-inventory-pack`, `domain-topic-survey`, `domain-context-pack`, `requirement-trace-pack`, `spec-domain-context-pack`, `changed-domain-impact-pack`, `feature-area-backlog-pack`, `coverage-gap-pack`, `sources-needing-review-pack`.
- **Views.** Domain types, Requirements board, Requirements by feature area, Requirements by process, Sources needing review, Uncovered requirements.
- **Scaffolds.** `docs/reference/domain/{contexts,processes,types,workflows}/`, `docs/reference/requirements/{requirements,sources}/`, guide `docs/reference/guides/complex-domain-workflow.md`.
- **Skills.** `complex-domain` routes to `requirements-ingest` (sources to atomic candidate requirements), `requirements-curation` (accept, reject, deduplicate, refresh), `domain-modeling` (feature areas, contexts, types), `workflow-mapping` (processes and actor workflows), `spec-from-domain` (before `agentic-engineering specify`), `traceability-review` (coverage and drift), and `domain-backport` (during `reconcile`). Eight overlays patch Agentic Engineering's alignment, compound, effort setup, implement, ingest-transcript, plan, reconciliation, and specify guidance.
- **Day to day.** Ingest sources as candidates, curate them, ground specs in accepted requirements, and review traceability as delivery lands.
- **Needs the user.** Accepting a requirement and any lifecycle change; provenance, confidence, and similarity never authorize acceptance.

## `ao-kb`: an existing AO KB in Rhizome

- **Purpose and target user.** A repository that is already an AO KB, the knowledge-base format maintained by AO's own toolkit and its `/ao-kb:process`, `/ao-kb:curate`, and `/ao-kb:suggest` skills. It is a thin reader: Rhizome browses, traces, and edits the knowledge, while AO's tools keep authoring and publishing. The vault root must be the AO KB root (`.kb/config/vault.md`, the account root for a multi-product KB). schemaVersion 8 is fully supported; a single-product KB at 4 through 7 sets up knowledge-only.
- **Ontology.** `Assertion` (with sources, citations, connections), `Source`, `Concept`, `Artifact`, `ConflictRegistry`/`ConflictEntry`, `MapOfContent`, `Submission`, `CurationDocument`; refresh adds generated `ao-knowledge` and `ao-model` schemas.
- **Recipes.** `ao-kb-claim-to-source`, `ao-kb-reverse-impact`, `ao-kb-concept-evidence`, `ao-kb-artifact-evidence`, `ao-kb-model-to-evidence`.
- **Views.** The Knowledge experience: KB overview, Review, Concepts, Assertions, Sources, Artifacts, Attention, Knowledge trace, record pages, and a Knowledge sidebar panel. Labels have exact meanings defined in the guide.
- **Actions, scripts, lifecycle.** Actions `check`, `refresh-bindings`, `index`, `readiness`, `read-evidence`; scripts behind them; a binding compiler in `module/bindings/` (Node, plus Python through `uv` for the model stage) that runs only through `refresh-bindings`. `module/lifecycle.yml` makes it the only starter with runtime automatic setup under SPEC-0138 (`docs/specs/product/starter-readiness.md`).
- **Skill.** `ao-kb-setup`: install, trust, check, includes, ownership, refresh bindings, index, then report three states separately: starter installed, bindings ready, index ready.
- **Day to day.** Browse the Knowledge site, edit notes in Rhizome, use AO's skills for knowledge changes, and refresh bindings after metamodel, configuration, or product-selection changes.
- **Needs the user.** `rzm trust` for module actions; the product choice in a multi-product KB; whether to retire or rename colliding local types such as lowercase `assertion`; installing `uv` for the model stage.

## `compounding`: learning from agent sessions

- **Purpose and target user.** A repository where people already use Claude Code or Codex heavily and want recurring friction turned into approved fixes, document edits, guard rules, or context shards. Optional and never on by default.
- **Installs.** `ContextShard` and `AgentFinding` types; recipe `context-shards`; views Agent findings, Context shards, and a review workspace; actions `recommendations`, `item`, `prompt`, `dismiss`, `hide`, `health`, `shards-check`; scheduled task `compounding/mine` (`every: 7d`, `scope: person`, `--task-mode --budget 10`); scripts `mine`, `lookup`, `shards`, `report`, `sessions`, `calibrate`, `hook`, and others; `models.json`; guide `docs/reference/guides/compounding.md`. Approved output lands in `docs/memory/context-shards/` and `docs/memory/agent-findings/`.
- **Skill.** `compounding`: lookup before unfamiliar edits, mine (a free dry run first, then a person-approved budget), review and approve, re-verify stale shards, and report friction. Hooks (`deliver-shards`, `guard-rules`) stay off until `rzm init --hooks on`.
- **Needs the user.** Node 20 or later; a TypeSafe credential for Jev screening (or an explicit unscreened run); a spend budget; a `Person` for approval records; approval of every proposal; turning hooks on and trusting entries in Codex with `/hooks`; agreement that transcript text goes to the harness's model provider and Jev.
- **Observation (Verified).** Findings carry `#action-item` items "so the action-items queries list them," but compounding requires only `core`. A first run with `--addons compounding` drops `action-items`; the compounding guide's own example is `--addons action-items,compounding`.

## Docs that disagree with the code

- `docs/reference/guides/choosing-your-starter.md` (last verified 2026-10-01) omits `ao-kb` entirely; `ao-kb` is also absent from `README.md`, `docs/reference/guides/getting-started.md`, and `docs/reference/guides/using-skills.md`. It is documented only in the guide the starter installs and in specs.
- The same guide says Agentic Engineering installs "manual adapters for specification, effort setup, planning, implementation, foundation review, transcript ingestion, and closure". The starter now ships one phase router plus `foundation-review` and `ingest-transcript`; the phrase predates the starter-skill consolidation (**Inference** from `docs/efforts/2026-09-05-11-42-starter-skill-consolidation.md` and the current tree).
- The guide does not warn that `--addons` replaces the default list; only the compounding guide does.
- `docs/reference/guides/Agentic Engineering starter - adoption guide.md` (last verified 2026-07-19) omits the Delivery experience and the default `action-items` add-on.
- The comment on `loadSkillTemplates` in `pkg/app/cli/init/helper_templates.go` names core skills ("ontology, note-authoring, onboarding, skill-creator") that no longer exist.
- The core `rhizome` skill's `references/onboarding.md` means orienting an agent to an unfamiliar codebase area, not onboarding a person to Rhizome. A usage skill that calls its work "onboarding" will collide with that route name.
