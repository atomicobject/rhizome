---
type: ReferenceDoc
summary: "Implementation contract for search seed normalization, target resolution, seed expansion, auto-expansion, and intent/weight selection."
reference-kind: analysis
derived-from:
  - docs/reference/domain/Search - Intent and weight tuning.md
  - docs/specs/technical/unified-search-answer-architecture.md
  - docs/specs/product/search-quality-evaluation-corpus.md
  - pkg/search/target_resolution.go
  - pkg/search/planner/planner.go
  - pkg/search/retrieval/auto_expand.go
  - pkg/app/unifiedsearch/run.go
  - pkg/app/mcp/semantic_query_unified.go
  - pkg/app/mcp/semantic_query_seeds.go
last-verified: 2026-04-29
status: active
---

# Search - Seed expansion strategies

## Summary

Search planning is a staged contract: callers normalize explicit seed tokens, target resolution may infer missing local seeds from query text, the planner expands bounded local structure, and broad text-only modes may auto-expand from high-quality base hits. Intent selection changes which retrievers are built and how evidence is weighted; it must not change the identity model for notes, code files, anchors, chunks, or ontology nodes.

Use this with [[Search - Intent and weight tuning]], [[unified-search-answer-architecture]], and [[search-quality-evaluation-corpus]] when changing search quality, `semantic-query`, or `pkg/search/planner`.

## Code anchors

- `target-resolution`: [pkg/search/target_resolution.go](../../../pkg/search/target_resolution.go)
- `planner`: [pkg/search/planner/planner.go](../../../pkg/search/planner/planner.go)
- `intent-profiles`: [pkg/search/planner/intent_profiles.go](../../../pkg/search/planner/intent_profiles.go)
- `auto-expand`: [pkg/search/retrieval/auto_expand.go](../../../pkg/search/retrieval/auto_expand.go)
- `cli-unified-search`: [pkg/app/unifiedsearch/run.go](../../../pkg/app/unifiedsearch/run.go)
- `mcp-semantic-query`: [pkg/app/mcp/semantic_query_unified.go](../../../pkg/app/mcp/semantic_query_unified.go)
- `mcp-seed-normalization`: [pkg/app/mcp/semantic_query_seeds.go](../../../pkg/app/mcp/semantic_query_seeds.go)

## Request normalization

CLI unified search and MCP semantic-query do not enter the planner with the same raw shape.

`pkg/app/unifiedsearch.Run` joins `Query`/`Queries`, merges `Seeds` and `Files`, resolves seed tokens with `ResolveSeedHandles`, normalizes explicit seed paths with `NormalizeExplicitSeedPaths`, and defaults intent to `related_to_seed` only when the request has seeds and no query text. If `IntentInput` is not a known intent, CLI unified search may run embedding-backed intent detection through `InferIntentFromText`; otherwise it falls back to ordinary `search`.

### MCP mode contract

`semanticQueryUnified` is stricter. MCP `mode` is treated as explicit when supplied; otherwise only the seed-only heuristic selects `related_to_seed`. It intentionally does not run freeform mode detection inside the server. It also expands single-token semantic queries with a singular/plural variant before planning, so lexical/vector base retrieval can see both forms without treating that expansion as an explicit seed.

MCP seed normalization prefers note seeds for directory tokens when markdown notes exist below the directory; otherwise it emits file handles. CLI unified search leaves directory file handles intact and lets the planner decide how to bound expansion.

## Target resolution

`ResolveQuerySpecTargets` is the repair stage for query-only local-target intents. It runs before planner expansion and is idempotent after the first pass.

Resolution order:

1. If explicit seeds already exist, preserve them, mark `TargetStatusExplicitPath` if needed, and skip inference.
2. If `ExplicitSeedPaths` already exist but seed handles are missing, mark `TargetStatusInferredPath`.
3. Try exact path/module matches from indexed note paths and code file paths, falling back to a shallow vault walk only when the store cannot answer.
4. Try indexed FQN tokens first, then symbol tokens.
5. If a symbol maps to one path, seed that path and add the matching anchor handle.
6. If multiple symbol matches share a dominant module path and the intent is broad, seed the module path and all matching anchors.
7. If a precision intent remains ambiguous or unresolved, return warnings and block broad fallback.
8. If `subsystem_overview` cannot resolve a credible local target, downgrade to `overview` and warn.

Precision intents are `go_to_def`, `find_usages`, `callers`, `callees`, `tests_for_code`, `implementers`, `overrides`, and `imports`. For these modes, ambiguity is safer than a broad semantic fallback because the user is asking for a target-specific operation.

## Explicit seed expansion

The planner expands explicit seeds only after query repair and target resolution.

Directory file seeds are bounded first. The default per-directory cap is 12 files, clamped down by `spec.Limits.Total` when present. Indexed expansion prefers `TopCodePathsByPageRankPrefix`, then indexed files by prefix, then a filesystem walk capped at depth 3 and filtered through vault ignore rules plus the default code extension set. Expanded file handles carry the `dirseed` fragment for debugging. When code intel is present, the planner also derives a small set of top module anchor handles from the expanded files.

File seeds then expand through indexed coderef/doclink evidence: `expandNoteSeedsFromDocLinks` turns file and directory seeds into linked note handles, capped at 50 total. After that, `expandAnchorSeedsFromFiles` turns file seeds into anchor handles, capped at 120 total. Both expansions dedupe by handle string.

This ordering matters: directory expansion narrows a potentially huge path to representative files; doclink expansion attaches documentation surfaces to those concrete files; anchor expansion attaches code symbols for ref and graph retrievers.

## Auto-expansion

Auto-expansion is only for broad text-driven modes with no explicit seeds: `search`, `overview`, `code_for_docs`, and `docs_for_code`. It runs base retrievers first, enriches base candidates with query-frame specificity, derives a small set of implicit seeds, then runs graph/ref/ontology/diffusion expansion in parallel.

