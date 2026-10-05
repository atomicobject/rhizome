---
type: ProductSpec
id: SPEC-0080
summary: "Defines one base Rhizome skill that gives agents reliable core operating guidance and routes lazily to focused references without owning starter-specific workflows."
spec-status: active
last-updated: 2026-09-12
aliases:
  - SPEC-0080
  - base-rhizome-agent-guidance
---

# Base Rhizome Agent Guidance

## Summary

Rhizome should give an agent one coherent expert entrypoint for using Rhizome itself. A core `rhizome` skill owns the universal mechanics—session bootstrap, search, file context, exact evidence, setup, configuration, indexing, validation, repair, safe Markdown mutation, structured notes, ontology operations, reports, and integration diagnosis—while starter and workflow skills continue to own deliverables such as coding, specification, planning, and domain work.

Agents use Rhizome without being asked when a change touches a boundary, contract, or invariant that a note, spec, or decision record may document; when the task needs the rationale behind existing behavior; when a note or attachment moves or a linked heading is renamed; or when the work changes reusable intent, rationale, or an operating boundary that a durable note should record. A local edit whose whole effect is visible in the diff, with no unresolved dependency, needs none of this. Guidance names these conditions rather than a blanket "use proactively" default, which current model guidance shows overtriggers.

The skill stays lean by classifying the request, selecting one primary route, and loading only the reference material needed for that route. The managed `RHIZOME.md` block becomes a small integration and routing contract rather than a second manual. Rhizome-specific skill authoring is opt-in and composes with whatever general skill-authoring guidance the active harness provides.

## Goals

- give agents one discoverable base skill for proactive, proportional repository knowledge use and explicit Rhizome operations
- make session, search, file-context, and exact-evidence behavior useful without loading a large command manual
- route setup, configuration, indexing, validation, repair, Markdown mutation, structured-note, ontology, and reporting work to focused references
- preserve clear ownership between universal Rhizome mechanics and starter-specific workflow skills
- make missing or broken Rhizome integration visible instead of letting agents imitate an integrated experience
- make Rhizome-enabled skill authoring an explicit, capability-based opt-in

## Non-Goals

- owning ordinary coding, specification, planning, implementation, or domain deliverables; base mechanics support those workflows
- naming bundled starters, starter-specific artifacts, or starter query recipes in the base skill
- loading every Rhizome reference for every request
- treating GraphQL as a replacement for broad semantic search or exact symbol evidence
- requiring a model-routing evaluation harness in CI or normal quality gates

## User Stories

### US1 - Start and use a Rhizome session for core retrieval work

- id:: ^SPEC-0080-US1
- summary:: An agent can start or reuse a Rhizome session, search for concepts and rationale, gather context for known files, and obtain exact code or note evidence without guessing command behavior.
- status:: ready

#### Acceptance Criteria

- The base skill classifies the task before choosing commands, retrieves repository knowledge without an explicit Rhizome request under the named conditions in the Summary (documented boundary or contract, needed rationale, note or heading mutation, durable-knowledge change), and reuses current context and an existing session when available. A local edit whose whole effect is visible in the diff requires no session or discovery. ^SPEC-0080-US1-AC1
- Normal startup guidance uses `rzm agent start --intent "<task>"` with repeatable `--file` seeds only when useful; file seeds share one context budget, so the set stays small and puts the most important seed first. It omits `--profile` and `--ontology`, and adds `--submodule-depth 1` only when immediate child-module guidance matters. ^SPEC-0080-US1-AC2
- Search guidance distinguishes broad semantic discovery, known-path file context, typed repeatable GraphQL/query-recipe retrieval, and exact code-symbol/reference proof. ^SPEC-0080-US1-AC3
- The skill uses `rzm agent surface` as the authority for current `rzm agent` flags and capabilities instead of preserving a large frozen command inventory; focused references use current top-level help/docs for installation, configuration, indexing, and other non-agent command families. ^SPEC-0080-US1-AC4

- Retrieved instructions are applied only from authorized instruction sources within their scope. Agents distinguish applicable approved contracts and current ontology structure/semantics from historical, candidate, descriptive, quoted, or generated evidence; retrieval alone grants no authority or approval. Conflicting intended and observed behavior remains explicit. ^SPEC-0080-US1-AC5

### US2 - Diagnose and maintain a working Rhizome installation

