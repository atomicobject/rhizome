---
type: EffortNote
id: EFF-2026-10-08-16-23
aliases: [EFF-2026-10-08-16-23]
name: Graph-aware search relevance
created-at: 2026-10-08T16:23:58Z
status: complete
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

- [[graph-aware-search-relevance|SPEC-0120]], revision 2026-10-08, proposed when frozen and active at closure.
- [[search-engine-quality|SPEC-0102]], revision 2026-09-12, active.
- [[search-quality-evaluation-corpus|SPEC-0041]], revision 2026-09-05, active.

## Stories In Scope (Frozen)

- [[graph-aware-search-relevance#^SPEC-0120-US1|SPEC-0120 US1 — Find a source by the words other notes use for it]].
- [[graph-aware-search-relevance#^SPEC-0120-US2|SPEC-0120 US2 — Prefer a source that sits among other relevant sources]].
- [[graph-aware-search-relevance#^SPEC-0120-US4|SPEC-0120 US4 — Prove link signals on a link-dense corpus]].
- [[search-quality-evaluation-corpus#^SPEC-0041-US5|SPEC-0041 US5 — Update the corpus deliberately after ranking changes]].

## Spec Coverage Checklist

- [x] US4 — link-dense fixture judged before any candidate run; baselines with and without today's graph features: `2083f713` and Execution Notes.
- [x] US1 — link labels and lines stored per link and scored as lexical evidence for the target: `64b6c184`, `dd455936`, `2897d021`.
- [x] US2 — linked corroboration measured; it failed the US4 rule and was removed in `9c80ce77` (see Deviations).
- [x] US4 — ablation per signal on the new family and all existing families: Execution Notes.

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

On `t3/graph-native-search`, in PR #16:

- `testdata/search-quality/linked-topics-vault`: 62 notes for a fictional library network in four topic clusters, with alias targets, linked groups using frontmatter and heading links, isolated distractors, a hub, and a decoy label. Eight `LINKED_TOPICS_VAULT-*` cases in `corpus-v2.json`, every note judged for every query before any run; catalog migration and courier consolidation are development, overdue fines and homebound service are held-out.
- D1: `graph_doc_edges.link_text` (intel schema v72) holds each link's label and line on the coarse note-link row; notemeta indexer version 13 rederives links on the first run after updating.
- D2: `LinkTextRetriever` emits `link_text_match` for broad text intents. A label at least two linking notes agree on that names every query concept is read with the title by query specificity and protected from window pruning.
- D3: graph features stay seed-only for text queries, by measurement.
- D4: linked corroboration was built, measured, and removed.
- `RankingPolicyVersion` is `search-quality-v3`; the search, notemeta, and stores subsystem notes, SPEC-0120, and the changelog describe the change.

## Execution Notes

- 2026-10-08T16:23:58Z — Drew asked to implement SPEC-0120 through effort closure in PR #16 instead of a docs-only PR. That instruction is the plan approval for US1, US2, and US4 with the spec's proposed answers; the configured current user is unavailable, so no `plan-approved-by` identity is recorded.
- 2026-10-08T16:50:00Z — Batch 0. An Opus 5.5 subagent wrote `testdata/search-quality/linked-topics-vault` and its eight cases together, without running search: 62 notes, 244 wiki links (3.0 per note without the hub), 35 aliased, 15 heading links, 84 in frontmatter, 0 unresolved. Each alias target is labeled by 7 or 8 notes and never contains its alias phrase. Families are `navigation` and `conceptual_discovery`.
- 2026-10-08T16:55:00Z — Baselines at `87d7ee5e`, interactive profile, isolated fresh indexes, pinned `voyage-4-lite` cache `/tmp/rz-glt/emb-ltv.sqlite`. Live: navigation nDCG@10 0.606, nav@1 0/4; conceptual 0.711. Fast: navigation 0.635 with recall@20 0, because nothing but the alias names the targets.
- 2026-10-08T16:55:00Z — Batch 1 (D3). Enabling graph features for text queries (`EnableGraph` without a note seed, built in a scratch worktree) moved live navigation 0.606 to 0.602 and conceptual 0.711 to 0.715. No gain, so text queries stay seed-only.
- 2026-10-08T16:58:00Z — Batch 2 (D1, D2). Link text alone retrieved every alias target (fast recall@20 0 to 1.0) but ranked them 15th to 18th: notes that use the alias in their own text also link to the target and carry query specificity the target lacks. `dd455936` reads an agreed label with the title. Live: navigation nDCG@10 0.606 to 0.911 and nav@1 0/4 to 4/4 (development 0.564 to 0.895, held-out 0.648 to 0.927); conceptual 0.711 to 0.716. Fast: navigation 0.635 to 0.868, nav@3 0 to 1.0.
- 2026-10-08T17:00:00Z — Batch 3 (D4). Corroboration had no effect while graph weight was zero for text queries. With graph features on it lowered conceptual to 0.675, but popularity and outgoing-link expansion came with it. Scored alone on the otherwise empty refs channel (weight 0.25) in a scratch build, it moved conceptual 0.716 to 0.707. Nearly every windowed candidate has vector evidence and distractors link to each other, so it rewards being linked; removed in `9c80ce77`.
- 2026-10-08T17:02:00Z — Private-vault check, local only: a SQLite backup of Drew's index and his Markdown notes copied to `/tmp`, indexed with each binary (migration and link rederivation took 92 s; 8,906 note-link rows carry text). For 30 sampled labels that at least two notes use for the same target with words its title lacks, the target reached the top 3 for 13 queries (baseline 6), the top 10 for 19 (12), and first for 6 (3); median CLI latency stayed 1.8 s, with the retriever at 12 ms. A first run missed agreed-label targets that the retriever ranked first: window pruning runs before query specificity, fixed in `2897d021`. The 8 remaining misses are one-word labels that also appear in the target's title and in many other titles. Two targets dropped (5 to 8, 11 to 14).
- 2026-10-08T17:03:00Z — Existing corpora, agent profile, final code against `87d7ee5e`. Fast development: every existing corpus unchanged except typed-note nDCG@10 0.881 to 0.886; must-read precision, role coverage, controls, and high-confidence precision unchanged. Fast held-out: polyglot 0.949 to 0.950, with 21 newly surfaced unjudged sources. Live (`voyage-4-lite`, cache `/tmp/rz-tp/embedding-cache-existing.sqlite`): development identical in every corpus and family (title-phrase 0.972 to 0.971), held-out overall nDCG@10 0.786 to 0.795 (repository 0.461 to 0.513, conceptual 0.686 to 0.707), with must-read precision, role coverage, controls, and high-confidence precision unchanged. `make check` passed.
- 2026-10-08T17:20:00Z — Independent review (Claude Fable, read-only) of `87d7ee5e..237b90a1` found no blocking design issue and three defects, fixed in `f2fcf8bd`: a note linking one target by both wiki and Markdown syntax counted twice and could form an agreed label alone; a 2000-row cap on the link-text scan dropped targets alphabetically before scoring; and the schema drift check did not require `link_text`.
- 2026-10-09T11:30:00Z — Greptile review (CLI, base effort) found three more defects, fixed in the next commit: an agreed label also raised answer support and identity matches, so a note named only by its linking notes could count as strong source-owned support; the agreed label was chosen before checking query coverage, so a more common shorter label hid one that names the whole query; and a long label or link was stored past the 300-byte cap. The labels now raise only ranking specificity, only labels that name every query concept compete, and each stored label and line is cut to 300 bytes of valid UTF-8. The fast and live reruns of every corpus matched the previous final run exactly.

## Deviations

- 2026-10-08 — US1's "ranking evidence only" criterion changed. Link text alone ranked alias targets 15th to 18th on the new corpus: the notes that use the alias in their own text also link to the target, and they carry query specificity the target lacks. A label that at least two linking notes agree on, and that names every query concept, now counts like the title in query specificity and is protected from pruning; one note's label and the line around a link stay ranking-only. SPEC-0120 was revised on 2026-10-08 to say so (frozen-scope-drift acknowledged for SPEC-0120). Authority: Drew's request to implement the spec, and the measured failure.
- 2026-10-08 — US2 linked corroboration was not delivered. It failed the US4 rule (conceptual nDCG@10 0.716 to 0.707 on the new corpus with live vectors), so `9c80ce77` removed it, and SPEC-0120 US2 returns to draft. The cluster idea continues as US3 presentation.

## Closure Checklist

- [x] Required quality gates pass.
- [x] Alignment and actual outcomes are verified.
- [x] Specs and documentation are reconciled.
- [x] Follow-ups are triaged.

## Compounding Follow-ups

- SPEC-0120 US3: nest a note's matched sections under one row in the Notes search page, then group linked notes. This is now the main route for Drew's cluster idea.
- SPEC-0120 US2 needs an aboutness test stricter than retrieval before linked corroboration is tried again: with live vectors nearly every windowed candidate counts as relevant.
- One-word queries whose word appears in many titles still bury the intended note; 8 of 30 sampled personal-vault labels missed the top 25 for that reason, before and after this change.
- Section-level link text: a heading link gives its text to the note, not the section.
- `NoteLinkTextMatches` scans link rows with `LIKE`; add an FTS index if very large vaults make it slow (12 ms on 8,906 rows).

## Status

Complete. Link text (US1) and the link-dense corpus (US4) ship in PR #16; linked corroboration (US2) was measured, removed, and recorded as a deviation. CI and review outcomes are recorded before merge.
