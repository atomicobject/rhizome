---
type: TechnicalSpec
summary: "Defines canonical, graph-visible external code reference targets that remain explicitly non-source-backed and are classified from high-confidence language evidence."
id: SPEC-0081
spec-status: active
last-updated: 2026-07-19
aliases:
  - SPEC-0081
---

# External code reference targets

## Summary

Rhizome will represent high-confidence external calls (including construction syntax), type references, member references, and imports as canonical graph endpoints without indexing dependency source or claiming a local definition. External targets extend the semantic code index spine with a pathless, explicitly external identity that reuses normalized reference targets across all uses.

Classification is evidence-based and conservative. Direct import bindings, Node built-ins, language builtins, and a curated JavaScript runtime-global catalog qualify; an unresolved local lookup or unknown receiver member does not. The first proving slice is npm/Node/JavaScript, but the persistence, identity, lifecycle, metrics, and query contracts are language-neutral.

This companion contract extends [[docs/specs/technical/semantic-code-index-spine|SPEC-0011]] and the navigation behavior in [[docs/specs/product/code-intel|SPEC-0015]]. It composes with, but does not modify in this effort, the active indexing and graph-read contracts in [[docs/specs/technical/indexing-pipeline-architecture|SPEC-0012]] and [[docs/specs/technical/noderef-batch-traversal-read-api|SPEC-0022]].

## Goals

- make important external dependencies visible in code graphs and reference queries
- reuse one canonical target across many calls (including construction syntax), type refs, member refs, and imports
- keep external identities unmistakably separate from source-backed definitions
- classify only evidence-backed package/runtime identities and preserve genuine unknowns
- bound storage, indexing time, query latency, and garbage-collection cost
- support npm/Node/JavaScript first without embedding JavaScript-specific identity assumptions in the durable model

## Non-Goals

- scanning or indexing `node_modules`, package caches, SDK source, or other dependency trees
- creating source chunks, FTS documents, embeddings, code anchors, snippets, or definition locations for external targets
- replacing LSP/package-manager resolution or claiming complete semantic resolution
- materializing nodes for arbitrary receiver members or unbound names
- making all external targets general semantic-search results by default
- showing external targets in Explorer, default graph browsing, workspace navigation, or ordinary node/result lists
- supporting pre-#174 TypeScript index output; implementation starts from the merged current-main resolver and rebuild contract
- introducing a distinct durable `constructs` relation in the first slice; construction syntax remains a call until a follow-up changes the repository-wide relation vocabulary
- resolving package export maps from dependency source or parsing lockfiles in the first slice

## User Stories

### US1 - Canonical evidence-backed external identity

- id:: ^SPEC-0081-US1
- summary:: Classify a high-confidence external or runtime reference once and reuse its canonical identity across every durable use.
- status:: satisfied

#### Acceptance Criteria

- Direct named, default, namespace, and type import bindings classify to an ecosystem/module/symbol identity without scanning dependency source. ^SPEC-0081-US1-AC1
- Node built-ins, language builtins, and curated JavaScript globals classify as explicit runtime builtin/global targets. ^SPEC-0081-US1-AC2
- Unknown receiver members, unbound bare names, and local-resolution misses without qualifying evidence remain `unknown` and create no external node. ^SPEC-0081-US1-AC3
- Calls (including construction syntax), type refs, member refs, and imports to the same canonical identity reuse one external node while retaining the durable relation kinds the first slice supports. ^SPEC-0081-US1-AC4
- Canonical identity uses ecosystem, canonical public module specifier, symbol path, and target kind; the first slice records nearest-`package.json` declared ranges as optional provenance rather than identity. ^SPEC-0081-US1-AC5

### US2 - Honest graph and query behavior

- id:: ^SPEC-0081-US2
- summary:: Navigate external dependency relationships without receiving a false source definition or semantic-search document.
- status:: satisfied

#### Acceptance Criteria

