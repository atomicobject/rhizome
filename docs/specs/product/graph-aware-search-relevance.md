---
type: ProductSpec
id: SPEC-0120
summary: "Search uses the vault's links as relevance evidence: the words other notes use when they link to a source count for that source, and a source that is about the query gains bounded support from linked sources that are also about the query. Popularity never counts as relevance, and every effect is measured on a link-dense corpus before it ships."
spec-status: proposed
last-updated: 2026-10-08
aliases:
  - SPEC-0120
---

# Graph-aware search relevance

## Summary

Search ranks each source mostly by its own text: lexical matches, embedding similarity, and how many query words its title and headings cover. Links between sources play a small part. A knowledge vault, though, says much of what it knows through links. People name a note by the words they use when they link to it, and a topic usually spans several notes and sections that link to each other. When someone searches for that topic, a source that sits inside a linked group of relevant sources is more likely to be what they want than an isolated source with similar wording.

Drew's personal vault shows how much of this structure exists. Its 3,695 notes hold about 21,400 wiki links, 5.8 per note, and 81% of notes link somewhere. About 2,800 links sit in frontmatter properties. 2,005 links point at a heading or block rather than a whole note. 4,765 links carry a display label, and 3,305 of those labels use words that the target's title does not contain.

Text searches use almost none of this today:

- **Graph features are off for text queries.** Index time computes HITS authority and hub scores and label-propagation communities over note links, and query time can expand from the top base results through outgoing links, ontology relations, and personalized PageRank. All of these are enabled only when a request names a note seed (`pkg/app/unifiedsearch/run.go`). For an ordinary text search the planner gives the graph and ontology channels zero weight and skips those expansion stages; only code-reference expansion runs. No document records why.
- **Personalized PageRank skips note-only vaults.** It needs at least eight indexed code files, and it drops Markdown links and the detailed note-link kinds because they have no edge weight.
- **Link labels are parsed and then dropped.** The note parser reads each link's label, target heading or block, and position, but the index keeps one `graph_doc_edges` row per linked note pair with none of them, so search cannot tell which section a link came from or what it called its target.
- **No evaluation corpus is link-dense.** The repository's docs average about 3.5 wiki links per note and every fixture vault averages under 1.5, so the corpus cannot show whether a link signal helps.

The existing signals also mostly measure popularity or reach, and the [search subsystem note](../../reference/subsystems/search.md) records why that is risky: graph popularity is never aboutness, and a second retrieval pass seeded from top results (pseudo-relevance feedback) lowered must-read precision on conceptual and multi-facet questions until it was removed.

This spec adds two kinds of query-dependent link evidence and the measurement that must justify each one:

1. **Link text.** The label and surrounding text of a link that points to a source count as lexical evidence for that source, attributed to the linking source.
2. **Linked corroboration.** A source that is itself about the query gains bounded support when sources it links with, by body link or typed relation, are also about the query.

It also states how results from one linked group may be shown together, which is the presentation half of the idea, and it sets out what must be measured before any of it changes ranking. It extends [[search-engine-quality|SPEC-0102]], whose correctness rules, quality targets, and caller profiles still govern, and adds a corpus family under [[search-quality-evaluation-corpus|SPEC-0041]].

## Terms

