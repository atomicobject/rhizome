---
type: ReferenceDoc
summary: "Repowise-derived design patterns Rhizome can reuse for risk, graph provenance, cross-repo signals, triage cards, answer retrieval, dead-code reporting, and contract extraction."
reference-kind: analysis
derived-from:
  - repowise/README.md
  - repowise/docs/architecture/ARCHITECTURE.md
  - repowise/docs/CODE_HEALTH.md
  - repowise/docs/MCP_TOOLS.md
  - repowise/packages/core/src/repowise/core
  - repowise/packages/server/src/repowise/server
last-verified: 2026-05-19
status: active
---

# Repowise techniques for Rhizome code intelligence

## Summary

Repowise is useful design input for Rhizome where it treats codebase intelligence as deterministic, queryable evidence: git-derived risk, code health biomarkers, confidence-scored graph edges, cross-repo overlays, task-shaped agent tools, answer-retrieval calibration, dead-code confidence tiers, and API contract extraction.

Do not copy Repowise's generated-wiki center of gravity into Rhizome. Rhizome's durable advantage is human-authored markdown, ontology-typed notes, NodeRef identity, query recipes, and provenance-preserving answer packets. The transferable parts are the deterministic code intelligence and agent/tool effectiveness tricks around that core.

## How to use this note

Use this note instead of reopening Repowise when planning these seven Rhizome capability areas:

1. risk and health reporting
2. cross-repo workspace signals
3. confidence-scored code graph edges
4. modification triage cards
5. answer retrieval blend and diagnostics
6. dead-code candidate reporting
7. API/package/topic contract extraction

When implementing in Rhizome, prefer existing Rhizome surfaces first:

- `rzm agent report` for deterministic reports and dashboards.
- `rzm agent file-context` for compact modification cards.
- `rzm agent semantic-query` and `rzm search` for answer/retrieval behavior.
- `pkg/search/graphdb`, `pkg/search/graphalg`, and existing code-edge tables for graph facts.
- `pkg/ontology/query` or runtime roots only after a fact has a stable read model.
- `.rhizome` local SQLite before introducing separate stores.

## Source map

Primary Repowise surfaces inspected:

- README and architecture docs: product model, layer framing, workspace behavior, MCP tools.
- `packages/core/src/repowise/core/analysis/health`: deterministic biomarkers, scoring, trends, coverage, duplication.
- `packages/core/src/repowise/core/analysis/dead_code`: confidence-tiered dead-code reporting.
- `packages/core/src/repowise/core/ingestion`: tree-sitter parsing, graph builder, call resolver, git indexer.
- `packages/core/src/repowise/core/workspace`: workspace config, cross-repo co-change, package dependency, API contract overlays.
- `packages/server/src/repowise/server/mcp_server`: task-shaped tools, answer pipeline, freshness metadata, risk/context/why/health/dead-code tools.
- Tests under `tests/unit` and `tests/integration`: good source for behavioral truth when docs drift.

Known Repowise drift to account for:

- README says "nine MCP tools" while the MCP docs list seven in one place.
- Workspace docs say `repo="all"` works for `get_context`; implementation/tests reject it for `get_context` and `get_risk`.
- Health CLI/model/MCP serialization do not expose exactly the same fields.
- Dead-code high-confidence tiers differ between core report and MCP grouping.

Treat Repowise docs as a map; trust implementation/tests for contracts.

## 1. Risk and health reporting

### Repowise pattern

Repowise's code health layer is deterministic: no LLM, no network, no external analyzer process. It reads source bytes, walks tree-sitter ASTs, creates a per-file `FileContext`, runs registered biomarkers, scores each file, persists findings/metrics/snapshots, and exposes the same information through CLI, MCP, REST, and UI.

Health is a separate intelligence layer that reads graph and git data but only writes health tables. That separation is worth copying: Rhizome reports should not mutate code graph, ontology, or embeddings state while computing risk.

### Data inputs

Useful inputs:

- AST function metrics: NLOC, cyclomatic complexity, nesting, cognitive complexity, parameter count, branch bumps.
- Git metadata: churn percentile, commit counts, line churn, significant commits, primary owner, recent owner, contributor count, bus factor, co-change partners.
- Graph metadata: dependents count, community/module label, PageRank or other centrality.
- Coverage reports: LCOV, Cobertura, Clover normalized to repo-relative file paths.
- Test-pair heuristic: common paired test names when coverage is unavailable.
- Duplication report: clone pairs plus co-change strength.

