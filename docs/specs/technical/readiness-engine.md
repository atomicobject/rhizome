---
type: TechnicalSpec
summary: "Defines the generic schema-guided readiness engine that computes explainable node readiness for typed listings, agent handoffs, and ontology workspace status surfaces."
id: SPEC-0053
spec-status: active
last-updated: 2026-04-30
aliases:
  - SPEC-0053
  - Readiness engine
---

# Readiness engine

## Summary

Rhizome needs a reusable readiness engine that turns ontology structure, validation results, edit-session state, and node workspace status into one explainable signal for typed note work. The engine should make type-filtered listings useful by default, help users identify notes that are ready, incomplete, invalid, modified, stale, or explicitly blocked on judgment, and give agents the same facts without scraping browser-only state.

Readiness is not validation. Validation answers whether authored content satisfies the ontology and link contracts. Readiness answers whether a typed node is operationally ready for the next workflow step. The first version should be conservative and schema-guided: it can identify missing required fields, missing required sections, validation issues, dirty state, and unavailable freshness signals, while leaving human-judgment states to explicit future rules.

The readiness engine is a cross-template capability. Project-kb, spec-driven, transcript, decision, and future ontology templates should all benefit from the same engine. Template-specific behavior belongs in ontology contracts, companion docs, saved views, or declarative rules, not in hard-coded engine branches.

## Goals

- compute readiness for any typed note, section, or embedded node with canonical node identity
- support type-filtered listing defaults, facets, sorting, grouping, and agent handoff queries
- distinguish validation issues from readiness gaps while letting both contribute to the row summary
- keep the engine deterministic, explainable, and usable without LLM judgment
- reuse ontology schema, validation output, and node workspace status rather than inventing a parallel health model
- support efficient batch computation for mostly typed repositories
- leave room for future declarative readiness rules without requiring project-specific code

## Non-Goals

- building a workflow-specific board, JIRA clone, or project-kb-only dashboard
- replacing the validation engine or repair-plan/apply-session model
- applying fixes, scaffolds, or generated note content
- making LLM-authored qualitative judgments such as "good enough" or "well written"
- encoding project concepts such as sprint, product, assignee, layer, lens, or priority in the generic engine
- defining the final frontend component layout for the listing workbench
- requiring all readiness inputs to be indexed before the feature can degrade gracefully

## Requirements

### Must

- The engine MUST accept canonical node identity as its primary subject and support note-root, section, and embedded-node instances.
- The engine MUST resolve author-facing locators into canonical node references before computing, caching, or returning readiness.
- The engine MUST compute readiness from generic signals first:
  - missing required frontmatter fields
  - empty required frontmatter fields
  - missing required markdown sections
  - empty required markdown sections
  - missing required embedded-node fields
  - validation issue presence and severity
  - dirty, modified, conflicted, or stale state exposed by edit/session/workspace status
  - unavailable or stale index/assessment inputs
- The engine MUST expose both an aggregate readiness state and the underlying contributing signals.
- The aggregate state MUST be deterministic for the same vault snapshot, schema, validation result, and edit-session state.
- The engine MUST distinguish validation issues from readiness gaps in the output model. A node can be valid but incomplete, invalid but structurally complete, modified but otherwise ready, or unknown because inputs are stale.
- The engine MUST preserve enough provenance for each signal to explain where it came from, including source kind, field name, section heading, embedded child ref, validation check, issue id when available, and source path or node ref.
- The engine MUST expose filterable facets for type/interface, readiness state, issue presence, missing required fields, missing required sections, modified/dirty state, stale/unknown state, and schema-derived enum/status-like fields when available.
- The engine MUST expose sortable summary fields for readiness, title, path, type, issue count, missing required count, modified time when known, and relation count when available.
- The engine MUST support batch queries over all instances of a type or interface without forcing the browser or agent to open each note one at a time.
- The first implementation MUST be read-only. It may point at validation issues or safe repair actions, but it must not apply changes.
- The engine MUST be template-agnostic. Workflow-specific saved views or declarative rule packs may configure the engine, but core readiness states must not branch on project-kb, spec-driven, or other starter names.
- Browser and agent surfaces MUST consume the same readiness service contract so a visible workbench state can become an agent handoff target.
- Readiness outputs MUST include an engine version or profile identifier so cached results, logs, and future rule changes can be explained.