Implicit seed derivation is intentionally conservative:

- `MaxSeedsTotal` defaults to 8, rises to 10 for `overview`, and 12 for `code_for_docs`.
- `MaxSeedsPerKind` defaults to 4, rises to 5 for `overview`, and 6 for `code_for_docs`.
- Seeds are scored through the same approximate evidence-channel weights as the final ranker.
- Candidates below `MinSeedSpecificity=0.25` are rejected unless they have strong direct lexical/title/doc evidence.
- Expansion errors degrade to warnings for broad intents; non-broad expansion errors remain failures.

Auto-expansion can include:

- outgoing wikilinks from note seeds
- indexed doclinks, code-anchor notes, code-anchor refs, and anchor graph refs
- ontology structural and ambient context when ontology and note metadata snapshots are ready
- bounded graph PPR diffusion when refs are ready and the vault is not too sparse or immature

Auto-expansion must remain a bridge, not a silent global retry. A weak base hit should not become an implicit seed that drags the packet away from the query.

## Intent and weights

Intent applies in three layers:

1. `ValidateIntent` and caller heuristics choose the requested mode.
2. `intentProfiles` can disable retriever classes or tune diversity, especially for precision modes.
3. `weightsForIntent` and `adjustWeightsForSignals` choose evidence-channel weights based on intent, query shape, implicit expansion, and current corpus shape.

Important profiles:

- `code_for_docs` disables note graph expansion and lowers `MaxPerOwner` to 2.
- `overview` lowers `MaxPerOwner` to 1.
- `subsystem_overview` lowers `MaxPerOwner` to 2 and adds explicit seed, local docs, tests, locality ranking, and subsystem shaping when local seeds exist.
- precision modes disable vector and graph in the profile so exact code-intel retrievers dominate.

Important weight rules:

- seed-only requests receive minimum semantic, graph, and refs weights so local expansion can contribute even without text.
- `docs_for_code` and `code_for_docs` strongly favor refs and seed locality.
- `overview` and `subsystem_overview` preserve semantic recall but add graph/ref/ontology/context-locality bias.
- broad text-only modes keep graph/ref weights small until auto-expansion supplies credible implicit seeds.
- signal inspection zeros channels that are unavailable and downweights graph/ref/ontology signals for sparse, immature, or code-first-docs vaults.

## Retriever selection contract

The planner builds retrievers; `search.Service` executes them. Keep these boundaries:

- Base retrievers are vector, note lexical, and intel lexical when text is present and the corresponding indexes are ready.
- Seed vector retrieval only appears when seeds exist and the semantic/intel store is ready.
- Explicit graph/ref retrievers require seeds.
- Ontology retrieval only runs for note seeds or auto-expansion when ontology and note metadata snapshots share the same notes hash.
- Fetch-like precision retrievers (`DefinitionRetriever`, `TestsForCodeRetriever`, `CallEdgesRetriever`) are added by intent after target resolution.
- Packing is added only when `spec.Budget.Chars > 0`; packing must not broaden retrieval.

## Maintenance stories and acceptance checks

### Maintainer changes target resolution

Acceptance checks:

- ambiguous precision targets return `target_ambiguous` plus `precision_fallback_blocked`
- query-only `subsystem_overview` resolves a credible local module or downgrades to `overview`
- exact indexed path matches are preferred over filesystem walking
- repeated resolution on an already-resolved `QuerySpec` does not call the store again

Useful tests: [pkg/search/target_resolution_test.go](../../../pkg/search/target_resolution_test.go) and [pkg/search/latency_benchmark_test.go](../../../pkg/search/latency_benchmark_test.go).

### Maintainer changes expansion or weights

Acceptance checks:

- auto-expansion remains off for explicit `subsystem_overview` seeds
- `overview`, `code_for_docs`, and `docs_for_code` still wire auto-expansion for text-only no-seed broad searches
- ontology and diffusion expansion share one `noderead.Scope`
- precision profile changes do not accidentally re-enable vector/graph fallback

Useful tests: [pkg/search/planner/auto_expand_intents_test.go](../../../pkg/search/planner/auto_expand_intents_test.go), [pkg/search/planner/intent_profiles_test.go](../../../pkg/search/planner/intent_profiles_test.go), and [pkg/search/retrieval/auto_expand_test.go](../../../pkg/search/retrieval/auto_expand_test.go).

### Maintainer changes CLI or MCP search entrypoints

Acceptance checks:

- CLI and MCP both surface target status, resolution confidence, target candidates, and warnings
- MCP keeps mode explicit or seed-only heuristic; it does not silently accept embedding-detected mode inside the server
- CLI intent detection remains guarded by known intent validation and the provider/store path
- `search-quality-evaluation-corpus` fixtures still distinguish broad search, docs-for-code, code-for-docs, and precision-target behavior

Useful tests: [pkg/app/unifiedsearch/run_test.go](../../../pkg/app/unifiedsearch/run_test.go), [pkg/app/mcp/semantic_query_unified_test.go](../../../pkg/app/mcp/semantic_query_unified_test.go), and [pkg/app/mcp/semantic_test.go](../../../pkg/app/mcp/semantic_test.go).

## Change rules

- Prefer adding evidence channels or profiles over introducing a second ranking vocabulary.
- Do not broaden precision fallbacks to global semantic search when target resolution fails.
- Keep expansion caps visible in code and tests when changing them.
- If a new retriever needs query-time expansion, decide whether it belongs in explicit seed expansion or auto-expansion before wiring it.
- When adding a new intent, update `Search - Intent and weight tuning`, planner profiles/weights, MCP allowed modes, and search-quality corpus expectations together.
