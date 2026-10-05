---
type: TechnicalSpec
summary: "Defines saved ontology query recipes as durable, intent-described GraphQL query assets that skills, agents, and schema-evolution tooling can validate and adapt."
id: SPEC-0052
spec-status: active
last-updated: 2026-07-16
aliases:
  - SPEC-0052
  - Saved query recipes
---

# Saved query recipes

## Summary

Rhizome already exposes a GraphQL ontology query surface. The missing layer is a durable recipe format that preserves why a query exists, what input it requires, what output shape it promises, and how ontology evolution should adapt it when schema names or structures change.

Saved query recipes should become the bridge between live ontology contracts and reusable agent skills. A skill such as `specify` should be able to ship a "spec landscape survey" query that runs broadly with no anchor before drafting, while a skill such as `generate-artifact` can ship anchored queries that start from a recipe, artifact, source, node locator, or current file. The query still executes through the GraphQL ontology query engine; the recipe makes the query inspectable, reusable, and maintainable.

## Goals

- preserve the human or agent problem each saved query is trying to solve
- support broad no-input queries and anchored queries with explicit input contracts
- let general skill-authoring guidance compose with the opted-in `rhizome` skill-authoring reference to discover, validate, and package query recipes inside skill bundles
- make created skills tell future agents which exact query recipes to run before drafting, editing, planning, or handoff
- expose saved query recipes consistently through non-browser agent surfaces, including the agent CLI surface, in-app agent tools, and MCP tools
- bundle high-value spec-driven query recipes and required discovery patterns into starter templates so agents load frozen scope, related specs, acceptance criteria, and code evidence before acting
- let ontology schema evolution proactively detect affected recipes and adapt them when possible
- require schema evolution to ask the user when the recipe's problem or input contract is not enough to choose a safe adaptation
- keep GraphQL as the execution substrate rather than inventing a second query language
- make saved queries useful to browser workbench views, agent handoffs, and CLI workflows through one shared recipe contract

## Non-Goals

- replacing `rzm agent ontology-query` or the generated GraphQL query schema
- building a full visual GraphQL IDE
- requiring every ad hoc query in chat to become a saved recipe
- hiding query complexity from expert users who want to write raw GraphQL
- making schema migration silently reinterpret user intent when the recipe is ambiguous
- hard-coding project-kb, spec-driven, or other starter-specific query behavior into the core recipe engine
- executing mutating actions; saved query recipes are read-oriented unless a later spec defines a separate write workflow

## Requirements

### Must