### Should

- The engine should produce row-ready summaries in one response: title, path, node ref, resolved type, summary text when available, readiness state, badges, issue counts, missing requirements, key field chips, and relation counts.
- The engine should prefer indexed ontology/type-instance data for listing queries and use live projection only as a bounded fallback.
- The engine should degrade explicitly when inputs are missing, stale, or unsupported instead of inventing a ready/incomplete answer.
- The engine should support multiple badges per node while still returning one primary state for default grouping.
- The engine should make readiness cheap to recompute after file changes by isolating schema-derived requirements, validation summaries, and edit-session status inputs.
- The engine should expose rule/input fingerprints that let clients decide whether cached readiness is stale.
- The engine should allow future declarative rules to add judgment states such as `needs_decision` only when the signal is explicit and explainable.
- The engine should allow future declarative rules to define targeted readiness goals, such as `phase_3_ready`, when the target can be evaluated from typed internal structure.
- The engine should keep readiness rule evaluation separate from repair planning so non-safe workflow gaps do not become fake automatic fixes.
- The engine should support both API pagination and stable ordering so large typed repositories remain usable.
- Agent-facing output should include enough query metadata to let an agent say what it is working on, such as type, filters, counts, and selected readiness states.

### May

- The engine may expose saved view presets once the general filter/sort/group contract stabilizes.
- The engine may later support ontology-authored readiness rules for relation cardinality, workflow phase applicability, acceptance checks, or required companion artifacts.
- The engine may later support ordered internal gates, such as "all prior phase children are complete before the current phase can be ready."
- The engine may later surface suggested next actions that point to validation repairs, authoring guides, or agent workflow skills, provided those actions remain separate from readiness computation.
- The engine may later persist readiness snapshots for trend reporting, but the initial contract can compute on demand from indexed inputs.

### Readiness states

- `ready`: no known blocking validation issue, all schema-required structure is present, no dirty/conflict state is active, and all required inputs were fresh enough to evaluate.
- `incomplete`: required fields, sections, embedded fields, or required structural blocks are missing or empty.
- `has_issues`: validation reports one or more blocking or user-visible issues for the node.
- `modified`: the node has dirty, externally modified, conflicted, or unsaved state that should be resolved before treating the row as ready.
- `unknown`: readiness could not be evaluated because schema, projection, validation, or freshness inputs are unavailable or stale.
- `needs_decision`: a future state that may only come from explicit declarative rules or workflow metadata. The engine must not infer this from vague prose.

Primary-state precedence for default grouping should be deterministic:

1. `has_issues`
2. `incomplete`
3. `modified`
4. `needs_decision`
5. `unknown`
6. `ready`

The response may also include secondary badges so a row can show, for example, `has_issues` plus `modified` without losing the primary grouping state.

### Data model

The core domain model should remain small and additive:

- `ReadinessSubject`: canonical node ref, author-facing locator, resolved type/interface set, path, title, and parent node ref when embedded.
- `ReadinessSummary`: subject, primary state, badges, counts, facets, sort keys, freshness metadata, and profile/version.
- `ReadinessSignal`: signal id, state contribution, severity, source kind, source ref, display label, machine reason, and optional validation issue id.
- `ReadinessFacet`: field or facet key, type, values, counts, and whether the facet is schema-derived or engine-derived.
- `ReadinessQuery`: type/interface filters, readiness filters, validation filters, missing requirement filters, enum/status filters, text search, grouping, sorting, pagination, and requested detail level.
- `ReadinessProfile`: engine version plus optional declarative rule set identifiers.
- `ReadinessTarget`: optional named goal for scoped readiness, such as whole-node readiness, phase readiness, handoff readiness, or publish readiness.
- `ReadinessGate`: explicit structural condition evaluated for a target, such as child existence, sibling order, status value, relation cardinality, checklist completion, or blocking issue count.

