---
type: TechnicalSpec
summary: "Defines rzm init's template architecture: embedded source trees, lean managed guidance, consolidated core skills, managed fences, starter scaffold refresh, starter management state, dependency-aware ejection, the ownership record, and collision semantics."
id: SPEC-0039
spec-status: active
last-updated: 2026-10-01
aliases:
  - SPEC-0039
  - init-template-architecture
---

# Init Template Architecture

## Summary

Init templates are embedded source artifacts compiled into the CLI. Runtime init code expands those sources into repo-local config, managed agent docs, command/prompt files, skills, starter docs, starter ontology schemas, query recipes, and configured views through one ownership rule. The architecture must keep ownership explicit: generated Rhizome content lives inside managed fences, starter-managed asset families, or `rhizome-*` artifacts, while user-owned repo content remains outside those boundaries. A tracked record of what Rhizome last wrote lets reruns update untouched files silently and ask only about edited ones; dependency-aware ejection removes whole starters from management.

## Goals

- keep template source paths discoverable and reviewable in the repo
- separate core Rhizome guidance from workflow-starter guidance
- keep universal Rhizome behavior in one reference-backed core skill and the managed agent-doc block lean
- make starter-doc refresh intentional instead of silently rewriting user-edited docs
- report starter update availability without making local process-doc updates automatic
- model starter management and ejection at starter granularity
- record what Rhizome last wrote so unedited files update silently and a declined version is never offered twice
- fail loudly when templates collide by target path or skill name

## Non-Goals

- runtime behavior changes outside `rzm init`
- arbitrary filesystem cleanup of non-Rhizome files
- a plugin marketplace for workflow templates
- semantic merging of markdown beyond the existing managed-block and hunk-diff workflow
- automatic three-way merge of local starter docs against previous and current embedded templates

## Requirements

### Template sources

- The core managed agent-doc block MUST be loaded from `docs/rhizome-md-templates/RHIZOME.md`; other files in that directory MUST remain only when they are genuinely shared embedded fragments rather than shadow manuals for installed skills.
- Agent command/helper templates MUST be loaded from `pkg/app/cli/init/templates/commands/*`.
- Codex and Claude prompt target directories MUST currently receive that same command/helper template family; the current runtime does not load a separate `pkg/app/cli/init/templates/prompts/*` source family.
- Core skill templates MUST be loaded from `pkg/app/cli/init/templates/skills/markdown/*`.
- Universal Rhizome operating guidance MUST be owned by `pkg/app/cli/init/templates/skills/markdown/rhizome/SKILL.md`, with canonical reference bodies directly under its `references/` directory rather than one-line transclusions into `docs/rhizome-md-templates/`.
- Each starter directory `pkg/app/cli/init/templates/starters/<template>/` MUST group its assets by install destination: `template.yaml` (metadata, not installed), `repo/` (the target repository root), `rhizome/` (the target `.rhizome/` directory), and `agents/` (agent harness surfaces).
- Starter scaffold files MUST be loaded from `repo/**` and installed at the same relative path in the target repository.
- Starter managed agent docs MUST be loaded from `agents/AGENTS.md`.
- Starter skill templates MUST be loaded from `agents/skills/*`, and starter skill overlays from `agents/skill-overlays/*`.
- Starter ontology schemas MUST be loaded from every `*.graphql` file under `rhizome/ontology/` and installed under `.rhizome/ontology/` preserving each schema's relative filename (for example `spec-driven.graphql`), so schemas from multiple selected starters merge rather than overwrite a single shared `schema.graphql`.
- Starter query recipes and views MUST be loaded from `rhizome/query-recipes/` and `rhizome/views/` and installed under `.rhizome/query-recipes/` and `.rhizome/views/`.

### Managed blocks

- The core Rhizome agent-doc block MUST be delimited by `<!-- BEGIN RZM INIT RHIZOME BLOCK -->` and `<!-- END RZM INIT RHIZOME BLOCK -->`.
- Each workflow template agent-doc block MUST be delimited by `<!-- BEGIN RZM INIT TEMPLATE BLOCK: <template> -->` and `<!-- END RZM INIT TEMPLATE BLOCK: <template> -->`.
- Rendering MUST strip legacy Rhizome fences and legacy generated `RHIZOME.md` redirects before adding current managed blocks.
- Rendering MUST preserve existing content outside managed fences.
- Template managed blocks MUST be keyed by normalized workflow template name.