- A saved query recipe MUST include a stable `id`, human-readable `name`, and `problem` field.
- The `problem` field MUST describe the decision, workflow, or knowledge gap the query is meant to solve, not merely restate the fields it selects.
- A recipe MUST include an `inputSpec` that says whether it is broad, optionally anchored, required-anchor, or multi-anchor.
- Broad recipes MUST explicitly declare that no input is required.
- Anchored recipes MUST declare accepted input kinds and, when relevant, accepted ontology types or interfaces.
- Supported input kinds MUST include at least `none`, `file`, `nodeRef`, `locator`, `type`, `interface`, `property`, and `text`.
- A recipe MUST include the GraphQL query text or a query template plus variable definitions.
- A recipe MUST include an output contract that describes the result shape and how callers should interpret it.
- A recipe MUST include adaptation guidance that describes what must be preserved if the ontology schema changes.
- A recipe MUST declare the schema roots, fields, interfaces, enum values, and relation traversals it depends on, either explicitly or through a derived validation index.
- Recipe validation MUST compile the GraphQL against the current ontology query schema.
- Recipe validation MUST detect missing roots, fields, fragments, enum values, incompatible anchor types, and output-contract drift where detectable.
- Agent-facing raw ontology query surfaces that accept caller input MUST support GraphQL variables when the underlying query engine supports them.
- Agent-facing saved-query surfaces MUST expose list, validate, and run operations with structured recipe metadata, input contracts, selected variables, query result data, and query errors.
- The authoritative agent surface MUST make saved-query commands discoverable enough for agents to run a recipe without falling back to `--help`; nested subcommand flags or concrete examples MUST cover list, validate, run, `--path`, `--id`, `--anchor`, and `--input`.
- MCP and in-app agent tool wrappers SHOULD expose the same read-only saved-query recipe operations as `rzm agent` when they expose ontology query tools at all.
- Schema evolution workflows MUST check saved query recipes after ontology changes and surface affected recipes before handoff.
- Schema evolution workflows MUST use the recipe's `problem`, `inputSpec`, `outputContract`, and `adaptationGuidance` to propose query updates.
- Schema evolution workflows MUST ask the user when a query cannot be safely adapted from the recipe contract alone.
- Skills that depend on repeated ontology lookups MUST prefer saved query recipes over prose-only instructions when the query shape is stable enough to reuse.
- Starter skills (spec-driven, project-kb, and any other bundled starter that ships skills) MUST declare required saved-query recipe calls or required discovery patterns at phase boundaries when the call is necessary to load governing context.
- `specify` MUST include a pre-spec related-contract discovery pattern before creating a new spec, so agents search existing specs, docs, decisions, and reference notes before drafting from scratch.
- `effort-new`, `plan`, `implement`, `alignment-audit`, `backport`, and `compound` MUST be able to load the current effort's frozen spec set, frozen story scope, coverage checklist, plan, actual delivery, deviations, and closure state through bundled recipes or equivalent required calls.
- Recipe bundles that need code files, tests, or implementation evidence MUST pair GraphQL saved recipes with `semantic-query`, `file-context`, or other live agent retrieval calls rather than pretending ontology GraphQL alone can return code evidence.
- The recipe format MUST be usable from skill bundles, repository docs, and future machine-readable registries without changing the underlying query semantics.

### Should

- Recipes should declare a `whenToRun` field such as before drafting, before editing, before creating a note, before planning, before handoff, or during validation.
- Recipes should declare fallback behavior for repositories that lack the expected type, interface, relation, or field.
- Recipes should distinguish required output fields from convenience fields so schema evolution can preserve the important part first.
- Recipes should include a short `interpretation` section that tells agents what to do with empty, partial, or high-volume results.
- Recipes should include example invocations for `rzm agent ontology-query` or the future recipe runner.
- Recipes should support variables rather than forcing string substitution into GraphQL text.
- Recipes should be small enough for future agents to read and adapt, with larger context or examples placed in nearby references.
- Rhizome-enabled skill authoring should generate or update a `references/query-recipes.md` file when a skill's workflow has repeatable ontology queries.
- Rhizome-enabled skill authoring should validate generated recipe snippets against the live schema before handing off the skill.
- Created skills should tell agents which recipe to run first and why, instead of only telling them to inspect the ontology schema.
- Validation issue rendering should label `query_recipes` checks and recipe issue codes clearly wherever validation results are shown outside the CLI.
- Browser and agent query-builder surfaces should be able to render the same recipe's problem, inputs, output, and GraphQL preview.
- Shared starter recipes should live in `.rhizome/query-recipes/` when they are generally useful across the repo, with skill-local recipes reserved for narrow skill-owned workflows. Both the spec-driven and project-kb starters bundle recipes through this path.
- Starter skills should treat bundled recipes as required context-loading steps, not optional examples, when the recipe protects frozen scope, story selection, acceptance criteria, drift audit, project context, source duplicate-check, evidence neighborhoods, or closure decisions.

### May

- Recipes may be stored in a structured `.rhizome/query-recipes/` registry in addition to Markdown skill references.
- Recipes may later support readiness-query inputs, saved browser view inputs, or previous-query-result inputs.
- Recipes may later carry examples of expected results from fixtures or representative vaults.
- Recipes may later expose tags, owner, lifecycle status, or compatibility ranges for template packaging.
- Recipes may later be addressable by `rzm agent ontology-query --recipe <id>` or a dedicated recipe command.

### Recipe contract

A saved query recipe should be representable as structured data even when authored inside Markdown:

```yaml
id: spec-landscape-survey
name: Spec landscape survey
problem: >
  Survey existing specs and embedded stories so an agent can decide whether a
  requested behavior belongs in an existing spec or requires a new spec.
whenToRun: before_creating_spec
inputSpec:
  mode: none
  anchors: []
query:
  language: graphql
  variables: {}
  body: |
    {
      notes(type: "SpecLike", first: 100) {
        nodes {
          path
          title
          ... on ProductSpec {
            id
            summary
            specStatus
          }
          ... on TechnicalSpec {
            id
            summary
            specStatus
          }
        }
      }
    }
outputContract:
  required:
    - path
    - title
    - id
    - summary
    - specStatus
  interpretation: >
    Prefer updating an existing active spec when the requested behavior is
    already inside its summary or story set.
adaptationGuidance: >
  Preserve the ability to survey all current spec-like contracts before
  creating a new spec, even if concrete spec type names or story fields change.
```

The same contract should support anchored recipes:

```yaml
id: spec-neighborhood
name: Spec neighborhood
problem: >
  Given a spec, story, file, or node locator, pull the nearby efforts,
  decisions, references, and related specs needed before editing or planning.
whenToRun: before_editing_spec
inputSpec:
  mode: required
  anchors:
    - kind: nodeRef
      accepts:
        - ProductSpec
        - TechnicalSpec
        - ExperienceSpec
        - ProcessSpec
        - UserStory
    - kind: file
      accepts:
        - docs/specs/**/*.md
variables:
  ref:
    kind: locator
    required: true
query:
  language: graphql
  body: |
    query SpecNeighborhood($path: String!) {
      note(path: $path) {
        path
        title
        linked(first: 20) {
          path
          title
        }
        backlinked(first: 20) {
          path
          title
        }
      }
    }
outputContract:
  required:
    - note.path
    - note.title
    - note.linked
    - note.backlinked
adaptationGuidance: >
  Preserve the ability to start from one authored artifact and collect the
  nearby planning, rationale, and execution context before changing it.
```

### Input contract

`inputSpec.mode` controls whether a recipe can run without caller-provided anchors:

- `none`: broad query over the repository or vault; no input is accepted or required.
- `optional`: the recipe can run broadly, but an anchor improves precision.
- `required`: exactly one anchor or variable set is required before execution.
- `multi`: multiple anchors are accepted for compare, merge, synthesis, or cross-artifact surveys.

Anchor declarations should describe both transport shape and semantic eligibility:

- `file`: path-like input that resolves inside the vault or repository.
- `nodeRef`: canonical node identity.
- `locator`: author-facing locator that Rhizome can resolve to a node.
- `type`: ontology type name.
- `interface`: ontology interface name.
- `property`: property name and optional value.
- `text`: search or semantic prompt text.

Anchored recipes should declare whether each anchor is required, optional, repeatable, and what type/interface/path patterns it accepts.

### Adaptation behavior

Ontology schema changes should treat saved query recipes as dependent artifacts. After changing `.rhizome/ontology/*.graphql`, tooling or agent workflow should:

1. Rebuild or inspect the current ontology query schema.
2. Validate every recipe that declares dependency on affected roots, fields, interfaces, relations, or enum values.
3. For each broken recipe, read the `problem`, `inputSpec`, `outputContract`, and `adaptationGuidance`.
4. Propose a minimal query update that preserves the declared problem and required output.
5. Ask the user when multiple adaptations are plausible or when preserving the old problem would conflict with the new schema model.
6. Update the recipe and any skill instructions that name it.
7. Re-run recipe validation before handoff.

Adaptation must be proactive. If an ontology change removes `PromptRecipe.phase` in favor of applicability records, the schema-editing workflow should not wait for a later agent to discover that `generate-artifact` has a broken query. It should identify the affected saved query, explain the intent it was preserving, and either migrate it or ask the user which new path best preserves the workflow.

### Skill integration

When the user opts into Rhizome-enabled skill authoring, the general skill-creation workflow should use the `rhizome` skill-authoring reference and treat saved query recipes as part of the skill bundle when a workflow repeatedly asks the same ontology question.

For a new or revised skill, the creator should:

1. Inspect the live ontology query schema.
2. Identify the workflow's first-pass broad survey queries and anchored follow-up queries.
3. Draft query recipes with `problem`, `inputSpec`, `outputContract`, and `adaptationGuidance`.
4. Validate the recipes against the live schema.
5. Place durable recipes in `references/query-recipes.md` or a future structured query registry.
6. Teach the skill body which recipe to run first, which recipes require anchors, and how to interpret empty or ambiguous results.

Example skill behavior:

- `specify` runs a broad spec landscape survey before creating a new spec.
- `generate-artifact` runs a broad recipe discovery query when the user asks for an artifact type, then an anchored recipe-neighborhood query once a `PromptRecipe` is selected.
- `curate-kb` runs broad curation-health queries plus anchored duplicate/finding detail queries.
- `ingest-source` runs anchored or property-based source existence checks before creating a new source note.

### Spec-driven starter bundle

The spec-driven starter should ship a compact query bundle that phase skills treat as required context-loading where applicable:

- `related-contracts-before-spec`: run before `specify` creates a new spec. This is a semantic discovery pattern rather than a pure GraphQL recipe: query for related active specs, docs, decisions, references, and code/docs context about the requested behavior. Agents should update or reference an existing spec when the requested behavior already belongs there.
- `identifier-allocation-context`: run before `specify` or `effort-new` creates a new typed note id. This is a required command pattern using `rzm agent next-id`, not a GraphQL recipe, because id allocation is stateful and must preserve shared prefixes and aliases.
- `effort-execution-context`: run before `plan`, `implement`, `alignment-audit`, `backport`, and `compound` when an effort path or id is known. It returns effort status, frozen specs, frozen story scope, coverage checklist, plan, intended delivery, actual delivery, deviations, compounding follow-ups, and closure status.
- `frozen-spec-index-pack`: run after `effort-execution-context` for each frozen spec. It returns a compact index-first view: spec path/title/id/summary/status, story ids/statuses, acceptance criteria content, references, decisions, and inbound efforts for targeted follow-up.
- `frozen-spec-detail-pack`: run after `frozen-spec-index-pack` when the index shows that full section content is needed. It returns richer spec section content, requirements, user stories, acceptance criteria, references, decisions, and inbound efforts.
- `story-acceptance-pack`: run during `effort-new`, `plan`, `implement`, and `alignment-audit` when selecting or verifying story scope. It returns ready stories, selected story metadata, acceptance criteria content, references, decisions, and prior efforts.
- `runtime-authoring-context`: run before typed-note drafting or restructuring when the target ontology type is known. It returns type metadata, rendered authoring guidance, next-id state, and warnings in one recipe; skills fall back to `ontology-authoring-guide` and `next-id` only when runtime support is unavailable.
- `runtime-code-evidence-pack`: run during `plan`, `implement`, `alignment-audit`, and `code-docs` when an affected code path is known. It returns bounded docs-for-code, linked notes, test candidates, normalized paths, and warnings.
- `runtime-note-code-evidence-pack`: run during `code-docs`, note authoring, or implementation handoff when the starting artifact is a code-linked note. It returns bounded code paths anchored by that note before the agent chooses concrete files.
- Agent capability refresh is not a GraphQL recipe: phase skills use `rzm agent surface` when they need current command/report/validation/tool metadata.
- `spec-code-evidence-discovery`: run during `plan`, `implement`, and `alignment-audit` when the agent needs broader implementation evidence tied to a spec, story, or acceptance criterion. Prefer `runtime-code-evidence-pack` first when an affected code path is known, then use `semantic-query` and `file-context` to broaden or fill gaps.
- `closure-drift-pack`: run during `alignment-audit`, `backport`, and `compound`. It returns actual delivery, deviations, coverage checklist, frozen specs/stories, closure statuses, and compounding follow-ups so closeout compares delivery truth against frozen intent.
- `frozen-scope-drift-check`: run before editing an active effort's frozen spec set and during alignment/closure. This is a required validation pattern using `rzm agent validate frozen-scope-drift`, because the check compares active effort timestamps against frozen spec updates.

The bundle should be written so future multi-tool recipe support can lift semantic/file-context patterns into machine-executable recipes, but the first implementation may document those patterns in skill references while GraphQL recipes cover typed note context.

### Agent and tool integration

