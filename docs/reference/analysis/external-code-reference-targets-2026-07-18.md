---
type: ReferenceDoc
summary: "Measured current-state audit and cost model for representing high-confidence external code references as graph-visible, explicitly non-source-backed targets."
reference-kind: analysis
last-verified: 2026-07-19
status: current
---

# External code reference targets: baseline and cost model

## Recommendation

The implemented foundation makes high-confidence external references representable without turning them into code anchors or copying one graph node per syntax occurrence. It reuses `intel_symbol_ref_targets` as the normalized raw-target dictionary, adds one classified external identity per canonical target, maps raw targets N:1 to that catalog, retains sparse use-specific evidence beside qualifying durable associations, and derives local/external/runtime/unknown classification from a consistent read snapshot.

The foundation review must now decide the edge strategy. Query-time projection remains far below the absolute latency budget but triggers the frozen relative gate at low/typical fan-in. No production materialized edge table has been added; the benchmark-only table exists solely to make the comparison reproducible.

The first implementation slice should classify direct named/default/type/namespace imports, Node built-ins, and an explicit JavaScript runtime-global catalog. Unknown receiver members and unbound bare identifiers stay unknown. Package versions are observed provenance on a stable ecosystem/module/symbol identity, not part of the default node identity.

## Audit scope and evidence

Original measured baseline: `7002cf0589dfdf3c0dd3ec704d0024a2256e7c4c` on 2026-07-18. Current-main compatibility and cardinalities were re-audited on 2026-07-19 at `47a2bb29ce827a90b15803f9d5a2458bd7899c61`, which includes TypeScript/JavaScript relationship hardening from PR #174. The pinned exact-archive checksum is `6d3dd7b705eb41dfff59f847ffb5eaae37761a888e145cc2cc9f147e73951898`. Code embeddings were disabled only in temporary corpus copies so provider latency did not contaminate code-index timings.

The current model already separates unique targets from durable references:

- `intel_symbol_ref_targets` is unique on language/package/name/FQN.
- `intel_symbol_refs` points to a shared target and is unique by source file, owner FQN, reference kind, and target.
- Repeated AST occurrences within the same owner/kind/target are deduplicated. Counts below are durable semantic associations, not raw call-site counts.
- `calls`, `type_ref`, and `member_ref` use the symbol-reference tables. Imports use a separate import-reference family.
- Local graph-edge rebuild drops any reference target that cannot be joined to a source-backed anchor.

Gaps on the audited current-main baseline after PR #174:

- Raw targets have no ecosystem, canonical external module/symbol, external/runtime/unknown class, confidence, manifest or lockfile provenance, or observed version.
- Confidence is use-specific across the language-neutral model: strong import/builtin evidence and weak fallback spellings may share a raw target, so target-level confidence remains unsafe even though #174 removed the main TypeScript unknown-instance-receiver fallback.
- Constructions are currently folded into calls in TypeScript and several other indexers. The first slice deliberately preserves that relation vocabulary; a distinct `constructs` relation is follow-up work.
- TypeScript external imports are not durable unless an imported symbol is used; Python/C#/PHP preserve more import information, while Go does not emit the same general import-ref shape.
- Code stats mix external libraries, built-ins/globals, speculative receiver members, and genuinely unknown refs into `unresolved`.
- Incremental replacement and purge did not prune orphan target rows, and watcher deletion could leave reverse-index rows stale. Phase 0 closes this gap.
- Graph, PageRank, search-definition, file-context, and go-to-definition paths assume source-backed anchors and paths.

## Measured baseline

### Structural corpus

| Measure | Current main |
|---|---:|
| Persisted code files | 1,641 |
| Indexed symbols | 24,653 |
| Durable symbol-reference associations | 87,232 |
| Unique raw targets | 13,566 |
| Import rows | 506 |
| Module definitions | 1,608 |
| Materialized local intel edges | 92,633 |

| Language / kind | Durable refs | Local exact | Currently unresolved |
|---|---:|---:|---:|
| Go calls | 80,920 | 32,019 | 48,901 |
| Python calls | 1,442 | 411 | 1,031 |
| Python types | 159 | 36 | 123 |
| TypeScript calls | 2,904 | 1,137 | 1,767 |
| TypeScript members | 514 | 5 | 509 |
| TypeScript types | 1,293 | 963 | 330 |