### Refresh and ownership workflow

- Every Rhizome-owned file (managed agent-doc blocks, core and starter skill files, harness command files, starter ontology schemas, query recipes, and views) MUST be written through one ownership updater.
- The updater MUST create missing files without prompting.
- The updater MUST record the SHA-256 of the content it writes, with line endings normalized to LF before hashing, keyed by project-relative path (and by block id for managed blocks), in the tracked `.rhizome/generated-files.yml`. Starter docs are recorded when created even though ordinary reruns never update them.
- Skill folders MUST contain only skill content. Ownership of a skill folder and pruning of files a newer template dropped MUST come from `.rhizome/generated-files.yml`; a dropped file is deleted only when it matches its record. The first run under this contract MUST fold existing `.rhizome-managed` markers and `.rhizome-managed-files` manifests into the record and delete them.
- `.rhizome/generated-files.yml` MUST hold only generated-file records, and MUST be written with sorted keys and one entry per line so concurrent branches merge cleanly; an unreadable or conflicted record MUST be treated as absent (falling back to the grouped decision) rather than failing the run.
- When on-disk content matches the recorded hash, the updater MUST replace it without prompting. When it matches the current render, the updater MUST only refresh the record.
- When on-disk content matches neither, the file is edited: interactive runs MUST offer take the update, keep my version (default), or show the diff; batch runs MUST keep it and report it. Keeping MUST record the declined render's hash so the same version is not offered again. Mirrored copies of one skill MUST share one decision.
- A run without a terminal MUST keep edited files and apply everything else; `--check` MUST plan without writing.
- A file with no record that differs from the current render MUST be treated as edited, and a run MUST group all such files into one decision before any per-file prompt. A managed block with no record is the exception: its fences already mark it as Rhizome's, so it updates like an unedited file.
- Starter docs MUST be create-only on ordinary reruns and MUST follow the rule above only when `--refresh-docs` is active.
- Reruns MUST compute the change list (missing, updatable, edited, newly detected content) before any write; `--check` MUST report it without writing and exit with status 1 when it is not empty.
- `.rhizome/template-rejections.yml`, hunk rejections, per-file and global always/never preferences, and starter family update policies MUST NOT be read; the first run under this contract deletes `.rhizome/template-rejections.yml`.
- Only after a core-skill refresh installs `rhizome`, it MUST remove managed directories for the retired core skills `rhizome-onboard`, `rhizome-note-authoring`, `rhizome-ontology`, and `rhizome-skill-creator` when their files are unedited; it MUST NOT remove edited predecessors or other user-owned skills, or leave redirect wrappers.
- Removing a workflow MUST remove its skills, managed blocks, saved queries, and views only when unedited (edited ones get the edit check), MUST keep its starter docs and ontology schema, and MUST NOT touch an ejected workflow's files. Inside a skill folder the record owns, only recorded files are removal candidates.
- Retired skills MUST be removed without asking once their replacement is installed: retired `rhizome-*` names are owned by name, other retired names need the record or a legacy marker, and a recorded file someone edited keeps the edit check.
- Ontology schemas, query recipes, and views SHOULD trigger a validation hint after updates are applied.

### Starter management state and dependency closure