Saved query recipes are an agent workflow primitive, not only a human CLI convenience. Non-browser agent surfaces should preserve one contract:

- `rzm agent surface` advertises recipe capability and gives enough examples for agents to list, validate, and run recipes.
- `rzm agent ontology-query` accepts JSON payloads with `query` and `variables` for one-off reads.
- `rzm agent query-recipe` accepts recipe paths, recipe ids, anchors, and explicit inputs, then returns recipe metadata beside results.
- In-app agent tools expose `ontology_query` with variables and `query_recipe` for list, validate, and run.
- MCP tools expose the same read-only ontology query and query-recipe operations when the MCP server is configured for a vault with ontology support.
- Validation tools include `query-recipes` in accepted checks, capabilities, labels, and fix-plan summaries.

Tool wrappers should not invent a separate recipe format. They should call the same parser, compiler, and executor used by the CLI and should return the same metadata fields so skills can work across CLI, chat, and MCP environments.

### Validation and lifecycle

- Recipe validation should be available independently of running the recipe.
- Recipe validation should report schema errors, input-contract errors, missing output fields, and ambiguous adaptation risks.
- Recipes should have stable ids so skills, docs, browser views, and agent handoffs can refer to them.
- Recipes should remain source-controlled with the skill or ontology/template package that owns them.
- `.rhizome/query-recipes/` is durable repository configuration and MUST stay tracked by git; Rhizome init gitignore templates MUST allowlist it the same way they allowlist `.rhizome/ontology/`.
- A recipe should be removed only when the workflow problem no longer exists or has been replaced by a better recipe with an explicit migration note.

## User Stories

### US1 - Save validated ontology query recipes with enough intent for future agents to reuse them
- id:: ^SPEC-0052-US1
- summary:: Save validated ontology query recipes with enough intent for future agents to reuse them.
- status:: ready

#### Acceptance Criteria

- A skill bundle can include named query recipes with `problem`, `inputSpec`, GraphQL, output contract, and adaptation guidance. ^SPEC-0052-US1-AC1
- The skill body can tell agents which recipe to run before drafting, editing, planning, or handoff.
- Recipe validation proves the query compiles against the live ontology query schema.
- The recipe distinguishes broad no-input queries from anchored queries.

### US2 - Run the right broad or anchored query without rediscovering the schema from scratch
- id:: ^SPEC-0052-US2
- summary:: Run the right broad or anchored query without rediscovering the schema from scratch.
- status:: ready

#### Acceptance Criteria

- The agent can see whether a recipe requires no input, optional input, required input, or multiple anchors. ^SPEC-0052-US2-AC1
- If required input is missing, the agent can ask for the right anchor or choose a different broad recipe.
- The agent can explain why it is running the recipe using the recipe's `problem` field.
- The agent can interpret empty, partial, or high-volume results using the output contract.
- Agent tool surfaces can pass variables and recipe inputs without rewriting GraphQL text.
- Agent surface metadata or examples show how to list, validate, and run recipes with required inputs.
- Saved query recipes are reachable as a top-level CLI surface (`rzm query-recipe` for humans, `rzm agent query-recipe` for agents) with `list`, `show <id>`, `validate`, and `run` subcommands; the human-facing `list` defaults to a table, `show <id>` prints the recipe contract plus a synthesized example invocation, `validate` groups issues per recipe id, and `--json` reverts each surface to the legacy JSON shape.

### US3 - Proactively adapt saved query recipes when schema evolution changes roots, fields, or relations
- id:: ^SPEC-0052-US3
- summary:: Proactively adapt saved query recipes when schema evolution changes roots, fields, or relations.
- status:: ready

#### Acceptance Criteria

- After a schema change, affected recipes are identified before handoff.
- Broken recipes are evaluated against their problem statement, input contract, output contract, and adaptation guidance.
- Safe adaptations update the recipe and preserve its workflow purpose.
- Ambiguous adaptations result in a clear user question rather than silent schema drift. ^SPEC-0052-US3-AC4

### US4 - Expose saved query recipes consistently outside the human CLI
- id:: ^SPEC-0052-US4
- summary:: Expose saved query recipes consistently outside the human CLI.
- status:: ready