Rhizome fit:

- Use existing code index and graph read models for paths, anchors, call/test/doc edges.
- Add git-history ingestion as a separable source. Do not block basic health reporting on git data being available.
- Keep report rows vault-root relative and strict-normalized through `pkg/paths`.
- Store health facts in `.rhizome/db.sqlite` so `report`, web, and future ontology/runtime roots read the same rows.

### Biomarkers worth adapting

Repowise biomarkers are small named detectors with explicit thresholds and evidence. Candidate Rhizome v1 set:

- `brain_method`: long + complex + central. Repowise threshold: NLOC >= 70, CCN >= 9, dependents >= 8.
- `nested_complexity`: deep control-flow nesting.
- `bumpy_road`: multiple independent branch groups in one function.
- `complex_method`: high cyclomatic complexity.
- `large_method`: high NLOC without enough other signal for brain-method.
- `primitive_obsession`: many primitive params in one signature.
- `dry_violation`: token-level clone region, weighted by active co-change.
- `untested_hotspot`: churn/centrality plus missing test or poor coverage.
- `coverage_gap`: low coverage with meaningful uncovered surface.
- `developer_congestion`: too many active contributors in one file.
- `knowledge_loss`: primary owner no longer active or no recent knowledgeable owner.

Keep each detector independent. It should return:

- biomarker type
- severity
- file path
- optional symbol/function
- line range
- short reason
- structured details
- evidence inputs used

### Scoring formula

Repowise's scoring shape is simple and explainable:

- Every file starts at `10.0`.
- Severity deductions: low `0.3`, medium `0.7`, high `1.2`, critical `2.0`.
- Category caps prevent one smell family from dominating:
  - structural complexity: `3.5`
  - size and complexity: `2.0`
  - duplication: `1.5`
  - test coverage: `2.0`
  - organizational: `1.0`
- Final score clamps to `[1.0, 10.0]`.
- KPIs are NLOC-weighted:
  - average health
  - hotspot health
  - worst performer
  - module rollups

Rhizome should copy the category-cap idea even if thresholds differ. It prevents noisy duplicate clone detection or one missing coverage report from making the entire health surface look worse than the evidence supports.

### Duplication technique

Repowise uses native Rabin-Karp clone detection over tree-sitter tokens:

1. Tokenize each parsed file.
2. Strip comments/whitespace.
3. Normalize identifiers and literals so renamed clones still match.
4. Build rolling 64-bit hashes over fixed windows. Default: 50 tokens.
5. Bucket matching hashes.
6. Verify token-kind equality to avoid hash-collision false positives.
7. Merge adjacent clone windows into contiguous regions.
8. Weight clone severity by co-change count between the two files.

Rhizome adaptation:

- Keep duplication report separate from health scoring.
- Use co-change to distinguish dormant duplication from active maintenance burden.
- Include clone partner paths and line ranges in evidence.
- For incremental health, still run duplication over enough full-repo context to compare changed files with unchanged partners.

### Trend and refactoring outputs

Repowise persists rolling snapshots and computes trend pure in memory:

- current vs baseline decline threshold: roughly `0.5` score drop
- predicted decline: three consecutive drops
- snapshot fields: hotspot health, average health, worst performer, per-file scores

Refactoring suggestions are deterministic templates keyed by biomarker type. Repowise ranks refactoring targets by total health impact divided by coarse effort bucket.

Rhizome adaptation:

- Store trend snapshots separately from current findings.
- Keep suggestion text in one package so CLI/MCP/UI do not drift.
- Prefer "candidate to inspect" over "recommended automatic fix."
- Add `rzm agent report --op health` and later a web view; do not start with a new top-level command unless report ergonomics fail.

### Agent payload shape

Useful dashboard mode:

```json
{
  "kpis": {
    "averageHealth": 7.8,
    "hotspotHealth": 6.4,
    "worstPerformerPath": "pkg/foo/bar.go"
  },
  "worstFiles": [],
  "topFindings": [],
  "modules": []
}
```