- `.rhizome/workflows.yml` `templates` remains the record of explicit workflow starters the repo has adopted.
- Required dependencies from `template.yaml` are resolved from metadata and are not duplicated into `templates` unless the user explicitly selects them.
- Optional default addons are distinct from required dependencies and may be enabled, disabled, or ejected independently. When an addon is managed only because it was activated by a starter being ejected, the ejection planner defaults that addon to ejected.
- Starter management and ejected-starter state MUST live in `.rhizome/workflows.yml`, separate from runtime settings in `.rhizome/config.yml` and from the ownership record in `.rhizome/generated-files.yml`. Existing `management.updatePolicy` and `management.sourceFingerprints` keys MUST be removed by the first run under this contract.
- Ordinary reads of `.rhizome/config.yml` and `.rhizome/workflows.yml` MUST treat blank/comment-only files as empty mappings and otherwise use one exact-one-document decoder. Reads are side-effect-free: unknown keys return path-aware warnings and remain preserved, while malformed YAML and known-field type mismatches fail. The bounded `rzm init` migration moves only the three retired v0.49 workflow fields into `.rhizome/workflows.yml`, updates tracking state, and supports idempotent retry.
- Every init path that creates or rewrites `.rhizome/config.yml` MUST serialize that file with industry-standard two-space YAML indentation and deterministic formatting across the whole file; init MUST NOT introduce invalid YAML or indentation-only churn relative to the canonical repo-local config writer.
- Init MUST be able to render the starter graph as:
  - explicit roots,
  - optional enabled addons,
  - optional disabled addons,
  - required transitive dependencies,
  - managed effective starters,
  - ejected starters.
- Ejection state MUST be stored durably so later `rzm init` reruns can explain that a starter was installed but is no longer managed.
- Noncanonical workflow state that predates the current ejection or addon-origin contract MUST be repaired explicitly by the maintainer; ordinary reads and init startup MUST NOT infer or persist a migration.

### Ejection semantics

- Ejection is a reconfigure-time operation, not a first-run install option.
- Ejecting a starter MUST keep installed files on disk and stop future updates and update notifications for every asset family owned by that starter.
- Ejecting a starter MUST leave existing managed blocks in agent docs untouched, including Rhizome fence comments; ejection freezes that content in place instead of stripping or rewriting it.
- Ejection MUST operate on the resolved dependency graph rather than deleting a single config string.
- Ejecting an explicit starter removes or marks that starter unmanaged, then recomputes dependency closure.
- Required transitive dependencies that are no longer required and were not explicitly selected remain on disk but stop being managed.
- Required dependencies that are still needed by any managed starter cannot be ejected unless the user chooses a cascading ejection of every dependent starter.
- Optional default addons are not required dependencies; the ejection preview MUST show them separately and default to ejecting addons that are only active through the ejected starter.
- Restoring management for an ejected starter MUST route through the starter update review path before writing changed assets.

### Collision semantics

- Unknown workflow template names MUST return an error before writes begin.
- Duplicate workflow template names MUST normalize to one entry.
- Starter scaffold target-path collisions MUST be rejected unless the two templates produce byte-identical content for the same target.
- Core skill and starter skill name collisions MUST be rejected.
- A skill template MUST contain `SKILL.md`.
- Markdown skill template files MUST not be empty after trimming.
- Non-markdown skill template files MUST not be empty and scripts under `scripts/` MUST remain executable when installed.

### Optional scaffold families

- A workflow template's starter ontology schemas MAY be installed under `.rhizome/ontology/` (one file per `*.graphql`, keyed by its filename) when the starter owns a `rhizome/ontology/` family.
- A workflow starter MAY omit `agents/AGENTS.md`, `agents/skills/`, `agents/skill-overlays/`, or any `rhizome/` family; the loader should treat missing optional starter families as absent rather than fatal.
- MCP config scaffolding MUST stay absent unless a future spec explicitly reintroduces it.

### Agentic-engineering workflow surface