- id:: ^SPEC-0080-US2
- summary:: An agent can help a user install, initialize, configure, index, validate, repair, and troubleshoot Rhizome through focused guidance appropriate to the problem.
- status:: ready

#### Acceptance Criteria

- The skill routes installation and integration diagnosis, session use, configuration, indexing/freshness, validation/repair, and reports/health to separate references and loads only the reference material relevant to the request. ^SPEC-0080-US2-AC1
- Installation guidance uses the project launcher for follow-up commands, uses existing authorization or asks before running init, accepts normal recommended changes within that authority, and explicitly enables managed guidance plus the shared skill surface only when re-enabling them is authorized. ^SPEC-0080-US2-AC2
- Verification is route-specific: read-only retrieval needs no blanket validation; configuration/index work checks resolution and freshness; repair reruns the same selector and scope; reports use only live-advertised operations. ^SPEC-0080-US2-AC3
- The skill distinguishes a missing skill, missing CLI, uninitialized repository, failed session, stale index/capability, and command failure instead of collapsing them into one fallback. ^SPEC-0080-US2-AC4

### US3 - Change Markdown and ontology-backed content safely

- id:: ^SPEC-0080-US3
- summary:: An agent can safely rename or move Markdown, author structured notes, and work with ontology usage or schema changes while respecting durable links and mutation authority.
- status:: ready

#### Acceptance Criteria

- Markdown file/attachment moves and externally linked heading renames are hard routes to Rhizome guidance and use Rhizome mutation surfaces rather than direct filesystem edits. ^SPEC-0080-US3-AC1
- Structured Markdown guidance discovers the live type, schema, authoring contract, and relevant typed neighborhood before authoring; it never guesses roots, fields, or relations. ^SPEC-0080-US3-AC2
- Ontology usage/diagnosis and ontology schema authoring/migration are separate references, and schema authoring is treated as the higher-risk route. ^SPEC-0080-US3-AC3
- Mutation steps respect the user's authority and run only the checks relevant to the changed links, identifiers, note type, recipes, views, or schema. ^SPEC-0080-US3-AC4

- Agents update the smallest useful durable knowledge surface when work changes reusable intent, rationale, or an operating boundary; they discover the configured type and bindings without assuming starter types or recipes, and create no note solely to narrate trivial work. ^SPEC-0080-US3-AC5

### US4 - Compose Rhizome mechanics with workflow and skill-authoring guidance

- id:: ^SPEC-0080-US4
- summary:: An agent can combine the base Rhizome skill with a more-specific workflow skill or general skill-authoring guidance without duplicating ownership or forcing Rhizome into unrelated work.
- status:: ready

#### Acceptance Criteria

- A more-specific workflow skill owns the deliverable and composes with `rhizome` only for universal mechanics; the base skill does not seize workflow ownership. ^SPEC-0080-US4-AC1
- Workflow skills supply intent, anchors, workflow-specific context, acceptance semantics, and the next decision; the base skill owns session reuse, retrieval, live schema discovery, safe mutation, and proportional Rhizome validation. Composition reuses current evidence and preserves existing authorization rather than repeating universal procedures or approval requests. ^SPEC-0080-US4-AC2
- When creating or revising a skill, the managed agent contract asks whether Rhizome capabilities would materially help only when the answer is not already explicit, naming concrete examples such as semantic search, file context/docs bindings, typed GraphQL/query recipes, ontology-aware Markdown, graph relationships, validation, or safe note mutation. ^SPEC-0080-US4-AC3
- When the user opts in, Rhizome skill-authoring guidance composes with whatever general skill-creation guidance is available and records whether Rhizome is required or an enhancement with a degraded path. ^SPEC-0080-US4-AC4

### US5 - Know whether Rhizome is actually active

- id:: ^SPEC-0080-US5
- summary:: A user is told when the promised Rhizome-integrated experience is incomplete, what layer is missing, and how to restore it.
- status:: ready

#### Acceptance Criteria

- The managed agent guidance routes work meeting the Summary's named conditions, explicit Rhizome work, and hard-gated Markdown mutations to the base skill; a local edit whose effect is visible in the diff stays lightweight, and unavailable integration is reported at the failed layer. ^SPEC-0080-US5-AC1
- An explicit Rhizome operation, structured-note mutation, or risky move/rename stops when the required integration is unavailable; ordinary workflow work reports the confidence or validation gap to its owning skill. ^SPEC-0080-US5-AC2
- Integration verification covers the project launcher and pin, configuration resolution, managed block, installed `rhizome` skill, and a minimal project-launcher session start. ^SPEC-0080-US5-AC3
- Guidance never silently substitutes generic shell behavior while implying Rhizome was used. ^SPEC-0080-US5-AC4