Useful target mode:

```json
{
  "mode": "targets",
  "metrics": [],
  "findings": [],
  "coverage": {},
  "trend": {},
  "refactoring": []
}
```

For agent effectiveness, keep the first response small: health score, top 2-3 biomarkers, coverage/test gap, owner, dependents, co-change count, and a follow-up command for deeper findings.

### Cautions

- Do not swallow detector failures silently. Repowise catches biomarker exceptions for resilience, but Rhizome should include report diagnostics.
- Define one canonical serialization early. Repowise has field drift between model, CLI, MCP, and UI.
- Keep report-only runs distinct from persisted runs.
- Do not make health depend on generated docs.

## 2. Cross-repo workspace signals

### Repowise pattern

Repowise keeps each repo's index independent and puts cross-repo intelligence in a workspace overlay:

- `.repowise-workspace.yaml`: repo aliases, paths, primary/default repo, indexed timestamps.
- `.repowise-workspace/cross_repo_edges.json`: cross-repo co-changes, package deps, summaries.
- `.repowise-workspace/contracts.json`: API/gRPC/topic provider-consumer links.

This is a good first shape for Rhizome: overlay before schema-heavy integration. It keeps failure domains independent and lets cross-repo signals ship without destabilizing single-repo indexing.

### Workspace config behavior

Useful Repowise details:

- Scan parent workspace up to a fixed depth.
- Prune junk and hidden directories.
- Stop at `.git` repo boundaries.
- Support root repo plus nested repos.
- Assign stable aliases.
- Lazy-load per-repo DB/vector/search contexts with an LRU cap.
- Protect the default repo from eviction.
- Read child repo state back into workspace config to avoid stale indexed-commit metadata when a child repo updates directly.

Rhizome adaptation:

- Do not require a collection vault to adopt cross-repo signals immediately.
- Add a workspace config that maps repo aliases to roots and per-repo `.rhizome` state.
- Keep local indexes per repo; add a workspace overlay read model.
- Make `repo` parameter semantics explicit and tested before documenting them.
- If `repo="all"` is expensive or ambiguous, reject it with available aliases and narrower alternatives.

### Cross-repo co-change algorithm

Repowise algorithm:

1. Parse recent non-merge git commits per repo. Default: 500.
2. Group commits by author email.
3. For each author, pair commits from different repos within a time window. Default: 24 hours.
4. Cross product changed files, capped at 20 files per commit side to avoid explosion.
5. Use exponential temporal decay. Repowise tau: 180 days.
6. Accumulate `(sourceRepo, sourceFile, targetRepo, targetFile)` score.
7. Keep edges above min score. Repowise min: `1.0`; cap: 200 edges.
8. Store frequency and most recent co-change date.

Tests pin:

- same author within window matches
- different author does not match
- outside window does not match
- recent co-changes outrank old ones
- file-pair cross product is expected behavior

Rhizome adaptation:

- Store both raw frequency and decayed strength.
- Preserve the time-window and decay constants in config or report metadata.
- Mark cross-repo co-change as temporal evidence, not dependency evidence.
- Surface as "historically changed together" in `file-context` and `risk`, not as a hard graph dependency.

### Package dependency mapping

Repowise scans:

- `package.json`: `file:` dependencies and workspaces
- Poetry/PEP path dependencies
- Cargo path dependencies
- Go `replace`
- `.csproj` `ProjectReference`
- internal NuGet-style `PackageReference` matching sibling assembly/project names

Rhizome adaptation:

- Package deps are stronger than co-change but weaker than source-level imports.
- Use normalized repo alias and manifest path.
- Preserve dependency kind in edge provenance.
- Keep package-edge ingestion independent from API contract extraction.

### Workspace update strategy

Repowise runs repo updates in parallel with a cap, applies a per-repo single-flight lock, then runs cross-repo analysis only when some repo aliases changed.

Rhizome adaptation:

- Reuse existing index locks per repo.
- Make workspace overlays rebuildable from per-repo state plus git logs.
- If a child repo fails, keep other repo updates and mark overlay partial.

## 3. Confidence-scored code graph edges

### Repowise pattern

Repowise uses a two-tier graph:

- file nodes
- symbol nodes

Important edge types:

