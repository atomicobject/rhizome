---
type: EffortNote
id: EFF-2026-04-30-13-31
name: "saved query recipes"
created-at: 2026-04-30T13:31:37Z
audit-status: complete
backport-status: complete
compound-status: complete
status: complete
summary: "Implement saved-query-recipes, GraphQL variables, non-browser agent/tool integration, and spec-driven starter recipe planning."
plan-approved-by: colthorp
plan-approved-at: 2026-04-30T09:46:27-04:00
aliases:
  - EFF-2026-04-30-13-31
---
# Saved query recipes

## Scope

This effort executes the ready implementation slice from [SPEC-0052](../specs/technical/saved-query-recipes.md): saved ontology query recipes as durable, intent-described GraphQL assets for skill bundles, future agents, and ontology schema evolution workflows.

In scope:

- recipe contract and validation support for skill-owned saved queries
- agent-facing recipe metadata for broad, optional, required, and multi-anchor execution
- schema-evolution checks that identify and adapt affected recipes before handoff
- documentation and skill-surface updates needed for agents to discover and use the recipes
- non-browser agent/tool integration for variables and saved query recipes across agent chat, MCP, agent surface metadata, CLI ergonomics, validation output, live skills, and tracked shared recipe registries
- spec-driven starter query bundles for required context loading across `specify`, `effort-new`, `plan`, `implement`, `alignment-audit`, `backport`, and `compound`
- required semantic-query and file-context discovery patterns where code evidence or related-contract discovery cannot be represented as pure ontology GraphQL

Out of scope:

- browser view to agent handoff behavior, because that story is still `draft`
- browser/web UI for browsing, editing, or running recipes
- mutating query recipes or a full visual GraphQL IDE
- hard-coding starter-specific recipe behavior into the core engine

## Spec Set (Frozen)

- [SPEC-0052](../specs/technical/saved-query-recipes.md) - governing saved query recipe contract and ready delivery stories.
- [SPEC-0001](../specs/process/development-loop.md) - governing spec -> effort -> plan -> execute -> audit -> backport -> compound flow.
- [SPEC-0002](../specs/process/effort-lifecycle.md) - governing effort note shape, approval, closure, and deviation handling.
- [SPEC-0006](../specs/process/id-allocation.md) - governing `EFF-XXXX` allocation and alias mirroring.
- [SPEC-0007](../specs/process/agent-skills.md) - governing phase skill contracts and required query/discovery patterns.
- [SPEC-0024](../specs/process/story-lifecycle.md) - governing ready-story selection and carry-forward behavior.
- [SPEC-0051](../specs/process/lifecycle-immutability.md) - governing frozen-scope drift and follow-on effort boundaries.

## Stories In Scope (Frozen)

- `SPEC-0052.US1` - Save validated ontology query recipes with enough intent for future agents to reuse them.
  - Criteria: `SPEC-0052.US1.AC1`, `SPEC-0052.US1.AC2`, `SPEC-0052.US1.AC3`, `SPEC-0052.US1.AC4`.
- `SPEC-0052.US2` - Run the right broad or anchored query without rediscovering the schema from scratch.
  - Criteria: `SPEC-0052.US2.AC1`, `SPEC-0052.US2.AC2`, `SPEC-0052.US2.AC3`, `SPEC-0052.US2.AC4`.
- `SPEC-0052.US3` - Proactively adapt saved query recipes when schema evolution changes roots, fields, or relations.
  - Criteria: `SPEC-0052.US3.AC1`, `SPEC-0052.US3.AC2`, `SPEC-0052.US3.AC3`, `SPEC-0052.US3.AC4`.
- `SPEC-0052.US5` - Expose saved query recipes consistently outside the human CLI.
  - Criteria: `SPEC-0052.US5.AC1`, `SPEC-0052.US5.AC2`, `SPEC-0052.US5.AC3`, `SPEC-0052.US5.AC4`, `SPEC-0052.US5.AC5`.
- `SPEC-0052.US6` - Load the right frozen scope, related specs, acceptance criteria, and code-evidence context through bundled recipes and required discovery patterns.
  - Criteria: `SPEC-0052.US6.AC1`, `SPEC-0052.US6.AC2`, `SPEC-0052.US6.AC3`, `SPEC-0052.US6.AC4`, `SPEC-0052.US6.AC5`.

Explicitly excluded:

- Draft browser/CLI handoff story and browser/web UI recipe work.

## Spec Coverage Checklist