The persisted TypeScript rows contain at least 158 conservative declared-package/Node-builtin candidates supporting 1,527 durable references: 1,066 calls, 419 member refs, and 42 type refs. This inexpensive dependency-list heuristic excludes project aliases and undeclared/transitive package-looking paths; it is evidence of value, not the production classifier.

PR #174 removed substantial speculative TypeScript noise while improving local resolution: total TS calls fell by 1,194 while local calls rose by 76, and TS member refs fell from 4,867 to 514. Unknown instance receivers are no longer the primary TypeScript false-positive source. Bare runtime globals/current-package fallbacks and non-TypeScript unknown receivers still require the conservative classification contract.

### TypeScript value sample

A 2026-07-18 pre-#174 read-only TypeScript Compiler API pass over all 146 then-current `web/` files found:

- 216 external import declarations across 37 modules;
- 104 unique imported identities;
- 3,791 high-confidence external call/construction/JSX/type occurrences;
- 845 explicit builtin/global occurrences;
- 166 unique external targets; and
- 3,278 unknown-receiver calls that must not become nodes.

On the 89-file production-like subset, excluding tests/specs/generated declarations, 561 high-confidence external uses collapse to at most 89 targets, while 1,500 unknown-receiver calls remain deliberately unclassified. This ratio justifies unique target nodes and rejects speculative per-use node creation.

The committed TypeScript integration fixture now includes representative external ESM and CJS inputs plus existing local workspace/export/path-alias coverage. Phase 1 uses those inputs to pin schema/store isolation; extraction, runtime globals, dependency-upgrade-impact behavior, and production fixture cardinality assertions remain Phase 2 or later.

### Storage and timing

| Existing reference storage | Bytes |
|---|---:|
| `intel_symbol_refs` plus two indexes | 32,542,720 |
| `intel_symbol_ref_targets` plus indexes | 3,092,480 |
| `intel_symbol_ref_files` | 73,728 |
| Total reference subsystem | 35,708,928 |

The ref-row/index family costs about 373 bytes per durable ref; the target family costs about 228 bytes per unique target; the blended subsystem is about 409 bytes per durable ref. The earlier 40.05s measurement is discarded as non-comparable because its temporary database retained materially different prior state.

The official alternating five-run exact-archive comparison measured main wall p50/p95 at 6.59/6.71s and the foundation at 6.60/7.44s: +0.15% p50 and +10.88% p95. Internal totals were 6.6/6.7s versus 6.6/7.4s; `index_code` was 4.0/4.0s versus 3.9/4.0s; reference persistence was 537/572ms versus 527/536ms; warm stats was 0.10/0.10s versus 0.09/0.10s. Indexing, persistence, and stats gates pass, but the conservative wall-p95 gate fails by 0.88 percentage points because one foundation call-edge sample took 1.3s. A separate pilot did not reproduce the slowdown; the official failure is retained for the foundation decision.

Phase 0's set-based garbage collection removes 100,000 orphan raw targets in 27-139ms, well inside the 1s budget, while shared-target and no-op cases remain safe.

### Implemented foundation cost

After redundant-index cleanup, the external catalog/map/evidence projection schema adds 319,488 bytes, 0.895% of the 35,708,928-byte baseline. The benchmark-only materialized edge variant adds a further 229,376 bytes, 0.642%; combined they would add 1.537%. Both fit the storage budget, but the materialized bytes are not part of the production schema. These are measurements from pinned synthetic profiles matching the audited corpus cardinalities, not byte-for-byte replays of three production databases; storage uses `dbstat` when available and otherwise `VACUUM`-stabilized page-count deltas.

Projection includes calls, member refs, type refs, and imports. At low/typical fan-in it is 2.1-2.5x slower than the test-only materialized table (17-23us versus 7-11us); at fan-in 1,000 it is 1.32x slower (591us versus 449us). All measurements are far below the 50ms endpoint budget. The frozen AC6 relative gate nevertheless triggers, so the foundation does not silently choose either strategy.

The v59 schema now contains `intel_external_targets`, `intel_external_target_map`, sparse `intel_external_symbol_evidence`, and `intel_external_import_evidence`. Narrow natural-key batch writes deduplicate targets and resolve IDs inside one writer transaction. Canonical identity, version-as-provenance, workspace/path-alias exclusion, and local-first classification are encoded in model/store tests. `IndexerVersion` is v1.10.0 and `ReverseIndexVersion` is v1.2.0 so stale databases rebuild. The exact origin corpus has zero external rows by design because extraction remains Phase 2.

