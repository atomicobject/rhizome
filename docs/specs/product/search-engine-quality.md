---
type: ProductSpec
id: SPEC-0102
aliases:
  - SPEC-0102
summary: "Defines measurable search relevance, exact-target correctness, evidence integrity, caller profiles, stable pagination, honest confidence, and performance for Rhizome's shared search engine."
spec-status: active
last-updated: 2026-09-12
---

# Search engine quality

## Summary

Rhizome must help people find the right source and help agents obtain sufficient, relevant evidence for a task. Its retrieval and indexing foundation remains the basis of one engine. Relevance decisions must survive grouping, filtering, pagination, and answer assembly. Caller profiles may choose different budgets and evidence composition, while preserving common correctness rules.

The [September 12 evaluation](../../reference/analysis/search-engine-evaluation-2026-09-12.md) found score clipping that reversed relevance order, precision tasks crowded out by documentation, unsupported high confidence, repeated-evidence score inflation, lost fallback configuration, and inconsistent agent pagination. This specification covers those defects and the quality system needed to establish broader excellence. Passing the original examples alone is insufficient.

This is the active delivery contract. The user explicitly approved the associated effort plan in chat on 2026-09-12; no `Person` identity was inferred because the configured current user is unavailable.

## Goals

- Find a uniquely identified note, path, or symbol first and return genuinely related tests or code relationships for targeted modes.
- Return useful ranked sources for lexical, conceptual, paraphrased, and multi-facet questions across code and prose corpora.
- Make ranking deterministic for identical effective requests and an unchanged index; reward independent evidence without counting a repeated fact twice.
- Preserve source identity, scope, confidence qualifications, and pagination integrity through every caller.
- Establish judged relevance and latency gates that can detect regressions before release.

## Non-Goals

- Replacing SQLite, exact vector storage, note-provider ownership, or the embedding-lane runtime without measured evidence that the existing design prevents these goals.
- Adding an LLM answer generator, a required remote reranking service, a new search backend, or a general experiment platform.
- Redesigning the retained search workspace, adding saved searches, or implementing a new language indexer.
- Treating semantic search as exhaustive structural proof; exact code and ontology tools remain available for that purpose.
- Publishing private corpora, changing closed efforts, or automatically merging or releasing the work.

## User Stories

### US1 - Find an identified source immediately

- id:: ^SPEC-0102-US1
- summary:: A person or agent searching for a known note, file, or symbol receives the exact eligible source before broader matches.
- status:: ready

#### Acceptance Criteria

- A unique eligible exact title, vault-relative path, or resolved symbol ranks first; case and punctuation rules are explicit and tested against case-sensitive paths and identifiers.
- Ambiguous titles or symbols retain all relevant disambiguation candidates and a qualified status; the engine does not silently choose an unrelated source.
- Lexical processing preserves identifier, path, acronym, quoted-phrase, and Unicode information. A bounded typo fallback helps ordinary prose/title discovery and is disclosed; exact modes retain literal target semantics.
- Exact-match priority survives score aggregation, source grouping, caller profile selection, and answer assembly.
- Navigational held-out accuracy meets the quality targets below, with `Search (Hub)` retained as a hard regression.

### US2 - Get the requested structural evidence

- id:: ^SPEC-0102-US2
- summary:: An agent using a precision mode receives the resolved definition, callers, callees, usages, or relevant tests without broad documentation displacing the requested evidence.
- status:: ready

#### Acceptance Criteria

- A unique `go_to_def` result agrees with the canonical code-symbol resolver and includes the actual definition in the first result.
- Callers, callees, and usages contain the requested relationship evidence with its coverage qualifications; a definition is not counted as a caller.
- `tests_for_code` prioritizes relevant test sources without requiring an additional tests-scope argument. Query-only, file-seeded, and explicitly resolved requests are covered.
- Supporting documentation remains available only as explicitly separate supporting evidence and cannot fill required structural result slots.
- Missing, ambiguous, unsupported, truncated, or unavailable structural evidence is explicit; no broad fallback is represented as a successful exact answer.

### US3 - Discover relevant evidence and understand its limits

- id:: ^SPEC-0102-US3
- summary:: A person or agent asking a conceptual question receives relevant sources, useful coverage, and confidence justified by the selected evidence.
- status:: ready

#### Acceptance Criteria

- Broad search retrieves complementary authored documentation and implementation when the query requires both, and preserves strong paraphrase-only evidence when literal overlap is absent.
- Historical effort records, popular hubs, common symbol names, and generic documentation do not qualify solely because they fill a role or have graph authority.
- Confidence distinguishes target resolution, source relevance, required role coverage, and retrieval availability. High confidence requires supported relevance and required coverage, not just populated roles.
- Known irrelevant/gibberish and unrelated-domain queries never receive high confidence. Weak nearest-neighbor suggestions are explicitly weak, or omitted from the primary answer; “no strong evidence” remains distinct from unavailable retrieval.
- Multi-query facets preserve which independent facet each source supports; repeating an identical facet does not boost its score.
- Conceptual relevance, coverage, and confidence satisfy the held-out quality targets below.