- Explicit dependency-impact and reference queries can return a pathless `external` endpoint with ecosystem/module/symbol/class/version-provenance metadata; Explorer and default graph/workspace/list queries never include it. ^SPEC-0081-US2-AC1
- External nodes never appear in Explorer, default graph browsing, ordinary node lists, general search, code-anchor, source-chunk, FTS, embedding, code-anchor-target, snippet, PageRank-definition, or go-to-definition results. ^SPEC-0081-US2-AC2
- Local source-backed resolution takes precedence, and definition-oriented APIs continue to report no local definition for an external target. ^SPEC-0081-US2-AC3
- Explicit impact/reference queries expose calls, type refs, member refs, and imports with stable ordering, evidence-derived classification, bounded results, and explicit source unavailability. ^SPEC-0081-US2-AC4
- Code statistics report disjoint `local_resolved`, `external_classified`, `runtime_global_builtin`, and `unknown` counts for both durable associations and unique targets, including calls and type refs. ^SPEC-0081-US2-AC5

### US3 - Bounded, convergent indexing lifecycle

- id:: ^SPEC-0081-US3
- summary:: Rebuild, incrementally update, and prune external targets within explicit storage and latency budgets.
- status:: satisfied

#### Acceptance Criteria

- Per-path replacement, watcher deletion, full purge, and full rebuild remove stale reference evidence and prune raw/external targets that have no remaining refs or imports. ^SPEC-0081-US3-AC1
- External evidence and canonical mappings persist after the durable batch boundary with no per-file store lookup on the hot ingest path; classification is derived from a consistent read snapshot. ^SPEC-0081-US3-AC2
- Full-index p50 regression is at most 5% and p95 at most 10%; classification/reference persistence regression is at most 15%. ^SPEC-0081-US3-AC3
- Classification/provenance storage adds at most 10% to the measured reference subsystem, with a 5% stretch target and no initial second external-edge table. ^SPEC-0081-US3-AC4
- Endpoint fan-in 1,000 is at most 50ms warm/100ms cold p95, a bounded 1,000-edge external graph is at most 150ms warm/300ms cold p95, and warm code stats is at most 250ms on the reference corpus. ^SPEC-0081-US3-AC5
- Query-time edge projection remains the default while it meets the hard endpoint budget. Relative comparisons with a dedicated materialized variant are advisory when the absolute projection latency remains below 1ms; materialization requires evidence of a material absolute latency or lifecycle benefit. ^SPEC-0081-US3-AC6

## Requirements

### Identity and provenance

- The implementation MUST reuse the existing normalized raw target rows where applicable and MUST NOT create a node per raw syntax occurrence or call.
- It MUST map a raw symbol target to at most one canonical external identity while allowing multiple raw spellings to map to the same external identity.
- It MUST keep immutable binding/runtime evidence and confidence on the durable reference association when evidence differs by use; target-level metadata MUST NOT promote weaker uses.
- It MUST generate a deterministic external handle from language-neutral canonical fields.
- Npm package subpaths MUST remain part of the canonical public module specifier; scoped package roots remain intact.
- Node builtin spellings such as `path` and `node:path` MUST converge to the canonical `node:path` module.
- Named and namespace imports of the same export MUST converge to the same symbol path; a default import uses symbol path `default`, with the local alias retained only as evidence.
- Unresolved repository workspace packages and `tsconfig` path aliases MUST remain `unknown`, not external. A source-backed local resolution still wins.
- Version provenance MUST be optional. The first slice records only the nearest applicable `package.json` declared range, manifest path, and source scope; exact lockfile versions are deferred.
- Missing or conflicting version evidence MUST NOT turn an otherwise valid logical identity into an unknown target.

### Classification

