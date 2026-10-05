---
type: ReferenceDoc
reference-kind: analysis
summary: "Frozen search quality evaluation report with metric denominators and retained failure evidence."
last-verified: 2026-09-12
---

# Search quality report

Revision: `0049fa9d1bca65d511c04094a143c87889e45b86`
Corpus fingerprint: `9cc7d7faa7a07ece89464b6ffcb2445dd6e4db931da11bbec3a4d66a8ceb4666`
Mode: `deterministic-lexical`
Split: `development`

| Slice | Cases | nDCG@10 | Recall@20 | Nav@1 | Nav@3 | Must-read precision | Role coverage | High-confidence precision | High-confidence coverage | Unjudged top 10 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| overall | 178 | 0.464 (164) | 0.637 (164) | 0.581 (31) | 0.774 (31) | 0.198 (164) | 0.647 (136) | 0.200 (15) | 0.091 (164) | 952 |
| corpus: polyglot-code-fixture | 41 | 0.365 (38) | 0.447 (38) | 0.714 (7) | 1.000 (7) | 0.316 (38) | 0.469 (32) | 0.667 (3) | 0.079 (38) | 114 |
| corpus: prose-knowledge-vault | 47 | 0.513 (44) | 0.659 (44) | 0.625 (8) | 0.750 (8) | 0.098 (44) | 0.683 (41) | 0.000 (0) | 0.000 (44) | 312 |
| corpus: rhizome-repository | 47 | 0.299 (42) | 0.631 (42) | 0.286 (7) | 0.429 (7) | 0.091 (42) | 0.679 (28) | 1.000 (1) | 0.024 (42) | 318 |
| corpus: typed-note-fixture | 43 | 0.679 (40) | 0.800 (40) | 0.667 (9) | 0.889 (9) | 0.306 (40) | 0.743 (35) | 0.000 (11) | 0.275 (40) | 208 |
| family: conceptual_discovery | 54 | 0.516 (54) | 0.741 (54) | 0.000 (0) | 0.000 (0) | 0.178 (54) | 0.745 (51) | 0.000 (7) | 0.130 (54) | 387 |
| family: control | 29 | 0.000 (15) | 0.000 (15) | 0.000 (0) | 0.000 (0) | 0.000 (15) | 0.000 (0) | 0.000 (0) | 0.000 (15) | 86 |
| family: multi_facet | 23 | 0.733 (23) | 0.978 (23) | 0.000 (0) | 0.000 (0) | 0.344 (23) | 0.955 (22) | 0.000 (0) | 0.000 (23) | 192 |
| family: navigation | 31 | 0.779 (31) | 1.000 (31) | 0.581 (31) | 0.774 (31) | 0.362 (31) | 1.000 (28) | 0.000 (4) | 0.129 (31) | 190 |
| family: structural_precision | 41 | 0.178 (41) | 0.268 (41) | 0.000 (0) | 0.000 (0) | 0.089 (41) | 0.029 (35) | 0.750 (4) | 0.098 (41) | 97 |

## Retained failures

Total: 133. Complete per-case output is retained in the adjacent JSON artifact.

- `AUDIT-DEFINITION` `judgment`: 9 unjudged sources in top 10

- `AUDIT-PAGINATION` `judgment`: 9 unjudged sources in top 10

- `AUDIT-TESTS` `judgment`: 10 unjudged sources in top 10

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