- `imports`
- `defines`
- `has_method`
- `calls`
- heritage/type-use edges
- dynamic/framework edges
- co-change edges excluded from dependency PageRank

The core lesson is not the exact graph shape; it is explicit edge provenance and confidence.

### Query-driven parsing

Repowise keeps language-specific behavior in tree-sitter `.scm` files plus language config, while the parser core stays mostly language-agnostic. Query compilation is cached.

Rhizome already uses language-specific indexing patterns; the useful lesson is to keep the language-specific contract inspectable:

- capture names
- symbol kind mapping
- import binding extraction
- call-site extraction
- type-reference extraction
- known framework/dynamic conventions

### Call-resolution confidence tiers

Repowise call resolver tiers:

- same-file exact match: confidence `0.95`
- import binding scoped match: `0.90`
- imported-file fallback: `0.85`
- module alias receiver call: roughly `0.88`
- global unique match: `0.50`
- cross-language global unique match: rejected

Rhizome adaptation:

- Store confidence plus provenance enum, not only a float.
- Suggested provenance names:
  - `same_file_exact`
  - `import_binding`
  - `imported_file_symbol`
  - `module_alias`
  - `global_unique`
  - `type_use`
  - `framework_convention`
  - `dynamic_marker`
  - `test_heuristic`
- Keep default navigation/search filters above a threshold. Repowise often filters call edges below about `0.7` for caller/callee payloads.
- Expose weaker edges in explain/diagnostic mode.

### Stronger duplicate edge wins

Repowise merges duplicate call/heritage edges by keeping stronger confidence. This is worth copying. A low-confidence global edge should not override a later import-scoped edge for the same source/target.

### Provenance as edge type

Repowise notes that arbitrary NetworkX attributes do not round-trip well through SQL, so some provenance becomes an edge type such as `type_use`. Rhizome should not be constrained by NetworkX, but the principle holds: provenance must live in stable persisted columns, not transient in-memory attrs.

Recommended Rhizome edge fields:

- source handle
- target handle
- edge kind
- provenance
- confidence
- language
- path
- line/range if available
- evidence payload or compact JSON
- stale/rebuild generation

### Type-use and static DI gaps

Repowise adds lower-confidence type-use edges from parsed type references. This helps static analysis see dependency-injection or framework-mediated relationships that do not show as direct calls.

Rhizome adaptation:

- Model type-use separately from call.
- Use it for reachability/risk/impact, not exact "called by" semantics.
- Show it in diagnostics as "static type reference" or "constructor/interface use."

### Import disambiguation

Repowise has a deterministic stem-priority function to avoid PageRank pollution from wrong test fixture imports:

- parent directory match first
- penalize low-value segments such as tests, fixtures, examples, docs, scripts
- shorter path depth wins
- lexical path tie-break

Rhizome adaptation:

- Any fallback import/path resolution should emit resolution rationale.
- Never let a weak path-stem match become indistinguishable from a language-resolved import.

### Graph metric scoping

Repowise keeps graph metrics scoped:

- file PageRank runs over file dependency subgraph
- symbol PageRank runs over symbol/call subgraph
- co-change is excluded from file PageRank
- large-repo betweenness samples instead of exact full graph
- PageRank non-convergence returns uniform scores rather than failing the whole report

Rhizome adaptation:

- Name the graph used by each metric.
- Avoid mixing note graph, code graph, co-change graph, and ontology graph into one unexplained centrality number.
- Persist metric provenance and graph generation.

## 4. Modification triage cards

### Repowise pattern

Repowise's `get_context` is intentionally a compact triage card, not a source-body tool. Raw source is split into `get_symbol`. This separation is directly useful for Rhizome `file-context`.

Default card behavior:

- target identity
- docs summary
- compact symbol list
- hotspot bit
- decision titles only
- freshness metadata
- optional ownership, last change, metrics, community, callers/callees, decisions, full docs

It avoids inlining everything because large MCP responses pollute agent context and cached prompt prefixes.

### Compact symbol list

Repowise compact symbol payload:

- name
- kind
- signature
- line
- symbol id

Cap: 40 symbols, with a truncation hint to widen.

Rhizome adaptation:

- `file-context` should provide a bounded "open these next" card.
- Use `code_symbol` or exact file reads for source.
- Include stable handles and line anchors so the agent can follow up without fuzzy search.

### Budget discipline

Repowise caps `get_context` around 8k tokens and trims in stages:

1. Strip heavy docs.
2. Shrink symbols by target-derived priority.
3. Drop whole targets if needed.
4. Return `truncated`, `droppedTargets`, and `droppedSymbols`.

Rhizome adaptation:

- Contextpack already owns deterministic budget behavior; expose omissions clearly in `file-context`.
- Preserve a first required item by trimming rather than omitting.
- Include exact follow-up commands when content is dropped.

### Raw source split

Repowise `get_symbol`:

- resolves one indexed symbol
- normalizes separators (`.`, `::`, `/`)
- guards path escape
- line-bounds output
- caps around 400 lines

Rhizome already has `code_symbol`. The note for future design: keep source reads proof-oriented and narrow; do not make the triage card do both orientation and proof.

### Freshness metadata

Repowise `_meta` freshness:

- reads live `.git/HEAD` by file I/O, not spawning git
- compares indexed commit to live HEAD
- emits `stale_warning` only on real mismatch
- age fallback warns only after 90 days when live git unavailable
- silence is a trusted current signal

Rhizome adaptation:

- Add freshness metadata to agent response envelopes where cheap.
- Avoid noisy "index is old" nags that train agents to ignore warnings.
- Prefer exact HEAD mismatch over elapsed time.

### Risk card integration

Repowise `get_risk` fuses:

- hotspot score
- trend/velocity
- risk type
- dependents count
- impact surface
- co-change partners
- owner/bus factor
- test gap
- security signals
- cross-repo impact
- health score and top biomarkers

PR mode adds a directive:

```json
{
  "directive": {
    "will_break": [],
    "missing_cochanges": [],
    "missing_tests": [],
    "overall_risk_score": 0,
    "summary": "..."
  }
}
```

Rhizome adaptation:

- Add a "before editing" mode to `file-context` or `report` before creating a new tool.
- Keep directive fields short and action-shaped.
- Do not include global hotspots in PR/diff-specific mode unless requested.

## 5. Answer retrieval blend and diagnostics

### Repowise pattern

Repowise `get_answer` is useful because its retrieval stage is calibrated and explainable:

1. FTS and vector retrieval in parallel.
2. Reciprocal rank fusion.
3. Hydrate hits with page metadata.
4. Apply term coverage / domain penalty / intersection boost.
5. Apply bounded PageRank bias.
6. Expand one hop through graph neighbors.
7. Gate synthesis on retrieval dominance.
8. Distinguish answer confidence from retrieval quality.

Rhizome already has a staged search planner/executor/answer packet architecture. Use Repowise as a checklist for calibration and diagnostics, not as a reason to collapse Rhizome's pure answer layer.

### Hybrid retrieval

Repowise constants:

- fetch 15 candidates per retriever
- RRF `k = 60`
- score scale `180.0` to keep old BM25-tuned gates meaningful
- FTS timeout: 5s
- vector timeout: 8s
- vector readiness wait: up to 30s

Why this works:

- FTS catches literal identifiers and exact terms.
- Vector catches conceptual matches.
- RRF rewards overlap without requiring score normalization between backends.

Rhizome adaptation:

- If current unified search already uses equivalent fusion, diagnostics should say so.
- Preserve retriever source list per candidate.
- Expose when one retrieval lane timed out or was unavailable.

### PageRank bias

Repowise PageRank bias:

- file PageRank loaded for candidate paths
- normalize within the candidate set
- multiply score by `1.0` to `1.3`
- use centrality as tie-break, not override

Rhizome adaptation:

- Keep graph bias bounded and explainable.
- Do not let PageRank bury exact-symbol or direct doc matches.
- Record `graphBias`, input score, output score, and graph generation in diagnostics.

### One-hop graph expansion

Repowise graph expansion:

- expand from top 2 hits
- both importers and importees
- only include neighbors with wiki/page content
- max 3 new candidates
- score = parent score * `0.7`
- mark `_expanded_from = "graph"`
- rank expansion candidates by PageRank

Rhizome adaptation:

- Equivalent expansion should happen after direct retrieval, before answer role selection.
- Mark expanded evidence as indirect.
- Use expansion to rescue near misses, not flood the answer packet.
- For Rhizome, eligible neighbors may include code anchors, tests, docs-for-code, and structural ontology refs.

### Dominance and fallback

Repowise synthesis gate:

- If top score is clearly dominant over second score, synthesize.
- Ratio threshold: roughly `1.2`.
- For high absolute score, absolute gap threshold around `0.5` can suffice.
- If ambiguous, skip synthesis and return `best_guesses` with one-line file justifications plus excerpts.

Additional gates:

- Downgrade hedged answers that say the source did not contain enough evidence.
- Downgrade if cited identifiers do not match hydrated symbols named in the question.
- Return fallback targets so agents verify low-confidence answers.

Rhizome adaptation:

- Rhizome answer packets are evidence-first, not generated prose. The same idea maps to confidence and `nextQueries`.
- Separate "retrieval quality" from "answer confidence":
  - retrieval quality: did retrieval find strong, specific, non-stale candidates?
  - answer confidence: did required roles and coverage make a usable packet?
- If retrieval is ambiguous, return candidate explanations and narrower follow-up queries instead of pretending the answer is weakly known.

### Diagnostics fields worth copying

Candidate-level:

- retriever sources: FTS, vector, graph expansion
- raw score and final score
- RRF contribution
- PageRank value and bias
- graph-expanded marker and parent
- search method (`embedding` vs `bm25`)
- confidence score
- direct vs indirect evidence

Run-level:

- indexed commit vs live HEAD
- stale warning
- retriever status and timeout/degradation
- result count per retriever
- fusion policy
- expansion count
- synthesis/answer gate reason
- fallback target list
- cache hit and schema version

Rhizome has [[search-diagnostics-explain-architecture]]. Use this note as a concrete checklist when implementing or reviewing that diagnostics shape.

### Cache behavior

Repowise answer cache:

- key: repo + normalized question hash
- payload schema version
- cache misses on older schema versions
- bypass or downgrade stale/hedged cached answers

Rhizome adaptation:

- Cache retrieval packets only if invalidation is clear.
- Include schema version and index generation in cache keys.
- Do not cache failures as if they were durable no-results answers.

## 6. Dead-code candidate reporting

### Repowise pattern

Repowise treats dead code as confidence-tiered evidence, not truth. This is exactly the right posture for Rhizome.

Finding model:

- kind
- file path
- optional symbol name/kind
- confidence
- reason
- last commit
- recent commit count
- line estimate
- package
- evidence list
- `safe_to_delete`
- owner
- age

Kinds:

- `unreachable_file`
- `unused_export`
- `unused_internal`
- `zombie_package`

### Detection passes

Default Repowise passes:

- unreachable files
- unused exports
- zombie packages

Unused internals exist but are off unless configured. That is a good default: private-symbol dead-code detection is noisier and language/framework dependent.

### Confidence scoring

Repowise unreachable-file examples:

- no recent commits and age >= 365 days: confidence `1.0`
- age >= 180 days: `0.9`
- age >= 90 days: `0.8`
- no 90-day commits: `0.7`
- recent new file: around `0.55`
- active file: around `0.4`
- dynamic import evidence in same package caps confidence to `0.4`

`safe_to_delete` generally requires confidence >= `0.7` and no dynamic naming/pattern concern.

Rhizome adaptation:

- Keep confidence and `safeToDelete` separate.
- Let dynamic evidence lower confidence; do not treat it as a positive liveness proof.
- Surface why confidence changed.

### Allowlists and false-positive controls

Repowise excludes or downgrades:

- entrypoint symbols: `main`, `Main`, `Program`, `Startup`, WSGI/ASGI factories, Windows entrypoints, test macros
- generated/framework/config/route/test paths
- fixtures
- non-code languages
- symbols that cannot be independently imported
- namespace/module anchors
- dynamic imports
- reflection/DI/event-bus/plugin patterns
- framework-mediated graph edges
- public symbols with explicit export markers
- namespace imports that make file members reachable by attribute
- nested definitions inside functions

Rhizome adaptation:

- Start conservative.
- Make allowlists data-driven and reportable.
- Add explain output before delete suggestions.
- Never auto-delete from this report.

