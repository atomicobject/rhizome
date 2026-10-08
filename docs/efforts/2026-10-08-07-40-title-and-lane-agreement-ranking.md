---
type: EffortNote
id: EFF-2026-10-08-07-40
aliases: [EFF-2026-10-08-07-40]
name: Title matches and lane agreement in search ranking
created-at: 2026-10-08T11:40:19Z
status: active
summary: Make a note found by several retrieval lanes score as one source, make query specificity measure concept coverage, and reward a title that contains the query phrase, measured on a new synthetic title-phrase corpus.
governing-specs:
  - "[[search-engine-quality]]"
  - "[[search-quality-evaluation-corpus]]"
---

# Title matches and lane agreement in search ranking

## Scope

Fix the ranking defects found when Drew searched "Innovation teams" in his personal vault on 2026-10-08. Three notes contain "innovation team(s)" in their filenames and a fourth in its H1, yet they first appeared at ranks 6, 14, 18, and 26 of 40. Notes that matched one query word and had slightly higher embedding similarity ranked above them.

Diagnosis, reproduced with the CLI built from `main` at `10e54947` against that vault (interactive profile, 100-candidate window):

- **Lane agreement is discarded.** The vector lane returns a whole-note chunk handle, `note_lexical` returns `note:<path>`, and Intel FTS returns section handles. The ranker scores each handle separately. `CanonicalizeSources` (`pkg/app/unifiedsearch/application/application.go:259`) then merges whole-note evidence for display but keeps the maximum score, so a note's title match and its vector similarity never add up.
- **Two of three signals are saturated.** Across the window, `query_specificity` ranged 0.83–1.0 because `ScoreFields` (`pkg/search/queryframe/frame.go:94`) adds a weight for every field a term appears in, so one query word repeated across path, title, breadcrumb, and heading reaches the cap. Intel FTS similarity ranged 0.968–0.978 because `bm25ToSimilarity` (`pkg/search/retrieval/lexical_intel.go:313`) is a sigmoid that pins every hit near 1, and the lexical channel clamp (`pkg/search/scoring.go:164`) hides the difference between a two-word and a one-word title match. Vector similarity (0.55–0.66) was the only signal with spread, so it set the order.
- **No title-phrase signal.** `note_title_exact` fires only when a title equals the whole query (`pkg/search/retrieval/note_lexical.go:137`), and title token matching is plural-sensitive.
- **Repeated rows.** With an owner cap of 3, one note filled three of the first seventeen interactive rows, and its title-match row was cut by the cap.

An offline replay of the weighted ranker over the same candidates reproduced every live score; only merged rows differed, each by exactly the 0.6 lexical contribution the maximum discards. Replayed fixes moved the four title-matching notes from (6, 14, 18, 26) to (7, 1, 19, 2) with combined scoring, (3, 1, 6, 2) with coverage-based specificity added, and (2, 3, 5, 4) with one row per note added.

Out of scope: new retrieval lanes, embedding-model changes, the opt-in reranker, learned ranking, excerpt rendering ("Excerpt unavailable"), and web layout changes. Showing one row per note with its matched sections nested is a follow-up (D4). The private vault stays out of the repository; it is a local dogfood check only.

## Spec Set (Frozen)

- [[search-engine-quality|SPEC-0102]], revision 2026-09-12, active.
- [[search-quality-evaluation-corpus|SPEC-0041]], revision 2026-09-05, active.

## Stories In Scope (Frozen)