## Durable identity and classification model

### Logical identity

The recommended public identity inputs are:

`ecosystem + canonical module + canonical symbol path + target kind`

Examples:

- `npm | react | useState | symbol`
- `node | node:path | join | builtin`
- `js-runtime | globalThis | fetch | global`
- `pypi | builtins | len | builtin`

The public `external_id` should be a deterministic handle derived from these values. Language remains extraction provenance and may help ecosystem selection, but should not force two nodes for the same ecosystem identity. Package version does not participate in the default ID; the nearest applicable `package.json` declared range, manifest path, and scope are optional metadata so upgrades preserve graph continuity. Exact lockfile versions and a future version-qualified view are follow-up work.

### Classification

Every durable reference association resolves into exactly one metric class:

1. `local_resolved`: joined to a source-backed local anchor.
2. `external_classified`: bound to a package/module identity with high-confidence evidence.
3. `runtime_global_builtin`: matched by an explicit runtime catalog or builtin extractor rule.
4. `unknown`: insufficient evidence, including unknown receiver members and unbound bare names.

Immutable binding/runtime evidence and confidence belong on the association because the same spelling can arise from stronger or weaker evidence. The four-way class itself is derived at query/statistics time: a current source-backed definition join wins, then qualifying external/runtime evidence, then unknown. This avoids cross-file reclassification writes when local definitions arrive or disappear. The unique external catalog stores canonical identity, not a blanket confidence assertion.

### Canonicalization and persistence seam

- `intel_external_targets` stores one deterministic ecosystem/module/symbol/kind identity.
- `intel_external_target_map(raw_target_id -> external_id)` maps each raw target to at most one canonical identity while allowing multiple raw spellings to converge.
- Sparse symbol-reference evidence rows attach only to qualifying durable associations; unknown/local rows do not pay the evidence-row cost.
- A new import-evidence family retains external module specifiers, binding kind, imported symbol, local alias, type-only state, confidence, and optional manifest-range provenance. Existing `intel_import_refs` remains the local resolved-module edge table.
- Npm subpaths remain part of the canonical public module specifier; default imports use symbol `default`; namespace and named access converge; `path` and `node:path` converge to `node:path`.
- Repository workspace packages and `tsconfig` path aliases resolve locally when possible and otherwise remain unknown. Bare-specifier shape alone never proves npm identity.

Qualifying first-slice evidence:

- direct named/default/type/namespace import binding;
- explicitly qualified Node built-in import;
- explicit Python builtin mapping already emitted by the indexer;
- well-known JavaScript global/builtin from a versioned runtime catalog.

Non-qualifying evidence:

- arbitrary `obj.member` or `obj.method()` without receiver identity;
- a bare name inferred only from spelling;
- a local-resolution miss by itself;
- package directory or `node_modules` scanning.

## Graph and query contract

- External targets are pathless, unindexed endpoints available only to explicit dependency-impact/reference operations.
- Calls (including construction syntax), type refs, member refs, and imports retain the durable relation kinds available in the first slice. A distinct construction relation is deferred.
- Existing local source resolution wins. A source-backed definition is never also presented as an external definition.
- External endpoints do not create `intel_code_anchors`, source chunks, FTS documents, embeddings, code-anchor targets, PageRank definition candidates, snippets, or go-to-definition success.
- Graph/query payloads expose ecosystem, canonical module/symbol, target kind, observed version provenance when available, classification provenance, and a `sourceAvailable: false`/equivalent affordance.
- Explorer, default graph browsing, workspaces, general search, and ordinary node/result lists exclude external targets at the server/query boundary; clients do not filter them after retrieval.
- A dedicated external-reference capability returns targeted reverse relationships. Existing graph flags and default graph reads never acquire external nodes.
- General lexical/semantic search does not return external nodes by default. Exact structured reference/graph surfaces may accept or return their handles.
- Default graph exclusion is by construction: ambient graph/search/workspace readers never read external catalog/evidence tables. The dedicated first-slice operation is uncached, so `GraphWebFingerprint` does not widen.

## Lifecycle