- The canonical starter directory and metadata id MUST be `agentic-engineering`; legacy `spec-driven` input MUST normalize through an explicit migration before ordinary starter resolution.
- Starter identity migration MUST include workflow selections, ejected state, dependency references, managed-starter fence ids, install inference, stale-key pruning, and byte-identical legacy-fence preservation for ejected starters.
- The starter MUST own one lean `agentic-engineering` router whose phases are selected by argument and whose references are self-contained. Separate starter skills exist only for distinct workflows that are not router phases (`foundation-review`, `ingest-transcript`); skill bundles MUST NOT import files from sibling skills.
- The router and retained workflow skills MUST preserve stable overlay extension slots used by dependent starters, and all complex-domain overlays targeting retired skills MUST be retargeted or explicitly retired.
- Core `rhizome` guidance MUST own universal retrieval, validation, safe mutation, documentation-binding, and structural-analysis mechanics; starter resources MUST compose with those routes rather than duplicate them.
- Team-owned process extension docs MUST be plain Markdown under `docs/engineering/`, MUST be factored by recurring engineering concern rather than workflow phase, MUST remain create-only after installation, MUST exclude schema-enforced note mechanics, and MUST NOT be overwritten by a requested doc refresh when their on-disk content differs from every shipped fingerprint unless the user confirms that file.
- Legacy process-doc retirement MUST use a checked-in historical sha256 catalog covering every shipped version of each legacy path; only catalog matches MAY be removed after replacement installation, while modified, uncataloged, and otherwise unproven files MUST remain reviewable with a sentinel-guarded retirement notice and tracked Markdown manifest entry.
- `.rhizome/migrations/**` MUST remain trackable through the managed `.rhizome/.gitignore` so migration reconciliation can be reviewed by the team.
- Domain-accurate `spec-driven.graphql` and `spec-driven.yaml` asset names MUST remain stable unless their modeled domain changes.

## User Stories

### US1 - Find the source template path, ownership boundary, and refresh mechanism for every init-installed artifact
- id:: ^SPEC-0039-US1
- summary:: Find the source template path, ownership boundary, and refresh mechanism for every init-installed artifact.
- status:: ready

The architecture should make template provenance boring to verify: an engineer can trace a repo artifact to an embedded source path and know whether updates are managed by fences, diff hunks, create-only starter docs, or skill installation rules.

#### Acceptance Criteria

- `pkg/app/cli/init/CONTEXT.md` and init guides list the source-of-truth template paths. ^SPEC-0039-US1-AC1
  - The init module context and guide docs identify core Rhizome guidance, command-backed helper artifacts, prompt target reuse of those helpers, core skills, starter scaffold files, starter managed docs, and starter skills as distinct source families.
  - Embedded template paths are described in repo-relative terms so agents can navigate to the source without guessing package internals.
  - Source-family descriptions include the update mechanism that owns installed files: managed fence, ownership updater, or create-only scaffold.
- Code anchors attach this technical spec and init references to init orchestration/template files.
  - Init entrypoints and policy helpers include coderefs to the smallest relevant spec, story, or acceptance-criteria node.
  - Rationale comments explain non-obvious ownership decisions such as managed-block replacement, create-only docs, collision rejection, and skill/template family separation.
  - Running `rzm agent file-context` against init orchestration/template files surfaces this spec or the relevant init reference docs.

### US2 - Add a starter by creating embedded template directories without accidentally shadowing existing skills or colliding with another starter's files
- id:: ^SPEC-0039-US2
- summary:: Add a starter by creating embedded template directories without accidentally shadowing existing skills or colliding with another starter's files.
- status:: ready

Adding a starter should fail at the boundary where ownership becomes ambiguous. Optional families can be absent, but duplicate artifact ownership cannot silently continue.

#### Acceptance Criteria

- Name and path collisions fail before partial starter writes.
  - Unknown starter names are rejected during normalization before scaffold writes.
  - Duplicate normalized starter names collapse to one selection.
  - If two selected starters write different bytes to the same target path, init fails before writing that target.
  - Starter skill names that collide with core skills or earlier starter skills fail before the colliding skill is installed.
- Optional starter families can be omitted without failing the whole template load.
  - Missing `agents/AGENTS.md` yields no starter managed block rather than a fatal error.
  - Missing `agents/skills/` yields no starter skills rather than a fatal error.
  - Missing optional starter ontology schemas are ignored, while empty present templates remain errors.
  - Generic starter scaffold walking reads only `repo/`, so optional family absence and family-specific validation stay independent.

### US3 - Manage starter update policy and ejection from the resolved starter graph
- id:: ^SPEC-0039-US3
- summary:: Manage starter update policy and ejection from the resolved starter graph.
- status:: ready

Starter maintenance should follow the same metadata-driven graph as starter installation. An engineer should be able to inspect a config and know which starters are explicit, which are required, which are optional addons, which are ejected, and which asset families Rhizome is still allowed to update.

#### Acceptance Criteria