### Output surfaces

Useful agent grouping:

```json
{
  "safeToDelete": [],
  "reviewFirst": [],
  "lowConfidence": [],
  "summary": {
    "findings": 0,
    "deletableLines": 0
  }
}
```

Filters worth adding eventually:

- min confidence
- kind
- owner
- directory/path
- safe-only
- include internals
- group by package/owner/kind

Rhizome adaptation:

- Add as `rzm agent report --op dead-code` first.
- Use existing code graph and git/contract/cross-repo evidence to lower confidence when external consumers exist.
- Link findings to tests and docs so cleanup agents know what to inspect before editing.

### Cautions

- Repowise line estimates for unreachable/zombie packages can be rough. Rhizome should compute actual line spans when it has file access.
- Keep confidence tiers consistent across CLI/MCP/UI.
- A REST/UI "analyze" endpoint must actually trigger analysis or clearly say it reads last persisted findings.

## 7. Contract extraction

### Repowise pattern

Repowise extracts API contracts into a separate workspace overlay. Contracts are provider/consumer facts with normalized IDs and confidence, then matched across repos.

This is high-value for Rhizome because it creates cross-repo blast-radius evidence that semantic search and git co-change cannot reliably infer.

### Contract data model

Repowise contract fields:

- repo alias
- contract ID
- contract type: `http`, `grpc`, `topic`
- role: `provider` or `consumer`
- file path
- symbol name
- confidence
- optional service boundary
- metadata

Matched link fields:

- contract ID
- contract type
- match type
- confidence
- provider repo/file/symbol/service
- consumer repo/file/symbol/service

Rhizome adaptation:

- Store extracted contracts as code graph facts first.
- Promote into ontology only when users need typed business/domain modeling over them.
- Preserve role, confidence, extraction method, and service boundary.

### Normalization and matching

Repowise normalization:

- HTTP:
  - method uppercased
  - path lowercased
  - trailing slash stripped
  - route params normalized across `:id`, `{id}`, `[id]`, `${expr}`
  - wildcard method supported: `http::*::/path`
- gRPC:
  - package/service lowercased
  - method case preserved
  - wildcard service method supported: `grpc::service/*`
- topics:
  - lowercased
- same-repo same-service links filtered out

Rhizome adaptation:

- Contract ID normalization must be documented and tested before used in risk.
- Wildcards should carry lower confidence or explicit match type.
- Same-service filtering needs service-boundary detection; otherwise same-repo monorepos create noise.

### Extractor coverage

Repowise HTTP extractor uses regex-first heuristics across:

- Express
- FastAPI
- Spring
- Laravel
- Go handlers
- ASP.NET
- `fetch`
- Axios
- Python `requests`
- `httpx`
- C# `HttpClient`

gRPC extractor combines:

- `.proto` service/rpc parsing
- Go/Java/Python/TypeScript/C# registration and client heuristics

Topic extractor recognizes:

- Kafka
- RabbitMQ
- NATS

NATS matching requires idiomatic variable names to reduce false positives.

Rhizome adaptation:

- Use regex extraction for v1; tree-sitter extraction can improve precision later.
- Each extractor must emit confidence and pattern name.
- Store "unmatched provider" and "unmatched consumer" as useful findings, not just links.

### Service boundaries

Repowise detects service roots from marker files plus source presence, then assigns files by longest-prefix match.

Rhizome adaptation:

- Needed for monorepos before contract links drive risk.
- Service boundary should be a configurable code area, not only inferred.
- Inferred boundary confidence should be visible.

### Agent payload integration

Repowise uses contract links in:

- `get_context`: `cross_repo.contracts`
- `get_risk`: `cross_repo_impact.contract_consumers` and `contract_providers`
- workspace dashboard

Rhizome adaptation:

- `file-context` should show "external consumers/providers" as compact counts plus top examples.
- `risk`/report mode should increase blast radius when contract consumers exist.
- `semantic-query` should use contract facts as provenance, not broad retrieval text.

## Shared agent/tool effectiveness tricks

### Small default, opt-in depth

Repowise's best agent surface idea: default responses are cards, not dossiers. The card tells the agent which deeper tool to call.

Rhizome rule:

- Default context should help decide next action.
- Proof reads should be explicit and narrow.
- Omitted content should include exact follow-up commands.