- Per-path replacement deletes old reference associations and relation-side evidence, removes raw-target mappings without qualifying evidence, then prunes external catalog and raw target rows with no remaining mappings/refs/import evidence.
- Watcher file deletion clears symbol refs, ref-file rows, import refs, and module defs before pruning.
- Full purge and rebuild converge to exactly the currently referenced/classifiable target set.
- A new local definition automatically wins the read-time join and suppresses the external projection; definition removal restores it only when durable qualifying evidence remains.
- Garbage collection is set-based and runs behind the durable batch boundary, never as a per-file store lookup on the ingest hot path.

## Cost budgets and benchmark gate

### Storage

- Exactly one external catalog node per canonical classified identity; no second per-occurrence or per-durable-ref graph-edge table in the initial design.
- External catalog/mapping/evidence storage including indexes must add at most 10% of the measured 35,708,928-byte reference subsystem (3,570,893 bytes); 5% (1,785,446 bytes) is the stretch target.
- Unique-target metadata should remain at or below 128 incremental amortized bytes per classified target where feasible.
- Report raw occurrences, durable associations, unique raw targets, and unique external nodes separately.

### Indexing

- Full structural self-index p50 regression at most 5%; p95 at most 10%, measured against five clean exact-commit runs captured in Phase 0.
- Reference persistence/classification phase regression at most 15%.
- No per-file database lookup or synchronous semantic work on the hot ingest path.
- Prune 100,000 orphan targets within 1s; ordinary no-op purge overhead at most 5%.

### Queries

- Endpoint-scoped external neighborhood, fan-in 1,000: p95 at most 50ms warm / 100ms cold.
- Bounded 1,000-edge graph including externals: p95 at most 150ms warm / 300ms cold.
- Warm `rzm code stats` on the measured corpus: at most 250ms.
- Existing graph reads without external nodes requested: at most 10% latency regression.

### Materialization decision

Benchmark query-time projection against a variant with a dedicated materialized external-edge table on:

1. the polyglot integration fixture;
2. the Rhizome self-index; and
3. a pinned, checksum-recorded larger noisy JavaScript/TypeScript corpus.

The Phase 1 harness uses pinned synthetic profiles/cardinalities for the polyglot fixture, Rhizome current index, and noisy JavaScript/TypeScript corpus. It uses five measured runs after warmup and captures storage through `dbstat` or a `VACUUM` plus page-count fallback, rebuild phase timings, fan-in 1/10/1,000 endpoint reads, representative graph sizes, stats aggregation, and GC at 0/10/50% orphan rates. The relative threshold triggers at low/typical fan-in even though every absolute result passes, leaving the production choice for foundation review. Even if materialization is approved, it may add only a deduped external edge per durable association, never a node or edge per raw occurrence.

## Compatibility and scope

The work is based on current `main`. PR #174 is now merged and raised `IndexerVersion` to `v1.9.0`; its richer TypeScript module/export resolution improves the inputs but does not persist external import-binding evidence. The durable seam remains `FileSummary` -> durable symbol/import refs -> shared raw target -> post-batch evidence/mapping persistence -> read-time classification/projection. The foundation sequences its rebuild markers after that baseline at `IndexerVersion` v1.10.0 and `ReverseIndexVersion` v1.2.0.

The model is language-neutral. Npm/Node/JavaScript is the first proving slice; Python builtins provide a second classifier example. Go, C#, PHP, and future Java/Swift/Kotlin support reuse the identity/provenance contracts but require language-specific extractor confidence rules.

## Foundation decisions and delivered outcome

The identity, catalog/map/evidence families, version-as-provenance policy, conservative classification boundary, construction-as-call scope, and ambient-surface exclusion were approved and delivered. Drew resolved both measured foundation decisions on 2026-07-19:

1. Query-time projection remains the production design. Final production measurement returned 1,000-reference fan-in in 0.70-0.72ms, still far below the 50ms endpoint budget, so the test-only materialized comparison does not justify a second edge lifecycle.
2. The +10.88% conservative rebuild-p95 result is an accepted non-blocking deviation from the +10% target. The final pinned quality gate passed; further optimization waits for production evidence.

The completed implementation also proved ESM and statically enumerable CJS bindings, canonical default-import public identity with raw binding evidence retained, Node/JavaScript runtime and Python builtin classification including shadowing exclusions, one-snapshot derived classification, full/incremental convergence, split statistics, and ambient-consumer isolation. The final built self-index covered 1,851 files, and live `react` default, `React.useState`, Python `len`, and code-stat queries passed. No React-specific or generic dependency-upgrade-impact skill was created.