Signal source kinds should include at least `schema_field`, `schema_section`, `embedded_field`, `validation_issue`, `edit_state`, `index_state`, `relation`, `internal_gate`, and `explicit_rule`.

### Targeted readiness over internal structure

The engine should be designed to support scoped readiness targets after the schema-only version ships. A target is a named question about a subject, not a new hard-coded workflow. For example, a spec with ordered phase children could ask whether phase 3 is ready, and the answer could depend on phases 1 and 2 being complete plus phase 3 satisfying its own required structure.

This remains generic when the engine evaluates explicit typed structure:

- ordered child or sibling nodes
- fields or enum/status values on those nodes
- required sections or embedded collections inside the target node
- checklist or acceptance-criteria completion when authored as structured data
- relation cardinality or required linked artifacts
- validation issue counts scoped to the subject or its gated children

The engine MUST NOT infer phase semantics from arbitrary prose. Internal gates only participate in readiness when the ontology, companion metadata, or declarative rule pack identifies the child set, order, target, and completion condition.

Targeted readiness output should explain both the selected target and the failing gate. For example, a phase target could report that `phase_3_ready` is blocked because a prior phase child is not complete or because the current phase is missing a required acceptance section.

Targeted readiness should reuse the same `ReadinessSummary` shape with target metadata rather than creating a second engine.

### Public API surface

The technical contract should expose readiness through a shared backend service and thin transport adapters.

- `POST /api/ontology/readiness/query` should be the primary browser-facing endpoint for listing, filtering, grouping, sorting, and pagination.
- `GET /api/ontology/readiness?ref=<node-ref-or-locator>` may provide a single-subject convenience endpoint for node workspace badges.
- Agent or CLI surfaces should expose the same query shape through a read command, such as `rzm agent ontology-readiness`, once the API contract is implemented.
- The query response should contain:
  - `query`
  - `profile`
  - `total`
  - `groups[]` when grouping is requested
  - `items[]` with `ReadinessSummary`
  - `facets[]`
  - `warnings[]` for stale or unavailable inputs
- The single-subject response should contain one `ReadinessSummary` plus signal detail.

### Data flow

Readiness should be computed as a derived projection over existing sources:

1. Load ontology type metadata and required field/section contracts.
2. Resolve type or interface filters to canonical type-instance subjects.
3. Resolve each subject to canonical node identity.
4. Join validation summary/issues by canonical node ref or best available path/field binding.
5. Join edit-session and node workspace status by canonical node ref.
6. Evaluate schema-required structure signals.
7. Evaluate validation, edit-state, and freshness signals.
8. Apply optional declarative readiness rules when present and versioned.
9. Aggregate signals into primary state, badges, counts, facets, and sort keys.
10. Return summaries and optional detail without mutating notes.

The implementation should treat indexed type-instance catalogs, node workspace snapshots, and validation summaries as accelerators under the engine. The readiness engine owns aggregation semantics, not low-level parsing or validation.

### Caching and freshness

- Readiness summaries may be cached, but cache keys MUST include schema fingerprint, node content fingerprint, validation fingerprint, edit-session version when present, readiness profile/version, and subject ref.
- Listing queries may cache grouped/faceted results only when the underlying item summaries are fresh for the same query fingerprint.
- Unknown or stale inputs MUST be represented in the output rather than silently omitted.
- File changes, schema changes, validation reruns, and edit-session mutations MUST be sufficient to invalidate affected readiness summaries.
- The cache model MUST not require global invalidation for one changed note when canonical subject fingerprints are available.

### Integration points