### US4 - Receive one consistent search contract across callers

- id:: ^SPEC-0102-US4
- summary:: Web, human CLI, and agent callers share one engine while using appropriate budgets and source composition for their task.
- status:: ready

#### Acceptance Criteria

- The same effective query, intent, profile, filters, index state, and candidate limit produces the same canonical ranked source sequence across adapters, before response-budget truncation.
- The interactive profile emphasizes navigation and relevant source snippets; the agent profile supports broader task evidence and compact source-owned bodies. Precision intent overrides both profiles' broad-discovery policies.
- Adapters translate requests and serialize selected results; they do not independently retrieve, re-score, manufacture score separation, or force source classes into the ranking.
- Profile identity and effective policy are inspectable in diagnostics and are bound into continuation identity. Public per-weight tuning controls are not required.
- Existing source navigation, retained tabs, and explicit filter controls from [[search-workspace]] continue to work with real engine responses.

### US5 - Trust filters and continuation

- id:: ^SPEC-0102-US5
- summary:: A caller can narrow and paginate search without missing eligible evidence through post-filtering or receiving duplicate pages from a changing candidate window.
- status:: ready

#### Acceptance Criteria

- Scope, test eligibility, concrete note type, exact-symbol requirements, and literal folder boundaries restrict candidates before bounded retrieval, ranking, grouping, counts, and response selection.
- Equal-score KNN boundaries preserve deterministic eligibility and scalar/vector parity. `%`, `_`, sibling-prefix paths, mixed case, and embedded nodes have regression coverage.
- Every continuation binds normalized query/facets, intent/profile, effective controls, vault/index identity, fixed candidate window, and ordered source identity.
- An unchanged search paginates without duplicate source identities or skipped entries in its fixed window. A changed result window yields explicit stale continuation; contradictory controls are rejected.
- Counts describe the bounded retrieved population, not an asserted exhaustive corpus total. Budget-truncated bodies, withheld sources, remaining results, and lane qualifications remain distinct.
- Every caller gets these guarantees even when no folder or note-type filter is supplied.

### US6 - Preserve evidence and ranking integrity

- id:: ^SPEC-0102-US6
- summary:: Maintainers can change retrieval or ranking while preserving deterministic, explainable relevance and diversity.
- status:: ready

#### Acceptance Criteria

- Merging an identical candidate/fact twice leaves its score unchanged; merging independent facts preserves their provenance under explicit bounds.
- Merge order and retriever completion order do not change chosen metadata, retained evidence, rank, or continuation membership for equivalent inputs.
- Ranking scores retain meaningful distinctions. Any display normalization is monotonic and cannot alter ranking or imply a calibrated probability.
- Fusion consumes genuine per-lane relevance ranks if retained; container iteration order and grouping order never become relevance evidence.
- Approximate pruning and deadline fallback use the effective normal-run weights and owner caps, with explicit qualifications for unavailable enrichment.
- Owner diversity and grouping preserve exact-target precedence and relevant multi-source coverage; duplicate enrichment and repeated facets do not create independent evidence.

### US7 - Stay responsive and useful under failure

- id:: ^SPEC-0102-US7
- summary:: Search returns useful results within a bounded time and explains missing capabilities without corrupting exactness or confidence.
- status:: ready

#### Acceptance Criteria

- Deadlines include provider work, retrieval, expansion, ranking, and source hydration. Cancellation stops work promptly and prevents unnecessary follow-on work.
- Broad discovery can return usable lexical/structural evidence when a provider or expansion lane fails; precision requests retain their exactness requirements.
- Index missing, index stale/incompatible, empty eligible corpus, provider unavailable, and no relevant match have distinguishable machine-readable outcomes.
- Cold and warm latency, provider calls, retrieved candidates, expansion size, body reads, and response bytes are measured on fixed workloads; performance targets below are met without weakening relevance targets.
- Query operations retain existing read-only store and embedding-runtime ownership. Optional caches use bounded, identity-keyed state and do not return stale results as current.

### US8 - Prove quality on a durable evaluation corpus

- id:: ^SPEC-0102-US8
- summary:: Maintainers can reproduce failures, compare alternatives, and reject regressions using a checked-in corpus and explicit relevance judgments.
- status:: ready

#### Acceptance Criteria