- **Source**: a search result's canonical identity: a note, an embedded node such as a section, or a code file or symbol, as [[search-engine-quality|SPEC-0102]] US9 defines it.
- **Link**: a directed connection from one source to another: a body wiki link or Markdown link, a link inside a typed property (frontmatter or inline field), or an ontology relation. A link from inside a section belongs to that section and to its note.
- **Link text**: the label a link displays (its alias, or the target's name when it has none) and the line that contains it: the paragraph, list item, table row, or frontmatter property line, capped at a few hundred characters around the link.
- **Aboutness**: evidence that a source matches the query by its own content or identity, as the search subsystem defines it today: identity matches, title, heading, and body term matches, and embedding similarity. Graph scores, retriever ranks, and query specificity alone are not aboutness.
- **Candidate window**: the bounded set of candidates retrieved for one request before ranking ([[search-engine-quality|SPEC-0102]] US5).

## Goals

- A source that other notes link to with the query's words ranks as if those words described it, even when its own title and text use different words.
- Among sources that are about the query, one connected to other relevant sources ranks above an isolated one with similar text evidence.
- Linked results can be shown together, so a reader sees a topic's sources as a group instead of scattered through the list.
- No source gains rank from popularity, link volume, or neighborhood alone.
- Each new signal ships only after it improves a link-dense evaluation family without regressing [[search-engine-quality|SPEC-0102]]'s existing families or hard regressions.

## Non-Goals

- Expanding the candidate window by graph traversal. Existing auto-expansion keeps its role. Link text is lexical retrieval: it may surface a source that no other lane found, because the query's words describe it.
- Learned ranking, graph neural networks, graph embeddings, or a separate graph database.
- Changing the persisted HITS, community, or anchor PageRank computations, except to stop using a signal that the evaluation shows hurts relevance.
- Public per-signal weight controls; profiles and intents choose weights as they do today.
- Changing which links the indexer recognizes or how notes break into nodes.
- Publishing private vault content. Private-vault checks report aggregate counts and ranks only.

## User Stories

### US1 - Find a source by the words other notes use for it

- id:: ^SPEC-0120-US1
- summary:: A person searching with the words that linking notes use for a source finds that source, even when its own title and body use different words.
- status:: ready

#### Acceptance Criteria

- A source whose incoming link labels or link blocks contain the query's concepts gains lexical evidence for those concepts. The evidence names the linking source and the link, so diagnostics can show why the source matched.
- A label that repeats the target's own title adds nothing beyond the title match the source already has.
- Many links with the same label count with diminishing returns, so a heavily linked hub cannot outrank a better-matching source by link volume. Links from one linking source to the same target count once.
- A link from a source to itself or from its own sections adds no link-text evidence. A source that links to many targets, such as an index or hub note, counts less for each of them: beyond 20 distinct targets, each link counts 20 divided by the source's target count.
- Link-text evidence is ranking evidence, not identity evidence: it never satisfies exact-title or exact-path precedence, and on its own it does not make a source must-read or reach high confidence.
- A link that points at a heading or block gives its link text to the note that holds it, and the evidence records the heading or block. Scoring the section itself waits for section-level link data.

### US2 - Prefer a source that sits among other relevant sources

- id:: ^SPEC-0120-US2
- summary:: Among sources that are about the query, one linked with other sources that are also about the query ranks above an otherwise similar isolated source.
- status:: ready

#### Acceptance Criteria

- A candidate gains linked-corroboration evidence only when it has aboutness of its own, and only from linked candidates in the same window that also have aboutness. A candidate with no aboutness gains nothing, however many relevant sources link to it.
- Corroboration is computed from links among candidates in the window, in both directions, at query time. It does not read persisted popularity scores.
- Corroboration is bounded: it is capped per candidate, it grows with diminishing returns, and it is too small to lift a candidate over one with clearly stronger evidence of its own. The evaluation reports the largest rank change it causes.
- Every link kind counts equally at first. Typed relations or section links gain their own weights only where the evaluation shows a gain.
- A source's sections corroborate their own note once, not once per section, so a long note does not support itself.
- Exact-target precedence, owner caps, and multi-facet coverage behave as [[search-engine-quality|SPEC-0102]] US1 and US6 require.

### US3 - See a topic's linked sources together

- id:: ^SPEC-0120-US3
- summary:: A person browsing search results sees sources that link to each other grouped under the strongest one, so a topic's notes and sections read as one result.
- status:: draft

#### Acceptance Criteria

- The web Notes search page nests a note's matched sections under one row first (the D4 follow-up from EFF-2026-10-08-07-40), then groups strongly linked notes under the highest-ranked member. Grouping changes presentation only; rank order, continuation, and agent results stay as the engine returns them.
- A group shows why its members belong together: the links between them.
- Grouping never hides a source: every ranked source remains reachable in the order the engine returned it.

### US4 - Prove link signals on a link-dense corpus

- id:: ^SPEC-0120-US4
- summary:: A maintainer can measure each link signal on its own, against judged cases built around linked topics, before it changes default ranking.
- status:: ready

#### Acceptance Criteria

- A synthetic link-dense fixture vault joins the evaluation corpus. Its topic clusters each contain linked notes and sections about one topic, isolated distractors with similar wording, a hub note that links to everything, typed relations, heading links, and aliases whose words do not appear in their targets. Judgments are written with the notes, before any candidate run, and clusters split between development and held-out.
- Each signal can be evaluated on its own and together with the others (an ablation), on the new family and on every existing family, in fast and pinned live-embedding runs.
- The first measurement is a baseline that enables today's graph features for text queries without new signals, so the new signals are compared against the existing machinery working, not only against it switched off.
- A signal becomes default only when the new family improves on development and held-out cases, existing families hold within measured run-to-run noise, must-read precision on conceptual and multi-facet cases does not fall, and every hard regression and no-answer control still passes.
- A private-vault check runs locally and reports ranks and aggregate counts only, never note titles or text.

## Requirements

- MUST keep every link signal query-dependent. Persisted HITS, community, and PageRank scores may remain inputs only where the evaluation shows they help; this spec adds no new query-independent boost.
- MUST attribute link-text evidence to its linking source and link, and deduplicate it with the existing evidence fact rules, so merging the same fact twice leaves the score unchanged.
- MUST compute linked corroboration only from candidates already in the window, after their own evidence is known, and before owner caps and pagination, so continuation identity covers it. A ranking-policy version change invalidates older continuation tokens.
- MUST keep approximate ranking and deadline fallback aligned with the normal-run weights, or qualify the fallback result when corroboration is unavailable.
- MUST stay within [[search-engine-quality|SPEC-0102]]'s latency targets. Link lookups read indexed link tables for the window's sources; they never crawl notes at query time.
- MUST report, in explain diagnostics, each candidate's link-text and corroboration evidence and the sources it came from.
- MUST read link text from the index. For each link, the index stores its label, the heading or block it targets, the node it sits in, and its containing block. The change to stored link data requires a documented reindex.
- SHOULD apply to note, section, and code sources alike where links exist; code-to-code call edges keep their existing anchor PageRank treatment.
- MAY retire an existing graph signal (`graph_hits_authority`, `graph_hits_hub`, `same_community`, or link-expansion seeding) for an intent when the ablation shows the new evidence replaces it.

## Decisions

Drew asked on 2026-10-08 to implement this spec, which accepted the proposed answers to its open questions:

1. **Link text** is the label and the line that contains the link, with the label weighted above the line. Whole linking sections are too broad: they would make every daily note evidence for every note it mentions.
2. **Order of work.** Link text comes first, because it is the cheapest and the most likely to help navigation. Linked corroboration follows and ships only if it measures. Grouped presentation (US3) follows the ranking signals, in a later effort.
3. **Hub links count less**, as US1 states, because a note that links to everything says little about any one target.
4. **Existing graph features** are measured enabled for text queries as the US4 baseline, then enabled, fixed, or removed based on that measurement, rather than left as dead planner stages.

## Documentation plan

- Update the [search subsystem note](../../reference/subsystems/search.md) with each signal's constraints and measurements, and bump its `last-verified`.
- Record the new corpus family and its split under [[search-quality-evaluation-corpus|SPEC-0041]]'s update workflow.
- Update [[Graph (Hub)]] and the graph signals reference if a persisted signal is retired.
- Add a changelog entry when a signal changes default ranking.