- `SPEC-0014` owns the product behavior for the ontology browser and typed listing workbench.
- `SPEC-0015` owns navigation and pane-stack experience concerns.
- `SPEC-0017` owns validation-fix user workflows and repair recommendations.
- `SPEC-0018` owns repair-plan and apply-session technical behavior.
- `SPEC-0019` owns the canonical node workspace and node status contract.
- `SPEC-0022` noderef read API work should remain an accelerator and identity source, not a competing readiness model.
- The generic-skills decision remains a constraint: workflow semantics belong in ontology surfaces, companion docs, or declarative rules rather than copied into every skill.

## User Stories

### US1 - See an explainable readiness grouping without opening every note
- id:: ^SPEC-0053-US1
- summary:: See an explainable readiness grouping without opening every note.
- status:: ready

#### Acceptance Criteria

- A type-filtered readiness query returns rows grouped by primary readiness state.
- Each row includes canonical node identity, title/path, resolved type, primary readiness state, badges, issue count, missing requirement count, and enough summary text to choose the next note.
- Expanding a row or requesting detail shows the signals that caused the readiness state.
- Validation issues and readiness gaps are labeled separately.

### US2 - Query the same readiness data the browser uses and turn a filtered result into a handoff target
- id:: ^SPEC-0053-US2
- summary:: Query the same readiness data the browser uses and turn a filtered result into a handoff target.
- status:: ready

#### Acceptance Criteria

- The agent-facing command or API accepts type/interface, readiness, issue, missing requirement, and text filters.
- The response includes query metadata, total counts, grouped counts, item summaries, and warnings about stale inputs.
- The response includes stable node refs that can be passed to node workspace, validation, or authoring-guide flows.
- The response does not depend on browser-only client state.

### US3 - Get useful readiness signals from schema contracts before writing template-specific code
- id:: ^SPEC-0053-US3
- summary:: Get useful readiness signals from schema contracts before writing template-specific code.
- status:: ready

#### Acceptance Criteria

- Required frontmatter fields, required sections, and embedded required fields produce readiness signals for any ontology type.
- A template can improve readiness behavior through ontology metadata, companion docs, saved views, or future declarative rules.
- The core readiness engine contains no starter-name branches for project-kb, spec-driven, transcript, decision, or similar templates.

### US4 - Add explicit decision or workflow-readiness rules without weakening deterministic schema-derived readiness
- id:: ^SPEC-0053-US4
- summary:: Add explicit decision or workflow-readiness rules without weakening deterministic schema-derived readiness.
- status:: draft

#### Acceptance Criteria

- Declarative rules can add explicit signals such as `needs_decision` while preserving provenance and profile/version metadata.
- Rule-driven signals are clearly marked as explicit-rule signals.
- Rule failures or stale rule inputs degrade to warnings or `unknown` rather than inferred judgment.
- Existing schema-derived readiness behavior continues to work when no rule pack is present.

### US5 - Ask whether a specific internal phase or stage is ready based on typed child structure and explicit gates
- id:: ^SPEC-0053-US5
- summary:: Ask whether a specific internal phase or stage is ready based on typed child structure and explicit gates.
- status:: draft

#### Acceptance Criteria

- A readiness query can name a target such as `phase_3_ready` when a declarative rule or ontology contract defines that target.
- The engine can evaluate ordered child or sibling gates, such as prior phases complete before the current phase is ready.
- The engine explains which internal gate blocks the target when the target is not ready.
- The engine does not infer phase order, completion, or acceptance semantics from arbitrary prose.
- Targeted readiness uses the same summary, signal, facet, and agent-facing output model as ordinary readiness.

## Open Questions

- Should the first implementation expose the agent surface as a new `rzm agent ontology-readiness` command or fold it into an existing report/query command?
- Should `modified` outrank `incomplete` for default grouping when a note is both incomplete and dirty, or is the proposed precedence better for triage?
- Which validation severities should count as `has_issues` in the first version?
- Where should future declarative readiness rules live: ontology SDL directives, companion markdown, `.rhizome` config, or saved view definitions?
- Should targeted readiness use a small built-in gate language, ontology SDL directives, or typed markdown rule notes as its authoring surface?
- Should readiness snapshots be persisted for trend reporting in the first implementation, or should trend reporting wait until the query contract has stabilized?