- [[search-engine-quality#^SPEC-0102-US1|SPEC-0102 US1 — Find an identified source immediately]]: exact-match priority survives score aggregation and source grouping.
- [[search-engine-quality#^SPEC-0102-US6|SPEC-0102 US6 — Preserve evidence and ranking integrity]]: ranking scores retain meaningful distinctions; grouping preserves relevant multi-source coverage.
- [[search-engine-quality#^SPEC-0102-US8|SPEC-0102 US8 — Prove quality on a durable evaluation corpus]]: the new failure class enters the corpus with judgments authored before the candidate run, split by topic cluster.
- [[search-quality-evaluation-corpus#^SPEC-0041-US5|SPEC-0041 US5 — Update the corpus deliberately after ranking changes]].

Relationship to [[2026-09-12-15-26-search-engine-excellence|EFF-2026-09-12-15-26]]: that effort owned SPEC-0102 delivery broadly and was still marked active. Drew chose to keep this work as its own bounded effort and to close that one separately.

## Spec Coverage Checklist

- [ ] US8 / SPEC-0041 US5 — new title-phrase corpus and baseline: Batch 0.
- [ ] US6 — lane agreement scored together: Batch 1.
- [ ] US6 — specificity retains distinctions: Batch 2.
- [ ] US1 — title-phrase match outranks single-word title matches: Batch 3.

## Plan

### Decisions

1. **D1 — Coalesce whole-note candidates before ranking.** Move whole-note canonical identity (note handle, whole-note node chunks) into `pkg/search` so `Service.Search` merges them through `MergeCandidate` before `Ranker.Rank`. Section and embedded-node candidates stay distinct, as `CanonicalSourceIdentity` already intends. `CanonicalizeSources` keeps its merge as a no-op safety net and no longer takes a maximum over separately scored rows. Bump the ranking-policy version in continuation identity. Rejected alternative: re-scoring after `CanonicalizeSources`, which would re-rank in the application layer after owner caps were applied (US4 forbids adapters from re-scoring).
2. **D2 — Specificity counts each query concept once.** `ScoreFields` scores each concept (term variants grouped, as `queryConcepts` already does for assessment) at its best-weighted field, divided by concept count, keeping the multi-concept and code-form bonuses. This mirrors the existing support score and removes repeated-field inflation. Highest-risk change for code navigation and precision; gate on the repository and polyglot corpora.
3. **D3 — Title phrase evidence.** `note_lexical` emits `note_title_phrase` (lexical channel, `Rankable`) when the note title contains every query concept as one contiguous, ordered, plural-insensitive run, and title token matching becomes plural-insensitive. It is ranking evidence, not identity evidence: it does not join `note_title_exact`'s exact-navigation precedence or strong-support rules.
4. **D4 — One row per note: follow-up.** Drew chose nesting a note's matched sections under one row in the web Notes search page over capping the interactive profile at one row per note. That is a presentation change outside this ranking effort and is recorded under Compounding Follow-ups.

BM25 normalization and FTS column weights are not planned changes. Batch 2 measures whether lexical ties still decide the new corpus after D1 and D2; if they do, a relative-BM25 change becomes a recorded deviation with its own measurement.

### Batch 0 — Measurement

- Add `testdata/search-quality/title-phrase-vault`: about 40 short synthetic notes in four topic clusters. Each cluster has a two-word query; two or three notes contain the phrase inside a longer title (one only in its H1, one with a singular form); the rest match one word in their titles, share vocabulary semantically, or match one word in the title and the other in the body. Target notes sit in folders that sort after distractors, so handle-order tie-breaking cannot pass a case by accident.
- Map the corpus in `tools/searchquality` and add navigation cases to `corpus-v2.json`: two clusters development, two held-out. Judge every fixture note for every query (grade 0 for other clusters) before any candidate run, so no top-10 source is unjudged.
- Baselines: fast run on the new corpus and on the full corpus; a pinned live-embedding run on the new corpus with `-embedding-cache`; the drews-vault replay as a local dogfood check.
- Exit: the new cases fail at baseline in the same way the dogfood query does, or the effort records why the fixture does not reproduce it.

### Batch 1 — Lane agreement (D1)

- Red first: a regression in `pkg/search/archetype_regression_test.go` where one note found by a vector chunk and a title match outranks a vector-only note with higher cosine, and an application test that whole-note rows are no longer max-scored.
- Implement D1 in `pkg/search` (service merge) and `pkg/app/unifiedsearch/application`.
- Exit: focused tests green; new corpus improves; existing corpus families hold within cache-family noise.

### Batch 2 — Specificity coverage (D2)

- Red first: `pkg/search/queryframe` test where a candidate matching one of two concepts in four identity fields scores below one matching both concepts once.
- Implement D2. Run the full fast corpus and the pinned new-corpus run; inspect repository and polyglot navigation and precision cells.

### Batch 3 — Title phrase (D3)

- Red first: `note_lexical` tests for phrase inside a longer title, plural variant, and out-of-order words (no phrase evidence).
- Implement D3; register the evidence type in `pkg/search/scoring.go`.

### Documentation

Update `docs/reference/subsystems/search.md` with the D1–D3 constraints and their measurements, bump `last-verified`, and record new corpus cases under SPEC-0041's update workflow. SPEC-0102 needs no change.

### Validation

- `go test ./pkg/search/... ./pkg/app/unifiedsearch/... ./tools/searchquality/...` per batch; `make check` before each commit; CI's `make check-full`.
- `tools/searchquality` fast runs per batch; pinned live runs inside one `-embedding-cache` family.
- `./scripts/rzm validate` for changed Markdown.
- Delivery: one pull request from this branch to `main`.

## Original Intended Delivery

Batches 0–3 as approved on 2026-10-08: the title-phrase corpus and baselines; whole-note candidates coalesced before ranking (D1); concept-coverage specificity (D2); title-phrase evidence (D3); subsystem note updated; one pull request to `main`.

## Actual Delivered

Not started.

## Execution Notes

- 2026-10-08T11:40:19Z — Effort drafted from the dogfood diagnosis. Drew approved starting with measurement cases in chat; the ranking batches await plan approval. The configured current user is unavailable, so no `plan-approved-by` identity is recorded.
- 2026-10-08T12:04:25Z — Batch 0 fixture landed: `testdata/search-quality/title-phrase-vault` (48 notes, four clusters of 12) and eight navigation cases in `corpus-v2.json` (`TITLE_PHRASE_VAULT-NAVIGATION-*`; volunteer drivers and grant reporting are development, intake forms and board committees are held-out; plural and singular query per cluster). Every fixture note is judged for every query, written with the notes before any candidate run. `tools/searchquality` maps the corpus, and `TestDevelopmentCorpusFamiliesUseDistinctPhysicalCollections` now expects five development collections. The generator script stayed outside the repository; the notes and judgments are the durable artifacts.
- 2026-10-08T12:04:25Z — Baselines at `10e54947`, interactive profile, isolated fresh index, corpus fingerprint `a39515d2…`:
  - Fast (`-fast`, lexical only): nDCG@10 0.790, recall@20 1.000, nav@1 0.000 (0/8), nav@3 0.875 (7/8), 0 unjudged. Every term match scores 1.5 (lexical and specificity both saturated), so handle order breaks ties: `Log/` and `Notes/` distractors such as `Notes/Drivers.md` and `Notes/Forms library.md` outrank the `Projects/` targets.
  - Live, `voyage-4-lite`, `-embedding-cache /tmp/rz-tp/embedding-cache.sqlite`: nDCG@10 0.892, recall@20 1.000, nav@1 0.750 (6/8), nav@3 1.000, 0 unjudged. Target top rows carry only `note_vector_similarity` and `query_specificity`; their title-match rows score separately at 1.5 and fall behind the owner cap, the same mechanism as the dogfood query. Failures: `Notes/Drivers.md` (one-line definition, highest cosine) leads both volunteer-driver cases. The small fixture has less embedding competition than the 1,000-note dogfood vault, so the live gap is narrower than in production.
  - Existing four corpora, fast, agent and interactive profiles: running from a detached worktree of `10e54947` at `/tmp/rz-tp/base-wt` so edits here cannot invalidate the repository-corpus source fingerprint.
- 2026-10-08T12:05:30Z — Plan approval: Drew approved D1–D3 in chat, chose nested sections for D4 (recorded as a follow-up), and asked to keep this effort and close EFF-2026-09-12-15-26. No `plan-approved-by` identity is recorded because the configured current user is unavailable.

## Deviations

None.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

- D4: nest a note's matched sections under one row in the web Notes search page (Drew's choice on 2026-10-08), so a note with several matching sections appears once.
- Held-out prose and typed cases whose judgments point outside their fixture roots abort isolated runs (`PROSE_KNOWLEDGE_VAULT-NAVIGATION-02` names `testdata/agent-experience/...`); this effort baselines those corpora on the development split only.

## Status

Active. Batch 0 is complete; Batch 1 (D1) is next.