- The resolver exposes enough cause metadata for update and ejection UX. ^SPEC-0039-US3-AC1
  - Resolution identifies explicit starters, enabled addons, disabled addons, required transitive dependencies, default-addon candidates, managed effective starters, and ejected starters.
  - Existing tests cover `agentic-engineering -> core`, `agentic-engineering -> action-items` as a default addon, `project-kb -> core`, `project-kb -> action-items`, `complex-domain -> agentic-engineering -> core`, and direct `action-items -> core`; migration tests separately cover legacy `spec-driven` input.
  - The implementation does not hard-code starter names beyond test fixtures and embedded starter metadata.
- The change planner classifies every Rhizome-owned target before writing. ^SPEC-0039-US3-AC2
  - Each target is classified as missing, current, updatable (unedited), or edited, using the ownership record and the current render.
  - The planner reports starter-doc changes only when `--refresh-docs` is active.
  - The plan is the same for interactive runs, non-terminal runs, and `--check`; only interactive runs may change an edited file's outcome before applying.
- Ejection is graph-safe. ^SPEC-0039-US3-AC3
  - Attempting to eject `core` while any managed dependent requires it returns a structured blocked result that names the dependents.
  - Attempting to eject `agentic-engineering` while `complex-domain` is managed returns a structured blocked result unless `complex-domain` is ejected in the same request.
  - Ejecting `agentic-engineering` in a repo where `core` was explicitly selected leaves `core` managed.
  - Ejecting `complex-domain` leaves `agentic-engineering` managed only when `agentic-engineering` was explicit or otherwise still required.
  - Optional default addons are handled by explicit keep/disable/eject choices rather than being silently treated as required dependencies, with eject as the default when the addon is only active through the ejected starter.
- Restoring management returns files to the ownership rule. ^SPEC-0039-US3-AC4
  - A restored starter's targets appear in the change list before any write.
  - Local files modified while ejected count as edited.

### US4 - Refresh universal Rhizome guidance as one managed skill without leaving stale predecessors
- id:: ^SPEC-0039-US4
- summary:: Refresh universal Rhizome guidance as one managed reference-backed skill while preserving user-owned artifacts and removing only known retired managed predecessors.
- status:: satisfied

#### Acceptance Criteria

- The installed core guidance has one `rhizome` skill whose reference files are rendered from canonical files beside its `SKILL.md`. ^SPEC-0039-US4-AC1
- Skill refresh removes only the four known retired managed core skill directories after the replacement skill is installed and only when their files are unedited; unrelated and user-owned skills are never deleted. ^SPEC-0039-US4-AC2
- The managed `RHIZOME.md` block remains a small router; operational procedures live in the installed skill and its references. ^SPEC-0039-US4-AC3
- Template tests prove fresh install, accepted refresh, declined refresh ordering, retired-directory cleanup, reference installation, and identical output across enabled agent skill mirrors. ^SPEC-0039-US4-AC4

### US5 - Install and migrate a lean agentic-engineering workflow surface
- id:: ^SPEC-0039-US5
- summary:: Install and migrate a lean router-and-resources workflow surface under one canonical starter identity without losing team-owned process guidance.
- status:: ready

The technical architecture should keep ambient context small while retaining phase-specific rigor. Universal Rhizome mechanics belong to the core skill; the workflow router owns Agentic Engineering sequencing; phase adapters own only their manual invocation boundary.

#### Acceptance Criteria

- Skill ownership is explicit and mechanically closed. ^SPEC-0039-US5-AC1
  - `agentic-engineering` is a lean router whose references contain workflow state, specification, effort setup, planning, implementation, quality gates, alignment, reconciliation/backport, compounding, closure orchestration, and a separate closure report contract.
  - Phases are router arguments; only `foundation-review` and `ingest-transcript` remain separate, thin workflow skills.
  - Workflow skills hand off by installed skill name and argument (`agentic-engineering specify`) and do not reference sibling skill paths; every installed skill bundle remains self-contained under the existing template closure checks.
  - The router's closure phase preserves the reviewed closure report contract and degraded sequential fallback through lazily loaded router resources rather than preloading every closure resource.
  - The router's references and retained workflow skills declare stable extension slots required by dependent starter overlays; overlay fragments may target a skill-relative Markdown file, and overlay targets, files, and slot ids are validated as a closed installed graph.
  - `development-loop`, `alignment-audit`, `quality-gates-check`, `backport`, `compound`, `debugging`, `rhizome-review-feedback`, `code-docs`, `refactor-planning`, `specify`, `effort-new`, `plan`, `implement`, and `effort-finish` cease to be separate starter skills after their unique durable guidance is reassigned.