- A machine-readable corpus implements [[search-quality-evaluation-corpus]], retains SQ-001 through SQ-009, and adds every verified September 12 failure with exact input and expected behavior.
- Each run records corpus/source revision, index/chunk/provider fingerprints, effective request, candidate-stage evidence, final ranking, answer roles, warnings, coverage, timings, and response size.
- Correctness tests use deterministic fixtures without network access. Semantic-quality evaluation uses fixed real-provider embeddings or live provider runs with documented identity; hash-based test embeddings are not reported as semantic relevance evidence.
- Training/development and held-out cases are separated by topic/source cluster. Expected sources and relevance grades are authored before inspecting the candidate system's ranking and preserve judgment provenance.
- Reports distinguish candidate recall failures, rank failures, answer-selection failures, index readiness, and diagnostic/contract failures. Per-family results accompany aggregates.
- The quality targets below and all hard regressions pass; expectation changes require behavior rationale and the same review discipline as code.

### US9 - Deliver usable evidence through the actual interfaces

- id:: ^SPEC-0102-US9
- summary:: A user or agent receives source-backed excerpts and actionable next steps without redundant content or misleading diagnostics.
- status:: ready

#### Acceptance Criteria

- Selected sources preserve canonical note/embedded-node/code identity and source location through raw, compact, answer, and HTTP forms.
- Interactive excerpts identify why a result is relevant when a matching passage is available; missing snippets remain explicit and are never replaced with generated assertions.
- Agent bodies are budgeted and deduplicated once, with resolvable role references; session dedupe does not remove result identity or imply absent source content.
- Explain/timing options add observations without changing retrieval, rank, source inclusion, answer roles, continuation, or semantic warnings for equivalent requests and budgets.
- Real browser and agent journeys cover exact lookup, conceptual search, filtering, multiple pages, source opening, reload, cancellation, missing embeddings, and unavailable evidence.

## Requirements

### Quality targets

These are proposed acceptance targets, not claims about the current engine. Freeze the corpus, judgments, metric implementation, hardware/runtime manifest, and scoring rules before tuning. The associated effort defines the collection and run protocol.

| Dimension | Acceptance target |
| --- | --- |
| Hard regressions | 100% pass for score order, merge idempotence/determinism, fallback forwarding, exact target, filter parity, pagination, diagnostics invariance, and unsupported-confidence cases |
| Exact navigation | At least 95% success at rank 1 and 99% at rank 3 on eligible unambiguous held-out navigation; 100% for literal canonical-path and canonical-symbol fixtures |
| Precision tasks | 100% correct target/relationship kind among primary results in structural fixtures; expected eligible sources achieve recall at 20 of at least 0.95 on judged cases |
| Conceptual discovery | Macro nDCG@10 at least 0.85, no corpus/family cell with enough cases below 0.75; judged required-source recall@20 at least 0.90 |
| Answer usefulness | At least 0.90 precision of selected must-read sources using relevance grade 2 or 3; at least 0.90 required-role coverage on answerable multi-source cases |
| Confidence | Zero high-confidence answers on designated no-answer controls; at least 0.95 precision among high-confidence answerable cases; confidence coverage is reported so withholding all high-confidence results cannot hide quality |
| Interactive latency | On the frozen standard corpus: warm local/lexical p95 at most 250 ms; semantic p95 at most 1,500 ms including actual query embedding, with an external-provider breakdown |
| Agent latency | Warm task-evidence p95 at most 2,000 ms on the standard corpus; default hard deadline at most 8 s unless the caller explicitly supplies a different bounded budget |
| Concurrent use | At four concurrent requests, local processing p95 grows by at most 2x relative to the same workload at one; results, scope, and pagination remain correct |

Metric definitions and denominators must accompany the report. Absent/ambiguous targets and unavailable lanes are scored in their own families, not removed from results to inflate success. Report uncertainty and sample sizes; small cells cannot support a general quality claim. If the targets are infeasible under the frozen workload, present measured causes and a scoped decision instead of lowering thresholds silently.

### Governing boundaries

- Preserve [[unified-search-answer-architecture]]'s planner/executor separation and pure answer assembly, [[search-diagnostics-explain-architecture]]'s additive observability, and [[search-answer-workflow]]'s raw/answer/facet/budget contracts.
- This spec tightens relevance, exactness, confidence, and continuation behavior and adds caller profiles; it does not authorize weakening source identity, read-only queries, provider ownership, or store migrations.
- Reconcile current subsystem notes and affected active specs when behavior changes. Record any frozen-scope implications on the owning live effort before revising a frozen contract.
- Keep the public compatibility story explicit. Existing valid query inputs remain supported through one current implementation; stale cursor formats may receive a documented refresh error instead of a permanent alternate ranking path.

## Open Questions

The proposed choices are one shared application orchestrator, two caller profiles with precision intent precedence, explicit rejection of unsupported high confidence, and a fixed-window continuation contract. Approval of the effort plan accepts these choices and the targets above. Phase 1 must turn them into concrete interfaces and confirm migration details through the plan-requested foundation review. A material departure requires a recorded decision.

Corpus source selection and relevance judgments require review for representativeness. Use redistributable fixture material and the local Rhizome repository; private customer repositories are outside the authorization of this effort. Corpus expansion and routine internal interfaces are implementation choices within the approved plan.