#### Acceptance Criteria

- In-app agent tools expose raw ontology queries with variables and saved-query recipe list, validate, and run operations.
- MCP tools and capabilities expose read-only ontology query and saved-query recipe operations when ontology support is available.
- Validation surfaces label query-recipe checks and issues clearly enough for agents to triage them without knowing internal check ids.
- Live and generated skills prefer saved recipes for stable ontology lookups and include actual recipe references where the query shape is stable.
- Optional shared recipe registries can be discovered without replacing skill-local recipes and remain tracked by git.
- The MCP tool name (`query_recipe`), recipe envelope (`apiVersion: rhizome.query-recipe.v1`), CLI commands (`rzm query-recipe`, `rzm agent query-recipe`), and validation check id (`query-recipes`) form one consistent naming family across CLI, MCP, agent-chat tools, and recipe YAML; integrators see the same name in every surface.

### US5 - Load the right frozen scope, related specs, acceptance criteria, and code-evidence context through bundled recipes and required discovery patterns
- id:: ^SPEC-0052-US5
- summary:: Load the right frozen scope, related specs, acceptance criteria, and code-evidence context through bundled recipes and required discovery patterns.
- status:: ready

#### Acceptance Criteria

- The spec-driven template bundles recipe definitions for effort execution context, frozen spec detail, story acceptance scope, and closure drift context, plus required command/validation patterns for identifier allocation and frozen-scope drift.
- `specify` requires a related-contract discovery step before creating a new spec, using semantic retrieval to find existing specs, docs, decisions, references, and code/docs context that should be updated or referenced.
- `specify`, `effort-new`, `plan`, `implement`, `alignment-audit`, `backport`, and `compound` name the relevant required recipe, command, validation, or discovery pattern before they ask agents to allocate ids, select scope, plan work, execute, audit, reconcile, or close.
- Code-file and test evidence discovery is represented as a required `semantic-query`/`file-context` pattern until saved query recipes can orchestrate non-GraphQL tools.
- Bundled recipes validate against the live ontology query schema and are included in `query-recipes` validation for generated spec-driven starters.

### US6 - Share a saved query recipe and its inputs as a precise work target
- id:: ^SPEC-0052-US6
- summary:: Share a saved query recipe and its inputs as a precise work target.
- status:: ready

#### Acceptance Criteria

- A browser or CLI surface can identify the recipe behind a view or result set.
- The handoff includes recipe id, input values, filters, and result interpretation.
- An agent can rerun or refine the same recipe without scraping UI state.
- The handoff remains valid across schema changes when the recipe can be adapted.

### US7 - Load authoring guides, next ids, and bounded code/doc evidence through saved recipes when runtime roots expose those stable reads
- id:: ^SPEC-0052-US7
- summary:: Load authoring guides, next ids, and bounded code/doc evidence through saved recipes while discovering agent capabilities through the live agent surface.
- status:: ready

#### Acceptance Criteria

- Runtime-enriched recipes can select `ontology` and `code` roots and bind their required inputs through recipe variables; the public schema has no `agent` root.
- Recipe validation reports runtime root dependencies separately from authored ontology root dependencies.
- Spec-driven and note-authoring skills prefer runtime-enriched recipes for authoring context, code evidence, and note-to-code evidence; they use `rzm agent surface` for agent capability metadata.
- Runtime recipes preserve partial-data and warning fields so agents can continue when an index, provider, or runtime capability is unavailable.

## Open Questions

- Should recipes initially live only in skill `references/query-recipes.md`, or should Rhizome introduce `.rhizome/query-recipes/` immediately? Decision for shared registries: use `.rhizome/query-recipes/`, and keep it tracked by git.
- Should recipe validation be part of ontology validation, skill validation, or both? Decision: expose it as its own `query-recipes` validation check and include it in the default validation sweep so schema edits surface saved query drift automatically.
- How much of the output contract can be mechanically checked against GraphQL result shapes in the first implementation?
- Should query recipes support composition, where one recipe's output becomes another recipe's anchor set?
- Should adaptation guidance be freeform prose at first, or should it include structured preservation goals?