- [x] `SPEC-0052.US1.AC1`: skill bundles can include named query recipes with `problem`, `inputSpec`, GraphQL, output contract, and adaptation guidance.
- [x] `SPEC-0052.US1.AC2`: skill bodies can tell agents which recipe to run before drafting, editing, planning, or handoff.
- [x] `SPEC-0052.US1.AC3`: recipe validation proves the query compiles against the live ontology query schema.
- [x] `SPEC-0052.US1.AC4`: recipe metadata distinguishes broad no-input queries from anchored queries.
- [x] `SPEC-0052.US2.AC1`: agents can see whether a recipe requires no input, optional input, required input, or multiple anchors.
- [x] `SPEC-0052.US2.AC2`: missing required input leads to a precise anchor request or a broad-recipe alternative.
- [x] `SPEC-0052.US2.AC3`: agents can explain why they are running the recipe from the recipe `problem`.
- [x] `SPEC-0052.US2.AC4`: agents can interpret empty, partial, or high-volume results from the output contract.
- [x] `SPEC-0052.US2.AC5`: agent tool surfaces can pass variables and recipe inputs without rewriting GraphQL text.
- [x] `SPEC-0052.US2.AC6`: agent surface metadata or examples show how to list, validate, and run recipes with required inputs.
- [x] `SPEC-0052.US3.AC1`: schema changes identify affected recipes before handoff.
- [x] `SPEC-0052.US3.AC2`: broken recipes are evaluated against problem, input contract, output contract, and adaptation guidance.
- [x] `SPEC-0052.US3.AC3`: safe adaptations update recipes while preserving workflow purpose.
- [x] `SPEC-0052.US3.AC4`: ambiguous adaptations produce a user question rather than silent schema drift.
- [x] `SPEC-0052.US5.AC1`: in-app agent tools expose raw ontology queries with variables and saved-query recipe list, validate, and run operations.
- [x] `SPEC-0052.US5.AC2`: MCP tools and capabilities expose read-only ontology query and saved-query recipe operations when ontology support is available.
- [x] `SPEC-0052.US5.AC3`: validation surfaces label query-recipe checks and issues clearly enough for agents to triage them without knowing internal check ids.
- [x] `SPEC-0052.US5.AC4`: live and generated skills prefer saved recipes for stable ontology lookups and include actual recipe references where the query shape is stable.
- [x] `SPEC-0052.US5.AC5`: optional shared recipe registries can be discovered without replacing skill-local recipes and remain tracked by git.
- [x] `SPEC-0052.US6.AC1`: the spec-driven template bundles recipe definitions for effort execution context, frozen spec detail, story acceptance scope, and closure drift context, plus required command/validation patterns for identifier allocation and frozen-scope drift.
- [x] `SPEC-0052.US6.AC2`: `specify` requires related-contract discovery before creating a new spec.
- [x] `SPEC-0052.US6.AC3`: phase skills name the relevant required recipe, command, validation, or discovery pattern before id allocation, scope, planning, execution, audit, reconciliation, or closeout decisions.
- [x] `SPEC-0052.US6.AC4`: code-file and test evidence discovery is represented as a required `semantic-query`/`file-context` pattern until saved query recipes can orchestrate non-GraphQL tools.
- [x] `SPEC-0052.US6.AC5`: bundled recipes validate against the live ontology query schema and are included in `query-recipes` validation for generated spec-driven starters.

## Plan

Decision: implement saved ontology query recipes as a small contract/validation layer around the existing read-only ontology GraphQL executor, then surface that layer through `rzm ontology ...`, `rzm agent ...`, validation checks, and skill templates. Keep browser handoff out of this effort.

### Technical Context

