---
type: ReferenceDoc
reference-kind: analysis
summary: "Migration matrix for moving legacy specs/ and docs/ content into Rhizome's spec-driven note structure."
derived-from:
  - specs/
  - docs/
  - docs/specs/process/README.md
  - docs/reference/README.md
  - docs/reference/analysis/README.md
---

# Legacy specs + notes migration matrix

## Summary

The legacy corpus is mixed. Some files are true system contracts, some are delivery records, some are durable rationale, and some are just one-off change proposals from an earlier era of note writing.

The migration rule is simple:

- keep durable system behavior, rationale, and operating guidance
- move active execution into efforts
- route evergreen supporting material into reference docs
- demote hubs to thin entrypoints
- archive or drop one-off change requests after their enduring content is backported

## Mapping Rules

| Legacy artifact | Recommended destination | Guidance |
| --- | --- | --- |
| `spec.md` | `docs/specs/product/`, `docs/specs/technical/`, `docs/specs/experience/`, or `docs/specs/operations/` | Keep only durable intended-state behavior, invariants, and user-facing contracts. If a spec mostly describes a single change request, mine it for lasting constraints and then archive it. |
| `plan.md` | `docs/efforts/` | Treat as live execution unless it contains durable architectural rationale. Roadmaps and task breakdowns do not belong in canonical specs. |
| `tasks.md` | `docs/efforts/` | Execution checklist only. Never canonical. Preserve only while the effort is active. |
| `research.md` | `docs/reference/analysis/` or `docs/reference/decisions/` | Keep durable findings, tradeoffs, and edge cases. If the file ends in a choice, split that choice into a decision note. |
| `data-model.md` | `docs/specs/technical/` or `docs/reference/domain/` | Put authoritative entities and contracts in a technical spec. Use reference notes only for explanatory context. |
| `contracts/*` | `docs/specs/technical/` | Treat machine-readable contracts as authoritative, then link out from reference docs for examples or rationale. |
| `quickstart.md` | `docs/reference/guides/` or `docs/efforts/` | Keep only if the guidance is evergreen or validation-oriented. Otherwise fold it into the relevant effort. |
| `checklists/*` | `docs/reference/requirements/` | Keep verification and quality criteria here unless they are part of a process spec. |
| `CONTEXT_RECIPE.json` | archive or effort attachment | Useful provenance for migration, but not a long-lived knowledge artifact. |
| `docs/hubs/*` | `DocumentationHub` only | Hubs should be thin navigation pages, not content containers. |
| `docs/design/*` | split into specs, decisions, and reference docs | These are usually the biggest migration source. Keep the durable architecture; discard the rest. |
| `docs/vision/*` | one durable overview plus supporting specs/reference docs | Collapse repeated narrative into a smaller set of canonical notes. |
| `docs/code-anchors/*` | keep as sidecar reference material for now | Preserve the behavior knowledge, but do not force code anchors into the core ontology model prematurely. |

## Recommended Destination Families

| Destination family | Use for | Typical source material |
| --- | --- | --- |
| `docs/specs/process/` | workflow norms and the spec-driven operating model | `spec-driven` process docs, authoring workflow, effort lifecycle |
| `docs/specs/product/` | user-visible product intent | browser UX, search behavior, agent workflow expectations |
| `docs/specs/technical/` | subsystem contracts and invariants | indexing, ontology, search, code intelligence, runtime architecture |
| `docs/specs/experience/` | interaction models and navigation behavior | browser layout, pane behavior, reading flow |
| `docs/specs/operations/` | operational guarantees and recovery behavior | watcher failures, cache staleness, rebuild behavior, degraded mode |
| `docs/reference/decisions/` | durable rationale for choices | single-writer semantics, locking, path normalization, contract tradeoffs |
| `docs/reference/analysis/` | comparative analysis, migration matrices, and tradeoff inventories | backport plans, cluster reviews, keep/drop rationale |
| `docs/reference/guides/` | evergreen how-to guidance | setup, validation, authoring workflow, recovery steps |
| `docs/reference/domain/` | canonical vocabulary and conceptual framing | ontology, search concepts, note-family semantics |
| `docs/reference/requirements/` | checklists and verification criteria | acceptance checklists, review rubrics, readiness gates |

## Cluster Guidance

### 1. Core infrastructure cluster

Keep and consolidate:

- `Code Index - Unified SQLite DB`
- `Code Index - Design decisions + invariants`
- `Indexing pipeline architecture (Design)`
- `Indexing pipeline - Concurrency + batching requirements`
- `Indexing pipeline - End-to-end walkthrough`
- `Indexing pipeline - rzm index orchestration`
- `Embeddings - indexing pipeline`
- `Embeddings - ranking + graph blend`
- `Search - Intent and weight tuning`
- `Search - Execution semantics`
- `PathRef contract`
- `Dirty tracking + Refresh semantics`
- `LiveRuntime (async server bootstrap)`
- `Watcher design + degraded mode`

Migration guidance:

- keep the durable architecture in `docs/specs/technical/`
- move irreversible choices into `docs/reference/decisions/`
- move operational failure modes into `docs/specs/operations/` or `docs/reference/guides/`