### Silence as a design feature

Repowise hook and `_meta` design tries not to nag:

- no stale warning unless HEAD mismatch or very old unreachable git
- hook is silent for focused Grep/Glob results
- zero-result search gets rescue suggestions
- flood results get PageRank triage
- git operations warn once per HEAD

Rhizome rule:

- Warnings should be rare and trusted.
- Do not surface every possible caveat.
- Prefer exact actionable warnings.

### Hook hardening

Repowise hook entrypoint:

- separate lightweight executable
- avoids importing full CLI
- catches broad exceptions
- exits 0 silently
- self-heals legacy hook config
- uses short timeout

Rhizome adaptation:

- If Rhizome adds editor hooks, keep them advisory and fail-open.
- Do not block agent tool use because context augmentation failed.

### Marker-managed generated files

Repowise editor-file generator:

- creates file when absent
- appends managed block when no markers exist
- replaces only managed block when markers exist
- writes atomically via temp file replace

Rhizome already has managed AGENTS/RHIZOME blocks. Keep using marker-managed, repo-local updates. Avoid writing global editor files unless explicitly configured by the user.

## What not to copy

- Do not make generated wiki pages the primary knowledge source. Rhizome should keep human-authored notes, specs, reference docs, and ontology nodes as source of truth.
- Do not split into separate SQL/vector/graph stores just because Repowise does. Rhizome's unified SQLite substrate is a feature.
- Do not add many overlapping MCP tools before checking whether `report`, `file-context`, `semantic-query`, or ontology runtime roots already fit.
- Do not hide implementation drift behind docs. Pin agent-surface behavior with tests and generated command surfaces.
- Do not treat graph-derived facts as authoritative without confidence/provenance.

## Suggested Rhizome implementation sequence

### Phase 1: Report-only risk and health

- Add git metadata ingestion or a lightweight git report source.
- Add deterministic health detector package.
- Add health tables or report rows in `.rhizome/db.sqlite`.
- Add `rzm agent report --op health`.
- Include health summary in `file-context` only after report rows exist.

### Phase 2: Edge confidence and provenance

- Add provenance/confidence to code edges.
- Define confidence tiers for current language indexers.
- Update code-reference/caller/callee surfaces to filter and explain weak edges.
- Add diagnostics for edge source and confidence.

### Phase 3: Compact modification triage

- Tighten `file-context` card shape.
- Add explicit omitted-content metadata.
- Add risk summary fields when health/git rows exist.
- Keep exact source reads in `code_symbol`/workspace read tools.

### Phase 4: Search/retrieval diagnostics calibration

- Compare current Rhizome search stages against Repowise checklist:
  - FTS + vector fusion
  - bounded graph bias
  - bounded neighbor expansion
  - dominance/coverage confidence
  - retrieval quality separate from packet confidence
- Fill missing diagnostics under [[search-diagnostics-explain-architecture]].

### Phase 5: Cross-repo overlay

- Add workspace config and overlay directory.
- Implement cross-repo co-change first.
- Add package dependency extraction second.
- Surface only compact counts/top examples in `file-context`; deeper report via `report`.

### Phase 6: Dead-code candidates

- Add conservative graph/git dead-code report.
- Start with unreachable files and unused exports.
- Add dynamic/framework allowlists before claiming "safe."
- Use confidence tiers and never auto-delete.

### Phase 7: Contract extraction

- Add HTTP provider/consumer regex extraction.
- Add contract normalization and matching.
- Add gRPC/topic later.
- Feed contract links into cross-repo impact and risk.
- Promote contract facts into ontology only when a durable typed workflow needs them.

## Review checklist for future Rhizome designs

When a proposal borrows from Repowise, check:

- Is this deterministic evidence, or generated prose?
- Does the payload preserve source/provenance/confidence?
- Is the default response a compact card?
- Is raw source behind a narrow follow-up?
- Are graph boosts bounded enough to avoid burying exact matches?
- Are retrieval quality and answer confidence separate?
- Is stale/index metadata precise enough to trust?
- Does cross-repo evidence stay separate from single-repo facts?
- Are docs backed by implementation tests?
- Can unrelated indexes fail without breaking the whole agent workflow?