- Every durable call/type/member/import association MUST fall into exactly one of `local_resolved`, `external_classified`, `runtime_global_builtin`, or `unknown`.
- Source-backed local resolution MUST take precedence over external classification.
- External classification MUST require import binding, explicit language-builtin evidence, or an approved versioned runtime catalog entry.
- Unknown receiver members MUST NOT be classified by name similarity.
- Runtime catalogs MUST be explicit, testable, and versioned independently of the stored external identity.
- The four-way class MUST be derived at query/statistics time: a current local anchor/definition join wins; otherwise qualifying package or runtime evidence selects its class; absence of either is `unknown`.
- Current-package fallback spellings without a joining local definition MUST remain `unknown`; the first slice MUST NOT rewrite those raw spellings into external identities.

### Persistence and lifecycle

- The initial design MUST materialize at most one external catalog row per canonical classified identity and MUST project use edges from durable refs/imports at query time.
- Persistence MUST include an explicit raw-target-to-external mapping keyed by raw target ID and separate sparse evidence rows keyed to qualifying symbol-reference associations.
- External import bindings and import-only modules MUST use a new import-evidence row family. The existing local-module `intel_import_refs(src_path,module)` contract MUST remain unchanged.
- Import-only module targets MUST use the same external catalog without fake symbol or code-anchor rows.
- Replacement and deletion MUST remove association/import evidence first. Garbage collection MUST then remove raw-target mappings without qualifying evidence, external catalog rows without mappings/import evidence, and raw targets without remaining refs/imports.
- Garbage collection MUST be set-based, transactionally consistent with replacement/purge, and safe for targets shared by multiple files.
- Indexer output changes MUST follow the repository indexer-version/rebuild contract.

### Query and UX

- External endpoints MUST be returned only by explicit dependency-impact/reference operations, never ambient discovery or browsing.
- Explorer, default graph reads, workspaces, note/code navigation, ordinary node lists, and general search MUST exclude external targets by construction: default read models do not read the external catalog or evidence tables.
- Existing graph reads MUST remain unchanged; a caller MUST select the dedicated external-reference capability rather than broadening a default graph flag.
- Definition, exact-symbol search, and source-navigation surfaces MUST preserve their source-backed truth boundary.
- Targeted API/tool payloads MUST report source unavailability and MUST NOT masquerade as note or code-definition payloads.
- The dedicated operation MUST accept either an exact external handle or structured `ecosystem + module + optional symbol prefix` candidate input. Candidate resolution MUST be deterministic and return ambiguity rather than guessing.
- Reverse fan-in output MUST group bounded results by relation kind and return owner FQN/path, canonical handle, evidence kind/confidence, optional manifest provenance, and `sourceAvailable: false` for the external endpoint.
- The first slice MUST leave this operation uncached. A later dedicated cache MAY use its own reference/evidence fingerprint; `GraphWebFingerprint` MUST NOT include external state.

### Validation

- Unit tests MUST cover N:1 target mapping, canonicalization, identity reuse, evidence-derived classification, unknown suppression, local precedence, version provenance, and relation-kind preservation.
- Polyglot integration tests MUST cover npm named/default/namespace/type imports, subpath imports, workspace/path-alias exclusions, Node built-ins, JavaScript globals, calls including construction syntax, type refs, import-only modules, unknown receivers, and a second-language builtin.
- Lifecycle tests MUST cover replacement, watcher deletion, full purge, rebuild convergence, local definition arrival/removal, and orphan pruning.
- Negative tests MUST prove no external rows enter Explorer/default graph/workspace/list/search responses, anchors, chunks, FTS, embeddings, code-anchor targets, or definition results.
- Benchmarks MUST compare query-time projection with dedicated edge materialization across the polyglot fixture, Rhizome self-index, and a pinned, reproducible large noisy JavaScript/TypeScript corpus.

## Open Questions

The proposed defaults requiring Drew's approval are recorded in [[../../reference/analysis/external-code-reference-targets-2026-07-18|the measured analysis]] and the owning effort. Lockfile exact-version provenance and a distinct `constructs` relation are explicit follow-ups, not hidden first-slice decisions. No additional implementation-level question should be invented after plan approval without recording a deviation.