Drop guidance:

- one-off implementation plans
- duplicated walkthrough prose after the technical spec exists
- obsolete optimization ideas that no longer describe the system

### 2. Ontology and browser cluster

Keep and consolidate:

- `Ontology system (Design)`
- `Ontology (Design)`
- `Ontology browser + Knowledge Workspace`
- `Structural note nodes` as the durable follow-on direction

Migration guidance:

- `032-ontology-browser` is useful planning context
- `033-structural-note-nodes` is the strongest recent spec-shaped artifact and should survive as a durable system contract
- split browser behavior into `docs/specs/product/` and `docs/specs/experience/`, then keep ontology/runtime invariants in `docs/specs/technical/`

Drop guidance:

- duplicated browser prose once the spec family is complete
- any older note that only rephrases the same product direction without adding a new constraint

### 3. Documentation binding cluster

Keep:

- `Coderefs - scanning + indexing`
- `Coderefs - rewrite on rename + move`
- `Code - docs binding (coderefs + code anchors)`
- `Code anchors - matching + scopes`
- `Code anchors - watcher + incremental updates`
- `Code anchors - frontmatter syntax`
- `Code anchors - language support (design + testing)`
- `Rhizome documentation - Anchors + coderefs design`
- `Rhizome documentation - Layering + authoring workflow`

Migration guidance:

- keep coderef behavior in reference docs and technical specs
- keep code-anchor material for now, but treat it as a sidecar surface until the ontology grows a clean family for it
- keep `docs/reference-notes/Spec-driven delivery starter - note families.md` and `...adoption guide.md` as supporting reference, not as the operating model itself

Drop guidance:

- redundant explanatory copies once the binding contract is captured in one canonical spec/reference set

### 4. Vision and operating-model cluster

Keep:

- `Rhizome - What It Is and Why It Exists`
- `Team-wide knowledge base (Markdown + Obsidian metaphor)`
- `Agent-ready workflows (prompts, commands, skills)`
- `Intent - Knowledge funnel + AI-forward development`

Migration guidance:

- collapse these into one durable overview plus a few targeted specs and reference docs
- preserve the high-level rationale, but stop letting vision pages carry normative contracts

Drop guidance:

- repeated manifesto-style paragraphs
- overlapping explanations that do not add a new architectural or product constraint

### 5. Early feature-spec cluster

Mostly archive after backport:

- `001-backlink-support`
- `002-rename-note-backlinks`
- `003-mcp-cache`
- `004-vault-health`
- `005-llm-integration`
- `006-unified-community-graph`
- `007-mcp-cache-invalidation-hints`
- `008-semantic-query-context-packing`
- `009-intel-context-pack`
- `010-unified-watcher`
- `011-unified-files-tool`
- `012-path-handling-centralization`
- `013-ollama-onboarding`
- `014-edge-kind-registry`
- `015-configurable-template-updates`

Keep only the durable parts:

- path normalization rules
- indexing and cache invariants
- retrieval semantics
- contract examples that still match the current system

Drop guidance:

- change-request scaffolding that no longer describes a subsystem boundary
- implementation plans that were later superseded by more complete specs or decisions

### 6. Late feature-spec cluster

Keep and review closely:

- `027-intent-driven-compression`
- `028-unified-path-refs`
- `029-search-intent-catalog`
- `031-call-edge-reverse-index`
- `032-ontology-browser`
- `033-structural-note-nodes`

Migration guidance:

- these are the most likely to survive into the new structure
- `031` carries durable indexing/search invariants
- `032` is useful as planning context and product direction
- `033` is the closest to the new target shape and should be preserved as canonical source material
- `029` should be kept if search intents remain a stable user-facing contract

Drop guidance:

- only drop after the enduring constraints have been backported into the relevant spec/decision/reference notes

## Keep / Drop Criteria

Keep a legacy note if it does at least one of these things:

- defines a stable subsystem boundary
- records an invariant or safety property
- explains a durable architectural tradeoff
- documents an operational hazard or recovery path
- carries reusable vocabulary that the new ontology should preserve

Drop or archive a legacy note if it mostly does one of these things:

- narrates a single change request that no longer matters
- duplicates later notes without adding new constraints
- describes execution state that belongs in an effort note
- preserves a plan that has already been superseded

## Review Order

If we are pruning the corpus, review it in this order:

1. `docs/specs/process/` and the new `docs/reference/*` entrypoints
2. the core infrastructure cluster
3. the ontology and browser cluster
4. the documentation-binding cluster
5. the vision and operating-model cluster
6. the late feature-spec cluster
7. the early feature-spec cluster
8. `docs/code-anchors/*` last, because that surface is still the least cleanly modeled

## Bottom Line

The safest move is not to flatten everything into one giant spec set. Backport the durable material into a smaller number of living specs, keep decision notes for the irreversible choices, and let reference analysis carry the migration record.

If a note no longer adds an invariant, a tradeoff, or a reusable contract, it can be dropped after the enduring content has been preserved elsewhere.
