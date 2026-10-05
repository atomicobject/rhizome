---
type: ReferenceDoc
reference-kind: analysis
summary: "Frozen search quality evaluation report with metric denominators and retained failure evidence."
last-verified: 2026-09-13
---

# Search quality report

Revision: `0049fa9d1bca65d511c04094a143c87889e45b86`
Corpus fingerprint: `abb109fb26ee1eaa5777f67e1893b127732035b41d9d3f0e97fa3f3ff9c9bfe3`
Mode: `deterministic-lexical`
Split: `development`

| Slice | Cases | nDCG@10 | Recall@20 | Nav@1 | Nav@3 | Must-read precision | Role coverage | High-confidence precision | High-confidence coverage | Unjudged top 10 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| overall | 178 | 0.518 (164) | 0.637 (164) | 0.967 (30) | 1.000 (30) | 0.204 (164) | 0.647 (136) | 0.000 (0) | 0.000 (164) | 949 |
| corpus: polyglot-code-fixture | 41 | 0.400 (38) | 0.447 (38) | 0.857 (7) | 1.000 (7) | 0.316 (38) | 0.469 (32) | 0.000 (0) | 0.000 (38) | 114 |
| corpus: prose-knowledge-vault | 47 | 0.552 (44) | 0.659 (44) | 1.000 (8) | 1.000 (8) | 0.106 (44) | 0.683 (41) | 0.000 (0) | 0.000 (44) | 312 |
| corpus: rhizome-repository | 47 | 0.398 (42) | 0.631 (42) | 1.000 (6) | 1.000 (6) | 0.107 (42) | 0.679 (28) | 0.000 (0) | 0.000 (42) | 315 |
| corpus: typed-note-fixture | 43 | 0.721 (40) | 0.800 (40) | 1.000 (9) | 1.000 (9) | 0.306 (40) | 0.743 (35) | 0.000 (0) | 0.000 (40) | 208 |
| family: conceptual_discovery | 54 | 0.516 (54) | 0.741 (54) | 0.000 (0) | 0.000 (0) | 0.181 (54) | 0.745 (51) | 0.000 (0) | 0.000 (54) | 387 |
| family: control | 29 | 0.000 (15) | 0.000 (15) | 0.000 (0) | 0.000 (0) | 0.000 (15) | 0.000 (0) | 0.000 (0) | 0.000 (15) | 86 |
| family: multi_facet | 23 | 0.733 (23) | 0.978 (23) | 0.000 (0) | 0.000 (0) | 0.344 (23) | 0.955 (22) | 0.000 (0) | 0.000 (23) | 192 |
| family: navigation | 31 | 0.988 (31) | 1.000 (31) | 0.967 (30) | 1.000 (30) | 0.389 (31) | 1.000 (28) | 0.000 (0) | 0.000 (31) | 189 |
| family: structural_precision | 41 | 0.236 (41) | 0.268 (41) | 0.000 (0) | 0.000 (0) | 0.089 (41) | 0.029 (35) | 0.000 (0) | 0.000 (41) | 95 |

## Retained failures

Total: 133. Complete per-case output is retained in the adjacent JSON artifact.

- `AUDIT-DEFINITION` `judgment`: 9 unjudged sources in top 10

- `AUDIT-PAGINATION` `judgment`: 9 unjudged sources in top 10

- `AUDIT-TESTS` `judgment`: 9 unjudged sources in top 10

- `AUDIT-TITLE` `judgment`: 9 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-02` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-06` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-07` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-10` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-12` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-13` `judgment`: 4 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-15` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-16` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-18` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-19` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-20` `judgment`: 4 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONCEPTUAL_DISCOVERY-21` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONTROL-07` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONTROL-10` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONTROL-11` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-CONTROL-12` `judgment`: 2 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-MULTI_FACET-02` `judgment`: 9 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-MULTI_FACET-03` `judgment`: 9 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-MULTI_FACET-04` `judgment`: 9 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-MULTI_FACET-08` `judgment`: 9 unjudged sources in top 10

- `POLYGLOT_CODE_FIXTURE-MULTI_FACET-09` `judgment`: 9 unjudged sources in top 10

108 additional failures are retained in JSON.