- Universal leverage guidance has one owner. ^SPEC-0039-US5-AC2
  - Core `rhizome` references own reusable documentation-binding guidance and the structural-analysis mechanics that improve Rhizome retrieval and code understanding.
  - Starter resources may tell an agent when to use those mechanics but do not duplicate session, search, file-context, exact-evidence, validation, or safe-mutation procedures.
  - Base evidence-authority and composition guidance is independent of starter types, artifact names, and recipe ids. Starters retain workflow semantics and phase context selection; overlays extend those phases without duplicating universal mechanics.
  - Managed AGENTS guidance remains a compact phase table and route list rather than another workflow manual.
- Process extension documents are small, plain, and team-owned. ^SPEC-0039-US5-AC3
  - The canonical starter ships a concise `docs/engineering/` set factored by concern: an index, testing policy, quality gates, documentation, review and approval, architecture, and release, each under roughly 40 lines.
  - Each concern doc states its defaults as editable sentences, names the phases that read it, and ends with a team extension section; schema-enforced note mechanics and ontology contracts stay in managed technical guidance instead of editable local policy.
  - Each phase reads only the concern documents relevant to it, and a concern document may be read from several phases. Managed guidance states the precedence order: user request, then local concern documents, then skill defaults.
- Identity migration preserves all coupled state. ^SPEC-0039-US5-AC4
  - Normalization occurs before unknown-id rejection and rewrites legacy config/workflow selections, dependency references, and managed-starter fence ids to `agentic-engineering`.
  - Ejected starter state migrates to the canonical id while frozen on-disk legacy fences remain byte-identical; rendering preserves both the canonical id and known legacy fence id for that ejected starter.
  - Legacy `spec-driven:*` starter fingerprint keys are removed with the rest of `management.sourceFingerprints`; the ownership record decides future updates.
  - A checked-in historical fingerprint catalog covers every shipped version of each legacy process-doc path. Catalog match proves unchanged content; absent, uncataloged, or mismatched evidence retains the document with a sentinel-guarded retirement notice and tracked manifest entry.
  - A closed retired-starter-skill name list removes the nine retired managed skill directories across enabled harness mirrors only after the replacement router is accepted; user-owned skills remain untouched.
  - Install inference recognizes canonical assets and maps legacy asset evidence directly to `agentic-engineering` without recreating a legacy starter id.
  - Filesystem migration steps are individually idempotent and complete before one atomic `.rhizome/workflows.yml` rewrite commits canonical state; failure before that rewrite leaves legacy state eligible for safe replay.
- Tests cover fresh install, migration, and bounded context. ^SPEC-0039-US5-AC5
  - Template tests cover canonical fresh install, legacy config/workflow input, ejected and dependency state, fence rewriting, fingerprint removal, inference, unchanged-doc deletion, modified-doc retention/manifest generation, idempotent rerun, and identical enabled agent mirrors.
  - Structural tests enforce adapter thinness, bundle-local references, router resource reachability, absence of retired starter skills, and closure loading only the resources required for the requested closure work.
  - Existing `spec-driven.graphql` and `spec-driven.yaml` install exactly once under their domain-accurate filenames, and `complex-domain` resolves through `agentic-engineering`.

## Resolved Questions and Follow-ups

- Prompt target directories continue to reuse `templates/commands/*` until prompt artifacts need distinct source ownership or refresh semantics; see [[Keep prompt targets command-backed until prompt semantics diverge]].
- Embedded starters may continue using directory layout while ownership is fully determined by bundled metadata, source family, and collision checks. Introduce explicit per-file ownership metadata before adding independently maintained, third-party, or overlapping starters.
- Three-way merge for edited files remains a future enhancement. The ownership record stores hashes, not prior contents, so it can tell edited from unedited files but cannot reconstruct or merge an earlier version.
