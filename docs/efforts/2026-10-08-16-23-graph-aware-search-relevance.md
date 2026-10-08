---
type: EffortNote
id: EFF-2026-10-08-16-23
aliases: [EFF-2026-10-08-16-23]
name: Graph-aware search relevance
created-at: 2026-10-08T16:23:58Z
status: active
summary: Deliver SPEC-0120 US1, US2, and US4. Link labels and their lines become lexical evidence for the linked note, linked corroboration is measured as a bounded ranking signal, and a link-dense synthetic corpus measures both against today's graph features.
governing-specs:
  - "[[graph-aware-search-relevance]]"
  - "[[search-engine-quality]]"
  - "[[search-quality-evaluation-corpus]]"
---

# Graph-aware search relevance

## Scope

Drew proposed making search more graph-native: notes break into nodes, typed relations connect them, and a tightly linked group of sources about the query topic should count for more than isolated matches. SPEC-0120 turns that into query-dependent link evidence. This effort delivers its ranking half and the measurement that has to justify it.

Starting facts, verified at `ee1a2796`:

- Text searches give graph evidence zero weight. `unifiedsearch.Run` sets `EnableGraph` only when the request has a note seed, so the planner zeroes the graph and ontology channels and skips graph, ontology, and personalized PageRank expansion for every ordinary query.
- `graph_doc_edges` keeps one row per linked note pair and kind. The Markdown parser reads each link's label, fragment, and range, and the projection drops them.
- No evaluation fixture averages more than 1.5 wiki links per note; Drew's vault averages 5.8.

Out of scope: US3 grouped presentation (nested sections in the Notes search page, then linked groups), section-level link-text candidates, new graph traversal, learned ranking, and changes to persisted HITS, community, or anchor PageRank computation.

## Spec Set (Frozen)

- [[graph-aware-search-relevance|SPEC-0120]], revision 2026-10-08, proposed until this effort closes.
- [[search-engine-quality|SPEC-0102]], revision 2026-09-12, active.
- [[search-quality-evaluation-corpus|SPEC-0041]], revision 2026-09-05, active.

## Stories In Scope (Frozen)

- [[graph-aware-search-relevance#^SPEC-0120-US1|SPEC-0120 US1 — Find a source by the words other notes use for it]].
- [[graph-aware-search-relevance#^SPEC-0120-US2|SPEC-0120 US2 — Prefer a source that sits among other relevant sources]].
- [[graph-aware-search-relevance#^SPEC-0120-US4|SPEC-0120 US4 — Prove link signals on a link-dense corpus]].
- [[search-quality-evaluation-corpus#^SPEC-0041-US5|SPEC-0041 US5 — Update the corpus deliberately after ranking changes]].

## Spec Coverage Checklist

- [ ] US4 — link-dense fixture judged before any candidate run; baselines with and without today's graph features.
- [ ] US1 — link labels and lines stored per link and scored as lexical evidence for the target.
- [ ] US2 — linked corroboration measured; shipped only if it passes the US4 rule.
- [ ] US4 — ablation per signal on the new family and all existing families.

## Plan

### Decisions

1. **D1 — Store link text on the edge row.** `graph_doc_edges` gains a `link_text` column, written by notemeta projection with the edge rows, so incremental deltas, snapshot replacement, and deletion keep it consistent without a new table. Each edge row's text joins the distinct labels and lines of the links from one source to one target, capped. Intel schema v72 adds the column; notemeta indexer version 13 forces rederivation, which is the documented reindex.
2. **D2 — A link-text retriever.** `LinkTextRetriever` reads note-link rows whose text contains a query term, scores each target by concept coverage of its labels (full weight) and lines (half weight) per linking source, discounts sources with more than 20 targets, combines sources with diminishing returns, and emits `link_text_match` on the lexical channel. It is not identity, aboutness, or lane-corroboration evidence. It runs for the broad text intents with the other lexical retrievers.
3. **D3 — Measure today's graph features first.** Run the corpus with `EnableGraph` on for text queries. Keep it on only if it passes the US4 rule; otherwise leave it seed-only and record the measurement.
4. **D4 — Linked corroboration as a ranker decorator.** `LinkedCorroborationRanker` loads edges among the window's owners, and gives a candidate with its own lexical or semantic evidence `linked_corroboration` evidence from linked candidates that also have it. Capped, diminishing, ranking-only. Ships only if it passes the US4 rule; otherwise recorded as measured and removed.

### Batches

- **Batch 0 — Measurement.** `testdata/search-quality/linked-topics-vault`: four topic clusters, alias targets, linked groups with frontmatter and heading links, isolated distractors, a hub, and a decoy label. Navigation and conceptual cases per cluster, every note judged for every query before any run; two clusters development, two held-out. Fast and pinned live baselines.
- **Batch 1 — Graph features baseline (D3).**
- **Batch 2 — Link text (D1, D2).** Store and migration tests first, then retriever tests, then measurement.
- **Batch 3 — Linked corroboration (D4).** Decorator tests first, then measurement.

US4 rule for any default change: the new family improves on development and held-out, existing families hold within measured noise, conceptual and multi-facet must-read precision does not fall, and every hard regression and no-answer control passes.

### Documentation

Search, notemeta, and stores subsystem notes for the new evidence, column, and versions; SPEC-0041 corpus record; changelog for the ranking change and the reindex; SPEC-0120 marked active at closure.

### Validation

`go test` for changed packages per batch, `make check` before each commit, CI `make check-full`; `tools/searchquality` fast and pinned live runs from detached snapshot worktrees; `./scripts/rzm validate`.

## Original Intended Delivery

Batches 0–3 on `t3/graph-native-search`, delivered in PR #16 with this effort's closure.

## Actual Delivered

Pending.

## Execution Notes

- 2026-10-08T16:23:58Z — Drew asked to implement SPEC-0120 through effort closure in PR #16 instead of a docs-only PR. That instruction is the plan approval for US1, US2, and US4 with the spec's proposed answers; the configured current user is unavailable, so no `plan-approved-by` identity is recorded.

## Deviations

None yet.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

- SPEC-0120 US3: nest a note's matched sections under one row in the Notes search page, then group linked notes.

## Status

Active. Batch 0 fixture in progress.