### US6 - Manually evaluate base-skill routing before broadening automation

- id:: ^SPEC-0080-US6
- summary:: A maintainer can manually exercise representative routing cases through a logged-in Codex subscription without adding model calls to CI or normal checks.
- status:: ready

#### Acceptance Criteria

- The harness is a separate, explicitly invoked manual tool and is not wired into CI, `make check`, or routine validation. ^SPEC-0080-US6-AC1
- Before designing or implementing the harness, maintainers use a documented grill-with-docs conversation to settle cases, evidence, cost controls, isolation, and human review; record the decision source and require separate approval before model execution. ^SPEC-0080-US6-AC2
- Harmless preflight/list/dry-run behavior is distinct from explicit model execution, and model runs use disposable fixtures with recorded prompts, versions, outputs, diffs, and human-review evidence. ^SPEC-0080-US6-AC3
- Deterministic init/template behavior remains covered by ordinary tests; the manual harness evaluates model routing and integrated behavior rather than duplicating product tests. ^SPEC-0080-US6-AC4

### US7 - Keep consolidated guidance accurate and operationally complete

- id:: ^SPEC-0080-US7
- summary:: An agent can rely on the consolidated skill, its embedded references, and adjacent workflow routes without encountering dead resources, inaccurate defaults, or missing safety constraints.
- status:: satisfied

#### Acceptance Criteria

- Runtime onboarding planning loads the canonical embedded onboarding reference, and a regression test fails if that resource is missing. ^SPEC-0080-US7-AC1
- Command examples match live behavior for content loading, agent-surface scope, reports, configuration, onboarding, and minimal session startup. ^SPEC-0080-US7-AC2
- Focused references retain the safety constraints needed for validation repair, structured Markdown, ontology authoring, documentation bindings, skill authoring, onboarding, and graph-safe mutations without restoring retired skill duplication. ^SPEC-0080-US7-AC3
- The spec-driven development-loop routes unfamiliar codebase or subsystem orientation through the base `rhizome` onboarding reference and composes with its lean session defaults. ^SPEC-0080-US7-AC4

## Requirements

- The installed core skill MUST be named `rhizome` and MUST expose focused references for installation/integration, sessions, evidence/composition, search/code evidence, file context, onboarding, configuration, indexing/freshness, validation/repair, Markdown mutations, structured Markdown, ontology usage, ontology authoring, documentation bindings, reports/health, and opted-in skill authoring.
- The skill MUST select one primary route and MUST load additional references only when they materially affect the outcome.
- The skill MUST keep starter-specific names, spec/effort terminology, and starter-owned recipe ids out of its base body and references.
- The managed `RHIZOME.md` guidance MUST remain a lean router and integration check, not a duplicate operational manual.
- The core guidance refresh MUST retire `rhizome-onboard`, `rhizome-note-authoring`, `rhizome-ontology`, and `rhizome-skill-creator` as managed core skills without leaving compatibility wrappers.
- GraphQL guidance MUST describe typed note/relationship queries, semantic-only note/node search and ontology-scoped surveys, schema-validated recipes, authoring/schema metadata, and bounded known-path code/document/test evidence; it MUST NOT describe GraphQL as unified note-and-code search or the code runtime as arbitrary code search.
- Workflow guidance MUST compose through the installed base skill, keep workflow-specific lifecycle and context selection with its owner, and avoid duplicating universal mechanics. Existing retired-skill cleanup remains a refresh compatibility contract.

## Open Questions

The behavioral evaluation effort records the documented grill-with-docs design source and bounded launch approval separately from harness implementation. Model calls remain manual-only; each launch manifest identifies its approved model/reasoning, cases, execution and cost caps, and evidence destination. Model and run limits belong to the approved effort and launch manifest; this specification does not authorize model execution.

## Documentation Plan

- consolidate core skill guidance under `pkg/app/cli/init/templates/skills/markdown/rhizome/`
- simplify `docs/rhizome-md-templates/RHIZOME.md` to routing and integration guidance
- align process and agent-surface documentation with the single-skill ownership model
- correct stale GraphQL capability wording where it would misroute agents
- maintain evidence-authority and composition guidance in the base skill and its focused reference; starter implementations consume this boundary