- Runtime: Go CLI, Cobra commands, vendored dependencies, tests beside implementation.
- Current query engine: `pkg/ontology/query` already builds an executable schema, prepares/validates GraphQL, executes read-only roots, and supports note/property/semantic roots.
- Current GraphQL variable gap: `pkg/ontology/query.Prepare` rejects variable definitions and execution currently passes `nil` into `ArgumentMap(...)`; Phase 6 changes this contract per [SPEC-0042](../specs/technical/ontology-graphql-query-contract.md#graphql-variables).
- Current CLI surfaces: `cmd/ontology_query.go` and `cmd/agent_ontology.go` expose raw `ontology query/schema/reference/authoring-guide`; `cmd/agent_surface.go` advertises agent-safe commands.
- Current validation architecture: `pkg/validate` owns suite checks, check names, issue/fix payloads, and optional `--fix`; `cmd/validate.go` and `cmd/agent_validate.go` share it.
- Current recipe-adjacent precedent: `pkg/app/mcp/context_recipe.go` uses versioned structured recipes for replaying context calls, but saved query recipes are ontology-query assets, not context-recipe calls.
- Current skill surfaces: generated skill templates live under `pkg/app/cli/init/templates/skills/...`; durable Rhizome guidance mirrors through `docs/rhizome-md-templates/*`.
- Current non-browser integration gaps: `pkg/app/agentchat/tools.go` exposes raw `ontology_query` but not variables or recipe execution; `pkg/app/mcp` capabilities do not expose ontology query or query recipes; `rzm agent surface` advertises the parent recipe command but not child command usage; live `.agents/skills/*` still mostly teach ad hoc ontology queries; `.rhizome/.gitignore` currently allowlists ontology config but not a future `.rhizome/query-recipes/` registry.
- Current spec-driven template gap: phase skills manually rediscover current effort, frozen specs, selected stories, acceptance criteria, related specs, and code evidence through prose. This is now stable enough for a bundled recipe set plus required semantic/file-context patterns.
- Current pulled-in lifecycle updates: local `main` added `rzm agent next-id` for typed identifier allocation and `validate --check frozen-scope-drift` for active efforts whose frozen specs changed after effort creation. Phase 8 should treat both as required command/validation patterns adjacent to the GraphQL recipe bundle.

### Gap Summary

- `SPEC-0052.US1`: delivered by Phases 1 through 5. Remaining work is better leverage through live skills and optional shared registries.
- `SPEC-0052.US2`: mostly delivered by Phases 2, 3, and 6. Remaining gaps are non-browser tool variable support and richer agent surface examples for recipe execution.
- `SPEC-0052.US3`: delivered by Phase 4. Remaining work is validation output polish across non-browser tool wrappers.
- `SPEC-0052.US5`: missing. CLI-backed recipe execution exists, but non-browser tool wrappers, capability metadata, live skills, and shared tracked registry handling are not yet integrated.
- `SPEC-0052.US6`: missing. We have one shared `.rhizome/query-recipes/spec-by-path.yaml`, but no spec-driven starter bundle for current effort context, frozen specs, story/AC scope, closure drift, pre-spec related-contract discovery, identifier allocation, or frozen-scope-drift checks.
- `SPEC-0052.US4`: intentionally excluded. Browser-to-CLI handoff remains draft.

### Architecture Decisions

- Add `pkg/ontology/queryrecipe` for recipe parsing, metadata validation, dependency extraction, and placeholder/anchor binding. Keep it separate from `pkg/ontology/query` so the executor remains the small GraphQL engine.
- Define recipe sources as structured YAML blocks in markdown plus standalone YAML files. First supported block form: fenced code blocks tagged `query-recipe` or `yaml query-recipe`; this keeps skill references readable while still machine-validatable.
- Recipe contract fields for this slice: `apiVersion`, `id`, `name`, `problem`, `inputSpec`, `query`, `outputContract`, `adaptationGuidance`, optional `examples`, optional `tags`.
- `inputSpec.mode` values: `none`, `optional_anchor`, `required_anchor`, `multi_anchor`. Missing required anchors fail before query execution with a JSON error that names the missing input and suggests any broad alternative from recipe metadata.
- Initial `query` support stores GraphQL plus declared placeholders/defaults because the current ontology GraphQL subset rejects variables. Phase 6 replaces placeholder substitution with first-class GraphQL variables while preserving explicit input declaration, validation, and agent-readable result metadata.
- `outputContract` describes expected result paths, empty-result meaning, partial-result meaning, and high-volume handling. The runner echoes this metadata with results so agents can interpret output without rediscovery.
- Schema-dependency extraction comes from the prepared GraphQL AST plus recipe metadata. It records roots, fields, relation traversals, inline-fragment type conditions, and enum literals for validation diagnostics.
- Safe adaptation is not silent schema guessing. The validation check reports affected recipes and an `agent_required` fix action with recipe purpose/input/output/adaptation context. Agents may edit recipes when the adaptation is obvious; ambiguous cases become user questions.
- Do not add MCP tool registration in this slice unless implementation discovers current MCP callers need it. The live agent ontology surface is Cobra-based, so `cmd/agent_surface.go` is the required agent contract.
- Phase 7 reverses the earlier MCP deferral: non-browser agent/tool parity is now in scope. MCP and in-app agent tools should wrap the same recipe/query runtime instead of shelling out through a separate format.
- Use `.rhizome/query-recipes/` for a shared registry if registry support is added. This directory is durable project configuration, not transient state, and must be allowlisted in `.rhizome/.gitignore` plus the `rzm init` gitignore writer.
- Phase 8 keeps GraphQL recipes scoped to ontology-backed typed note context. It does not force code-file discovery, id allocation, or frozen-scope drift into GraphQL; those use required `semantic-query`, `file-context`, `next-id`, and `validate --check frozen-scope-drift` patterns until a later multi-tool recipe format exists.
- Spec-driven starter recipes should live in `.rhizome/query-recipes/spec-driven.yaml` and be copied/generated into spec-driven starter templates. Narrow skill-owned examples may remain in `references/query-recipes.md`.

### Implementation Phases

Phase 1: recipe contract and parser.

- [x] T001 [SPEC-0052.US1] Add `pkg/ontology/queryrecipe` types for recipe metadata, input spec, query body, output contract, adaptation guidance, dependencies, validation issues, and source locations.
- [x] T002 [SPEC-0052.US1] Parse standalone YAML files and markdown fenced recipe blocks with stable path/line source locations.
- [x] T003 [SPEC-0052.US1] Validate required metadata, unique recipe ids, supported input modes, non-empty GraphQL, and output/adaptation fields.
- [x] T004 [SPEC-0052.US1] Add unit tests in `pkg/ontology/queryrecipe` for valid markdown recipes, missing fields, duplicate ids, invalid input modes, and source-location diagnostics.

Phase 2: query compilation and execution.

- [x] T005 [SPEC-0052.US1] Compile recipe GraphQL against `ontologyquery.ExecutableSchema` and report prepare errors as recipe validation issues.
- [x] T006 [SPEC-0052.US2] Add anchor/input binding that maps declared recipe inputs into explicit placeholders only, with GraphQL escaping and an error for undeclared or unbound placeholders.
- [x] T007 [SPEC-0052.US2] Add recipe execution helpers that return query results plus recipe `problem`, `inputSpec`, `outputContract`, and selected input values.
- [x] T008 [SPEC-0052.US2] Add tests for no-input, optional-anchor, required-anchor, and multi-anchor recipes, including missing-input failures and empty-result interpretation metadata.

Phase 3: CLI and agent surface.

- [x] T009 [SPEC-0052.US1] Add `rzm ontology query-recipe list|validate|run --path <file-or-dir> [--id <id>] [--anchor <value>] [--input key=value]`.
- [x] T010 [SPEC-0052.US2] Add matching JSON-first `rzm agent query-recipe list|validate|run` commands.
- [x] T011 [SPEC-0052.US2] Update `cmd/agent_surface.go` so `query-recipe` appears in command order, category, capabilities, and command metadata.
- [x] T012 [SPEC-0052.US2] Add command tests for list/validate/run, precise missing-anchor messages, duplicate recipe id errors, and agent JSON shape.

Phase 4: validation and schema-evolution workflow.

- [x] T013 [SPEC-0052.US3] Add `validate.CheckQueryRecipes` plus `RunQueryRecipes` in `pkg/validate`; include `--check query-recipes` in `cmd/validate.go` and `cmd/agent_validate.go`.
- [x] T014 [SPEC-0052.US3] Discover recipe files from skill-template references and documented recipe paths, with explicit path options for direct validation commands.
- [x] T015 [SPEC-0052.US3] Emit affected-recipe issues for missing roots, fields, relations, inline-fragment type conditions, enum values, or output-contract paths after schema changes.
- [x] T016 [SPEC-0052.US3] Emit `agent_required` fix actions containing recipe source, problem, input contract, output contract, dependency summary, and adaptation guidance; do not auto-apply ambiguous rewrites.
- [x] T017 [SPEC-0052.US3] Add validation tests for schema drift, affected dependency reporting, and `--fix --non-interactive` safely skipping agent-required recipe adaptations.

Phase 5: docs and skill templates.

- [x] T018 [SPEC-0052.US1] Add saved query recipe reference docs or update `docs/rhizome-md-templates/Skill ontology recipes.md` with the contract, examples, and when to save a query.
- [x] T019 [SPEC-0052.US1] Update `pkg/app/cli/init/templates/skills/markdown/rhizome-skill-creator/SKILL.md` to prefer saved query recipes for stable ontology-query patterns.
- [x] T020 [SPEC-0052.US1] Add a generated skill reference such as `pkg/app/cli/init/templates/skills/markdown/rhizome-skill-creator/references/query-recipes.md`.
- [x] T021 [SPEC-0052.US3] Update `pkg/app/cli/init/templates/skills/markdown/rhizome-ontology/SKILL.md` so schema edits run recipe validation and repair/adapt affected recipes before handoff.
- [x] T022 [SPEC-0052.US2] Update `docs/rhizome-md-templates/Agent CLI usage.md` and `docs/rhizome-md-templates/Ontology overview.md` if the new agent command should be part of default guidance.

Phase 6: first-class GraphQL variable support.

- [x] T023 [SPEC-0042] Change `pkg/ontology/query` preparation to accept caller-supplied variable values, apply operation defaults, reject missing required variables, and validate/coerce values before root-bound checks.
- [x] T024 [SPEC-0042] Thread validated variables through `PreparedQuery`/execution so every `ArgumentMap(...)` call in roots, relations, ambient fields, and section children uses the same value map.
- [x] T025 [SPEC-0042] Add regression tests in `pkg/ontology/query/query_test.go` for variable-backed `path`, `find`, `property`, `semantic`, `first`, relation, and `children(first:)` arguments, plus missing/invalid/default variable cases.
- [x] T026 [SPEC-0042] Extend `rzm ontology query` and `rzm agent ontology-query` with JSON variable input, preferring a `variables` object for agent JSON and file/inline JSON options for the human CLI.
- [x] T027 [SPEC-0052] Migrate `pkg/ontology/queryrecipe` execution from placeholder substitution to declared GraphQL variables; keep recipe input validation explicit and fail unknown/unbound recipe inputs before query execution.
- [x] T028 [SPEC-0052] Update recipe fixtures, command tests, and validation checks so recipes compile and run through GraphQL variables rather than rewritten query text.
- [x] T029 [SPEC-0042][SPEC-0052] Update saved-query-recipes docs and generated skill-template guidance to show variable-first recipes and remove placeholder guidance once the implementation lands.

Phase 7: non-browser integration and leverage.

- [x] T030 [SPEC-0052.US5.AC1][SPEC-0052.US2.AC5] Update `pkg/app/agentchat/tools.go` so the `ontology_query` tool accepts a `variables` object and calls the same variable-aware agent CLI or shared runtime path.
- [x] T031 [SPEC-0052.US5.AC1] Add an in-app agent `query_recipe` tool with `op: list|validate|run`, `path`, `id`, `anchor`, `inputs`, and optional input JSON; return recipe metadata, selected variables, validation issues, output contract, and query result data.
- [x] T032 [SPEC-0052.US5.AC3] Update in-app agent `validate` tool descriptions and accepted-check guidance so `query-recipes` is a first-class validation check.
- [x] T033 [SPEC-0052.US2.AC6] Improve `cmd/agent_surface.go` examples and/or nested command metadata for `ontology-query`, `query-recipe list`, `query-recipe validate`, and `query-recipe run`, including variables and required inputs.
- [x] T034 [SPEC-0052.US2.AC5] Add CLI ergonomics for larger recipe inputs if needed: prefer `--inputs-json` / `--inputs-file` for recipe runners over expanding ad hoc `key=value` only.
- [x] T035 [SPEC-0052.US5.AC2] Add MCP `ontology_query_schema`, `ontology_query`, and `query_recipe` read-only tools in `pkg/app/mcp`, with capabilities advertising and variable/input support aligned to `rzm agent`.
- [x] T036 [SPEC-0052.US5.AC2] Add MCP tests for capability advertisement, variable-backed ontology queries, recipe list/validate/run, missing required inputs, and schema/recipe validation errors. Implemented capability/list coverage; deeper direct MCP execution matrix is recorded as compounding follow-up because CLI/agent/runtime tests cover the shared behavior.
- [x] T037 [SPEC-0052.US5.AC3] Polish query-recipe validation issue/fix labels and summaries in `pkg/validate` so CLI, agent JSON, and MCP outputs are understandable without internal check-name knowledge.
- [x] T038 [SPEC-0052.US5.AC4] Update live `.agents/skills/*` and generated skill templates where stable ontology lookup patterns exist (`rhizome-note-authoring`, `rhizome-onboard`, `specify`, `plan`, `implement`, `ingest-transcript`, `rhizome-skill-creator`) to prefer saved recipes over repeated ad hoc GraphQL. Implemented the highest-value stable surfaces for this effort; broader skill migration is recorded as compounding follow-up.
- [x] T039 [SPEC-0052.US5.AC4] Add actual `references/query-recipes.md` recipes for the highest-value stable workflows instead of only documenting the recipe format.
- [x] T040 [SPEC-0052.US5.AC5] Add `.rhizome/query-recipes/` discovery to query-recipe validation and recipe list/run commands without replacing skill-local `references/query-recipes.md`.
- [x] T041 [SPEC-0052.US5.AC5] Update `.rhizome/.gitignore`, `pkg/app/cli/init/write_config.go`, and init tests so `.rhizome/query-recipes/` is allowlisted and remains tracked by git.
- [x] T042 [SPEC-0052.US5] Update README and non-browser agent guidance docs to explain when to use raw variable-backed `ontology-query` versus saved `query-recipe`.
- [x] T043 [SPEC-0052.US5] Add focused tests across `pkg/app/agentchat`, `pkg/app/mcp`, `cmd`, `pkg/validate`, and `pkg/app/cli/init`; run `gofmt` on touched Go files.

Phase 8: spec-driven template recipe bundle and required discovery patterns.

- [x] T044 [SPEC-0052.US6.AC1] Add `.rhizome/query-recipes/spec-driven.yaml` with GraphQL recipes for `effort-execution-context`, `frozen-spec-index-pack`, `frozen-spec-detail-pack`, `story-acceptance-pack`, and `closure-drift-pack`.
- [x] T045 [SPEC-0052.US6.AC1][SPEC-0052.US6.AC5] Add the same spec-driven recipe registry to the spec-driven starter template so generated repos receive and validate the bundled recipes.
- [x] T046 [SPEC-0052.US6.AC2] Update live and generated `specify` skill guidance to require `related-contracts-before-spec`: semantic discovery over related specs, docs, decisions, references, and implementation/docs context before creating a new spec.
- [x] T047 [SPEC-0052.US6.AC1][SPEC-0052.US6.AC3] Update live and generated `specify`/`effort-new` guidance to require `identifier-allocation-context` through `rzm agent next-id` before creating typed spec, story, or effort ids.
- [x] T048 [SPEC-0052.US6.AC3] Update live and generated `effort-new` to use `story-acceptance-pack` while selecting ready stories and acceptance criteria, and to record exact story/criterion ids in frozen scope.
- [x] T049 [SPEC-0052.US6.AC3] Update live and generated `plan` to run `effort-execution-context`, then `frozen-spec-index-pack`, before drafting or revising the implementation plan; use `frozen-spec-detail-pack` only when full section content is needed.
- [x] T050 [SPEC-0052.US6.AC3][SPEC-0052.US6.AC4] Update live and generated `implement` to run `effort-execution-context` before execution and to use `spec-code-evidence-discovery` semantic/file-context pattern before touching code tied to selected stories or acceptance criteria.
- [x] T051 [SPEC-0052.US6.AC3][SPEC-0052.US6.AC4] Update live and generated `alignment-audit` to run `closure-drift-pack`, `frozen-spec-index-pack`, `frozen-scope-drift-check`, and the code-evidence discovery pattern before reporting drift; use `frozen-spec-detail-pack` when a specific spec contract needs full text.
- [x] T052 [SPEC-0052.US6.AC3] Update live and generated `backport` to run `closure-drift-pack`, `frozen-spec-index-pack`, and `frozen-scope-drift-check` before reconciling specs, stories, acceptance criteria, decisions, or references; use `frozen-spec-detail-pack` before editing full spec text.
- [x] T053 [SPEC-0052.US6.AC3] Update live and generated `compound` to run `closure-drift-pack` before closing compounding triage or marking `compound-status` complete.
- [x] T054 [SPEC-0052.US6.AC4] Add a durable reference doc, likely `docs/rhizome-md-templates/Spec-driven query bundle.md`, that distinguishes GraphQL recipes from required `semantic-query`/`file-context`/`next-id`/validation patterns and gives command examples.
- [x] T055 [SPEC-0052.US6.AC5] Add command/validation tests showing the new bundled recipes load from `.rhizome/query-recipes/` and from generated spec-driven starter paths, then pass `validate --check query-recipes`.
- [x] T056 [SPEC-0052.US6] Update `docs/specs/process/agent-skills.md` and any mirrored starter docs so required context recipes/patterns are part of the phase contract, not optional examples.
- [x] T057 [SPEC-0052.US6] Run `rzm agent query-recipe validate`, `rzm agent validate --check query-recipes --check ontology --check frozen-scope-drift`, focused Go tests for queryrecipe/init validation, and full `make check` before handoff.

### Documentation Plan

- Update or create a saved-query-recipes guide under `docs/rhizome-md-templates/` and mirror through init template references where current Rhizome template mirroring expects it.
- Keep implementation docs near the code with `pkg/ontology/queryrecipe/CONTEXT.md` if the new package has enough behavior to merit retrieval context.
- Update skill templates only under `pkg/app/cli/init/templates/skills/...`; do not edit generated local skill copies.
- For Phase 6, update [SPEC-0042](../specs/technical/ontology-graphql-query-contract.md#graphql-variables) implementation-facing docs and the saved-query-recipes examples so variables are the normative recipe input path.
- For Phase 7, update both live repo skills and generated starter/template skill sources when the current repo depends on those skills during development; mirror durable generic guidance through `docs/rhizome-md-templates/*` and `pkg/app/cli/init/templates/...`.
- Document `.rhizome/query-recipes/` as tracked project configuration wherever the shared registry is introduced.
- For Phase 8, document the spec-driven recipe bundle in shared Rhizome guidance and generated spec-driven starter assets. Be explicit that related-contract, code-evidence, identifier-allocation, and frozen-scope-drift discovery are required retrieval/command/validation patterns, not pure GraphQL recipes yet.
- Backport any final deviations into [SPEC-0052](../specs/technical/saved-query-recipes.md) only after implementation reality proves the spec wording needs adjustment.

### Rationale Capture Plan

- Add `IMPORTANT:` comments near recipe input binding explaining why arbitrary GraphQL string rewriting is avoided.
- For Phase 6, replace that rationale with a smaller `IMPORTANT:` comment at the variable boundary explaining why caller JSON is validated/coerced before root-bound checks.
- Add `WHY:` or `RATIONALE:` comments near schema-drift validation explaining why ambiguous adaptations become `agent_required` instead of automatic edits.
- Use code-adjacent coderefs in the new package and validation check to link the behavior to `[[saved-query-recipes#^SPEC-0052-US1-AC1]]`, `[[saved-query-recipes#^SPEC-0052-US2-AC1]]`, and `[[saved-query-recipes#^SPEC-0052-US3-AC4]]`.
- Add a short package context note only if tests and comments are insufficient for later retrieval.

### Validation Plan

- Focused first: `go test ./pkg/ontology/queryrecipe ./pkg/ontology/query ./pkg/validate ./cmd`
- Phase 6 focused first: `go test ./pkg/ontology/query ./pkg/ontology/queryrecipe ./cmd`
- CLI smoke: `go run . ontology query-recipe validate --path <fixture>` and `go run . agent query-recipe run --path <fixture> --id <id>`
- Phase 6 CLI smoke: `go run . ontology query --variables-file <fixture-vars.json> '<query-with-variables>'` and `go run . agent ontology-query --json '<payload-with-variables>'`
- Repo validation: `rzm agent validate --check ontology --check aliases --check query-recipes --max-issues 80`
- Phase 7 focused first: `go test ./pkg/app/agentchat ./pkg/app/mcp ./cmd ./pkg/validate ./pkg/app/cli/init`
- Phase 7 CLI/MCP smoke: variable-backed raw ontology query, recipe list/validate/run from skill refs, recipe list/validate/run from `.rhizome/query-recipes/`, and `rzm agent surface` examples for recipe execution.
- Phase 8 recipe smoke: `rzm agent query-recipe validate`, then run each bundled spec-driven GraphQL recipe against a representative effort/spec/story path.
- Phase 8 retrieval-pattern smoke: run the documented `related-contracts-before-spec` semantic query, `spec-code-evidence-discovery` semantic/file-context pattern, `rzm agent next-id` pattern, and `rzm agent validate --check frozen-scope-drift` against this repo.
- Full handoff gate when implementation completes: `make check`

Phase 7 implemented for CLI, in-app agent tools, MCP handlers/capabilities, shared registry discovery, tracked `.rhizome/query-recipes/`, and focused tests. Remaining follow-up: deepen direct MCP execution tests and migrate additional live/generator skills to concrete saved recipes where their ontology lookup patterns stabilize.

## Original Intended Delivery

Deliver the ready saved-query-recipes behavior for reusable skill-packaged ontology query recipes, agent recipe execution guidance, and schema-evolution adaptation checks, while leaving draft browser/CLI handoff behavior for a later effort.

## Actual Delivered

Implemented saved ontology query recipes across parser/validation, CLI/agent execution, schema-drift validation, and skill/template docs. Added first-class GraphQL variable support for ontology queries and migrated recipe execution to variables instead of placeholder substitution. Added the spec-driven starter query bundle, required phase-skill recipe/discovery patterns, multi-document YAML recipe loading, starter init support for `.rhizome/query-recipes/spec-driven.yaml`, and default validation coverage for saved query/schema drift. Post-closure cleanup removed the remaining context-recipe guidance, made the agent surface recipe-first and truthfully advertise code-symbol tools, strengthened skill-creator recipe reuse/authoring guidance, and taught `implement` to cite specs, stories, and acceptance criteria through durable node-link/locator targets. Browser handoff remains intentionally excluded with `SPEC-0052.US4`.

## Execution Notes

- 2026-04-30T13:31:37Z: Effort opened for [SPEC-0052](../specs/technical/saved-query-recipes.md). Selected ready stories `SPEC-0052.US1`, `SPEC-0052.US2`, and `SPEC-0052.US3`; excluded the draft browser/CLI handoff story.
- 2026-04-30: Plan drafted after checking live ontology query, agent command, validation-suite, context-recipe, and skill-template surfaces. Plan keeps implementation to `SPEC-0052.US1` through `SPEC-0052.US3` and leaves `SPEC-0052.US4` out.
- 2026-04-30: User approved execution in chat with "Execute"; implementation started.
- 2026-04-30: Implemented `pkg/ontology/queryrecipe`, `rzm ontology query-recipe`, `rzm agent query-recipe`, and `validate --check query-recipes`; added parser/command tests and updated skill/template guidance.
- 2026-04-30: Validation evidence: `go test ./...` passed; `make check` passed after removing the generated local `.cache` directory from earlier sandboxed test runs; `go run . agent validate --check query-recipes --max-issues 80` passed with 1 recipe checked. Baseline ontology validation still reports 3 pre-existing project-kb template broken-note-link issues outside this effort.
- 2026-04-30: Follow-on scope added by user request: [SPEC-0042](../specs/technical/ontology-graphql-query-contract.md#graphql-variables) now requires GraphQL variables, and Phase 6 plans the implementation needed to replace saved-query placeholder binding.
- 2026-04-30: Implemented Phase 6: `PrepareWithVariables`, validated variable maps in `PreparedQuery`, executor-wide variable-aware `ArgumentMap(...)`, `rzm ontology query --variables-json/--variables-file`, `rzm agent ontology-query --json`, and variable-first query recipe compilation/execution.
- 2026-04-30: Phase 6 validation evidence: `go test ./pkg/ontology/query ./pkg/ontology/queryrecipe ./cmd` passed; `go test ./...` passed.
- 2026-04-30: Rhizome validation evidence: `RZM_REPO_DELEGATE=1 GOCACHE=/private/tmp/rhizome-gocache go run . agent validate --check query-recipes --max-issues 80` passed with 1 recipe checked. Combined `ontology`, `aliases`, and `query-recipes` validation still reports the 3 pre-existing project-kb template broken-note-link issues outside this effort.
- 2026-04-30: Full gate evidence: `RZM_REPO_DELEGATE=1 GOCACHE=/private/tmp/rhizome-gocache make check` passed.
- 2026-04-30: Added Phase 7 plan for non-browser integration after broad review: agent chat variable/recipe tools, MCP parity, richer agent surface examples, CLI input ergonomics, validation wording, live/generator skill recipe adoption, `.rhizome/query-recipes/` discovery, and gitignore allowlisting. Browser/web UI remains out of this phase.
- 2026-04-30: Implemented Phase 7 non-browser surfaces: in-app agent `ontology_query` variables, `query_recipe` list/validate/run, nested `agent surface` recipe metadata, `--inputs-json` / `--inputs-file`, MCP ontology query/recipe handlers and capabilities, shared default registry discovery, committed `.rhizome/query-recipes/spec-by-path.yaml`, and `.rhizome/query-recipes/` gitignore allowlisting.
- 2026-04-30: Added Phase 8 plan after audit of spec-driven template skills/types/workflows and after merging local `main` updates for `next-id` and `frozen-scope-drift`. The new phase bundles required spec-driven context recipes and required semantic/file-context/command/validation patterns, including pre-spec related-contract discovery for `specify`.
- 2026-04-30: Implemented Phase 8: added `.rhizome/query-recipes/spec-driven.yaml`, starter init support for template-owned query recipes, multi-document YAML recipe loading, generated spec-driven starter recipe/docs assets, live/template phase-skill guidance for required recipes and semantic/file-context patterns, and updated `SPEC-0052` / `SPEC-0007` coverage.
- 2026-04-30: Phase 8 validation evidence: `rzm agent query-recipe validate --path .rhizome/query-recipes/spec-driven.yaml` passed; `rzm agent validate --check query-recipes --check ontology --check frozen-scope-drift --max-issues 80` passed `query_recipes` with 6 recipes checked and `frozen_scope_drift` with no drift, while reporting 3 pre-existing project-kb managed-doc broken links outside this effort; `go test ./pkg/ontology/queryrecipe ./pkg/app/cli/init` passed; `git diff --check` passed; `make check` passed.
- 2026-04-30: Follow-up integration: added `query_recipes` to the default validation sweep so bare `rzm validate` / `rzm agent validate` check saved queries against the live ontology schema. Updated the live `rhizome-ontology` skill to proactively run query-recipe validation during schema evolution; generated ontology skill template already had this guidance. Validation evidence: `go test ./pkg/validate` passed; bare `rzm agent validate --max-issues 20` selected `query_recipes` and checked 6 recipes with no recipe issues, while still reporting the known 3 project-kb managed-doc broken links and 4 alias baseline issues; `make check` passed.
- 2026-04-30: Review-fix pass: made default query-recipe validation skip no-recipe/no-ontology vaults, validated `outputContract.expectedPaths` against selected response paths, bound `multi_anchor` recipe input as list variables, repaired duplicate ids (`EFF-0012`, `SPEC-0053` for readiness-engine), mirrored the live `rhizome-skill-creator` recipe reference, and added focused starter recipe validation against the repo schema. Default recipe discovery now scans active project recipe roots, not generated starter template sources; starter recipes have their own init test so live/template copies do not collide during ordinary validation. Validation evidence: `go test ./pkg/validate ./pkg/ontology/queryrecipe ./pkg/app/cli/init ./cmd` passed; `rzm agent validate --check query-recipes --check aliases --check frozen-scope-drift --max-issues 80` passed with 6 recipes checked; default `rzm agent validate --max-issues 80` now has zero query-recipe, alias, or frozen-scope issues and only the known 3 project-kb managed-doc broken links; `git diff --check` passed; `make check` passed.
- 2026-04-30: Closure audit/backport/compound pass completed. `closure-drift-pack` resolved the effort and showed the only remaining gaps were stale checklist boxes and two residual hardening items; coverage checklist and status fields were reconciled to delivered behavior. Backport review found the governing specs already describe implementation reality after the prior updates, so no additional spec edits were needed. Compounding triage recorded MCP execution-test hardening and broader skill-recipe migration as follow-up capacity work.
- Post-closure 2026-04-30: Tuned spec-driven recipe context shape after review. Added `frozen-spec-index-pack` as the index-first default for frozen specs, kept `frozen-spec-detail-pack` as the explicit deep-read query, and updated live/generated plan, audit, backport, and query-bundle guidance to use index first.
- Post-closure 2026-05-02: Cleanup and effectiveness pass for agent-facing surfaces after user review. Removed stale context-recipe guidance from the curated agent surface, made `query-recipe` the front-loaded typed-note workflow, added the advertised code-symbol tools to the curated surface with valid examples, strengthened `rhizome-skill-creator` guidance for recipe reuse/new recipe authoring/storage choices, tightened `story-acceptance-pack` to expose compact story/criterion link locators only where citation decisions need them, and added `implement` guidance for linking code/tests/docs to the smallest durable spec/story/acceptance-criterion node via recipe locators or `rzm agent node-link`. Validation evidence: `go test ./pkg/ontology/queryrecipe`, `go test ./pkg/app/cli/init`, `go test ./cmd ./pkg/app/cli/init`, `go test ./pkg/ontology/queryrecipe ./pkg/app/mcp`, `git diff --check`, review rerun against main, review-finding fix, and final `make check` all passed. Web tests still emit the existing React `act(...)` warnings only.

## Deviations

- Post-implementation scope extension: the original effort froze and delivered the ready `SPEC-0052.US1` through `SPEC-0052.US3` slice using explicit placeholder binding because variables were unsupported. The active effort now adds Phase 6 to implement first-class GraphQL variables and migrate recipes to that path before closure.
- Second scope extension: `SPEC-0052.US5` adds non-browser integration so variables and saved recipes are usable through agent chat, MCP, richer agent surface metadata, live skills, validation outputs, and tracked shared recipe registries. Browser/web UI remains excluded under draft `SPEC-0052.US4`.
- Third scope extension: `SPEC-0052.US6` adds spec-driven starter recipe bundling and required discovery patterns. This extends recipe work beyond generic surfaces into phase-skill operating contracts, while keeping non-GraphQL code evidence, id allocation, and frozen-scope drift as documented retrieval/command/validation patterns rather than overloading GraphQL recipes.

## Compounding Follow-ups

- Trigger: Phase 7 left direct MCP execution coverage thinner than CLI/agent/runtime coverage.
  Surface: verification
  Missing support: focused tests for MCP `ontology_query` variable execution and `query_recipe` validate/run error paths against an indexed fixture.
  Smallest durable fix: add an MCP ontology tool test helper that seeds a tiny ontology vault/store and exercises schema, query variables, recipe validate/run, missing required input, and schema/recipe validation errors without relying on shell commands.
  Capability/safety boundary: None.
  Size: Simple
  Evidence this helped: MCP parity regressions fail in `go test ./pkg/app/mcp` before integration gates.
  Owner/next effort: follow-up verification hardening effort.
- Trigger: Phase 8 added the highest-value spec-driven recipes, but not every live/generated skill has a concrete saved recipe yet.
  Surface: workflow
  Missing support: broader migration guidance for `rhizome-note-authoring`, `rhizome-onboard`, `ingest-transcript`, and future team skills when their ontology lookup patterns become stable.
  Smallest durable fix: audit skill-local ad hoc GraphQL examples and add saved recipe references only where the query shape is stable enough to validate and reuse.
  Capability/safety boundary: None.
  Size: Larger
  Evidence this helped: agents start from validated recipe ids instead of rediscovering schema roots in repeated phase work.
  Owner/next effort: follow-up skill-recipe adoption effort.

## Status

Complete. Initial saved-query-recipes implementation, Phase 6 GraphQL variable support, Phase 7 non-browser integration surfaces, Phase 8 spec-driven starter recipe bundle/discovery-pattern integration, review-fix pass, alignment audit, backport review, compounding triage, and post-closure agent-surface/skill-template cleanup are complete. Draft browser/web handoff remains intentionally excluded under `SPEC-0052.US4`; deeper MCP execution-test hardening and broader skill-recipe adoption remain recorded as follow-up capacity work.

## Closure Checklist

- [x] Historical delivery and closure evidence is recorded in this effort's Status and Execution Notes.
